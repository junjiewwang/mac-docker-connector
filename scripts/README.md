
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


## 网络不通的排查：

### 检查 iptables 规则：是否在 FORWARD 链之前被拦截？
iptables 的处理流程是「表→链→规则」，数据包需先经过raw表、mangle表，再到filter表的FORWARD链。若前面的表 / 链有拦截规则，会导致包无法到达FORWARD。
1. 检查所有表的规则（重点看PREROUTING链）
    ```bash
    列出所有表的所有链规则（带行号）
    sudo iptables -t raw -L -n --line-numbers
    sudo iptables -t mangle -L -n --line-numbers
    sudo iptables -t nat -L -n --line-numbers
    sudo iptables -t filter -L -n --line-numbers  # filter表包含FORWARD链
    ```
2. 关键排查点：
   是否有DROP/REJECT规则在FORWARD之前生效？
   例如：mangle表的PREROUTING链若有DROP规则，会直接丢弃包，不进入FORWARD。
   FORWARD链的默认策略是否为DROP？
   若默认策略是DROP，且没有允许转发的规则，包会被丢弃（但仍会进入FORWARD链再被丢弃，可通过日志确认）。
3. 若raw中有Drop记录，指向的是Docker容器的IP地址，则需要参考docker网络的文档
   > To stop Docker from setting the FORWARD chain's policy to DROP, include "ip-forward-no-drop": true in /etc/docker/daemon.json, or add option --ip-forward-no-drop to the dockerd command line.

## 资料
### docker网络
https://docs.docker.com/engine/network/packet-filtering-firewalls/