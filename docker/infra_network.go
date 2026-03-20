package main

import (
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
)

// BridgeInfo Docker 网桥信息
type BridgeInfo struct {
	Name      string `json:"name"`       // 网桥名称，如 br-abc123 或 docker0
	Subnet    string `json:"subnet"`     // 子网 CIDR，如 172.18.0.0/16
	NetworkID string `json:"network_id"` // Docker 网络 ID
}

// MinikubeInfo Minikube 集群信息
type MinikubeInfo struct {
	BridgeName  string `json:"bridge_name"`            // Minikube 网桥名称
	ContainerIP string `json:"container_ip"`            // Minikube 容器 IP
	Subnet      string `json:"subnet"`                  // 网桥子网
	ServiceCIDR string `json:"service_cidr,omitempty"`  // Service CIDR
	PodCIDR     string `json:"pod_cidr,omitempty"`      // Pod CIDR
}

// kubectlCheckOnce 确保 kubectl 可用性检查只执行一次
var (
	kubectlCheckOnce   sync.Once
	kubectlIsAvailable bool
)

// kubectlAvailable 检查 kubectl 是否可用（命令存在 + kubeconfig 可达）
// 结果会被缓存，避免每次调用都检查。首次检查如果失败会打印告警日志。
func kubectlAvailable() bool {
	kubectlCheckOnce.Do(func() {
		// 1. 检查 kubectl 命令是否存在
		if !commandExists("kubectl") {
			fmt.Println("[NETWORK] ⚠️ kubectl 未安装，K8s 相关功能不可用")
			kubectlIsAvailable = false
			return
		}

		// 2. 检查 kubeconfig 是否可达
		// kubectl 依赖 $HOME/.kube/config 或 $KUBECONFIG 环境变量
		home := os.Getenv("HOME")
		kubeconfig := os.Getenv("KUBECONFIG")
		if home == "" && kubeconfig == "" {
			fmt.Println("[NETWORK] ⚠️ HOME 和 KUBECONFIG 环境变量均未设置，kubectl 将无法定位 kubeconfig")
			kubectlIsAvailable = false
			return
		}

		// 3. 快速验证：执行 kubectl cluster-info 检查连通性
		_, err := runCommand("kubectl", "cluster-info", "--request-timeout=3s")
		if err != nil {
			fmt.Printf("[NETWORK] ⚠️ kubectl 不可用（cluster-info 失败: %v），K8s 相关功能已禁用\n", err)
			kubectlIsAvailable = false
			return
		}

		fmt.Println("[NETWORK] ✅ kubectl 可用，K8s 功能已启用")
		kubectlIsAvailable = true
	})
	return kubectlIsAvailable
}

// NetworkInfoProvider 网络信息提供者
type NetworkInfoProvider struct{}

// GetPhysicalInterface 获取物理网卡名称（默认路由的出接口）
func (n *NetworkInfoProvider) GetPhysicalInterface() string {
	out, err := runCommand("ip", "route")
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`default.*dev\s+(\S+)`)
	match := re.FindStringSubmatch(out)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

// GetDockerBridges 获取所有 Docker 网桥信息（批量加速版）
func (n *NetworkInfoProvider) GetDockerBridges() []BridgeInfo {
	var bridges []BridgeInfo

	// 获取所有 bridge 驱动网络 ID
	out, err := runCommand("docker", "network", "ls", "-q", "--filter", "driver=bridge")
	if err != nil || strings.TrimSpace(out) == "" {
		return bridges
	}

	networkIDs := strings.Split(strings.TrimSpace(out), "\n")
	if len(networkIDs) == 0 {
		return bridges
	}

	// 一次性 inspect 所有网络，减少子进程调用次数
	// 使用逗号分隔多个 IPAM Config（IPv4+IPv6 双栈场景），避免多 CIDR 拼接为无效字符串
	inspectFmt := `{{.ID}}|{{index .Options "com.docker.network.bridge.name"}}|{{range $i, $c := .IPAM.Config}}{{if $i}},{{end}}{{$c.Subnet}}{{end}}`
	args := append([]string{"docker", "network", "inspect"}, networkIDs...)
	args = append(args, "--format", inspectFmt)

	out, err = runCommand(args...)
	if err != nil {
		return bridges
	}

	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		nid := strings.TrimSpace(parts[0])
		bridgeName := strings.TrimSpace(parts[1])
		subnetRaw := strings.TrimSpace(parts[2])

		if bridgeName == "" || bridgeName == "<no value>" {
			idLen := 12
			if len(nid) < idLen {
				idLen = len(nid)
			}
			bridgeName = "br-" + nid[:idLen]
		}

		// 检查接口是否存在
		if !n.InterfaceExists(bridgeName) {
			continue
		}

		// 从可能的多个 CIDR（逗号分隔）中提取第一个合法的 IPv4 CIDR
		subnet := extractFirstIPv4CIDR(subnetRaw)
		if subnet != "" {
			bridges = append(bridges, BridgeInfo{
				Name:      bridgeName,
				Subnet:    subnet,
				NetworkID: nid,
			})
		}
	}

	return bridges
}

// GetMinikubeInfo 获取 Minikube 信息（减少 docker 调用次数）
func (n *NetworkInfoProvider) GetMinikubeInfo() *MinikubeInfo {
	// 查找 minikube 容器
	out, err := runCommand("docker", "ps", "--filter", "name=minikube", "--format", "{{.ID}}")
	if err != nil {
		return nil
	}
	containerID := strings.TrimSpace(out)
	if containerID == "" {
		return nil
	}

	// 单次 inspect 获取 networkID 与 container IP
	out, err = runCommand("docker", "inspect", containerID, "--format",
		"{{range .NetworkSettings.Networks}}{{.NetworkID}}|{{.IPAddress}}{{end}}")
	if err != nil || !strings.Contains(out, "|") {
		return nil
	}
	parts := strings.SplitN(strings.TrimSpace(out), "|", 2)
	if len(parts) != 2 {
		return nil
	}
	networkID := strings.TrimSpace(parts[0])
	containerIP := strings.TrimSpace(parts[1])

	// 单次 network inspect 获取 bridge name 与 subnet
	// 使用逗号分隔多个 IPAM Config（IPv4+IPv6 双栈场景）
	inspectFmt := `{{index .Options "com.docker.network.bridge.name"}}|{{range $i, $c := .IPAM.Config}}{{if $i}},{{end}}{{$c.Subnet}}{{end}}`
	out, err = runCommand("docker", "network", "inspect", networkID, "--format", inspectFmt)
	if err != nil || !strings.Contains(out, "|") {
		return nil
	}
	parts = strings.SplitN(strings.TrimSpace(out), "|", 2)
	if len(parts) != 2 {
		return nil
	}
	bridgeName := strings.TrimSpace(parts[0])
	// 从可能的多个 CIDR（逗号分隔）中提取第一个合法的 IPv4 CIDR
	subnet := extractFirstIPv4CIDR(strings.TrimSpace(parts[1]))

	if bridgeName == "" || bridgeName == "<no value>" {
		idLen := 12
		if len(networkID) < idLen {
			idLen = len(networkID)
		}
		bridgeName = "br-" + networkID[:idLen]
	}

	info := &MinikubeInfo{
		BridgeName:  bridgeName,
		ContainerIP: containerIP,
		Subnet:      subnet,
	}

	// 获取 Service CIDR 和 Pod CIDR
	info.ServiceCIDR = n.getServiceCIDR()
	info.PodCIDR = n.getPodCIDR()

	return info
}

// GetMinikubeDNSIP 获取 Minikube DNS 服务 IP
func (n *NetworkInfoProvider) GetMinikubeDNSIP() string {
	if !kubectlAvailable() {
		return ""
	}

	for _, svcName := range []string{"kube-dns", "coredns"} {
		out, err := runCommand("kubectl", "--request-timeout=3s", "get", "svc", "-n", "kube-system", svcName,
			"-o", "jsonpath={.spec.clusterIP}")
		if err == nil {
			dnsIP := strings.TrimSpace(out)
			if dnsIP != "" {
				return dnsIP
			}
		}
	}
	return ""
}

// GetExternalInterface 获取外部（默认路由）接口名称
func (n *NetworkInfoProvider) GetExternalInterface() string {
	return n.GetPhysicalInterface()
}

// InterfaceExists 检查网络接口是否存在
func (n *NetworkInfoProvider) InterfaceExists(iface string) bool {
	err := runCommandSilent("ip", "link", "show", iface)
	return err == nil
}

// getServiceCIDR 获取 Kubernetes Service CIDR
func (n *NetworkInfoProvider) getServiceCIDR() string {
	if !kubectlAvailable() {
		return ""
	}

	type cidrStrategy struct {
		name    string
		args    []string
		pattern string
	}

	strategies := []cidrStrategy{
		{
			name: "API Server Pod",
			args: []string{"kubectl", "--request-timeout=3s", "get", "pod", "-n", "kube-system",
				"-l", "component=kube-apiserver",
				"-o", "jsonpath={.items[0].spec.containers[0].command}"},
			pattern: `service-cluster-ip-range=([0-9./]+)`,
		},
		{
			name: "kubeadm-config",
			args: []string{"kubectl", "--request-timeout=3s", "get", "cm", "-n", "kube-system", "kubeadm-config",
				"-o", "jsonpath={.data.ClusterConfiguration}"},
			pattern: `serviceSubnet:\s*([0-9./]+)`,
		},
		{
			name: "kube-proxy",
			args: []string{"kubectl", "--request-timeout=3s", "get", "cm", "-n", "kube-system", "kube-proxy",
				"-o", `jsonpath={.data.config\.conf}`},
			pattern: `clusterCIDR:\s*"?([0-9./]+)"?`,
		},
	}

	for _, s := range strategies {
		out, err := runCommand(s.args...)
		if err != nil || strings.TrimSpace(out) == "" {
			continue
		}
		re := regexp.MustCompile(s.pattern)
		match := re.FindStringSubmatch(out)
		if len(match) > 1 {
			if debug {
				fmt.Printf("[NETWORK] 通过 %s 获取 Service CIDR: %s\n", s.name, match[1])
			}
			return match[1]
		}
	}

	// 备用方案：通过 kubernetes service IP 推断
	out, err := runCommand("kubectl", "--request-timeout=3s", "get", "svc", "-n", "default", "kubernetes",
		"-o", "jsonpath={.spec.clusterIP}")
	if err == nil {
		serviceIP := strings.TrimSpace(out)
		ipPattern := regexp.MustCompile(`^\d+\.\d+\.\d+\.\d+$`)
		if ipPattern.MatchString(serviceIP) {
			parts := strings.Split(serviceIP, ".")
			cidr := parts[0] + "." + parts[1] + ".0.0/16"
			if debug {
				fmt.Printf("[NETWORK] 通过 kubernetes service 推断 Service CIDR: %s\n", cidr)
			}
			return cidr
		}
	}

	return ""
}

// getPodCIDR 获取 Kubernetes Pod CIDR
// 优先获取集群级 cluster-cidr（覆盖整个集群），
// 而非 Node 级 spec.podCIDR（仅分配给单个节点的子网，范围较小）
func (n *NetworkInfoProvider) getPodCIDR() string {
	if !kubectlAvailable() {
		return ""
	}

	type cidrStrategy struct {
		name    string
		args    []string
		pattern string
	}

	strategies := []cidrStrategy{
		{
			name: "kube-controller-manager --cluster-cidr（集群级）",
			args: []string{"kubectl", "--request-timeout=3s", "get", "pod", "-n", "kube-system",
				"-l", "component=kube-controller-manager",
				"-o", "jsonpath={.items[0].spec.containers[0].command}"},
			pattern: `cluster-cidr=([0-9./]+)`,
		},
		{
			name: "kubeadm-config podSubnet（集群级）",
			args: []string{"kubectl", "--request-timeout=3s", "get", "cm", "-n", "kube-system", "kubeadm-config",
				"-o", "jsonpath={.data.ClusterConfiguration}"},
			pattern: `podSubnet:\s*([0-9./]+)`,
		},
		{
			name: "kube-proxy clusterCIDR（集群级）",
			args: []string{"kubectl", "--request-timeout=3s", "get", "cm", "-n", "kube-system", "kube-proxy",
				"-o", `jsonpath={.data.config\.conf}`},
			pattern: `clusterCIDR:\s*"?([0-9./]+)"?`,
		},
		{
			name: "Node spec.podCIDR（节点级，备用）",
			args: []string{"kubectl", "--request-timeout=3s", "get", "nodes",
				"-o", "jsonpath={.items[0].spec.podCIDR}"},
			pattern: `^([0-9./]+)$`,
		},
	}

	for _, s := range strategies {
		if debug {
			fmt.Printf("[NETWORK] 尝试获取 Pod CIDR: %s...\n", s.name)
		}
		out, err := runCommand(s.args...)
		if err != nil || strings.TrimSpace(out) == "" {
			continue
		}
		re := regexp.MustCompile(s.pattern)
		match := re.FindStringSubmatch(strings.TrimSpace(out))
		if len(match) > 1 {
			if debug {
				fmt.Printf("[NETWORK] 通过 %s 获取 Pod CIDR: %s\n", s.name, match[1])
			}
			return match[1]
		}
	}

	if debug {
		fmt.Println("[NETWORK] 无法获取 Pod CIDR")
	}
	return ""
}

// extractFirstIPv4CIDR 从可能包含多个 CIDR（逗号分隔）的字符串中提取第一个合法的 IPv4 CIDR
// 例如输入 "192.168.49.0/24,fc00:f853:ccd:e793::/64" 返回 "192.168.49.0/24"
// 如果输入没有逗号分隔符（旧格式拼接），也尝试通过正则提取
func extractFirstIPv4CIDR(raw string) string {
	if raw == "" {
		return ""
	}

	// 优先按逗号分隔，取第一个合法的 IPv4 CIDR
	for _, cidr := range strings.Split(raw, ",") {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		ip, _, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		// 只保留 IPv4
		if ip.To4() != nil {
			return cidr
		}
	}

	// 兜底：如果没有逗号分隔（可能是旧格式拼接），用正则提取第一个 IPv4 CIDR
	re := regexp.MustCompile(`(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}/\d{1,2})`)
	match := re.FindString(raw)
	if match != "" {
		_, _, err := net.ParseCIDR(match)
		if err == nil {
			return match
		}
	}

	return ""
}
