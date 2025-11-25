#!/usr/bin/env python3
"""
Lima Docker 虚拟机网络配置脚本

功能：配置Docker网桥、Minikube集群路由、DNS解析和网络转发规则
"""

import json
import os
import re
import subprocess
import sys
import time
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import List, Optional, Dict
from functools import lru_cache

# ==================== 配置常量 ====================

class Config:
    """配置常量"""
    DAEMON_JSON_PATH = Path("/etc/docker/daemon.json")
    DNS_CONF_DIR = Path("/etc/systemd/resolved.conf.d")
    DNS_CONF_FILE = DNS_CONF_DIR / "minikube-dns.conf"
    HOSTS_FILE = Path("/etc/hosts")
    REQUIRED_COMMANDS = ['docker', 'iptables', 'ip']
    IPTABLES_TABLES = ['filter', 'nat']

# ==================== 日志和命令执行 ====================

class Logger:
    """统一的日志工具"""
    COLORS = {
        'INFO': '\033[0;32m',
        'WARN': '\033[1;33m',
        'ERROR': '\033[0;31m',
        'DEBUG': '\033[0;34m',
        'NC': '\033[0m'
    }
    
    @staticmethod
    def _log(level: str, message: str, file=None):
        color = Logger.COLORS.get(level, '')
        nc = Logger.COLORS['NC']
        print(f"{color}[{level}]{nc} {message}", file=file)
    
    @staticmethod
    def info(message: str):
        Logger._log('INFO', message)
    
    @staticmethod
    def warn(message: str):
        Logger._log('WARN', message)
    
    @staticmethod
    def error(message: str):
        Logger._log('ERROR', message, sys.stderr)
    
    @staticmethod
    def debug(message: str):
        Logger._log('DEBUG', message)
    
    @staticmethod
    def section(title: str):
        """打印章节标题"""
        print(f"\n{'='*42}\n{title}\n{'='*42}\n")

class CommandExecutor:
    """精简的命令执行工具"""
    
    @staticmethod
    def run(cmd: List[str], check: bool = True, capture_output: bool = True, 
            shell: bool = False, sudo: bool = False) -> subprocess.CompletedProcess:
        """统一的命令执行接口"""
        if sudo:
            if shell:
                cmd_str = ' '.join(cmd) if isinstance(cmd, list) else cmd
                cmd = ['sudo', 'bash', '-c', cmd_str]
                shell = False
            else:
                cmd = ['sudo'] + cmd
        
        if cmd and 'kubectl' in str(cmd[0]):
            shell = True
            cmd = ' '.join(cmd) if isinstance(cmd, list) else cmd
        
        try:
            return subprocess.run(cmd, check=check, capture_output=capture_output, 
                                text=True, shell=shell)
        except subprocess.CalledProcessError as e:
            if check:
                cmd_str = cmd if isinstance(cmd, str) else ' '.join(cmd)
                Logger.error(f"命令执行失败: {cmd_str}")
                Logger.error(f"错误信息: {e.stderr}")
                raise
            return e
        except OSError as e:
            if e.errno == 8 and not shell:
                Logger.debug(f"尝试使用 shell 模式: {' '.join(cmd)}")
                return CommandExecutor.run(cmd, check=check, capture_output=capture_output, 
                                         shell=True, sudo=sudo)
            raise
    
    @staticmethod
    def run_sudo(cmd: List[str], **kwargs) -> subprocess.CompletedProcess:
        """以 sudo 权限执行命令"""
        return CommandExecutor.run(cmd, sudo=True, **kwargs)
    
    @staticmethod
    def command_exists(cmd: str) -> bool:
        """检查命令是否存在"""
        return subprocess.run(['which', cmd], capture_output=True).returncode == 0

# ==================== 数据模型 ====================

@dataclass
class BridgeInfo:
    """Docker 网桥信息"""
    name: str
    subnet: str
    network_id: str = ""

@dataclass
class MinikubeInfo:
    """Minikube 信息"""
    bridge_name: str
    container_ip: str
    subnet: str
    service_cidr: Optional[str] = None

# ==================== Docker 配置管理 ====================

class DockerConfigManager:
    """Docker 配置管理器"""
    
    @classmethod
    def check_and_fix_iptables_config(cls) -> bool:
        """检查并修复 Docker iptables 配置"""
        Logger.section("🔍 检查 Docker iptables 配置")
        
        needs_restart = False
        path = Config.DAEMON_JSON_PATH
        
        if not path.exists():
            Logger.warn(f"Docker 配置文件不存在: {path}")
            Logger.info("创建新的配置文件...")
            needs_restart = cls._create_config_file()
        else:
            Logger.info(f"检查配置文件: {path}")
            needs_restart = cls._check_and_update_config()
        
        if needs_restart:
            cls._restart_docker()
        
        cls._verify_config()
        Logger.section("✅ Docker iptables 配置检查完成")
        return True
    
    @classmethod
    def _create_config_file(cls) -> bool:
        """创建新的配置文件"""
        config = {"iptables": False}
        path = Config.DAEMON_JSON_PATH
        
        try:
            path.parent.mkdir(parents=True, exist_ok=True)
            with open(path, 'w') as f:
                json.dump(config, f, indent=2)
            
            CommandExecutor.run_sudo(['chown', 'root:root', str(path)])
            CommandExecutor.run_sudo(['chmod', '644', str(path)])
            Logger.info("✅ 配置文件已创建")
            return True
        except Exception as e:
            Logger.error(f"❌ 创建配置文件失败: {e}")
            sys.exit(1)
    
    @classmethod
    def _check_and_update_config(cls) -> bool:
        """检查并更新配置"""
        path = Config.DAEMON_JSON_PATH
        try:
            with open(path, 'r') as f:
                config = json.load(f)
            
            if config.get('iptables') is False:
                Logger.info("✅ Docker iptables 配置正确: iptables = false")
                return False
            
            Logger.warn(f"⚠️  Docker iptables 配置错误: iptables = {config.get('iptables')}")
            Logger.info("需要修改为: iptables = false")
            
            backup_path = f"{path}.backup.{datetime.now().strftime('%Y%m%d_%H%M%S')}"
            CommandExecutor.run_sudo(['cp', str(path), backup_path])
            Logger.info(f"已备份原配置到: {backup_path}")
            
            config['iptables'] = False
            with open(path, 'w') as f:
                json.dump(config, f, indent=2)
            
            Logger.info("✅ 配置已修改")
            return True
        except json.JSONDecodeError as e:
            Logger.error(f"❌ 配置文件格式错误: {e}")
            sys.exit(1)
        except Exception as e:
            Logger.error(f"❌ 配置检查失败: {e}")
            sys.exit(1)
    
    @classmethod
    def _restart_docker(cls):
        """重启 Docker 服务"""
        Logger.warn("⚠️  Docker 配置已修改，需要重启 Docker 服务")
        Logger.info("正在重启 Docker 服务...")
        
        try:
            CommandExecutor.run_sudo(['systemctl', 'restart', 'docker'])
            Logger.info("✅ Docker 服务重启命令已发出，开始健康检查...")
            
            # 以更快的轮询替代固定等待，尽快恢复继续执行
            max_wait_seconds = 10
            for i in range(max_wait_seconds):
                result = CommandExecutor.run(['docker', 'ps'], check=False)
                if result.returncode == 0:
                    Logger.info("✅ Docker 服务运行正常")
                    break
                time.sleep(1)
            else:
                Logger.error("❌ Docker 服务启动异常")
                sys.exit(1)
        except Exception as e:
            Logger.error(f"❌ Docker 服务重启失败: {e}")
            Logger.info("请手动执行: sudo systemctl restart docker")
            sys.exit(1)
    
    @classmethod
    def _verify_config(cls):
        """验证配置"""
        try:
            with open(Config.DAEMON_JSON_PATH, 'r') as f:
                config = json.load(f)
            
            if config.get('iptables') is False:
                Logger.info("✅ Docker iptables 配置验证通过")
            else:
                Logger.error("❌ Docker iptables 配置验证失败")
                Logger.error("当前配置内容:")
                print(json.dumps(config, indent=2))
                sys.exit(1)
        except Exception as e:
            Logger.error(f"❌ 配置验证失败: {e}")
            sys.exit(1)

# ==================== Hostname 配置管理 ====================

class HostnameConfigManager:
    """Hostname 配置管理器"""
    
    @classmethod
    def check_and_configure_hostname(cls) -> bool:
        """检查并配置 hostname 到 /etc/hosts"""
        Logger.section("🔍 检查 hostname 配置")
        
        try:
            hostname = subprocess.run(['hostname'], capture_output=True, text=True, check=True).stdout.strip()
            if not hostname:
                Logger.error("无法获取主机名")
                return False
            
            Logger.info(f"当前主机名: {hostname}")
            
            hosts_file = Config.HOSTS_FILE
            if not hosts_file.exists():
                Logger.error(f"{hosts_file} 文件不存在")
                return False
            
            # 检查 hostname 是否已在 /etc/hosts 中
            with open(hosts_file, 'r') as f:
                content = f.read()
            
            # 使用正则表达式检查 hostname 是否存在
            pattern = rf'^127\.0\.0\.1\s+.*\s+{re.escape(hostname)}(\s|$)'
            pattern2 = rf'^127\.0\.0\.1\s+{re.escape(hostname)}(\s|$)'
            
            if re.search(pattern, content, re.MULTILINE) or re.search(pattern2, content, re.MULTILINE):
                Logger.info("✅ hostname 已存在于 /etc/hosts 中")
            else:
                Logger.warn("⚠️  hostname 不在 /etc/hosts 中，正在添加...")
                cls._add_hostname_to_hosts(hostname, hosts_file)
            
            Logger.section("✅ hostname 配置检查完成")
            return True
            
        except Exception as e:
            Logger.error(f"❌ hostname 配置失败: {e}")
            return False
    
    @classmethod
    def _add_hostname_to_hosts(cls, hostname: str, hosts_file: Path):
        """添加 hostname 到 /etc/hosts"""
        try:
            # 备份 /etc/hosts
            backup_path = f"{hosts_file}.backup.{datetime.now().strftime('%Y%m%d_%H%M%S')}"
            CommandExecutor.run_sudo(['cp', str(hosts_file), backup_path])
            Logger.info(f"已备份 /etc/hosts 到: {backup_path}")
            
            with open(hosts_file, 'r') as f:
                lines = f.readlines()
            
            # 检查是否已有 127.0.0.1 行
            localhost_line_idx = -1
            for idx, line in enumerate(lines):
                if re.match(r'^127\.0\.0\.1\s+', line):
                    localhost_line_idx = idx
                    break
            
            if localhost_line_idx >= 0:
                # 在现有的 127.0.0.1 行末尾添加 hostname
                line = lines[localhost_line_idx].rstrip()
                if not re.search(rf'\s+{re.escape(hostname)}(\s|$)', line):
                    lines[localhost_line_idx] = f"{line} {hostname}\n"
                    Logger.info("✅ 已将 hostname 添加到现有的 127.0.0.1 行")
            else:
                # 没有 127.0.0.1 行，添加新行
                lines.append(f"127.0.0.1 localhost {hostname}\n")
                Logger.info("✅ 已添加新的 127.0.0.1 行包含 hostname")
            
            # 写入文件
            temp_file = f"{hosts_file}.tmp"
            with open(temp_file, 'w') as f:
                f.writelines(lines)
            
            CommandExecutor.run_sudo(['mv', temp_file, str(hosts_file)])
            CommandExecutor.run_sudo(['chmod', '644', str(hosts_file)])
            
            # 验证添加结果
            with open(hosts_file, 'r') as f:
                content = f.read()
            
            pattern = rf'^127\.0\.0\.1\s+.*\s+{re.escape(hostname)}(\s|$)'
            pattern2 = rf'^127\.0\.0\.1\s+{re.escape(hostname)}(\s|$)'
            
            if re.search(pattern, content, re.MULTILINE) or re.search(pattern2, content, re.MULTILINE):
                Logger.info("✅ hostname 配置验证通过")
            else:
                Logger.error("❌ hostname 配置验证失败")
                return False
            
        except Exception as e:
            Logger.error(f"❌ 添加 hostname 失败: {e}")
            raise

# ==================== iptables 规则管理 ====================

class IptablesManager:
    """统一的 iptables 规则管理器（支持批量操作）"""
    
    def __init__(self, batch_mode: bool = True):
        self.batch_mode = batch_mode
        self.rules_to_add = []
        self._cache = {}
    
    @staticmethod
    def rule_exists(table: str, chain: str, rule: List[str]) -> bool:
        """检查规则是否存在"""
        cmd = ['sudo', 'iptables']
        if table != 'filter':
            cmd.extend(['-t', table])
        cmd.extend(['-C', chain] + rule)
        return subprocess.run(cmd, capture_output=True).returncode == 0
    
    def add_rule(self, table: str, chain: str, rule: List[str]):
        """添加规则（支持批量模式）"""
        if self.batch_mode:
            self.rules_to_add.append({'table': table, 'chain': chain, 'rule': rule})
        else:
            self._execute_rule(table, chain, rule)
    
    def _execute_rule(self, table: str, chain: str, rule: List[str]) -> bool:
        """立即执行单条规则"""
        if self.rule_exists(table, chain, rule):
            Logger.debug(f"规则已存在: iptables -t {table} -A {chain} {' '.join(rule)}")
            return False
        
        cmd = ['sudo', 'iptables']
        if table != 'filter':
            cmd.extend(['-t', table])
        cmd.extend(['-A', chain] + rule)
        
        CommandExecutor.run(cmd)
        Logger.info(f"已添加规则: iptables -t {table} -A {chain} {' '.join(rule)}")
        return True
    
    def _load_existing_rules(self, table: str, chain: str):
        """加载并缓存现有规则"""
        cache_key = f"{table}:{chain}"
        if cache_key in self._cache:
            return
        
        cmd = ['sudo', 'iptables']
        if table != 'filter':
            cmd.extend(['-t', table])
        cmd.extend(['-S', chain])
        
        result = CommandExecutor.run(cmd, check=False)
        self._cache[cache_key] = result.stdout if result.returncode == 0 else ""
    
    def _rule_exists_in_cache(self, table: str, chain: str, rule: List[str]) -> bool:
        """从缓存中检查规则是否存在"""
        cache_key = f"{table}:{chain}"
        if cache_key not in self._cache:
            self._load_existing_rules(table, chain)
        
        rule_str = ' '.join(rule)
        return rule_str in self._cache[cache_key]
    
    def commit(self) -> Dict[str, int]:
        """批量提交所有规则"""
        if not self.rules_to_add:
            return {'added': 0, 'skipped': 0}
        
        Logger.debug(f"开始批量处理 {len(self.rules_to_add)} 条规则...")
        
        # 预加载规则缓存
        tables_chains = set((r['table'], r['chain']) for r in self.rules_to_add)
        for table, chain in tables_chains:
            self._load_existing_rules(table, chain)
        
        added, skipped = 0, 0
        
        for rule_info in self.rules_to_add:
            table, chain, rule = rule_info['table'], rule_info['chain'], rule_info['rule']
            
            if self._rule_exists_in_cache(table, chain, rule):
                Logger.debug(f"规则已存在: iptables -t {table} -A {chain} {' '.join(rule)}")
                skipped += 1
                continue
            
            cmd = ['sudo', 'iptables']
            if table != 'filter':
                cmd.extend(['-t', table])
            cmd.extend(['-A', chain] + rule)
            
            try:
                CommandExecutor.run(cmd)
                Logger.info(f"已添加规则: iptables -t {table} -A {chain} {' '.join(rule)}")
                added += 1
                
                # 更新缓存
                cache_key = f"{table}:{chain}"
                self._cache[cache_key] += f"\n-A {chain} {' '.join(rule)}"
            except Exception as e:
                Logger.error(f"添加规则失败: {e}")
        
        self.rules_to_add = []
        Logger.debug(f"批量处理完成: 添加 {added} 条，跳过 {skipped} 条")
        return {'added': added, 'skipped': skipped}
    
    @staticmethod
    def list_rules(table: str, chain: str) -> List[str]:
        """列出规则"""
        cmd = ['sudo', 'iptables']
        if table != 'filter':
            cmd.extend(['-t', table])
        cmd.extend(['-L', chain, '-n', '--line-numbers'])
        
        result = CommandExecutor.run(cmd)
        return result.stdout.strip().split('\n')[2:]

# ==================== 网络信息获取 ====================

class NetworkInfoProvider:
    """网络信息提供者"""
    
    @staticmethod
    def get_physical_interface() -> Optional[str]:
        """获取物理网卡名称"""
        result = CommandExecutor.run(['ip', 'route'])
        match = re.search(r'default.*dev\s+(\S+)', result.stdout)
        return match.group(1) if match else None
    
    @staticmethod
    def get_docker_bridges() -> List[BridgeInfo]:
        """获取所有 Docker 网桥信息（批量加速版）"""
        bridges: List[BridgeInfo] = []
        # 先获取所有 bridge 驱动网络ID
        result = CommandExecutor.run(['docker', 'network', 'ls', '-q', '--filter', 'driver=bridge'])
        network_ids = [nid for nid in result.stdout.strip().split('\n') if nid]
        if not network_ids:
            return bridges
        
        # 一次性 inspect 所有网络，减少子进程调用次数
        fmt = '{{.ID}}|{{index .Options "com.docker.network.bridge.name"}}|{{range .IPAM.Config}}{{.Subnet}}{{end}}'
        inspect_cmd = ['docker', 'network', 'inspect'] + network_ids + ['--format', fmt]
        result = CommandExecutor.run(inspect_cmd, check=False)
        if result.returncode != 0:
            return bridges
        
        for line in result.stdout.strip().splitlines():
            parts = line.split('|')
            if len(parts) != 3:
                continue
            nid, bridge_name, subnet = parts
            bridge_name = bridge_name.strip()
            subnet = subnet.strip()
            if not bridge_name or bridge_name == '<no value>':
                bridge_name = f"br-{nid[:12]}"
            if not NetworkInfoProvider._interface_exists(bridge_name):
                continue
            if subnet:
                bridges.append(BridgeInfo(name=bridge_name, subnet=subnet, network_id=nid))
        
        return bridges
    
    @staticmethod
    def get_minikube_info() -> Optional[MinikubeInfo]:
        """获取 Minikube 信息（减少 docker 调用次数）"""
        result = CommandExecutor.run(
            ['docker', 'ps', '--filter', 'name=minikube', '--format', '{{.ID}}'],
            check=False
        )
        container_id = result.stdout.strip()
        if not container_id:
            return None
        
        # 单次 inspect 获取 networkID 与 container IP
        result = CommandExecutor.run(
            ['docker', 'inspect', container_id, '--format',
             '{{range .NetworkSettings.Networks}}{{.NetworkID}}|{{.IPAddress}}{{end}}'],
            check=False
        )
        if result.returncode != 0 or '|' not in result.stdout:
            return None
        network_id, container_ip = [s.strip() for s in result.stdout.strip().split('|', 1)]
        
        # 单次 network inspect 获取 bridge name 与 subnet
        fmt = '{{index .Options "com.docker.network.bridge.name"}}|{{range .IPAM.Config}}{{.Subnet}}{{end}}'
        result = CommandExecutor.run(
            ['docker', 'network', 'inspect', network_id, '--format', fmt],
            check=False
        )
        if result.returncode != 0 or '|' not in result.stdout:
            return None
        bridge_name, subnet = [s.strip() for s in result.stdout.strip().split('|', 1)]
        if not bridge_name or bridge_name == '<no value>':
            bridge_name = f"br-{network_id[:12]}"
        
        service_cidr = NetworkInfoProvider._get_service_cidr_fast()
        
        return MinikubeInfo(
            bridge_name=bridge_name,
            container_ip=container_ip,
            subnet=subnet,
            service_cidr=service_cidr
        )
    
    @staticmethod
    def get_minikube_dns_ip() -> Optional[str]:
        """获取 Minikube DNS 服务 IP"""
        if not CommandExecutor.command_exists('kubectl'):
            return None
        
        for svc_name in ['kube-dns', 'coredns']:
            result = CommandExecutor.run(
                ['kubectl', 'get', 'svc', '-n', 'kube-system', svc_name,
                 '-o', 'jsonpath={.spec.clusterIP}'],
                check=False
            )
            dns_ip = result.stdout.strip()
            if dns_ip:
                return dns_ip
        
        return None
    
    @staticmethod
    def _get_service_cidr_fast() -> Optional[str]:
        """快速获取 Kubernetes Service CIDR"""
        if not CommandExecutor.command_exists('kubectl'):
            return None
        
        strategies = [
            {
                'name': 'API Server Pod',
                'cmd': ['kubectl', 'get', 'pod', '-n', 'kube-system',
                       '-l', 'component=kube-apiserver',
                       '-o', 'jsonpath={.items[0].spec.containers[0].command}'],
                'pattern': r'service-cluster-ip-range=([0-9./]+)'
            },
            {
                'name': 'kubeadm-config',
                'cmd': ['kubectl', 'get', 'cm', '-n', 'kube-system', 'kubeadm-config',
                       '-o', 'jsonpath={.data.ClusterConfiguration}'],
                'pattern': r'serviceSubnet:\s*([0-9./]+)'
            },
            {
                'name': 'kube-proxy',
                'cmd': ['kubectl', 'get', 'cm', '-n', 'kube-system', 'kube-proxy',
                       '-o', 'jsonpath={.data.config\\.conf}'],
                'pattern': r'clusterCIDR:\s*"?([0-9./]+)"?'
            }
        ]
        
        for strategy in strategies:
            result = CommandExecutor.run(strategy['cmd'], check=False)
            if result.returncode == 0:
                match = re.search(strategy['pattern'], result.stdout)
                if match:
                    cidr = match.group(1)
                    Logger.debug(f"通过 {strategy['name']} 获取 Service CIDR: {cidr}")
                    return cidr
        
        result = CommandExecutor.run(
            ['kubectl', 'get', 'svc', '-n', 'default', 'kubernetes',
             '-o', 'jsonpath={.spec.clusterIP}'],
            check=False
        )
        if result.returncode == 0:
            service_ip = result.stdout.strip()
            if service_ip and re.match(r'^\d+\.\d+\.\d+\.\d+$', service_ip):
                cidr = '.'.join(service_ip.split('.')[:2]) + '.0.0/16'
                Logger.debug(f"通过 kubernetes service 推断 Service CIDR: {cidr}")
                return cidr
        
        return None
    
    @staticmethod
    @lru_cache(maxsize=128)
    def _interface_exists(interface: str) -> bool:
        """检查网络接口是否存在（带缓存）"""
        result = CommandExecutor.run(['ip', 'link', 'show', interface], check=False)
        return result.returncode == 0

# ==================== 路由管理 ====================

class RouteManager:
    """路由管理器"""
    
    @staticmethod
    def route_exists(network: str, gateway: str) -> bool:
        """检查路由是否存在"""
        result = CommandExecutor.run(['ip', 'route', 'show'])
        pattern = f"^{re.escape(network)} via {re.escape(gateway)}"
        return bool(re.search(pattern, result.stdout, re.MULTILINE))
    
    @staticmethod
    def add_route(network: str, gateway: str) -> bool:
        """添加路由（如果不存在）"""
        if RouteManager.route_exists(network, gateway):
            Logger.debug(f"路由已存在: {network} via {gateway}")
            return False
        
        CommandExecutor.run_sudo(['ip', 'route', 'add', network, 'via', gateway])
        Logger.info(f"已添加路由: {network} via {gateway}")
        return True

# ==================== 网络配置器 ====================

class NetworkConfigurator:
    """网络配置器"""
    
    def __init__(self, cached_info: Optional[Dict] = None):
        self.info_provider = NetworkInfoProvider()
        self.iptables = IptablesManager(batch_mode=True)
        self.route_manager = RouteManager()
        self._cache = cached_info or {}
        self._physical_if = None
        self._bridges = None
        self._minikube_info = None
    
    @property
    def physical_interface(self) -> Optional[str]:
        if self._physical_if is None:
            self._physical_if = self._cache.get('physical_if') or \
                               self.info_provider.get_physical_interface()
        return self._physical_if
    
    @property
    def bridges(self) -> List[BridgeInfo]:
        if self._bridges is None:
            self._bridges = self._cache.get('bridges') or \
                           self.info_provider.get_docker_bridges()
        return self._bridges
    
    @property
    def minikube_info(self) -> Optional[MinikubeInfo]:
        if self._minikube_info is None:
            self._minikube_info = self._cache.get('minikube_info') or \
                                 self.info_provider.get_minikube_info()
        return self._minikube_info
    
    def _configure_forwarding(self, title: str, rules_generator):
        """通用的转发配置方法"""
        Logger.section(title)
        
        for rule_info in rules_generator():
            self.iptables.add_rule(**rule_info)
        
        stats = self.iptables.commit()
        if stats['added'] > 0:
            Logger.info(f"✅ 批量添加了 {stats['added']} 条规则")
    
    def configure_docker_bridges_nat(self):
        """配置 Docker 网桥访问外网"""
        physical_if = self.physical_interface
        if not physical_if:
            Logger.error("无法获取物理网卡名称")
            return
        
        Logger.info(f"物理网卡: {physical_if}")
        bridges = self.bridges
        if not bridges:
            Logger.warn("未找到 Docker 网桥")
            return
        
        def rules():
            for bridge in bridges:
                Logger.info(f"配置网桥: {bridge.name} (子网: {bridge.subnet})")
                yield {'table': 'filter', 'chain': 'FORWARD',
                       'rule': ['-i', bridge.name, '-o', physical_if, '-j', 'ACCEPT']}
                yield {'table': 'filter', 'chain': 'FORWARD',
                       'rule': ['-i', physical_if, '-o', bridge.name,
                               '-m', 'state', '--state', 'RELATED,ESTABLISHED', '-j', 'ACCEPT']}
                yield {'table': 'nat', 'chain': 'POSTROUTING',
                       'rule': ['-s', bridge.subnet, '-o', physical_if, '-j', 'MASQUERADE']}
        
        self._configure_forwarding("1. 配置 Docker 网桥访问外网", rules)
    
    def configure_tun0_to_bridges(self):
        """配置 tun0 到所有 Docker 网桥的转发规则"""
        if not self.info_provider._interface_exists('tun0'):
            Logger.warn("tun0 设备不存在，跳过配置")
            return
        
        Logger.info("tun0 设备已找到")
        bridges = self.bridges
        if not bridges:
            Logger.warn("未找到 Docker 网桥")
            return
        
        def rules():
            for bridge in bridges:
                Logger.info(f"配置 tun0 与网桥 {bridge.name} ({bridge.subnet}) 的转发规则")
                yield {'table': 'filter', 'chain': 'FORWARD',
                       'rule': ['-i', 'tun0', '-o', bridge.name, '-j', 'ACCEPT']}
                yield {'table': 'filter', 'chain': 'FORWARD',
                       'rule': ['-i', bridge.name, '-o', 'tun0',
                               '-m', 'state', '--state', 'RELATED,ESTABLISHED', '-j', 'ACCEPT']}
        
        self._configure_forwarding("2. 配置 tun0 到所有 Docker 网桥的转发规则", rules)
    
    def configure_minikube_routes(self):
        """配置 Minikube 集群子网路由"""
        Logger.section("3. 配置 Minikube 集群子网路由")
        
        minikube_info = self.minikube_info
        if not minikube_info:
            Logger.warn("未找到运行中的 Minikube 容器，跳过配置")
            return
        
        Logger.info(f"Minikube 容器 IP: {minikube_info.container_ip}")
        
        if minikube_info.service_cidr:
            Logger.info(f"Kubernetes Service CIDR: {minikube_info.service_cidr}")
            self.route_manager.add_route(minikube_info.service_cidr, minikube_info.container_ip)
            Logger.info("✅ Service 网络路由配置完成")
        else:
            Logger.warn("无法获取 Kubernetes Service CIDR，跳过路由配置")
    
    def configure_minikube_dns(self):
        """配置 Minikube DNS"""
        Logger.section("4. 配置 Minikube DNS")
        
        minikube_info = self.minikube_info
        if not minikube_info:
            Logger.warn("未找到运行中的 Minikube 容器，跳过 DNS 配置")
            return
        
        dns_ip = self.info_provider.get_minikube_dns_ip()
        if not dns_ip:
            Logger.warn("无法获取 Minikube DNS 服务 IP，跳过 DNS 配置")
            Logger.info("提示: 请确保 kubectl 已配置并可以访问 Minikube 集群")
            return
        
        Logger.info(f"Minikube DNS 服务 IP: {dns_ip}")
        
        if not CommandExecutor.command_exists('systemctl'):
            Logger.warn("systemctl 命令不可用，跳过 DNS 配置")
            return
        
        result = CommandExecutor.run(['systemctl', 'is-active', 'systemd-resolved'], check=False)
        if result.returncode != 0:
            Logger.warn("systemd-resolved 服务未运行，跳过 DNS 配置")
            return
        
        conf_dir = Config.DNS_CONF_DIR
        conf_file = Config.DNS_CONF_FILE
        
        Logger.info(f"创建 DNS 配置目录: {conf_dir}")
        conf_dir.mkdir(parents=True, exist_ok=True)
        
        needs_update = True
        if conf_file.exists():
            with open(conf_file, 'r') as f:
                if f"DNS={dns_ip}" in f.read():
                    Logger.debug(f"DNS 配置已存在且正确: {dns_ip}")
                    needs_update = False
        
        if needs_update:
            Logger.info(f"写入 DNS 配置文件: {conf_file}")
            
            config_content = f"""# Minikube DNS 配置
# 自动生成于: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}
[Resolve]
DNS={dns_ip}
Domains=cluster.local
"""
            
            with open(conf_file, 'w') as f:
                f.write(config_content)
            
            CommandExecutor.run_sudo(['chown', 'root:root', str(conf_file)])
            CommandExecutor.run_sudo(['chmod', '644', str(conf_file)])
            Logger.info("✅ DNS 配置文件已创建")
            
            Logger.info("重启 systemd-resolved 服务...")
            CommandExecutor.run_sudo(['systemctl', 'restart', 'systemd-resolved'])
            Logger.info("✅ systemd-resolved 服务已重启")
            
            time.sleep(1)
            result = CommandExecutor.run(['systemctl', 'is-active', 'systemd-resolved'], check=False)
            if result.returncode == 0:
                Logger.info("✅ DNS 配置已生效")
                
                # 验证 DNS 解析
                Logger.info("验证 Kubernetes DNS 解析...")
                self._verify_dns_resolution(dns_ip)
            else:
                Logger.error("systemd-resolved 服务启动失败")
        else:
            Logger.info("✅ DNS 配置无需更新")
            
            # 即使配置未更新，也验证 DNS 解析
            Logger.info("验证 Kubernetes DNS 解析...")
            self._verify_dns_resolution(dns_ip)
    
    def _verify_dns_resolution(self, dns_ip: str):
        """验证 DNS 解析"""
        if not CommandExecutor.command_exists('nslookup'):
            Logger.debug("nslookup 命令不可用，跳过 DNS 解析验证")
            return
        
        try:
            result = CommandExecutor.run(
                ['nslookup', 'kubernetes.default.svc.cluster.local', dns_ip],
                check=False
            )
            if result.returncode == 0:
                Logger.info("✅ DNS 解析验证成功: kubernetes.default.svc.cluster.local")
            else:
                Logger.warn("⚠️  DNS 解析验证失败，可能需要等待 DNS 服务完全启动")
        except Exception as e:
            Logger.warn(f"⚠️  DNS 解析验证失败: {e}")
    
    def configure_bridges_to_minikube(self):
        """配置其他 Docker 网桥与 Minikube 的通信"""
        minikube_info = self.minikube_info
        if not minikube_info:
            Logger.warn("未找到运行中的 Minikube 容器，跳过配置")
            return
        
        Logger.info(f"Minikube 网桥: {minikube_info.bridge_name}")
        bridges = self.bridges
        if not bridges:
            Logger.warn("未找到其他 Docker 网桥")
            return
        
        def rules():
            for bridge in bridges:
                if bridge.name == minikube_info.bridge_name:
                    continue
                Logger.info(f"配置网桥 {bridge.name} 与 Minikube 的通信")
                yield {'table': 'filter', 'chain': 'FORWARD',
                       'rule': ['-i', bridge.name, '-o', minikube_info.bridge_name, '-j', 'ACCEPT']}
                yield {'table': 'filter', 'chain': 'FORWARD',
                       'rule': ['-i', minikube_info.bridge_name, '-o', bridge.name,
                               '-m', 'state', '--state', 'RELATED,ESTABLISHED', '-j', 'ACCEPT']}
        
        self._configure_forwarding("5. 配置 Docker 网桥与 Minikube 的通信", rules)
    
    def configure_bridge_internal_communication(self):
        """配置非 Minikube 的 Docker 网桥子网内通信"""
        minikube_info = self.minikube_info
        minikube_bridge = minikube_info.bridge_name if minikube_info else None
        bridges = self.bridges
        
        if not bridges:
            Logger.warn("未找到 Docker 网桥")
            return
        
        def rules():
            for bridge in bridges:
                if minikube_bridge and bridge.name == minikube_bridge:
                    Logger.debug(f"跳过 Minikube 网桥: {bridge.name}")
                    continue
                
                Logger.info(f"配置网桥 {bridge.name} ({bridge.subnet}) 子网内通信")
                yield {'table': 'filter', 'chain': 'FORWARD',
                       'rule': ['-i', bridge.name, '-o', bridge.name, '-j', 'ACCEPT']}
                yield {'table': 'filter', 'chain': 'FORWARD',
                       'rule': ['-i', bridge.name, '-m', 'state', '--state', 'RELATED,ESTABLISHED', '-j', 'ACCEPT']}
                yield {'table': 'filter', 'chain': 'FORWARD',
                       'rule': ['-o', bridge.name, '-m', 'state', '--state', 'RELATED,ESTABLISHED', '-j', 'ACCEPT']}
        
        self._configure_forwarding("6. 配置 Docker 网桥子网内通信", rules)
    
    def cleanup_invalid_rules(self):
        """清理无效的网桥规则"""
        Logger.info("开始清理无效的网桥规则...")
        
        result = CommandExecutor.run(['ip', 'link', 'show'])
        existing_bridges = set(re.findall(r'br-[a-f0-9]+', result.stdout))
        
        for table, chain in [('filter', 'FORWARD'), ('nat', 'POSTROUTING')]:
            rules = IptablesManager.list_rules(table, chain)
            for rule in rules:
                bridges_in_rule = re.findall(r'br-[a-f0-9]+', rule)
                for bridge in bridges_in_rule:
                    if bridge not in existing_bridges:
                        Logger.warn(f"发现无效网桥规则: {rule}")
        
        Logger.info("清理检查完成")

# ==================== 拓扑图生成器 ====================

class TopologyGenerator:
    """网络拓扑图生成器"""
    
    def __init__(self):
        self.info_provider = NetworkInfoProvider()
    
    def _check_internal_communication(self, bridge_name: str) -> bool:
        """检查网桥子网内通信是否已配置"""
        rules_to_check = [
            ['-i', bridge_name, '-o', bridge_name, '-j', 'ACCEPT'],
            ['-i', bridge_name, '-m', 'state', '--state', 'RELATED,ESTABLISHED', '-j', 'ACCEPT'],
            ['-o', bridge_name, '-m', 'state', '--state', 'RELATED,ESTABLISHED', '-j', 'ACCEPT']
        ]
        
        return all(IptablesManager.rule_exists('filter', 'FORWARD', rule) for rule in rules_to_check)
    
    def generate(self):
        """生成网络拓扑图"""
        Logger.section("7. 网络拓扑图")
        
        print("┌─────────────────────────────────────────────────────────────┐")
        print("│                    网络转发拓扑图                            │")
        print("└─────────────────────────────────────────────────────────────┘\n")
        
        physical_if = self.info_provider.get_physical_interface()
        minikube_info = self.info_provider.get_minikube_info()
        
        print(f"📡 物理网卡: {physical_if}")
        print("🔧 TUN 设备: tun0\n")
        
        print("🐳 Docker 网桥:")
        bridges = self.info_provider.get_docker_bridges()
        
        for bridge in bridges:
            is_minikube = minikube_info and bridge.name == minikube_info.bridge_name
            internal_comm = self._check_internal_communication(bridge.name)
            
            if is_minikube:
                print(f"  ├─ {bridge.name} ({bridge.subnet}) [Minikube]")
            else:
                status = " ✓子网内通信" if internal_comm else ""
                print(f"  ├─ {bridge.name} ({bridge.subnet}){status}")
            
            if not is_minikube and internal_comm:
                print(f"  │   ├─> {bridge.name} (子网内通信)")
            
            print(f"  │   ├─> {physical_if} (外网)")
            
            if minikube_info and bridge.name != minikube_info.bridge_name:
                print(f"  │   ├─> {minikube_info.bridge_name} (Minikube)")
            
            if self.info_provider._interface_exists('tun0'):
                print("  │   └─> tun0 (宿主机)")
        
        print()
        
        if minikube_info and minikube_info.service_cidr:
            print("🛣️  Minikube 路由:")
            print(f"  └─ Service CIDR: {minikube_info.service_cidr} via {minikube_info.container_ip}\n")
        
        if Config.DNS_CONF_FILE.exists():
            print("🌐 DNS 配置:")
            with open(Config.DNS_CONF_FILE, 'r') as f:
                content = f.read()
                dns_match = re.search(r'^DNS=(.+)$', content, re.MULTILINE)
                domains_match = re.search(r'^Domains=(.+)$', content, re.MULTILINE)
                
                if dns_match:
                    print(f"  ├─ DNS 服务器: {dns_match.group(1)}")
                if domains_match:
                    print(f"  └─ 搜索域: {domains_match.group(1)}")
            print()
        
        print("┌─────────────────────────────────────────────────────────────┐")
        print("│                    转发规则统计                              │")
        print("└─────────────────────────────────────────────────────────────┘\n")
        
        forward_rules = IptablesManager.list_rules('filter', 'FORWARD')
        nat_rules = IptablesManager.list_rules('nat', 'POSTROUTING')
        
        result = CommandExecutor.run(['ip', 'route'])
        route_count = len([line for line in result.stdout.split('\n') if 'via' in line])
        
        print(f"📊 FORWARD 规则数: {len(forward_rules)}")
        print(f"📊 NAT 规则数: {len(nat_rules)}")
        print(f"📊 路由条目数: {route_count}\n")

# ==================== 主程序 ====================

class DockerNetworkSetup:
    """Docker 网络配置主程序"""
    
    def __init__(self, verbose: bool = False):
        self.verbose = verbose
        self._network_cache = self._collect_network_info()
        self.configurator = NetworkConfigurator(self._network_cache)
        self.topology_generator = TopologyGenerator()
    
    def _collect_network_info(self) -> Dict:
        """一次性收集所有网络信息并缓存"""
        Logger.debug("正在收集网络信息...")
        info_provider = NetworkInfoProvider()
        
        cache = {
            'physical_if': info_provider.get_physical_interface(),
            'bridges': info_provider.get_docker_bridges(),
            'minikube_info': info_provider.get_minikube_info()
        }
        
        Logger.debug(f"网络信息收集完成: {len(cache['bridges'])} 个网桥")
        return cache
    
    def run(self):
        """运行配置"""
        Logger.section("Lima Docker 虚拟机网络配置脚本")
        
        self._check_commands()
        self._check_permissions()
        
        DockerConfigManager.check_and_fix_iptables_config()
        HostnameConfigManager.check_and_configure_hostname()
        self._enable_ip_forward()
        
        # 执行配置
        self.configurator.configure_docker_bridges_nat()
        self.configurator.configure_tun0_to_bridges()
        self.configurator.configure_minikube_routes()
        self.configurator.configure_minikube_dns()
        self.configurator.configure_bridges_to_minikube()
        self.configurator.configure_bridge_internal_communication()
        
        self.configurator.cleanup_invalid_rules()
        print()
        
        self.topology_generator.generate()
        
        if self.verbose:
            self._show_detailed_rules()
        
        Logger.section("✅ 网络配置完成！")
        Logger.info(f"提示: 使用 '{sys.argv[0]} -v' 查看详细规则列表")
    
    def _check_commands(self):
        """检查必要命令"""
        for cmd in Config.REQUIRED_COMMANDS:
            if not CommandExecutor.command_exists(cmd):
                Logger.error(f"命令 {cmd} 未找到，请先安装")
                sys.exit(1)
    
    def _check_permissions(self):
        """检查权限"""
        if os.geteuid() != 0:
            result = subprocess.run(['sudo', '-n', 'true'], capture_output=True)
            if result.returncode != 0:
                Logger.error("此脚本需要 root 权限或 sudo 权限")
                sys.exit(1)
    
    def _enable_ip_forward(self):
        """启用 IP 转发"""
        result = CommandExecutor.run(['sysctl', '-n', 'net.ipv4.ip_forward'])
        if result.stdout.strip() != '1':
            Logger.info("启用 IP 转发...")
            CommandExecutor.run_sudo(['sysctl', '-w', 'net.ipv4.ip_forward=1'], capture_output=False)
            Logger.info("✅ IP 转发已启用\n")
    
    def _show_detailed_rules(self):
        """显示详细规则"""
        Logger.section("详细规则列表")
        
        print("\n🔍 FORWARD 链规则:")
        CommandExecutor.run_sudo(['iptables', '-L', 'FORWARD', '-n', '-v', '--line-numbers'],
                                capture_output=False)
        
        print("\n🔍 NAT POSTROUTING 链规则:")
        CommandExecutor.run_sudo(['iptables', '-t', 'nat', '-L', 'POSTROUTING', '-n', '-v', '--line-numbers'],
                                capture_output=False)
        
        print("\n🔍 路由表:")
        CommandExecutor.run(['ip', 'route'], capture_output=False)
        print()

def main():
    """主函数"""
    import argparse
    
    parser = argparse.ArgumentParser(
        description='Lima Docker 虚拟机网络配置脚本',
        formatter_class=argparse.RawDescriptionHelpFormatter
    )
    parser.add_argument('-v', '--verbose', action='store_true',
                       help='显示详细规则列表')
    
    args = parser.parse_args()
    
    try:
        setup = DockerNetworkSetup(verbose=args.verbose)
        setup.run()
    except KeyboardInterrupt:
        print()
        Logger.warn("用户中断操作")
        sys.exit(130)
    except Exception as e:
        Logger.error(f"执行失败: {e}")
        if args.verbose:
            import traceback
            traceback.print_exc()
        sys.exit(1)

if __name__ == '__main__':
    main()