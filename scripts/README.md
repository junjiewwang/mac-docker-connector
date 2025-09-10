
## 配置NAT 可访问外网
```
# 启用NAT伪装（动态IP适用）
iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE
```

## 配置mac 可访问docker容器
```
# 允许 tun0 → 容器网桥的转发
iptables -I FORWARD 1 -i tun0 -o br-73914898e944 -j ACCEPT
# 允许回包
iptables -I FORWARD 2 -i br-73914898e944 -o tun0 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT

# 绕过 Docker 隔离规则
iptables -I DOCKER-ISOLATION-STAGE-2 1 -i tun0 -o br-73914898e944 -j RETURN
```