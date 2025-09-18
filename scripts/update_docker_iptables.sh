#!/bin/bash

# 清理不存在的Docker网桥iptables规则脚本，并为存在的网桥添加tun0转发规则
# 使用方法:
#   sudo ./cleanup-docker-iptables.sh                    # 只清理无效规则
#   sudo ./cleanup-docker-iptables.sh --add-tun-rules    # 清理无效规则并添加tun0转发规则

set -e

echo "开始清理不存在的Docker网桥iptables规则..."

# 获取当前存在的Docker网络接口
get_existing_docker_interfaces() {
    {
        # 获取所有docker网络的网桥接口名
        docker network ls --filter driver=bridge -q | while read network_id; do
            if [ -n "$network_id" ]; then
                bridge_name=$(docker network inspect "$network_id" --format '{{.Options.com.docker.network.bridge.name}}' 2>/dev/null)
                if [ -n "$bridge_name" ] && [ "$bridge_name" != "<no value>" ] && [ "$bridge_name" != "null" ]; then
                    echo "$bridge_name"
                fi
            fi
        done

        # 同时获取默认的docker0接口（如果存在）
        if ip link show docker0 >/dev/null 2>&1; then
            echo "docker0"
        fi

        # 获取其他br-开头的docker网桥（通过ip link直接检查）
        ip link show | grep -o 'br-[a-f0-9]\{12\}' 2>/dev/null || true
    } | grep -v '^$' | sort | uniq
}

# 检查网桥是否存在
interface_exists() {
    local interface=$1
    ip link show "$interface" &>/dev/null
}

# 清理FORWARD链中的规则
cleanup_forward_chain() {
    echo "检查FORWARD链中的Docker规则..."

    # 获取当前存在的接口列表
    existing_interfaces=$(get_existing_docker_interfaces | sort | uniq)

    # 获取FORWARD链中包含docker网桥的规则（倒序处理）
    iptables -L FORWARD -v --line-numbers | grep -E "(docker|br-)" | sort -nr | while read line; do
        if [ -z "$line" ]; then continue; fi

        line_num=$(echo "$line" | awk '{print $1}')
        # 提取接口名，支持更多格式
        interface=$(echo "$line" | grep -o -E "(docker[0-9]*|br-[a-f0-9]{12}|br-[a-f0-9]{16})" | head -1)

        if [ -n "$interface" ] && [ "$interface" != "<no value>" ] && [ "$interface" != "null" ]; then
            # 检查接口是否在存在列表中
            if echo "$existing_interfaces" | grep -q "^$interface$"; then
                echo "保留FORWARD链中存在接口 $interface 的规则"
            else
                echo "移除FORWARD链中不存在接口 $interface 的规则 (行号: $line_num)"
                iptables -D FORWARD "$line_num" 2>/dev/null || echo "警告: 无法删除FORWARD链规则 $line_num"
            fi
        fi
    done
}

# 清理DOCKER-ISOLATION-STAGE-2链中的规则
cleanup_docker_isolation_stage2() {
    echo "检查DOCKER-ISOLATION-STAGE-2链中的Docker规则..."

    # 检查链是否存在（更准确的检测方法）
    if ! iptables -L DOCKER-ISOLATION-STAGE-2 -n >/dev/null 2>&1; then
        echo "DOCKER-ISOLATION-STAGE-2链不存在，跳过"
        return
    fi

    # 获取当前存在的接口列表
    existing_interfaces=$(get_existing_docker_interfaces | sort | uniq)

    # 获取DOCKER-ISOLATION-STAGE-2链中包含docker网桥的规则（倒序处理）
    iptables -L DOCKER-ISOLATION-STAGE-2 -v --line-numbers | grep -E "(docker|br-)" | sort -nr | while read line; do
        if [ -z "$line" ]; then continue; fi

        line_num=$(echo "$line" | awk '{print $1}')
        interface=$(echo "$line" | grep -o -E "(docker[0-9]*|br-[a-f0-9]{12}|br-[a-f0-9]{16})" | head -1)

        if [ -n "$interface" ] && [ "$interface" != "<no value>" ] && [ "$interface" != "null" ]; then
            # 检查接口是否在存在列表中
            if echo "$existing_interfaces" | grep -q "^$interface$"; then
                echo "保留DOCKER-ISOLATION-STAGE-2链中存在接口 $interface 的规则"
            else
                echo "移除DOCKER-ISOLATION-STAGE-2链中不存在接口 $interface 的规则 (行号: $line_num)"
                iptables -D DOCKER-ISOLATION-STAGE-2 "$line_num" 2>/dev/null || echo "警告: 无法删除DOCKER-ISOLATION-STAGE-2链规则 $line_num"
            fi
        fi
    done
}

# 清理其他Docker相关链中的规则
cleanup_other_docker_chains() {
    echo "检查其他Docker相关链..."

    # 获取当前存在的接口列表
    existing_interfaces=$(get_existing_docker_interfaces | sort | uniq)

    # 获取所有Docker相关的自定义链
    docker_chains=$(iptables -L | grep "^Chain DOCKER" | awk '{print $2}' || true)

    for chain in $docker_chains; do
        if [ "$chain" = "DOCKER-ISOLATION-STAGE-2" ]; then
            continue  # 已经处理过了
        fi

        echo "检查链: $chain"
        # 检查链中是否有包含网桥接口的规则
        rules_with_interfaces=$(iptables -L "$chain" -v --line-numbers | grep -E "(docker|br-)" || true)

        if [ -n "$rules_with_interfaces" ]; then
            echo "$rules_with_interfaces" | sort -nr | while read line; do
                if [ -z "$line" ]; then continue; fi

                line_num=$(echo "$line" | awk '{print $1}')
                interface=$(echo "$line" | grep -o -E "(docker[0-9]*|br-[a-f0-9]{12}|br-[a-f0-9]{16})" | head -1)

                if [ -n "$interface" ] && [ "$interface" != "<no value>" ] && [ "$interface" != "null" ]; then
                    # 检查接口是否在存在列表中
                    if echo "$existing_interfaces" | grep -q "^$interface$"; then
                        echo "保留链 $chain 中存在接口 $interface 的规则"
                    else
                        echo "移除链 $chain 中不存在接口 $interface 的规则 (行号: $line_num)"
                        iptables -D "$chain" "$line_num" 2>/dev/null || echo "警告: 无法删除链 $chain 规则 $line_num"
                    fi
                fi
            done
        else
            echo "链 $chain 中没有找到包含网桥接口的规则"
        fi
    done
}

# 检查tun0接口是否存在
check_tun0_interface() {
    if ! ip link show tun0 &>/dev/null; then
        echo "警告: tun0接口不存在，跳过添加tun0转发规则"
        return 1
    fi
    return 0
}

# 检查规则是否已存在
rule_exists() {
    local rule_spec="$1"
    iptables -C $rule_spec 2>/dev/null
}

# 为存在的Docker网桥添加tun0转发规则
add_tun0_rules() {
    echo "为存在的Docker网桥添加tun0转发规则..."

    # 检查tun0接口是否存在
    if ! check_tun0_interface; then
        return 1
    fi

    # 获取当前存在的接口列表
    existing_interfaces=$(get_existing_docker_interfaces | sort | uniq)

    if [ -z "$existing_interfaces" ]; then
        echo "没有找到存在的Docker网桥接口"
        return 1
    fi

    echo "为以下Docker网桥添加tun0转发规则:"
    echo "$existing_interfaces"
    echo ""

    # 为每个存在的网桥接口添加规则
    echo "$existing_interfaces" | while read interface; do
        if [ -z "$interface" ] || [ "$interface" = "<no value>" ] || [ "$interface" = "null" ]; then
            continue
        fi

        # 验证接口名格式是否有效
        if ! echo "$interface" | grep -qE '^(docker[0-9]*|br-[a-f0-9]{12})$'; then
            echo "跳过无效接口名: $interface"
            continue
        fi

        echo "为网桥 $interface 添加tun0转发规则..."

        # 1. 允许 tun0 → 容器网桥的转发
        forward_rule="FORWARD -i tun0 -o $interface -j ACCEPT"
        if rule_exists "$forward_rule"; then
            echo "  规则已存在: tun0 → $interface 转发"
        else
            if iptables -I FORWARD 1 -i tun0 -o "$interface" -j ACCEPT; then
                echo "  ✓ 添加成功: tun0 → $interface 转发"
            else
                echo "  ✗ 添加失败: tun0 → $interface 转发"
            fi
        fi

        # 2. 允许回包
        return_rule="FORWARD -i $interface -o tun0 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT"
        if rule_exists "$return_rule"; then
            echo "  规则已存在: $interface → tun0 回包"
        else
            if iptables -I FORWARD 2 -i "$interface" -o tun0 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT; then
                echo "  ✓ 添加成功: $interface → tun0 回包"
            else
                echo "  ✗ 添加失败: $interface → tun0 回包"
            fi
        fi

        # 3. 绕过 Docker 隔离规则
        if iptables -L DOCKER-ISOLATION-STAGE-2 -n >/dev/null 2>&1; then
            isolation_rule="DOCKER-ISOLATION-STAGE-2 -i tun0 -o $interface -j RETURN"
            if rule_exists "$isolation_rule"; then
                echo "  规则已存在: 绕过Docker隔离 tun0 → $interface"
            else
                if iptables -I DOCKER-ISOLATION-STAGE-2 1 -i tun0 -o "$interface" -j RETURN; then
                    echo "  ✓ 添加成功: 绕过Docker隔离 tun0 → $interface"
                else
                    echo "  ✗ 添加失败: 绕过Docker隔离 tun0 → $interface"
                fi
            fi
        else
            echo "  跳过: DOCKER-ISOLATION-STAGE-2链不存在"
        fi

        echo ""
    done
}

# 移除tun0相关规则
remove_tun0_rules() {
    echo "移除所有tun0相关的转发规则..."

    # 获取当前存在的接口列表
    existing_interfaces=$(get_existing_docker_interfaces | sort | uniq)

    # 移除FORWARD链中的tun0规则
    echo "移除FORWARD链中的tun0规则..."
    iptables -L FORWARD -v --line-numbers | grep "tun0" | sort -nr | while read line; do
        if [ -z "$line" ]; then continue; fi
        line_num=$(echo "$line" | awk '{print $1}')
        echo "  移除FORWARD链规则 (行号: $line_num)"
        iptables -D FORWARD "$line_num" 2>/dev/null || echo "  警告: 无法删除FORWARD链规则 $line_num"
    done

    # 移除DOCKER-ISOLATION-STAGE-2链中的tun0规则
    if iptables -L DOCKER-ISOLATION-STAGE-2 -n >/dev/null 2>&1; then
        echo "移除DOCKER-ISOLATION-STAGE-2链中的tun0规则..."
        iptables -L DOCKER-ISOLATION-STAGE-2 -v --line-numbers | grep "tun0" | sort -nr | while read line; do
            if [ -z "$line" ]; then continue; fi
            line_num=$(echo "$line" | awk '{print $1}')
            echo "  移除DOCKER-ISOLATION-STAGE-2链规则 (行号: $line_num)"
            iptables -D DOCKER-ISOLATION-STAGE-2 "$line_num" 2>/dev/null || echo "  警告: 无法删除DOCKER-ISOLATION-STAGE-2链规则 $line_num"
        done
    fi

    echo "tun0规则移除完成"
}

# 显示帮助信息
show_help() {
    echo "用法: $0 [选项]"
    echo ""
    echo "选项:"
    echo "  无参数                    只清理不存在的Docker网桥iptables规则"
    echo "  --add-tun-rules          清理无效规则并为存在的网桥添加tun0转发规则"
    echo "  --remove-tun-rules       移除所有tun0相关的转发规则"
    echo "  --only-add-tun-rules     只添加tun0转发规则，不执行清理"
    echo "  -h, --help               显示此帮助信息"
    echo ""
    echo "示例:"
    echo "  sudo $0                           # 只清理无效规则"
    echo "  sudo $0 --add-tun-rules           # 清理并添加tun0规则"
    echo "  sudo $0 --remove-tun-rules        # 移除tun0规则"
    echo "  sudo $0 --only-add-tun-rules      # 只添加tun0规则"
}

# 主函数
main() {
    # 检查是否以root权限运行
    if [ "$(id -u)" -ne 0 ]; then
        echo "错误: 此脚本需要root权限运行"
        echo "请使用: sudo $0"
        exit 1
    fi

    # 解析命令行参数
    ADD_TUN_RULES=false
    REMOVE_TUN_RULES=false
    ONLY_ADD_TUN_RULES=false
    CLEANUP_RULES=true

    case "$1" in
        --add-tun-rules)
            ADD_TUN_RULES=true
            ;;
        --remove-tun-rules)
            REMOVE_TUN_RULES=true
            CLEANUP_RULES=false
            ;;
        --only-add-tun-rules)
            ONLY_ADD_TUN_RULES=true
            CLEANUP_RULES=false
            ;;
        -h|--help)
            show_help
            exit 0
            ;;
        "")
            ADD_TUN_RULES=true
            ;;
        *)
            echo "错误: 未知参数 '$1'"
            echo ""
            show_help
            exit 1
            ;;
    esac

    echo "当前存在的Docker网桥接口:"
    get_existing_docker_interfaces | sort | uniq
    echo ""

    # 备份当前iptables规则
    backup_file="/tmp/iptables-backup-$(date +%Y%m%d-%H%M%S).rules"
    echo "备份当前iptables规则到 $backup_file"
    iptables-save > "$backup_file"
    echo ""

    # 执行清理（如果需要）
    if [ "$CLEANUP_RULES" = true ]; then
        echo "开始清理不存在的Docker网桥iptables规则..."
        cleanup_forward_chain
        echo ""
        cleanup_docker_isolation_stage2
        echo ""
        cleanup_other_docker_chains
        echo ""
        echo "清理完成！"
        echo ""
    fi

    # 移除tun0规则（如果需要）
    if [ "$REMOVE_TUN_RULES" = true ]; then
        remove_tun0_rules
        echo ""
    fi

    # 添加tun0规则（如果需要）
    if [ "$ADD_TUN_RULES" = true ] || [ "$ONLY_ADD_TUN_RULES" = true ]; then
        add_tun0_rules
        echo ""
    fi

    echo "操作完成！"
    echo "如需恢复，可使用备份文件: iptables-restore < $backup_file"
}


main "$@"
