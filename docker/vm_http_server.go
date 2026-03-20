package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// VMHTTPServer VM 端内嵌 HTTP 服务
// 绑定 local IP:2522，仅通过 TUN 隧道内网可达
type VMHTTPServer struct {
	linkMgr  *LinkManager
	localIP  string
	port     int
	server   *http.Server

	// SSE 客户端管理
	sseMu    sync.Mutex
	sseChans []chan []byte
}

// NewVMHTTPServer 创建 VM HTTP 服务
func NewVMHTTPServer(linkMgr *LinkManager, localIP string, port int) *VMHTTPServer {
	return &VMHTTPServer{
		linkMgr: linkMgr,
		localIP: localIP,
		port:    port,
	}
}

// Start 启动 HTTP 服务（非阻塞，在 goroutine 中运行）
func (s *VMHTTPServer) Start() error {
	mux := http.NewServeMux()

	// API 路由注册
	mux.HandleFunc("/api/links", s.handleLinks)
	mux.HandleFunc("/api/links/stream", s.handleLinksStream)
	mux.HandleFunc("/api/apply", s.handleApply)
	mux.HandleFunc("/api/revert", s.handleRevert)
	mux.HandleFunc("/api/network/info", s.handleNetworkInfo)
	mux.HandleFunc("/api/health", s.handleHealth)

	listenAddr := fmt.Sprintf("%s:%d", s.localIP, s.port)
	s.server = &http.Server{
		Addr:         listenAddr,
		Handler:      s.corsMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 0, // SSE 需要长连接，不设写超时
		IdleTimeout:  120 * time.Second,
	}

	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("无法监听 %s: %v", listenAddr, err)
	}

	go func() {
		fmt.Printf("[VM-HTTP] ✅ HTTP API 已启动: http://%s\n", listenAddr)
		if err := s.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			fmt.Printf("[VM-HTTP] ❌ HTTP 服务异常: %v\n", err)
		}
	}()

	// 启动 SSE 广播循环
	go s.sseBroadcastLoop()

	return nil
}

// corsMiddleware 跨域中间件（Dashboard 反向代理可能需要）
func (s *VMHTTPServer) corsMiddleware(next http.Handler) http.Handler {
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

// ==================== API 处理函数 ====================

// handleHealth 健康检查端点
func (s *VMHTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"version": "2.3",
		"mode":    "service",
		"time":    time.Now().Format(time.RFC3339),
	})
}

// handleLinks 获取所有链路状态
// GET /api/links
func (s *VMHTTPServer) handleLinks(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		s.writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
		return
	}

	statuses := s.linkMgr.StatusAll()
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"links": statuses,
		"names": s.linkMgr.AllLinkNames(),
		"time":  time.Now().Format(time.RFC3339),
	})
}

// linkActionRequest apply/revert 请求体
type linkActionRequest struct {
	Link string `json:"link"` // 链路名称，如 "internet" 或 "host-k8s.service"
}

// handleApply 应用指定链路
// POST /api/apply {"link":"internet"}
func (s *VMHTTPServer) handleApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		s.writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}

	var req linkActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "无效的请求体: "+err.Error())
		return
	}

	linkName, subLevel := ParseLinkSpec(req.Link)
	link := s.linkMgr.GetLink(linkName)
	if link == nil {
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("未知的链路: %s", linkName))
		return
	}

	fmt.Printf("[VM-HTTP] 应用链路: %s\n", req.Link)
	err := link.Apply(subLevel)
	if err != nil {
		s.writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":      false,
			"message": err.Error(),
			"link":    req.Link,
		})
		return
	}

	// 刷新状态
	statuses := link.Status(subLevel)
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":       true,
		"link":     req.Link,
		"statuses": statuses,
	})

	// 通知 SSE 客户端
	s.notifySSEClients()
}

// handleRevert 还原指定链路
// POST /api/revert {"link":"internet"}
func (s *VMHTTPServer) handleRevert(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		s.writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}

	var req linkActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "无效的请求体: "+err.Error())
		return
	}

	linkName, subLevel := ParseLinkSpec(req.Link)
	link := s.linkMgr.GetLink(linkName)
	if link == nil {
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("未知的链路: %s", linkName))
		return
	}

	fmt.Printf("[VM-HTTP] 还原链路: %s\n", req.Link)
	err := link.Revert(subLevel)
	if err != nil {
		s.writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":      false,
			"message": err.Error(),
			"link":    req.Link,
		})
		return
	}

	statuses := link.Status(subLevel)
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":       true,
		"link":     req.Link,
		"statuses": statuses,
	})

	// 通知 SSE 客户端
	s.notifySSEClients()
}

// handleNetworkInfo 获取网络信息
// GET /api/network/info
func (s *VMHTTPServer) handleNetworkInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		s.writeError(w, http.StatusMethodNotAllowed, "仅支持 GET 方法")
		return
	}

	bridges := s.linkMgr.Bridges()
	mkInfo := s.linkMgr.MinikubeInfo()
	physIf := s.linkMgr.PhysicalInterface()
	tun0Exists := s.linkMgr.network.InterfaceExists("tun0")

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"physical_interface": physIf,
		"tun0_exists":       tun0Exists,
		"bridges":           bridges,
		"minikube":          mkInfo,
		"time":              time.Now().Format(time.RFC3339),
	})
}

// ==================== SSE 实时推送 ====================

// handleLinksStream SSE 实时推送链路状态
// GET /api/links/stream
func (s *VMHTTPServer) handleLinksStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// 注册 SSE 客户端通道
	ch := make(chan []byte, 8)
	s.sseMu.Lock()
	s.sseChans = append(s.sseChans, ch)
	s.sseMu.Unlock()

	// 客户端断开时清理
	defer func() {
		s.sseMu.Lock()
		for i, c := range s.sseChans {
			if c == ch {
				s.sseChans = append(s.sseChans[:i], s.sseChans[i+1:]...)
				break
			}
		}
		s.sseMu.Unlock()
		close(ch)
	}()

	// 立即发送一次当前状态
	s.sendSSEStatus(w, flusher)

	// 监听推送通道和客户端断开
	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			fmt.Println("[VM-HTTP] SSE 客户端已断开")
			return
		case data, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// sendSSEStatus 发送一次完整的链路状态到 SSE 流
func (s *VMHTTPServer) sendSSEStatus(w http.ResponseWriter, flusher http.Flusher) {
	statuses := s.linkMgr.StatusAll()
	payload := map[string]interface{}{
		"links": statuses,
		"time":  time.Now().Format(time.RFC3339),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

// sseBroadcastLoop 定时广播链路状态（每 10s）
func (s *VMHTTPServer) sseBroadcastLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		s.sseMu.Lock()
		clientCount := len(s.sseChans)
		s.sseMu.Unlock()

		if clientCount == 0 {
			continue // 无客户端，跳过
		}

		statuses := s.linkMgr.StatusAll()
		payload := map[string]interface{}{
			"links": statuses,
			"time":  time.Now().Format(time.RFC3339),
		}
		data, err := json.Marshal(payload)
		if err != nil {
			continue
		}

		s.broadcastSSE(data)
	}
}

// notifySSEClients 通知所有 SSE 客户端（apply/revert 后调用）
func (s *VMHTTPServer) notifySSEClients() {
	s.sseMu.Lock()
	clientCount := len(s.sseChans)
	s.sseMu.Unlock()

	if clientCount == 0 {
		return
	}

	statuses := s.linkMgr.StatusAll()
	payload := map[string]interface{}{
		"links": statuses,
		"time":  time.Now().Format(time.RFC3339),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	s.broadcastSSE(data)
}

// broadcastSSE 向所有 SSE 客户端广播数据
func (s *VMHTTPServer) broadcastSSE(data []byte) {
	s.sseMu.Lock()
	defer s.sseMu.Unlock()

	// 使用非阻塞写入，避免慢客户端阻塞广播
	var activeChs []chan []byte
	for _, ch := range s.sseChans {
		select {
		case ch <- data:
			activeChs = append(activeChs, ch)
		default:
			// 通道已满，客户端可能已断开，跳过
			if debug {
				fmt.Println("[VM-HTTP] SSE 客户端通道已满，跳过")
			}
			activeChs = append(activeChs, ch)
		}
	}
	s.sseChans = activeChs
}

// ==================== 工具方法 ====================

// writeJSON 写入 JSON 响应
func (s *VMHTTPServer) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(data)
}

// writeError 写入错误响应
func (s *VMHTTPServer) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, map[string]interface{}{
		"error":   true,
		"message": message,
	})
}

// ==================== peerIP 提取工具 ====================

// extractLocalIP 从 addr 参数（如 "192.168.252.1/24"）提取 local IP
// local IP 就是 addr 中 CIDR 斜杠前的 IP，即 VM 本机 TUN 地址
func extractLocalIP(addrCIDR string) string {
	parts := strings.Split(addrCIDR, "/")
	if len(parts) == 0 {
		return ""
	}
	ip := net.ParseIP(parts[0])
	if ip == nil {
		return ""
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return ""
	}
	return ip4.String()
}
