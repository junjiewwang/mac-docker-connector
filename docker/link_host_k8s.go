package main

import "fmt"

// HostK8sLink host-k8s 链路：宿主机 (tun0) ↔ Kubernetes (Minikube)
//
// 子层级:
// - service: route(service_cidr) + FORWARD tun0↔mk_bridge + DNS
// - pod: route(pod_cidr) + FORWARD tun0↔mk_bridge(pod_cidr)
type HostK8sLink struct {
	mgr *LinkManager
}

func (l *HostK8sLink) Name() string        { return "host-k8s" }
func (l *HostK8sLink) Description() string  { return "Host ↔ Kubernetes" }
func (l *HostK8sLink) SubLevels() []string  { return []string{"service", "pod"} }

// serviceRules 生成 Service 子层级的 iptables 规则
func (l *HostK8sLink) serviceRules() []ruleInfo {
	mkInfo := l.mgr.MinikubeInfo()
	if mkInfo == nil {
		fmt.Println("[host-k8s.service] ⚠️  未找到运行中的 Minikube 容器")
		return nil
	}
	if !l.mgr.network.InterfaceExists("tun0") {
		fmt.Println("[host-k8s.service] ⚠️  tun0 设备不存在")
		return nil
	}

	fmt.Printf("[host-k8s.service] tun0 ↔ %s (Minikube Service)\n", mkInfo.BridgeName)
	return []ruleInfo{
		{
			Table: "filter", Chain: "FORWARD",
			Rule:  []string{"-i", "tun0", "-o", mkInfo.BridgeName, "-j", "ACCEPT"},
			Label: fmt.Sprintf("FORWARD tun0 → %s", mkInfo.BridgeName),
		},
		{
			Table: "filter", Chain: "FORWARD",
Rule:  []string{"-i", mkInfo.BridgeName, "-o", "tun0", "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
			Label: fmt.Sprintf("FORWARD %s → tun0 (ESTABLISHED)", mkInfo.BridgeName),
		},
	}
}

// podRules 生成 Pod 子层级的 iptables 规则
func (l *HostK8sLink) podRules() []ruleInfo {
	mkInfo := l.mgr.MinikubeInfo()
	if mkInfo == nil || mkInfo.PodCIDR == "" {
		fmt.Println("[host-k8s.pod] ⚠️  未找到 Minikube 或 Pod CIDR")
		return nil
	}
	if !l.mgr.network.InterfaceExists("tun0") {
		fmt.Println("[host-k8s.pod] ⚠️  tun0 设备不存在")
		return nil
	}

	fmt.Printf("[host-k8s.pod] tun0 ↔ %s (Pod CIDR: %s)\n", mkInfo.BridgeName, mkInfo.PodCIDR)
	return []ruleInfo{
		{
			Table: "filter", Chain: "FORWARD",
			Rule:  []string{"-i", "tun0", "-d", mkInfo.PodCIDR, "-o", mkInfo.BridgeName, "-j", "ACCEPT"},
			Label: fmt.Sprintf("FORWARD tun0 → %s (Pod CIDR)", mkInfo.BridgeName),
		},
		{
			Table: "filter", Chain: "FORWARD",
			Rule:  []string{"-i", mkInfo.BridgeName, "-s", mkInfo.PodCIDR, "-o", "tun0", "-j", "ACCEPT"},
			Label: fmt.Sprintf("FORWARD %s → tun0 (Pod CIDR)", mkInfo.BridgeName),
		},
	}
}

func (l *HostK8sLink) Apply(subLevel string) error {
	mkInfo := l.mgr.MinikubeInfo()
	if mkInfo == nil {
		return fmt.Errorf("未找到运行中的 Minikube 容器，跳过 host-k8s 配置")
	}

	if subLevel == "" || subLevel == "service" {
		rules := l.serviceRules()
		if len(rules) > 0 {
			l.mgr.batchAddRules("🖥️ host-k8s.service: 配置宿主机访问 K8s Service", rules)
		}
		// Service 路由
		if mkInfo.ServiceCIDR != "" {
			fmt.Printf("[host-k8s.service] Service CIDR 路由: %s via %s\n", mkInfo.ServiceCIDR, mkInfo.ContainerIP)
			l.mgr.routeMgr.AddRoute(mkInfo.ServiceCIDR, mkInfo.ContainerIP)
		}
		// DNS 配置
		fmt.Println("[host-k8s.service] 配置 Kubernetes DNS...")
		l.mgr.dnsMgr.ConfigureDNS(mkInfo)
	}

	if subLevel == "" || subLevel == "pod" {
		rules := l.podRules()
		if len(rules) > 0 {
			l.mgr.batchAddRules("🖥️ host-k8s.pod: 配置宿主机访问 K8s Pod", rules)
		}
		// Pod 路由
		if mkInfo.PodCIDR != "" {
			fmt.Printf("[host-k8s.pod] Pod CIDR 路由: %s via %s\n", mkInfo.PodCIDR, mkInfo.ContainerIP)
			l.mgr.routeMgr.AddRoute(mkInfo.PodCIDR, mkInfo.ContainerIP)
		}
	}

	l.mgr.InvalidateCache()
	return nil
}

func (l *HostK8sLink) Revert(subLevel string) error {
	mkInfo := l.mgr.MinikubeInfo()
	if mkInfo == nil {
		return fmt.Errorf("未找到运行中的 Minikube 容器，跳过还原")
	}

	if subLevel == "" || subLevel == "service" {
		rules := l.serviceRules()
		if len(rules) > 0 {
			l.mgr.batchRemoveRules("🖥️ host-k8s.service: 还原宿主机访问 K8s Service", rules)
		}
		if mkInfo.ServiceCIDR != "" {
			l.mgr.routeMgr.RemoveRoute(mkInfo.ServiceCIDR, mkInfo.ContainerIP)
		}
		l.mgr.dnsMgr.RevertDNS()
	}

	if subLevel == "" || subLevel == "pod" {
		rules := l.podRules()
		if len(rules) > 0 {
			l.mgr.batchRemoveRules("🖥️ host-k8s.pod: 还原宿主机访问 K8s Pod", rules)
		}
		if mkInfo.PodCIDR != "" {
			l.mgr.routeMgr.RemoveRoute(mkInfo.PodCIDR, mkInfo.ContainerIP)
		}
	}

	l.mgr.InvalidateCache()
	return nil
}

func (l *HostK8sLink) Status(subLevel string) []LinkStatus {
	var results []LinkStatus
	mkInfo := l.mgr.MinikubeInfo()

	if subLevel == "" || subLevel == "service" {
		st := LinkStatus{Name: "host-k8s.service", Description: "Host ↔ K8s Service"}
		rules := l.serviceRules()
		l.mgr.rulesToStatus(rules, &st)

		// Service 路由检查
		if mkInfo != nil && mkInfo.ServiceCIDR != "" {
			ok := l.mgr.routeMgr.RouteExists(mkInfo.ServiceCIDR, mkInfo.ContainerIP)
			st.AppendCheckTyped(fmt.Sprintf("route %s via %s", mkInfo.ServiceCIDR, mkInfo.ContainerIP), ok, "route")
		}

		// DNS 检查
		dnsOK, dnsIP, _ := l.mgr.dnsMgr.CheckDNSStatus()
		if dnsIP == "" {
			dnsIP = "N/A"
		}
		st.AppendCheckTyped(fmt.Sprintf("DNS config (%s)", dnsIP), dnsOK, "dns")

		st.ComputeStatus()
		results = append(results, st)
	}

	if subLevel == "" || subLevel == "pod" {
		st := LinkStatus{Name: "host-k8s.pod", Description: "Host ↔ K8s Pod"}
		rules := l.podRules()
		l.mgr.rulesToStatus(rules, &st)

		// Pod 路由检查
		if mkInfo != nil && mkInfo.PodCIDR != "" {
			ok := l.mgr.routeMgr.RouteExists(mkInfo.PodCIDR, mkInfo.ContainerIP)
			st.AppendCheckTyped(fmt.Sprintf("route %s via %s", mkInfo.PodCIDR, mkInfo.ContainerIP), ok, "route")
		}

		st.ComputeStatus()
		results = append(results, st)
	}

	return results
}
