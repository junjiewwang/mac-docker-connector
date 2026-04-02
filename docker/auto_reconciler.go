package main

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

const managedRuleCommentPrefix = "mdc-auto:"

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
	Routes        []DesiredRoute        `json:"routes"`
	IptablesPairs []DesiredIptablesPair `json:"iptables_pairs"`
	VMLinks       []string              `json:"vm_links"`
	GeneratedAt   string                `json:"generated_at"`
	Source        string                `json:"source,omitempty"`
}

type AutoReconcileStatus struct {
	ControlPlaneMode string              `json:"control_plane_mode"`
	Desired          DesiredNetworkState `json:"desired"`
	LastReconcile    string              `json:"last_reconcile"`
	LastError        string              `json:"last_error,omitempty"`
	Unresolved       []string            `json:"unresolved,omitempty"`
}

type managedRuleSpec struct {
	Table string
	Chain string
	Tag   string
	Rule  []string
}

func (s managedRuleSpec) withComment() []string {
	rule := append([]string{}, s.Rule...)
	for i := 0; i < len(rule); i++ {
		if rule[i] == "-j" {
			withComment := append([]string{}, rule[:i]...)
			withComment = append(withComment, "-m", "comment", "--comment", s.Tag)
			withComment = append(withComment, rule[i:]...)
			return withComment
		}
	}
	rule = append(rule, "-m", "comment", "--comment", s.Tag)
	return rule
}

func (s managedRuleSpec) line() string {
	return "-A " + s.Chain + " " + strings.Join(s.withComment(), " ")
}

func (s managedRuleSpec) signature() string {
	return iptablesRuleSignature(s.Chain, s.Rule)
}

type managedDesiredResources struct {
	Rules         []managedRuleSpec
	DesiredRoutes map[string]string
	CleanupRoutes map[string]string
	DNSDesired    bool
	DNSMinikube   *MinikubeInfo
	Unresolved    []string
}

type AutoReconciler struct {
	linkMgr *LinkManager

	mu                sync.RWMutex
	desired           DesiredNetworkState
	lastReconcile     time.Time
	lastError         string
	unresolved        []string
	lastManagedRoutes map[string]string
}

func NewAutoReconciler(linkMgr *LinkManager) *AutoReconciler {
	return &AutoReconciler{
		linkMgr:           linkMgr,
		lastManagedRoutes: make(map[string]string),
	}
}

func (r *AutoReconciler) Start() {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			_ = r.Reconcile("periodic")
		}
	}()
}

func (r *AutoReconciler) UpdateDesiredState(state DesiredNetworkState) error {
	normalized := normalizeDesiredState(state)
	r.mu.Lock()
	r.desired = normalized
	r.mu.Unlock()
	return r.Reconcile("desired-state-update")
}

func (r *AutoReconciler) DesiredState() DesiredNetworkState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return copyDesiredState(r.desired)
}

func (r *AutoReconciler) Status() AutoReconcileStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	status := AutoReconcileStatus{
		ControlPlaneMode: "single",
		Desired:          copyDesiredState(r.desired),
		Unresolved:       append([]string{}, r.unresolved...),
	}
	if !r.lastReconcile.IsZero() {
		status.LastReconcile = r.lastReconcile.Format(time.RFC3339)
	}
	if r.lastError != "" {
		status.LastError = r.lastError
	}
	return status
}

func (r *AutoReconciler) AnnotateDesired(statuses []LinkStatus) {
	desiredLinks := make(map[string]bool)
	for _, name := range r.DesiredState().VMLinks {
		desiredLinks[name] = true
	}
	for i := range statuses {
		statuses[i].Desired = desiredLinks[statuses[i].Name]
	}
}

func (r *AutoReconciler) Reconcile(reason string) error {
	r.linkMgr.RefreshCache()
	resources := r.buildDesiredResources()

	if err := r.reconcileManagedRules(resources.Rules); err != nil {
		r.recordReconcileResult(resources.Unresolved, err)
		return err
	}
	if err := r.reconcileManagedRoutes(resources.DesiredRoutes, resources.CleanupRoutes); err != nil {
		r.recordReconcileResult(resources.Unresolved, err)
		return err
	}
	if err := r.reconcileManagedDNS(resources.DNSDesired, resources.DNSMinikube); err != nil {
		r.recordReconcileResult(resources.Unresolved, err)
		return err
	}

	r.linkMgr.InvalidateCache()
	r.recordReconcileResult(resources.Unresolved, nil)
	if debug {
		fmt.Printf("[AUTO] reconcile 完成: reason=%s rules=%d unresolved=%d\n", reason, len(resources.Rules), len(resources.Unresolved))
	}
	return nil
}

func (r *AutoReconciler) recordReconcileResult(unresolved []string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastReconcile = time.Now()
	r.unresolved = append([]string{}, unresolved...)
	if err != nil {
		r.lastError = err.Error()
	} else {
		r.lastError = ""
	}
}

func (r *AutoReconciler) buildDesiredResources() managedDesiredResources {
	desired := r.DesiredState()
	resources := managedDesiredResources{
		DesiredRoutes: make(map[string]string),
		CleanupRoutes: make(map[string]string),
	}

	bridges := r.linkMgr.Bridges()
	nonMkBridges := r.linkMgr.NonMkBridges()
	mkInfo := r.linkMgr.MinikubeInfo()
	physicalIf := r.linkMgr.PhysicalInterface()
	tun0Exists := r.linkMgr.network.InterfaceExists("tun0")

	bridgeBySubnet := make(map[string]BridgeInfo)
	for _, bridge := range bridges {
		if bridge.Subnet != "" {
			bridgeBySubnet[bridge.Subnet] = bridge
		}
	}

	for _, route := range desired.Routes {
		bridge, ok := bridgeBySubnet[route.Network]
		if !ok {
			resources.Unresolved = append(resources.Unresolved, fmt.Sprintf("route %s 未解析到当前 Docker bridge", route.Network))
			continue
		}
		if !tun0Exists {
			resources.Unresolved = append(resources.Unresolved, fmt.Sprintf("route %s 缺少 tun0", route.Network))
			continue
		}
		tagBase := "route:" + sanitizeTag(route.Network)
		resources.Rules = append(resources.Rules,
			managedRuleSpec{
				Table: "filter",
				Chain: "FORWARD",
				Tag:   managedRuleCommentPrefix + tagBase + ":tun-to-bridge",
				Rule:  []string{"-i", "tun0", "-d", route.Network, "-o", bridge.Name, "-j", "ACCEPT"},
			},
			managedRuleSpec{
				Table: "filter",
				Chain: "FORWARD",
				Tag:   managedRuleCommentPrefix + tagBase + ":bridge-to-tun",
				Rule:  []string{"-i", bridge.Name, "-s", route.Network, "-o", "tun0", "-j", "ACCEPT"},
			},
		)
	}

	for _, pair := range desired.IptablesPairs {
		if !pair.Connect {
			continue
		}
		bridgeA, okA := bridgeBySubnet[pair.SubnetA]
		bridgeB, okB := bridgeBySubnet[pair.SubnetB]
		if !okA || !okB {
			resources.Unresolved = append(resources.Unresolved, fmt.Sprintf("pair %s <-> %s 未解析到当前 Docker bridge", pair.SubnetA, pair.SubnetB))
			continue
		}
		tagBase := "pair:" + sanitizeTag(pair.SubnetA) + "__" + sanitizeTag(pair.SubnetB)
		resources.Rules = append(resources.Rules,
			managedRuleSpec{
				Table: "filter",
				Chain: "FORWARD",
				Tag:   managedRuleCommentPrefix + tagBase + ":a-to-b",
				Rule:  []string{"-i", bridgeA.Name, "-s", pair.SubnetA, "-o", bridgeB.Name, "-d", pair.SubnetB, "-j", "ACCEPT"},
			},
			managedRuleSpec{
				Table: "filter",
				Chain: "FORWARD",
				Tag:   managedRuleCommentPrefix + tagBase + ":b-to-a",
				Rule:  []string{"-i", bridgeB.Name, "-s", pair.SubnetB, "-o", bridgeA.Name, "-d", pair.SubnetA, "-j", "ACCEPT"},
			},
		)
	}

	desiredLinks := make(map[string]bool)
	for _, name := range desired.VMLinks {
		desiredLinks[name] = true
	}

	if desiredLinks["internet"] {
		if physicalIf == "" {
			resources.Unresolved = append(resources.Unresolved, "internet 缺少默认出口网卡")
		} else {
			for _, bridge := range bridges {
				tagBase := "vmlink:internet:" + sanitizeTag(bridge.Subnet)
				resources.Rules = append(resources.Rules,
					managedRuleSpec{
						Table: "filter",
						Chain: "FORWARD",
						Tag:   managedRuleCommentPrefix + tagBase + ":bridge-to-external",
						Rule:  []string{"-i", bridge.Name, "-o", physicalIf, "-j", "ACCEPT"},
					},
					managedRuleSpec{
						Table: "filter",
						Chain: "FORWARD",
						Tag:   managedRuleCommentPrefix + tagBase + ":external-to-bridge",
						Rule:  []string{"-i", physicalIf, "-o", bridge.Name, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
					},
					managedRuleSpec{
						Table: "nat",
						Chain: "POSTROUTING",
						Tag:   managedRuleCommentPrefix + tagBase + ":masquerade",
						Rule:  []string{"-s", bridge.Subnet, "-o", physicalIf, "-j", "MASQUERADE"},
					},
				)
			}
		}
	}

	if desiredLinks["host-docker"] {
		if !tun0Exists {
			resources.Unresolved = append(resources.Unresolved, "host-docker 缺少 tun0")
		} else {
			for _, bridge := range nonMkBridges {
				tagBase := "vmlink:host-docker:" + sanitizeTag(bridge.Subnet)
				resources.Rules = append(resources.Rules,
					managedRuleSpec{
						Table: "filter",
						Chain: "FORWARD",
						Tag:   managedRuleCommentPrefix + tagBase + ":tun-to-bridge",
						Rule:  []string{"-i", "tun0", "-o", bridge.Name, "-j", "ACCEPT"},
					},
					managedRuleSpec{
						Table: "filter",
						Chain: "FORWARD",
						Tag:   managedRuleCommentPrefix + tagBase + ":bridge-to-tun",
						Rule:  []string{"-i", bridge.Name, "-o", "tun0", "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
					},
				)
			}
		}
	}

	if desiredLinks["docker-docker"] {
		for _, bridge := range nonMkBridges {
			tagBase := "vmlink:docker-docker:" + sanitizeTag(bridge.Subnet)
			resources.Rules = append(resources.Rules,
				managedRuleSpec{
					Table: "filter",
					Chain: "FORWARD",
					Tag:   managedRuleCommentPrefix + tagBase + ":self",
					Rule:  []string{"-i", bridge.Name, "-o", bridge.Name, "-j", "ACCEPT"},
				},
				managedRuleSpec{
					Table: "filter",
					Chain: "FORWARD",
					Tag:   managedRuleCommentPrefix + tagBase + ":out-established",
					Rule:  []string{"-i", bridge.Name, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
				},
				managedRuleSpec{
					Table: "filter",
					Chain: "FORWARD",
					Tag:   managedRuleCommentPrefix + tagBase + ":in-established",
					Rule:  []string{"-o", bridge.Name, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
				},
			)
		}
	}

	if desiredLinks["host-k8s.service"] {
		if mkInfo == nil {
			resources.Unresolved = append(resources.Unresolved, "host-k8s.service 未发现运行中的 Minikube")
		} else if !tun0Exists {
			resources.Unresolved = append(resources.Unresolved, "host-k8s.service 缺少 tun0")
		} else {
			tagBase := "vmlink:host-k8s.service:" + sanitizeTag(mkInfo.Subnet)
			resources.Rules = append(resources.Rules,
				managedRuleSpec{
					Table: "filter",
					Chain: "FORWARD",
					Tag:   managedRuleCommentPrefix + tagBase + ":tun-to-mk",
					Rule:  []string{"-i", "tun0", "-o", mkInfo.BridgeName, "-j", "ACCEPT"},
				},
				managedRuleSpec{
					Table: "filter",
					Chain: "FORWARD",
					Tag:   managedRuleCommentPrefix + tagBase + ":mk-to-tun",
					Rule:  []string{"-i", mkInfo.BridgeName, "-o", "tun0", "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
				},
			)
			if mkInfo.ServiceCIDR != "" {
				resources.DesiredRoutes[mkInfo.ServiceCIDR] = mkInfo.ContainerIP
				resources.CleanupRoutes[mkInfo.ServiceCIDR] = mkInfo.ContainerIP
			} else {
				resources.Unresolved = append(resources.Unresolved, "host-k8s.service 未发现 Service CIDR")
			}
			resources.DNSDesired = true
			resources.DNSMinikube = mkInfo
		}
	} else if mkInfo != nil && mkInfo.ServiceCIDR != "" {
		resources.CleanupRoutes[mkInfo.ServiceCIDR] = mkInfo.ContainerIP
	}

	if desiredLinks["host-k8s.pod"] {
		if mkInfo == nil {
			resources.Unresolved = append(resources.Unresolved, "host-k8s.pod 未发现运行中的 Minikube")
		} else if !tun0Exists {
			resources.Unresolved = append(resources.Unresolved, "host-k8s.pod 缺少 tun0")
		} else if mkInfo.PodCIDR == "" {
			resources.Unresolved = append(resources.Unresolved, "host-k8s.pod 未发现 Pod CIDR")
		} else {
			tagBase := "vmlink:host-k8s.pod:" + sanitizeTag(mkInfo.PodCIDR)
			resources.Rules = append(resources.Rules,
				managedRuleSpec{
					Table: "filter",
					Chain: "FORWARD",
					Tag:   managedRuleCommentPrefix + tagBase + ":tun-to-mk",
					Rule:  []string{"-i", "tun0", "-d", mkInfo.PodCIDR, "-o", mkInfo.BridgeName, "-j", "ACCEPT"},
				},
				managedRuleSpec{
					Table: "filter",
					Chain: "FORWARD",
					Tag:   managedRuleCommentPrefix + tagBase + ":mk-to-tun",
					Rule:  []string{"-i", mkInfo.BridgeName, "-s", mkInfo.PodCIDR, "-o", "tun0", "-j", "ACCEPT"},
				},
			)
			resources.DesiredRoutes[mkInfo.PodCIDR] = mkInfo.ContainerIP
			resources.CleanupRoutes[mkInfo.PodCIDR] = mkInfo.ContainerIP
		}
	} else if mkInfo != nil && mkInfo.PodCIDR != "" {
		resources.CleanupRoutes[mkInfo.PodCIDR] = mkInfo.ContainerIP
	}

	if desiredLinks["docker-k8s.service"] {
		if mkInfo == nil {
			resources.Unresolved = append(resources.Unresolved, "docker-k8s.service 未发现运行中的 Minikube")
		} else {
			for _, bridge := range nonMkBridges {
				tagBase := "vmlink:docker-k8s.service:" + sanitizeTag(bridge.Subnet)
				resources.Rules = append(resources.Rules,
					managedRuleSpec{
						Table: "filter",
						Chain: "FORWARD",
						Tag:   managedRuleCommentPrefix + tagBase + ":bridge-to-mk",
						Rule:  []string{"-i", bridge.Name, "-o", mkInfo.BridgeName, "-j", "ACCEPT"},
					},
					managedRuleSpec{
						Table: "filter",
						Chain: "FORWARD",
						Tag:   managedRuleCommentPrefix + tagBase + ":mk-to-bridge",
						Rule:  []string{"-i", mkInfo.BridgeName, "-o", bridge.Name, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
					},
				)
			}
		}
	}

	if desiredLinks["docker-k8s.pod"] {
		if mkInfo == nil {
			resources.Unresolved = append(resources.Unresolved, "docker-k8s.pod 未发现运行中的 Minikube")
		} else if mkInfo.PodCIDR == "" {
			resources.Unresolved = append(resources.Unresolved, "docker-k8s.pod 未发现 Pod CIDR")
		} else {
			for _, bridge := range nonMkBridges {
				tagBase := "vmlink:docker-k8s.pod:" + sanitizeTag(bridge.Subnet)
				resources.Rules = append(resources.Rules,
					managedRuleSpec{
						Table: "filter",
						Chain: "FORWARD",
						Tag:   managedRuleCommentPrefix + tagBase + ":bridge-to-mk",
						Rule:  []string{"-i", bridge.Name, "-d", mkInfo.PodCIDR, "-o", mkInfo.BridgeName, "-j", "ACCEPT"},
					},
					managedRuleSpec{
						Table: "filter",
						Chain: "FORWARD",
						Tag:   managedRuleCommentPrefix + tagBase + ":mk-to-bridge",
						Rule:  []string{"-i", mkInfo.BridgeName, "-s", mkInfo.PodCIDR, "-o", bridge.Name, "-j", "ACCEPT"},
					},
				)
			}
		}
	}

	return resources
}

func (r *AutoReconciler) reconcileManagedRules(desired []managedRuleSpec) error {
	byChain := make(map[string][]managedRuleSpec)
	for _, spec := range desired {
		key := spec.Table + ":" + spec.Chain
		byChain[key] = append(byChain[key], spec)
	}
	allKeys := []string{"filter:FORWARD", "nat:POSTROUTING"}
	for _, key := range allKeys {
		parts := strings.SplitN(key, ":", 2)
		if err := r.reconcileManagedChain(parts[0], parts[1], byChain[key]); err != nil {
			return err
		}
	}
	return nil
}

func (r *AutoReconciler) reconcileManagedChain(table, chain string, desired []managedRuleSpec) error {
	currentLines, err := listManagedRuleLines(table, chain)
	if err != nil {
		return err
	}
	currentByTag := make(map[string][]string)
	for _, line := range currentLines {
		tag := extractManagedTag(line)
		if tag == "" {
			continue
		}
		currentByTag[tag] = append(currentByTag[tag], line)
	}

	desiredByTag := make(map[string]managedRuleSpec)
	for _, spec := range desired {
		desiredByTag[spec.Tag] = spec
	}

	var firstErr error
	for tag, lines := range currentByTag {
		spec, ok := desiredByTag[tag]
		if !ok {
			for _, line := range lines {
				if err := deleteManagedRuleLine(table, line); err != nil && firstErr == nil {
					firstErr = err
				}
			}
			continue
		}
		expectedSignature := spec.signature()
		matched := false
		for _, line := range lines {
			if iptablesLineSignature(line) == expectedSignature {
				matched = true
				continue
			}
			if err := deleteManagedRuleLine(table, line); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if matched {
			delete(desiredByTag, tag)
		}
	}

	for _, spec := range desiredByTag {
		if err := addManagedRule(spec); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (r *AutoReconciler) reconcileManagedRoutes(desiredRoutes map[string]string, cleanupRoutes map[string]string) error {
	r.mu.RLock()
	prevRoutes := copyStringMap(r.lastManagedRoutes)
	r.mu.RUnlock()

	for network, gateway := range prevRoutes {
		if desiredGateway, ok := desiredRoutes[network]; ok && desiredGateway == gateway {
			continue
		}
		_, _ = r.linkMgr.routeMgr.RemoveRoute(network, gateway)
	}

	for network, gateway := range cleanupRoutes {
		if _, ok := desiredRoutes[network]; ok {
			continue
		}
		_, _ = r.linkMgr.routeMgr.RemoveRoute(network, gateway)
	}

	for network, gateway := range desiredRoutes {
		if oldGateway, ok := prevRoutes[network]; ok && oldGateway == gateway {
			continue
		}
		if oldGateway, ok := prevRoutes[network]; ok && oldGateway != gateway {
			_, _ = r.linkMgr.routeMgr.RemoveRoute(network, oldGateway)
		}
		if _, err := r.linkMgr.routeMgr.AddRoute(network, gateway); err != nil {
			return err
		}
	}

	r.mu.Lock()
	r.lastManagedRoutes = copyStringMap(desiredRoutes)
	r.mu.Unlock()
	return nil
}

func (r *AutoReconciler) reconcileManagedDNS(desired bool, mkInfo *MinikubeInfo) error {
	if desired {
		_, err := r.linkMgr.dnsMgr.ConfigureDNS(mkInfo)
		return err
	}
	r.linkMgr.dnsMgr.RevertDNS()
	return nil
}

func normalizeDesiredState(state DesiredNetworkState) DesiredNetworkState {
	normalized := DesiredNetworkState{
		GeneratedAt: state.GeneratedAt,
		Source:      state.Source,
	}

	routeSet := make(map[string]bool)
	for _, route := range state.Routes {
		network := normalizeCIDR(route.Network)
		if network == "" || routeSet[network] {
			continue
		}
		routeSet[network] = true
		normalized.Routes = append(normalized.Routes, DesiredRoute{Network: network, Expose: route.Expose})
	}
	sort.Slice(normalized.Routes, func(i, j int) bool {
		return normalized.Routes[i].Network < normalized.Routes[j].Network
	})

	pairSet := make(map[string]bool)
	for _, pair := range state.IptablesPairs {
		a, b := canonicalSubnetPair(pair.SubnetA, pair.SubnetB)
		if a == "" || b == "" {
			continue
		}
		key := a + "|" + b
		pairSet[key] = pair.Connect
	}
	for key, connect := range pairSet {
		parts := strings.SplitN(key, "|", 2)
		normalized.IptablesPairs = append(normalized.IptablesPairs, DesiredIptablesPair{
			SubnetA: parts[0],
			SubnetB: parts[1],
			Connect: connect,
		})
	}
	sort.Slice(normalized.IptablesPairs, func(i, j int) bool {
		if normalized.IptablesPairs[i].SubnetA != normalized.IptablesPairs[j].SubnetA {
			return normalized.IptablesPairs[i].SubnetA < normalized.IptablesPairs[j].SubnetA
		}
		if normalized.IptablesPairs[i].SubnetB != normalized.IptablesPairs[j].SubnetB {
			return normalized.IptablesPairs[i].SubnetB < normalized.IptablesPairs[j].SubnetB
		}
		return !normalized.IptablesPairs[i].Connect && normalized.IptablesPairs[j].Connect
	})

	linkSet := make(map[string]bool)
	for _, name := range state.VMLinks {
		name = strings.TrimSpace(name)
		if name == "" || linkSet[name] {
			continue
		}
		linkSet[name] = true
		normalized.VMLinks = append(normalized.VMLinks, name)
	}
	sort.Strings(normalized.VMLinks)
	return normalized
}

func copyDesiredState(src DesiredNetworkState) DesiredNetworkState {
	data, _ := json.Marshal(src)
	var dst DesiredNetworkState
	_ = json.Unmarshal(data, &dst)
	return dst
}

func copyStringMap(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func normalizeCIDR(raw string) string {
	_, ipNet, err := net.ParseCIDR(strings.TrimSpace(raw))
	if err != nil || ipNet == nil {
		return ""
	}
	return ipNet.String()
}

func canonicalSubnetPair(subnetA, subnetB string) (string, string) {
	a := normalizeCIDR(subnetA)
	b := normalizeCIDR(subnetB)
	if a == "" || b == "" {
		return "", ""
	}
	if a <= b {
		return a, b
	}
	return b, a
}

func sanitizeTag(raw string) string {
	replacer := strings.NewReplacer("/", "_", ".", "-", ":", "-", " ", "-", "|", "-", ",", "-")
	return replacer.Replace(raw)
}

func listManagedRuleLines(table, chain string) ([]string, error) {
	args := []string{"iptables"}
	if table != "filter" {
		args = append(args, "-t", table)
	}
	args = append(args, "-S", chain)
	out, err := runCommandSudo(args...)
	if err != nil {
		return nil, fmt.Errorf("列出规则失败: table=%s chain=%s err=%v", table, chain, err)
	}
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, managedRuleCommentPrefix) {
			lines = append(lines, trimmed)
		}
	}
	return lines, nil
}

func extractManagedTag(line string) string {
	fields := tokenizeIptablesLine(line)
	for i := 0; i < len(fields)-1; i++ {
		if fields[i] == "--comment" {
			return fields[i+1]
		}
	}
	return ""
}

func deleteManagedRuleLine(table, line string) error {
	fields := tokenizeIptablesLine(line)
	if len(fields) < 3 || fields[0] != "-A" {
		return fmt.Errorf("无法解析规则行: %s", line)
	}
	args := []string{"iptables"}
	if table != "filter" {
		args = append(args, "-t", table)
	}
	args = append(args, "-D", fields[1])
	args = append(args, fields[2:]...)
	if _, err := runCommandSudo(args...); err != nil {
		return fmt.Errorf("删除托管规则失败: %s => %v", line, err)
	}
	return nil
}

func addManagedRule(spec managedRuleSpec) error {
	args := []string{"iptables"}
	if spec.Table != "filter" {
		args = append(args, "-t", spec.Table)
	}
	args = append(args, "-A", spec.Chain)
	args = append(args, spec.withComment()...)
	if _, err := runCommandSudo(args...); err != nil {
		return fmt.Errorf("添加托管规则失败: %s => %v", spec.line(), err)
	}
	return nil
}
