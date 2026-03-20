[English](https://github.com/wenjunxiao/mac-docker-connector/blob/master/README.md) | [中文简体](https://github.com/wenjunxiao/mac-docker-connector/blob/master/README-ZH.md)

> Change mac-docker-connector to desktop-docker-connector to support both Docker Desktop for Mac and Docker Desktop for Windows
# desktop-docker-connector

  `Docker Desktop for Mac and Windows` does not provide access to container IP from host(macOS or Windows). 
  Reference [Known limitations, use cases, and workarounds](https://docs.docker.com/docker-for-mac/networking/#i-cannot-ping-my-containers). 
  There is a [complex solution](https://pjw.io/articles/2018/04/25/access-to-the-container-network-of-docker-for-mac/),
  which is also my source of inspiration. The main idea is to build a VPN between the macOS/Windows host and the docker virtual machine.
```
+---------------+          +--------------------+
|               |          | Hypervisor/Hyper-V |
| macOS/Windows |          |  +-----------+     |
|               |          |  | Container |     |
|               |   vpn    |  +-----------+     |
|   VPN Client  |<-------->|   VPN Server       |
+---------------+          +--------------------+
```
  But the macOS/Windows host cannot access the container, the vpn port must be exported and forwarded.
  Since the VPN connection is duplex, so we can reverse it.
```
+---------------+          +--------------------+
|               |          | Hypervisor/Hyper-V |
| macOS/Windows |          |  +-----------+     |
|               |          |  | Container |     |
|               |   vpn    |  +-----------+     |
| VPN Server    |<-------->|   VPN Client       |
+---------------+          +--------------------+
```
  Even so, we need to do more extra work to use openvpn, such as certificates, configuration, etc.
  All I want is to access the container via IP, why is it so cumbersome. 
  No need for security, multi-clients, or certificates, just connect.
```
+---------------+          +--------------------+
|               |          | Hypervisor/Hyper-V |
| macOS/Windows |          |  +-----------+     |
|               |          |  | Container |     |
|               |   udp    |  +-----------+     |
| TUN Server    |<-------->|   TUN Client       |
+---------------+          +--------------------+
```
  In the view of [Docker and iptables](https://docs.docker.com/network/iptables/), 
  this tool also provides the ability of two subnets to access each other.
```
+-------------------------------+ 
|      Hypervisor/Hyper-V       | 
| +----------+     +----------+ | 
| | subnet 1 |<--->| subnet 2 | |
| +----------+     +----------+ |
+-------------------------------+
```

## Features

| Feature | Description | Since |
|---------|-------------|-------|
| **UDP TUN Tunnel** | Access Docker container IPs from macOS/Windows host via TUN device | v1.0 |
| **Cross-subnet Access** | Allow two Docker subnets to access each other via iptables rules | v1.0 |
| **Container Expose** | Expose your Docker containers to other people via accessor | v1.0 |
| **Custom DNS & Proxy** | Custom host DNS resolution + local service proxy for containers | v1.0 |
| **Web Dashboard** | Built-in web UI for route verification, one-click fix, and status visualization | Phase 1 |
| **VM Link Management** | 5 network links (iptables/route/dns) managed via HTTP API with Go-rewritten link layer | Phase 2 |
| **Network Topology** | Interactive SVG topology diagram showing all VM link states in real-time | Phase 2 |
| **Lima VM Deployment** | Native Linux systemd service deployment for Lima VM (replacing Docker container) | Phase 2 |

## Architecture

```
+-------------------------------------------------------------------+
|                       macOS Host                                  |
|                                                                   |
|  Browser ──HTTP──▶ desktop-docker-connector                       |
|                    ├─ UDP :2511  (TUN VPN Tunnel)                 |
|                    ├─ HTTP :2511 (Web Dashboard)                  |
|                    │   ├─ GET  /api/status                        |
|                    │   ├─ GET  /api/routes/verify                 |
|                    │   ├─ POST /api/routes/fix                    |
|                    │   └─ /api/vm/* (reverse proxy to VM)         |
|                    └─ config: docker-connector.conf               |
|                                                                   |
|                         ▲ UDP Tunnel                              |
|                         │                                         |
+-------------------------│-----------------------------------------+
                          │
+-------------------------▼-----------------------------------------+
|                   Lima VM / Hypervisor                             |
|                                                                   |
|  docker-connector (systemd service, -mode=service)                |
|  ├─ TUN Client (UDP tunnel to Desktop)                            |
|  ├─ HTTP :2522 (VM Link API, bound to peer IP)                   |
|  │   ├─ GET  /api/links          (all link status)               |
|  │   ├─ GET  /api/links/stream   (SSE real-time)                 |
|  │   ├─ POST /api/apply          (apply link rules)              |
|  │   ├─ POST /api/revert         (revert link rules)             |
|  │   └─ GET  /api/network/info   (network info)                  |
|  └─ LinkManager                                                   |
|      ├─ InternetLink        (Docker/K8s ↔ Internet)              |
|      ├─ HostDockerLink      (Host ↔ Docker)                      |
|      ├─ HostK8sLink         (Host ↔ K8s, .service/.pod)          |
|      ├─ DockerK8sLink       (Docker ↔ K8s, .service/.pod)        |
|      └─ DockerDockerLink    (Docker ↔ Docker cross-subnet)       |
|                                                                   |
|  Docker Daemon  |  Minikube / K8s                                 |
+-------------------------------------------------------------------+
```

## Usage

### Host
#### MacOS

  Install mac client of `desktop-docker-connector`.
```bash
$ brew tap wenjunxiao/brew
$ brew install docker-connector
```

  Config route of docker network
```bash
$ docker network ls --filter driver=bridge --format "{{.ID}}" | xargs docker network inspect --format "route {{range .IPAM.Config}}{{.Subnet}}{{end}}" >> "$(brew --prefix)/etc/docker-connector.conf"
```

  Start the service
```bash
$ sudo brew services start docker-connector
```

#### Windows

  Need to install tap driver [tap-windows](http://build.openvpn.net/downloads/releases/) from [OpenVPN](https://community.openvpn.net/openvpn/wiki/ManagingWindowsTAPDrivers).
  Download the latest version `http://build.openvpn.net/downloads/releases/latest/tap-windows-latest-stable.exe` and install.
  
  Download windows client of `desktop-docker-connector` from [Releases](https://github.com/wenjunxiao/desktop-docker-connector/releases), and then unzip it.

  Append bridge network to `options.conf`, format like `route 172.17.0.0/16`.
```conf
route 172.17.0.0/16
```
  Run directly by bat `start-connector.bat` or install as service by follow step:
  1. Run the bat `install-service.bat` to install as windows service.
  2. Run the bat `start-service.bat` to start the connector service.
  And finally, you can  run the bat `stop-service.bat` to stop the connector service, 
  run the bat `uninstall-service.bat` to uninstall the connector service.
  
### Docker

  Install docker front of `desktop-docker-connector`
```bash
$ docker pull wenjunxiao/desktop-docker-connector
```

  Start the docker front. The network must be `host`, and add `NET_ADMIN` capability.

```bash
$ docker run -it -d --restart always --net host --cap-add NET_ADMIN --name desktop-connector wenjunxiao/desktop-docker-connector
```

  If you want to expose the containers of docker to other pepole, Please reference [docker-accessor](./accessor)

### Lima VM (Service Mode)

  For Lima VM users, the connector can be deployed as a native Linux systemd service, which provides full access to system commands (`docker`, `kubectl`, `iptables`, `systemd-resolved`, etc.) and enables the VM Link Management features.

  **Quick deploy with the install script:**
```bash
# Copy the binary and deploy script into Lima VM
$ limactl shell default -- bash -s < deploy/install.sh
```

  **Or manual deployment:**
```bash
# 1. Build the VM connector binary
$ cd docker && GOOS=linux GOARCH=amd64 go build -o docker-connector .

# 2. Copy binary to VM
$ limactl copy docker-connector default:/usr/local/bin/

# 3. Copy and configure systemd service
$ limactl copy deploy/docker-connector.service default:/etc/systemd/system/
$ limactl copy deploy/connector.env default:/etc/docker-connector/connector.env

# 4. Start the service
$ limactl shell default -- sudo systemctl daemon-reload
$ limactl shell default -- sudo systemctl enable --now docker-connector
```

  Check the service status:
```bash
$ limactl shell default -- sudo systemctl status docker-connector
$ limactl shell default -- sudo journalctl -u docker-connector -f
```

## Web Dashboard

  The built-in Web Dashboard is available at `http://localhost:2511` (same port as the UDP tunnel).

  **Features:**
  - **Route Verification** — Cross-check `docker-connector.conf` routes against macOS system routing table (`netstat -rn`)
  - **One-click Fix** — Automatically add missing routes with `route -n add`
  - **Connector Status** — Uptime, client connection, TUN interface, peer IP
  - **VM Link Panel** — Real-time status of all 5 network links in the VM (requires service mode)
  - **Network Topology** — Interactive SVG diagram showing link states with color coding
  - **Auto Refresh** — Dashboard polls every 5 seconds with diff-based DOM updates (no flicker)

## Configuration

  Basic configuration items, do not need to modify these, unless your environment conflicts,
  if necessary, then the docker container `desktop-docker-connector` also needs to be started with the same parameters
* `addr` virtual network address, default `192.168.251.1/24` (change if it conflict)
  ```
  addr 192.168.251.1/24
  ```
* `port` udp listen port, default `2511` (change if it conflict)
  ```
  port 2511
  ```
* `mtu` the MTU of network, default `1400`
  ```
  mtu 1400
  ```
* `host` udp listen host, used to be connected by `desktop-docker-connector`, default `127.0.0.1` for security and adaptation
  ```
  host 127.0.0.1
  ```

  Dynamic hot-loading configuration items can take effect without restarting,
  and need to be added or modified according to your needs.
* `route` Add a route to access the docker container subnet, usually when you create a bridge network by `docker network create --subnet 172.56.72.0/24 app`, run `echo "route 172.56.72.0/24" >> "$(brew --prefix)/etc/docker-connector.conf"` to append route to config file.
  ```
  route 172.56.72.0/24
  ```
* `iptables` Insert(`+`) or delete(`-`) a iptable rule for two subnets to access each other.
  ```
  iptables 172.0.1.0+172.0.2.0
  iptables 172.0.3.0-172.0.4.0
  ```
  The ip is subnet address without mask, and join with `+` to insert a rule, and join with `-` to delete a rule.
* `expose` Expose you docker container to other pepole, default disabled.
  ```
  expose 0.0.0.0:2512
  ```
  the exposed address should be connected by [docker-accessor](./accessor).
  And then add `expose` after then `route` you want to be exposed
  ```
  route 172.100.0.0/16 expose
  ```
* `token` Define the access token and the virtual IP assigned after connection
  ```
  token token-name 192.168.251.3
  ```
  The token name is customized and unique, and the IP must be valid in the virtual network
  defined by `addr`  
* `hosts` allows the custom domain with ip `127.0.0.1`, also can be used in the container
   ````
   hosts /etc/hosts .local .inc
   ````
   The first parameter is the hosts file, and the subsequent parameters are the filtered domain name suffix
* `proxy` allows services that listen locally on `127.0.0.1` to be accessed by the container
   ````
   proxy 127.0.0.1:80:80
   ````
   The first part `127.0.0.1:80` is the address where the local service listens, and the port `80` in the latter part is the port where the proxy listens

## VM Link Management

  When running in **service mode** (`-mode=service`), the VM connector provides 5 network link managers to configure iptables, routes, and DNS rules for full network connectivity.

| Link | Sub-levels | Description | Operations |
|------|-----------|-------------|------------|
| **internet** | — | Docker/K8s ↔ Internet | FORWARD bridge↔physical NIC + NAT MASQUERADE |
| **host-docker** | — | Host (tun0) ↔ Docker containers | FORWARD tun0↔non-minikube bridges |
| **host-k8s** | `.service` `.pod` | Host (tun0) ↔ Kubernetes | route + FORWARD tun0↔mk bridge + DNS |
| **docker-k8s** | `.service` `.pod` | Docker ↔ Kubernetes | FORWARD non-mk bridge↔mk bridge |
| **docker-docker** | — | Docker cross-subnet communication | FORWARD bridge↔bridge |

  Use the Dashboard's **VM Link Panel** or call the HTTP API directly:
```bash
# Get all link status
$ curl http://localhost:2511/api/vm/links

# Apply all links
$ curl -X POST http://localhost:2511/api/vm/apply

# Apply specific link
$ curl -X POST http://localhost:2511/api/vm/apply -d '{"links":["host-docker"]}'

# Revert specific link
$ curl -X POST http://localhost:2511/api/vm/revert -d '{"links":["host-k8s.service"]}'
```

## Project Structure

```
├── desktop/                  # macOS/Windows host connector (TUN server)
│   ├── main.go               # Entry point + service management
│   ├── service.go            # VPN service logic + config hot-reload
│   ├── config.go             # Configuration parsing
│   ├── dashboard.go          # Web Dashboard HTTP server + API
│   ├── dashboard_html.go     # Embedded frontend (HTML/CSS/JS)
│   ├── expose.go             # Container expose (accessor support)
│   └── proxy.go              # Local service proxy
├── docker/                   # VM/container connector (TUN client)
│   ├── main.go               # Entry point (container/service mode)
│   ├── vm_http_server.go     # VM HTTP API + SSE streaming
│   ├── link_manager.go       # Link interface + registry
│   ├── link_internet.go      # InternetLink
│   ├── link_host_docker.go   # HostDockerLink
│   ├── link_host_k8s.go      # HostK8sLink (.service/.pod)
│   ├── link_docker_k8s.go    # DockerK8sLink (.service/.pod)
│   ├── link_docker_docker.go # DockerDockerLink
│   ├── infra_iptables.go     # IptablesManager
│   ├── infra_network.go      # NetworkInfoProvider (docker CLI)
│   ├── infra_route.go        # RouteManager (ip route)
│   ├── infra_dns.go          # DnsManager (systemd-resolved)
│   ├── infra_command.go      # Command executor
│   └── dns_server.go         # Embedded DNS server
├── deploy/                   # Deployment scripts
│   ├── install.sh            # Lima VM one-click install
│   ├── deploy-to-lima.sh     # Lima-specific deploy script
│   ├── deploy-desktop.sh     # Desktop deploy script
│   ├── docker-connector.service  # systemd unit file
│   └── connector.env         # Service environment config
├── scripts/                  # Python network setup scripts
│   └── setup-docker-network.py   # Original Python link manager
├── docs/                     # Design documents
│   ├── phase2-vm-link-management.md  # Phase 2 full design doc
│   ├── web-dashboard.md      # Web Dashboard design doc
│   ├── zone-link-refactor.md # Zone/Link model design
│   └── pod-network-support.md
├── DEBUG_GUIDE.md            # Debug & troubleshooting guide
└── accessor/                 # Docker accessor (for exposing containers)
```

## Debugging

  See [DEBUG_GUIDE.md](./DEBUG_GUIDE.md) for detailed debugging instructions, including:
  - Log level configuration (`-log-level DEBUG`)
  - Packet tracing (`TUN->UDP` / `UDP->TUN`)
  - Client connection diagnostics
  - Common issue troubleshooting

## License

  [MIT](./LICENSE)