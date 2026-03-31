# Lima VM 配置更新优化

## 需求背景

当 Lima VM 中已存在 `/etc/docker-connector/connector.env` 配置文件时，执行 `deploy-to-lima.sh` 部署不会以本地 `deploy/connector.env` 为主更新 VM 中的配置。用户在 macOS 端修改了 `connector.env` 后重新部署，VM 中的配置不会同步更新。

## 问题分析

原有 `install.sh` 的 `install_config()` 逻辑：
- 如果 VM 已有配置文件 → 仅更新命令行 `--addr=` 等显式指定的参数，其余保留旧值
- 如果 VM 无配置文件 → 用默认值创建

`deploy-to-lima.sh` 虽然将 `connector.env` 传输到了 VM 的临时目录，但 `install.sh` 并未使用这个文件，导致本地修改无法生效。

## 解决方案

### 配置优先级

```
命令行参数 > 脚本同目录下的 connector.env > VM 已有配置 > 默认值
```

### 流程图

```mermaid
flowchart TD
    A[install_config 开始] --> B{脚本同目录下<br/>存在 connector.env?}
    B -->|是| C[用 connector.env 覆盖<br/>VM 配置文件]
    C --> D{命令行有<br/>显式参数?}
    D -->|是| E[用命令行参数<br/>覆盖对应字段]
    D -->|否| F[读取最终配置并显示]
    E --> F
    B -->|否| G{VM 已有<br/>配置文件?}
    G -->|是| H{命令行有<br/>显式参数?}
    H -->|是| I[用命令行参数<br/>更新对应字段]
    H -->|否| J[保留现有配置]
    I --> F
    J --> F
    G -->|否| K[用默认值创建<br/>新配置文件]
    K --> F
```

### 部署流程

```mermaid
sequenceDiagram
    participant Mac as macOS (deploy-to-lima.sh)
    participant VM as Lima VM (install.sh)

    Mac->>Mac: 编译 Linux 二进制
    Mac->>VM: 传输 binary + install.sh + service + connector.env
    Mac->>VM: 执行 sudo bash install.sh --binary=...
    VM->>VM: install_binary() 安装二进制
    VM->>VM: install_config() 检测同目录 connector.env
    Note over VM: 发现 connector.env → 覆盖 VM 配置
    Note over VM: 命令行参数 → 再次覆盖对应字段
    VM->>VM: install_service() 启动服务
```

## 实施记录

### 修改文件

| 文件 | 修改内容 | 状态 |
|------|---------|------|
| `deploy/install.sh` | 重构 `install_config()` 函数，增加脚本同目录 `connector.env` 检测和覆盖逻辑 | ✅ 已完成 |
| `deploy/deploy-to-lima.sh` | 更新 `do_transfer()` 中 connector.env 传输注释，从"仅作参考"改为"会覆盖 VM 配置" | ✅ 已完成 |

### 改动详情

#### install.sh - `install_config()` 函数

**改动前**：只有两个分支
1. VM 已有配置 → 仅更新命令行显式参数
2. VM 无配置 → 用默认值创建

**改动后**：三个分支
1. 脚本同目录有 `connector.env` → 覆盖 VM 配置 → 再应用命令行参数
2. 脚本同目录无 `connector.env`，但 VM 已有配置 → 仅更新命令行显式参数（行为不变）
3. 都没有 → 用默认值创建（行为不变）

#### deploy-to-lima.sh - `do_transfer()` 函数

将注释从 `# 传输 env 模板（仅作参考）` 改为 `# 传输 env 配置文件（install.sh 会以此文件为主覆盖 VM 中的已有配置）`

## 遗留问题

- 无
