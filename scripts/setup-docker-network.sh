#!/bin/bash

#############################################
# Lima Docker 虚拟机网络配置脚本
# 功能：
# 0. 检查并修复 Docker iptables 配置（关键前置步骤）
# 1. 配置所有 Docker 网桥访问外网
# 2. 配置 tun0 到所有 Docker 网桥的转发规则
# 3. 配置 Minikube 集群子网路由
# 4. 配置 Minikube DNS 解析
# 5. 配置其他 Docker 网桥与 Minikube 的通信
# 6. 清理无效规则
# 7. 生成网络拓扑图
#############################################

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 日志函数
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_debug() {
    echo -e "${BLUE}[DEBUG]${NC} $1" >&2
}

# 检查命令是否存在
check_command() {
    if ! command -v "$1" &> /dev/null; then
        log_error "命令 $1 未找到，请先安装"
        exit 1
    fi
}

# 检查并修复 Docker iptables 配置
check_docker_iptables_config() {
    log_info "==========================================="
    log_info "🔍 检查 Docker iptables 配置"
    log_info "==========================================="
    echo ""
    
    local daemon_json="/etc/docker/daemon.json"
    local needs_restart=false
    local config_correct=false
    
    # 检查配置文件是否存在
    if [ ! -f "$daemon_json" ]; then
        log_warn "Docker 配置文件不存在: $daemon_json"
        log_info "创建新的配置文件..."
        
        sudo mkdir -p /etc/docker
        echo '{
  "iptables": false
}' | sudo tee "$daemon_json" > /dev/null
        
        if [ $? -eq 0 ]; then
            log_info "✅ 配置文件已创建"
            needs_restart=true
        else
            log_error "❌ 创建配置文件失败"
            exit 1
        fi
    else
        log_info "检查配置文件: $daemon_json"
        
        # 检查 iptables 配置
        if grep -q '"iptables"' "$daemon_json"; then
            local iptables_value=$(grep '"iptables"' "$daemon_json" | grep -oP ':\s*\K(true|false)' | tr -d ' ')
            
            if [ "$iptables_value" = "false" ]; then
                log_info "✅ Docker iptables 配置正确: iptables = false"
                config_correct=true
            else
                log_warn "⚠️  Docker iptables 配置错误: iptables = $iptables_value"
                log_info "需要修改为: iptables = false"
                
                # 备份原配置
                sudo cp "$daemon_json" "${daemon_json}.backup.$(date +%Y%m%d_%H%M%S)"
                log_info "已备份原配置到: ${daemon_json}.backup.$(date +%Y%m%d_%H%M%S)"
                
                # 修改配置
                sudo sed -i 's/"iptables"\s*:\s*true/"iptables": false/g' "$daemon_json"
                
                if grep -q '"iptables"\s*:\s*false' "$daemon_json"; then
                    log_info "✅ 配置已修改"
                    needs_restart=true
                else
                    log_error "❌ 配置修改失败"
                    exit 1
                fi
            fi
        else
            log_warn "⚠️  配置文件中未找到 iptables 配置项"
            log_info "添加 iptables 配置..."
            
            # 备份原配置
            sudo cp "$daemon_json" "${daemon_json}.backup.$(date +%Y%m%d_%H%M%S)"
            
            # 使用 jq 添加配置（如果可用）
            if command -v jq &> /dev/null; then
                sudo jq '. + {"iptables": false}' "$daemon_json" | sudo tee "${daemon_json}.tmp" > /dev/null
                sudo mv "${daemon_json}.tmp" "$daemon_json"
            else
                # 手动添加配置
                local content=$(sudo cat "$daemon_json")
                if [[ "$content" =~ ^\{.*\}$ ]]; then
                    # 在最后一个 } 前添加配置
                    echo "$content" | sudo sed 's/}$/,\n  "iptables": false\n}/' | sudo tee "$daemon_json" > /dev/null
                else
                    log_error "❌ 配置文件格式不正确，无法自动修改"
                    log_info "请手动在 $daemon_json 中添加: \"iptables\": false"
                    exit 1
                fi
            fi
            
            if grep -q '"iptables"\s*:\s*false' "$daemon_json"; then
                log_info "✅ 配置已添加"
                needs_restart=true
            else
                log_error "❌ 配置添加失败"
                exit 1
            fi
        fi
    fi
    
    # 如果需要重启 Docker
    if [ "$needs_restart" = true ]; then
        log_warn "⚠️  Docker 配置已修改，需要重启 Docker 服务"
        echo ""
        log_info "正在重启 Docker 服务..."
        
        if sudo systemctl restart docker; then
            log_info "✅ Docker 服务重启成功"
            
            # 等待 Docker 完全启动
            log_info "等待 Docker 服务完全启动..."
            sleep 5
            
            # 验证 Docker 是否正常运行
            if docker ps &> /dev/null; then
                log_info "✅ Docker 服务运行正常"
            else
                log_error "❌ Docker 服务启动异常"
                exit 1
            fi
        else
            log_error "❌ Docker 服务重启失败"
            log_info "请手动执行: sudo systemctl restart docker"
            exit 1
        fi
    fi
    
    # 最终验证
    if [ "$config_correct" = false ]; then
        # 重新检查配置
        if grep -q '"iptables"\s*:\s*false' "$daemon_json"; then
            log_info "✅ Docker iptables 配置验证通过"
        else
            log_error "❌ Docker iptables 配置验证失败"
            log_error "当前配置内容:"
            sudo cat "$daemon_json" | sed 's/^/  /'
            exit 1
        fi
    fi
    
    echo ""
    log_info "==========================================="
    log_info "✅ Docker iptables 配置检查完成"
    log_info "==========================================="
    echo ""
}

# 检查规则是否存在
rule_exists() {
    local table="$1"
    local chain="$2"
    local rule="$3"
    
    if [ "$table" = "filter" ]; then
        sudo iptables -C "$chain" $rule 2>/dev/null
    else
        sudo iptables -t "$table" -C "$chain" $rule 2>/dev/null
    fi
}

# 添加 iptables 规则（如果不存在）
add_rule_if_not_exists() {
    local table="$1"
    local chain="$2"
    shift 2
    local rule="$@"
    
    if rule_exists "$table" "$chain" "$rule"; then
        log_debug "规则已存在: iptables -t $table -A $chain $rule"
        return 0
    fi
    
    if [ "$table" = "filter" ]; then
        sudo iptables -A "$chain" $rule
    else
        sudo iptables -t "$table" -A "$chain" $rule
    fi
    log_info "已添加规则: iptables -t $table -A $chain $rule"
}

# 检查路由是否存在
route_exists() {
    local network="$1"
    local gateway="$2"
    
    ip route show | grep -q "^${network} via ${gateway}"
}

# 添加路由（如果不存在）
add_route_if_not_exists() {
    local network="$1"
    local gateway="$2"
    
    if route_exists "$network" "$gateway"; then
        log_debug "路由已存在: $network via $gateway"
        return 0
    fi
    
    sudo ip route add "$network" via "$gateway"
    log_info "已添加路由: $network via $gateway"
}

# 获取物理网卡名称
get_physical_interface() {
    # 获取默认路由的网卡
    ip route | grep default | awk '{print $5}' | head -n 1
}

# 获取所有 Docker 网桥信息
get_docker_bridges() {
    local bridges=""
    
    # 获取所有 Docker 网络
    for network_id in $(docker network ls -q --filter driver=bridge); do
        local bridge_name=$(docker network inspect "$network_id" --format='{{index .Options "com.docker.network.bridge.name"}}' 2>/dev/null || echo "")
        
        # 如果网桥名称为空，使用默认命名规则
        if [ -z "$bridge_name" ] || [ "$bridge_name" = "<no value>" ]; then
            bridge_name="br-${network_id:0:12}"
        fi
        
        # 验证网桥是否存在
        if ip link show "$bridge_name" &>/dev/null; then
            local subnet=$(docker network inspect "$network_id" --format='{{range .IPAM.Config}}{{.Subnet}}{{end}}')
            if [ -n "$subnet" ]; then
                if [ -z "$bridges" ]; then
                    bridges="$bridge_name:$subnet"
                else
                    bridges="$bridges"$'\n'"$bridge_name:$subnet"
                fi
            fi
        fi
    done
    
    if [ -n "$bridges" ]; then
        echo "$bridges"
    fi
}

# 获取 Minikube 信息
get_minikube_info() {
    local container_id=$(docker ps --filter "name=minikube" --format "{{.ID}}" 2>/dev/null)
    
    if [ -z "$container_id" ]; then
        return 1
    fi
    
    local network_id=$(docker inspect "$container_id" --format='{{range $k, $v := .NetworkSettings.Networks}}{{$v.NetworkID}}{{end}}')
    local bridge_name=$(docker network inspect "$network_id" --format='{{index .Options "com.docker.network.bridge.name"}}' 2>/dev/null || echo "")
    
    if [ -z "$bridge_name" ] || [ "$bridge_name" = "<no value>" ]; then
        bridge_name="br-${network_id:0:12}"
    fi
    
    local container_ip=$(docker inspect "$container_id" --format='{{range $k, $v := .NetworkSettings.Networks}}{{$v.IPAddress}}{{end}}')
    local subnet=$(docker network inspect "$network_id" --format='{{range .IPAM.Config}}{{.Subnet}}{{end}}')
    
    # 获取 Kubernetes Service CIDR（使用快速方法）
    local service_cidr=""
    if command -v kubectl &> /dev/null; then
        log_debug "正在获取 Kubernetes Service CIDR..."
        
        # 方法1: 查询 API Server Pod 启动参数（最快，<1秒）
        log_debug "尝试方法1: 查询 API Server Pod..."
        service_cidr=$(kubectl get pod -n kube-system -l component=kube-apiserver -o jsonpath='{.items[0].spec.containers[0].command[*]}' 2>/dev/null | tr ',' '\n' | tr ' ' '\n' | grep -E '^--service-cluster-ip-range=' | cut -d= -f2 | head -n 1)
        
        # 方法2: 查询 kubeadm-config ConfigMap（备用，<1秒）
        if [ -z "$service_cidr" ]; then
            log_debug "尝试方法2: 查询 kubeadm-config..."
            service_cidr=$(kubectl get cm -n kube-system kubeadm-config -o jsonpath='{.data.ClusterConfiguration}' 2>/dev/null | grep -oP 'serviceSubnet:\s*\K[0-9./]+')
        fi
        
        # 方法3: 查询 kube-controller-manager Pod（备用）
        if [ -z "$service_cidr" ]; then
            log_debug "尝试方法3: 查询 kube-controller-manager..."
            service_cidr=$(kubectl get pod -n kube-system -l component=kube-controller-manager -o jsonpath='{.items[0].spec.containers[0].command[*]}' 2>/dev/null | tr ',' '\n' | tr ' ' '\n' | grep -E '^--service-cluster-ip-range=' | cut -d= -f2 | head -n 1)
        fi
        
        # 方法4: 通过 kubernetes service IP 推断（最后备用）
        if [ -z "$service_cidr" ]; then
            log_debug "尝试方法4: 通过 kubernetes service 推断..."
            local k8s_svc_ip=$(kubectl get svc -n default kubernetes -o jsonpath='{.spec.clusterIP}' 2>/dev/null)
            if [ -n "$k8s_svc_ip" ]; then
                # 根据第一个 IP 推断网段（通常是 10.96.0.0/12 或 10.96.0.0/16）
                local first_octet=$(echo "$k8s_svc_ip" | cut -d. -f1)
                local second_octet=$(echo "$k8s_svc_ip" | cut -d. -f2)
                
                # 常见的 Service CIDR 配置
                if [ "$first_octet" = "10" ] && [ "$second_octet" -ge 96 ] && [ "$second_octet" -lt 112 ]; then
                    service_cidr="10.96.0.0/12"  # 默认 kubeadm 配置
                else
                    service_cidr="${first_octet}.${second_octet}.0.0/16"
                fi
                log_debug "推断的 Service CIDR: $service_cidr (基于 kubernetes service IP: $k8s_svc_ip)"
            fi
        fi
        
        if [ -n "$service_cidr" ]; then
            log_debug "成功获取 Service CIDR: $service_cidr"
        else
            log_debug "无法获取 Service CIDR"
        fi
    fi
    
    echo "$bridge_name:$container_ip:$subnet:$service_cidr"
}

# 清理无效的网桥规则
cleanup_invalid_rules() {
    log_info "开始清理无效的网桥规则..."
    
    # 获取当前存在的网桥
    local existing_bridges=$(ip link show | grep -oP '(?<=: )br-[a-f0-9]+' | sort -u)
    
    # 检查 filter 表的 FORWARD 链
    local line_num=1
    while read -r line; do
        if [[ "$line" =~ br-[a-f0-9]+ ]]; then
            local bridge=$(echo "$line" | grep -oP 'br-[a-f0-9]+' | head -n 1)
            if ! echo "$existing_bridges" | grep -q "^${bridge}$"; then
                log_warn "发现无效网桥规则: $line"
                # 注意：实际删除需要谨慎，这里只是标记
                # sudo iptables -D FORWARD $line_num
            fi
        fi
        ((line_num++))
    done < <(sudo iptables -L FORWARD -n --line-numbers | tail -n +3)
    
    # 检查 nat 表的 POSTROUTING 链
    line_num=1
    while read -r line; do
        if [[ "$line" =~ br-[a-f0-9]+ ]]; then
            local bridge=$(echo "$line" | grep -oP 'br-[a-f0-9]+' | head -n 1)
            if ! echo "$existing_bridges" | grep -q "^${bridge}$"; then
                log_warn "发现无效 NAT 规则: $line"
            fi
        fi
        ((line_num++))
    done < <(sudo iptables -t nat -L POSTROUTING -n --line-numbers | tail -n +3)
    
    log_info "清理检查完成"
}

# 配置 Docker 网桥访问外网
configure_docker_bridges_nat() {
    log_info "=========================================="
    log_info "1. 配置 Docker 网桥访问外网"
    log_info "=========================================="
    
    local physical_if=$(get_physical_interface)
    if [ -z "$physical_if" ]; then
        log_error "无法获取物理网卡名称"
        return 1
    fi
    log_info "物理网卡: $physical_if"
    
    local bridges=$(get_docker_bridges)
    if [ -z "$bridges" ]; then
        log_warn "未找到 Docker 网桥"
        return 0
    fi
    
    while IFS= read -r bridge_info; do
        local bridge_name=$(echo "$bridge_info" | cut -d: -f1)
        local subnet=$(echo "$bridge_info" | cut -d: -f2)
        
        log_info "配置网桥: $bridge_name (子网: $subnet)"
        
        # 允许网桥与物理网卡的双向转发
        add_rule_if_not_exists "filter" "FORWARD" -i "$bridge_name" -o "$physical_if" -j ACCEPT
        add_rule_if_not_exists "filter" "FORWARD" -i "$physical_if" -o "$bridge_name" -m state --state RELATED,ESTABLISHED -j ACCEPT
        
        # 配置 NAT 转换
        add_rule_if_not_exists "nat" "POSTROUTING" -s "$subnet" -o "$physical_if" -j MASQUERADE
        
        log_info "✅ 网桥 $bridge_name 配置完成"
        echo ""
    done <<< "$bridges"
}

# 配置 tun0 到所有 Docker 网桥的转发规则
configure_tun0_to_bridges() {
    log_info "=========================================="
    log_info "2. 配置 tun0 到所有 Docker 网桥的转发规则"
    log_info "=========================================="
    
    # 检查 tun0 是否存在
    if ! ip link show tun0 &>/dev/null; then
        log_warn "tun0 设备不存在，跳过配置"
        return 0
    fi
    
    log_info "tun0 设备已找到"
    
    local bridges=$(get_docker_bridges)
    if [ -z "$bridges" ]; then
        log_warn "未找到 Docker 网桥"
        return 0
    fi
    
    while IFS= read -r bridge_info; do
        local bridge_name=$(echo "$bridge_info" | cut -d: -f1)
        local subnet=$(echo "$bridge_info" | cut -d: -f2)
        
        log_info "配置 tun0 与网桥 $bridge_name ($subnet) 的转发规则"
        
        # 允许 tun0 与 Docker 网桥的双向转发
        add_rule_if_not_exists "filter" "FORWARD" -i tun0 -o "$bridge_name" -j ACCEPT
        add_rule_if_not_exists "filter" "FORWARD" -i "$bridge_name" -o tun0 -m state --state RELATED,ESTABLISHED -j ACCEPT
        
        log_info "✅ tun0 与网桥 $bridge_name 转发规则配置完成"
    done <<< "$bridges"
    
    echo ""
}

# 配置 Minikube 集群子网路由
configure_minikube_routes() {
    log_info "=========================================="
    log_info "3. 配置 Minikube 集群子网路由"
    log_info "=========================================="
    
    local minikube_info=$(get_minikube_info)
    if [ $? -ne 0 ] || [ -z "$minikube_info" ]; then
        log_warn "未找到运行中的 Minikube 容器，跳过配置"
        return 0
    fi
    
    local container_ip=$(echo "$minikube_info" | cut -d: -f2)
    local service_cidr=$(echo "$minikube_info" | cut -d: -f4)
    
    log_info "Minikube 容器 IP: $container_ip"
    
    if [ -n "$service_cidr" ]; then
        log_info "Kubernetes Service CIDR: $service_cidr"
        add_route_if_not_exists "$service_cidr" "$container_ip"
        log_info "✅ Service 网络路由配置完成"
    else
        log_warn "无法获取 Kubernetes Service CIDR，跳过路由配置"
    fi
    
    echo ""
}

# 获取 Minikube DNS 服务 IP
get_minikube_dns_ip() {
    if ! command -v kubectl &> /dev/null; then
        return 1
    fi
    
    # 尝试获取 kube-dns 或 coredns 服务的 ClusterIP
    local dns_ip=$(kubectl get svc -n kube-system kube-dns -o jsonpath='{.spec.clusterIP}' 2>/dev/null)
    
    if [ -z "$dns_ip" ]; then
        dns_ip=$(kubectl get svc -n kube-system coredns -o jsonpath='{.spec.clusterIP}' 2>/dev/null)
    fi
    
    echo "$dns_ip"
}

# 配置 Minikube DNS
configure_minikube_dns() {
    log_info "=========================================="
    log_info "4. 配置 Minikube DNS"
    log_info "=========================================="
    
    # 检查 Minikube 是否运行
    local minikube_info=$(get_minikube_info)
    if [ $? -ne 0 ] || [ -z "$minikube_info" ]; then
        log_warn "未找到运行中的 Minikube 容器，跳过 DNS 配置"
        return 0
    fi
    
    # 获取 DNS 服务 IP
    local dns_ip=$(get_minikube_dns_ip)
    
    if [ -z "$dns_ip" ]; then
        log_warn "无法获取 Minikube DNS 服务 IP，跳过 DNS 配置"
        log_info "提示: 请确保 kubectl 已配置并可以访问 Minikube 集群"
        return 0
    fi
    
    log_info "Minikube DNS 服务 IP: $dns_ip"
    
    # 检查 systemd-resolved 是否可用
    if ! command -v systemctl &> /dev/null; then
        log_warn "systemctl 命令不可用，跳过 DNS 配置"
        return 0
    fi
    
    if ! systemctl is-active --quiet systemd-resolved 2>/dev/null; then
        log_warn "systemd-resolved 服务未运行，跳过 DNS 配置"
        return 0
    fi
    
    # 创建配置目录
    local conf_dir="/etc/systemd/resolved.conf.d"
    local conf_file="$conf_dir/minikube-dns.conf"
    
    log_info "创建 DNS 配置目录: $conf_dir"
    sudo mkdir -p "$conf_dir"
    
    # 检查配置文件是否已存在且内容相同
    local needs_update=true
    if [ -f "$conf_file" ]; then
        local existing_dns=$(grep "^DNS=" "$conf_file" 2>/dev/null | cut -d= -f2 | tr -d ' ')
        if [ "$existing_dns" = "$dns_ip" ]; then
            log_debug "DNS 配置已存在且正确: $dns_ip"
            needs_update=false
        else
            log_info "DNS 配置需要更新: $existing_dns -> $dns_ip"
        fi
    fi
    
    if [ "$needs_update" = true ]; then
        log_info "写入 DNS 配置文件: $conf_file"
        
        sudo tee "$conf_file" > /dev/null << EOF
# Minikube DNS 配置
# 自动生成于: $(date '+%Y-%m-%d %H:%M:%S')
[Resolve]
# Minikube DNS 服务 IP
DNS=$dns_ip

# Kubernetes 集群域名
Domains=cluster.local
EOF
        
        if [ $? -eq 0 ]; then
            log_info "✅ DNS 配置文件已创建"
            
            # 重启 systemd-resolved 服务
            log_info "重启 systemd-resolved 服务..."
            if sudo systemctl restart systemd-resolved; then
                log_info "✅ systemd-resolved 服务已重启"
                
                # 验证配置
                sleep 1
                if systemctl is-active --quiet systemd-resolved; then
                    log_info "✅ DNS 配置已生效"
                    
                    # 显示当前 DNS 配置
                    log_debug "当前 DNS 服务器:"
                    resolvectl status 2>/dev/null | grep "DNS Servers" | head -n 1 | sed 's/^/  /'
                else
                    log_error "systemd-resolved 服务启动失败"
                fi
            else
                log_error "重启 systemd-resolved 服务失败"
            fi
        else
            log_error "创建 DNS 配置文件失败"
        fi
    else
        log_info "✅ DNS 配置无需更新"
    fi
    
    echo ""
}

# 配置其他 Docker 网桥与 Minikube 的通信
configure_bridges_to_minikube() {
    log_info "=========================================="
    log_info "5. 配置 Docker 网桥与 Minikube 的通信"
    log_info "=========================================="
    
    local minikube_info=$(get_minikube_info)
    if [ $? -ne 0 ] || [ -z "$minikube_info" ]; then
        log_warn "未找到运行中的 Minikube 容器，跳过配置"
        return 0
    fi
    
    local minikube_bridge=$(echo "$minikube_info" | cut -d: -f1)
    log_info "Minikube 网桥: $minikube_bridge"
    
    local bridges=$(get_docker_bridges)
    if [ -z "$bridges" ]; then
        log_warn "未找到其他 Docker 网桥"
        return 0
    fi
    
    while IFS= read -r bridge_info; do
        local bridge_name=$(echo "$bridge_info" | cut -d: -f1)
        
        # 跳过 Minikube 自己的网桥
        if [ "$bridge_name" = "$minikube_bridge" ]; then
            continue
        fi
        
        log_info "配置网桥 $bridge_name 与 Minikube 的通信"
        
        # 允许双向转发
        add_rule_if_not_exists "filter" "FORWARD" -i "$bridge_name" -o "$minikube_bridge" -j ACCEPT
        add_rule_if_not_exists "filter" "FORWARD" -i "$minikube_bridge" -o "$bridge_name" -m state --state RELATED,ESTABLISHED -j ACCEPT
        
        log_info "✅ 网桥 $bridge_name 与 Minikube 通信配置完成"
    done <<< "$bridges"
    
    echo ""
}

# 配置非 Minikube 网桥的子网内通信
configure_bridge_internal_communication() {
    log_info "=========================================="
    log_info "6. 配置 Docker 网桥子网内通信"
    log_info "=========================================="
    
    local minikube_info=$(get_minikube_info)
    local minikube_bridge=""
    
    if [ -n "$minikube_info" ]; then
        minikube_bridge=$(echo "$minikube_info" | cut -d: -f1)
        log_info "Minikube 网桥: $minikube_bridge (将跳过)"
    fi
    
    local bridges=$(get_docker_bridges)
    if [ -z "$bridges" ]; then
        log_warn "未找到 Docker 网桥"
        return 0
    fi
    
    while IFS= read -r bridge_info; do
        local bridge_name=$(echo "$bridge_info" | cut -d: -f1)
        local subnet=$(echo "$bridge_info" | cut -d: -f2)
        
        # 跳过 Minikube 网桥
        if [ -n "$minikube_bridge" ] && [ "$bridge_name" = "$minikube_bridge" ]; then
            log_debug "跳过 Minikube 网桥: $bridge_name"
            continue
        fi
        
        log_info "配置网桥 $bridge_name ($subnet) 子网内通信"
        
        # 1. 允许该网桥上的所有流量转发（同一网桥的容器间通信）
        add_rule_if_not_exists "filter" "FORWARD" -i "$bridge_name" -o "$bridge_name" -j ACCEPT
        
        # 2. 允许从该网桥出去的已建立连接
        add_rule_if_not_exists "filter" "FORWARD" -i "$bridge_name" -m state --state RELATED,ESTABLISHED -j ACCEPT
        
        # 3. 允许到该网桥的已建立连接
        add_rule_if_not_exists "filter" "FORWARD" -o "$bridge_name" -m state --state RELATED,ESTABLISHED -j ACCEPT
        
        log_info "✅ 网桥 $bridge_name 子网内通信配置完成"
    done <<< "$bridges"
    
    echo ""
}

# 生成网络拓扑图
generate_topology() {
    log_info "=========================================="
    log_info "7. 网络拓扑图"
    log_info "=========================================="
    
    echo ""
    echo "┌─────────────────────────────────────────────────────────────┐"
    echo "│                    网络转发拓扑图                            │"
    echo "└─────────────────────────────────────────────────────────────┘"
    echo ""
    
    local physical_if=$(get_physical_interface)
    local minikube_info=$(get_minikube_info)
    local minikube_bridge=""
    
    if [ -n "$minikube_info" ]; then
        minikube_bridge=$(echo "$minikube_info" | cut -d: -f1)
    fi
    
    # 解析 FORWARD 规则
    local rules=$(sudo iptables -L FORWARD -n -v | tail -n +3)
    
    echo "📡 物理网卡: $physical_if"
    echo "🔧 TUN 设备: tun0"
    echo ""
    
    # Docker 网桥
    echo "🐳 Docker 网桥:"
    local bridges=$(get_docker_bridges)
    while IFS= read -r bridge_info; do
        local bridge_name=$(echo "$bridge_info" | cut -d: -f1)
        local subnet=$(echo "$bridge_info" | cut -d: -f2)
        
        if [ "$bridge_name" = "$minikube_bridge" ]; then
            echo "  ├─ $bridge_name ($subnet) [Minikube]"
        else
            echo "  ├─ $bridge_name ($subnet)"
        fi
        
        # 检查是否配置了子网内通信（非 Minikube 网桥）
        local has_internal_comm=false
        if [ "$bridge_name" != "$minikube_bridge" ]; then
            if echo "$rules" | grep -q "$bridge_name.*$bridge_name"; then
                has_internal_comm=true
                echo "  │   ├─> $bridge_name (子网内通信) ✓"
            fi
        fi
        
        # 显示转发关系
        if echo "$rules" | grep -q "$bridge_name.*$physical_if"; then
            if [ "$has_internal_comm" = true ]; then
                echo "  │   ├─> $physical_if (外网)"
            else
                echo "  │   └─> $physical_if (外网)"
            fi
        fi
        
        if [ -n "$minikube_bridge" ] && [ "$bridge_name" != "$minikube_bridge" ]; then
            if echo "$rules" | grep -q "$bridge_name.*$minikube_bridge"; then
                echo "  │   ├─> $minikube_bridge (Minikube)"
            fi
        fi
        
        if ip link show tun0 &>/dev/null && echo "$rules" | grep -q "tun0.*$bridge_name"; then
            echo "  │   └─> tun0 (宿主机)"
        fi
    done <<< "$bridges"
    
    echo ""
    
    # 路由信息
    if [ -n "$minikube_info" ]; then
        echo "🛣️  Minikube 路由:"
        local container_ip=$(echo "$minikube_info" | cut -d: -f2)
        local service_cidr=$(echo "$minikube_info" | cut -d: -f4)
        
        if [ -n "$service_cidr" ]; then
            echo "  └─ Service CIDR: $service_cidr via $container_ip"
        fi
    fi
    
    echo ""
    
    # DNS 配置信息
    local dns_conf_file="/etc/systemd/resolved.conf.d/minikube-dns.conf"
    if [ -f "$dns_conf_file" ]; then
        echo "🌐 DNS 配置:"
        local dns_ip=$(grep "^DNS=" "$dns_conf_file" 2>/dev/null | cut -d= -f2 | tr -d ' ')
        local domains=$(grep "^Domains=" "$dns_conf_file" 2>/dev/null | cut -d= -f2 | tr -d ' ')
        
        if [ -n "$dns_ip" ]; then
            echo "  ├─ DNS 服务器: $dns_ip"
        fi
        if [ -n "$domains" ]; then
            echo "  └─ 搜索域: $domains"
        fi
    fi
    
    echo ""
    echo "┌─────────────────────────────────────────────────────────────┐"
    echo "│                    转发规则统计                              │"
    echo "└─────────────────────────────────────────────────────────────┘"
    echo ""
    
    local forward_count=$(sudo iptables -L FORWARD -n | tail -n +3 | wc -l)
    local nat_count=$(sudo iptables -t nat -L POSTROUTING -n | tail -n +3 | wc -l)
    local route_count=$(ip route | grep -c "via" || echo "0")
    
    echo "📊 FORWARD 规则数: $forward_count"
    echo "📊 NAT 规则数: $nat_count"
    echo "📊 路由条目数: $route_count"
    echo ""
}

# 显示详细规则
show_detailed_rules() {
    log_info "=========================================="
    log_info "详细规则列表"
    log_info "=========================================="
    
    echo ""
    echo "🔍 FORWARD 链规则:"
    sudo iptables -L FORWARD -n -v --line-numbers
    
    echo ""
    echo "🔍 NAT POSTROUTING 链规则:"
    sudo iptables -t nat -L POSTROUTING -n -v --line-numbers
    
    echo ""
    echo "🔍 路由表:"
    ip route
    echo ""
}

# 主函数
main() {
    log_info "=========================================="
    log_info "Lima Docker 虚拟机网络配置脚本"
    log_info "=========================================="
    echo ""
    
    # 检查必要命令
    check_command docker
    check_command iptables
    check_command ip
    
    # 检查是否为 root 或有 sudo 权限
    if [ "$EUID" -ne 0 ] && ! sudo -n true 2>/dev/null; then
        log_error "此脚本需要 root 权限或 sudo 权限"
        exit 1
    fi
    
    # ⚠️ 关键步骤：检查 Docker iptables 配置（必须在最前面）
    check_docker_iptables_config
    
    # 启用 IP 转发
    if [ "$(sysctl -n net.ipv4.ip_forward)" != "1" ]; then
        log_info "启用 IP 转发..."
        sudo sysctl -w net.ipv4.ip_forward=1 >/dev/null
        log_info "✅ IP 转发已启用"
        echo ""
    fi
    
    # 执行配置
    configure_docker_bridges_nat
    configure_tun0_to_bridges
    configure_minikube_routes
    configure_minikube_dns
    configure_bridges_to_minikube
    configure_bridge_internal_communication
    
    # 清理无效规则
    cleanup_invalid_rules
    echo ""
    
    # 生成拓扑图
    generate_topology
    
    # 显示详细规则（可选）
    if [ "$1" = "-v" ] || [ "$1" = "--verbose" ]; then
        show_detailed_rules
    fi
    
    log_info "=========================================="
    log_info "✅ 网络配置完成！"
    log_info "=========================================="
    echo ""
    log_info "提示: 使用 '$0 -v' 查看详细规则列表"
}

# 运行主函数
main "$@"
