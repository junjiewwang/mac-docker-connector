package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/kardianos/service"
	"github.com/op/go-logging"
	"github.com/songgao/water"
)

type Connector struct {
	iface *water.Interface
	stop  bool
}

func (c *Connector) Start(s service.Service) error {
	c.stop = false
	go c.run()
	return nil
}

func (c *Connector) Stop(s service.Service) error {
	c.stop = true
	go func() {
		clearRoutes()
		if conn != nil {
			conn.Close()
		}
		if c.iface != nil {
			c.iface.Close()
		}
	}()
	return nil
}

func (c *Connector) run() {
	flag.Parse()
	if level, err := logging.LogLevel(logLevel); err == nil {
		logging.SetLevel(level, "vpn")
	}

	// 输出网络调试信息
	logger.Infof("[NETWORK DEBUG] Starting desktop-docker-connector")
	logger.Infof("[NETWORK DEBUG] Local IP will be: %v", localIP)
	logger.Infof("[NETWORK DEBUG] Peer address: %s", addr)
	logger.Infof("[NETWORK DEBUG] Listen host: %s, port: %d", host, port)
	logger.Infof("[NETWORK DEBUG] Bind to interface: %v", bind)
	logger.Infof("[NETWORK DEBUG] Config file: %s", configFile)
	logger.Infof("[NETWORK DEBUG] Log level: %s", logLevel)
	if logfile != "" {
		if !filepath.IsAbs(logfile) {
			path, err := filepath.Abs(os.Args[0])
			if err == nil {
				logfile = filepath.Join(path, "..", logfile)
			}
		}
		file, err := os.OpenFile(logfile, os.O_RDWR|os.O_TRUNC|os.O_CREATE, 0660)
		if err == nil {
			backend := logging.NewLogBackend(file, "", log.LstdFlags)
			leveledBackend = logging.AddModuleLevel(backend)
			logger.SetBackend(leveledBackend)
		}
	}
	if configFile != "" && !filepath.IsAbs(configFile) {
		path, err := filepath.Abs(os.Args[0])
		if err == nil {
			configFile = filepath.Join(path, "..", configFile)
		}
		logger.Infof("config file => %v\n", configFile)
	}
	var iface *water.Interface
	if _, err := os.Stat(configFile); err == nil {
		logger.Infof("load config(%v) => %s\n", watch, configFile)
		iface = loadConfig(iface, true)
		if watch {
			watcher, err := fsnotify.NewWatcher()
			if err != nil {
				logger.Fatal(err)
			}
			var timer *time.Timer
			defer watcher.Close()
			loader := func() {
				timer = nil
				loadConfig(iface, false)
			}
			go func() {
				for {
					select {
					case event, ok := <-watcher.Events:
						if !ok {
							return
						}
						if event.Op&fsnotify.Write == fsnotify.Write {
							logger.Debugf("config file changed => %s\n", configFile)
							if timer != nil {
								timer.Stop()
							}
							timer = time.AfterFunc(time.Duration(2)*time.Second, loader)
						} else if event.Op&fsnotify.Rename == fsnotify.Rename {
							logger.Debugf("config file renamed => %s\n", event.Name)
							if timer != nil {
								timer.Stop()
							}
							timer = time.AfterFunc(time.Duration(2)*time.Second, loader)
							if err = watcher.Remove(configFile); err != nil {
								logger.Warningf("remove watch error => %v\n", err)
							}
							if err = watcher.Add(event.Name); err != nil {
								logger.Warningf("watch error => %v\n", err)
							}
						}
					case err, ok := <-watcher.Errors:
						if !ok {
							return
						}
						logger.Info("error:", err)
					}
				}
			}()
			if err = watcher.Add(configFile); err == nil {
				if full, err := filepath.Abs(configFile); err != nil {
					logger.Debugf("watch config => %s\n", full)
				} else {
					logger.Debugf("watch config => %s\n", configFile)
				}
			} else {
				logger.Warningf("watch error => %v\n", err)
			}
		}
	} else {
		if peer, subnet, err = net.ParseCIDR(addr); err != nil {
			logger.Fatal(err)
		}
		copy([]byte(localIP), []byte(peer.To4()))
		localIP[3]++
		if bind {
			iface = setup(localIP, peer, subnet)
		}
	}
	udpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		logger.Fatalf("invalid address => %s:%d", host, port)
	}
	// 监听
	conn, err = net.ListenUDP("udp", udpAddr)
	if err != nil {
		logger.Fatalf("failed to listen %s:%d => %s", host, port, err.Error())
		return
	}
	defer conn.Close()
	logger.Infof("[UDP LISTENER] Successfully listening on %v", conn.LocalAddr())

	// 输出网络接口状态
	if iface != nil {
		logger.Infof("[TUN INTERFACE] TUN interface created: %s", iface.Name())
		logger.Infof("[TUN INTERFACE] Local IP: %d.%d.%d.%d", localIP[0], localIP[1], localIP[2], localIP[3])
		if peer != nil {
			logger.Infof("[TUN INTERFACE] Peer IP: %s", peer.String())
		}
		if subnet != nil {
			logger.Infof("[TUN INTERFACE] Subnet: %s", subnet.String())
		}
	} else {
		logger.Warningf("[TUN INTERFACE] No TUN interface bound - running in proxy mode only")
	}

	// 客户端连接信息
	if cliAddr == "" {
		logger.Infof("[CLIENT] Looking for saved peer info in %s", TmpPeer)
		if tmp, err := ioutil.ReadFile(TmpPeer); err == nil {
			if cli, err = net.ResolveUDPAddr("udp", string(tmp)); err == nil {
				logger.Infof("[CLIENT] Loaded saved peer: %v", cli)
			} else {
				logger.Warningf("[CLIENT] Failed to parse saved peer address '%s': %v", string(tmp), err)
			}
		} else {
			logger.Infof("[CLIENT] No saved peer info found, waiting for client connection")
		}
	} else {
		if cli, err = net.ResolveUDPAddr("udp", cliAddr); err == nil {
			logger.Infof("[CLIENT] Using configured peer: %v", cli)
		} else {
			logger.Warningf("[CLIENT] Failed to parse configured peer address '%s': %v", cliAddr, err)
		}
	}

	// 输出当前配置状态
	logger.Infof("[CONFIG] IPTables rules count: %d", len(iptables))
	for rule, enabled := range iptables {
		logger.Debugf("[CONFIG] IPTables rule '%s': %v", rule, enabled)
	}
	logger.Debugf("[CONFIG] Hosts config: %s", hosts)
	c.iface = iface

	// 输出网络诊断信息
	logNetworkDiagnostics(iface)

	// 启动定期网络状态检查
	go func() {
		ticker := time.NewTicker(30 * time.Second) // 每30秒检查一次
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if !c.stop {
					logger.Debugf("[HEALTH CHECK] Periodic network status check")
					if cli == nil {
						logger.Warningf("[HEALTH CHECK] No client connected - waiting for connection")
					} else {
						logger.Debugf("[HEALTH CHECK] Client connected: %v", cli)
					}
					if iface == nil {
						logger.Warningf("[HEALTH CHECK] TUN interface not available")
					}
				}
			}
			if c.stop {
				break
			}
		}
	}()

	go func() {
		if iface == nil {
			logger.Info("not bind to interface")
			return
		}
		buf := make([]byte, 2000)
		for {
			n, err := iface.Read(buf)
			if err != nil {
				if c.stop {
					break
				}
				logger.Warningf("tap read error: %v\n", err)
				continue
			}

			// 记录详细的数据包信息
			logPacketDetails(buf, n, "TUN->UDP")

			if localIP[0] == buf[16] && localIP[1] == buf[17] && localIP[2] == buf[18] && localIP[3] == buf[19] {
				logger.Debugf("[LOCAL LOOPBACK] Packet to local IP: %d.%d.%d.%d", localIP[0], localIP[1], localIP[2], localIP[3])
				if _, err := iface.Write(buf[:n]); err != nil {
					logger.Warningf("local write error: %v\n", err)
				}
				continue
			}

			// 检查客户端连接状态
			if cli == nil {
				logger.Warningf("[TUN->UDP] No client connected, dropping packet to %d.%d.%d.%d", buf[16], buf[17], buf[18], buf[19])
				continue
			}

			if _, err := conn.WriteToUDP(buf[:n], cli); err != nil {
				logger.Warningf("[TUN->UDP] UDP write error to client %v: %v\n", cli, err)
				continue
			}
			logger.Debugf("[TUN->UDP] Successfully forwarded packet to client %v", cli)
		}
	}()
	var lastCli string
	var n int
	data := make([]byte, 2000)
	logger.Infof("[UDP LISTENER] Starting UDP packet processing loop, listening on %v", conn.LocalAddr())

	for {
		n, cli, err = conn.ReadFromUDP(data)
		if err != nil {
			if c.stop {
				break
			}
			logger.Warning("failed read udp msg, error: " + err.Error())
			continue
		}

		logger.Debugf("[UDP->TUN] Received %d bytes from client %v", n, cli)

		// 处理心跳包
		if data[0] == 0 && n == 1 {
			if lastCli == cli.String() {
				logger.Debugf("[HEARTBEAT] Client heartbeat => %v", cli)
			} else {
				if lastCli == "" {
					logger.Infof("[CLIENT] Client init => %v", cli)
				} else {
					logger.Infof("[CLIENT] Client change from %s to %v", lastCli, cli)
				}
				lastCli = cli.String()
				if cliAddr == "" {
					if err := ioutil.WriteFile(TmpPeer, []byte(lastCli), 0644); err != nil {
						logger.Warningf("[CLIENT] Failed to save peer info: %v", err)
					} else {
						logger.Debugf("[CLIENT] Saved peer info to %s", TmpPeer)
					}
				}
				logger.Infof("[CONFIG] Sending controls to new client %v", cli)
				sendControls(cli, iptables, hosts)
			}
			continue
		}

		// 处理控制包
		if data[0] == 1 && n > 1 {
			logger.Debugf("[CONTROL] Received control packet from %v, size: %d", cli, n-1)
			appendConfig(data[1:n])
			continue
		}

		// 记录详细的数据包信息
		if n > 1 { // 排除心跳包和控制包
			logPacketDetails(data, n, "UDP->TUN")
		}

		dest := toIntIP(data, 16, 17, 18, 19)
		if sess, ok := sessions[dest]; ok && n > 1 {
			logger.Debugf("[SESSION] Forwarding packet to session %v (dest IP: %d.%d.%d.%d)", sess,
				(dest>>24)&0xFF, (dest>>16)&0xFF, (dest>>8)&0xFF, dest&0xFF)
			if _, err := expose.WriteToUDP(data[:n], sess); err != nil {
				logger.Warningf("[SESSION] Session write error: %d bytes, dest: %v, error: %v", n, sess, err)
			}
		} else if bind {
			if iface == nil {
				logger.Warningf("[TUN] Interface not available, dropping packet")
				continue
			}

			logger.Debugf("[UDP->TUN] Writing %d bytes to TUN interface", n)
			if _, err := iface.Write(data[:n]); err != nil {
				logger.Warningf("[UDP->TUN] TUN write error: %d bytes, error: %v", n, err)

				// 提供更详细的错误信息
				if n > 20 {
					dstIP := fmt.Sprintf("%d.%d.%d.%d", data[16], data[17], data[18], data[19])
					logger.Warningf("[UDP->TUN] Failed packet destination: %s", dstIP)
				}
			} else {
				logger.Debugf("[UDP->TUN] Successfully wrote packet to TUN interface")
			}
		} else {
			logger.Debugf("[UDP->TUN] Not bound to interface, skipping packet write")
		}
	}
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func toIntIP(packet []byte, offset0 int, offset1 int, offset2 int, offset3 int) (sum uint64) {
	sum = 0
	sum += uint64(packet[offset0]) << 24
	sum += uint64(packet[offset1]) << 16
	sum += uint64(packet[offset2]) << 8
	sum += uint64(packet[offset3])
	return sum
}

// 网络诊断辅助函数
func logNetworkDiagnostics(iface *water.Interface) {
	logger.Infof("[DIAGNOSTICS] =========================")
	logger.Infof("[DIAGNOSTICS] Network Connectivity Check")
	logger.Infof("[DIAGNOSTICS] =========================")

	// 检查TUN接口状态
	if iface != nil {
		logger.Infof("[DIAGNOSTICS] ✓ TUN interface is active: %s", iface.Name())
	} else {
		logger.Warningf("[DIAGNOSTICS] ✗ TUN interface is not available")
	}

	// 检查UDP连接状态
	if conn != nil {
		logger.Infof("[DIAGNOSTICS] ✓ UDP listener is active on: %v", conn.LocalAddr())
	} else {
		logger.Warningf("[DIAGNOSTICS] ✗ UDP listener is not available")
	}

	// 检查客户端连接状态
	if cli != nil {
		logger.Infof("[DIAGNOSTICS] ✓ Client connected: %v", cli)
	} else {
		logger.Warningf("[DIAGNOSTICS] ✗ No client connected")
	}

	// 检查网络配置
	logger.Infof("[DIAGNOSTICS] Local IP: %d.%d.%d.%d", localIP[0], localIP[1], localIP[2], localIP[3])
	if peer != nil {
		logger.Infof("[DIAGNOSTICS] Peer IP: %s", peer.String())
	}
	if subnet != nil {
		logger.Infof("[DIAGNOSTICS] Subnet: %s", subnet.String())
	}

	logger.Infof("[DIAGNOSTICS] =========================")
}

// 解析并记录数据包详细信息
func logPacketDetails(data []byte, n int, direction string) {
	if n < 20 {
		logger.Debugf("[PACKET %s] Packet too small: %d bytes", direction, n)
		return
	}

	version := (data[0] >> 4) & 0x0F
	_ = (data[0] & 0x0F) * 4 // headerLen - unused but kept for documentation
	_ = data[1]              // tos - unused but kept for documentation
	totalLen := (uint16(data[2]) << 8) | uint16(data[3])
	id := (uint16(data[4]) << 8) | uint16(data[5])
	flags := (data[6] >> 5) & 0x07
	fragOffset := ((uint16(data[6]) & 0x1F) << 8) | uint16(data[7])
	ttl := data[8]
	protocol := data[9]
	_ = (uint16(data[10]) << 8) | uint16(data[11]) // checksum - unused but kept for documentation
	srcIP := fmt.Sprintf("%d.%d.%d.%d", data[12], data[13], data[14], data[15])
	dstIP := fmt.Sprintf("%d.%d.%d.%d", data[16], data[17], data[18], data[19])

	var protocolName string
	switch protocol {
	case 1:
		protocolName = "ICMP"
	case 6:
		protocolName = "TCP"
	case 17:
		protocolName = "UDP"
	default:
		protocolName = fmt.Sprintf("Protocol-%d", protocol)
	}

	logger.Debugf("[PACKET %s] IP v%d, Len:%d, ID:%d, TTL:%d, Proto:%s, %s->%s",
		direction, version, totalLen, id, ttl, protocolName, srcIP, dstIP)

	if flags&0x02 != 0 {
		logger.Debugf("[PACKET %s] Don't Fragment flag set", direction)
	}
	if fragOffset != 0 {
		logger.Debugf("[PACKET %s] Fragment offset: %d", direction, fragOffset)
	}

	// 协议特定信息
	if protocol == 1 && n >= 24 { // ICMP
		icmpType := data[20]
		icmpCode := data[21]
		icmpChecksum := (uint16(data[22]) << 8) | uint16(data[23])
		logger.Debugf("[PACKET %s] ICMP Type:%d, Code:%d, Checksum:0x%04x", direction, icmpType, icmpCode, icmpChecksum)

		switch icmpType {
		case 8:
			logger.Debugf("[PACKET %s] ICMP Echo Request (ping)", direction)
		case 0:
			logger.Debugf("[PACKET %s] ICMP Echo Reply (pong)", direction)
		case 3:
			logger.Debugf("[PACKET %s] ICMP Destination Unreachable", direction)
		case 11:
			logger.Debugf("[PACKET %s] ICMP Time Exceeded", direction)
		}
	}
}

func sendControls(cli *net.UDPAddr, tables map[string]bool, hosts string) {
	logger.Infof("[CONTROL] Sending controls to client %v", cli)
	logger.Debugf("[CONTROL] IPTables rules: %v", tables)
	logger.Debugf("[CONTROL] Hosts config: %s", hosts)

	var reply bytes.Buffer
	controlCount := 0
	for k, v := range tables {
		if reply.Len() > 0 {
			reply.WriteString(",")
		}
		if v {
			reply.WriteString("connect ")
			logger.Debugf("[CONTROL] Adding connect rule: %s", k)
		} else {
			reply.WriteString("disconnect ")
			logger.Debugf("[CONTROL] Adding disconnect rule: %s", k)
		}
		reply.WriteString(k)
		controlCount++
	}

	loadHosts(&reply, hosts)
	l := reply.Len()

	logger.Infof("[CONTROL] Prepared %d control rules, total payload size: %d bytes", controlCount, l)

	if l < 50 {
		logger.Infof("[CONTROL] Sending to client %s: %d bytes - %s", cli, l, reply.String())
	} else {
		logger.Infof("[CONTROL] Sending to client %s: %d bytes (payload too large to display)", cli, l)
	}

	if l > 0 {
		l16 := uint16(l)
		header := make([]byte, 3)
		header[0] = 1
		header[1] = byte(l16 >> 8)
		header[2] = byte(l16 & 0x00ff)

		logger.Debugf("[CONTROL] Sending header: [%d, %d, %d] (length: %d)", header[0], header[1], header[2], l16)
		if _, err := conn.WriteToUDP(header, cli); err != nil {
			logger.Warningf("[CONTROL] Failed to send header to %v: %v", cli, err)
			return
		}

		tmp := reply.Bytes()
		chunks := 0
		for i := 0; i < l; i += MTU {
			chunkSize := min(i+MTU, l) - i
			logger.Debugf("[CONTROL] Sending chunk %d: %d bytes (offset %d-%d)", chunks+1, chunkSize, i, i+chunkSize-1)
			if _, err := conn.WriteToUDP(tmp[i:min(i+MTU, l)], cli); err != nil {
				logger.Warningf("[CONTROL] Failed to send chunk %d to %v: %v", chunks+1, cli, err)
				return
			}
			chunks++
		}
		logger.Infof("[CONTROL] Successfully sent %d chunks to client %v", chunks, cli)
	} else {
		logger.Infof("[CONTROL] No controls to send to client %v", cli)
	}
}
