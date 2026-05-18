#!/bin/bash
# ============================================================
# Minikube 条件自动启动脚本
# ============================================================
# 逻辑：
#   1. 检查 minikube 命令是否存在
#   2. 检查是否存在已创建的 minikube 集群（profile）
#   3. 如果集群已存在但未运行，执行 minikube start
#   4. 如果集群不存在，跳过不做任何操作
# ============================================================

set -uo pipefail

LOG_PREFIX="[minikube-autostart]"

log_info()  { echo "${LOG_PREFIX} [INFO]  $*"; }
log_warn()  { echo "${LOG_PREFIX} [WARN]  $*"; }
log_error() { echo "${LOG_PREFIX} [ERROR] $*"; }

# 检查 minikube 是否已安装
if ! command -v minikube &>/dev/null; then
    log_info "minikube 未安装，跳过自动启动"
    exit 0
fi

# 检查是否存在 minikube 集群 profile
# minikube profile list 返回已创建的集群列表
# 如果没有任何 profile，输出 "No minikube profile was found" 并返回非零退出码
if ! minikube profile list &>/dev/null; then
    log_info "未发现 minikube 集群 profile，跳过自动启动"
    exit 0
fi

# 获取默认 profile 的状态
PROFILE_STATUS=$(minikube status --format='{{.Host}}' 2>/dev/null || echo "")

case "$PROFILE_STATUS" in
    Running)
        log_info "minikube 集群已在运行中，无需操作"
        exit 0
        ;;
    Stopped|"")
        log_info "检测到已有 minikube 集群，正在启动..."
        ;;
    *)
        log_warn "minikube 状态异常: ${PROFILE_STATUS}，尝试启动..."
        ;;
esac

# 构造启动参数（root 用户需要 --force）
START_ARGS="--driver=docker"
if [ "$(id -u)" -eq 0 ]; then
    START_ARGS="${START_ARGS} --force"
    log_info "以 root 用户运行，添加 --force 参数"
fi

# 启动 minikube（继承已有配置）
if minikube start ${START_ARGS} 2>&1; then
    log_info "minikube 集群启动成功"

    # 输出集群信息
    CLUSTER_IP=$(minikube ip 2>/dev/null || echo "unknown")
    log_info "集群 IP: ${CLUSTER_IP}"
    log_info "可通过 'minikube status' 查看详细状态"

    # 通知 docker-connector 重新检测 kubectl 可用性
    # 通过 systemctl reload 发送 SIGHUP 信号，触发 K8s 链路 reconcile
    if systemctl is-active --quiet docker-connector 2>/dev/null; then
        log_info "通知 docker-connector 重新检测 K8s 可用性..."
        systemctl reload docker-connector 2>&1 || log_warn "docker-connector reload 失败（非致命）"
    else
        log_info "docker-connector 未运行，跳过通知（将在启动时自动检测）"
    fi
else
    EXIT_CODE=$?
    log_error "minikube 集群启动失败 (exit code: ${EXIT_CODE})"
    log_error "请手动执行 'minikube start' 排查问题"
    exit ${EXIT_CODE}
fi
