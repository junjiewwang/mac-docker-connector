package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
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
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
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
	if result.TUNInterface != "" {
		for _, sysRoute := range sysRoutes {
			if sysRoute.Interface == result.TUNInterface {
				if _, ok := confRoutes[sysRoute.Destination]; !ok {
					// 排除 host 路由 (peer 自身的路由) 和 link-local 路由
					if sysRoute.Destination == peerStr || strings.HasPrefix(sysRoute.Destination, "169.254") {
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
	// - "10.244.0.0/16" (已是 CIDR)
	// - "90.7/16" (缩写形式)
	// - "10.244.0.0" (host 路由)
	// - "default" (默认路由)
	// - "link#XX" (链路路由)

	if dest == "default" || strings.HasPrefix(dest, "link#") {
		return dest
	}

	// 已经有 / 的，尝试扩展缩写
	if strings.Contains(dest, "/") {
		parts := strings.Split(dest, "/")
		ip := expandShortIP(parts[0])
		return ip + "/" + parts[1]
	}

	// 无 / 的是 host 路由
	return expandShortIP(dest)
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

		// 执行 route add
		err := runCmd("route -n add -net %s %s", route.Network, peerStr)
		if err != nil {
			result.Failed++
			result.Details = append(result.Details, fmt.Sprintf("❌ %s: %v", route.Network, err))
		} else {
			result.Fixed++
			result.Details = append(result.Details, fmt.Sprintf("✅ %s via %s", route.Network, peerStr))
		}
	}

	return result
}

// ==================== 辅助工具函数 ====================

// 用于解析 netstat 输出中的 CIDR 掩码
var cidrRegex = regexp.MustCompile(`^(\d+\.[\d.]*)/(\d+)$`)
