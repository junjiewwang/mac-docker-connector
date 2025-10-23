# Desktop Docker Connector 调试指南

## 概述
本指南帮助你使用新增的调试功能来排查 Mac 访问容器 IP 不通的问题。

## 启用调试模式

### 1. 设置日志级别
```bash
# 启动时设置为 DEBUG 级别
./desktop-docker-connector -log-level DEBUG

# 或者设置为 INFO 级别（推荐）
./desktop-docker-connector -log-level INFO
```

### 2. 启用日志文件
```bash
# 将日志输出到文件
./desktop-docker-connector -log-level DEBUG -log-file connector.log
```

## 调试信息说明

### 网络诊断信息
启动时会输出完整的网络诊断信息：
```
[DIAGNOSTICS] =========================
[DIAGNOSTICS] Network Connectivity Check
[DIAGNOSTICS] =========================
[DIAGNOSTICS] ✓ TUN interface is active: utun3
[DIAGNOSTICS] ✓ UDP listener is active on: 127.0.0.1:2511
[DIAGNOSTICS] ✓ Client connected: 172.17.0.2:54321
[DIAGNOSTICS] Local IP: 95.112.80.9
[DIAGNOSTICS] Peer IP: 192.168.251.1
[DIAGNOSTICS] Subnet: 192.168.251.1/24
```

### 数据包跟踪
每个数据包都会被详细记录：
```
[PACKET TUN->UDP] IP v4, Len:84, ID:12345, TTL:64, Proto:ICMP, 95.112.80.9->172.19.0.2
[PACKET TUN->UDP] ICMP Type:8, Code:0, Checksum:0x1234
[PACKET TUN->UDP] ICMP Echo Request (ping)
```

### 客户端连接状态
```
[CLIENT] Client init => 172.17.0.2:54321
[HEARTBEAT] Client heartbeat => 172.17.0.2:54321
[CLIENT] Client change from 172.17.0.2:54321 to 172.17.0.3:54322
```

### 控制信息传输
```
[CONTROL] Sending controls to client 172.17.0.2:54321
[CONTROL] Adding connect rule: -A FORWARD -d 172.19.0.2/32 -j ACCEPT
[CONTROL] Successfully sent 2 chunks to client 172.17.0.2:54321
```

## 排查步骤

### 1. 检查基础连接
```bash
# 查看是否有客户端连接
grep "Client init\|Client change" connector.log

# 查看心跳状态
grep "HEARTBEAT" connector.log
```

### 2. 检查数据包流向
```bash
# 查看从 Mac 发出的数据包
grep "TUN->UDP.*172.19.0.2" connector.log

# 查看从容器返回的数据包  
grep "UDP->TUN.*172.19.0.2" connector.log
```

### 3. 检查 ICMP 包（ping）
```bash
# 查看 ping 请求
grep "ICMP Echo Request" connector.log

# 查看 ping 回复
grep "ICMP Echo Reply" connector.log
```

### 4. 检查控制规则
```bash
# 查看发送给容器的控制规则
grep "CONTROL.*connect.*172.19.0.2" connector.log
```

## 常见问题诊断

### 问题1：没有客户端连接
**症状：** 看到 `No client connected` 警告
**解决：** 
1. 检查 Docker 容器是否运行
2. 检查容器中的 connector 是否启动
3. 检查网络连通性

### 问题2：数据包只有出没有回
**症状：** 只看到 `TUN->UDP` 没有 `UDP->TUN`
**解决：**
1. 检查目标容器是否存在且运行
2. 检查容器内的路由配置
3. 检查 iptables 规则是否正确应用

### 问题3：TUN 接口问题
**症状：** 看到 `TUN interface not available`
**解决：**
1. 检查是否有管理员权限
2. 检查 TUN/TAP 驱动是否安装
3. 重启服务

### 问题4：控制规则未发送
**症状：** 没有看到 `CONTROL` 相关日志
**解决：**
1. 检查配置文件是否正确
2. 检查客户端是否发送了心跳包
3. 重新加载配置

## 实时监控命令

```bash
# 实时查看所有调试信息
tail -f connector.log

# 只查看错误和警告
tail -f connector.log | grep -E "ERROR|WARNING|WARN"

# 只查看特定 IP 的流量
tail -f connector.log | grep "172.19.0.2"

# 查看数据包统计
grep -c "TUN->UDP" connector.log
grep -c "UDP->TUN" connector.log
```

## 性能考虑

- DEBUG 级别会产生大量日志，仅在排查问题时使用
- 生产环境建议使用 INFO 或 WARNING 级别
- 定期清理日志文件避免磁盘空间不足

## 联系支持

如果问题仍然存在，请提供以下信息：
1. 完整的启动日志
2. 问题发生时的数据包跟踪日志
3. 网络配置信息
4. Docker 容器状态