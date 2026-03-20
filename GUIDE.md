
# 🐳 Desktop Docker Connector — 使用指南

> **一句话概括：** 让你的 macOS/Windows 宿主机可以**直接用 IP 访问 Docker 容器**，就像它们跑在本地一样。

---

## 🤔 为什么需要这个工具？

如果你用过 Docker Desktop for Mac 或 Windows，一定遇到过这个经典的"灵魂拷问"：

> _"我在 Docker 里跑了个 Redis，容器 IP 是 `172.17.0.2`，为什么在 Mac 上 `ping 172.17.0.2` 不通？"_

Docker 官方对此的回答是：**"这是已知限制"** 🤷‍♂️（[Known limitations](https://docs.docker.com/docker-for-mac/networking/#i-cannot-ping-my-containers)）。

原因是 Docker Desktop 运行在一个轻量级虚拟机（HyperKit/Lima）里，macOS 宿主机和 Docker 容器之间隔了一层 Hypervisor，网络不互通。

**Desktop Docker Connector** 就是为了解决这个问题而生的——它在宿主机和虚拟机之间建立一条 **UDP TUN 隧道**，让你的 Mac/Windows **像在 Linux 上一样直接访问容器 IP**。

```
                    ✨ 魔法发生的地方
                         │
+------------------+     │     +------------------+
|   🖥 macOS       |     │     |   🐧 Linux VM     |
|                  |  TUN 隧道  |                  |
|  ping 172.17.0.2 | ◀══════▶ |  🐳 Docker        |
|  ✅ 通了！       |           |  📦 Containers    |
+------------------+           +------------------+
```

### ✨ 不只是 ping 通

| 能力 | 说明 | 你能做什么 |
|:---:|------|-----------|
| 🔗 | **直接访问容器 IP** | `curl 172.17.0.2:8080`、`redis-cli -h 172.20.0.3` |
| 🌉 | **Docker 子网互通** | 让 `bridge-A` 的容器和 `bridge-B` 的容器互相通信 |
| ☸️ | **K8s 集成** | macOS 直接访问 Minikube Pod IP 和 Service ClusterIP |
| 🌐 | **容器上网** | 让 Docker/K8s 容器访问外网（NAT） |
| 🖥 | **Web Dashboard** | 内置管理面板，路由可视化 + 一键修复 |
| 📡 | **VM 链路管理** | 5 条网络链路可视化管理，SVG 拓扑图 |
| 🔧 | **配置热加载** | 修改配置文件无需重启，实时生效 |
| 📤 | **容器导出** | 把你的容器暴露给同事，让他也能访问 |

---

## 🚀 3 分钟快速上手

### 前置条件

- macOS（推荐）或 Windows
- Docker Desktop 已安装并运行
- Homebrew（macOS 用户）

### Step 1️⃣ — 安装 Desktop 端

```bash
# 添加 tap 源
brew tap wenjunxiao/brew

# 安装 connector
brew install docker-connector
```

### Step 2️⃣ — 配置 Docker 子网路由

把当前 Docker 所有 bridge 子网加入配置文件：

```bash
docker network ls --filter driver=bridge --format "{{.ID}}" \
  | xargs docker network inspect --format "route {{range .IPAM.Config}}{{.Subnet}}{{end}}" \
  >> "$(brew --prefix)/etc/docker-connector.conf"
```

> 💡 **Tip**: 以后新建了 Docker 网络，只需往配置文件追加一行 `route x.x.x.x/xx` 即可，无需重启。

### Step 3️⃣ — 启动 Desktop 端服务

```bash
sudo brew services start docker-connector
```

### Step 4️⃣ — 启动 Docker 端

```bash
# 拉取容器镜像
docker pull wenjunxiao/mac-docker-connector

# 启动（必须 host 网络 + NET_ADMIN）
docker run -it -d --restart always \
  --net host --cap-add NET_ADMIN \
  --name mac-connector \
  wenjunxiao/mac-docker-connector
```

### Step 5️⃣ — 验证 🎉

```bash
# 找一个正在运行的容器 IP
docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' <容器名>

# 直接 ping！
ping 172.17.0.2
# PING 172.17.0.2: 64 data bytes
# 64 bytes from 172.17.0.2: icmp_seq=0 ttl=63 time=0.8ms ✅
```

> 🎊 **就这样！** 你的 macOS 现在可以直接用 IP 访问 Docker 容器了。

---

## 🖥 Web Dashboard — 你的网络指挥中心

启动 Desktop 端后，打开浏览器访问：

```
http://localhost:2511
```

你会看到一个精心设计的管理面板，它能帮你：

### 📊 Routes 路由面板

| 功能 | 说明 |
|------|------|
| **路由状态总览** | 一眼看到哪些路由正常（✅ OK）、哪些缺失（❌ MISSING） |
| **一键修复** | 点击 "Fix All" 按钮自动补齐缺失路由 |
| **连接状态** | 运行时间、TUN 接口、Peer IP、客户端连接状态 |
| **自动刷新** | 每 5 秒轮询，差量 DOM 更新（不闪烁） |

### ⚙️ Configuration 配置面板

| 功能 | 说明 |
|------|------|
| **YAML 编辑器** | 直接在浏览器中编辑路由、DNS、Expose 配置 |
| **Docker 子网发现** | 点击按钮自动发现 VM 中的 Docker 子网 |
| **实时预览** | 编辑后即时看到配置变化 |
| **保存 & 重载** | 一键保存，配置热加载生效 |

### 🔗 VM Links 链路面板（需要服务模式）

| 功能 | 说明 |
|------|------|
| **5 条链路状态卡片** | 每条链路独立显示 Active/Partial/Inactive 状态 |
| **Apply / Revert 操作** | 一键启用/还原网络规则 |
| **规则详情展开** | 查看每条链路的 iptables/route/dns 具体规则 |
| **网络拓扑图** | 交互式 SVG 图，链路颜色随状态实时变化 |
| **SSE 实时推送** | 状态变化自动推送到浏览器，无需手动刷新 |

---

## 🎯 实战场景教程

### 场景 1：macOS 直接连接 Docker 里的 MySQL

```bash
# 创建一个自定义网络并启动 MySQL
docker network create --subnet=172.20.0.0/16 mynet
docker run -d --name mysql --net mynet --ip 172.20.0.10 \
  -e MYSQL_ROOT_PASSWORD=123456 mysql:8.0

# 确保路由已配置
echo "route 172.20.0.0/16" >> "$(brew --prefix)/etc/docker-connector.conf"

# 直接从 macOS 连接！
mysql -h 172.20.0.10 -u root -p123456
# Welcome to the MySQL monitor. ✅
```

### 场景 2：两个 Docker 子网的容器互相通信

默认情况下，不同 bridge 网络的容器是隔离的。通过 connector 的 iptables 规则可以打通：

```bash
# 创建两个独立子网
docker network create --subnet=172.21.0.0/16 net-a
docker network create --subnet=172.22.0.0/16 net-b

# 在配置文件中添加互通规则
echo "iptables 172.21.0.0+172.22.0.0" >> "$(brew --prefix)/etc/docker-connector.conf"

# 现在 net-a 和 net-b 的容器可以互相 ping 了 🎉
```

### 场景 3：macOS 直接访问 Minikube Pod（服务模式）

如果你在 Lima VM 中跑 Minikube，以服务模式部署 connector 后：

```bash
# 在 Dashboard VM Links 面板中 Apply "host-k8s" 链路
# 或者用 API：
curl -X POST http://localhost:2511/api/vm/apply -d '{"links":["host-k8s"]}'

# 获取一个 Pod IP
kubectl get pods -o wide
# NAME          IP
# nginx-xxx     10.244.0.5

# macOS 直接访问 Pod！
curl http://10.244.0.5:80
# <html>Welcome to nginx!</html> ✅

# 甚至可以用 K8s Service ClusterIP！
kubectl get svc
# NAME       CLUSTER-IP      PORT
# my-svc     10.96.123.45    80

curl http://10.96.123.45:80  ✅
```

### 场景 4：把容器导出给同事

你在本地搭了一套开发环境，想让同事直接访问：

```bash
# 在你的配置文件中开启 expose
echo "expose 0.0.0.0:2512" >> "$(brew --prefix)/etc/docker-connector.conf"

# 给同事分配一个 token 和虚拟 IP
echo "token colleague-john 224.223.203.10" >> "$(brew --prefix)/etc/docker-connector.conf"

# 标记要导出的路由
# route 172.20.0.0/16 expose
```

同事只需安装 accessor 客户端并使用你提供的 token 连接即可。

---

## 🐧 Lima VM 服务模式（进阶）

对于使用 **Lima VM** 的用户，connector 可以部署为原生 Linux systemd 服务。相比 Docker 容器模式，服务模式功能更强大：

| 对比 | Docker 容器模式 | Linux 服务模式 |
|------|:--------------:|:-------------:|
| 部署方式 | `docker run` | `systemctl start` |
| iptables 操作 | ✅ 需要 NET_ADMIN | ✅ 原生 root |
| kubectl / K8s 集成 | ❌ 需要额外挂载 | ✅ 直接可用 |
| DNS 配置 (systemd-resolved) | ❌ 不可用 | ✅ 直接操作 |
| VM 链路管理 (5 条链路) | ❌ 不支持 | ✅ 完整支持 |
| Web Dashboard 链路面板 | ❌ 不可用 | ✅ 实时可视化 |
| 调试方式 | `docker exec` + `docker logs` | `journalctl -u docker-connector` |

### 一键部署

```bash
# 在 macOS 上执行（自动编译 → 传输 → 安装）
bash deploy/deploy-to-lima.sh

# 指定 VM 名称和网络地址
bash deploy/deploy-to-lima.sh --vm=docker --addr=192.168.100.1/24

# 查看部署状态
bash deploy/deploy-to-lima.sh --status
```

### 常用管理命令

```bash
# 查看服务状态
bash deploy/deploy-to-lima.sh --status

# 更新代码后重新部署
bash deploy/deploy-to-lima.sh

# 仅更新配置（不重新编译）
bash deploy/deploy-to-lima.sh --reload

# 查看实时日志
limactl shell default -- journalctl -u docker-connector -f

# 卸载
bash deploy/deploy-to-lima.sh --uninstall
```

### Desktop 端开发迭代

如果你修改了 Desktop 端源码，同样有一键更新脚本：

```bash
# 编译 + 替换 brew 二进制 + 重启服务
bash deploy/deploy-desktop.sh

# 仅编译不更新
bash deploy/deploy-desktop.sh --build-only

# 查看状态
bash deploy/deploy-desktop.sh --status

# 查看日志
bash deploy/deploy-desktop.sh --logs
```

---

## ⚙️ 配置详解

配置文件位置：`$(brew --prefix)/etc/docker-connector.conf`

### 基础配置（通常无需修改）

```ini
# 虚拟网络地址（Desktop 和 Docker 端需保持一致）
addr 192.168.251.1/24

# UDP 监听端口
port 2511

# MTU 值
mtu 1400

# UDP 监听地址（安全起见绑定本地）
host 127.0.0.1
```

### 路由配置（动态热加载 🔥）

```ini
# 添加 Docker 子网路由（每一行一个）
route 172.17.0.0/16
route 172.20.0.0/16
route 172.21.0.0/16

# 标记为可导出的路由（需配合 expose 使用）
route 172.20.0.0/16 expose
```

### 子网互通规则

```ini
# 用 + 连接表示插入互通规则
iptables 172.21.0.0+172.22.0.0

# 用 - 连接表示删除互通规则
iptables 172.21.0.0-172.22.0.0
```

### 容器导出

```ini
# 开放导出端口
expose 0.0.0.0:2512

# token 名称 + 分配的虚拟 IP
token alice 224.223.203.10
token bob 224.223.203.11
```

### 自定义 DNS & 代理

```ini
# 容器内可使用本地 hosts 中的 .local / .inc 域名
hosts /etc/hosts .local .inc

# 让容器访问本地 127.0.0.1:3000 的服务
proxy 127.0.0.1:3000:3000
```

> 💡 **热加载提示**：除了 `addr`、`port`、`mtu`、`host` 这四项基础配置外，所有其他配置修改后**立即生效**，无需重启服务。

---

## 🔍 网络架构全景

```
+-------------------------------------------------------------------+
|                       macOS 宿主机                                 |
|                                                                   |
|  🌐 浏览器 ──HTTP──▶ desktop-docker-connector                     |
|                       ├─ UDP :2511  (TUN 隧道)                    |
|                       ├─ HTTP :2511 (Web Dashboard)               |
|                       │   ├─ /api/status           (连接状态)      |
|                       │   ├─ /api/routes/verify    (路由校验)      |
|                       │   ├─ /api/routes/fix       (一键修复)      |
|                       │   ├─ /api/config/*         (配置管理)      |
|                       │   └─ /api/vm/*    ─────────────────┐      |
|                       └─ 配置: docker-connector.conf       │      |
|                                                            │      |
|                         ▲ UDP 隧道                   反向代理     |
|                         │                              │          |
+-------------------------│------------------------------│----------+
                          │                              │
+-------------------------▼------------------------------▼----------+
|                   Lima VM / Hypervisor                             |
|                                                                   |
|  docker-connector (systemd / container)                           |
|  ├─ TUN Client ◀══ UDP 隧道 ══▶ Desktop                          |
|  ├─ HTTP :2522 (VM API, 仅内网可见)                                |
|  └─ LinkManager                                                   |
|      ├─ 🌐 InternetLink        容器 ↔ 外网                       |
|      ├─ 🖥 HostDockerLink      宿主机 ↔ Docker                    |
|      ├─ ☸️ HostK8sLink         宿主机 ↔ K8s                       |
|      ├─ 🔗 DockerK8sLink       Docker ↔ K8s                      |
|      └─ 🐳 DockerDockerLink    Docker ↔ Docker (跨子网)           |
|                                                                   |
|     Docker Daemon  ◀──▶  Minikube / K8s                           |
+-------------------------------------------------------------------+
```

---

## 🐛 常见问题排查

### ❓ Q: ping 不通容器 IP

**检查清单：**

1. **Desktop 端服务是否运行？**
   ```bash
   sudo brew services list | grep docker-connector
   ```

2. **Docker 端容器是否启动？**
   ```bash
   docker ps | grep mac-connector
   ```

3. **路由是否配置？**
   ```bash
   # 查看配置文件中的路由
   cat "$(brew --prefix)/etc/docker-connector.conf" | grep route
   
   # 查看系统路由表
   netstat -rn | grep 172
   ```

4. **打开 Dashboard 看一眼**
   ```
   http://localhost:2511
   ```
   MISSING 状态的路由点击 "Fix" 即可修复。

### ❓ Q: 新建了 Docker 网络，如何添加路由？

```bash
# 方式一：手动追加
echo "route 172.25.0.0/16" >> "$(brew --prefix)/etc/docker-connector.conf"

# 方式二：在 Dashboard Configuration 面板中添加

# 方式三：使用 Docker 子网发现（服务模式）
# Dashboard → Configuration → 点击 "Discover Docker Subnets" 按钮
```

### ❓ Q: Dashboard 打不开？

Dashboard 与 UDP 隧道共用 `:2511` 端口。确保：
1. Desktop 端服务正在运行
2. 浏览器访问 `http://localhost:2511`（不是 `https`）

### ❓ Q: VM 链路面板显示 "VM 不可达"？

说明 Desktop 无法连接到 VM 的 HTTP API。检查：
1. VM 端是否以**服务模式**运行（需要 `-mode=service`）
2. TUN 隧道是否建立（看 Dashboard 连接状态是否为 Connected）
3. VM 中服务状态：`limactl shell default -- systemctl status docker-connector`

### ❓ Q: 如何调试网络不通的问题？

```bash
# 1. 启用 DEBUG 日志
./desktop-docker-connector -log-level DEBUG -log-file debug.log

# 2. 查看数据包流向
grep "TUN->UDP" debug.log   # 宿主机 → 容器方向
grep "UDP->TUN" debug.log   # 容器 → 宿主机方向

# 3. 只有去没有回？检查容器内的路由和 iptables
docker exec mac-connector ip route
docker exec mac-connector iptables -L FORWARD -n
```

更多调试技巧请参考 [DEBUG_GUIDE.md](./DEBUG_GUIDE.md)。

---

## 📋 HTTP API 参考

### Desktop 端 API（`:2511`）

| 端点 | 方法 | 说明 |
|------|:----:|------|
| `GET /` | GET | Web Dashboard 页面 |
| `GET /api/status` | GET | 连接器运行状态 |
| `GET /api/routes/verify` | GET | 路由校验结果 |
| `POST /api/routes/fix` | POST | 修复缺失路由 |
| `GET /api/config` | GET | 获取当前配置 |
| `POST /api/config` | POST | 保存配置 |
| `GET /api/vm/links` | GET | VM 链路状态（反向代理） |
| `GET /api/vm/links/stream` | GET | VM 链路 SSE 实时推送 |
| `POST /api/vm/apply` | POST | 应用 VM 链路规则 |
| `POST /api/vm/revert` | POST | 还原 VM 链路规则 |
| `GET /api/vm/network/info` | GET | VM 网络信息 |

### 使用示例

```bash
# 查看连接状态
curl -s http://localhost:2511/api/status | jq

# 路由校验
curl -s http://localhost:2511/api/routes/verify | jq

# 查看 VM 链路
curl -s http://localhost:2511/api/vm/links | jq '.links[] | {name, status, rules_active, rules_total}'

# 一键 Apply 所有链路
curl -X POST http://localhost:2511/api/vm/apply | jq

# Apply 指定链路
curl -X POST http://localhost:2511/api/vm/apply \
  -H 'Content-Type: application/json' \
  -d '{"links":["host-docker","internet"]}'

# SSE 实时监听（Ctrl+C 退出）
curl -N http://localhost:2511/api/vm/links/stream
```

---

## 🏗 项目结构

```
desktop-docker-connector/
├── 📂 desktop/                  # macOS/Windows 宿主机端（TUN 服务端）
│   ├── main.go                  # 程序入口 + 服务管理
│   ├── service.go               # VPN 核心逻辑 + 配置热加载
│   ├── config.go                # 配置解析 + Docker 子网发现
│   ├── dashboard.go             # Web Dashboard + 反向代理
│   ├── dashboard_html.go        # 前端 UI（HTML/CSS/JS 内嵌）
│   ├── expose.go                # 容器导出 (accessor)
│   └── proxy.go                 # 本地服务代理
│
├── 📂 docker/                   # VM/容器端（TUN 客户端）
│   ├── main.go                  # 入口（container / service 两种模式）
│   ├── vm_http_server.go        # VM HTTP API + SSE 推送
│   ├── link_manager.go          # 链路接口 + 注册表
│   ├── link_*.go                # 5 条链路实现
│   ├── infra_*.go               # 基础设施层（iptables/route/dns/network）
│   └── dns_server.go            # 内嵌 DNS 服务
│
├── 📂 deploy/                   # 部署工具
│   ├── deploy-to-lima.sh        # Lima VM 一键部署
│   ├── deploy-desktop.sh        # Desktop 一键编译更新
│   ├── install.sh               # VM 内安装脚本
│   ├── docker-connector.service # systemd 服务文件
│   └── connector.env            # 服务环境配置
│
├── 📂 docs/                     # 设计文档
├── 📂 scripts/                  # Python 网络设置脚本（原始版本）
├── DEBUG_GUIDE.md               # 调试与故障排查
├── GUIDE.md                     # 👈 你正在读的这份文档
└── README.md                    # 项目 README
```

---

## 🤝 Windows 用户指南

### 安装步骤

1. **安装 TAP 驱动**（来自 OpenVPN）：
   - 下载 [tap-windows](http://build.openvpn.net/downloads/releases/latest/tap-windows-latest-stable.exe) 并安装

2. **下载 Desktop Connector**：
   - 从 [Releases](https://github.com/wenjunxiao/desktop-docker-connector/releases) 下载最新版本并解压

3. **配置路由**：
   - 编辑 `options.conf`，添加 Docker 子网路由：
     ```ini
     route 172.17.0.0/16
     route 172.20.0.0/16
     ```

4. **启动**：
   - 方式一：直接运行 `start-connector.bat`
   - 方式二：安装为 Windows 服务：
     ```cmd
     install-service.bat    :: 安装服务
     start-service.bat      :: 启动服务
     stop-service.bat       :: 停止服务
     uninstall-service.bat  :: 卸载服务
     ```

---

## 💡 进阶技巧

### 🔄 配置文件的热加载机制

Connector 会自动 watch 配置文件变化。大部分配置修改后**无需重启**：

| 配置项 | 热加载 | 说明 |
|--------|:------:|------|
| `route` | ✅ | 增删路由立即生效 |
| `iptables` | ✅ | 增删子网互通规则立即生效 |
| `expose` | ✅ | 导出配置立即生效 |
| `token` | ✅ | Token 变更立即生效 |
| `hosts` | ✅ | DNS 映射立即生效 |
| `proxy` | ✅ | 代理配置立即生效 |
| `addr` / `port` / `mtu` / `host` | ❌ | 需要重启服务 |

### 🏎 性能优化

Connector 内置了多项传输优化：

- **日志守卫**：高流量时跳过不必要的日志格式化，减少 75% CPU 开销
- **缓冲区池化**：`sync.Pool` 复用 64KB 缓冲区，GC 停顿降低 90%
- **智能心跳**：有数据流时自动跳过心跳包，节省带宽
- **控制包合并**：header + data 合并为单个 UDP 包，避免分片丢失

### 🔐 安全说明

- Desktop 端 UDP 默认绑定 `127.0.0.1`，仅本机可连接
- VM HTTP API 绑定 TUN peer IP，仅隧道内网可见
- `expose` 功能默认关闭，需要显式开启 + token 认证

---

## 📜 许可证

[MIT License](./LICENSE) — 自由使用，开心就好 🎉

---

> _"Docker 容器跑在虚拟机里，我却能像访问本地服务一样直接 `curl` 它的 IP——这就是 Desktop Docker Connector 带给我的魔法。"_ ✨
