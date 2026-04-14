---
name: env-init
description: mac-docker-connector 项目环境初始化技能。从全新 macOS 出发，自动完成 Homebrew、Go、Lima 安装，创建 rootful Docker 虚拟机（名称固定为 docker），编译并部署 Desktop 端和 VM 端服务，配置网络路由打通 macOS 与 Docker 容器的网络。当需要初始化开发环境、搭建 mac-docker-connector 运行环境、或从零配置 Docker 网络连通时使用此技能。
---

# mac-docker-connector 环境初始化

从全新 macOS 出发，完成全链路环境搭建。分 6 个阶段执行，每个阶段幂等（可重复执行）。

## 前置条件

- macOS 系统（Apple Silicon 或 Intel）
- 管理员权限（部分步骤需要 sudo）
- 网络连接（下载依赖）

## 执行流程

按顺序执行以下 6 个阶段。每个阶段开始前先检查是否已完成，已完成则跳过。

### Phase 1: 基础工具链安装

依次检查并安装：

```bash
# 1.1 Homebrew
command -v brew || /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

# 1.2 Go 编译器
command -v go || brew install go

# 1.3 Lima
command -v limactl || brew install lima

# 1.4 Python3（VM 网络配置脚本需要）
command -v python3 || brew install python3
```

验证：`brew --version && go version && limactl --version && python3 --version`

### Phase 2: 创建 Lima VM

VM 名称固定为 `docker`，使用 rootful 模式 + Docker 引擎。模板基于 Lima 官方 `docker-rootful.yaml`，额外添加了项目特有配置。

**模板特性**（基于官方 docker-rootful.yaml）：
- `minimumLimaVersion: 2.0.0`
- `base: template:_images/ubuntu-lts` + `template:_default/mounts`（官方镜像引用）
- Docker 安装使用官方一键脚本 `curl -fsSL https://get.docker.com | sh`
- `probes` 探针确保 Docker 安装成功且 dockerd 运行
- `docker.socket` 转发到宿主机（支持 `docker context` 直接使用）
- `daemon.json` 启用 CDI 和 containerd snapshotter
- 额外安装 `iptables` 和 `python3`（docker-connector 依赖）
- 资源配置：4 CPU / 4GiB 内存 / 60GiB 磁盘
- Rosetta 启用（Apple Silicon 运行 x86_64 容器）
- 端口转发 2522（docker-connector HTTP API）

```bash
# 检查 VM 是否已存在
limactl list --format '{{.Name}}' 2>/dev/null | grep -qx "docker"

# 如果不存在，使用项目内模板创建
limactl create --name=docker --tty=false skills/env-init/references/lima-vm-template.yaml

# 启动 VM
limactl start docker

# 配置 docker context（可选，在宿主机直接使用 docker 命令）
docker context create lima-docker --docker "host=unix://$(limactl list docker --format '{{.Dir}}')/sock/docker.sock"
docker context use lima-docker
```

验证：
```bash
limactl list  # 应显示 docker Running
limactl shell docker docker info  # Docker 引擎可用
# 或通过 docker context 直接验证：
docker info  # 如果已配置 docker context
```

**注意**：VM 创建过程需要下载镜像，可能耗时较长（5-15 分钟）。模板内置 probes 探针，会自动等待 Docker 安装完成。

### Phase 3: 源码编译

在项目根目录下编译两个二进制文件：

```bash
# 3.1 编译 Desktop 端（macOS 原生）
mkdir -p build
env GOOS=darwin GOARCH=$(uname -m | sed 's/x86_64/amd64/;s/arm64/arm64/') CGO_ENABLED=0 \
    go build -C desktop -o ../build/docker-connector-desktop .

# 3.2 交叉编译 VM 端（Linux）
env GOOS=linux GOARCH=$(uname -m | sed 's/x86_64/amd64/;s/arm64/arm64/') CGO_ENABLED=0 \
    go build -C docker -o ../build/docker-connector .
```

验证：`ls -la build/docker-connector build/docker-connector-desktop`

### Phase 4: Desktop 端部署

通过 Homebrew 安装 Desktop 端服务。

```bash
# 4.1 添加 tap 并安装（如果尚未安装）
brew tap wenjunxiao/brew
brew install docker-connector

# 4.2 使用项目脚本部署（编译 + 替换二进制 + 同步配置 + 重启服务）
bash deploy/deploy-desktop.sh
```

验证：
```bash
sudo brew services list | grep docker-connector  # 应显示 started
```

**配置说明**：`deploy/connector.env` 是单一配置源，`deploy-desktop.sh` 会自动同步到 Desktop 端配置文件。

### Phase 5: VM 端部署

使用项目脚本一键部署到 Lima VM。

```bash
# 使用 deploy-to-lima.sh，指定 VM 名称为 docker
bash deploy/deploy-to-lima.sh --vm=docker
```

此脚本自动完成：交叉编译 → 传输到 VM → 安装 systemd 服务 → 验证。

验证：
```bash
limactl shell docker sudo systemctl is-active docker-connector  # 应输出 active
```

### Phase 6: 网络配置与验证

配置 macOS 路由和 VM 内网络转发，打通 macOS ↔ Docker 容器网络。

```bash
# 6.1 配置 macOS 路由（Desktop 端启动时自动处理，但需确认配置文件）
# 检查 Desktop 端配置中是否有 route 规则
BREW_PREFIX=$(brew --prefix)
cat "${BREW_PREFIX}/etc/docker-connector.conf"
# 确保包含类似: route 172.17.0.0/16

# 6.2 在 VM 中运行网络配置脚本
limactl shell docker sudo python3 /path/to/setup-docker-network.py
# 或者从宿主机传入脚本执行：
limactl cp scripts/setup-docker-network.py docker:/tmp/setup-docker-network.py
limactl shell docker sudo python3 /tmp/setup-docker-network.py
```

端到端验证：
```bash
# 在 VM 中启动一个测试容器
limactl shell docker docker run -d --name test-nginx nginx

# 获取容器 IP
CONTAINER_IP=$(limactl shell docker docker inspect -f '{{range.NetworkSettings.Networks}}{{.IPAddress}}{{end}}' test-nginx)

# 从 macOS 直接 ping 容器
ping -c 3 $CONTAINER_IP

# 从 macOS 直接 curl 容器
curl http://$CONTAINER_IP

# 清理测试容器
limactl shell docker docker rm -f test-nginx
```

### 环境验证（一键检查）

任何时候可运行验证脚本检查环境状态：

```bash
bash skills/env-init/scripts/verify-env.sh
```

## 配置参考

核心配置文件 `deploy/connector.env`：

| 参数 | 默认值 | 说明 |
|------|--------|------|
| CONNECTOR_PORT | 2521 | UDP 隧道端口 |
| CONNECTOR_ADDR | 192.168.252.1/24 | 虚拟网络地址 |
| CONNECTOR_HOST | host.lima.internal | Desktop 端地址 |
| CONNECTOR_HTTP_PORT | 2522 | VM HTTP API 端口 |

## 故障排查

| 问题 | 排查命令 |
|------|---------|
| Desktop 服务异常 | `bash deploy/deploy-desktop.sh --status` |
| VM 服务异常 | `bash deploy/deploy-to-lima.sh --vm=docker --status` |
| VM 服务日志 | `limactl shell docker -- journalctl -u docker-connector -n 50 --no-pager` |
| Desktop 日志 | `tail -50 $(brew --prefix)/var/log/docker-connector.log` |
| 网络不通 | 检查 TUN 设备: `ifconfig \| grep utun` |
| Docker 不可用 | `limactl shell docker docker info` |
