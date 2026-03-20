package main

import "fmt"

// DockerDockerLink docker-docker 链路：不同 Docker 子网之间的容器互通
//
// 底层操作（对每个非 Minikube 网桥）：
// - FORWARD bridge→bridge 自身
// - FORWARD bridge 出站 RELATED,ESTABLISHED
// - FORWARD bridge 入站 RELATED,ESTABLISHED
type DockerDockerLink struct {
	mgr *LinkManager
}

func (l *DockerDockerLink) Name() string        { return "docker-docker" }
func (l *DockerDockerLink) Description() string  { return "Docker ↔ Docker" }
func (l *DockerDockerLink) SubLevels() []string  { return nil }

// rules 生成 docker-docker 链路的 iptables 规则
func (l *DockerDockerLink) rules() []ruleInfo {
	nonMkBridges := l.mgr.NonMkBridges()
	var rules []ruleInfo
	for _, bridge := range nonMkBridges {
		fmt.Printf("[docker-docker] 网桥 %s (%s) 子网内通信\n", bridge.Name, bridge.Subnet)
		rules = append(rules,
			ruleInfo{
				Table: "filter", Chain: "FORWARD",
				Rule:  []string{"-i", bridge.Name, "-o", bridge.Name, "-j", "ACCEPT"},
				Label: fmt.Sprintf("FORWARD %s → %s (自身)", bridge.Name, bridge.Name),
			},
			ruleInfo{
				Table: "filter", Chain: "FORWARD",
				Rule:  []string{"-i", bridge.Name, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
				Label: fmt.Sprintf("FORWARD %s 出站 (ESTABLISHED)", bridge.Name),
			},
			ruleInfo{
				Table: "filter", Chain: "FORWARD",
				Rule:  []string{"-o", bridge.Name, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
				Label: fmt.Sprintf("FORWARD → %s 入站 (ESTABLISHED)", bridge.Name),
			},
		)
	}
	return rules
}

func (l *DockerDockerLink) Apply(subLevel string) error {
	rules := l.rules()
	if len(rules) == 0 {
		return fmt.Errorf("无可用非 Minikube 网桥")
	}
	l.mgr.batchAddRules("🐳 docker-docker: 配置 Docker 子网间通信", rules)
	l.mgr.InvalidateCache()
	return nil
}

func (l *DockerDockerLink) Revert(subLevel string) error {
	rules := l.rules()
	if len(rules) == 0 {
		return nil
	}
	l.mgr.batchRemoveRules("🐳 docker-docker: 还原 Docker 子网间通信", rules)
	l.mgr.InvalidateCache()
	return nil
}

func (l *DockerDockerLink) Status(subLevel string) []LinkStatus {
	st := LinkStatus{Name: "docker-docker", Description: l.Description()}
	rules := l.rules()
	l.mgr.rulesToStatus(rules, &st)
	st.ComputeStatus()
	return []LinkStatus{st}
}
