# Minikube 自动启动服务

## 需求

Lima VM 启动时，如果存在已创建的 minikube 集群，自动将其拉起；如果不存在 minikube 集群则不做任何操作。

## 设计方案

采用独立 systemd 服务（方案 B），符合单一职责原则，可独立 enable/disable。

### 启动顺序

```
network-online.target → docker.service → minikube-autostart.service → docker-connector.service
```

### 条件判断逻辑

1. 检查 `minikube` 命令是否存在 → 不存在则 exit 0
2. 检查 `minikube profile list` 是否有已创建的集群 → 没有则 exit 0
3. 检查集群状态：已运行则跳过，已停止则执行 `minikube start`

## 文件清单

| 文件 | 安装位置 | 说明 |
|------|----------|------|
| `deploy/minikube-autostart.service` | `/etc/systemd/system/minikube-autostart.service` | systemd 服务定义 |
| `deploy/minikube-autostart.sh` | `/usr/local/bin/minikube-autostart.sh` | 条件启动脚本 |

## 安装方式

通过 `deploy/install.sh` 自动安装（已集成到主安装流程），或手动：

```bash
sudo cp minikube-autostart.sh /usr/local/bin/minikube-autostart.sh
sudo chmod +x /usr/local/bin/minikube-autostart.sh
sudo cp minikube-autostart.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable minikube-autostart
```

## 常用命令

```bash
# 查看状态
systemctl status minikube-autostart

# 查看日志
journalctl -u minikube-autostart -f

# 禁用自动启动
systemctl disable minikube-autostart

# 手动触发
systemctl start minikube-autostart
```

## 实施进展

- [x] 创建 `minikube-autostart.sh` 条件启动脚本
- [x] 创建 `minikube-autostart.service` systemd 服务文件
- [x] 集成到 `install.sh` 安装/卸载流程
- [x] 集成到 `deploy-to-lima.sh` 文件传输流程
- [x] 更新 `docker-connector.service` 启动顺序（After minikube-autostart）
- [x] 事件驱动 kubectl 检测：minikube 就绪后通过 SIGHUP 通知 docker-connector

## 事件驱动 kubectl 检测

### 问题背景

`kubectlAvailable()` 原先使用 `sync.Once` 只检查一次。如果 docker-connector 启动时 minikube 尚未就绪，kubectl 检测失败后永远不会重试，导致 K8s 链路（host-k8s.service / host-k8s.pod）始终不可用。

### 解决方案

采用事件驱动机制，避免盲目重试：

```
minikube-autostart.sh
  ├── minikube start 成功
  └── systemctl reload docker-connector  (发送 SIGHUP)
         │
         ▼
docker-connector (main.go)
  ├── 收到 SIGHUP
  ├── ResetKubectlCheck()  → 重置 kubectl 可用性缓存
  └── reconciler.Reconcile("sighup-reload")  → 立即触发 reconcile
         │
         ▼
kubectlAvailable() 重新执行检测 → K8s 链路正常建立
```

### 改动点

| 文件 | 改动 |
|------|------|
| `docker/infra_network.go` | `sync.Once` → mutex+bool 可重置机制，新增 `ResetKubectlCheck()` |
| `docker/main.go` | 添加 SIGHUP handler，收到信号后重置+reconcile |
| `deploy/docker-connector.service` | 添加 `ExecReload=/bin/kill -HUP $MAINPID` |
| `deploy/minikube-autostart.sh` | 启动成功后 `systemctl reload docker-connector` |

## 遗留问题

- 暂无
