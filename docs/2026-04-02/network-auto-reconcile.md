# 网络自动收敛实施记录

## 需求背景

当前 `desktop` 端可以配置 Docker 子网路由、子网互通、公网访问链路以及与 Minikube 的网络通信能力，但 Lima VM 内的 Docker/Minikube 运行时网络对象会动态变化，例如：

- Docker bridge 网络删除后重建，bridge 名从一个 `br-xxxx` 变成另一个
- 默认出口网卡发生变化
- Minikube 容器 IP、bridge、Service CIDR、Pod CIDR、DNS 解析目标发生变化

现有能力更多是“配置 + 手动 Apply”或“配置一次性下发”，缺少基于运行时拓扑变化的自动收敛和自动清理。

## 目标

实现一套统一的网络自动收敛机制：

1. `desktop` 端把当前配置解析为 **期望网络状态** 并自动同步到 VM
2. VM 端持续发现当前 Docker/Minikube/出口网络拓扑
3. VM 端把期望状态自动收敛成当前有效的 iptables/route/DNS 规则
4. 当 bridge 名、默认出口网卡、Minikube 信息变化时，自动重建规则并清理陈旧规则
5. 同一套机制覆盖：
   - Docker 子网路由自动应用
   - Docker 子网互通自动应用
   - 公网访问链路自动应用
   - 与 Minikube 的网络通信自动应用

## 设计原则

- **配置存稳定语义**：配置文件只保存 CIDR/链路名，不保存 bridge 名等运行时对象
- **运行时动态解析**：bridge 名、默认出口网卡、Minikube 运行信息全部由 VM 实时发现
- **幂等收敛**：允许反复执行 reconcile，不依赖人工判断当前状态
- **精准清理**：所有自动管理的规则统一加 comment/tag，便于清理陈旧规则
- **单控制面优先**：网络规则变更统一通过 Desktop 配置 / `desired-state` 驱动，避免多条写路径并存
- **渐进保留未迁移能力**：旧 UDP 通道仅保留 `hosts/DNS` 等尚未迁移能力，不再承担网络规则写入

## 新增配置语义

### 1. 现有 CIDR 配置继续保留

- `route <cidr>`：宿主机访问该 Docker 子网
- `iptables <cidrA>+<cidrB>`：两个 Docker 子网互通
- `iptables <cidrA>-<cidrB>`：显式要求该对子网不互通

### 2. 新增持久化 VM 链路配置

新增配置项：

```conf
vm-link internet
vm-link host-docker
vm-link host-k8s.service
vm-link host-k8s.pod
vm-link docker-k8s.service
vm-link docker-k8s.pod
vm-link docker-docker
```

含义：这些链路不再只是一次性 Apply，而是进入 **Desktop 持久化配置 + VM 自动收敛** 模式。

## 已完成实现

### Desktop 侧

- [x] 新增 `vm-link` 配置解析与结构化输出
- [x] 新增 `DesiredNetworkState` 构建逻辑
- [x] 新增 `desktop -> VM` 的 `PUT /api/desired-state` 自动同步
- [x] 在 `loadConfig()` 热加载后自动同步期望状态
- [x] 在 VM 恢复在线时自动全量同步
- [x] 增加周期性重同步兜底
- [x] 新增 `/api/config/vm-link` 配置接口
- [x] VM Links 面板的 `Apply/Revert` 改为持久化配置操作

### VM 侧

- [x] 新增 `AutoReconciler` 自动收敛器
- [x] 支持按当前 bridge 动态收敛 Docker route 规则
- [x] 支持按当前 bridge 动态收敛 Docker 子网互通规则
- [x] 支持公网链路 `internet` 自动收敛
- [x] 支持 `host-docker`、`docker-docker` 自动收敛
- [x] 支持 `host-k8s.service`、`host-k8s.pod` 自动收敛
- [x] 支持 `docker-k8s.service`、`docker-k8s.pod` 自动收敛
- [x] 为自动管理规则加统一 comment 前缀 `mdc-auto:`
- [x] 支持根据 tag 清理陈旧规则
- [x] 支持 Minikube 路由与 DNS 的自动收敛/清理
- [x] 新增 `GET/PUT /api/desired-state`
- [x] 新增 `GET /api/reconcile/status`
- [x] 在链路状态中增加 `desired` 标记
- [x] 明确返回 `control_plane_mode=single`
- [x] 停用 VM 侧手动 `apply/revert` 写规则入口，统一收敛到单控制面
- [x] 旧 UDP 通道仅保留 `hosts/DNS` 辅助能力，忽略 `connect/disconnect` 网络规则控制

## 本次修改文件

| 文件 | 修改内容 |
|---|---|
| `desktop/main.go` | 新增 `vmLinks` 全局状态 |
| `desktop/config.go` | 解析/输出 `vm-link` 配置，并在热加载后触发期望状态同步 |
| `desktop/dashboard.go` | 注册 `/api/config/vm-link`，VM 在线恢复时触发自动重同步 |
| `desktop/dashboard_html.go` | VM Links 面板改为持久化配置操作，并显示 `AUTO ON/OFF` |
| `desktop/vm_desired_state.go` | 新增期望状态模型、构建逻辑、同步逻辑、VM 链路配置 API |
| `docker/main.go` | 初始化 `AutoReconciler` |
| `docker/link_manager.go` | 为 `LinkStatus` 增加 `desired` 字段 |
| `docker/infra_iptables.go` | 状态检查兼容自动收敛规则的 comment 版本 |
| `docker/vm_http_server.go` | 注入收敛器、注册新接口、SSE/状态输出增加 desired 标记 |
| `docker/vm_desired_state_http.go` | 新增期望状态 HTTP 接口 |
| `docker/auto_reconciler.go` | 新增统一自动收敛器，实现规则生成、清理、路由和 DNS 收敛 |

## 联调发现与本轮修复

### 联调发现

- `iptables -S` 返回的规则参数顺序与本地生成顺序不一致，导致自动收敛把“同义规则”误判为漂移
- `iptables -S` 返回的 `--comment` 值带双引号，删除托管规则时若直接按 `strings.Fields` 透传，会把引号当成 comment 内容，导致删除失败
- 一条托管规则删除失败后，本轮 reconcile 会提前返回，缺失规则无法继续补回，影响自愈能力
- 链路状态此前只看“是否存在同义规则”，无法区分 `managed`、`legacy` 或 `mixed`，在联调时容易把 legacy 兜底误判为自动收敛生效

### 本轮代码修复

- [x] 新增 `docker/iptables_rule_utils.go`，统一处理规则 token 清洗、comment 引号剥离与语义签名比较
- [x] `AutoReconciler` 改为按规则语义签名比较 managed 规则，不再依赖 `iptables -S` 的原始输出顺序
- [x] 删除托管规则时统一先做 token 清洗，避免 comment 引号导致删除失败
- [x] `reconcileManagedChain()` 调整为“继续执行并汇总首个错误”，避免单条 stale 规则删除失败阻塞缺失规则补回
- [x] `IptablesManager` 新增规则来源识别：`managed` / `legacy` / `mixed`
- [x] VM Links 状态与详情增加来源信息展示，便于识别 legacy 干扰
- [x] 新增回归测试，覆盖规则重排、comment 引号和规则来源识别

## 编译验证

- [x] `cd desktop && go build ./...`
- [x] `cd docker && go build ./...`
- [x] `cd docker && go test ./...`

## 当前实施进展

| 步骤 | 内容 | 状态 |
|---|---|---|
| 1 | 创建需求与实施文档 | 已完成 |
| 2 | `desktop` 端期望状态生成与同步 | 已完成 |
| 3 | VM 端期望状态存储与 reconcile | 已完成 |
| 4 | 状态展示与文档回写 | 已完成 |
| 5 | 联调问题定位与自愈修复 | 已完成 |

## 尚未完成 / 后续建议

- [ ] 在真实部署环境中重新执行一次“删除托管规则后自动补回”的联调回归，确认现场 legacy 干扰已可识别
- [ ] 视需要增加“严格单控制面”开关或 legacy 清理工具，避免旧规则长期混用
- [ ] 在 Dashboard 中额外展示 `unresolved` / `last_reconcile` / `last_error`
- [ ] 对 `route` / `iptables` 配置操作增加“立即同步完成”的更明确提示
- [ ] 视需要补充 README / 使用手册中的 `vm-link` 配置说明

## 风险与注意事项

- 自动清理目前只处理带有 `mdc-auto:` comment 的规则，不会主动清理历史手工规则
- 如果某个 CIDR 当前无法解析到 bridge，系统会保留为 `unresolved`，等待下一次 reconcile，而不会强制报错退出
- Minikube 相关能力依赖 `kubectl`、运行中的 Minikube 容器以及当前集群信息；若缺失会进入 `unresolved`
- 旧 UDP 控制通道仍然保留，但已经收缩为仅处理 `hosts/DNS`；网络规则控制统一走单控制面
- `POST /api/vm/apply` 与 `POST /api/vm/revert` 现在会返回迁移提示，不再直接改动 VM 规则
- 若同时存在“历史手工规则”和“自动收敛规则”，链路状态可能显示为 active，但实际生效来源可能不是自动收敛规则

## 遗留问题

- 当前已完成编译验证，但尚未在真实 Lima + Docker + Minikube 环境做完整联调回归
- `route` / `iptables` 配置项仍主要从 Configuration 面板管理；`vm-link` 则从 VM Links 面板持久化，后续可考虑统一交互入口
