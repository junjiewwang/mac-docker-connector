package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	desiredStateSyncMu       sync.Mutex
	lastDesiredStateSyncTime time.Time
)

const desiredStateSyncInterval = 30 * time.Second

var supportedVMLinks = map[string]bool{
	"internet":           true,
	"host-docker":        true,
	"host-k8s.service":   true,
	"host-k8s.pod":       true,
	"docker-k8s.service": true,
	"docker-k8s.pod":     true,
	"docker-docker":      true,
}

type DesiredRoute struct {
	Network string `json:"network"`
	Expose  bool   `json:"expose,omitempty"`
}

type DesiredIptablesPair struct {
	SubnetA string `json:"subnet_a"`
	SubnetB string `json:"subnet_b"`
	Connect bool   `json:"connect"`
}

type DesiredNetworkState struct {
	Routes        []DesiredRoute         `json:"routes"`
	IptablesPairs []DesiredIptablesPair  `json:"iptables_pairs"`
	VMLinks       []string               `json:"vm_links"`
	GeneratedAt   string                 `json:"generated_at"`
	Source        string                 `json:"source,omitempty"`
}

func normalizeVMLinkName(raw string) string {
	name := strings.TrimSpace(raw)
	if supportedVMLinks[name] {
		return name
	}
	return ""
}

func normalizeCIDRString(raw string) string {
	_, ipNet, err := net.ParseCIDR(strings.TrimSpace(raw))
	if err != nil || ipNet == nil {
		return ""
	}
	return ipNet.String()
}

func canonicalPair(subnetA, subnetB string) (string, string) {
	a := normalizeCIDRString(subnetA)
	b := normalizeCIDRString(subnetB)
	if a == "" || b == "" {
		return "", ""
	}
	if a <= b {
		return a, b
	}
	return b, a
}

func buildDesiredNetworkState() (*DesiredNetworkState, error) {
	cfg, err := parseConfigToJSON()
	if err != nil {
		return nil, err
	}

	state := &DesiredNetworkState{
		GeneratedAt: time.Now().Format(time.RFC3339),
		Source:      "desktop-config",
	}

	routeMap := make(map[string]bool)
	for _, route := range cfg.Routes {
		network := normalizeCIDRString(route.Network)
		if network == "" {
			continue
		}
		if _, ok := routeMap[network]; ok {
			continue
		}
		routeMap[network] = route.Expose
		state.Routes = append(state.Routes, DesiredRoute{Network: network, Expose: route.Expose})
	}
	sort.Slice(state.Routes, func(i, j int) bool {
		return state.Routes[i].Network < state.Routes[j].Network
	})

	pairMap := make(map[string]bool)
	for _, pair := range cfg.Iptables {
		a, b := canonicalPair(pair.SubnetA, pair.SubnetB)
		if a == "" || b == "" {
			continue
		}
		key := a + "|" + b
		pairMap[key] = pair.Action != "disconnect"
	}
	for key, connect := range pairMap {
		parts := strings.SplitN(key, "|", 2)
		state.IptablesPairs = append(state.IptablesPairs, DesiredIptablesPair{
			SubnetA: parts[0],
			SubnetB: parts[1],
			Connect: connect,
		})
	}
	sort.Slice(state.IptablesPairs, func(i, j int) bool {
		if state.IptablesPairs[i].SubnetA != state.IptablesPairs[j].SubnetA {
			return state.IptablesPairs[i].SubnetA < state.IptablesPairs[j].SubnetA
		}
		if state.IptablesPairs[i].SubnetB != state.IptablesPairs[j].SubnetB {
			return state.IptablesPairs[i].SubnetB < state.IptablesPairs[j].SubnetB
		}
		return !state.IptablesPairs[i].Connect && state.IptablesPairs[j].Connect
	})

	linkSet := make(map[string]bool)
	for _, link := range cfg.VMLinks {
		name := normalizeVMLinkName(link.Name)
		if name == "" || linkSet[name] {
			continue
		}
		linkSet[name] = true
		state.VMLinks = append(state.VMLinks, name)
	}
	sort.Strings(state.VMLinks)

	return state, nil
}

func syncDesiredState(reason string) error {
	desiredStateSyncMu.Lock()
	defer desiredStateSyncMu.Unlock()

	if peer == nil {
		return fmt.Errorf("peer IP 未初始化")
	}

	state, err := buildDesiredNetworkState()
	if err != nil {
		return fmt.Errorf("构建期望网络状态失败: %v", err)
	}

	body, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("序列化期望网络状态失败: %v", err)
	}

	vmURL := fmt.Sprintf("http://%s:%d/api/desired-state", peer.String(), vmHTTPPort)
	req, err := http.NewRequest(http.MethodPut, vmURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建同步请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Desired-State-Reason", reason)

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("同步到 VM 失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("VM 返回非 200 状态: %d", resp.StatusCode)
	}

	var vmResp struct {
		OK      *bool  `json:"ok"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&vmResp); err != nil {
		return fmt.Errorf("解析 VM 同步响应失败: %v", err)
	}
	if vmResp.OK != nil && !*vmResp.OK {
		if vmResp.Message == "" {
			vmResp.Message = "VM 收敛失败"
		}
		return fmt.Errorf(vmResp.Message)
	}

	lastDesiredStateSyncTime = time.Now()
	logger.Infof("[DESIRED-STATE] 已同步到 VM: reason=%s routes=%d pairs=%d vm_links=%d", reason, len(state.Routes), len(state.IptablesPairs), len(state.VMLinks))
	return nil
}

func syncDesiredStateAsync(reason string) {
	go func() {
		if err := syncDesiredState(reason); err != nil {
			logger.Warningf("[DESIRED-STATE] 同步失败: reason=%s err=%v", reason, err)
		}
	}()
}

func syncDesiredStateIfStale(reason string) {
	desiredStateSyncMu.Lock()
	stale := time.Since(lastDesiredStateSyncTime) >= desiredStateSyncInterval
	desiredStateSyncMu.Unlock()
	if stale {
		syncDesiredStateAsync(reason)
	}
}

func handleAPIConfigVMLink(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "message": "无效的请求体"})
		return
	}

	name := normalizeVMLinkName(req.Name)
	if name == "" {
		writeJSON(w, map[string]interface{}{"ok": false, "message": "不支持的 VM 链路名称"})
		return
	}

	switch r.Method {
	case http.MethodPost:
		if vmLinks[name] {
			writeJSON(w, map[string]interface{}{"ok": false, "message": fmt.Sprintf("VM 链路 %s 已存在", name)})
			return
		}
		if err := addConfigLine("vm-link " + name); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "message": err.Error()})
			return
		}
		syncDesiredStateAsync("api-config-vm-link-add")
		writeJSON(w, map[string]interface{}{"ok": true, "message": fmt.Sprintf("VM 链路 %s 已加入自动收敛配置，热加载将在 2 秒内生效", name)})
	case http.MethodDelete:
		if err := removeConfigLine(func(key, value string) bool {
			return key == "vm-link" && normalizeVMLinkName(value) == name
		}); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "message": err.Error()})
			return
		}
		syncDesiredStateAsync("api-config-vm-link-delete")
		writeJSON(w, map[string]interface{}{"ok": true, "message": fmt.Sprintf("VM 链路 %s 已移出自动收敛配置，热加载将在 2 秒内生效", name)})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
