#!/bin/bash
# ============================================================
# Minikube 端口转发脚本
# ============================================================
# 使用 socat 将 minikube 内部服务转发到 VM 的 0.0.0.0 端口，
# 使 macOS 宿主机可通过 Lima 端口转发访问 minikube 的 Docker 和 K8s API。
#
# 转发映射：
#   $(minikube ip):2376 → 0.0.0.0:12376  (minikube Docker API)
#   $(minikube ip):8443 → 0.0.0.0:18443  (minikube K8s API Server)
#
# 依赖：socat, minikube
# ============================================================

set -uo pipefail

LOG_PREFIX="[minikube-port-forward]"

log_info()  { echo "${LOG_PREFIX} [INFO]  $*"; }
log_warn()  { echo "${LOG_PREFIX} [WARN]  $*"; }
log_error() { echo "${LOG_PREFIX} [ERROR] $*"; }

# 转发端口配置
DOCKER_GUEST_PORT=2376
DOCKER_HOST_PORT=12376
K8S_GUEST_PORT=8443
K8S_HOST_PORT=18443

# PID 文件
PID_DIR="/var/run/minikube-port-forward"
mkdir -p "$PID_DIR"

cleanup() {
    log_info "收到停止信号，清理 socat 进程..."
    for pidfile in "$PID_DIR"/*.pid; do
        [ -f "$pidfile" ] || continue
        pid=$(cat "$pidfile" 2>/dev/null)
        if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
            kill "$pid" 2>/dev/null
            log_info "已停止进程 $pid ($(basename "$pidfile" .pid))"
        fi
        rm -f "$pidfile"
    done
    exit 0
}

trap cleanup SIGTERM SIGINT EXIT

# 检查 socat 是否安装
if ! command -v socat &>/dev/null; then
    log_error "socat 未安装，请先安装: apt-get install -y socat"
    exit 1
fi

# 检查 minikube 是否运行
if ! command -v minikube &>/dev/null; then
    log_error "minikube 未安装"
    exit 1
fi

# 获取 minikube IP（等待 minikube 就绪）
MAX_RETRY=30
RETRY=0
MINIKUBE_IP=""

while [ $RETRY -lt $MAX_RETRY ]; do
    MINIKUBE_IP=$(minikube ip 2>/dev/null || echo "")
    if [ -n "$MINIKUBE_IP" ]; then
        break
    fi
    RETRY=$((RETRY + 1))
    log_info "等待 minikube 就绪... ($RETRY/$MAX_RETRY)"
    sleep 5
done

if [ -z "$MINIKUBE_IP" ]; then
    log_error "无法获取 minikube IP，minikube 可能未运行"
    exit 1
fi

log_info "minikube IP: $MINIKUBE_IP"

# 启动 Docker API 转发
start_forward() {
    local name="$1"
    local src_port="$2"
    local dst_host="$3"
    local dst_port="$4"
    local pidfile="$PID_DIR/${name}.pid"

    # 检查端口是否已被占用
    if ss -tlnp 2>/dev/null | grep -q ":${src_port} "; then
        log_warn "端口 $src_port ($name) 已被占用，跳过"
        return 0
    fi

    log_info "启动转发: 0.0.0.0:${src_port} → ${dst_host}:${dst_port} ($name)"
    socat TCP-LISTEN:${src_port},fork,reuseaddr TCP:${dst_host}:${dst_port} &
    local pid=$!
    echo "$pid" > "$pidfile"
    log_info "$name 转发已启动 (PID: $pid)"
}

# 启动两个转发
start_forward "docker-api" "$DOCKER_HOST_PORT" "$MINIKUBE_IP" "$DOCKER_GUEST_PORT"
start_forward "k8s-api" "$K8S_HOST_PORT" "$MINIKUBE_IP" "$K8S_GUEST_PORT"

log_info "所有端口转发已启动，等待信号..."

# 持续运行，等待信号
while true; do
    # 检查子进程是否存活
    for pidfile in "$PID_DIR"/*.pid; do
        [ -f "$pidfile" ] || continue
        pid=$(cat "$pidfile" 2>/dev/null)
        if [ -n "$pid" ] && ! kill -0 "$pid" 2>/dev/null; then
            name=$(basename "$pidfile" .pid)
            log_warn "$name 转发进程 (PID: $pid) 已退出，尝试重启..."
            rm -f "$pidfile"
            case "$name" in
                docker-api)
                    start_forward "docker-api" "$DOCKER_HOST_PORT" "$MINIKUBE_IP" "$DOCKER_GUEST_PORT"
                    ;;
                k8s-api)
                    start_forward "k8s-api" "$K8S_HOST_PORT" "$MINIKUBE_IP" "$K8S_GUEST_PORT"
                    ;;
            esac
        fi
    done
    sleep 10
done
