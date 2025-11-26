# 🚀 Docker & Kubernetes 网络配置指南

> **让 Mac/Windows 直接访问容器 IP，就像它们在本地一样！**  
> 一键配置 + 零学习成本 = 开发效率翻倍 ⚡️

---

## ⚡️ 三秒上手

```bash
# 就这么简单！
sudo ./setup-docker-network.sh
```

**脚本会自动完成所有配置**，包括：

- ✅ Docker iptables 配置检查与修复
- ✅ Hostname 配置到 /etc/hosts（幂等操作）
- ✅ Docker 容器访问外网
- ✅ 宿主机直接访问容器 IP
- ✅ Kubernetes Pod/Service 直接访问
- ✅ DNS 解析（支持 `.cluster.local` 域名，自动配置 systemd-resolved）
- ✅ DNS 解析验证（nslookup kubernetes.default.svc.cluster.local）

> 💡 **提示**：首次运行需要 5-10 秒，之后重启也能自动恢复配置。所有配置都支持幂等操作，重复运行不会产生副作用

---

## 🎯 这能解决什么问题？

### 痛点场景

你是否遇到过这些烦恼？

```bash
# ❌ 以前：必须用端口映射
docker run -p 8080:80 nginx
curl localhost:8080

# ✅ 现在：直接访问容器 IP
docker run nginx
curl http://172.17.0.2  # 直接访问！
```

**更多场景**：

- 🔧 **微服务调试**：不用记一堆端口号，直接用容器 IP
- 🧪 **集成测试**：测试脚本可以直接连接容器，无需端口转发
- ☸️ **Kubernetes 开发**：在 Mac 上直接访问 Pod IP 和 Service IP
- 🌐 **DNS 解析**：`curl http://my-service.default.svc.cluster.local` 直接生效

---

## 📊 工作原理（一图胜千言）

```
┌─────────────────────────────────────────────────────────┐
│                    Mac/Windows 宿主机                    │
│                                                          │
│  💻 浏览器/IDE ──▶ 🔌 TUN0 虚拟网卡 (docker-connector) │
└────────────────────────────┬────────────────────────────┘
                             │ UDP 隧道
                             ▼
┌─────────────────────────────────────────────────────────┐
│              Linux VM (Lima/WSL2/Docker Desktop)        │
│                                                          │
│  🐳 Docker 容器 ◀──▶ 🌉 网桥 ◀──▶ 🌍 外网              │
│  ☸️  K8s Pod    ◀──▶ 🌉 网桥 ◀──▶ 🌍 外网              │
└─────────────────────────────────────────────────────────┘
```

**核心技术**：

- **TUN 虚拟网卡**：在宿主机和虚拟机之间建立隧道
- **iptables 转发**：智能路由数据包
- **静态路由**：让宿主机知道如何访问容器网络

---

## 📖 使用指南

### 方式一：自动配置（推荐 ⭐️）

```bash
# 1. 下载脚本
chmod +x setup-docker-network.sh

# 2. 运行（需要 sudo）
sudo ./setup-docker-network.sh

# 3. 验证
docker run -d nginx
docker inspect <container_id> | grep IPAddress
curl http://<container_ip>  # 成功！🎉
```

**高级选项**：

```bash
# 查看详细配置信息
sudo ./setup-docker-network.sh -v

# 查看帮助
sudo ./setup-docker-network.sh --help
```

---

### 方式二：手动配置（理解原理）

<details>
<summary>📚 <b>点击展开：手动配置步骤</b></summary>

#### 步骤 0：配置 Docker iptables（⚠️ 必须）

```bash
# 1. 编辑 Docker 配置
sudo tee /etc/docker/daemon.json << EOF
{
  "iptables": false
}
EOF

# 2. 重启 Docker
sudo systemctl restart docker
```

> **为什么？** Docker 默认会自动管理 iptables，这会与我们的配置冲突。

#### 步骤 1：配置容器访问外网

```bash
# 启用 IP 转发
sudo sysctl -w net.ipv4.ip_forward=1

# 添加 NAT 规则（替换 eth0 为你的网卡）
sudo iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE
```

#### 步骤 2：配置宿主机访问容器

```bash
# 获取网桥名称
BRIDGE=$(docker network inspect bridge --format='{{index .Options "com.docker.network.bridge.name"}}')

# 配置转发规则
sudo iptables -I FORWARD 1 -i tun0 -o $BRIDGE -j ACCEPT
sudo iptables -I FORWARD 2 -i $BRIDGE -o tun0 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
sudo iptables -I DOCKER-ISOLATION-STAGE-2 1 -i tun0 -o $BRIDGE -j RETURN
```

#### 步骤 3：配置 Kubernetes（可选）

```bash
# 启动 Minikube
minikube start --cpus=6 --memory=10240 --service-cluster-ip-range='10.96.0.0/16'

# 添加路由
MINIKUBE_IP=$(minikube ip)
sudo ip route add 10.96.0.0/16 via $MINIKUBE_IP
```

</details>

---

## ☸️ Kubernetes 配置

### 快速启动 Minikube

```bash
# 推荐配置
minikube start \
  --cpus=6 \
  --memory=10240 \
  --disk-size=100g \
  --service-cluster-ip-range='10.96.0.0/16'

# 运行配置脚本
sudo ./setup-docker-network.sh

# 验证
kubectl get svc
curl http://<service_ip>  # 直接访问 Service！
```

### DNS 配置（访问 .cluster.local 域名）

<details>
<summary>📚 <b>点击展开：DNS 配置步骤</b></summary>

#### 在 Lima/WSL2 虚拟机内

```bash
# 1. 获取 DNS 服务 IP
DNS_IP=$(kubectl get svc -n kube-system kube-dns -o jsonpath='{.spec.clusterIP}')

# 2. 配置 systemd-resolved
sudo mkdir -p /etc/systemd/resolved.conf.d
sudo tee /etc/systemd/resolved.conf.d/minikube-dns.conf << EOF
[Resolve]
DNS=$DNS_IP
Domains=cluster.local
EOF

# 3. 重启服务
sudo systemctl restart systemd-resolved

# 4. 验证
nslookup kubernetes.default.svc.cluster.local
```

#### 在 Mac 宿主机

```bash
# 1. 创建 resolver 配置
sudo mkdir -p /etc/resolver
DNS_IP=$(kubectl get svc -n kube-system kube-dns -o jsonpath='{.spec.clusterIP}')

sudo tee /etc/resolver/cluster.local << EOF
nameserver $DNS_IP
port 53
EOF

# 2. 验证
ping kubernetes.default.svc.cluster.local
```

</details>

---

## 🐛 故障排查

### 快速诊断

```bash
# 1. 检查 Docker iptables 配置
grep '"iptables"' /etc/docker/daemon.json
# 应该显示: "iptables": false

# 2. 检查 TUN 设备
ip addr show tun0

# 3. 检查路由
ip route

# 4. 检查 iptables 规则
sudo iptables -L FORWARD -n -v

# 5. 抓包分析
sudo tcpdump -i tun0 -nn host <container_ip>
```

### 常见问题速查

| 问题                | 可能原因                 | 解决方案                                |
|-------------------|----------------------|-------------------------------------|
| 🚫 无法访问容器         | Docker iptables 配置错误 | 设置 `"iptables": false` 并重启 Docker   |
| 🚫 容器无法访问外网       | 缺少 NAT 规则            | 运行 `sudo ./setup-docker-network.sh` |
| 🚫 DNS 解析失败       | DNS 配置未生效            | 重启 `systemd-resolved` 服务            |
| 🚫 Minikube 重启后失效 | IP 地址变化              | 重新运行配置脚本                            |
| 同子网容器之间网络不通       | 防火墙阻拦                | 关闭防火墙                               |

<details>
<summary>📚 <b>点击展开：详细故障排查指南</b></summary>

### 问题 1：Docker iptables 配置错误

**现象**：脚本提示 `iptables = true`

**解决**：

```bash
# 备份配置
sudo cp /etc/docker/daemon.json /etc/docker/daemon.json.backup

# 修改配置
sudo tee /etc/docker/daemon.json << EOF
{
  "iptables": false
}
EOF

# 重启 Docker
sudo systemctl restart docker
```

### 问题 2：无法访问容器但容器能访问外网

**原因**：单向路由问题

**解决**：

```bash
# 检查回包规则
sudo iptables -L FORWARD -n -v | grep ESTABLISHED

# 禁用反向路径过滤
sudo sysctl -w net.ipv4.conf.all.rp_filter=0
sudo sysctl -w net.ipv4.conf.tun0.rp_filter=0
```

### 问题 3：网段冲突

**解决**：

```bash
# 删除 Minikube
minikube delete

# 使用不冲突的网段
minikube start --service-cluster-ip-range='10.96.0.0/16'
```

### 问题 4：DNS 配置不生效

**解决**：

```bash
# 检查服务状态
systemctl status systemd-resolved

# 查看 DNS 配置
resolvectl status

# 重启服务
sudo systemctl restart systemd-resolved

# 清除 DNS 缓存（Mac）
sudo killall -HUP mDNSResponder
```

</details>

---

## 💡 最佳实践

### ✅ 推荐做法

1. **使用自动化脚本**
   ```bash
   # 定期运行检查配置
   sudo ./setup-docker-network.sh
   ```

2. **选择不冲突的网段**
   ```bash
   # 推荐使用这些网段
   --service-cluster-ip-range='10.96.0.0/16'
   --service-cluster-ip-range='172.20.0.0/16'
   ```

3. **备份配置**
   ```bash
   # 备份 iptables 规则
   sudo iptables-save > ~/iptables-backup-$(date +%Y%m%d).rules
   
   # 备份路由表
   ip route > ~/route-backup-$(date +%Y%m%d).txt
   ```

4. **监控资源**
   ```bash
   # 定期检查 Minikube 资源
   kubectl top nodes
   kubectl top pods --all-namespaces
   ```

### ❌ 避免的做法

- ❌ 不要在生产环境禁用 Docker iptables 管理
- ❌ 不要使用常见的私有网段（如 192.168.0.0/16）
- ❌ 不要忘记备份配置
- ❌ 不要在不理解的情况下清空 iptables 规则

---

## 🎓 进阶技巧

<details>
<summary>📚 <b>点击展开：高级配置</b></summary>

### 自定义脚本

```bash
# 编辑脚本
vim setup-docker-network.sh

# 可自定义的变量
PHYSICAL_IF="eth0"        # 物理网卡名称
LOG_LEVEL="INFO"          # 日志级别
SKIP_TUN0=false          # 跳过 tun0 配置
SKIP_MINIKUBE=false      # 跳过 Minikube 配置
```

### 网络拓扑可视化

```bash
# 生成网络拓扑图
sudo ./setup-docker-network.sh -v

# 查看详细的 iptables 规则
sudo iptables -L -n -v --line-numbers

# 查看路由表
ip route show table all
```

</details>

---

## 📚 参考资料

### 官方文档

- [Docker 网络概述](https://docs.docker.com/network/)
- [Kubernetes 网络模型](https://kubernetes.io/docs/concepts/cluster-administration/networking/)
- [Minikube 网络配置](https://minikube.sigs.k8s.io/docs/handbook/accessing/)

### 相关项目

- [docker-connector 主项目](https://github.com/wenjunxiao/mac-docker-connector)
- [Lima 文档](https://github.com/lima-vm/lima)

### 工具文档

- [iptables 教程](https://www.netfilter.org/documentation/HOWTO/packet-filtering-HOWTO.html)
- [tcpdump 使用指南](https://www.tcpdump.org/manpages/tcpdump.1.html)

---

## 🤝 贡献

发现问题或有改进建议？欢迎：

- 🐛 [提交 Issue](https://github.com/wenjunxiao/mac-docker-connector/issues)
- 🔧 [提交 Pull Request](https://github.com/wenjunxiao/mac-docker-connector/pulls)
- 💬 分享你的使用经验

---

## 📝 快速命令参考

```bash
# === 脚本相关 ===
sudo ./setup-docker-network.sh          # 运行配置
sudo ./setup-docker-network.sh -v       # 详细模式

# === Docker 相关 ===
docker network ls                        # 查看网络
docker inspect <container> | grep IPAddress  # 获取容器 IP

# === Kubernetes 相关 ===
kubectl get pods -o wide                 # 查看 Pod IP
kubectl get svc                          # 查看 Service
minikube ip                              # 查看 Minikube IP

# === 网络诊断 ===
ip route                                 # 查看路由
sudo iptables -L FORWARD -n -v          # 查看转发规则
sudo tcpdump -i tun0 -nn                # 抓包分析
```

---

## ⚡️ 快速检查清单

配置完成后，确认以下项目：

- [ ] ✅ Docker 配置 `"iptables": false`
- [ ] ✅ 运行了配置脚本
- [ ] ✅ TUN0 设备已创建
- [ ] ✅ 可以 ping 通容器 IP
- [ ] ✅ 容器可以访问外网
- [ ] ✅ 可以访问 Kubernetes Service（如果使用）
- [ ] ✅ DNS 解析正常（如果配置）

---

## 🎉 总结

**三步搞定网络配置**：

1. 🔧 配置 Docker：`"iptables": false`
2. 🚀 运行脚本：`sudo ./setup-docker-network.sh`
3. ✅ 验证访问：`curl http://<container_ip>`

**遇到问题？**

- 📖 查看[故障排查](#-故障排查)章节
- 🔍 运行 `sudo ./setup-docker-network.sh -v` 查看详情
- 💬 [提交 Issue](https://github.com/wenjunxiao/mac-docker-connector/issues) 获取帮助

---

<div align="center">

**🌟 如果这个指南帮到了你，别忘了给项目点个 Star！**

Made with ❤️ by the community

</div>