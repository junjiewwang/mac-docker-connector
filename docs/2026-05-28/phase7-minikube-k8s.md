# Phase 7: Minikube & Kubernetes 配置

## 需求概述

在 env-init skill 中新增可选的 Phase 7，实现：
1. macOS `docker` CLI 连接到 Lima VM 里 minikube 的内嵌 Docker
2. macOS `kubectl` 连接到 Lima VM 里 minikube 的 K8s API Server

## 架构设计

```
┌──────────────────── macOS Host ────────────────────┐
│                                                     │
│  docker context: lima-docker   → Lima VM Docker     │ (已有)
│  docker context: minikube      → Minikube Docker    │ (新增)
│  kubectl (KUBECONFIG)          → Minikube K8s API   │ (新增)
│                                                     │
│  端口映射:                                           │
│    127.0.0.1:12376 → VM:12376 → minikube:2376      │
│    127.0.0.1:18443 → VM:18443 → minikube:8443      │
└─────────────────────────────────────────────────────┘
         │ Lima port-forward
         ▼
┌──────────────── Lima VM (docker) ──────────────────┐
│  socat 转发 (minikube-port-forward.service)         │
│    0.0.0.0:12376 → $(minikube ip):2376             │
│    0.0.0.0:18443 → $(minikube ip):8443             │
│                                                     │
│  Docker Engine                                      │
│    └── minikube container (docker driver)           │
│           ├── Docker-in-Docker (:2376)              │
│           └── K8s API Server (:8443)                │
└─────────────────────────────────────────────────────┘
```

## 实施进展

### 已完成

- [x] 新增 `deploy/minikube-port-forward.sh` - socat 端口转发脚本
- [x] 新增 `deploy/minikube-port-forward.service` - systemd 服务单元
- [x] 修改 `skills/env-init/references/lima-vm-template.yaml` - 新增 12376/18443 端口转发
- [x] 修改 `skills/env-init/SKILL.md` - 新增 Phase 7 完整操作步骤
- [x] 修改 `skills/env-init/scripts/verify-env.sh` - 新增 Phase 7 验证检查

### 文件变更清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `deploy/minikube-port-forward.sh` | 新增 | socat 转发脚本，自动重连、PID 管理 |
| `deploy/minikube-port-forward.service` | 新增 | systemd 服务，依赖 minikube-autostart |
| `skills/env-init/references/lima-vm-template.yaml` | 修改 | 新增端口转发 12376, 18443 |
| `skills/env-init/SKILL.md` | 修改 | 新增 Phase 7（条件执行） |
| `skills/env-init/scripts/verify-env.sh` | 修改 | 新增 Phase 7 验证 |

## 服务依赖链

```
docker.service
  └── minikube-autostart.service (oneshot, 启动 minikube)
        └── minikube-port-forward.service (socat 转发)
```

## 端口使用

| 端口 | 用途 | 来源 |
|------|------|------|
| 2521 | docker-connector UDP 隧道 | 已有 |
| 2522 | docker-connector HTTP API | 已有 |
| 12376 | minikube Docker API (TLS) | 新增 |
| 18443 | minikube K8s API Server | 新增 |

## docker context 使用方式

```bash
# 管理普通容器、构建镜像
docker context use lima-docker

# 推送镜像到 K8s 可用的 Docker（免推 registry）
docker context use minikube

# 查看所有 context
docker context ls
```

## kubectl 使用方式

```bash
export KUBECONFIG="$HOME/.kube/minikube-config"
kubectl get nodes
kubectl get pods -A
```

## 遗留问题

- TLS 证书更新：minikube 重建集群后证书会变化，需重新导出到 macOS
- minikube IP 变化：minikube 重建后 IP 可能变化，port-forward 服务会自动重启适配
- 资源消耗：minikube + K8s 组件约占用 2GB 额外内存
