#!/bin/bash
# ============================================================
# Docker Connector 环境验证脚本
# ============================================================
# 检查所有组件的安装和运行状态
# 用法: bash skills/env-init/scripts/verify-env.sh
# ============================================================

set -uo pipefail

# 颜色
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# 计数器
PASS=0
FAIL=0
WARN=0

VM_NAME="docker"

check_pass() { echo -e "  ${GREEN}✅ $1${NC}"; ((PASS++)); }
check_fail() { echo -e "  ${RED}❌ $1${NC}"; ((FAIL++)); }
check_warn() { echo -e "  ${YELLOW}⚠️  $1${NC}"; ((WARN++)); }

# ============================================================
# Phase 1: 基础工具链
# ============================================================
echo -e "\n${BLUE}═══ Phase 1: 基础工具链 ═══${NC}\n"

# Homebrew
if command -v brew &>/dev/null; then
    check_pass "Homebrew: $(brew --version 2>/dev/null | head -1)"
else
    check_fail "Homebrew 未安装"
fi

# Go
if command -v go &>/dev/null; then
    check_pass "Go: $(go version 2>/dev/null | awk '{print $3}')"
else
    check_fail "Go 未安装"
fi

# Lima
if command -v limactl &>/dev/null; then
    check_pass "Lima: $(limactl --version 2>/dev/null | head -1)"
else
    check_fail "Lima 未安装"
fi

# Python3
if command -v python3 &>/dev/null; then
    check_pass "Python3: $(python3 --version 2>/dev/null)"
else
    check_fail "Python3 未安装"
fi

# ============================================================
# Phase 2: Lima VM
# ============================================================
echo -e "\n${BLUE}═══ Phase 2: Lima VM (${VM_NAME}) ═══${NC}\n"

if command -v limactl &>/dev/null; then
    # VM 是否存在
    if limactl list --format '{{.Name}}' 2>/dev/null | grep -qx "$VM_NAME"; then
        check_pass "VM '${VM_NAME}' 存在"

        # VM 是否运行
        vm_status=$(limactl list --format '{{.Name}}:{{.Status}}' 2>/dev/null | grep "^${VM_NAME}:" | cut -d: -f2)
        if [ "$vm_status" = "Running" ]; then
            check_pass "VM '${VM_NAME}' 运行中"

            # Docker 是否可用
            if limactl shell "$VM_NAME" docker info &>/dev/null; then
                check_pass "Docker 引擎可用"
                # Docker 版本
                docker_ver=$(limactl shell "$VM_NAME" docker --version 2>/dev/null | awk '{print $3}' | tr -d ',')
                check_pass "Docker 版本: ${docker_ver}"
            else
                check_fail "Docker 引擎不可用"
            fi
        else
            check_fail "VM '${VM_NAME}' 未运行 (状态: ${vm_status})"
        fi
    else
        check_fail "VM '${VM_NAME}' 不存在"
    fi
else
    check_fail "Lima 未安装，跳过 VM 检查"
fi

# ============================================================
# Phase 3: 源码编译产物
# ============================================================
echo -e "\n${BLUE}═══ Phase 3: 编译产物 ═══${NC}\n"

# 检测项目根目录（skills/env-init/scripts/ -> 项目根目录需要上溯 3 级）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../../../" && pwd)"

if [ -f "${PROJECT_ROOT}/build/docker-connector" ]; then
    check_pass "VM 端二进制: build/docker-connector"
else
    check_warn "VM 端二进制未编译 (build/docker-connector)"
fi

if [ -f "${PROJECT_ROOT}/build/docker-connector-desktop" ]; then
    check_pass "Desktop 端二进制: build/docker-connector-desktop"
else
    check_warn "Desktop 端二进制未编译 (build/docker-connector-desktop)"
fi

# ============================================================
# Phase 4: Desktop 端服务
# ============================================================
echo -e "\n${BLUE}═══ Phase 4: Desktop 端服务 ═══${NC}\n"

if command -v brew &>/dev/null; then
    # 检查 brew 是否安装了 docker-connector
    if brew list docker-connector &>/dev/null; then
        check_pass "docker-connector 已通过 brew 安装"

        # 检查服务状态
        if sudo brew services list 2>/dev/null | grep -q "docker-connector.*started\|docker-connector.*running"; then
            check_pass "Desktop 服务运行中"
        else
            check_warn "Desktop 服务未运行"
        fi

        # 检查配置文件
        brew_prefix="$(brew --prefix)"
        conf_file="${brew_prefix}/etc/docker-connector.conf"
        if [ -f "$conf_file" ]; then
            check_pass "配置文件存在: ${conf_file}"
        else
            check_warn "配置文件不存在: ${conf_file}"
        fi
    else
        check_warn "docker-connector 未通过 brew 安装"
    fi
else
    check_fail "Homebrew 未安装，跳过 Desktop 端检查"
fi

# ============================================================
# Phase 5: VM 端服务
# ============================================================
echo -e "\n${BLUE}═══ Phase 5: VM 端服务 ═══${NC}\n"

if command -v limactl &>/dev/null; then
    vm_status=$(limactl list --format '{{.Name}}:{{.Status}}' 2>/dev/null | grep "^${VM_NAME}:" | cut -d: -f2)
    if [ "$vm_status" = "Running" ]; then
        # 检查 docker-connector 服务
        if limactl shell "$VM_NAME" sudo systemctl is-active --quiet docker-connector 2>/dev/null; then
            check_pass "VM 端 docker-connector 服务运行中"
        else
            check_warn "VM 端 docker-connector 服务未运行"
        fi

        # 检查配置文件
        if limactl shell "$VM_NAME" test -f /etc/docker-connector/connector.env 2>/dev/null; then
            check_pass "VM 端配置文件存在"
        else
            check_warn "VM 端配置文件不存在"
        fi
    else
        check_warn "VM 未运行，跳过 VM 端服务检查"
    fi
else
    check_fail "Lima 未安装，跳过 VM 端检查"
fi

# ============================================================
# Phase 6: 网络连通性
# ============================================================
echo -e "\n${BLUE}═══ Phase 6: 网络连通性 ═══${NC}\n"

# 读取配置
ENV_FILE="${PROJECT_ROOT}/deploy/connector.env"
if [ -f "$ENV_FILE" ]; then
    CONNECTOR_ADDR=$(grep -E '^CONNECTOR_ADDR=' "$ENV_FILE" | cut -d'=' -f2 | tr -d '[:space:]')
    VM_IP=$(echo "$CONNECTOR_ADDR" | cut -d'/' -f1)

    # 检查 TUN 设备
    if ifconfig utun 2>/dev/null | grep -q "inet" || ifconfig 2>/dev/null | grep -A2 "utun" | grep -q "inet"; then
        check_pass "TUN 设备已创建"
    else
        check_warn "TUN 设备未检测到（Desktop 服务可能未运行）"
    fi

    # 检查路由
    if netstat -rn 2>/dev/null | grep -q "172\." || route -n get 172.17.0.0 &>/dev/null; then
        check_pass "Docker 网络路由已配置"
    else
        check_warn "Docker 网络路由未配置"
    fi

    # 检查 Dashboard
    if curl -s --connect-timeout 3 "http://127.0.0.1:2511" &>/dev/null; then
        check_pass "Dashboard 可达 (http://127.0.0.1:2511)"
    else
        check_warn "Dashboard 不可达"
    fi
else
    check_warn "connector.env 不存在，跳过网络检查"
fi

# ============================================================
# 汇总
# ============================================================
echo -e "\n${BLUE}═══════════════════════════════════${NC}"
echo -e "  ${GREEN}通过: ${PASS}${NC}  ${RED}失败: ${FAIL}${NC}  ${YELLOW}警告: ${WARN}${NC}"
echo -e "${BLUE}═══════════════════════════════════${NC}\n"

if [ "$FAIL" -gt 0 ]; then
    echo -e "${RED}环境未就绪，请修复上述失败项${NC}"
    exit 1
elif [ "$WARN" -gt 0 ]; then
    echo -e "${YELLOW}环境基本就绪，但有部分组件未配置${NC}"
    exit 0
else
    echo -e "${GREEN}✅ 环境完全就绪！${NC}"
    exit 0
fi
