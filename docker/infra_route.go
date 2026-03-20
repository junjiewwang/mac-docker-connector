package main

import (
	"fmt"
	"regexp"
	"strings"
)

// RouteManager 路由管理器
type RouteManager struct{}

// RouteExists 检查路由是否存在
func (r *RouteManager) RouteExists(network, gateway string) bool {
	out, err := runCommand("ip", "route", "show")
	if err != nil {
		return false
	}
	pattern := `^` + regexp.QuoteMeta(network) + `\s+via\s+` + regexp.QuoteMeta(gateway)
	re := regexp.MustCompile(pattern)
	for _, line := range strings.Split(out, "\n") {
		if re.MatchString(strings.TrimSpace(line)) {
			return true
		}
	}
	return false
}

// AddRoute 添加路由（如果不存在）
func (r *RouteManager) AddRoute(network, gateway string) (bool, error) {
	if r.RouteExists(network, gateway) {
		if debug {
			fmt.Printf("[ROUTE] 路由已存在: %s via %s\n", network, gateway)
		}
		return false, nil
	}

	_, err := runCommandSudo("ip", "route", "add", network, "via", gateway)
	if err != nil {
		return false, fmt.Errorf("添加路由失败: %s via %s => %v", network, gateway, err)
	}
	fmt.Printf("[ROUTE] 已添加路由: %s via %s\n", network, gateway)
	return true, nil
}

// RemoveRoute 删除路由（如果存在）
func (r *RouteManager) RemoveRoute(network, gateway string) (bool, error) {
	if !r.RouteExists(network, gateway) {
		if debug {
			fmt.Printf("[ROUTE] 路由不存在，无需删除: %s via %s\n", network, gateway)
		}
		return false, nil
	}

	_, err := runCommandSudo("ip", "route", "del", network, "via", gateway)
	if err != nil {
		return false, fmt.Errorf("删除路由失败: %s via %s => %v", network, gateway, err)
	}
	fmt.Printf("[ROUTE] 已删除路由: %s via %s\n", network, gateway)
	return true, nil
}

// GetRoutes 获取当前路由表，返回 map[destination]interface
func (r *RouteManager) GetRoutes() map[string]string {
	routes := make(map[string]string)
	out, err := runCommand("route", "-n")
	if err != nil {
		return routes
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 2 {
			routes[fields[0]] = fields[len(fields)-1]
		}
	}
	return routes
}
