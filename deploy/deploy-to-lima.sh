#!/bin/bash
# ============================================================
# Docker Connector - macOS 一键部署到 Lima VM
# ============================================================
# 在 macOS 宿主机上运行，自动完成：
#   1. 交叉编译 Linux 二进制
#   2. 传输到 Lima VM
#   3. 远程执行 install.sh 安装服务
#   4. 验证服务状态
#
# 用法:
#   bash deploy/deploy-to-lima.sh [选项]
#
# 选项:
#   --vm=NAME            Lima VM 名称 (默认: default)
#   --addr=IP/MASK       虚拟网络地址 (默认: 使用 install.sh 默认值)
#   --port=PORT          UDP 隧道端口 (默认: 使用 install.sh 默认值)
#   --host=HOST          Desktop 端地址 (默认: 使用 install.sh 默认值)
#   --http-port=PORT     VM HTTP API 端口 (默认: 使用 install.sh 默认值)
#   --skip-build         跳过编译，直接使用已有二进制
#   --arch=ARCH          目标架构 (默认: 自动检测宿主机架构, 可选: amd64, arm64)
#   --reload             仅更新配置文件(service+env)并重启服务，不重新编译
#   --uninstall          卸载 VM 中的服务
#   --status             查看 VM 中的服务状态
#   --dry-run            仅显示将执行的命令，不实际执行
#
# 示例:
#   # 默认部署
#   bash deploy/deploy-to-lima.sh
#
#   # 指定 VM 和网络地址
#   bash deploy/deploy-to-lima.sh --vm=docker --addr=10.10.10.1/24
#
#   # 跳过编译（使用上次编译的二进制）
#   bash deploy/deploy-to-lima.sh --skip-build
#
#   # 显式指定架构（通常无需，会自动检测）
#   bash deploy/deploy-to-lima.sh --arch=arm64
#
#   # 仅更新配置文件并重启（不重新编译）
#   bash deploy/deploy-to-lima.sh --reload
#
#   # 查看 VM 中的服务状态
#   bash deploy/deploy-to-lima.sh --status
#
#   # 卸载
#   bash deploy/deploy-to-lima.sh --uninstall
# ============================================================

set -euo pipefail

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# 默认配置
VM_NAME="default"
TARGET_ARCH=""  # 留空表示自动检测
SKIP_BUILD=false
DRY_RUN=false
ACTION="deploy"

# install.sh 转发参数（只有用户显式指定时才传递）
INSTALL_ARGS=()

# 项目路径（自动检测）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
DOCKER_DIR="${PROJECT_ROOT}/docker"
DEPLOY_DIR="${PROJECT_ROOT}/deploy"

# 临时构建目录
BUILD_DIR="${PROJECT_ROOT}/build"
BINARY_NAME="docker-connector"

# VM 端临时路径
VM_TMP="/tmp/docker-connector-deploy"

log_info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }
log_step()  { echo -e "${BLUE}[STEP]${NC}  $*"; }
log_cmd()   { echo -e "${CYAN}  \$ $*${NC}"; }

# 解析命令行参数
parse_args() {
    for arg in "$@"; do
        case "$arg" in
            --vm=*)         VM_NAME="${arg#*=}" ;;
            --arch=*)       TARGET_ARCH="${arg#*=}" ;;
            --skip-build)   SKIP_BUILD=true ;;
            --dry-run)      DRY_RUN=true ;;
            --reload)       ACTION="reload" ;;
            --uninstall)    ACTION="uninstall" ;;
            --status)       ACTION="status" ;;
            --help|-h)      ACTION="help" ;;
            # 以下参数转发给 install.sh
            --addr=*)       INSTALL_ARGS+=("$arg") ;;
            --port=*)       INSTALL_ARGS+=("$arg") ;;
            --host=*)       INSTALL_ARGS+=("$arg") ;;
            --http-port=*)  INSTALL_ARGS+=("$arg") ;;
            *)              log_error "未知参数: $arg"; show_help; exit 1 ;;
        esac
    done
}

# 显示帮助
show_help() {
    head -43 "$0" | tail -40
    exit 0
}

# 自动检测目标架构（基于宿主机 CPU 架构推断）
detect_arch() {
    if [ -n "$TARGET_ARCH" ]; then
        # 用户显式指定了 --arch，不覆盖
        return
    fi

    local host_arch
    host_arch=$(uname -m)
    case "$host_arch" in
        x86_64)     TARGET_ARCH="amd64" ;;
        arm64|aarch64) TARGET_ARCH="arm64" ;;
        *)          log_warn "未知宿主机架构: $host_arch，默认使用 amd64"
                    TARGET_ARCH="amd64" ;;
    esac
    log_info "自动检测架构: ${host_arch} → GOARCH=${TARGET_ARCH}"
}

# 执行或模拟命令
run_cmd() {
    if [ "$DRY_RUN" = true ]; then
        log_cmd "$@"
    else
        log_cmd "$@"
        "$@"
    fi
}

# ============================================================
# 环境检查
# ============================================================
check_env() {
    log_step "检查环境..."

    # 检查操作系统
    if [ "$(uname -s)" != "Darwin" ]; then
        log_error "此脚本仅适用于 macOS"
        exit 1
    fi

    # 检查 limactl
    if ! command -v limactl &>/dev/null; then
        log_error "未找到 limactl，请先安装 Lima:"
        log_error "  brew install lima"
        exit 1
    fi

    # 检查 Go 编译器（如果需要编译）
    if [ "$SKIP_BUILD" = false ] && [ "$ACTION" = "deploy" ]; then
        if ! command -v go &>/dev/null; then
            log_error "未找到 Go 编译器，请先安装:"
            log_error "  brew install go"
            exit 1
        fi
        log_info "Go 版本: $(go version | awk '{print $3}')"
    fi

    # 检查 VM 是否存在且在运行
    if ! limactl list --format '{{.Name}}' 2>/dev/null | grep -qx "$VM_NAME"; then
        log_error "Lima VM '$VM_NAME' 不存在"
        log_error "可用的 VM:"
        limactl list --format '  {{.Name}} ({{.Status}})' 2>/dev/null || true
        exit 1
    fi

    local vm_status
    vm_status=$(limactl list --format '{{.Name}}:{{.Status}}' 2>/dev/null | grep "^${VM_NAME}:" | cut -d: -f2)
    if [ "$vm_status" != "Running" ]; then
        log_error "Lima VM '$VM_NAME' 未运行 (当前状态: $vm_status)"
        log_error "请先启动: limactl start $VM_NAME"
        exit 1
    fi

    log_info "环境检查通过 (macOS + limactl + VM '$VM_NAME' Running)"
}

# ============================================================
# 编译
# ============================================================
do_build() {
    log_step "交叉编译 Linux/${TARGET_ARCH} 二进制..."

    mkdir -p "$BUILD_DIR"

    local output="${BUILD_DIR}/${BINARY_NAME}"

    # 检查源码目录
    if [ ! -f "${DOCKER_DIR}/main.go" ]; then
        log_error "源码目录不存在: ${DOCKER_DIR}/main.go"
        exit 1
    fi

    run_cmd env GOOS=linux GOARCH="${TARGET_ARCH}" CGO_ENABLED=0 \
        go build -C "${DOCKER_DIR}" -o "${output}" .

    if [ "$DRY_RUN" = false ]; then
        local size
        size=$(ls -lh "$output" | awk '{print $5}')
        log_info "编译完成: ${output} (${size})"
    fi
}

# ============================================================
# 传输文件到 VM
# ============================================================
do_transfer() {
    log_step "传输文件到 VM '$VM_NAME'..."

    local binary="${BUILD_DIR}/${BINARY_NAME}"

    # 检查二进制文件
    if [ ! -f "$binary" ]; then
        log_error "二进制文件不存在: $binary"
        if [ "$SKIP_BUILD" = true ]; then
            log_error "使用 --skip-build 时需要先执行过一次完整部署"
        fi
        exit 1
    fi

    # 在 VM 中创建临时目录
    run_cmd limactl shell "$VM_NAME" mkdir -p "$VM_TMP"

    # 传输二进制文件
    run_cmd limactl cp "$binary" "${VM_NAME}:${VM_TMP}/${BINARY_NAME}"

    # 传输 install.sh
    run_cmd limactl cp "${DEPLOY_DIR}/install.sh" "${VM_NAME}:${VM_TMP}/install.sh"

    # 传输 service 文件
    run_cmd limactl cp "${DEPLOY_DIR}/docker-connector.service" "${VM_NAME}:${VM_TMP}/docker-connector.service"

    # 传输 env 配置文件（install.sh 会以此文件为主覆盖 VM 中的已有配置）
    if [ -f "${DEPLOY_DIR}/connector.env" ]; then
        run_cmd limactl cp "${DEPLOY_DIR}/connector.env" "${VM_NAME}:${VM_TMP}/connector.env"
    fi

    log_info "文件传输完成"
}

# ============================================================
# 远程安装
# ============================================================
do_remote_install() {
    log_step "在 VM '$VM_NAME' 中执行安装..."

    # 构建 install.sh 命令
    local install_cmd="sudo bash ${VM_TMP}/install.sh --binary=${VM_TMP}/${BINARY_NAME}"

    # 追加用户指定的参数
    for arg in "${INSTALL_ARGS[@]+"${INSTALL_ARGS[@]}"}"; do
        install_cmd="$install_cmd $arg"
    done

    run_cmd limactl shell "$VM_NAME" bash -c "$install_cmd"
}

# ============================================================
# 远程卸载
# ============================================================
do_remote_uninstall() {
    log_step "在 VM '$VM_NAME' 中执行卸载..."

    # 先传输 install.sh（卸载也需要它）
    run_cmd limactl shell "$VM_NAME" mkdir -p "$VM_TMP"
    run_cmd limactl cp "${DEPLOY_DIR}/install.sh" "${VM_NAME}:${VM_TMP}/install.sh"

    run_cmd limactl shell "$VM_NAME" sudo bash "${VM_TMP}/install.sh" --uninstall
}

# ============================================================
# 热更新配置（仅更新 service + env，不传输二进制）
# ============================================================
do_reload_config() {
    log_step "更新 VM '$VM_NAME' 中的配置文件..."

    # 创建 VM 临时目录
    run_cmd limactl shell "$VM_NAME" mkdir -p "$VM_TMP"

    # 传输 service 文件
    run_cmd limactl cp "${DEPLOY_DIR}/docker-connector.service" "${VM_NAME}:${VM_TMP}/docker-connector.service"

    # 传输 env 模板
    if [ -f "${DEPLOY_DIR}/connector.env" ]; then
        run_cmd limactl cp "${DEPLOY_DIR}/connector.env" "${VM_NAME}:${VM_TMP}/connector.env"
    fi

    # 在 VM 中更新配置文件
    log_step "在 VM 中安装配置文件并重启服务..."
    run_cmd limactl shell "$VM_NAME" sudo cp "${VM_TMP}/docker-connector.service" /etc/systemd/system/docker-connector.service

    # 如果有 env 文件，也更新
    if [ -f "${DEPLOY_DIR}/connector.env" ]; then
        run_cmd limactl shell "$VM_NAME" sudo mkdir -p /etc/docker-connector
        run_cmd limactl shell "$VM_NAME" sudo cp "${VM_TMP}/connector.env" /etc/docker-connector/connector.env
    fi

    # daemon-reload + restart
    run_cmd limactl shell "$VM_NAME" sudo systemctl daemon-reload
    run_cmd limactl shell "$VM_NAME" sudo systemctl restart docker-connector

    # 清理临时文件
    run_cmd limactl shell "$VM_NAME" rm -rf "$VM_TMP"

    # 等待服务启动
    sleep 2

    # 检查服务状态
    if limactl shell "$VM_NAME" systemctl is-active --quiet docker-connector 2>/dev/null; then
        log_info "✅ 配置已更新，服务运行正常"
    else
        log_error "❌ 服务启动失败，查看日志:"
        log_error "   limactl shell $VM_NAME -- journalctl -u docker-connector -n 20 --no-pager"
        # 显示最近几行日志帮助排查
        echo ""
        limactl shell "$VM_NAME" journalctl -u docker-connector -n 10 --no-pager 2>/dev/null || true
        return 1
    fi
}

# ============================================================
# 远程查看状态
# ============================================================
do_remote_status() {
    log_step "查看 VM '$VM_NAME' 中的服务状态..."

    # 先传输 install.sh
    run_cmd limactl shell "$VM_NAME" mkdir -p "$VM_TMP"
    run_cmd limactl cp "${DEPLOY_DIR}/install.sh" "${VM_NAME}:${VM_TMP}/install.sh"

    run_cmd limactl shell "$VM_NAME" sudo bash "${VM_TMP}/install.sh" --status
}

# ============================================================
# 清理 VM 中的临时文件
# ============================================================
do_cleanup() {
    log_step "清理临时文件..."
    run_cmd limactl shell "$VM_NAME" rm -rf "$VM_TMP"
    log_info "临时文件已清理"
}

# ============================================================
# 验证部署
# ============================================================
do_verify() {
    log_step "验证部署..."

    echo ""

    # 检查服务是否运行
    if limactl shell "$VM_NAME" systemctl is-active --quiet docker-connector 2>/dev/null; then
        log_info "✅ 服务运行正常"
    else
        log_warn "⚠️  服务未运行，查看日志:"
        log_warn "   limactl shell $VM_NAME -- journalctl -u docker-connector -n 20 --no-pager"
        return 1
    fi

    # 尝试访问 HTTP API（通过 VM 内部验证）
    local health_result
    if health_result=$(limactl shell "$VM_NAME" curl -s --connect-timeout 3 "http://localhost:2522/api/health" 2>/dev/null); then
        log_info "✅ HTTP API 可达: $health_result"
    else
        log_warn "⚠️  HTTP API 暂不可达（可能需要 TUN 隧道先建立连接）"
    fi

    echo ""
}

# ============================================================
# 显示部署摘要
# ============================================================
show_summary() {
    echo ""
    echo -e "${GREEN}============================================================${NC}"
    echo -e "${GREEN} 部署完成！${NC}"
    echo -e "${GREEN}============================================================${NC}"
    echo ""
    echo -e "  VM 名称:     ${BLUE}${VM_NAME}${NC}"
    echo -e "  目标架构:    ${BLUE}linux/${TARGET_ARCH}${NC}"
    echo -e "  编译产物:    ${BLUE}${BUILD_DIR}/${BINARY_NAME}${NC}"
    echo ""
    echo -e "  ${YELLOW}常用命令:${NC}"
    echo -e "    查看状态:  ${BLUE}bash deploy/deploy-to-lima.sh --status${NC}"
    echo -e "    重新部署:  ${BLUE}bash deploy/deploy-to-lima.sh${NC}"
    echo -e "    跳过编译:  ${BLUE}bash deploy/deploy-to-lima.sh --skip-build${NC}"
    echo -e "    更新配置:  ${BLUE}bash deploy/deploy-to-lima.sh --reload${NC}"
    echo -e "    查看日志:  ${BLUE}limactl shell $VM_NAME -- journalctl -u docker-connector -f${NC}"
    echo -e "    卸载:      ${BLUE}bash deploy/deploy-to-lima.sh --uninstall${NC}"
    echo ""
}

# ============================================================
# 主流程
# ============================================================
main() {
    parse_args "$@"
    detect_arch

    echo ""
    echo -e "${GREEN}Docker Connector → Lima VM 部署工具${NC}"
    echo -e "${GREEN}=====================================${NC}"
    echo ""

    if [ "$DRY_RUN" = true ]; then
        log_warn "DRY-RUN 模式：仅显示命令，不实际执行"
        echo ""
    fi

    case "$ACTION" in
        help)
            show_help
            ;;
        reload)
            check_env
            echo ""
            do_reload_config
            ;;
        status)
            check_env
            do_remote_status
            ;;
        uninstall)
            check_env
            do_remote_uninstall
            ;;
        deploy)
            check_env
            echo ""

            # Step 1: 编译
            if [ "$SKIP_BUILD" = true ]; then
                log_info "跳过编译 (--skip-build)"
            else
                do_build
            fi
            echo ""

            # Step 2: 传输
            do_transfer
            echo ""

            # Step 3: 远程安装
            do_remote_install
            echo ""

            # Step 4: 清理临时文件
            do_cleanup
            echo ""

            # Step 5: 验证
            do_verify

            # 摘要
            show_summary
            ;;
    esac
}

main "$@"
