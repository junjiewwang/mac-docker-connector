package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/op/go-logging"
)

// ==================== Dashboard 数据模型 ====================

// RouteStatus 路由状态枚举
type RouteStatus string

const (
	RouteOK       RouteStatus = "ok"
	RouteMissing  RouteStatus = "missing"
	RouteConflict RouteStatus = "conflict"
	RouteWrongGW  RouteStatus = "wrong_gw"
	RouteExtra    RouteStatus = "extra"
)

// RouteEntry 路由条目
type RouteEntry struct {
	Network           string      `json:"network"`
	Status            RouteStatus `json:"status"`
	ConfSource        bool        `json:"conf_source"`         // 是否来自 conf 文件
	SystemGateway     string      `json:"system_gateway"`      // 系统中的实际网关
	SystemInterface   string      `json:"system_interface"`    // 系统中的实际接口
	ExpectedGateway   string      `json:"expected_gateway"`    // 期望的网关 (peer IP)
	ExpectedInterface string      `json:"expected_interface"`  // 期望的接口 (utunX)
}

// RouteSummary 路由校验汇总
type RouteSummary struct {
	Total    int `json:"total"`
	OK       int `json:"ok"`
	Missing  int `json:"missing"`
	Conflict int `json:"conflict"`
	WrongGW  int `json:"wrong_gw"`
	Extra    int `json:"extra"`
}

// ConnectorStatus Connector 进程状态
type ConnectorStatus struct {
	Uptime          string `json:"uptime"`
	UDPPort         int    `json:"udp_port"`
	ClientConnected bool   `json:"client_connected"`
	ClientAddr      string `json:"client_addr"`
	TUNInterface    string `json:"tun_interface"`
	PeerIP          string `json:"peer_ip"`
	LocalIP         string `json:"local_ip"`
	ConfigFile      string `json:"config_file"`
}

// DashboardResponse 综合状态响应
type DashboardResponse struct {
	Timestamp string           `json:"timestamp"`
	Connector ConnectorStatus  `json:"connector"`
	Routes    RouteVerifyResult `json:"routes"`
}

// RouteVerifyResult 路由校验结果
type RouteVerifyResult struct {
	PeerIP       string       `json:"peer_ip"`
	TUNInterface string       `json:"tun_interface"`
	Summary      RouteSummary `json:"summary"`
	Routes       []RouteEntry `json:"routes"`
}

// FixResult 修复结果
type FixResult struct {
	Fixed   int      `json:"fixed"`
	Failed  int      `json:"failed"`
	Details []string `json:"details"`
}

// SystemRoute 从 netstat 解析出的系统路由
type SystemRoute struct {
	Destination string
	Gateway     string
	Interface   string
	Flags       string
}

// ==================== Dashboard 服务 ====================

var (
	dashboardStartTime time.Time
	dashboardOnce      sync.Once
	vmHTTPPort         = 2522 // VM 端 HTTP API 端口
)

// startDashboard 启动 HTTP Dashboard 服务
func startDashboard() {
	dashboardOnce.Do(func() {
		dashboardStartTime = time.Now()

		mux := http.NewServeMux()

		// 前端页面
		mux.HandleFunc("/", handleDashboardPage)

		// API 端点
		mux.HandleFunc("/api/status", handleAPIStatus)
		mux.HandleFunc("/api/routes/verify", handleAPIRoutesVerify)
		mux.HandleFunc("/api/routes/fix", handleAPIRoutesFix)

		// Config API 端点
		mux.HandleFunc("/api/config", handleAPIConfig)
		mux.HandleFunc("/api/config/raw", handleAPIConfigRaw)
		mux.HandleFunc("/api/config/route", handleAPIConfigRoute)
		mux.HandleFunc("/api/config/iptables", handleAPIConfigIptables)
		mux.HandleFunc("/api/config/expose", handleAPIConfigExpose)
		mux.HandleFunc("/api/config/token", handleAPIConfigToken)
		mux.HandleFunc("/api/config/hosts", handleAPIConfigHosts)
		mux.HandleFunc("/api/config/proxy", handleAPIConfigProxy)
		mux.HandleFunc("/api/config/basic", handleAPIConfigBasic)
		mux.HandleFunc("/api/config/discover", handleAPIConfigDiscover)

		// VM 反向代理 — /api/vm/* → http://peerIP:2522/api/*
		registerVMProxy(mux)

		listenAddr := fmt.Sprintf("%s:%d", host, port)
		logger.Infof("[DASHBOARD] Starting HTTP Dashboard on http://%s", listenAddr)

		server := &http.Server{
			Addr:         listenAddr,
			Handler:      corsMiddleware(mux),
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 30 * time.Second,
		}

		if err := server.ListenAndServe(); err != nil {
			logger.Warningf("[DASHBOARD] HTTP server error: %v", err)
		}
	})
}

// corsMiddleware CORS 中间件
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ==================== API Handlers ====================

// handleDashboardPage 返回 Dashboard HTML 页面
func handleDashboardPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(dashboardHTML))
}

// handleAPIStatus 返回综合状态
func handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := DashboardResponse{
		Timestamp: time.Now().Format(time.RFC3339),
		Connector: getConnectorStatus(),
		Routes:    verifyRoutes(),
	}

	writeJSON(w, status)
}

// handleAPIRoutesVerify 路由校验
func handleAPIRoutesVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, verifyRoutes())
}

// handleAPIRoutesFix 修复缺失路由
func handleAPIRoutesFix(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	result := fixMissingRoutes()
	writeJSON(w, result)
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// ==================== Connector 状态采集 ====================

func getConnectorStatus() ConnectorStatus {
	status := ConnectorStatus{
		UDPPort:    port,
		ConfigFile: configFile,
	}

	// 运行时间
	if !dashboardStartTime.IsZero() {
		status.Uptime = formatDuration(time.Since(dashboardStartTime))
	}

	// 客户端连接
	if cli != nil {
		status.ClientConnected = true
		status.ClientAddr = cli.String()
	}

	// Peer IP
	if peer != nil {
		status.PeerIP = peer.String()
	}

	// Local IP
	if localIP != nil && len(localIP) == 4 {
		status.LocalIP = fmt.Sprintf("%d.%d.%d.%d", localIP[0], localIP[1], localIP[2], localIP[3])
	}

	// TUN 接口
	status.TUNInterface = detectTUNInterface()

	return status
}

// detectTUNInterface 检测 TUN 接口名称
func detectTUNInterface() string {
	// 通过 ifconfig 查找 peer IP 关联的 utun 接口
	if peer == nil {
		return ""
	}

	out, err := exec.Command("ifconfig").Output()
	if err != nil {
		return ""
	}

	peerStr := peer.String()
	lines := strings.Split(string(out), "\n")
	currentIface := ""

	for _, line := range lines {
		// 接口行：utunX: flags=...
		if !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, " ") && strings.Contains(line, ":") {
			currentIface = strings.Split(line, ":")[0]
		}
		// 查找包含 peer IP 的行
		if strings.Contains(line, peerStr) && strings.HasPrefix(currentIface, "utun") {
			return currentIface
		}
	}

	return ""
}

// formatDuration 格式化时间间隔
func formatDuration(d time.Duration) string {
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

// ==================== 路由校验核心逻辑 ====================

// verifyRoutes 校验 conf 中的路由与系统路由表的一致性
func verifyRoutes() RouteVerifyResult {
	result := RouteVerifyResult{}

	// 1. 获取 peer IP
	if peer != nil {
		result.PeerIP = peer.String()
	}

	// 2. 检测 TUN 接口
	result.TUNInterface = detectTUNInterface()

	// 3. 获取 conf 中的路由列表
	confRoutes := getConfRoutes()

	// 4. 获取系统路由表
	sysRoutes := getSystemRoutes()

	// 5. 逐条对比
	peerStr := ""
	if peer != nil {
		peerStr = peer.String()
	}

	for network := range confRoutes {
		entry := RouteEntry{
			Network:         network,
			ConfSource:      true,
			ExpectedGateway: peerStr,
			ExpectedInterface: result.TUNInterface,
		}

		if sysRoute, ok := findSystemRoute(sysRoutes, network, peerStr); ok {
			entry.SystemGateway = sysRoute.Gateway
			entry.SystemInterface = sysRoute.Interface

			if sysRoute.Gateway == peerStr || (result.TUNInterface != "" && sysRoute.Interface == result.TUNInterface) {
				entry.Status = RouteOK
				result.Summary.OK++
			} else if strings.HasPrefix(sysRoute.Interface, "utun") {
				entry.Status = RouteConflict
				result.Summary.Conflict++
			} else {
				entry.Status = RouteWrongGW
				result.Summary.WrongGW++
			}
		} else {
			entry.Status = RouteMissing
			result.Summary.Missing++
		}

		result.Summary.Total++
		result.Routes = append(result.Routes, entry)
	}

	// 6. 检查系统中有但 conf 中没有的路由（仅限 utun 接口）
	// 预处理 conf 路由的 IP 部分（去掉掩码），用于过滤关联路由
	confIPs := make(map[string]bool)
	for network := range confRoutes {
		ip := extractIP(network)
		if ip != "" {
			confIPs[ip] = true
		}
	}

	if result.TUNInterface != "" {
		for _, sysRoute := range sysRoutes {
			if sysRoute.Interface == result.TUNInterface {
				if _, ok := confRoutes[sysRoute.Destination]; !ok {
					// 排除 host 路由 (peer 自身的路由) 和 link-local 路由
					if sysRoute.Destination == peerStr || strings.HasPrefix(sysRoute.Destination, "169.254") {
						continue
					}
					// 排除与 conf 路由 IP 相同但掩码不同的关联路由
					// 例如 conf 有 172.17.0.0/16，系统有 172.17.0.0（/8 host路由），
					// 两者 IP 部分相同，属于关联路由，不标记为 EXTRA
					sysIP := extractIP(sysRoute.Destination)
					if sysIP != "" && confIPs[sysIP] {
						continue
					}
					entry := RouteEntry{
						Network:         sysRoute.Destination,
						Status:          RouteExtra,
						ConfSource:      false,
						SystemGateway:   sysRoute.Gateway,
						SystemInterface: sysRoute.Interface,
					}
					result.Summary.Extra++
					result.Summary.Total++
					result.Routes = append(result.Routes, entry)
				}
			}
		}
	}

	return result
}

// getConfRoutes 从全局 routes map 获取 conf 中配置的路由
func getConfRoutes() map[string]bool {
	result := make(map[string]bool)
	for k, v := range routes {
		_ = v
		result[k] = true
	}
	return result
}

// getSystemRoutes 通过 netstat -rn 获取系统路由表
func getSystemRoutes() []SystemRoute {
	out, err := exec.Command("netstat", "-rn").Output()
	if err != nil {
		logger.Warningf("[DASHBOARD] Failed to get system routes: %v", err)
		return nil
	}

	var sysRoutes []SystemRoute
	lines := strings.Split(string(out), "\n")
	// 跳过表头
	inIPv4Section := false

	for _, line := range lines {
		if strings.Contains(line, "Internet:") {
			inIPv4Section = true
			continue
		}
		if strings.Contains(line, "Internet6:") {
			inIPv4Section = false
			continue
		}
		if !inIPv4Section {
			continue
		}

		// 解析路由条目
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		// 跳过表头行
		if fields[0] == "Destination" {
			continue
		}

		route := SystemRoute{
			Destination: normalizeNetstatDest(fields[0]),
			Gateway:     fields[1],
			Flags:       fields[2],
		}

		// 接口在不同 macOS 版本可能在不同列
		for _, f := range fields[3:] {
			if strings.HasPrefix(f, "en") || strings.HasPrefix(f, "utun") ||
				strings.HasPrefix(f, "lo") || strings.HasPrefix(f, "bridge") ||
				strings.HasPrefix(f, "gif") || strings.HasPrefix(f, "awdl") {
				route.Interface = f
				break
			}
		}

		if route.Destination != "" {
			sysRoutes = append(sysRoutes, route)
		}
	}

	return sysRoutes
}

// normalizeNetstatDest 将 netstat 的目标地址格式标准化为 CIDR 格式
func normalizeNetstatDest(dest string) string {
	// netstat 的格式可能是：
	// - "10.96/16" (缩写+显式掩码)
	// - "172.17"   (缩写，无掩码 → 根据八位组数推断: 2个=>/16)
	// - "10"       (缩写，1个八位组 => /8)
	// - "192.168.1" (缩写，3个八位组 => /24)
	// - "192.168.49.2" (完整4八位组，host 路由)
	// - "192.168.49.2/32" (显式 host 路由)
	// - "default" / "link#XX" (特殊路由)

	if dest == "default" || strings.HasPrefix(dest, "link#") {
		return dest
	}

	// 已经有 / 的，尝试扩展缩写 IP 部分
	if strings.Contains(dest, "/") {
		parts := strings.Split(dest, "/")
		ip := expandShortIP(parts[0])
		return ip + "/" + parts[1]
	}

	// 无 / 的情况：根据缩写的八位组数推断掩码
	// macOS netstat 省略尾部 .0 八位组：
	//   "172.17"     = 2 个八位组 → 172.17.0.0/16
	//   "10"         = 1 个八位组 → 10.0.0.0/8
	//   "192.168.1"  = 3 个八位组 → 192.168.1.0/24
	//   "192.168.1.1" = 4 个八位组 → host 路由 192.168.1.1（/32）
	parts := strings.Split(dest, ".")
	numParts := len(parts)

	if numParts < 4 {
		// 缩写形式，推断为网络路由，掩码 = 八位组数 * 8
		expanded := expandShortIP(dest)
		mask := numParts * 8
		return fmt.Sprintf("%s/%d", expanded, mask)
	}

	// 完整的 4 八位组，host 路由
	return dest
}

// expandShortIP 将缩写的 IP 地址扩展为完整形式
// 例如 "90.7" -> "90.7.0.0"
func expandShortIP(ip string) string {
	parts := strings.Split(ip, ".")
	for len(parts) < 4 {
		parts = append(parts, "0")
	}
	return strings.Join(parts, ".")
}

// findSystemRoute 在系统路由表中查找匹配的路由
func findSystemRoute(sysRoutes []SystemRoute, network string, peerIP string) (SystemRoute, bool) {
	// 标准化 conf 中的网段格式用于比较
	confNorm := normalizeNetworkForCompare(network)

	for _, sr := range sysRoutes {
		sysNorm := normalizeNetworkForCompare(sr.Destination)

		if confNorm == sysNorm {
			return sr, true
		}
	}

	return SystemRoute{}, false
}

// normalizeNetworkForCompare 标准化网络地址用于比较
func normalizeNetworkForCompare(network string) string {
	// 尝试解析为 CIDR
	if strings.Contains(network, "/") {
		_, ipNet, err := net.ParseCIDR(network)
		if err == nil {
			return ipNet.String()
		}
	}

	// 可能是 host 路由
	ip := net.ParseIP(network)
	if ip != nil {
		return ip.String() + "/32"
	}

	return network
}

// ==================== 路由修复 ====================

// fixMissingRoutes 修复所有缺失的路由
func fixMissingRoutes() FixResult {
	result := FixResult{}

	if peer == nil {
		result.Details = append(result.Details, "错误: peer IP 未设置")
		return result
	}

	verification := verifyRoutes()
	peerStr := peer.String()

	for _, route := range verification.Routes {
		if route.Status != RouteMissing {
			continue
		}

		// 使用 runOutCmd 捕获命令输出（包括 stderr），便于排查问题
		cmdStr := fmt.Sprintf("route -n add -net %s %s", route.Network, peerStr)
		out, err := runOutCmd("route -n add -net %s %s", route.Network, peerStr)
		outTrimmed := strings.TrimSpace(out)

		if err != nil {
			result.Failed++
			detail := fmt.Sprintf("❌ %s: %v", route.Network, err)
			if outTrimmed != "" {
				detail += fmt.Sprintf(" (%s)", outTrimmed)
			}
			result.Details = append(result.Details, detail)
			logger.Warningf("[DASHBOARD] route add failed: %s => err=%v, output=%s", cmdStr, err, outTrimmed)
		} else {
			result.Fixed++
			result.Details = append(result.Details, fmt.Sprintf("✅ %s via %s", route.Network, peerStr))
			logger.Infof("[DASHBOARD] route add ok: %s => %s", cmdStr, outTrimmed)
		}
	}

	// 修复完成后，验证路由是否真正生效
	if result.Fixed > 0 {
		time.Sleep(200 * time.Millisecond) // 等待路由表更新
		postVerify := verifyRoutes()
		stillMissing := 0
		for _, route := range postVerify.Routes {
			if route.Status == RouteMissing {
				stillMissing++
			}
		}
		if stillMissing > 0 {
			// 修正计数：route 命令返回成功但路由未真正生效
			actualFixed := result.Fixed - stillMissing
			if actualFixed < 0 {
				actualFixed = 0
			}
			result.Details = append(result.Details,
				fmt.Sprintf("\n⚠️ 验证发现 %d 条路由未生效（命令执行成功但路由表中未找到）", stillMissing))
			logger.Warningf("[DASHBOARD] post-fix verify: %d routes still missing after fix", stillMissing)
			result.Failed += (result.Fixed - actualFixed)
			result.Fixed = actualFixed
		}
	}

	return result
}

// ==================== Config API Handlers ====================

// handleAPIConfig 获取结构化配置
func handleAPIConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg, err := parseConfigToJSON()
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "message": err.Error()})
		return
	}
	writeJSON(w, cfg)
}

// handleAPIConfigRaw 获取或覆盖原始配置文件
func handleAPIConfigRaw(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		content, err := readConfigRaw()
		if err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]interface{}{"content": content})
	case "PUT":
		var req struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "message": "无效的请求体"})
			return
		}
		if err := writeConfigRaw(req.Content); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "message": "配置文件已更新，热加载将在 2 秒内生效"})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAPIConfigRoute 添加或删除路由
func handleAPIConfigRoute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Network string `json:"network"`
		Expose  bool   `json:"expose"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "message": "无效的请求体"})
		return
	}

	// 验证 network 非空
	req.Network = strings.TrimSpace(req.Network)
	if req.Network == "" {
		writeJSON(w, map[string]interface{}{"ok": false, "message": "网络地址不能为空"})
		return
	}

	switch r.Method {
	case "POST":
		// 添加路由时严格验证 CIDR 格式
		if _, _, err := net.ParseCIDR(req.Network); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "message": fmt.Sprintf("无效的 CIDR 格式: %s", req.Network)})
			return
		}
		// 检查是否已存在
		if _, exists := routes[req.Network]; exists {
			writeJSON(w, map[string]interface{}{"ok": false, "message": fmt.Sprintf("路由 %s 已存在", req.Network)})
			return
		}
		line := "route " + req.Network
		if req.Expose {
			line += " expose"
		}
		if err := addConfigLine(line); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "message": fmt.Sprintf("路由 %s 已添加，热加载将在 2 秒内生效", req.Network)})
	case "DELETE":
		if err := removeConfigLine(func(key, value string) bool {
			if key != "route" {
				return false
			}
			vals := strings.Fields(value)
			return len(vals) > 0 && vals[0] == req.Network
		}); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "message": fmt.Sprintf("路由 %s 已删除，热加载将在 2 秒内生效", req.Network)})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAPIConfigIptables 添加或删除 iptables 互通规则
func handleAPIConfigIptables(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SubnetA string `json:"subnet_a"`
		SubnetB string `json:"subnet_b"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "message": "无效的请求体"})
		return
	}
	if req.SubnetA == "" || req.SubnetB == "" {
		writeJSON(w, map[string]interface{}{"ok": false, "message": "subnet_a 和 subnet_b 不能为空"})
		return
	}

	switch r.Method {
	case "POST":
		line := fmt.Sprintf("iptables %s+%s", req.SubnetA, req.SubnetB)
		if err := addConfigLine(line); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "message": "iptables 规则已添加"})
	case "DELETE":
		if err := removeConfigLine(func(key, value string) bool {
			if key != "iptables" {
				return false
			}
			// 匹配 a+b 或 b+a 或 a-b 或 b-a
			return value == req.SubnetA+"+"+req.SubnetB ||
				value == req.SubnetB+"+"+req.SubnetA ||
				value == req.SubnetA+"-"+req.SubnetB ||
				value == req.SubnetB+"-"+req.SubnetA
		}); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "message": "iptables 规则已删除"})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAPIConfigExpose 更新 expose 配置
func handleAPIConfigExpose(w http.ResponseWriter, r *http.Request) {
	if r.Method != "PUT" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Address string `json:"address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "message": "无效的请求体"})
		return
	}

	// 先尝试删除旧的 expose 行
	_ = removeConfigLine(func(key, value string) bool { return key == "expose" })

	if req.Address != "" {
		if err := addConfigLine("expose " + req.Address); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "message": err.Error()})
			return
		}
	}
	writeJSON(w, map[string]interface{}{"ok": true, "message": "expose 配置已更新"})
}

// handleAPIConfigToken 添加或删除 token
func handleAPIConfigToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		IP   string `json:"ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "message": "无效的请求体"})
		return
	}

	switch r.Method {
	case "POST":
		if req.Name == "" || req.IP == "" {
			writeJSON(w, map[string]interface{}{"ok": false, "message": "name 和 ip 不能为空"})
			return
		}
		line := fmt.Sprintf("token %s %s", req.Name, req.IP)
		if err := addConfigLine(line); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "message": fmt.Sprintf("token %s 已添加", req.Name)})
	case "DELETE":
		if req.Name == "" {
			writeJSON(w, map[string]interface{}{"ok": false, "message": "name 不能为空"})
			return
		}
		if err := removeConfigLine(func(key, value string) bool {
			if key != "token" {
				return false
			}
			vals := strings.Fields(value)
			return len(vals) > 0 && vals[0] == req.Name
		}); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "message": fmt.Sprintf("token %s 已删除", req.Name)})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAPIConfigHosts 更新 hosts 配置
func handleAPIConfigHosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != "PUT" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "message": "无效的请求体"})
		return
	}

	// 先删除旧的 hosts 行
	_ = removeConfigLine(func(key, value string) bool { return key == "hosts" })

	if req.Value != "" {
		if err := addConfigLine("hosts " + req.Value); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "message": err.Error()})
			return
		}
	}
	writeJSON(w, map[string]interface{}{"ok": true, "message": "hosts 配置已更新"})
}

// handleAPIConfigProxy 添加或删除 proxy
func handleAPIConfigProxy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Rule string `json:"rule"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "message": "无效的请求体"})
		return
	}
	if req.Rule == "" {
		writeJSON(w, map[string]interface{}{"ok": false, "message": "rule 不能为空"})
		return
	}

	switch r.Method {
	case "POST":
		if err := addConfigLine("proxy " + req.Rule); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "message": fmt.Sprintf("proxy %s 已添加", req.Rule)})
	case "DELETE":
		if err := removeConfigLine(func(key, value string) bool {
			return key == "proxy" && strings.TrimSpace(value) == req.Rule
		}); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "message": fmt.Sprintf("proxy %s 已删除", req.Rule)})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAPIConfigBasic 更新基础配置（loglevel、pong 等可热加载项）
func handleAPIConfigBasic(w http.ResponseWriter, r *http.Request) {
	if r.Method != "PUT" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		LogLevel *string `json:"loglevel"`
		Pong     *bool   `json:"pong"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "message": "无效的请求体"})
		return
	}

	changes := []string{}

	if req.LogLevel != nil {
		// 验证日志级别
		if _, err := logging.LogLevel(*req.LogLevel); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "message": fmt.Sprintf("无效的日志级别: %s", *req.LogLevel)})
			return
		}
		_ = removeConfigLine(func(key, value string) bool { return key == "loglevel" })
		if err := addConfigLine("loglevel " + *req.LogLevel); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "message": err.Error()})
			return
		}
		changes = append(changes, "loglevel="+*req.LogLevel)
	}

	if req.Pong != nil {
		_ = removeConfigLine(func(key, value string) bool { return key == "pong" })
		val := "off"
		if *req.Pong {
			val = "on"
		}
		if err := addConfigLine("pong " + val); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "message": err.Error()})
			return
		}
		changes = append(changes, fmt.Sprintf("pong=%s", val))
	}

	writeJSON(w, map[string]interface{}{
		"ok":      true,
		"message": fmt.Sprintf("配置已更新: %s", strings.Join(changes, ", ")),
	})
}

// handleAPIConfigDiscover 自动发现 Docker 子网
func handleAPIConfigDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	subnets, err := discoverDockerSubnets()
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "message": err.Error(), "networks": []DockerSubnet{}})
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "networks": subnets})
}

// ==================== 辅助工具函数 ====================

// 用于解析 netstat 输出中的 CIDR 掩码
var cidrRegex = regexp.MustCompile(`^(\d+\.[\d.]*)/(\d+)$`)

// extractIP 从 CIDR 或 host 路由中提取纯 IP 部分（去掉掩码）
// 例如 "172.17.0.0/16" → "172.17.0.0"，"172.17.0.0" → "172.17.0.0"
func extractIP(network string) string {
	if strings.Contains(network, "/") {
		parts := strings.SplitN(network, "/", 2)
		ip := net.ParseIP(parts[0])
		if ip != nil {
			return ip.String()
		}
		return parts[0]
	}
	ip := net.ParseIP(network)
	if ip != nil {
		return ip.String()
	}
	return network
}

// ==================== VM 反向代理 ====================

// VM 可达状态管理
var (
	vmReachable     bool
	vmReachableMu   sync.RWMutex
	vmLastCheckTime time.Time
)

// registerVMProxy 注册 VM 反向代理路由
func registerVMProxy(mux *http.ServeMux) {
	// VM 连接状态端点（本地处理，不代理）
	mux.HandleFunc("/api/vm/status", handleVMStatus)

	// 反向代理：/api/vm/* → http://peerIP:2522/api/*
	vmProxy := createVMReverseProxy()
	if vmProxy != nil {
		mux.Handle("/api/vm/", vmProxy)
		logger.Infof("[VM-PROXY] 反向代理已注册: /api/vm/* → http://%s:%d/api/*", peer.String(), vmHTTPPort)
	} else {
		// peer 还未初始化时，返回等待信息
		mux.HandleFunc("/api/vm/", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]interface{}{
				"error":   true,
				"message": "VM 端未连接：peer IP 尚未初始化",
			})
		})
		logger.Warningf("[VM-PROXY] peer IP 未初始化，VM 反向代理暂不可用")
	}

	// 启动 VM 健康检测循环
	go vmHealthCheckLoop()
}

// createVMReverseProxy 创建 VM 反向代理实例
func createVMReverseProxy() http.Handler {
	if peer == nil {
		return nil
	}

	vmTarget := fmt.Sprintf("http://%s:%d", peer.String(), vmHTTPPort)
	targetURL, err := url.Parse(vmTarget)
	if err != nil {
		logger.Warningf("[VM-PROXY] 无效的 VM 目标地址: %s, 错误: %v", vmTarget, err)
		return nil
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = targetURL.Scheme
			req.URL.Host = targetURL.Host
			// /api/vm/links → /api/links（替换前缀 /api/vm → /api）
			req.URL.Path = strings.Replace(req.URL.Path, "/api/vm", "/api", 1)
			if req.URL.Path == "" {
				req.URL.Path = "/"
			}
			req.Host = targetURL.Host

			logger.Debugf("[VM-PROXY] 代理请求: %s → %s%s", req.Method, targetURL.Host, req.URL.Path)
		},
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   3 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          10,
			IdleConnTimeout:       60 * time.Second,
			ResponseHeaderTimeout: 5 * time.Second,
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			logger.Warningf("[VM-PROXY] 代理错误: %s %s → %v", r.Method, r.URL.Path, err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   true,
				"message": fmt.Sprintf("VM 端不可达: %v", err),
				"hint":    "请确认 VM 端 docker-connector 以 -mode=service 运行",
			})
		},
		// SSE 流需要 FlushInterval
		FlushInterval: 200 * time.Millisecond,
	}

	return proxy
}

// handleVMStatus 返回 VM 连接状态（不经过代理，本地判断）
func handleVMStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	vmReachableMu.RLock()
	reachable := vmReachable
	lastCheck := vmLastCheckTime
	vmReachableMu.RUnlock()

	peerStr := ""
	if peer != nil {
		peerStr = peer.String()
	}

	writeJSON(w, map[string]interface{}{
		"vm_reachable": reachable,
		"vm_peer_ip":   peerStr,
		"vm_http_port": vmHTTPPort,
		"vm_api_url":   fmt.Sprintf("http://%s:%d", peerStr, vmHTTPPort),
		"last_check":   lastCheck.Format(time.RFC3339),
		"time":         time.Now().Format(time.RFC3339),
	})
}

// vmHealthCheckLoop 定期检测 VM HTTP 服务是否可达
func vmHealthCheckLoop() {
	// 启动时等待 3 秒再首次检测（等待隧道建立）
	time.Sleep(3 * time.Second)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// 首次立即检测
	checkVMHealth()

	for range ticker.C {
		checkVMHealth()
	}
}

// checkVMHealth 单次检测 VM HTTP 服务
func checkVMHealth() {
	if peer == nil {
		vmReachableMu.Lock()
		vmReachable = false
		vmLastCheckTime = time.Now()
		vmReachableMu.Unlock()
		return
	}

	vmURL := fmt.Sprintf("http://%s:%d/api/health", peer.String(), vmHTTPPort)
	client := &http.Client{Timeout: 2 * time.Second}

	resp, err := client.Get(vmURL)
	reachable := err == nil && resp != nil && resp.StatusCode == http.StatusOK
	if resp != nil {
		resp.Body.Close()
	}

	vmReachableMu.Lock()
	oldReachable := vmReachable
	vmReachable = reachable
	vmLastCheckTime = time.Now()
	vmReachableMu.Unlock()

	// 状态变化时记录日志
	if reachable != oldReachable {
		if reachable {
			logger.Infof("[VM-PROXY] ✅ VM HTTP 服务已连接: %s", vmURL)
		} else {
			logger.Warningf("[VM-PROXY] ❌ VM HTTP 服务不可达: %s (err: %v)", vmURL, err)
		}
	}
}
