package main

import "fmt"

// DockerK8sLink docker-k8s 链路：Docker 容器子网 ↔ Kubernetes (Minikube)
//
// 子层级:
// - service: FORWARD 非mk网桥↔mk_bridge
// - pod: FORWARD 非mk网桥↔mk_bridge (pod_cidr过滤)
type DockerK8sLink struct {
	mgr *LinkManager
}

func (l *DockerK8sLink) Name() string        { return "docker-k8s" }
func (l *DockerK8sLink) Description() string  { return "Docker ↔ Kubernetes" }
func (l *DockerK8sLink) SubLevels() []string  { return []string{"service", "pod"} }

// serviceRules 生成 Service 子层级的 iptables 规则
func (l *DockerK8sLink) serviceRules() []ruleInfo {
	mkInfo := l.mgr.MinikubeInfo()
	if mkInfo == nil {
		return nil
	}

	nonMkBridges := l.mgr.NonMkBridges()
	var rules []ruleInfo
	for _, bridge := range nonMkBridges {
		fmt.Printf("[docker-k8s.service] %s ↔ %s (Service)\n", bridge.Name, mkInfo.BridgeName)
		rules = append(rules,
			ruleInfo{
				Table: "filter", Chain: "FORWARD",
				Rule:  []string{"-i", bridge.Name, "-o", mkInfo.BridgeName, "-j", "ACCEPT"},
				Label: fmt.Sprintf("FORWARD %s → %s", bridge.Name, mkInfo.BridgeName),
			},
			ruleInfo{
				Table: "filter", Chain: "FORWARD",
				Rule:  []string{"-i", mkInfo.BridgeName, "-o", bridge.Name, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
				Label: fmt.Sprintf("FORWARD %s → %s (ESTABLISHED)", mkInfo.BridgeName, bridge.Name),
			},
		)
	}
	return rules
}

// podRules 生成 Pod 子层级的 iptables 规则
func (l *DockerK8sLink) podRules() []ruleInfo {
	mkInfo := l.mgr.MinikubeInfo()
	if mkInfo == nil || mkInfo.PodCIDR == "" {
		return nil
	}

	nonMkBridges := l.mgr.NonMkBridges()
	var rules []ruleInfo
	for _, bridge := range nonMkBridges {
		fmt.Printf("[docker-k8s.pod] %s ↔ %s (Pod CIDR: %s)\n", bridge.Name, mkInfo.BridgeName, mkInfo.PodCIDR)
		rules = append(rules,
			ruleInfo{
				Table: "filter", Chain: "FORWARD",
				Rule:  []string{"-i", bridge.Name, "-d", mkInfo.PodCIDR, "-o", mkInfo.BridgeName, "-j", "ACCEPT"},
				Label: fmt.Sprintf("FORWARD %s → %s (Pod CIDR)", bridge.Name, mkInfo.BridgeName),
			},
			ruleInfo{
				Table: "filter", Chain: "FORWARD",
				Rule:  []string{"-i", mkInfo.BridgeName, "-s", mkInfo.PodCIDR, "-o", bridge.Name, "-j", "ACCEPT"},
				Label: fmt.Sprintf("FORWARD %s → %s (Pod CIDR)", mkInfo.BridgeName, bridge.Name),
			},
		)
	}
	return rules
}

func (l *DockerK8sLink) Apply(subLevel string) error {
	mkInfo := l.mgr.MinikubeInfo()
	if mkInfo == nil {
		return fmt.Errorf("未找到运行中的 Minikube 容器，跳过 docker-k8s 配置")
	}

	if subLevel == "" || subLevel == "service" {
		rules := l.serviceRules()
		if len(rules) > 0 {
			l.mgr.batchAddRules("🐳 docker-k8s.service: 配置容器访问 K8s Service", rules)
		}
	}

	if subLevel == "" || subLevel == "pod" {
		rules := l.podRules()
		if len(rules) > 0 {
			l.mgr.batchAddRules("🐳 docker-k8s.pod: 配置容器访问 K8s Pod", rules)
		}
	}

	l.mgr.InvalidateCache()
	return nil
}

func (l *DockerK8sLink) Revert(subLevel string) error {
	mkInfo := l.mgr.MinikubeInfo()
	if mkInfo == nil {
		return fmt.Errorf("未找到运行中的 Minikube 容器，跳过还原")
	}

	if subLevel == "" || subLevel == "service" {
		rules := l.serviceRules()
		if len(rules) > 0 {
			l.mgr.batchRemoveRules("🐳 docker-k8s.service: 还原容器访问 K8s Service", rules)
		}
	}

	if subLevel == "" || subLevel == "pod" {
		rules := l.podRules()
		if len(rules) > 0 {
			l.mgr.batchRemoveRules("🐳 docker-k8s.pod: 还原容器访问 K8s Pod", rules)
		}
	}

	l.mgr.InvalidateCache()
	return nil
}

func (l *DockerK8sLink) Status(subLevel string) []LinkStatus {
	var results []LinkStatus

	if subLevel == "" || subLevel == "service" {
		st := LinkStatus{Name: "docker-k8s.service", Description: "Docker ↔ K8s Service"}
		rules := l.serviceRules()
		l.mgr.rulesToStatus(rules, &st)
		st.ComputeStatus()
		results = append(results, st)
	}

	if subLevel == "" || subLevel == "pod" {
		st := LinkStatus{Name: "docker-k8s.pod", Description: "Docker ↔ K8s Pod"}
		rules := l.podRules()
		l.mgr.rulesToStatus(rules, &st)
		st.ComputeStatus()
		results = append(results, st)
	}

	return results
}
