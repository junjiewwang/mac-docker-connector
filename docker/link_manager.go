package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Link 链路接口
// 每条链路代表两个域(Zone)之间的网络连通性
type Link interface {
	// Name 链路名称，如 "internet", "host-docker"
	Name() string
	// Description 链路描述
	Description() string
	// SubLevels 子层级列表，如 ["service", "pod"]，nil 表示无子层级
	SubLevels() []string
	// Apply 应用链路配置
	Apply(subLevel string) error
	// Revert 还原链路配置
	Revert(subLevel string) error
	// Status 获取链路状态
	Status(subLevel string) []LinkStatus
}

// LinkStatus 链路状态
type LinkStatus struct {
	Name          string       `json:"name"`
	Description   string       `json:"description"`
	Status        string       `json:"status"` // "active" / "partial" / "inactive"
	Desired       bool         `json:"desired,omitempty"`
	ActiveSource  string       `json:"active_source,omitempty"`
	ManagedActive int          `json:"managed_active,omitempty"`
	LegacyActive  int          `json:"legacy_active,omitempty"`
	RulesActive   int          `json:"rules_active"`
	RulesTotal    int          `json:"rules_total"`
	Details       []RuleDetail `json:"details"`
}

// RuleDetail 规则详情
type RuleDetail struct {
	Label  string `json:"label"`
	Active bool   `json:"active"`
	Type   string `json:"type"` // "iptables", "route", "dns"
	Source string `json:"source,omitempty"`
}

// ComputeStatus 根据 active/total 计算状态字符串
func (s *LinkStatus) ComputeStatus() {
	if s.RulesTotal == 0 {
		s.Status = "inactive"
	} else if s.RulesActive == s.RulesTotal {
		s.Status = "active"
	} else if s.RulesActive > 0 {
		s.Status = "partial"
	} else {
		s.Status = "inactive"
	}

	switch {
	case s.ManagedActive > 0 && s.LegacyActive > 0:
		s.ActiveSource = "mixed"
	case s.ManagedActive > 0:
		s.ActiveSource = "managed"
	case s.LegacyActive > 0:
		s.ActiveSource = "legacy"
	default:
		s.ActiveSource = ""
	}
}

// AppendCheck 向 LinkStatus 追加一条检查结果
func (s *LinkStatus) AppendCheck(label string, active bool) {
	s.AppendCheckTyped(label, active, "iptables")
}

// AppendCheckTyped 向 LinkStatus 追加一条带类型的检查结果
func (s *LinkStatus) AppendCheckTyped(label string, active bool, ruleType string) {
	s.AppendCheckObserved(label, active, ruleType, "")
}

// AppendCheckObserved 向 LinkStatus 追加一条带来源信息的检查结果
func (s *LinkStatus) AppendCheckObserved(label string, active bool, ruleType string, source string) {
	s.Details = append(s.Details, RuleDetail{Label: label, Active: active, Type: ruleType, Source: source})
	s.RulesTotal++
	if active {
		s.RulesActive++
		switch source {
		case "managed":
			s.ManagedActive++
		case "legacy":
			s.LegacyActive++
		case "mixed":
			s.ManagedActive++
			s.LegacyActive++
		}
	}
}

// ruleInfo 内部用于规则生成器的结构
type ruleInfo struct {
	Table string
	Chain string
	Rule  []string
	Label string // 可选，用于 status 展示
}

// LinkManager 链路管理器（注册表 + 缓存）
type LinkManager struct {
	links   []Link
	linkMap map[string]Link

	// 网络信息缓存
	iptables *IptablesManager
	network  *NetworkInfoProvider
	routeMgr *RouteManager
	dnsMgr   *DnsManager

	// 缓存
	cacheMu       sync.RWMutex
	physicalIf    string
	bridges       []BridgeInfo
	minikubeInfo  *MinikubeInfo
	cacheTime     time.Time
	cacheDuration time.Duration
}

// NewLinkManager 创建链路管理器并注册所有链路
func NewLinkManager() *LinkManager {
	network := &NetworkInfoProvider{}
	routeMgr := &RouteManager{}

	mgr := &LinkManager{
		linkMap:       make(map[string]Link),
		iptables:      NewIptablesManager(true),
		network:       network,
		routeMgr:      routeMgr,
		dnsMgr:        NewDnsManager(network),
		cacheDuration: 60 * time.Second,
	}

	// 注册所有链路（按执行顺序）
	mgr.register(&InternetLink{mgr: mgr})
	mgr.register(&HostDockerLink{mgr: mgr})
	mgr.register(&HostK8sLink{mgr: mgr})
	mgr.register(&DockerK8sLink{mgr: mgr})
	mgr.register(&DockerDockerLink{mgr: mgr})

	return mgr
}

// register 注册链路
func (m *LinkManager) register(link Link) {
	m.links = append(m.links, link)
	m.linkMap[link.Name()] = link
}

// GetLink 获取指定名称的链路
func (m *LinkManager) GetLink(name string) Link {
	return m.linkMap[name]
}

// AllLinks 获取所有已注册链路
func (m *LinkManager) AllLinks() []Link {
	return m.links
}

// RefreshCache 刷新网络信息缓存
func (m *LinkManager) RefreshCache() {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()

	m.physicalIf = m.network.GetPhysicalInterface()
	m.bridges = m.network.GetDockerBridges()
	m.minikubeInfo = m.network.GetMinikubeInfo()
	m.cacheTime = time.Now()
	m.iptables.ClearCache()

	if debug {
		fmt.Printf("[LINK] 缓存已刷新: %d 个网桥, 物理接口=%s, minikube=%v\n",
			len(m.bridges), m.physicalIf, m.minikubeInfo != nil)
	}
}

// InvalidateCache 失效缓存（apply/revert 后调用）
func (m *LinkManager) InvalidateCache() {
	m.cacheMu.Lock()
	m.cacheTime = time.Time{} // 零值，强制下次刷新
	m.iptables.ClearCache()
	m.cacheMu.Unlock()
}

// ensureCache 确保缓存有效，如过期则自动刷新
func (m *LinkManager) ensureCache() {
	m.cacheMu.RLock()
	expired := time.Since(m.cacheTime) > m.cacheDuration
	m.cacheMu.RUnlock()

	if expired {
		m.RefreshCache()
	}
}

// PhysicalInterface 获取缓存的物理接口
func (m *LinkManager) PhysicalInterface() string {
	m.ensureCache()
	m.cacheMu.RLock()
	defer m.cacheMu.RUnlock()
	return m.physicalIf
}

// Bridges 获取缓存的网桥列表
func (m *LinkManager) Bridges() []BridgeInfo {
	m.ensureCache()
	m.cacheMu.RLock()
	defer m.cacheMu.RUnlock()
	return m.bridges
}

// MinikubeInfo 获取缓存的 Minikube 信息
func (m *LinkManager) MinikubeInfo() *MinikubeInfo {
	m.ensureCache()
	m.cacheMu.RLock()
	defer m.cacheMu.RUnlock()
	return m.minikubeInfo
}

// NonMkBridges 获取非 Minikube 的网桥列表
func (m *LinkManager) NonMkBridges() []BridgeInfo {
	bridges := m.Bridges()
	mkInfo := m.MinikubeInfo()
	mkBridge := ""
	if mkInfo != nil {
		mkBridge = mkInfo.BridgeName
	}

	var result []BridgeInfo
	for _, b := range bridges {
		if b.Name != mkBridge {
			result = append(result, b)
		}
	}
	return result
}

// ParseLinkSpec 解析链路规格字符串，返回 (linkName, subLevel)
// 例如: "internet" -> ("internet", "")
//
//	"host-k8s.service" -> ("host-k8s", "service")
func ParseLinkSpec(spec string) (string, string) {
	if idx := strings.Index(spec, "."); idx >= 0 {
		return spec[:idx], spec[idx+1:]
	}
	return spec, ""
}

// AllLinkNames 获取所有合法的链路名称（含子层级）
func (m *LinkManager) AllLinkNames() []string {
	var names []string
	for _, link := range m.links {
		names = append(names, link.Name())
		for _, sub := range link.SubLevels() {
			names = append(names, link.Name()+"."+sub)
		}
	}
	return names
}

// StatusAll 获取所有链路的状态
func (m *LinkManager) StatusAll() []LinkStatus {
	m.ensureCache()
	var allStatus []LinkStatus
	for _, link := range m.links {
		statuses := link.Status("")
		allStatus = append(allStatus, statuses...)
	}
	return allStatus
}

// batchAddRules 批量添加 iptables 规则（Link 共用方法）
func (m *LinkManager) batchAddRules(title string, rules []ruleInfo) BatchResult {
	fmt.Printf("[LINK] %s\n", title)
	for _, r := range rules {
		m.iptables.AddRule(r.Table, r.Chain, r.Rule)
	}
	result := m.iptables.Commit()
	if result.Added > 0 {
		fmt.Printf("[LINK] ✅ 添加了 %d 条规则\n", result.Added)
	} else {
		fmt.Println("[LINK] ✅ 所有规则已存在")
	}
	return result
}

// batchRemoveRules 批量删除 iptables 规则（Link 共用方法）
func (m *LinkManager) batchRemoveRules(title string, rules []ruleInfo) BatchResult {
	fmt.Printf("[LINK] %s\n", title)
	for _, r := range rules {
		m.iptables.RemoveRule(r.Table, r.Chain, r.Rule)
	}
	result := m.iptables.CommitRemove()
	if result.Removed > 0 {
		fmt.Printf("[LINK] ✅ 删除了 %d 条规则\n", result.Removed)
	} else {
		fmt.Println("[LINK] ✅ 无需删除任何规则")
	}
	return result
}

// rulesToStatus 从规则列表构建 status
func (m *LinkManager) rulesToStatus(rules []ruleInfo, st *LinkStatus) {
	for _, r := range rules {
		presence := m.iptables.InspectRule(r.Table, r.Chain, r.Rule)
		label := r.Label
		if label == "" {
			if r.Table == "filter" {
				label = strings.Join(r.Rule[:minInt(4, len(r.Rule))], " ")
			} else {
				label = "NAT " + strings.Join(r.Rule[:minInt(4, len(r.Rule))], " ")
			}
		}
		ruleType := "iptables"
		if r.Table == "nat" {
			ruleType = "nat"
		}
		st.AppendCheckObserved(label, presence.Active(), ruleType, presence.Source())
	}
}

// minInt Go 1.13 兼容的 min 函数
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
