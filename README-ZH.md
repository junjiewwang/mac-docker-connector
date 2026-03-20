[English](https://github.com/wenjunxiao/mac-docker-connector/blob/master/README.md) | [中文简体](https://github.com/wenjunxiao/mac-docker-connector/blob/master/README-ZH.md)

> 把`mac-docker-connector`重命名为`desktop-docker-connector`是为了同时支持Mac和Widnwos下的Docker
# desktop-docker-connector

  `Docker Desktop for Mac and Windows` 没有提供从宿主的macOS或Windows通过容器IP访问容器的方式。参考[Known limitations, use cases, and workarounds](https://docs.docker.com/docker-for-mac/networking/#i-cannot-ping-my-containers)。通过一个[复杂解决方法](https://pjw.io/articles/2018/04/25/access-to-the-container-network-of-docker-for-mac/)得到灵感，主要方式在宿主的macOS和Docker的Hypervisor之间建立一个VPN
```
+---------------+          +--------------------+
|               |          | Hypervisor/Hyper-V |
| macOS/Windows |          |  +-----------+     |
|               |          |  | Container |     |
|               |   vpn    |  +-----------+     |
|   VPN Client  |<-------->|   VPN Server       |
+---------------+          +--------------------+
```
  但是宿主的macOS无法直接访问Hypervisor，VPN服务容器需要使用`host`以便与Hypervisor在同一网络环境中，必须使用一个转发容器（比如`socat`)导出端口到macOS，然后转发到VPN服务。考虑到VPN连接的双工的，因此我们可以把VPN服务和客户端反转一下，变成下面的结构
```
+---------------+          +--------------------+
|               |          | Hypervisor/Hyper-V |
| macOS/Windows |          |  +-----------+     |
|               |          |  | Container |     |
|               |   vpn    |  +-----------+     |
| VPN Server    |<-------->|   VPN Client       |
+---------------+          +--------------------+
```
  尽管如此, 我们需要做更多额外的工作来使用openvpn，比如证书、配置等。
  这对于只是通过IP访问容器的需求来说，这些工作略显麻烦。
  我们只需要建立一个连接通道，无需证书，也可以无需客户端
```
+---------------+          +--------------------+
|               |          | Hypervisor/Hyper-V |
| macOS/Windows |          |  +-----------+     |
|               |          |  | Container |     |
|               |   udp    |  +-----------+     |
| TUN Server    |<-------->|   TUN Client       |
+---------------+          +--------------------+
```
  鉴于Docker官方文档[Docker and iptables](https://docs.docker.com/network/iptables/)中描述那样,
  两个子网之间的互通性有时也是需要的，因此还可以通过`iptables`来提供两个子网之间的互相连接
```
+-------------------------------+ 
|      Hypervisor/Hyper-V       | 
| +----------+     +----------+ | 
| | subnet 1 |<--->| subnet 2 | |
| +----------+     +----------+ |
+-------------------------------+
```

## 功能特性

| 功能 | 说明 | 版本 |
|------|------|------|
| **UDP TUN 隧道** | 通过 TUN 设备从 macOS/Windows 宿主机直接访问 Docker 容器 IP | v1.0 |
| **子网互通** | 通过 iptables 规则实现两个 Docker 子网之间的互相访问 | v1.0 |
| **容器导出** | 通过 accessor 将你的 Docker 容器导出给其他人访问 | v1.0 |
| **自定义 DNS 与代理** | 容器内使用自定义域名解析 + 本地服务代理 | v1.0 |
| **Web Dashboard** | 内置 Web 面板，提供路由校验、一键修复和状态可视化 | Phase 1 |
| **VM 链路管理** | 5 条网络链路（iptables/route/dns）通过 HTTP API 管理，Go 重写链路层 | Phase 2 |
| **网络拓扑图** | 交互式 SVG 拓扑图，实时展示 VM 所有链路状态 | Phase 2 |
| **Lima VM 部署** | 原生 Linux systemd 服务部署（替代 Docker 容器方式） | Phase 2 |

## 架构

```
+-------------------------------------------------------------------+
|                       macOS 宿主机                                 |
|                                                                   |
|  浏览器 ──HTTP──▶ desktop-docker-connector                        |
|                   ├─ UDP :2511  (TUN VPN 隧道)                    |
|                   ├─ HTTP :2511 (Web Dashboard)                   |
|                   │   ├─ GET  /api/status                         |
|                   │   ├─ GET  /api/routes/verify                  |
|                   │   ├─ POST /api/routes/fix                     |
|                   │   └─ /api/vm/* (反向代理到 VM)                 |
|                   └─ 配置: docker-connector.conf                   |
|                                                                   |
|                         ▲ UDP 隧道                                |
|                         │                                         |
+-------------------------│-----------------------------------------+
                          │
+-------------------------▼-----------------------------------------+
|                   Lima VM / Hypervisor                             |
|                                                                   |
|  docker-connector (systemd 服务, -mode=service)                   |
|  ├─ TUN Client (UDP 隧道连接 Desktop)                             |
|  ├─ HTTP :2522 (VM 链路 API, 绑定 peer IP)                       |
|  │   ├─ GET  /api/links          (所有链路状态)                    |
|  │   ├─ GET  /api/links/stream   (SSE 实时推送)                   |
|  │   ├─ POST /api/apply          (应用链路规则)                    |
|  │   ├─ POST /api/revert         (还原链路规则)                    |
|  │   └─ GET  /api/network/info   (网络信息)                       |
|  └─ LinkManager                                                   |
|      ├─ InternetLink        (Docker/K8s ↔ 外网)                  |
|      ├─ HostDockerLink      (宿主机 ↔ Docker)                    |
|      ├─ HostK8sLink         (宿主机 ↔ K8s, .service/.pod)        |
|      ├─ DockerK8sLink       (Docker ↔ K8s, .service/.pod)        |
|      └─ DockerDockerLink    (Docker ↔ Docker 跨子网)             |
|                                                                   |
|  Docker Daemon  |  Minikube / K8s                                 |
+-------------------------------------------------------------------+
```

## 使用

### 宿主机

#### Mac
  先安装Mac端的服务`mac-docker-connector`
```bash
$ brew tap wenjunxiao/brew
$ brew install docker-connector
```

  首次配置通过以下命令把所有Docker所有`bridge`子网放入配置文件，后续的增减可以参考后面的详细配置
```bash
$ docker network ls --filter driver=bridge --format "{{.ID}}" | xargs docker network inspect --format "route {{range .IPAM.Config}}{{.Subnet}}{{end}}" >> "$(brew --prefix)/etc/docker-connector.conf"
```

  启动Mac端的服务
```bash
$ sudo brew services start docker-connector
```

  安装Docker端的容器`mac-docker-connector`
```bash
$ docker pull wenjunxiao/mac-docker-connector
```

#### Windows

  从[Releases](https://github.com/wenjunxiao/desktop-docker-connector/releases)下载 `desktop-docker-connector`然后解压.

  执行以下命令安装服务，把所有需要访问的Bridge子网地址按照`route 172.17.0.0/16`的格式写入`options.conf`
```cmd
$ desktop-docker-connector.exe install -config options.conf
```

  把所有需要访问的Bridge子网地址按照`route 172.17.0.0/16`的格式写入`options.conf`
```conf
route 172.17.0.0/16
```
  可以通过脚本`start-connector.bat`来直接启动连接器，或者把连接器按照以下步骤安装成服务之后启动:
  1. 运行脚本`install-service.bat`安装服务.
  2. 运行脚本`start-service.bat`来启动服务.
  还可以通过运行脚本`stop-service.bat`停止服务以及运行脚本`uninstall-service.bat`卸载服务

### Docker

  启动Docker端的容器，其中网络必须是`host`，并且添加`NET_ADMIN`特性
```bash
$ docker run -it -d --restart always --net host --cap-add NET_ADMIN --name mac-connector wenjunxiao/mac-docker-connector
```

  如果你向导出你自己的容器给其他人，让其他人可以访问你在容器中搭建的服务，其他人必须安装另一个客户端[docker-accessor](./accessor)，同时你必须开启`expose`（这默认是关闭的）和提供访问的令牌(`token`)，
  更详细的配置说明参考配置说明

### Lima VM（服务模式）

  对于 Lima VM 用户，connector 可以部署为原生 Linux systemd 服务，能够完整使用系统命令（`docker`、`kubectl`、`iptables`、`systemd-resolved` 等），并启用 VM 链路管理功能。

  **使用安装脚本快速部署：**
```bash
# 将二进制文件和部署脚本复制到 Lima VM 中
$ limactl shell default -- bash -s < deploy/install.sh
```

  **或手动部署：**
```bash
# 1. 编译 VM 端 connector 二进制
$ cd docker && GOOS=linux GOARCH=amd64 go build -o docker-connector .

# 2. 复制二进制文件到 VM
$ limactl copy docker-connector default:/usr/local/bin/

# 3. 复制并配置 systemd 服务文件
$ limactl copy deploy/docker-connector.service default:/etc/systemd/system/
$ limactl copy deploy/connector.env default:/etc/docker-connector/connector.env

# 4. 启动服务
$ limactl shell default -- sudo systemctl daemon-reload
$ limactl shell default -- sudo systemctl enable --now docker-connector
```

  查看服务状态：
```bash
$ limactl shell default -- sudo systemctl status docker-connector
$ limactl shell default -- sudo journalctl -u docker-connector -f
```

## Web Dashboard

  内置的 Web Dashboard 可通过 `http://localhost:2511` 访问（与 UDP 隧道共用端口）。

  **功能：**
  - **路由校验** — 交叉对比 `docker-connector.conf` 路由配置与 macOS 系统路由表（`netstat -rn`）
  - **一键修复** — 对 MISSING 路由自动执行 `route -n add`
  - **连接状态** — 运行时间、客户端连接状态、TUN 接口信息、Peer IP
  - **VM 链路面板** — 实时显示 VM 中 5 条网络链路的状态（需要服务模式）
  - **网络拓扑图** — 交互式 SVG 拓扑图，通过颜色标识链路状态
  - **自动刷新** — 每 5 秒轮询，使用差量 DOM 更新（无闪烁）

## 配置说明

  基本的配置选项，通常你不需要修改他们，除非你的环境冲突（比如端口被占用，子网已使用）。
  一旦需要变更，那么Docker容器`mac-docker-connector`也需要使用相同的参数重新启动
* `addr` 虚拟网络地址, 默认 `192.168.251.1/24`（可以修改，但容器端需要同步修改参数）
  ```
  addr 192.168.251.1/24
  ```
* `port` UDP服务监听端口, 默认`2511`（可以修改，但容器端需要同步修改参数）
  ```
  port 2511
  ```
* `mtu` 网络的MTU值，默认`1400`（可以修改，但容器端需要同步修改参数）
  ```
  mtu 1400
  ```
* `host` UDP监听的地址，仅用于Docker容器`mac-docker-connector`连接使用，处于安全和适应移动办公设置成`127.0.0.1`（通常无需修改）
  ```
  host 127.0.0.1
  ```

  动态热加载的配置选项，修改配置文件之后无需启动，立即生效（除非禁用`watch`）,可以在需要的时候随时增减
* `route` 添加一条访问Docker容器子网的的路由，通常在你通过`docker network create --subnet x.x.x.x/mask name`命令创建一个`bridge`子网时需要添加
  ```
  route 172.100.0.0/16
  ```
* `iptables` 插入(`+`)或删除(`-`)一条`iptables`规则，用于两个子网之间互相访问
  ```
  iptables 172.0.1.0+172.0.2.0
  iptables 172.0.3.0-172.0.4.0
  ```
  IP是无掩码子网的地址，通过`+`连接表示插入一条可以互相访问的规则，通过`-`连接表示删除它们之间互相访问的规则
* `expose` 导出你本地的容器给其他人，指定其他人用于连接的开放端口
  ```
  expose 0.0.0.0:2512
  ```
  导出的地址必须是其他人可以通过[docker-accessor](./accessor)访问的地址
* `token` 定义其他人访问你的服务的令牌，以及连接成功之后分配的虚拟网络IP
  ```
  token token-name 192.168.251.3
  ```
  令牌是自定义的字符串，并且在配置文件中唯一，IP则必须是`addr`配置的虚拟网络中有效的IP
* `hosts` 让本地自定义`127.0.0.1`对应的域名也可以在容器中使用
  ```
  hosts /etc/hosts .local .inc
  ```
  第一个参数是hosts文件，后续的参数是过滤的域名后缀
* `proxy` 让本地监听`127.0.0.1`的服务也可以被容器访问
  ```
  proxy 127.0.0.1:80:80
  ```
  第一部分`127.0.0.1:80`是本地服务监听的地址，后面部分的端口`80`是代理监听的端口

## VM 链路管理

  在 **服务模式**（`-mode=service`）下运行时，VM 端 connector 提供 5 条网络链路管理器，用于配置 iptables、路由和 DNS 规则，实现完整的网络连通性。

| 链路 | 子层级 | 说明 | 底层操作 |
|------|--------|------|---------|
| **internet** | — | Docker/K8s ↔ 外网 | FORWARD 网桥↔物理网卡 + NAT MASQUERADE |
| **host-docker** | — | 宿主机 (tun0) ↔ Docker 容器 | FORWARD tun0↔非 Minikube 网桥 |
| **host-k8s** | `.service` `.pod` | 宿主机 (tun0) ↔ Kubernetes | route + FORWARD tun0↔mk 网桥 + DNS |
| **docker-k8s** | `.service` `.pod` | Docker ↔ Kubernetes | FORWARD 非 mk 网桥↔mk 网桥 |
| **docker-docker** | — | Docker 跨子网通信 | FORWARD 网桥↔网桥 |

  通过 Dashboard 的 **VM 链路面板** 或直接调用 HTTP API 操作：
```bash
# 获取所有链路状态
$ curl http://localhost:2511/api/vm/links

# 应用所有链路
$ curl -X POST http://localhost:2511/api/vm/apply

# 应用指定链路
$ curl -X POST http://localhost:2511/api/vm/apply -d '{"links":["host-docker"]}'

# 还原指定链路
$ curl -X POST http://localhost:2511/api/vm/revert -d '{"links":["host-k8s.service"]}'
```

## 项目结构

```
├── desktop/                  # macOS/Windows 宿主机 connector（TUN 服务端）
│   ├── main.go               # 入口 + 服务管理
│   ├── service.go            # VPN 服务逻辑 + 配置热加载
│   ├── config.go             # 配置文件解析
│   ├── dashboard.go          # Web Dashboard HTTP 服务 + API
│   ├── dashboard_html.go     # 内嵌前端（HTML/CSS/JS）
│   ├── expose.go             # 容器导出（accessor 支持）
│   └── proxy.go              # 本地服务代理
├── docker/                   # VM/容器端 connector（TUN 客户端）
│   ├── main.go               # 入口（容器/服务 两种模式）
│   ├── vm_http_server.go     # VM HTTP API + SSE 流式推送
│   ├── link_manager.go       # Link 接口 + 注册表
│   ├── link_internet.go      # InternetLink
│   ├── link_host_docker.go   # HostDockerLink
│   ├── link_host_k8s.go      # HostK8sLink（.service/.pod）
│   ├── link_docker_k8s.go    # DockerK8sLink（.service/.pod）
│   ├── link_docker_docker.go # DockerDockerLink
│   ├── infra_iptables.go     # IptablesManager
│   ├── infra_network.go      # NetworkInfoProvider（docker CLI）
│   ├── infra_route.go        # RouteManager（ip route）
│   ├── infra_dns.go          # DnsManager（systemd-resolved）
│   ├── infra_command.go      # 命令执行封装
│   └── dns_server.go         # 内嵌 DNS 服务
├── deploy/                   # 部署脚本
│   ├── install.sh            # Lima VM 一键安装
│   ├── deploy-to-lima.sh     # Lima 专用部署脚本
│   ├── deploy-desktop.sh     # Desktop 部署脚本
│   ├── docker-connector.service  # systemd 服务单元文件
│   └── connector.env         # 服务环境配置
├── scripts/                  # Python 网络设置脚本
│   └── setup-docker-network.py   # 原始 Python 链路管理器
├── docs/                     # 设计文档
│   ├── phase2-vm-link-management.md  # Phase 2 完整设计文档
│   ├── web-dashboard.md      # Web Dashboard 设计文档
│   ├── zone-link-refactor.md # Zone/Link 模型设计
│   └── pod-network-support.md
├── DEBUG_GUIDE.md            # 调试与故障排查指南
└── accessor/                 # Docker accessor（容器导出客户端）
```

## 调试

  参考 [DEBUG_GUIDE.md](./DEBUG_GUIDE.md) 获取详细的调试说明，包括：
  - 日志级别设置（`-log-level DEBUG`）
  - 数据包跟踪（`TUN->UDP` / `UDP->TUN`）
  - 客户端连接诊断
  - 常见问题排查

## 许可证

  [MIT](./LICENSE)