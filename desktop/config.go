package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/op/go-logging"
	"github.com/songgao/water"
)

func normalizeAddr(addr string) string {
	if strings.Index(addr, "[::]") == 0 {
		return strings.Replace(addr, "[::]", "0.0.0.0", 1)
	}
	return addr
}

func isSameAddr(addr0, addr1 string) bool {
	if addr0 == addr1 {
		return true
	}
	return normalizeAddr(addr0) == normalizeAddr(addr1)
}

func loadConfig(iface *water.Interface, init bool) *water.Interface {
	fi, err := os.Open(configFile)
	if err != nil {
		logger.Error("load config failed", err)
		return iface
	}
	defer fi.Close()
	re := regexp.MustCompile(`^\s*(\w+\S+)(?:\s+(.*))?$`)
	news := make(map[string]bool)
	news1 := make(map[string]string)
	iptables1 := make(map[string]bool)
	if proxyServer != nil {
		proxyServer.StartClear()
	}
	br := bufio.NewReader(fi)
	for {
		a, _, c := br.ReadLine()
		if c == io.EOF {
			break
		}
		s := strings.TrimSpace(string(a))
		match := re.FindStringSubmatch(s)
		if match != nil {
			val := match[2]
			switch match[1] {
			case "loglevel":
				if level, err := logging.LogLevel(val); err == nil {
					logging.SetLevel(level, "vpn")
					if leveledBackend != nil {
						leveledBackend.SetLevel(level, "vpn")
					}
				}
			case "route":
				vals := strings.Split(val, " ")
				if len(vals) > 1 {
					news[vals[0]] = vals[1] == "expose"
				} else {
					news[vals[0]] = false
				}
			case "host":
				host = val
			case "addr":
				addr = val
			case "port":
				if v, err := strconv.Atoi(val); err == nil {
					port = v
				}
			case "vm-http-port":
				if v, err := strconv.Atoi(val); err == nil {
					vmHTTPPort = v
				}
			case "mtu":
				if v, err := strconv.Atoi(val); err == nil {
					MTU = v
				}
			case "pong":
				pong = val == "on" || val == "true"
			case "expose":
				restart := strings.Contains(val, "restart")
				val = strings.Fields(val)[0]
				if udpAddr, err := net.ResolveUDPAddr("udp", val); err == nil {
					if expose != nil && (restart || !isSameAddr(expose.LocalAddr().String(), val)) {
						logger.Infof("expose changed: %s => %s\n", expose.LocalAddr().String(), val)
						expose.Close()
						expose = nil
					}
					if expose == nil {
						if expose, err = net.ListenUDP("udp", udpAddr); err != nil {
							logger.Warningf("failed to listen => %s\n", val)
						} else {
							go handleExpose()
						}
					}
				} else {
					logger.Warningf("invalid address => %s\n", val)
				}
			case "token":
				vals := strings.Split(val, " ")
				news1[vals[0]] = vals[1]
			case "iptables":
				vals := strings.Split(val, "+")
				join := true
				if len(vals) == 1 {
					vals = strings.Split(val, "-")
					join = false
				}
				val = fmt.Sprintf("%s %s", vals[0], vals[1])
				if vals[0] > vals[1] {
					val = fmt.Sprintf("%s %s", vals[1], vals[0])
				}
				iptables1[val] = join
			case "hosts":
				hosts = val
			case "proxy":
				GetProxyServer().Add(val)
			default:
				logger.Warningf("unknown action => %s\n", match[1])
			}
		} else if s != "" && !strings.HasPrefix(s, "#") {
			logger.Warningf("invalid config => %s\n", s)
		}
	}
	if init {
		if peer, subnet, err = net.ParseCIDR(addr); err != nil {
			logger.Fatal(err)
		}
		copy([]byte(localIP), []byte(peer.To4()))
		localIP[3]++
		if bind {
			iface = setup(localIP, peer, subnet)
		}
		for k, v := range iptables1 {
			iptables[k] = v
		}
	}
	if proxyServer != nil {
		proxyServer.EndClear()
		proxyServer.Start(localIP)
	}
	logger.Debugf("routes %s => %s\n", map2json(routes), map2json(news))
	var delKeys []string
	for key := range routes {
		if val, ok := news[key]; ok {
			routes[key] = val
			delete(news, key)
		} else {
			// 路由已从配置文件中删除，收集待清理的 key
			delKeys = append(delKeys, key)
			if bind {
				delRoute(key)
			}
		}
	}
	// 循环外统一从内存 map 中移除已删除的路由
	for _, key := range delKeys {
		delete(routes, key)
	}
	for key := range news {
		routes[key] = news[key]
		if bind {
			delRoute(key)
			addRoute(key, peer)
		}
	}
	for key := range tokens {
		if v, ok := news1[key]; ok {
			tokens[key] = v
		} else {
			delete(tokens, key)
		}
	}
	for key := range news1 {
		tokens[key] = news1[key]
	}
	for key := range iptables {
		if v, ok := iptables1[key]; ok {
			if iptables[key] == v {
				delete(iptables1, key)
			} else {
				iptables[key] = v
			}
		} else {
			delete(iptables, key)
			iptables1[key] = false
		}
	}
	if cli != nil {
		sendControls(cli, iptables1, hosts)
	}
	news = nil
	news1 = nil
	iptables1 = nil
	return iface
}

func map2json(m interface{}) string {
	if b, err := json.Marshal(m); err == nil {
		return string(b)
	}
	return ""
}

func appendConfig(data []byte) {
	fd, err := os.OpenFile(configFile, os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	fd.WriteString("\n")
	fd.Write(data)
	fd.Close()
}

func sendConfig() {
	logger.Infof("send config to => %s:%d\n", host, port)
	udpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return
	}
	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return
	}
	defer conn.Close()
	var data bytes.Buffer
	if len(os.Args) > 2 {
		data.WriteByte(1)
		data.WriteString(strings.Join(os.Args[2:], " "))
		conn.Write(data.Bytes())
	} else {
		reader := bufio.NewReader(os.Stdin)
		for {
			line, hasMore, err := reader.ReadLine()
			if err != nil {
				break
			}
			data.Reset()
			data.WriteByte(1)
			data.Write(line)
			conn.Write(data.Bytes())
			if !hasMore {
				break
			}
		}
	}
}

func clearRoutes() {
	for key := range routes {
		delRoute(key)
	}
}

// ==================== 配置管理工具函数 ====================

// ConfigJSON 结构化配置数据模型
type ConfigJSON struct {
	Basic    BasicConfig    `json:"basic"`
	Routes   []RouteConfig  `json:"routes"`
	Iptables []IptablesConfig `json:"iptables"`
	Expose   string         `json:"expose"`
	Tokens   []TokenConfig  `json:"tokens"`
	Hosts    string         `json:"hosts"`
	Proxies  []string       `json:"proxies"`
	ConfigFile string       `json:"config_file"`
	Watch    bool           `json:"watch"`
}

type BasicConfig struct {
	Addr       string `json:"addr"`
	Port       int    `json:"port"`
	MTU        int    `json:"mtu"`
	Host       string `json:"host"`
	LogLevel   string `json:"loglevel"`
	Pong       bool   `json:"pong"`
	VmHTTPPort int    `json:"vm_http_port"`
}

type RouteConfig struct {
	Network string `json:"network"`
	Expose  bool   `json:"expose"`
}

type IptablesConfig struct {
	SubnetA string `json:"subnet_a"`
	SubnetB string `json:"subnet_b"`
	Action  string `json:"action"` // "connect" 或 "disconnect"
}

type TokenConfig struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
}

// DockerSubnet 自动发现的 Docker 子网
type DockerSubnet struct {
	Network string `json:"network"`
	Name    string `json:"name"`
	Driver  string `json:"driver"`
	Added   bool   `json:"added"` // 是否已在配置中
}

// configMu 配置文件读写锁，防止并发写入
var configMu sync.Mutex

// parseConfigToJSON 解析配置文件为结构化 JSON 数据（只读，不执行副作用）
func parseConfigToJSON() (*ConfigJSON, error) {
	configMu.Lock()
	defer configMu.Unlock()

	cfg := &ConfigJSON{
		Basic: BasicConfig{
			Addr:       addr,
			Port:       port,
			MTU:        MTU,
			Host:       host,
			LogLevel:   logLevel,
			Pong:       pong,
			VmHTTPPort: vmHTTPPort,
		},
		ConfigFile: configFile,
		Watch:      watch,
	}

	if configFile == "" {
		// 从内存状态读取
		for k, v := range routes {
			cfg.Routes = append(cfg.Routes, RouteConfig{Network: k, Expose: v})
		}
		for k, v := range tokens {
			cfg.Tokens = append(cfg.Tokens, TokenConfig{Name: k, IP: v})
		}
		return cfg, nil
	}

	fi, err := os.Open(configFile)
	if err != nil {
		return cfg, err
	}
	defer fi.Close()

	re := regexp.MustCompile(`^\s*(\w+\S+)(?:\s+(.*))?$`)
	br := bufio.NewReader(fi)
	for {
		a, _, c := br.ReadLine()
		if c == io.EOF {
			break
		}
		s := strings.TrimSpace(string(a))
		match := re.FindStringSubmatch(s)
		if match == nil {
			continue
		}
		val := match[2]
		switch match[1] {
		case "route":
			vals := strings.Split(val, " ")
			rc := RouteConfig{Network: vals[0]}
			if len(vals) > 1 && vals[1] == "expose" {
				rc.Expose = true
			}
			cfg.Routes = append(cfg.Routes, rc)
		case "iptables":
			vals := strings.Split(val, "+")
			action := "connect"
			if len(vals) == 1 {
				vals = strings.Split(val, "-")
				action = "disconnect"
			}
			if len(vals) == 2 {
				cfg.Iptables = append(cfg.Iptables, IptablesConfig{
					SubnetA: vals[0], SubnetB: vals[1], Action: action,
				})
			}
		case "expose":
			cfg.Expose = strings.Fields(val)[0]
		case "token":
			vals := strings.Split(val, " ")
			if len(vals) >= 2 {
				cfg.Tokens = append(cfg.Tokens, TokenConfig{Name: vals[0], IP: vals[1]})
			}
		case "hosts":
			cfg.Hosts = val
		case "proxy":
			cfg.Proxies = append(cfg.Proxies, val)
		case "loglevel":
			cfg.Basic.LogLevel = val
		case "pong":
			cfg.Basic.Pong = (val == "on" || val == "true")
		case "addr":
			cfg.Basic.Addr = val
		case "port":
			if v, err := strconv.Atoi(val); err == nil {
				cfg.Basic.Port = v
			}
		case "mtu":
			if v, err := strconv.Atoi(val); err == nil {
				cfg.Basic.MTU = v
			}
		case "vm-http-port":
			if v, err := strconv.Atoi(val); err == nil {
				cfg.Basic.VmHTTPPort = v
			}
		case "host":
			cfg.Basic.Host = val
		}
	}

	return cfg, nil
}

// readConfigRaw 读取配置文件原始内容
func readConfigRaw() (string, error) {
	configMu.Lock()
	defer configMu.Unlock()

	if configFile == "" {
		return "", fmt.Errorf("配置文件路径未设置")
	}
	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// writeConfigRaw 覆盖写入配置文件（先备份）
func writeConfigRaw(content string) error {
	configMu.Lock()
	defer configMu.Unlock()

	if configFile == "" {
		return fmt.Errorf("配置文件路径未设置")
	}
	backupConfigFile()
	return ioutil.WriteFile(configFile, []byte(content), 0644)
}

// addConfigLine 追加一行到配置文件末尾
func addConfigLine(line string) error {
	configMu.Lock()
	defer configMu.Unlock()

	if configFile == "" {
		return fmt.Errorf("配置文件路径未设置")
	}
	backupConfigFile()

	fd, err := os.OpenFile(configFile, os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer fd.Close()
	// 确保新行前有换行符
	fd.WriteString("\n")
	fd.WriteString(line)
	return nil
}

// removeConfigLine 从配置文件中精确删除匹配的行（先备份）
// matcher 函数接收去除注释和空行后的 "key value" 部分，返回 true 表示该行需要删除
func removeConfigLine(matcher func(key, value string) bool) error {
	configMu.Lock()
	defer configMu.Unlock()

	if configFile == "" {
		return fmt.Errorf("配置文件路径未设置")
	}
	backupConfigFile()

	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		return err
	}

	re := regexp.MustCompile(`^\s*(\w+\S+)(?:\s+(.*))?$`)
	lines := strings.Split(string(data), "\n")
	var result []string
	removed := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		match := re.FindStringSubmatch(trimmed)
		if match != nil && matcher(match[1], match[2]) {
			removed = true
			continue // 跳过该行（即删除）
		}
		result = append(result, line)
	}

	if !removed {
		return fmt.Errorf("未找到匹配的配置行")
	}

	// 清理末尾多余空行
	for len(result) > 0 && strings.TrimSpace(result[len(result)-1]) == "" {
		result = result[:len(result)-1]
	}
	result = append(result, "") // 确保文件末尾有一个换行

	return ioutil.WriteFile(configFile, []byte(strings.Join(result, "\n")), 0644)
}

// backupConfigFile 备份配置文件到 .bak（无锁版本，调用方需持有 configMu）
func backupConfigFile() {
	if configFile == "" {
		return
	}
	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		return
	}
	_ = ioutil.WriteFile(configFile+".bak", data, 0644)
}

// discoverDockerSubnets 通过 docker CLI 自动发现 Docker bridge 子网
func discoverDockerSubnets() ([]DockerSubnet, error) {
	// 通过 VM 端 HTTP API 获取 Docker 子网信息
	if peer == nil {
		return nil, fmt.Errorf("VM 未连接：peer IP 尚未初始化")
	}

	vmURL := fmt.Sprintf("http://%s:%d/api/docker/subnets", peer.String(), vmHTTPPort)
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(vmURL)
	if err != nil {
		return nil, fmt.Errorf("请求 VM 端失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("VM 端返回错误状态: %d", resp.StatusCode)
	}

	// 解析 VM 端返回的 JSON
	var vmResp struct {
		OK      bool `json:"ok"`
		Subnets []struct {
			Network string `json:"network"`
			Name    string `json:"name"`
			Driver  string `json:"driver"`
		} `json:"subnets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&vmResp); err != nil {
		return nil, fmt.Errorf("解析 VM 响应失败: %v", err)
	}

	if !vmResp.OK {
		return nil, fmt.Errorf("VM 端返回失败")
	}

	var subnets []DockerSubnet
	for _, s := range vmResp.Subnets {
		// 验证 CIDR 格式合法性
		ip, ipNet, err := net.ParseCIDR(s.Network)
		if err != nil {
			logger.Warningf("自动发现: 跳过非法 CIDR %q (%s)", s.Network, s.Name)
			continue
		}
		// 只保留 IPv4 子网
		if ip.To4() == nil {
			continue
		}
		// 过滤不可路由的特殊网段（240.0.0.0/4 保留, 127.0.0.0/8 回环, 169.254.0.0/16 链路本地, 0.0.0.0/8）
		firstOctet := ip.To4()[0]
		if firstOctet >= 240 || firstOctet == 127 || firstOctet == 0 {
			logger.Warningf("自动发现: 跳过特殊网段 %s (%s)", s.Network, s.Name)
			continue
		}
		// 169.254.0.0/16 链路本地
		if firstOctet == 169 && ip.To4()[1] == 254 {
			continue
		}

		normalizedCIDR := ipNet.String()
		// 检查是否已在配置中
		_, added := routes[normalizedCIDR]

		subnets = append(subnets, DockerSubnet{
			Network: normalizedCIDR,
			Name:    s.Name,
			Driver:  s.Driver,
			Added:   added,
		})
	}

	return subnets, nil
}

func loadHosts(buf *bytes.Buffer, hosts string) {
	if hosts == "" {
		return
	}
	re := regexp.MustCompile(`^\s*(".*"|\S*)\s+((?:[\w.+-]+\s*){1,})$`)
	match := re.FindStringSubmatch(hosts)
	if match == nil {
		logger.Warningf("invalid hosts config: %v\n", hosts)
		return
	}
	fi, err := os.Open(match[1])
	if err != nil {
		logger.Warningf("invalid hosts file: %v\n", match[1])
		return
	}
	defer fi.Close()
	dns := match[2]
	if buf.Len() > 0 {
		buf.WriteString(",")
	}
	buf.WriteString("dns ")
	buf.WriteString(dns)
	domain_arr := strings.Split(strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(dns, " ")), " ")
	domain_match := func(s string) bool {
		for _, val := range domain_arr {
			for _, d := range strings.Split(s, " ") {
				if strings.HasSuffix(d, val) {
					return true
				}
			}
		}
		return false
	}
	re = regexp.MustCompile(`^\s*(\d+[\d.]+)\s+([^#]+)\s*(#.*)?$`)
	br := bufio.NewReader(fi)
	for {
		a, _, c := br.ReadLine()
		if c == io.EOF {
			break
		}
		s := string(a)
		match := re.FindStringSubmatch(s)
		if match != nil {
			if strings.Contains(match[3], "docker-connector:ignore") {
				continue
			}
			if !domain_match(match[2]) && !strings.Contains(match[3], "docker-connector:resolve") {
				continue
			}
			if buf.Len() > 0 {
				buf.WriteString(",")
			}
			buf.WriteString("host ")
			if match[1] == "127.0.0.1" {
				buf.WriteString(localIP.String())
			} else {
				buf.WriteString(match[1])
			}
			buf.WriteString(" ")
			buf.WriteString(match[2])
		}
	}
}
