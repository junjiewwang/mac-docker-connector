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

## 遗留问题

- 暂无
