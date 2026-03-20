package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	dnsConfDir  = "/etc/systemd/resolved.conf.d"
	dnsConfFile = "/etc/systemd/resolved.conf.d/minikube-dns.conf"
)

// DnsManager DNS 配置管理器（systemd-resolved）
type DnsManager struct {
	network *NetworkInfoProvider
}

// NewDnsManager 创建新的 DNS 管理器
func NewDnsManager(network *NetworkInfoProvider) *DnsManager {
	return &DnsManager{network: network}
}

// ConfigureDNS 配置 Minikube DNS
// 向 systemd-resolved 写入 DNS 转发配置，使 .cluster.local 域名解析到 Minikube DNS
func (d *DnsManager) ConfigureDNS(minikubeInfo *MinikubeInfo) (bool, error) {
	if minikubeInfo == nil {
		return false, fmt.Errorf("未找到运行中的 Minikube 容器，跳过 DNS 配置")
	}

	dnsIP := d.network.GetMinikubeDNSIP()
	if dnsIP == "" {
		return false, fmt.Errorf("无法获取 Minikube DNS 服务 IP，跳过 DNS 配置")
	}
	fmt.Printf("[DNS] Minikube DNS 服务 IP: %s\n", dnsIP)

	// 检查 systemctl 是否可用
	if !commandExists("systemctl") {
		return false, fmt.Errorf("systemctl 命令不可用，跳过 DNS 配置")
	}

	// 检查 systemd-resolved 是否运行
	out, err := runCommand("systemctl", "is-active", "systemd-resolved")
	if err != nil || strings.TrimSpace(out) != "active" {
		return false, fmt.Errorf("systemd-resolved 服务未运行，跳过 DNS 配置")
	}

	// 确保配置目录存在
	if err := os.MkdirAll(dnsConfDir, 0755); err != nil {
		return false, fmt.Errorf("创建 DNS 配置目录失败: %v", err)
	}

	// 检查现有配置是否正确
	needsUpdate := true
	if data, err := os.ReadFile(dnsConfFile); err == nil {
		if strings.Contains(string(data), "DNS="+dnsIP) {
			if debug {
				fmt.Printf("[DNS] DNS 配置已存在且正确: %s\n", dnsIP)
			}
			needsUpdate = false
		}
	}

	if needsUpdate {
		fmt.Printf("[DNS] 写入 DNS 配置文件: %s\n", dnsConfFile)
		configContent := fmt.Sprintf(`# Minikube DNS 配置
# 自动生成于: %s
[Resolve]
DNS=%s
Domains=cluster.local
`, time.Now().Format("2006-01-02 15:04:05"), dnsIP)

		if err := os.WriteFile(dnsConfFile, []byte(configContent), 0644); err != nil {
			return false, fmt.Errorf("写入 DNS 配置文件失败: %v", err)
		}
		fmt.Println("[DNS] ✅ DNS 配置文件已创建")

		// 重启 systemd-resolved
		fmt.Println("[DNS] 重启 systemd-resolved 服务...")
		if _, err := runCommandSudo("systemctl", "restart", "systemd-resolved"); err != nil {
			return false, fmt.Errorf("重启 systemd-resolved 失败: %v", err)
		}
		fmt.Println("[DNS] ✅ systemd-resolved 服务已重启")

		// 等待服务完全启动
		time.Sleep(1 * time.Second)

		out, err := runCommand("systemctl", "is-active", "systemd-resolved")
		if err != nil || strings.TrimSpace(out) != "active" {
			return false, fmt.Errorf("systemd-resolved 服务启动失败")
		}
		fmt.Println("[DNS] ✅ DNS 配置已生效")
		d.VerifyDNS(dnsIP)
	} else {
		fmt.Println("[DNS] ✅ DNS 配置无需更新")
		d.VerifyDNS(dnsIP)
	}

	return true, nil
}

// RevertDNS 还原 DNS 配置
func (d *DnsManager) RevertDNS() {
	if _, err := os.Stat(dnsConfFile); os.IsNotExist(err) {
		if debug {
			fmt.Println("[DNS] DNS 配置文件不存在，无需还原")
		}
		return
	}

	if _, err := runCommandSudo("rm", "-f", dnsConfFile); err != nil {
		fmt.Printf("[DNS] 删除 DNS 配置文件失败: %v\n", err)
		return
	}
	fmt.Printf("[DNS] 已删除 DNS 配置文件: %s\n", dnsConfFile)

	if commandExists("systemctl") {
		out, _ := runCommand("systemctl", "is-active", "systemd-resolved")
		if strings.TrimSpace(out) == "active" {
			fmt.Println("[DNS] 重启 systemd-resolved 服务...")
			runCommandSudo("systemctl", "restart", "systemd-resolved")
			fmt.Println("[DNS] ✅ systemd-resolved 服务已重启")
		}
	}
}

// VerifyDNS 验证 DNS 解析
func (d *DnsManager) VerifyDNS(dnsIP string) {
	if !commandExists("nslookup") {
		if debug {
			fmt.Println("[DNS] nslookup 命令不可用，跳过 DNS 解析验证")
		}
		return
	}

	out, err := runCommand("nslookup", "kubernetes.default.svc.cluster.local", dnsIP)
	if err == nil && out != "" {
		fmt.Println("[DNS] ✅ DNS 解析验证成功: kubernetes.default.svc.cluster.local")
	} else {
		fmt.Println("[DNS] ⚠️  DNS 解析验证失败，可能需要等待 DNS 服务完全启动")
	}
}

// CheckDNSStatus 检查 DNS 状态，返回 (配置存在, dnsIP, 搜索域)
func (d *DnsManager) CheckDNSStatus() (exists bool, dnsIP string, domains string) {
	data, err := os.ReadFile(dnsConfFile)
	if err != nil {
		return false, "", ""
	}
	content := string(data)

	dnsRe := regexp.MustCompile(`(?m)^DNS=(.+)$`)
	domainsRe := regexp.MustCompile(`(?m)^Domains=(.+)$`)

	if match := dnsRe.FindStringSubmatch(content); len(match) > 1 {
		dnsIP = strings.TrimSpace(match[1])
	}
	if match := domainsRe.FindStringSubmatch(content); len(match) > 1 {
		domains = strings.TrimSpace(match[1])
	}

	return dnsIP != "", dnsIP, domains
}

// IsDNSConfigured 检查 DNS 是否已配置
func (d *DnsManager) IsDNSConfigured() bool {
	_, err := os.Stat(dnsConfFile)
	if os.IsNotExist(err) {
		return false
	}
	exists, _, _ := d.CheckDNSStatus()
	return exists
}
