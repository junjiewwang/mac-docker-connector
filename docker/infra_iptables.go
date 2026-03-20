package main

import (
	"fmt"
	"strings"
	"sync"
)

// IptablesRule 表示一条 iptables 规则
type IptablesRule struct {
	Table string   // filter / nat / mangle
	Chain string   // FORWARD / PREROUTING / DOCKER-USER 等
	Rule  []string // 规则参数（不含 -A/-D/-C 和链名）
}

// String 返回规则的可读字符串表示
func (r IptablesRule) String() string {
	return fmt.Sprintf("iptables -t %s %s %s", r.Table, r.Chain, strings.Join(r.Rule, " "))
}

// IptablesManager 统一的 iptables 规则管理器（支持批量操作）
type IptablesManager struct {
	batchMode     bool
	rulesToAdd    []IptablesRule
	rulesToRemove []IptablesRule
	cache         map[string]string // table:chain -> iptables -S 输出
	mu            sync.Mutex
}

// BatchResult 批量操作结果
type BatchResult struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
	Skipped int `json:"skipped"`
	Failed  int `json:"failed"`
}

// NewIptablesManager 创建新的 iptables 管理器
func NewIptablesManager(batchMode bool) *IptablesManager {
	return &IptablesManager{
		batchMode: batchMode,
		cache:     make(map[string]string),
	}
}

// RuleExists 检查规则是否存在
func (m *IptablesManager) RuleExists(table, chain string, rule []string) bool {
	args := []string{"iptables"}
	if table != "filter" {
		args = append(args, "-t", table)
	}
	args = append(args, "-C", chain)
	args = append(args, rule...)

	err := runCommandSudoSilent(args...)
	return err == nil
}

// AddRule 添加规则（支持批量模式）
func (m *IptablesManager) AddRule(table, chain string, rule []string) {
	r := IptablesRule{Table: table, Chain: chain, Rule: rule}
	if m.batchMode {
		m.mu.Lock()
		m.rulesToAdd = append(m.rulesToAdd, r)
		m.mu.Unlock()
	} else {
		m.executeAddRule(r)
	}
}

// RemoveRule 删除规则（支持批量模式）
func (m *IptablesManager) RemoveRule(table, chain string, rule []string) {
	r := IptablesRule{Table: table, Chain: chain, Rule: rule}
	if m.batchMode {
		m.mu.Lock()
		m.rulesToRemove = append(m.rulesToRemove, r)
		m.mu.Unlock()
	} else {
		m.executeRemoveRule(r)
	}
}

// executeAddRule 立即执行添加单条规则
func (m *IptablesManager) executeAddRule(r IptablesRule) bool {
	if m.RuleExists(r.Table, r.Chain, r.Rule) {
		if debug {
			fmt.Printf("[IPTABLES] 规则已存在: %s\n", r)
		}
		return false
	}

	args := []string{"iptables"}
	if r.Table != "filter" {
		args = append(args, "-t", r.Table)
	}
	args = append(args, "-A", r.Chain)
	args = append(args, r.Rule...)

	if _, err := runCommandSudo(args...); err != nil {
		fmt.Printf("[IPTABLES] 添加规则失败: %s => %v\n", r, err)
		return false
	}
	fmt.Printf("[IPTABLES] 已添加规则: %s\n", r)
	return true
}

// executeRemoveRule 立即执行删除单条规则
func (m *IptablesManager) executeRemoveRule(r IptablesRule) bool {
	if !m.RuleExists(r.Table, r.Chain, r.Rule) {
		if debug {
			fmt.Printf("[IPTABLES] 规则不存在，无需删除: %s\n", r)
		}
		return false
	}

	args := []string{"iptables"}
	if r.Table != "filter" {
		args = append(args, "-t", r.Table)
	}
	args = append(args, "-D", r.Chain)
	args = append(args, r.Rule...)

	if _, err := runCommandSudo(args...); err != nil {
		fmt.Printf("[IPTABLES] 删除规则失败: %s => %v\n", r, err)
		return false
	}
	fmt.Printf("[IPTABLES] 已删除规则: %s\n", r)
	return true
}

// loadCache 加载并缓存指定 table:chain 的现有规则
func (m *IptablesManager) loadCache(table, chain string) {
	key := table + ":" + chain
	if _, ok := m.cache[key]; ok {
		return
	}

	args := []string{"iptables"}
	if table != "filter" {
		args = append(args, "-t", table)
	}
	args = append(args, "-S", chain)

	out, err := runCommandSudo(args...)
	if err != nil {
		m.cache[key] = ""
		return
	}
	m.cache[key] = out
}

// ruleExistsInCache 从缓存中检查规则是否存在
// 使用逐行精确匹配（而非子串匹配），避免误判：
// 例如查 "-o br-xxx -m conntrack ..." 时不会误匹配到
// "-A FORWARD -i br-yyy -o br-xxx -m conntrack ..."
func (m *IptablesManager) ruleExistsInCache(table, chain string, rule []string) bool {
	key := table + ":" + chain
	cached, ok := m.cache[key]
	if !ok {
		m.loadCache(table, chain)
		cached = m.cache[key]
	}
	// 构造 iptables -S 输出中的完整行格式："-A CHAIN rule..."
	target := "-A " + chain + " " + strings.Join(rule, " ")
	for _, line := range strings.Split(cached, "\n") {
		if strings.TrimSpace(line) == target {
			return true
		}
	}
	return false
}

// Commit 批量提交所有待添加的规则
func (m *IptablesManager) Commit() BatchResult {
	m.mu.Lock()
	rules := m.rulesToAdd
	m.rulesToAdd = nil
	m.mu.Unlock()

	result := BatchResult{}
	if len(rules) == 0 {
		return result
	}

	if debug {
		fmt.Printf("[IPTABLES] 开始批量处理 %d 条规则...\n", len(rules))
	}

	// 预加载规则缓存
	seen := make(map[string]bool)
	for _, r := range rules {
		key := r.Table + ":" + r.Chain
		if !seen[key] {
			m.loadCache(r.Table, r.Chain)
			seen[key] = true
		}
	}

	for _, r := range rules {
		if m.ruleExistsInCache(r.Table, r.Chain, r.Rule) {
			if debug {
				fmt.Printf("[IPTABLES] 规则已存在(缓存): %s\n", r)
			}
			result.Skipped++
			continue
		}

		args := []string{"iptables"}
		if r.Table != "filter" {
			args = append(args, "-t", r.Table)
		}
		args = append(args, "-A", r.Chain)
		args = append(args, r.Rule...)

		if _, err := runCommandSudo(args...); err != nil {
			fmt.Printf("[IPTABLES] 添加规则失败: %s => %v\n", r, err)
			result.Failed++
			continue
		}
		fmt.Printf("[IPTABLES] 已添加规则: %s\n", r)
		result.Added++

		// 更新缓存
		key := r.Table + ":" + r.Chain
		m.cache[key] += "\n-A " + r.Chain + " " + strings.Join(r.Rule, " ")
	}

	if debug {
		fmt.Printf("[IPTABLES] 批量处理完成: 添加 %d，跳过 %d，失败 %d\n", result.Added, result.Skipped, result.Failed)
	}
	return result
}

// CommitRemove 批量提交所有待删除的规则
func (m *IptablesManager) CommitRemove() BatchResult {
	m.mu.Lock()
	rules := m.rulesToRemove
	m.rulesToRemove = nil
	m.mu.Unlock()

	result := BatchResult{}
	if len(rules) == 0 {
		return result
	}

	if debug {
		fmt.Printf("[IPTABLES] 开始批量删除 %d 条规则...\n", len(rules))
	}

	for _, r := range rules {
		if m.executeRemoveRule(r) {
			result.Removed++
		} else {
			result.Skipped++
		}
	}

	// 清空缓存，因为删除后缓存已过时
	m.cache = make(map[string]string)

	if debug {
		fmt.Printf("[IPTABLES] 批量删除完成: 删除 %d，跳过 %d\n", result.Removed, result.Skipped)
	}
	return result
}

// ClearCache 清空规则缓存（在 apply/revert 操作后调用）
func (m *IptablesManager) ClearCache() {
	m.mu.Lock()
	m.cache = make(map[string]string)
	m.mu.Unlock()
}

// ListRules 列出指定 table:chain 的规则
func (m *IptablesManager) ListRules(table, chain string) ([]string, error) {
	args := []string{"iptables"}
	if table != "filter" {
		args = append(args, "-t", table)
	}
	args = append(args, "-L", chain, "-n", "--line-numbers")

	out, err := runCommandSudo(args...)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(out, "\n")
	// 跳过前两行（表头）
	if len(lines) > 2 {
		return lines[2:], nil
	}
	return nil, nil
}
