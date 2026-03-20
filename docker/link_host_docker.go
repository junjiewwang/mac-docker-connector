package main

import "fmt"

// HostDockerLink host-docker 链路：宿主机 (tun0) ↔ Docker 容器子网（非 Minikube 网桥）
//
// 底层操作：
// - FORWARD tun0→非mk网桥
// - FORWARD 非mk网桥→tun0 RELATED,ESTABLISHED
type HostDockerLink struct {
	mgr *LinkManager
}

func (l *HostDockerLink) Name() string        { return "host-docker" }
func (l *HostDockerLink) Description() string  { return "Host ↔ Docker" }
func (l *HostDockerLink) SubLevels() []string  { return nil }

// rules 生成 host-docker 链路的 iptables 规则
func (l *HostDockerLink) rules() []ruleInfo {
	if !l.mgr.network.InterfaceExists("tun0") {
		fmt.Println("[host-docker] ⚠️  tun0 设备不存在，跳过")
		return nil
	}

	nonMkBridges := l.mgr.NonMkBridges()
	var rules []ruleInfo
	for _, bridge := range nonMkBridges {
		fmt.Printf("[host-docker] tun0 ↔ %s (%s)\n", bridge.Name, bridge.Subnet)
		rules = append(rules,
			ruleInfo{
				Table: "filter", Chain: "FORWARD",
				Rule:  []string{"-i", "tun0", "-o", bridge.Name, "-j", "ACCEPT"},
				Label: fmt.Sprintf("FORWARD tun0 → %s", bridge.Name),
			},
			ruleInfo{
				Table: "filter", Chain: "FORWARD",
Rule:  []string{"-i", bridge.Name, "-o", "tun0", "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
				Label: fmt.Sprintf("FORWARD %s → tun0 (ESTABLISHED)", bridge.Name),
			},
		)
	}
	return rules
}

func (l *HostDockerLink) Apply(subLevel string) error {
	rules := l.rules()
	if len(rules) == 0 {
		return fmt.Errorf("无可用非 Minikube 网桥或 tun0 不存在")
	}
	l.mgr.batchAddRules("🖥️ host-docker: 配置宿主机与 Docker 容器通信", rules)
	l.mgr.InvalidateCache()
	return nil
}

func (l *HostDockerLink) Revert(subLevel string) error {
	rules := l.rules()
	if len(rules) == 0 {
		return nil
	}
	l.mgr.batchRemoveRules("🖥️ host-docker: 还原宿主机与 Docker 容器通信", rules)
	l.mgr.InvalidateCache()
	return nil
}

func (l *HostDockerLink) Status(subLevel string) []LinkStatus {
	st := LinkStatus{Name: "host-docker", Description: l.Description()}
	rules := l.rules()
	l.mgr.rulesToStatus(rules, &st)
	st.ComputeStatus()
	return []LinkStatus{st}
}
