#!/bin/bash
# ============================================================
# Docker Connector - Lima VM 一键安装/升级脚本
# ============================================================
# 用法:
#   # 在 Lima VM 中执行（需要 root 权限）
#   sudo bash install.sh [选项]
#
# 选项:
#   --addr=IP/MASK       虚拟网络地址 (默认: 192.168.251.1/24)
#   --port=PORT          UDP 隧道端口 (默认: 2521)
#   --host=HOST          Desktop 端地址 (默认: host.lima.internal)
#   --http-port=PORT     VM HTTP API 端口 (默认: 2522)
#   --binary=PATH        指定已编译的二进制文件路径 (默认: 从源码编译)
#   --uninstall          卸载服务
#   --status             查看服务状态
#
# 示例:
#   # 默认安装
#   sudo bash install.sh
#
#   # 指定二进制文件安装
#   sudo bash install.sh --binary=./docker-connector
#
#   # 自定义网络地址
#   sudo bash install.sh --addr=10.10.10.1/24 --port=2521
#
#   # 卸载
#   sudo bash install.sh --uninstall
# ============================================================

set -euo pipefail

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 默认配置
CONNECTOR_ADDR="192.168.251.1/24"
CONNECTOR_PORT="2521"
CONNECTOR_HOST="host.lima.internal"
CONNECTOR_HTTP_PORT="2522"
BINARY_PATH=""
ACTION="install"

# 追踪用户显式指定的参数
EXPLICIT_ADDR=false
EXPLICIT_PORT=false
EXPLICIT_HOST=false
EXPLICIT_HTTP_PORT=false

# 安装路径
INSTALL_BIN="/usr/local/bin/docker-connector"
INSTALL_SERVICE="/etc/systemd/system/docker-connector.service"
INSTALL_ENV_DIR="/etc/docker-connector"
INSTALL_ENV="${INSTALL_ENV_DIR}/connector.env"

# Minikube 自动启动相关路径
MINIKUBE_AUTOSTART_SCRIPT="/usr/local/bin/minikube-autostart.sh"
MINIKUBE_AUTOSTART_SERVICE="/etc/systemd/system/minikube-autostart.service"

# 脚本所在目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

log_info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }
log_step()  { echo -e "${BLUE}[STEP]${NC}  $*"; }

# 解析命令行参数
parse_args() {
    for arg in "$@"; do
        case "$arg" in
            --addr=*)       CONNECTOR_ADDR="${arg#*=}"; EXPLICIT_ADDR=true ;;
            --port=*)       CONNECTOR_PORT="${arg#*=}"; EXPLICIT_PORT=true ;;
            --host=*)       CONNECTOR_HOST="${arg#*=}"; EXPLICIT_HOST=true ;;
            --http-port=*)  CONNECTOR_HTTP_PORT="${arg#*=}"; EXPLICIT_HTTP_PORT=true ;;
            --binary=*)     BINARY_PATH="${arg#*=}" ;;
            --uninstall)    ACTION="uninstall" ;;
            --status)       ACTION="status" ;;
            --help|-h)      ACTION="help" ;;
            *)              log_error "未知参数: $arg"; exit 1 ;;
        esac
    done
}

# 显示帮助
show_help() {
    head -30 "$0" | tail -27
    exit 0
}

# 检查运行环境
check_env() {
    # 检查 root 权限
    if [ "$(id -u)" -ne 0 ]; then
        log_error "请以 root 权限运行: sudo bash $0"
        exit 1
    fi

    # 检查操作系统
    if [ "$(uname -s)" != "Linux" ]; then
        log_error "此脚本仅适用于 Linux (Lima VM)"
        exit 1
    fi

    # 检查 systemd
    if ! command -v systemctl &>/dev/null; then
        log_error "systemd 不可用，无法安装服务"
        exit 1
    fi

    log_info "环境检查通过 (Linux + systemd)"
}

# 安装二进制文件
install_binary() {
    log_step "安装二进制文件..."

    if [ -n "$BINARY_PATH" ]; then
        # 使用指定的二进制文件
        if [ ! -f "$BINARY_PATH" ]; then
            log_error "二进制文件不存在: $BINARY_PATH"
            exit 1
        fi

        # 如果服务正在运行，先停止（避免 "Text file busy" 错误）
        if systemctl is-active --quiet docker-connector 2>/dev/null; then
            log_warn "服务正在运行，先停止以更新二进制文件..."
            systemctl stop docker-connector
        fi

        cp "$BINARY_PATH" "$INSTALL_BIN"
        log_info "已复制二进制文件: $BINARY_PATH → $INSTALL_BIN"
    else
        # 检查是否已安装
        if [ -f "$INSTALL_BIN" ]; then
            log_warn "二进制文件已存在: $INSTALL_BIN"
            log_warn "如需更新，请使用 --binary=PATH 指定新版本"
        else
            log_error "未找到二进制文件，请使用以下方式之一："
            log_error "  1. 先编译: cd docker/ && GOOS=linux GOARCH=amd64 go build -o docker-connector ."
            log_error "  2. 指定路径: sudo bash install.sh --binary=./docker-connector"
            exit 1
        fi
    fi

    chmod +x "$INSTALL_BIN"
    log_info "二进制文件权限: $(ls -la "$INSTALL_BIN" | awk '{print $1}')"
}

# 安装配置文件
# 优先级: 命令行参数 > 脚本同目录下的 connector.env > VM 已有配置 > 默认值
install_config() {
    log_step "安装配置文件..."

    mkdir -p "$INSTALL_ENV_DIR"

    # 检查脚本同目录下是否有 connector.env（由 deploy-to-lima.sh 传输过来）
    local source_env="${SCRIPT_DIR}/connector.env"

    if [ -f "$source_env" ]; then
        # 脚本同目录下存在 connector.env，以它为主覆盖 VM 配置
        cp "$source_env" "$INSTALL_ENV"
        log_info "已从部署源更新配置: $source_env → $INSTALL_ENV"

        # 从新写入的 env 文件中读取值
        source "$INSTALL_ENV"

        # 再应用命令行显式指定的参数（命令行优先级最高）
        local cli_updated=false
        if [ "$EXPLICIT_ADDR" = true ]; then
            sed -i "s|^CONNECTOR_ADDR=.*|CONNECTOR_ADDR=${CONNECTOR_ADDR}|" "$INSTALL_ENV"
            log_info "命令行覆盖 CONNECTOR_ADDR=${CONNECTOR_ADDR}"
            cli_updated=true
        fi
        if [ "$EXPLICIT_PORT" = true ]; then
            sed -i "s|^CONNECTOR_PORT=.*|CONNECTOR_PORT=${CONNECTOR_PORT}|" "$INSTALL_ENV"
            log_info "命令行覆盖 CONNECTOR_PORT=${CONNECTOR_PORT}"
            cli_updated=true
        fi
        if [ "$EXPLICIT_HOST" = true ]; then
            sed -i "s|^CONNECTOR_HOST=.*|CONNECTOR_HOST=${CONNECTOR_HOST}|" "$INSTALL_ENV"
            log_info "命令行覆盖 CONNECTOR_HOST=${CONNECTOR_HOST}"
            cli_updated=true
        fi
        if [ "$EXPLICIT_HTTP_PORT" = true ]; then
            sed -i "s|^CONNECTOR_HTTP_PORT=.*|CONNECTOR_HTTP_PORT=${CONNECTOR_HTTP_PORT}|" "$INSTALL_ENV"
            log_info "命令行覆盖 CONNECTOR_HTTP_PORT=${CONNECTOR_HTTP_PORT}"
            cli_updated=true
        fi

        # 重新读取最终生效的值用于显示
        if [ "$cli_updated" = true ]; then
            source "$INSTALL_ENV"
        fi
    elif [ -f "$INSTALL_ENV" ]; then
        # 脚本同目录下无 connector.env，但 VM 已有配置：仅更新命令行显式指定的参数
        local updated=false
        if [ "$EXPLICIT_ADDR" = true ]; then
            sed -i "s|^CONNECTOR_ADDR=.*|CONNECTOR_ADDR=${CONNECTOR_ADDR}|" "$INSTALL_ENV"
            log_info "已更新 CONNECTOR_ADDR=${CONNECTOR_ADDR}"
            updated=true
        fi
        if [ "$EXPLICIT_PORT" = true ]; then
            sed -i "s|^CONNECTOR_PORT=.*|CONNECTOR_PORT=${CONNECTOR_PORT}|" "$INSTALL_ENV"
            log_info "已更新 CONNECTOR_PORT=${CONNECTOR_PORT}"
            updated=true
        fi
        if [ "$EXPLICIT_HOST" = true ]; then
            sed -i "s|^CONNECTOR_HOST=.*|CONNECTOR_HOST=${CONNECTOR_HOST}|" "$INSTALL_ENV"
            log_info "已更新 CONNECTOR_HOST=${CONNECTOR_HOST}"
            updated=true
        fi
        if [ "$EXPLICIT_HTTP_PORT" = true ]; then
            sed -i "s|^CONNECTOR_HTTP_PORT=.*|CONNECTOR_HTTP_PORT=${CONNECTOR_HTTP_PORT}|" "$INSTALL_ENV"
            log_info "已更新 CONNECTOR_HTTP_PORT=${CONNECTOR_HTTP_PORT}"
            updated=true
        fi
        if [ "$updated" = false ]; then
            log_warn "配置文件已存在，未指定新参数，保留现有配置: $INSTALL_ENV"
        fi

        # 从已有 env 文件中读取当前生效的值用于显示
        source "$INSTALL_ENV"
    else
        # 首次安装：用默认值创建配置文件
        cat > "$INSTALL_ENV" <<EOF
# Docker Connector 服务配置
# 修改后执行: systemctl restart docker-connector

CONNECTOR_PORT=${CONNECTOR_PORT}
CONNECTOR_ADDR=${CONNECTOR_ADDR}
CONNECTOR_HOST=${CONNECTOR_HOST}
CONNECTOR_HTTP_PORT=${CONNECTOR_HTTP_PORT}
EOF
        log_info "配置文件已写入: $INSTALL_ENV"
    fi

    log_info "当前配置:"
    log_info "  地址: $CONNECTOR_ADDR"
    log_info "  端口: $CONNECTOR_PORT"
    log_info "  宿主机: $CONNECTOR_HOST"
    log_info "  HTTP 端口: $CONNECTOR_HTTP_PORT"
}

# 安装 systemd 服务
install_service() {
    log_step "安装 systemd 服务..."

    # 如果服务已运行，先停止
    if systemctl is-active --quiet docker-connector 2>/dev/null; then
        log_warn "服务正在运行，先停止..."
        systemctl stop docker-connector
    fi

    # 复制 service 文件
    if [ -f "${SCRIPT_DIR}/docker-connector.service" ]; then
        cp "${SCRIPT_DIR}/docker-connector.service" "$INSTALL_SERVICE"
        log_info "已复制 service 文件"
    else
        log_error "找不到 service 文件: ${SCRIPT_DIR}/docker-connector.service"
        exit 1
    fi

    # 重新加载 systemd
    systemctl daemon-reload
    log_info "systemd 已重新加载"

    # 启用开机自启
    systemctl enable docker-connector
    log_info "已设置开机自启"

    # 启动服务
    systemctl start docker-connector
    log_info "服务已启动"

    # 等待 2 秒后检查状态
    sleep 2
    if systemctl is-active --quiet docker-connector; then
        log_info "✅ 服务运行正常"
    else
        log_error "❌ 服务启动失败，请检查日志:"
        log_error "   journalctl -u docker-connector -n 50 --no-pager"
        exit 1
    fi
}

# 安装 minikube 自动启动服务
install_minikube_autostart() {
    log_step "安装 minikube 自动启动服务..."

    # 检查源文件是否存在
    local source_script="${SCRIPT_DIR}/minikube-autostart.sh"
    local source_service="${SCRIPT_DIR}/minikube-autostart.service"

    if [ ! -f "$source_script" ] || [ ! -f "$source_service" ]; then
        log_warn "minikube-autostart 文件不完整，跳过安装"
        log_warn "  需要: $source_script"
        log_warn "  需要: $source_service"
        return 0
    fi

    # 安装启动脚本
    cp "$source_script" "$MINIKUBE_AUTOSTART_SCRIPT"
    chmod +x "$MINIKUBE_AUTOSTART_SCRIPT"
    log_info "已安装启动脚本: $MINIKUBE_AUTOSTART_SCRIPT"

    # 安装 systemd 服务
    cp "$source_service" "$MINIKUBE_AUTOSTART_SERVICE"
    log_info "已安装 service 文件: $MINIKUBE_AUTOSTART_SERVICE"

    # 重新加载 systemd
    systemctl daemon-reload

    # 启用开机自启
    systemctl enable minikube-autostart
    log_info "已设置 minikube-autostart 开机自启"

    # 如果 minikube 已安装且有集群存在，立即启动
    if command -v minikube &>/dev/null && minikube profile list &>/dev/null; then
        log_info "检测到 minikube 集群存在，正在启动..."
        systemctl start minikube-autostart || log_warn "minikube 启动失败，可稍后手动启动"
    else
        log_info "当前未检测到 minikube 集群，服务将在下次 VM 启动时检查"
    fi

    log_info "✅ minikube-autostart 服务安装完成"
}

# 显示安装后信息
show_post_install() {
    echo ""
    echo -e "${GREEN}============================================================${NC}"
    echo -e "${GREEN} Docker Connector 安装完成！${NC}"
    echo -e "${GREEN}============================================================${NC}"
    echo ""
    echo -e "  二进制文件:  ${BLUE}${INSTALL_BIN}${NC}"
    echo -e "  服务文件:    ${BLUE}${INSTALL_SERVICE}${NC}"
    echo -e "  配置文件:    ${BLUE}${INSTALL_ENV}${NC}"
    echo ""
    echo -e "  ${YELLOW}常用命令:${NC}"
    echo -e "    查看状态:  ${BLUE}systemctl status docker-connector${NC}"
    echo -e "    查看日志:  ${BLUE}journalctl -u docker-connector -f${NC}"
    echo -e "    重启服务:  ${BLUE}systemctl restart docker-connector${NC}"
    echo -e "    停止服务:  ${BLUE}systemctl stop docker-connector${NC}"
    echo ""
    echo -e "  ${YELLOW}Minikube 自动启动:${NC}"
    echo -e "    查看状态:  ${BLUE}systemctl status minikube-autostart${NC}"
    echo -e "    查看日志:  ${BLUE}journalctl -u minikube-autostart -f${NC}"
    echo -e "    禁用自启:  ${BLUE}systemctl disable minikube-autostart${NC}"
    echo ""

    # HTTP API 绑定在 VM 的 local IP 上
    local_ip=$(echo "$CONNECTOR_ADDR" | cut -d'/' -f1)
    echo -e "  ${YELLOW}VM HTTP API:${NC}"
    echo -e "    直接访问:  ${BLUE}curl http://${local_ip}:${CONNECTOR_HTTP_PORT}/api/health${NC}"
    echo -e "    Dashboard: ${BLUE}http://localhost:2521${NC} → VM Links Tab"
    echo ""
}

# 卸载
do_uninstall() {
    log_step "卸载 Docker Connector..."

    # 停止并禁用 minikube-autostart 服务
    if systemctl is-active --quiet minikube-autostart 2>/dev/null; then
        systemctl stop minikube-autostart
        log_info "minikube-autostart 服务已停止"
    fi
    if systemctl is-enabled --quiet minikube-autostart 2>/dev/null; then
        systemctl disable minikube-autostart
        log_info "已取消 minikube-autostart 开机自启"
    fi
    [ -f "$MINIKUBE_AUTOSTART_SERVICE" ] && rm -f "$MINIKUBE_AUTOSTART_SERVICE" && log_info "已删除: $MINIKUBE_AUTOSTART_SERVICE"
    [ -f "$MINIKUBE_AUTOSTART_SCRIPT" ]  && rm -f "$MINIKUBE_AUTOSTART_SCRIPT"  && log_info "已删除: $MINIKUBE_AUTOSTART_SCRIPT"

    # 停止并禁用 docker-connector 服务
    if systemctl is-active --quiet docker-connector 2>/dev/null; then
        systemctl stop docker-connector
        log_info "服务已停止"
    fi
    if systemctl is-enabled --quiet docker-connector 2>/dev/null; then
        systemctl disable docker-connector
        log_info "已取消开机自启"
    fi

    # 删除文件
    [ -f "$INSTALL_SERVICE" ] && rm -f "$INSTALL_SERVICE" && log_info "已删除: $INSTALL_SERVICE"
    [ -f "$INSTALL_BIN" ]     && rm -f "$INSTALL_BIN"     && log_info "已删除: $INSTALL_BIN"

    # 配置文件保留（用户可能有自定义配置）
    if [ -d "$INSTALL_ENV_DIR" ]; then
        log_warn "配置目录保留: $INSTALL_ENV_DIR（如需删除请手动执行: rm -rf $INSTALL_ENV_DIR）"
    fi

    systemctl daemon-reload
    log_info "✅ 卸载完成"
}

# 查看状态
do_status() {
    echo -e "${BLUE}============ Docker Connector 状态 ============${NC}"
    echo ""

    # 二进制文件
    if [ -f "$INSTALL_BIN" ]; then
        echo -e "  二进制: ${GREEN}已安装${NC} ($INSTALL_BIN)"
    else
        echo -e "  二进制: ${RED}未安装${NC}"
    fi

    # 服务状态
    if systemctl is-active --quiet docker-connector 2>/dev/null; then
        echo -e "  服务:   ${GREEN}运行中${NC}"
    elif systemctl is-enabled --quiet docker-connector 2>/dev/null; then
        echo -e "  服务:   ${YELLOW}已停止 (已启用开机自启)${NC}"
    else
        echo -e "  服务:   ${RED}未安装${NC}"
    fi

    # 配置文件
    if [ -f "$INSTALL_ENV" ]; then
        echo -e "  配置:   ${GREEN}已配置${NC} ($INSTALL_ENV)"
        echo ""
        echo -e "  ${YELLOW}当前配置:${NC}"
        grep -v '^#' "$INSTALL_ENV" | grep -v '^$' | while read -r line; do
            echo -e "    ${line}"
        done
    else
        echo -e "  配置:   ${RED}未配置${NC}"
    fi

    echo ""

    # 最近日志
    if systemctl is-active --quiet docker-connector 2>/dev/null; then
        echo -e "  ${YELLOW}最近日志 (最后 5 条):${NC}"
        journalctl -u docker-connector -n 5 --no-pager 2>/dev/null | while read -r line; do
            echo -e "    ${line}"
        done
    fi

    echo ""
}

# 主流程
main() {
    parse_args "$@"

    case "$ACTION" in
        help)
            show_help
            ;;
        uninstall)
            check_env
            do_uninstall
            ;;
        status)
            do_status
            ;;
        install)
            check_env
            log_info "开始安装 Docker Connector (service 模式)..."
            echo ""
            install_binary
            install_config
            install_service
            install_minikube_autostart
            show_post_install
            ;;
    esac
}

main "$@"
