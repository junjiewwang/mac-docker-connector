#!/usr/bin/env python3
"""
Lima Docker 虚拟机网络配置脚本 (Zone + Link 模型)

四个域 (Zone): Host / Internet / Kubernetes / Docker
五条链路 (Link): internet / host-docker / host-k8s / docker-k8s / docker-docker

功能：配置Docker网桥、Minikube集群路由、DNS解析和网络转发规则
"""

import json
import os
import re
import subprocess
import sys
import time
from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from datetime import datetime
from pathlib import Path
from typing import List, Optional, Dict, Tuple, Set
from functools import lru_cache

# ==================== 配置常量 ====================

class Config:
    """配置常量"""
    DAEMON_JSON_PATH = Path("/etc/docker/daemon.json")
    DNS_CONF_DIR = Path("/etc/systemd/resolved.conf.d")
    DNS_CONF_FILE = DNS_CONF_DIR / "minikube-dns.conf"
    HOSTS_FILE = Path("/etc/hosts")
    REQUIRED_COMMANDS = ['docker', 'iptables', 'ip']

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
    pod_cidr: Optional[str] = None

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
        self.rules_to_remove = []
        self._cache = {}
    
    @staticmethod
    def rule_exists(table: str, chain: str, rule: List[str]) -> bool:
        """检查规则是否存在"""
        cmd = ['sudo', 'iptables']
        if table != 'filter':
            cmd.extend(['-t', table])
        cmd.extend(['-C', chain] + rule)
        return subprocess.run(cmd, capture_output=True).returncode == 0
    
    def remove_rule(self, table: str, chain: str, rule: List[str]):
        """删除规则（支持批量模式）"""
        if self.batch_mode:
            self.rules_to_remove.append({'table': table, 'chain': chain, 'rule': rule})
        else:
            self._execute_remove_rule(table, chain, rule)
    
    def _execute_remove_rule(self, table: str, chain: str, rule: List[str]) -> bool:
        """立即执行删除单条规则"""
        if not self.rule_exists(table, chain, rule):
            Logger.debug(f"规则不存在，无需删除: iptables -t {table} -D {chain} {' '.join(rule)}")
            return False
        
        cmd = ['sudo', 'iptables']
        if table != 'filter':
            cmd.extend(['-t', table])
        cmd.extend(['-D', chain] + rule)
        
        CommandExecutor.run(cmd)
        Logger.info(f"已删除规则: iptables -t {table} -D {chain} {' '.join(rule)}")
        return True
    
    def commit_remove(self) -> Dict[str, int]:
        """批量提交删除规则"""
        if not self.rules_to_remove:
            return {'removed': 0, 'skipped': 0}
        
        Logger.debug(f"开始批量删除 {len(self.rules_to_remove)} 条规则...")
        removed, skipped = 0, 0
        
        for rule_info in self.rules_to_remove:
            table, chain, rule = rule_info['table'], rule_info['chain'], rule_info['rule']
            if self._execute_remove_rule(table, chain, rule):
                removed += 1
            else:
                skipped += 1
        
        self.rules_to_remove = []
        Logger.debug(f"批量删除完成: 删除 {removed} 条，跳过 {skipped} 条")
        return {'removed': removed, 'skipped': skipped}
    
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
        pod_cidr = NetworkInfoProvider._get_pod_cidr_fast()
        
        return MinikubeInfo(
            bridge_name=bridge_name,
            container_ip=container_ip,
            subnet=subnet,
            service_cidr=service_cidr,
            pod_cidr=pod_cidr
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
    def _get_pod_cidr_fast() -> Optional[str]:
        """快速获取 Kubernetes Pod CIDR
        
        注意：优先获取集群级 cluster-cidr（覆盖整个集群），
        而非 Node 级 spec.podCIDR（仅分配给单个节点的子网，范围较小）
        """
        if not CommandExecutor.command_exists('kubectl'):
            return None
        
        strategies = [
            {
                'name': 'kube-controller-manager --cluster-cidr（集群级，覆盖最全）',
                'cmd': ['kubectl', 'get', 'pod', '-n', 'kube-system',
                       '-l', 'component=kube-controller-manager',
                       '-o', 'jsonpath={.items[0].spec.containers[0].command}'],
                'pattern': r'cluster-cidr=([0-9./]+)'
            },
            {
                'name': 'kubeadm-config podSubnet（集群级）',
                'cmd': ['kubectl', 'get', 'cm', '-n', 'kube-system', 'kubeadm-config',
                       '-o', 'jsonpath={.data.ClusterConfiguration}'],
                'pattern': r'podSubnet:\s*([0-9./]+)'
            },
            {
                'name': 'kube-proxy clusterCIDR（集群级）',
                'cmd': ['kubectl', 'get', 'cm', '-n', 'kube-system', 'kube-proxy',
                       '-o', 'jsonpath={.data.config\\.conf}'],
                'pattern': r'clusterCIDR:\s*"?([0-9./]+)"?'
            },
            {
                'name': 'Node spec.podCIDR（节点级，范围较小，备用）',
                'cmd': ['kubectl', 'get', 'nodes',
                       '-o', 'jsonpath={.items[0].spec.podCIDR}'],
                'pattern': r'^([0-9./]+)$'
            }
        ]
        
        for strategy in strategies:
            Logger.debug(f"尝试获取 Pod CIDR: {strategy['name']}...")
            result = CommandExecutor.run(strategy['cmd'], check=False)
            if result.returncode == 0 and result.stdout.strip():
                match = re.search(strategy['pattern'], result.stdout.strip())
                if match:
                    cidr = match.group(1)
                    Logger.debug(f"通过 {strategy['name']} 获取 Pod CIDR: {cidr}")
                    return cidr
        
        Logger.debug("无法获取 Pod CIDR")
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
    
    @staticmethod
    def remove_route(network: str, gateway: str) -> bool:
        """删除路由（如果存在）"""
        if not RouteManager.route_exists(network, gateway):
            Logger.debug(f"路由不存在，无需删除: {network} via {gateway}")
            return False
        
        CommandExecutor.run_sudo(['ip', 'route', 'del', network, 'via', gateway])
        Logger.info(f"已删除路由: {network} via {gateway}")
        return True

# ==================== DNS 管理器 ====================

class DnsManager:
    """DNS 配置管理器（从原 NetworkConfigurator 提取）"""

    @staticmethod
    def configure_dns(minikube_info) -> bool:
        """配置 Minikube DNS"""
        if not minikube_info:
            Logger.warn("未找到运行中的 Minikube 容器，跳过 DNS 配置")
            return False

        dns_ip = NetworkInfoProvider.get_minikube_dns_ip()
        if not dns_ip:
            Logger.warn("无法获取 Minikube DNS 服务 IP，跳过 DNS 配置")
            return False

        Logger.info(f"Minikube DNS 服务 IP: {dns_ip}")

        if not CommandExecutor.command_exists('systemctl'):
            Logger.warn("systemctl 命令不可用，跳过 DNS 配置")
            return False

        result = CommandExecutor.run(['systemctl', 'is-active', 'systemd-resolved'], check=False)
        if result.returncode != 0:
            Logger.warn("systemd-resolved 服务未运行，跳过 DNS 配置")
            return False

        conf_dir = Config.DNS_CONF_DIR
        conf_file = Config.DNS_CONF_FILE

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
                DnsManager.verify_dns(dns_ip)
            else:
                Logger.error("systemd-resolved 服务启动失败")
                return False
        else:
            Logger.info("✅ DNS 配置无需更新")
            DnsManager.verify_dns(dns_ip)

        return True

    @staticmethod
    def revert_dns():
        """还原 DNS 配置"""
        conf_file = Config.DNS_CONF_FILE
        if conf_file.exists():
            CommandExecutor.run_sudo(['rm', '-f', str(conf_file)])
            Logger.info(f"已删除 DNS 配置文件: {conf_file}")

            if CommandExecutor.command_exists('systemctl'):
                result = CommandExecutor.run(['systemctl', 'is-active', 'systemd-resolved'], check=False)
                if result.returncode == 0:
                    Logger.info("重启 systemd-resolved 服务...")
                    CommandExecutor.run_sudo(['systemctl', 'restart', 'systemd-resolved'])
                    Logger.info("✅ systemd-resolved 服务已重启")
        else:
            Logger.debug("DNS 配置文件不存在，无需还原")

    @staticmethod
    def verify_dns(dns_ip: str):
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

    @staticmethod
    def check_dns_status() -> Tuple[bool, Optional[str], Optional[str]]:
        """检查 DNS 状态，返回 (配置存在, dns_ip, 搜索域)"""
        conf_file = Config.DNS_CONF_FILE
        if not conf_file.exists():
            return False, None, None

        with open(conf_file, 'r') as f:
            content = f.read()
            dns_match = re.search(r'^DNS=(.+)$', content, re.MULTILINE)
            domains_match = re.search(r'^Domains=(.+)$', content, re.MULTILINE)
            dns_ip = dns_match.group(1) if dns_match else None
            domains = domains_match.group(1) if domains_match else None
            return True, dns_ip, domains


# ==================== Link 抽象基类 ====================

@dataclass
class LinkStatus:
    """链路状态"""
    name: str
    description: str
    total: int = 0
    active: int = 0
    details: List[Tuple[str, bool]] = field(default_factory=list)

    @property
    def is_ok(self) -> bool:
        return self.total > 0 and self.active == self.total

    @property
    def mark(self) -> str:
        GREEN = '\033[0;32m'
        RED = '\033[0;31m'
        NC = '\033[0m'
        if self.is_ok:
            return f"{GREEN}✅ {self.active}/{self.total}{NC}"
        return f"{RED}❌ {self.active}/{self.total}{NC}"


class AbstractLink(ABC):
    """链路抽象基类

    每条链路代表两个域(Zone)之间的网络连通性。
    子类需实现 apply / revert / status 三个方法。
    """

    # 链路名称，如 'internet', 'host-docker'
    name: str = ""
    # 链路描述
    description: str = ""
    # 子层级列表，如 ['service', 'pod']，空列表表示无子层级
    sub_levels: List[str] = []

    def __init__(self, iptables: IptablesManager, route_mgr: RouteManager,
                 info_provider: NetworkInfoProvider, cache: Dict):
        self.iptables = iptables
        self.route_mgr = route_mgr
        self.info_provider = info_provider
        self._cache = cache

    @property
    def physical_interface(self) -> Optional[str]:
        return self._cache.get('physical_if')

    @property
    def bridges(self) -> List[BridgeInfo]:
        return self._cache.get('bridges', [])

    @property
    def minikube_info(self) -> Optional[MinikubeInfo]:
        return self._cache.get('minikube_info')

    def non_mk_bridges(self) -> List[BridgeInfo]:
        """获取非 Minikube 的网桥列表"""
        mk_info = self.minikube_info
        mk_bridge = mk_info.bridge_name if mk_info else None
        return [b for b in self.bridges if b.name != mk_bridge]

    @abstractmethod
    def apply(self, sub_level: Optional[str] = None):
        """应用链路配置"""
        pass

    @abstractmethod
    def revert(self, sub_level: Optional[str] = None):
        """还原链路配置"""
        pass

    @abstractmethod
    def status(self, sub_level: Optional[str] = None) -> List[LinkStatus]:
        """获取链路状态"""
        pass

    def _batch_add_rules(self, title: str, rules_generator):
        """批量添加 iptables 规则"""
        Logger.section(title)
        for rule_info in rules_generator():
            self.iptables.add_rule(**rule_info)
        stats = self.iptables.commit()
        if stats['added'] > 0:
            Logger.info(f"✅ 添加了 {stats['added']} 条规则")
        else:
            Logger.info("✅ 所有规则已存在")

    def _batch_remove_rules(self, title: str, rules_generator):
        """批量删除 iptables 规则"""
        Logger.section(title)
        for rule_info in rules_generator():
            self.iptables.remove_rule(**rule_info)
        stats = self.iptables.commit_remove()
        if stats['removed'] > 0:
            Logger.info(f"✅ 删除了 {stats['removed']} 条规则")
        else:
            Logger.info("✅ 无需删除任何规则")

    def _check_rule(self, table: str, chain: str, rule: List[str]) -> bool:
        """检查单条规则是否存在"""
        return IptablesManager.rule_exists(table, chain, rule)

    def _rules_to_status(self, rules_gen, st: 'LinkStatus', label_fn=None):
        """从 _rules() 生成器构建 status，避免重复定义规则

        Args:
            rules_gen: 规则生成器（callable）
            st: LinkStatus 对象
            label_fn: 可选的标签生成函数 (rule_info) -> str
        """
        for rule_info in rules_gen():
            table, chain, rule = rule_info['table'], rule_info['chain'], rule_info['rule']
            ok = self._check_rule(table, chain, rule)
            if label_fn:
                label = label_fn(rule_info)
            else:
                label = f"{' '.join(rule[:4])}" if table == 'filter' else f"NAT {' '.join(rule[:4])}"
            st.details.append((label, ok))
            st.total += 1
            st.active += int(ok)


# ==================== InternetLink ====================

class InternetLink(AbstractLink):
    """internet 链路：所有网桥（含 Minikube）↔ 外网

    底层操作：
    - FORWARD bridge→lima0
    - FORWARD lima0→bridge RELATED,ESTABLISHED
    - NAT MASQUERADE
    """
    name = "internet"
    description = "Docker/K8s ↔ Internet"
    sub_levels = []

    def _rules(self):
        physical_if = self.physical_interface
        if not physical_if:
            Logger.error("无法获取物理网卡名称")
            return
        bridges = self.bridges
        for bridge in bridges:
            Logger.info(f"网桥: {bridge.name} (子网: {bridge.subnet})")
            yield {'table': 'filter', 'chain': 'FORWARD',
                   'rule': ['-i', bridge.name, '-o', physical_if, '-j', 'ACCEPT']}
            yield {'table': 'filter', 'chain': 'FORWARD',
                   'rule': ['-i', physical_if, '-o', bridge.name,
                           '-m', 'state', '--state', 'RELATED,ESTABLISHED', '-j', 'ACCEPT']}
            yield {'table': 'nat', 'chain': 'POSTROUTING',
                   'rule': ['-s', bridge.subnet, '-o', physical_if, '-j', 'MASQUERADE']}

    def apply(self, sub_level=None):
        self._batch_add_rules("🌐 internet: 配置所有网桥访问外网", self._rules)

    def revert(self, sub_level=None):
        # internet revert 需要警告
        print("\n⚠️  警告: 还原 internet 链路将导致所有容器和 K8s 集群无法访问外网！")
        try:
            answer = input("确认继续？[y/N] ").strip().lower()
        except EOFError:
            answer = 'n'
        if answer != 'y':
            Logger.info("已取消还原 internet 链路")
            return
        self._batch_remove_rules("🌐 internet: 还原所有网桥外网访问", self._rules)

    def status(self, sub_level=None) -> List[LinkStatus]:
        st = LinkStatus(name="internet", description=self.description)
        self._rules_to_status(self._rules, st)
        return [st]


# ==================== HostDockerLink ====================

class HostDockerLink(AbstractLink):
    """host-docker 链路：宿主机 (tun0) ↔ Docker 容器子网（非 Minikube 网桥）

    底层操作：
    - FORWARD tun0→非mk网桥
    - FORWARD 非mk网桥→tun0 RELATED,ESTABLISHED
    """
    name = "host-docker"
    description = "Host ↔ Docker"
    sub_levels = []

    def _rules(self):
        if not self.info_provider._interface_exists('tun0'):
            Logger.warn("tun0 设备不存在，跳过")
            return
        for bridge in self.non_mk_bridges():
            Logger.info(f"tun0 ↔ {bridge.name} ({bridge.subnet})")
            yield {'table': 'filter', 'chain': 'FORWARD',
                   'rule': ['-i', 'tun0', '-o', bridge.name, '-j', 'ACCEPT']}
            yield {'table': 'filter', 'chain': 'FORWARD',
                   'rule': ['-i', bridge.name, '-o', 'tun0',
                           '-m', 'state', '--state', 'RELATED,ESTABLISHED', '-j', 'ACCEPT']}

    def apply(self, sub_level=None):
        self._batch_add_rules("🖥️ host-docker: 配置宿主机与 Docker 容器通信", self._rules)

    def revert(self, sub_level=None):
        self._batch_remove_rules("🖥️ host-docker: 还原宿主机与 Docker 容器通信", self._rules)

    def status(self, sub_level=None) -> List[LinkStatus]:
        st = LinkStatus(name="host-docker", description=self.description)
        self._rules_to_status(self._rules, st)
        return [st]


# ==================== HostK8sLink ====================

class HostK8sLink(AbstractLink):
    """host-k8s 链路：宿主机 (tun0) ↔ Kubernetes (Minikube)

    子层级:
    - service: route(service_cidr) + FORWARD tun0↔mk_bridge + DNS
    - pod: route(pod_cidr) + FORWARD tun0↔mk_bridge(pod_cidr)
    """
    name = "host-k8s"
    description = "Host ↔ Kubernetes"
    sub_levels = ['service', 'pod']

    def _service_rules(self):
        """Service 子层级的 iptables 规则"""
        mk_info = self.minikube_info
        if not mk_info:
            Logger.warn("未找到运行中的 Minikube 容器")
            return
        if not self.info_provider._interface_exists('tun0'):
            Logger.warn("tun0 设备不存在")
            return
        Logger.info(f"tun0 ↔ {mk_info.bridge_name} (Minikube Service)")
        yield {'table': 'filter', 'chain': 'FORWARD',
               'rule': ['-i', 'tun0', '-o', mk_info.bridge_name, '-j', 'ACCEPT']}
        yield {'table': 'filter', 'chain': 'FORWARD',
               'rule': ['-i', mk_info.bridge_name, '-o', 'tun0',
                       '-m', 'state', '--state', 'RELATED,ESTABLISHED', '-j', 'ACCEPT']}

    def _pod_rules(self):
        """Pod 子层级的 iptables 规则"""
        mk_info = self.minikube_info
        if not mk_info or not mk_info.pod_cidr:
            Logger.warn("未找到 Minikube 或 Pod CIDR")
            return
        if not self.info_provider._interface_exists('tun0'):
            Logger.warn("tun0 设备不存在")
            return
        Logger.info(f"tun0 ↔ {mk_info.bridge_name} (Pod CIDR: {mk_info.pod_cidr})")
        yield {'table': 'filter', 'chain': 'FORWARD',
               'rule': ['-i', 'tun0', '-d', mk_info.pod_cidr, '-o', mk_info.bridge_name, '-j', 'ACCEPT']}
        yield {'table': 'filter', 'chain': 'FORWARD',
               'rule': ['-i', mk_info.bridge_name, '-s', mk_info.pod_cidr, '-o', 'tun0', '-j', 'ACCEPT']}

    def apply(self, sub_level=None):
        mk_info = self.minikube_info
        if not mk_info:
            Logger.warn("未找到运行中的 Minikube 容器，跳过 host-k8s 配置")
            return

        if sub_level in (None, 'service'):
            self._batch_add_rules("🖥️ host-k8s.service: 配置宿主机访问 K8s Service", self._service_rules)
            # Service 路由
            if mk_info.service_cidr:
                Logger.info(f"Service CIDR 路由: {mk_info.service_cidr} via {mk_info.container_ip}")
                self.route_mgr.add_route(mk_info.service_cidr, mk_info.container_ip)
            # DNS 配置
            Logger.info("配置 Kubernetes DNS...")
            DnsManager.configure_dns(mk_info)

        if sub_level in (None, 'pod'):
            self._batch_add_rules("🖥️ host-k8s.pod: 配置宿主机访问 K8s Pod", self._pod_rules)
            # Pod 路由
            if mk_info.pod_cidr:
                Logger.info(f"Pod CIDR 路由: {mk_info.pod_cidr} via {mk_info.container_ip}")
                self.route_mgr.add_route(mk_info.pod_cidr, mk_info.container_ip)

    def revert(self, sub_level=None):
        mk_info = self.minikube_info
        if not mk_info:
            Logger.warn("未找到运行中的 Minikube 容器，跳过还原")
            return

        if sub_level in (None, 'service'):
            self._batch_remove_rules("🖥️ host-k8s.service: 还原宿主机访问 K8s Service", self._service_rules)
            if mk_info.service_cidr:
                self.route_mgr.remove_route(mk_info.service_cidr, mk_info.container_ip)
            DnsManager.revert_dns()

        if sub_level in (None, 'pod'):
            self._batch_remove_rules("🖥️ host-k8s.pod: 还原宿主机访问 K8s Pod", self._pod_rules)
            if mk_info.pod_cidr:
                self.route_mgr.remove_route(mk_info.pod_cidr, mk_info.container_ip)

    def _append_status_check(self, st: LinkStatus, label: str, ok: bool):
        """向 LinkStatus 追加一条检查结果"""
        st.details.append((label, ok))
        st.total += 1
        st.active += int(ok)

    def status(self, sub_level=None) -> List[LinkStatus]:
        results = []
        mk_info = self.minikube_info

        if sub_level in (None, 'service'):
            st = LinkStatus(name="host-k8s.service", description="Host ↔ K8s Service")
            self._rules_to_status(self._service_rules, st)
            # Service 路由
            if mk_info and mk_info.service_cidr:
                ok = RouteManager.route_exists(mk_info.service_cidr, mk_info.container_ip)
                self._append_status_check(st, f"route {mk_info.service_cidr} via {mk_info.container_ip}", ok)
            # DNS
            dns_ok, dns_ip, _ = DnsManager.check_dns_status()
            self._append_status_check(st, f"DNS config ({dns_ip or 'N/A'})", dns_ok)
            results.append(st)

        if sub_level in (None, 'pod'):
            st = LinkStatus(name="host-k8s.pod", description="Host ↔ K8s Pod")
            self._rules_to_status(self._pod_rules, st)
            # Pod 路由
            if mk_info and mk_info.pod_cidr:
                ok = RouteManager.route_exists(mk_info.pod_cidr, mk_info.container_ip)
                self._append_status_check(st, f"route {mk_info.pod_cidr} via {mk_info.container_ip}", ok)
            results.append(st)

        return results


# ==================== DockerK8sLink ====================

class DockerK8sLink(AbstractLink):
    """docker-k8s 链路：Docker 容器子网 ↔ Kubernetes (Minikube)

    子层级:
    - service: FORWARD 非mk网桥↔mk_bridge
    - pod: FORWARD 非mk网桥↔mk_bridge (pod_cidr过滤)
    """
    name = "docker-k8s"
    description = "Docker ↔ Kubernetes"
    sub_levels = ['service', 'pod']

    def _service_rules(self):
        """Service 子层级的 iptables 规则"""
        mk_info = self.minikube_info
        if not mk_info:
            return
        for bridge in self.non_mk_bridges():
            Logger.info(f"{bridge.name} ↔ {mk_info.bridge_name} (Service)")
            yield {'table': 'filter', 'chain': 'FORWARD',
                   'rule': ['-i', bridge.name, '-o', mk_info.bridge_name, '-j', 'ACCEPT']}
            yield {'table': 'filter', 'chain': 'FORWARD',
                   'rule': ['-i', mk_info.bridge_name, '-o', bridge.name,
                           '-m', 'state', '--state', 'RELATED,ESTABLISHED', '-j', 'ACCEPT']}

    def _pod_rules(self):
        """Pod 子层级的 iptables 规则"""
        mk_info = self.minikube_info
        if not mk_info or not mk_info.pod_cidr:
            return
        for bridge in self.non_mk_bridges():
            Logger.info(f"{bridge.name} ↔ {mk_info.bridge_name} (Pod CIDR: {mk_info.pod_cidr})")
            yield {'table': 'filter', 'chain': 'FORWARD',
                   'rule': ['-i', bridge.name, '-d', mk_info.pod_cidr, '-o', mk_info.bridge_name, '-j', 'ACCEPT']}
            yield {'table': 'filter', 'chain': 'FORWARD',
                   'rule': ['-i', mk_info.bridge_name, '-s', mk_info.pod_cidr, '-o', bridge.name, '-j', 'ACCEPT']}

    def apply(self, sub_level=None):
        mk_info = self.minikube_info
        if not mk_info:
            Logger.warn("未找到运行中的 Minikube 容器，跳过 docker-k8s 配置")
            return

        if sub_level in (None, 'service'):
            self._batch_add_rules("🐳 docker-k8s.service: 配置容器访问 K8s Service", self._service_rules)

        if sub_level in (None, 'pod'):
            self._batch_add_rules("🐳 docker-k8s.pod: 配置容器访问 K8s Pod", self._pod_rules)

    def revert(self, sub_level=None):
        mk_info = self.minikube_info
        if not mk_info:
            Logger.warn("未找到运行中的 Minikube 容器，跳过还原")
            return

        if sub_level in (None, 'service'):
            self._batch_remove_rules("🐳 docker-k8s.service: 还原容器访问 K8s Service", self._service_rules)

        if sub_level in (None, 'pod'):
            self._batch_remove_rules("🐳 docker-k8s.pod: 还原容器访问 K8s Pod", self._pod_rules)

    def status(self, sub_level=None) -> List[LinkStatus]:
        results = []

        if sub_level in (None, 'service'):
            st = LinkStatus(name="docker-k8s.service", description="Docker ↔ K8s Service")
            self._rules_to_status(self._service_rules, st)
            results.append(st)

        if sub_level in (None, 'pod'):
            st = LinkStatus(name="docker-k8s.pod", description="Docker ↔ K8s Pod")
            self._rules_to_status(self._pod_rules, st)
            results.append(st)

        return results


# ==================== DockerDockerLink ====================

class DockerDockerLink(AbstractLink):
    """docker-docker 链路：不同 Docker 子网之间的容器互通

    底层操作（对每个非 Minikube 网桥）：
    - FORWARD bridge→bridge 自身
    - FORWARD bridge 出站 RELATED,ESTABLISHED
    - FORWARD bridge 入站 RELATED,ESTABLISHED
    """
    name = "docker-docker"
    description = "Docker ↔ Docker"
    sub_levels = []

    def _rules(self):
        for bridge in self.non_mk_bridges():
            Logger.info(f"网桥 {bridge.name} ({bridge.subnet}) 子网内通信")
            yield {'table': 'filter', 'chain': 'FORWARD',
                   'rule': ['-i', bridge.name, '-o', bridge.name, '-j', 'ACCEPT']}
            yield {'table': 'filter', 'chain': 'FORWARD',
                   'rule': ['-i', bridge.name, '-m', 'state', '--state', 'RELATED,ESTABLISHED', '-j', 'ACCEPT']}
            yield {'table': 'filter', 'chain': 'FORWARD',
                   'rule': ['-o', bridge.name, '-m', 'state', '--state', 'RELATED,ESTABLISHED', '-j', 'ACCEPT']}

    def apply(self, sub_level=None):
        self._batch_add_rules("🐳 docker-docker: 配置 Docker 子网间通信", self._rules)

    def revert(self, sub_level=None):
        self._batch_remove_rules("🐳 docker-docker: 还原 Docker 子网间通信", self._rules)

    def status(self, sub_level=None) -> List[LinkStatus]:
        st = LinkStatus(name="docker-docker", description=self.description)
        self._rules_to_status(self._rules, st)
        return [st]


# ==================== Link 注册表 ====================

# 所有可用链路（按执行顺序）
ALL_LINK_CLASSES = [
    InternetLink,
    HostDockerLink,
    HostK8sLink,
    DockerK8sLink,
    DockerDockerLink,
]

# 链路名称到类的映射
LINK_MAP = {cls.name: cls for cls in ALL_LINK_CLASSES}

# 所有合法的 --only 参数值
ALL_LINK_NAMES: Set[str] = set()
for cls in ALL_LINK_CLASSES:
    ALL_LINK_NAMES.add(cls.name)
    for sub in cls.sub_levels:
        ALL_LINK_NAMES.add(f"{cls.name}.{sub}")


def parse_link_spec(spec: str) -> Tuple[str, Optional[str]]:
    """解析 link 规格字符串，返回 (link_name, sub_level)

    例如：
    - 'internet' -> ('internet', None)
    - 'host-k8s' -> ('host-k8s', None)
    - 'host-k8s.service' -> ('host-k8s', 'service')
    - 'docker-k8s.pod' -> ('docker-k8s', 'pod')
    """
    if '.' in spec:
        link_name, sub_level = spec.split('.', 1)
        return link_name, sub_level
    return spec, None


# ==================== 拓扑图生成器 ====================

class TopologyGenerator:
    """网络拓扑图生成器"""

    def __init__(self, cache: Dict):
        self._cache = cache
        self.info_provider = NetworkInfoProvider()

    @property
    def bridges(self) -> List[BridgeInfo]:
        return self._cache.get('bridges', [])

    @property
    def minikube_info(self) -> Optional[MinikubeInfo]:
        return self._cache.get('minikube_info')

    @property
    def physical_interface(self) -> Optional[str]:
        return self._cache.get('physical_if')

    def generate(self):
        """生成网络拓扑图"""
        print("\n┌─────────────────────────────────────────────────────────────┐")
        print("│                    网络转发拓扑图                            │")
        print("└─────────────────────────────────────────────────────────────┘\n")

        physical_if = self.physical_interface
        mk_info = self.minikube_info

        print(f"📡 物理网卡: {physical_if}")
        print("🔧 TUN 设备: tun0\n")

        print("🐳 Docker 网桥:")
        for bridge in self.bridges:
            is_minikube = mk_info and bridge.name == mk_info.bridge_name

            if is_minikube:
                print(f"  ├─ {bridge.name} ({bridge.subnet}) [Minikube]")
            else:
                print(f"  ├─ {bridge.name} ({bridge.subnet})")

            if not is_minikube:
                print(f"  │   ├─> {bridge.name} (子网内通信)")

            print(f"  │   ├─> {physical_if} (外网)")

            if mk_info and not is_minikube:
                print(f"  │   ├─> {mk_info.bridge_name} (Minikube)")

            if self.info_provider._interface_exists('tun0'):
                print("  │   └─> tun0 (宿主机)")

        print()

        if mk_info and (mk_info.service_cidr or mk_info.pod_cidr):
            print("🛣️  Minikube 路由:")
            if mk_info.service_cidr:
                print(f"  ├─ Service CIDR: {mk_info.service_cidr} via {mk_info.container_ip}")
            if mk_info.pod_cidr:
                print(f"  └─ Pod CIDR: {mk_info.pod_cidr} via {mk_info.container_ip}")
            print()

        dns_ok, dns_ip, domains = DnsManager.check_dns_status()
        if dns_ok:
            print("🌐 DNS 配置:")
            if dns_ip:
                print(f"  ├─ DNS 服务器: {dns_ip}")
            if domains:
                print(f"  └─ 搜索域: {domains}")
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
    """Docker 网络配置主程序 (Zone + Link 模型)"""

    def __init__(self, verbose: bool = False, only_links: Optional[List[str]] = None,
                 skip_links: Optional[List[str]] = None):
        self.verbose = verbose
        self.only_links = only_links  # 包含 'link_name' 或 'link_name.sub' 格式
        self.skip_links = skip_links
        self._network_cache = self._collect_network_info()
        self._links = self._create_links()
        self.topology_generator = TopologyGenerator(self._network_cache)

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

    def _create_links(self) -> Dict[str, AbstractLink]:
        """创建所有链路实例"""
        links = {}
        iptables = IptablesManager(batch_mode=True)
        route_mgr = RouteManager()
        info_provider = NetworkInfoProvider()

        for cls in ALL_LINK_CLASSES:
            link = cls(iptables=iptables, route_mgr=route_mgr,
                      info_provider=info_provider, cache=self._network_cache)
            links[cls.name] = link
        return links

    def _get_link_specs(self) -> List[Tuple[str, Optional[str]]]:
        """解析需要执行的链路列表

        返回 [(link_name, sub_level), ...] 格式
        """
        if self.only_links:
            specs = []
            for spec in self.only_links:
                link_name, sub_level = parse_link_spec(spec)
                specs.append((link_name, sub_level))
            return specs

        # 默认执行所有链路
        return [(cls.name, None) for cls in ALL_LINK_CLASSES]

    def _should_skip(self, link_name: str) -> bool:
        """检查是否应跳过指定链路"""
        if self.skip_links:
            for skip in self.skip_links:
                skip_name, _ = parse_link_spec(skip)
                if skip_name == link_name:
                    return True
        return False

    def do_apply(self):
        """执行 apply（应用配置）"""
        Logger.section("Lima Docker 虚拟机网络配置脚本 [apply]")

        self._check_commands()
        self._check_permissions()

        # 隐式前置检查
        self._run_prechecks()

        specs = self._get_link_specs()
        for link_name, sub_level in specs:
            if self._should_skip(link_name):
                continue
            link = self._links.get(link_name)
            if link:
                link.apply(sub_level=sub_level)

        # 清理无效规则
        self._cleanup_invalid_rules()

        # 拓扑图
        self.topology_generator.generate()

        if self.verbose:
            self._show_detailed_rules()

        Logger.section("✅ 网络配置完成！")
        Logger.info(f"提示: 使用 '{sys.argv[0]} status' 查看配置状态")

    def do_revert(self):
        """执行 revert（还原配置）"""
        Logger.section("Lima Docker 虚拟机网络配置脚本 [revert]")

        self._check_commands()
        self._check_permissions()

        specs = self._get_link_specs()
        # 逆序还原
        for link_name, sub_level in reversed(specs):
            if self._should_skip(link_name):
                continue
            link = self._links.get(link_name)
            if link:
                link.revert(sub_level=sub_level)

        Logger.section(f"✅ 网络配置还原完成！")
        Logger.info(f"提示: 使用 '{sys.argv[0]} status' 确认还原结果")

    def do_status(self):
        """执行 status（查看状态）"""
        Logger.section("Lima Docker 虚拟机网络配置脚本 [status]")

        self._check_commands()
        self._check_permissions()

        GREEN = '\033[0;32m'
        RED = '\033[0;31m'
        CYAN = '\033[0;36m'
        NC = '\033[0m'

        print("\n┌──────────────────────────────────────────────────────┐")
        print("│              Lima Docker 网络连通性状态                 │")
        print("└──────────────────────────────────────────────────────┘\n")

        specs = self._get_link_specs()

        for link_name, sub_level in specs:
            if self._should_skip(link_name):
                continue
            link = self._links.get(link_name)
            if not link:
                continue

            statuses = link.status(sub_level=sub_level)

            # 对于有子层级的链路，先显示汇总行
            if link.sub_levels and sub_level is None:
                # 汇总
                total_all = sum(s.total for s in statuses)
                active_all = sum(s.active for s in statuses)
                if total_all > 0 and active_all == total_all:
                    mark = f"{GREEN}✅ {active_all}/{total_all}{NC}"
                else:
                    mark = f"{RED}❌ {active_all}/{total_all}{NC}"

                icon = self._link_icon(link_name)
                print(f"  {icon} {CYAN}{link_name:<18}{NC} {link.description:<25} {mark}")

                # 子层级
                for i, st in enumerate(statuses):
                    prefix = "└─" if i == len(statuses) - 1 else "├─"
                    sub_name = st.name.split('.')[-1] if '.' in st.name else st.name
                    print(f"    {prefix} {sub_name:<16} {st.description:<25} {st.mark}")
            else:
                for st in statuses:
                    icon = self._link_icon(link_name)
                    print(f"  {icon} {CYAN}{st.name:<18}{NC} {st.description:<25} {st.mark}")

        print()

        # 详细模式显示详情
        if self.verbose:
            print(f"\n  {CYAN}{'详细规则状态:'}{NC}\n")
            for link_name, sub_level in specs:
                if self._should_skip(link_name):
                    continue
                link = self._links.get(link_name)
                if not link:
                    continue
                statuses = link.status(sub_level=sub_level)
                for st in statuses:
                    if st.details:
                        print(f"  📋 {st.name}:")
                        for desc, ok in st.details:
                            mark = f"{GREEN}✅{NC}" if ok else f"{RED}❌{NC}"
                            print(f"    {mark} {desc}")
                        print()

        # 拓扑图
        self.topology_generator.generate()

        if self.verbose:
            self._show_detailed_rules()

    @staticmethod
    def _link_icon(link_name: str) -> str:
        """获取链路图标"""
        icons = {
            'internet': '🌐',
            'host-docker': '🖥️',
            'host-k8s': '🖥️',
            'docker-k8s': '🐳',
            'docker-docker': '🐳',
        }
        return icons.get(link_name, '🔗')

    def _run_prechecks(self):
        """隐式前置检查"""
        DockerConfigManager.check_and_fix_iptables_config()
        HostnameConfigManager.check_and_configure_hostname()
        self._enable_ip_forward()

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

    def _cleanup_invalid_rules(self):
        """清理无效的网桥规则"""
        Logger.info("\n开始清理无效的网桥规则...")

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

    # 构建链路帮助信息
    link_help_lines = []
    for cls in ALL_LINK_CLASSES:
        if cls.sub_levels:
            sub_str = ', '.join(f"{cls.name}.{s}" for s in cls.sub_levels)
            link_help_lines.append(f"  {cls.name:<18} {cls.description} (子层级: {sub_str})")
        else:
            link_help_lines.append(f"  {cls.name:<18} {cls.description}")
    link_help = '\n'.join(link_help_lines)

    epilog = f"""可用链路 (Link):
{link_help}

示例:
  %(prog)s                                        # 应用所有链路
  %(prog)s apply --only internet                   # 仅应用 internet 链路
  %(prog)s apply --only host-k8s                   # 应用 host-k8s (service + pod)
  %(prog)s apply --only host-k8s.service           # 仅应用 host-k8s 的 service 子层级
  %(prog)s apply --only host-k8s.pod               # 仅应用 host-k8s 的 pod 子层级
  %(prog)s apply --only docker-k8s.service         # 仅应用 docker-k8s 的 service 子层级
  %(prog)s apply --only internet,host-docker       # 应用多个链路
  %(prog)s revert                                  # 还原所有链路
  %(prog)s revert --only docker-k8s                # 仅还原 docker-k8s
  %(prog)s status                                  # 查看所有链路状态
  %(prog)s status --only host-k8s                  # 查看 host-k8s 状态
"""

    parser = argparse.ArgumentParser(
        description='Lima Docker 虚拟机网络配置脚本 (Zone + Link 模型)',
        epilog=epilog,
        formatter_class=argparse.RawDescriptionHelpFormatter
    )

    parser.add_argument('subcommand', nargs='?', default='apply',
                       choices=['apply', 'revert', 'status'],
                       help='子命令: apply(默认)/revert/status')
    parser.add_argument('--only', type=str, default=None,
                       help='仅执行指定链路（逗号分隔，支持.子层级，如 host-k8s.service）')
    parser.add_argument('--skip', type=str, default=None,
                       help='跳过指定链路（逗号分隔）')
    parser.add_argument('-v', '--verbose', action='store_true',
                       help='显示详细规则列表')

    args = parser.parse_args()

    # 解析链路列表
    only_links = args.only.split(',') if args.only else None
    skip_links = args.skip.split(',') if args.skip else None

    # 验证链路名
    if only_links:
        for spec in only_links:
            if spec not in ALL_LINK_NAMES:
                Logger.error(f"未知链路: {spec}")
                Logger.info(f"可用链路: {', '.join(sorted(ALL_LINK_NAMES))}")
                sys.exit(1)
    if skip_links:
        for spec in skip_links:
            link_name, _ = parse_link_spec(spec)
            if link_name not in LINK_MAP:
                Logger.error(f"未知链路: {spec}")
                Logger.info(f"可用链路: {', '.join(sorted(ALL_LINK_NAMES))}")
                sys.exit(1)

    try:
        setup = DockerNetworkSetup(
            verbose=args.verbose,
            only_links=only_links,
            skip_links=skip_links
        )

        if args.subcommand == 'apply':
            setup.do_apply()
        elif args.subcommand == 'revert':
            setup.do_revert()
        elif args.subcommand == 'status':
            setup.do_status()

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