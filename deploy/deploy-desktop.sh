#!/bin/bash
# ============================================================
# Docker Connector Desktop — macOS 一键编译 & 更新
# ============================================================
# 在 macOS 宿主机上运行，自动完成：
#   1. 从源码编译 macOS Desktop 端二进制
#   2. 停止 brew 服务
#   3. 替换二进制文件
#   4. 启动 brew 服务
#   5. 验证服务状态
#
# 前置条件:
#   - 已通过 brew install docker-connector 完成首次安装
#   - Go 编译器已安装
#
# 用法:
#   bash deploy/deploy-desktop.sh [选项]
#
# 选项:
#   --skip-build         跳过编译，直接使用 build/ 下已有的二进制
#   --build-only         仅编译，不替换和重启服务
#   --restart             仅重启服务（不编译、不替换）
#   --status             查看服务状态
#   --logs               查看最近日志
#   --config             打开配置文件进行编辑
#   --dry-run            仅显示将执行的命令，不实际执行
#   --help               显示帮助
#
# 示例:
#   # 编译 + 更新 + 重启
#   bash deploy/deploy-desktop.sh
#
#   # 仅编译（不更新服务）
#   bash deploy/deploy-desktop.sh --build-only
#
#   # 跳过编译，使用上次编译的二进制更新
#   bash deploy/deploy-desktop.sh --skip-build
#
#   # 查看服务状态
#   bash deploy/deploy-desktop.sh --status
#
#   # 查看日志
#   bash deploy/deploy-desktop.sh --logs
#
#   # 仅重启服务
#   bash deploy/deploy-desktop.sh --restart
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
SKIP_BUILD=false
BUILD_ONLY=false
DRY_RUN=false
ACTION="deploy"

# 项目路径（自动检测）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
DESKTOP_DIR="${PROJECT_ROOT}/desktop"
BUILD_DIR="${PROJECT_ROOT}/build"
BINARY_NAME="docker-connector-desktop"

# Homebrew 路径（自动检测）
BREW_PREFIX=""
BREW_BIN=""
BREW_CONF=""
BREW_LOG=""

log_info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }
log_step()  { echo -e "${BLUE}[STEP]${NC}  $*"; }
log_cmd()   { echo -e "${CYAN}  \$ $*${NC}"; }

# ============================================================
# 解析命令行参数
# ============================================================
parse_args() {
    for arg in "$@"; do
        case "$arg" in
            --skip-build)   SKIP_BUILD=true ;;
            --build-only)   BUILD_ONLY=true ;;
            --restart)      ACTION="restart" ;;
            --status)       ACTION="status" ;;
            --logs)         ACTION="logs" ;;
            --config)       ACTION="config" ;;
            --dry-run)      DRY_RUN=true ;;
            --help|-h)      ACTION="help" ;;
            *)              log_error "未知参数: $arg"; show_help; exit 1 ;;
        esac
    done
}

# ============================================================
# 显示帮助
# ============================================================
show_help() {
    head -46 "$0" | tail -43
    exit 0
}

# ============================================================
# 执行或模拟命令
# ============================================================
run_cmd() {
    if [ "$DRY_RUN" = true ]; then
        log_cmd "$@"
    else
        log_cmd "$@"
        "$@"
    fi
}

# ============================================================
# 检测 Homebrew 安装路径
# ============================================================
detect_brew_paths() {
    # 检测 brew prefix
    if command -v brew &>/dev/null; then
        BREW_PREFIX="$(brew --prefix)"
    elif [ -d "/opt/homebrew" ]; then
        BREW_PREFIX="/opt/homebrew"
    elif [ -d "/usr/local" ]; then
        BREW_PREFIX="/usr/local"
    else
        log_error "未找到 Homebrew 安装路径"
        exit 1
    fi

    # 检测 docker-connector 的安装路径
    local cellar_dir="${BREW_PREFIX}/Cellar/docker-connector"
    if [ ! -d "$cellar_dir" ]; then
        log_error "docker-connector 未通过 Homebrew 安装"
        log_error "请先执行: brew install docker-connector"
        exit 1
    fi

    # 获取安装版本目录（取最新版本）
    local version_dir
    version_dir=$(ls -1d "${cellar_dir}"/*/ 2>/dev/null | sort -V | tail -1)
    if [ -z "$version_dir" ]; then
        log_error "未找到 docker-connector 版本目录"
        exit 1
    fi

    BREW_BIN="${version_dir}bin/docker-connector"
    BREW_CONF="${BREW_PREFIX}/etc/docker-connector.conf"
    BREW_LOG="${BREW_PREFIX}/var/log/docker-connector.log"

    log_info "Homebrew prefix: ${BREW_PREFIX}"
    log_info "二进制路径:      ${BREW_BIN}"
    log_info "配置文件:        ${BREW_CONF}"
    log_info "日志文件:        ${BREW_LOG}"
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

    # 检测 Homebrew 路径
    detect_brew_paths

    # 检查 Go 编译器（如果需要编译）
    if [ "$SKIP_BUILD" = false ] && [ "$ACTION" = "deploy" ] || [ "$BUILD_ONLY" = true ]; then
        if ! command -v go &>/dev/null; then
            log_error "未找到 Go 编译器，请先安装:"
            log_error "  brew install go"
            exit 1
        fi
        log_info "Go 版本: $(go version | awk '{print $3}')"
    fi

    # 检查源码目录
    if [ ! -f "${DESKTOP_DIR}/main.go" ]; then
        log_error "Desktop 源码目录不存在: ${DESKTOP_DIR}/main.go"
        log_error "请确保在项目根目录下运行此脚本"
        exit 1
    fi

    log_info "环境检查通过"
}

# ============================================================
# 从 connector.env 同步配置到 docker-connector.conf
# ============================================================
do_sync_config() {
    log_step "从 connector.env 同步配置到 Desktop 端..."

    local env_file="${SCRIPT_DIR}/connector.env"
    if [ ! -f "$env_file" ]; then
        log_warn "connector.env 不存在: ${env_file}，跳过配置同步"
        return 0
    fi

    # 读取 connector.env 中的配置
    local env_port env_addr env_http_port
    env_port=$(grep -E '^CONNECTOR_PORT=' "$env_file" | cut -d'=' -f2 | tr -d '[:space:]')
    env_addr=$(grep -E '^CONNECTOR_ADDR=' "$env_file" | cut -d'=' -f2 | tr -d '[:space:]')
    env_http_port=$(grep -E '^CONNECTOR_HTTP_PORT=' "$env_file" | cut -d'=' -f2 | tr -d '[:space:]')

    if [ -z "$env_port" ] && [ -z "$env_addr" ] && [ -z "$env_http_port" ]; then
        log_warn "connector.env 中未找到有效配置，跳过同步"
        return 0
    fi

    log_info "读取 connector.env:"
    [ -n "$env_port" ]      && log_info "  CONNECTOR_PORT=${env_port}"
    [ -n "$env_addr" ]      && log_info "  CONNECTOR_ADDR=${env_addr}"
    [ -n "$env_http_port" ] && log_info "  CONNECTOR_HTTP_PORT=${env_http_port}"

    # 如果 conf 文件不存在，生成初始配置
    if [ ! -f "$BREW_CONF" ]; then
        log_info "配置文件不存在，生成初始配置: ${BREW_CONF}"
        local conf_content=""
        [ -n "$env_addr" ]      && conf_content+="addr ${env_addr}\n"
        [ -n "$env_port" ]      && conf_content+="port ${env_port}\n"
        [ -n "$env_http_port" ] && conf_content+="vm-http-port ${env_http_port}\n"
        if [ "$DRY_RUN" = false ]; then
            echo -e "$conf_content" | sudo tee "$BREW_CONF" > /dev/null
        else
            log_cmd "echo -e \"${conf_content}\" | sudo tee ${BREW_CONF}"
        fi
        log_info "初始配置文件已生成"
        return 0
    fi

    # conf 文件已存在，用 sed 原地更新对应行
    log_info "更新现有配置文件: ${BREW_CONF}"

    if [ -n "$env_addr" ]; then
        if sudo grep -qE '^\s*addr\s' "$BREW_CONF"; then
            run_cmd sudo sed -i '' "s|^[[:space:]]*addr[[:space:]].*|addr ${env_addr}|" "$BREW_CONF"
        else
            run_cmd sudo bash -c "echo 'addr ${env_addr}' >> '${BREW_CONF}'"
        fi
        log_info "  addr => ${env_addr}"
    fi

    if [ -n "$env_port" ]; then
        if sudo grep -qE '^\s*port\s' "$BREW_CONF"; then
            run_cmd sudo sed -i '' "s|^[[:space:]]*port[[:space:]].*|port ${env_port}|" "$BREW_CONF"
        else
            run_cmd sudo bash -c "echo 'port ${env_port}' >> '${BREW_CONF}'"
        fi
        log_info "  port => ${env_port}"
    fi

    if [ -n "$env_http_port" ]; then
        if sudo grep -qE '^\s*vm-http-port\s' "$BREW_CONF"; then
            run_cmd sudo sed -i '' "s|^[[:space:]]*vm-http-port[[:space:]].*|vm-http-port ${env_http_port}|" "$BREW_CONF"
        else
            run_cmd sudo bash -c "echo 'vm-http-port ${env_http_port}' >> '${BREW_CONF}'"
        fi
        log_info "  vm-http-port => ${env_http_port}"
    fi

    log_info "配置同步完成"
}

# ============================================================
# 编译 Desktop 端二进制
# ============================================================
do_build() {
    log_step "编译 macOS Desktop 端二进制..."

    mkdir -p "$BUILD_DIR"

    local output="${BUILD_DIR}/${BINARY_NAME}"

    run_cmd env GOOS=darwin GOARCH="$(uname -m | sed 's/x86_64/amd64/;s/arm64/arm64/')" CGO_ENABLED=0 \
        go build -C "${DESKTOP_DIR}" -o "${output}" .

    if [ "$DRY_RUN" = false ]; then
        local size
        size=$(ls -lh "$output" | awk '{print $5}')
        log_info "编译完成: ${output} (${size})"
    fi
}

# ============================================================
# 停止 brew 服务
# ============================================================
do_stop_service() {
    log_step "停止 brew 服务..."

    # 检查服务是否在运行（root 服务需 sudo 查看）
    if sudo brew services list 2>/dev/null | grep -q "docker-connector.*started\|docker-connector.*running"; then
        run_cmd sudo brew services stop docker-connector
        # 等待进程完全退出
        sleep 2
        # 确认进程已退出
        if pgrep -f "docker-connector.*-config" &>/dev/null; then
            log_warn "服务进程仍在运行，尝试强制停止..."
            run_cmd sudo pkill -f "docker-connector.*-config" || true
            sleep 1
        fi
        log_info "服务已停止"
    else
        log_info "服务未在运行，跳过停止步骤"
    fi
}

# ============================================================
# 替换二进制文件
# ============================================================
do_replace_binary() {
    log_step "替换二进制文件..."

    local source="${BUILD_DIR}/${BINARY_NAME}"

    # 检查编译产物（dry-run 模式跳过）
    if [ "$DRY_RUN" = false ] && [ ! -f "$source" ]; then
        log_error "编译产物不存在: $source"
        if [ "$SKIP_BUILD" = true ]; then
            log_error "使用 --skip-build 时需要先执行过一次完整部署"
        fi
        exit 1
    fi

    # 备份当前二进制（如果存在且不是备份文件）
    if [ -f "$BREW_BIN" ]; then
        local bak="${BREW_BIN}.bak"
        log_info "备份当前二进制: ${bak}"
        run_cmd sudo cp "$BREW_BIN" "$bak"
    fi

    # 复制新的二进制
    run_cmd sudo cp "$source" "$BREW_BIN"
    run_cmd sudo chmod 755 "$BREW_BIN"

    if [ "$DRY_RUN" = false ]; then
        local size
        size=$(ls -lh "$BREW_BIN" | awk '{print $5}')
        log_info "二进制已更新: ${BREW_BIN} (${size})"
    fi
}

# ============================================================
# 启动 brew 服务
# ============================================================
do_start_service() {
    log_step "启动 brew 服务..."

    run_cmd sudo brew services start docker-connector

    # 等待服务启动
    sleep 3
}

# ============================================================
# 验证服务状态
# ============================================================
do_verify() {
    log_step "验证服务状态..."

    echo ""

    # 检查 brew services 状态（root 服务需 sudo 查看）
    local svc_status
    svc_status=$(sudo brew services list 2>/dev/null | grep "^docker-connector " || echo "")

    if echo "$svc_status" | grep -q "started\|running"; then
        log_info "✅ brew 服务运行正常"
        echo -e "    ${CYAN}${svc_status}${NC}"
    else
        log_warn "⚠️  brew 服务状态异常"
        echo -e "    ${YELLOW}${svc_status}${NC}"
    fi

    # 检查进程是否存在
    if pgrep -f "docker-connector.*-config" &>/dev/null; then
        local pid
        pid=$(pgrep -f "docker-connector.*-config" | head -1)
        log_info "✅ 进程运行中 (PID: ${pid})"
    else
        log_warn "⚠️  未检测到 docker-connector 进程"
    fi

    # 检查日志中是否有错误
    if [ -f "$BREW_LOG" ]; then
        local recent_errors
        recent_errors=$(tail -5 "$BREW_LOG" 2>/dev/null | grep -i "fatal\|error\|panic" || echo "")
        if [ -n "$recent_errors" ]; then
            log_warn "⚠️  最近日志中发现错误:"
            echo "$recent_errors" | while read -r line; do
                echo -e "    ${RED}${line}${NC}"
            done
        else
            log_info "✅ 最近日志无错误"
        fi
    fi

    # 检查 Dashboard HTTP 端口
    if curl -s --connect-timeout 2 "http://127.0.0.1:2511" &>/dev/null; then
        log_info "✅ Dashboard HTTP 端口可达 (2511)"
    else
        log_info "ℹ️  Dashboard HTTP 端口暂不可达（可能需要客户端连接后才可用）"
    fi

    echo ""
}

# ============================================================
# 显示服务状态
# ============================================================
do_show_status() {
    log_step "Docker Connector Desktop 服务状态"
    echo ""

    # brew services 状态（root 服务需 sudo 查看）
    echo -e "${BLUE}[brew services]${NC}"
    sudo brew services list 2>/dev/null | head -1
    sudo brew services list 2>/dev/null | grep "docker-connector" || echo "  未找到服务"
    echo ""

    # 进程信息
    echo -e "${BLUE}[进程信息]${NC}"
    local proc_info
    proc_info=$(ps aux 2>/dev/null | grep "[d]ocker-connector" || echo "")
    if [ -n "$proc_info" ]; then
        echo "$proc_info"
    else
        echo "  未检测到运行中的进程"
    fi
    echo ""

    # 二进制版本
    echo -e "${BLUE}[二进制信息]${NC}"
    if [ -f "$BREW_BIN" ]; then
        ls -lh "$BREW_BIN"
    else
        echo "  二进制文件不存在"
    fi
    echo ""

    # 配置文件
    echo -e "${BLUE}[配置文件]${NC}"
    if [ -f "$BREW_CONF" ]; then
        echo "  路径: ${BREW_CONF}"
        echo "  ---"
        cat "$BREW_CONF" | sed 's/^/  /'
    else
        echo "  配置文件不存在"
    fi
    echo ""

    # 最近日志
    echo -e "${BLUE}[最近日志 (最后 10 行)]${NC}"
    if [ -f "$BREW_LOG" ]; then
        tail -10 "$BREW_LOG" | sed 's/^/  /'
    else
        echo "  日志文件不存在"
    fi
    echo ""
}

# ============================================================
# 显示日志
# ============================================================
do_show_logs() {
    log_step "Docker Connector Desktop 日志"
    echo ""

    if [ -f "$BREW_LOG" ]; then
        log_info "日志文件: ${BREW_LOG}"
        echo ""
        tail -50 "$BREW_LOG"
        echo ""
        log_info "实时跟踪日志:"
        log_info "  tail -f ${BREW_LOG}"
    else
        log_warn "日志文件不存在: ${BREW_LOG}"
    fi
}

# ============================================================
# 打开配置文件
# ============================================================
do_edit_config() {
    if [ ! -f "$BREW_CONF" ]; then
        log_error "配置文件不存在: ${BREW_CONF}"
        exit 1
    fi

    local editor="${EDITOR:-vi}"
    log_info "使用 ${editor} 打开配置文件: ${BREW_CONF}"
    sudo "$editor" "$BREW_CONF"
    echo ""
    log_info "配置已修改，重启服务以生效:"
    log_info "  bash deploy/deploy-desktop.sh --restart"
}

# ============================================================
# 重启服务
# ============================================================
do_restart() {
    log_step "重启 Docker Connector Desktop 服务..."
    run_cmd sudo brew services restart docker-connector
    sleep 3
    do_verify
}

# ============================================================
# 显示部署摘要
# ============================================================
show_summary() {
    echo ""
    echo -e "${GREEN}============================================================${NC}"
    echo -e "${GREEN} Desktop 端更新完成！${NC}"
    echo -e "${GREEN}============================================================${NC}"
    echo ""
    echo -e "  二进制:      ${BLUE}${BREW_BIN}${NC}"
    echo -e "  配置文件:    ${BLUE}${BREW_CONF}${NC}"
    echo -e "  日志文件:    ${BLUE}${BREW_LOG}${NC}"
    echo -e "  编译产物:    ${BLUE}${BUILD_DIR}/${BINARY_NAME}${NC}"
    echo ""
    echo -e "  ${YELLOW}常用命令:${NC}"
    echo -e "    查看状态:    ${BLUE}bash deploy/deploy-desktop.sh --status${NC}"
    echo -e "    查看日志:    ${BLUE}bash deploy/deploy-desktop.sh --logs${NC}"
    echo -e "    仅重启:      ${BLUE}bash deploy/deploy-desktop.sh --restart${NC}"
    echo -e "    跳过编译:    ${BLUE}bash deploy/deploy-desktop.sh --skip-build${NC}"
    echo -e "    编辑配置:    ${BLUE}bash deploy/deploy-desktop.sh --config${NC}"
    echo -e "    实时日志:    ${BLUE}tail -f ${BREW_LOG}${NC}"
    echo ""
}

# ============================================================
# 主流程
# ============================================================
main() {
    parse_args "$@"

    echo ""
    echo -e "${GREEN}Docker Connector Desktop — macOS 编译 & 更新工具${NC}"
    echo -e "${GREEN}====================================================${NC}"
    echo ""

    if [ "$DRY_RUN" = true ]; then
        log_warn "DRY-RUN 模式：仅显示命令，不实际执行"
        echo ""
    fi

    case "$ACTION" in
        help)
            show_help
            ;;
        status)
            check_env
            echo ""
            do_show_status
            ;;
        logs)
            check_env
            echo ""
            do_show_logs
            ;;
        config)
            check_env
            echo ""
            do_edit_config
            ;;
        restart)
            check_env
            echo ""
            do_restart
            ;;
        deploy)
            check_env
            echo ""

            # Step 0: 从 connector.env 同步配置
            do_sync_config
            echo ""

            # Step 1: 编译
            if [ "$SKIP_BUILD" = true ]; then
                log_info "跳过编译 (--skip-build)"
            else
                do_build
            fi
            echo ""

            # 如果仅编译模式，到这里就结束
            if [ "$BUILD_ONLY" = true ]; then
                log_info "仅编译模式 (--build-only)，跳过服务更新"
                echo ""
                show_summary
                return 0
            fi

            # Step 2: 停止服务
            do_stop_service
            echo ""

            # Step 3: 替换二进制
            do_replace_binary
            echo ""

            # Step 4: 启动服务
            do_start_service
            echo ""

            # Step 5: 验证
            do_verify

            # 摘要
            show_summary
            ;;
    esac
}

main "$@"
