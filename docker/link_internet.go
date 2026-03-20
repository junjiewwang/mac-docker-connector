package main

import "fmt"

// InternetLink internet 链路：所有网桥（含 Minikube）↔ 外网
//
// 底层操作：
// - FORWARD bridge→物理网卡
// - FORWARD 物理网卡→bridge RELATED,ESTABLISHED
// - NAT MASQUERADE
type InternetLink struct {
	mgr *LinkManager
}

func (l *InternetLink) Name() string        { return "internet" }
func (l *InternetLink) Description() string  { return "Docker/K8s ↔ Internet" }
func (l *InternetLink) SubLevels() []string  { return nil }

// rules 生成 internet 链路的 iptables 规则
func (l *InternetLink) rules() []ruleInfo {
	physicalIf := l.mgr.PhysicalInterface()
	if physicalIf == "" {
		fmt.Println("[internet] ⚠️  无法获取物理网卡名称")
		return nil
	}

	bridges := l.mgr.Bridges()
	var rules []ruleInfo
	for _, bridge := range bridges {
		fmt.Printf("[internet] 网桥: %s (子网: %s)\n", bridge.Name, bridge.Subnet)
		rules = append(rules,
			ruleInfo{
				Table: "filter", Chain: "FORWARD",
				Rule:  []string{"-i", bridge.Name, "-o", physicalIf, "-j", "ACCEPT"},
				Label: fmt.Sprintf("FORWARD %s → %s", bridge.Name, physicalIf),
			},
			ruleInfo{
				Table: "filter", Chain: "FORWARD",
				Rule:  []string{"-i", physicalIf, "-o", bridge.Name, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
				Label: fmt.Sprintf("FORWARD %s → %s (ESTABLISHED)", physicalIf, bridge.Name),
			},
			ruleInfo{
				Table: "nat", Chain: "POSTROUTING",
				Rule:  []string{"-s", bridge.Subnet, "-o", physicalIf, "-j", "MASQUERADE"},
				Label: fmt.Sprintf("NAT MASQUERADE %s → %s", bridge.Subnet, physicalIf),
			},
		)
	}
	return rules
}

func (l *InternetLink) Apply(subLevel string) error {
	rules := l.rules()
	if len(rules) == 0 {
		return fmt.Errorf("无可用网桥或物理接口")
	}
	l.mgr.batchAddRules("🌐 internet: 配置所有网桥访问外网", rules)
	l.mgr.InvalidateCache()
	return nil
}

func (l *InternetLink) Revert(subLevel string) error {
	rules := l.rules()
	if len(rules) == 0 {
		return nil
	}
	l.mgr.batchRemoveRules("🌐 internet: 还原所有网桥外网访问", rules)
	l.mgr.InvalidateCache()
	return nil
}

func (l *InternetLink) Status(subLevel string) []LinkStatus {
	st := LinkStatus{Name: "internet", Description: l.Description()}
	rules := l.rules()
	l.mgr.rulesToStatus(rules, &st)
	st.ComputeStatus()
	return []LinkStatus{st}
}
