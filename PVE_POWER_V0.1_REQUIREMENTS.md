# PowerCheck：PVE 断电保护与自动唤醒系统 v0.1 需求文档

## 1. 项目目标

为两台相互独立、未组成集群的 PVE 主机提供：

- 停电判断；
- VM、LXC 和 PVE 宿主机安全关机；
- 来电后通过独立 Ubuntu Server 唤醒机发送 WOL；
- Web 设备管理、事件记录和配置管理。

PVE 使用无通信接口 UPS，因此程序通过群晖 NUT 状态和网络可达性判断停电，并通过时间预算推测 UPS 剩余运行时间。

## 2. 安装模式

统一安装程序提供两种角色：

1. **PVE 节点**
   - 安装本项目开发的断电检测和安全关机守护程序 `powercheck-node`；
   - 读取 NUT 状态；
   - 检测内网和外网；
   - 记录本地事件。
2. **管理/唤醒机**
   - 安装管理程序 `powercheck-manager`；
   - 安装在 4 GB 内存、16 GB 存储的 Ubuntu Server；
   - 提供 Web 管理界面；
   - 保存设备地址；
   - 来电启动后发送 WOL；
   - 汇总 PVE 事件。

安装程序同时支持无人值守参数：

```bash
./install.sh --role pve
./install.sh --role manager
```

本文中的 **PowerCheck Node** 是本项目开发并安装在 PVE 宿主机上的
`powercheck-node`，不是 PVE 自带程序，也不是安装在虚拟机内部的
QEMU Guest Agent。它调用 PVE 已有的 `pvenode`、`qm`、`pct` 等工具完成工作。

## 3. PVE 停电判断

### 3.1 判断条件

- NUT 连续报告 `OB` 或 `LB` 30 秒：确认停电。
- NUT 不可达，并且 NAS、网关和多个外网目标全部不可达，连续持续 60 秒：推断停电。
- NUT 不可达，但至少一个内网目标可达：视为 NAS/NUT 故障，不关机。
- 内网正常、只有外网不可达：视为宽带故障，不关机。
- 确认停电前，连续 3 次检测恢复正常：取消本次判断。
- 提供可自动过期的维护模式，维护期间只记录、不关机。

不再设置额外的 180 秒启动保护期。连续 30 秒或 60 秒的确认时间本身用于过滤启动期间和短暂网络异常，避免额外等待占用 300 秒 UPS 时间预算。

NUT 为当前主要数据源；apcupsd 可作为可选数据源，但同一节点只能设置一个主要 UPS 数据源。

### 3.2 每 5 秒检测方式

每 5 秒执行一次完整检测轮次，同一轮中的任务并行进行：

1. 查询 NUT；
2. 检测 NAS 和网关；
3. 检测多个外网目标。

所有任务使用同一个轮次编号和开始时间，单轮默认最多等待 3 秒。程序收集完本轮结果或到达超时时间后，生成一份完整快照，再统一进行逻辑判断；不得在某一个 Ping 先返回时提前判断。

每轮执行规则：

- 不同目标并行探测，避免顺序执行导致总耗时相加；
- 同一目标每轮只发送一次探测；
- 单轮超时后取消仍未完成的任务；
- 上一轮未结束时不得启动重叠的新轮次；
- 下一轮按 5 秒检测间隔继续执行；
- 两台 PVE 可以各自独立探测，少量 Ping 和 NUT 查询不会造成冲突。

单轮结果只是一份状态快照，不会因为一次失败立即关机。程序按以下优先级处理连续快照：

1. NUT 返回 `OB/LB`：开始或继续 30 秒停电确认；
2. NUT 返回新鲜的 `OL`：认为市电正常，清除未确认的停电计时；
3. NUT 不可达，但任意内网或外网目标可达：认为是 NUT 或网络局部故障，不触发关机；
4. NUT、全部内网目标和全部外网目标均不可达：开始或继续 60 秒停电确认；
5. 如果曾收到 `OB/LB`，随后 NUT 和网络同时失联，继续原有停电计时，不重新开始。

因此，30 秒约为连续 6 轮有效 `OB/LB` 快照，60 秒约为连续 12 轮完整失联快照。

### 3.3 状态

```text
NORMAL
→ CONFIRMING_OUTAGE
→ SHUTTING_DOWN
→ POWERED_OFF
```

进入 `SHUTTING_DOWN` 后不再取消，也不得重复启动第二个关机任务。

## 4. 300 秒关机时间预算

`T` 表示第一次出现停电证据的时间，不是 PowerCheck Node 启动时间：

- NUT 路径：第一次收到 `OB/LB` 的时间；
- 网络推断路径：第一次出现 NUT、全部内网和全部外网目标均不可达的时间。

```text
T+0 秒     开始记录停电证据
T+30 秒    NUT 持续 OB/LB 时，确认停电并开始安全关闭 Guest
或者
T+60 秒    完整失联持续 60 秒时，推断停电并开始安全关闭 Guest
T+240 秒   剩余 60 秒，强制停止仍未关闭的 Guest
随后       使用 systemctl poweroff 正常关闭 PVE
T+300 秒   预计 UPS 可能耗尽
```

正常关闭阶段使用 PVE 官方机制：

```bash
pvenode stopall --force-stop 0 --timeout 180
```

要求：

- 安装时根据实际 PVE 版本检查 `pvenode stopall` 参数；
- 正常阶段不得直接使用 `qm stop` 或 `pct stop`；
- T+240 秒只强制停止仍在运行的 Guest；
- Guest 处理完成后使用 `systemctl poweroff`，不得强制关闭宿主机；
- 停电开始时间必须持久化，PowerCheck Node 重启后不得重新获得 300 秒；
- 所有时间均可在配置文件中修改。

## 5. Guest 安全关机

- 已安装并启用 QEMU Guest Agent 的 VM，通过 Guest Agent 关机。
- 未安装 Guest Agent 的 VM，应在 PVE 中关闭 Agent 选项，由 PVE 发送 ACPI 关机信号。
- LXC 使用 PVE 正常容器关机机制。
- 所有 VM 必须在正式启用自动关机前完成一次手动关机测试。
- 如果其他 VM 使用 TrueNAS 提供的存储，应先关闭业务 VM，最后关闭 TrueNAS。
- TrueNAS 建议设置为启动顺序 `1`，使其最先启动、最后关闭。

### 5.1 QEMU Guest Agent 测试

管理/唤醒机 Web 页面提供无损的“测试 Guest Agent”按钮。测试时由
`powercheck-node` 在对应 PVE 本机执行：

1. 检查该 VM 是否在 PVE 配置中启用了 QEMU Guest Agent；
2. 对运行中的 VM 执行 `qm agent <VMID> ping`；
3. 将结果和检测时间返回管理页面。

页面状态分为：

- 未测试；
- 测试通过；
- 未启用；
- 已启用但无响应；
- VM 未运行；
- 检测错误。

测试通过后显示绿色勾和“最后测试时间”。再次点击测试时重新判断并覆盖旧结果，不能把一次成功永久视为正常。

该测试不会关闭 VM。无法使用 QEMU Guest Agent 的 VM 仍需单独通过 PVE 的正常“关机”操作测试 ACPI；ACPI 测试具有实际关机效果，不能混入无损测试按钮。

## 6. 管理/唤醒机

唤醒机直接连接市电，BIOS 设置为来电自动启动。它不参与 PVE 的停电关机决策。

每次唤醒机启动：

1. 等待网卡和交换机稳定 120 秒；
2. 对所有 `enabled=true` 且 `autoWake=true` 的设备启动 WOL 任务；
3. 立即发送一次 WOL，之后每 30 秒发送一次；
4. 每个任务固定持续 120 秒后停止；
5. 不根据设备是否上线提前停止；
6. 同一设备不允许并行 WOL 任务；
7. 再次下达唤醒命令时，将该设备任务截止时间重置为当前时间加 120 秒。

## 7. 设备管理

每台设备至少保存：

- 设备名称；
- MAC 地址；
- IP 地址（可选，用于显示状态）；
- 是否启用；
- 是否随唤醒机启动自动发送 WOL。

Web 界面提供：

- 添加、编辑和删除设备；
- 单台设备立即唤醒；
- 全部唤醒；
- WOL 任务剩余时间；
- 设备状态展示。

## 8. Web 界面

Web 页面至少包括：

1. **仪表板**：PVE 节点、PowerCheck Node、UPS、内网、外网和 WOL 任务状态；
2. **设备管理**：名称、IP、MAC、启用和自动唤醒；
3. **事件日历**：停电、确认、Guest 关机、PVE 关机、恢复和 WOL 事件；
4. **事件详情**：当时的 NUT、内网、外网和 Guest 关机结果；
5. **设置**：调整检测、总时间、紧急预留和 WOL 参数；
6. **日志**：查看 PVE 同步的历史记录；
7. **Guest 关机能力**：列出 VM、QEMU Guest Agent 状态、最后测试时间和重新测试按钮。

### 8.1 仪表板和实时状态

仪表板参考 `leafss1022/pve-ups-manager` 的信息组织方式，但重新实现权限和数据层。至少显示：

- UPS 状态、数据来源和最后更新时间；
- NUT 可提供时显示电量、电压、负载和预计运行时间；
- PVE 主机 CPU、内存、磁盘和运行时间；
- NAS、网关和外网目标检测结果；
- 当前状态机阶段、停电开始时间和 300 秒剩余时间；
- VM、LXC 和 QEMU Guest Agent 概况；
- WOL 任务状态。

群晖 NUT 显示的数据必须明确标注为“群晖所连接 UPS”，不得让用户误认为是 PVE 无通信 UPS 的真实电量。

页面使用 SSE 推送单向实时状态，连接失败时回退到 AJAX/HTTP 轮询；页面更新不得要求整页刷新。

### 8.2 历史趋势和事件日志

- UPS 检测仍为每 5 秒一次，但历史指标默认每 60 秒写入一次，减少 16 GB 系统盘写入；
- 状态变化和告警事件立即写入；
- 趋势图默认显示最近 30 个采样点，并允许切换最近 1 小时、24 小时、7 天和 30 天；
- 长期数据按小时聚合，原始指标和聚合指标分别设置保留期限；
- 事件按信息、警告、断电、关机、恢复和错误分类；
- 分类同时使用文字和图标，颜色只作为辅助；
- 支持深色/亮色主题和手机自适应；
- 停电时显示固定告警条和确认弹窗，可选浏览器通知。

### 8.3 PushPlus 通知

PushPlus 为可选功能：

- 通知异步发送，不得阻塞检测和关机；
- 网络中断时进入有上限的待发送队列，恢复后重试；
- 重试次数和间隔必须有限制；
- Token 仅保存在权限为 `0600` 的配置或密钥文件中；
- Web 和日志中必须隐藏 Token；
- 发送停电确认、开始关机、强制停止、宿主机关机失败和恢复事件。

### 8.4 配置管理

v0.1 只提供结构化配置表单，不直接开放任意系统文件编辑：

- 修改前显示差异；
- 服务端校验类型、范围和地址格式；
- 保存时自动创建版本和备份；
- PowerCheck Node 本地再次校验后原子写入；
- 应用失败时自动恢复上一版本；
- Web 可只读查看生效配置和来源。

群晖上的 NUT 服务不由 PowerCheck 修改。原始 `ups.conf`、`upsmon.conf`、`apcupsd.conf` 在线编辑器不进入 v0.1。

### 8.5 高风险操作

- Web 提供“模拟关机”和“只演练”按钮；
- 真实手动关机默认禁用，不进入普通仪表板操作；
- UPS 自检命令可能改变 UPS 工作状态，v0.1 只检测自检能力，不执行真实自检；
- 后续若开放真实关机或自检，必须重新输入密码、二次确认、显示目标，并满足 PVE 本地授权文件要求；
- Web 和 Manager 不得下发任意 Shell 命令。

管理程序默认提供内置 HTTPS：

```text
https://<唤醒机IP>:8443
```

首次安装自动生成本地证书和管理员账号，不强制依赖硬路由反向代理。

## 9. 数据和安全

- 管理机使用嵌入式 SQLite，不安装独立数据库；
- SQLite 启用 WAL 和完整同步；
- PVE 在网络中断期间将日志保存在本地，恢复后再同步；
- 只记录状态变化，不持续写入每一次 Ping；
- 日志和事件默认保留 90 天，并设置磁盘占用上限；
- Web 需要登录认证，密码不得明文保存；
- 登录 Cookie 使用 `Secure`、`HttpOnly` 和 `SameSite`；
- 修改和 WOL 请求必须防止 CSRF，并限制登录失败频率；
- Web 仅通过学校 VPN 使用，不面向公网；
- PowerCheck Node 不提供无认证的远程 root 关机接口。

### 9.1 Node 与 Manager 通信

- PowerCheck Node 主动通过 HTTPS 连接 PowerCheck Manager；
- Node 定期上传心跳、状态、指标和事件，并领取允许列表内的任务；
- Manager 不需要主动连接 PVE 的 root 服务；
- 每台 Node 使用独立注册令牌，令牌可撤销和重新生成；
- 任务仅允许状态查询、QEMU Guest Agent 测试、读取配置和应用经过校验的 PowerCheck 配置；
- v0.1 不允许通过任务通道执行任意命令或远程强制关闭宿主机；
- 网络中断时 Node 独立完成关机，恢复后补传本地事件。

## 10. 技术约束

- PowerCheck Node 和 PowerCheck Manager 使用 Go；
- 本地开发和测试使用 Go 1.26.5；
- 构建为单文件程序；
- Web 静态资源嵌入管理程序；
- 不要求桌面环境；
- 不使用 Docker；
- 不要求 Node.js 或 Python 运行环境；
- 使用 systemd 开机启动和异常重启；
- 管理程序使用普通系统用户运行；
- 适配 Ubuntu Server 4 GB 内存、16 GB 存储环境。

## 11. 测试与演练

系统必须支持分层测试，默认安装后不得立即具备真实关机能力。

### 11.1 模拟模式

模拟模式不访问真实 NUT、网络、PVE 或 WOL 设备。使用场景文件提供虚拟输入，并使用虚拟时钟快速推进 300 秒状态机。

示例场景：

```yaml
- at_seconds: 0
  nut: unreachable
  lan: down
  wan: down
- at_seconds: 60
  expect: graceful_shutdown
- at_seconds: 240
  expect: emergency_stop_remaining
- at_seconds: 240
  expect: host_poweroff_requested
```

模拟模式只记录“本应执行”的动作，不得运行任何系统关机命令。Web 页面必须持续显示明显的“模拟模式”标识。

第一阶段提供 `powercheck-sim` 命令行模拟器、JSON 场景和自动测试。时间参数可从 JSON 配置覆盖，配置中的时长统一使用秒；未知字段和不合理的时间组合必须拒绝加载。

### 11.2 只演练模式

只演练模式使用真实 NUT 和网络检测，但所有关机动作只写入日志：

```text
WOULD RUN: pvenode stopall --force-stop 0 --timeout 180
WOULD RUN: qm stop <VMID>
WOULD RUN: systemctl poweroff
```

即使配置错误，只演练模式也不得关闭 Guest 或宿主机。

第三阶段提供 `powercheck-dryrun`：

- 配置文件必须明确包含 `mode: "dry-run"`，其他模式拒绝启动；
- 默认只执行一轮读取，只有显式指定 `-watch` 才持续监测；
- 使用 `pvesh get /cluster/resources --type vm --output-format json` 读取 VM/LXC；
- 使用 `pve_node` 过滤本节点资源，防止误处理其他节点；
- 使用只读 NUT 客户端 `upsc` 获取 `ups.status` 和其他变量；
- 使用 `qm agent <VMID> ping` 提供无损 Agent 测试；
- NUT、PVE、内网和多个外网目标在同一轮并发读取；
- 状态机产生的关机动作仅写入 `would_run`，不得传给命令执行器；
- 只读执行器使用固定白名单且不经过 Shell；
- 白名单不得包含 `pvenode stopall`、`qm stop`、`pct stop`、`systemctl poweroff` 或 `upscmd`；
- 参数必须逐项校验，拒绝命令拼接和以选项开头的目标；
- PVE 读取失败时 `all_guests_stopped` 必须为 `false`。

第三阶段提供 `-demo`，在 Windows 上使用内存命令输出验证真实解析链路，不访问 PVE、NUT 或网络。

### 11.3 假 PVE 执行器

开发和自动化测试使用假 PVE 执行器模拟：

- VM 和 LXC 列表；
- QEMU Guest Agent 成功、失败和超时；
- Guest 正常关闭、关机卡住和强制停止；
- `pvenode stopall` 成功和失败；
- 宿主机关机请求。

测试时只检查命令顺序和时间，不执行真实的 `pvenode`、`qm`、`pct` 或 `systemctl`。

第二阶段已实现纯内存假 PVE 执行器，并接入 JSON 虚拟时间场景。执行器能够记录 Agent、ACPI、LXC、`pvenode stopall`、紧急强停和宿主机关机事件；支持模拟成功、失败、超时和卡住。强停后仍有 Guest 运行时，假执行器必须把宿主机关机标记为 `blocked`。这些命令仅作为字符串记录，不调用操作系统。

### 11.4 NUT 模拟

集成测试可使用 NUT 官方 `dummy-ups` 驱动模拟 `OL`、`OB`、`LB`、失联和恢复，不需要拔掉真实 UPS 电源。

### 11.5 分阶段启用真实能力

真实 PVE 上按以下顺序启用：

1. 安装后先运行只演练模式；
2. 验证 NUT 和网络判断；
3. 无损测试 QEMU Guest Agent；
4. 使用单独的测试 VM 验证 ACPI 和正常关机；
5. 允许正常关闭 Guest，但禁止强制停止和宿主机关机；
6. 允许宿主机正常关机；
7. 最后才允许剩余 60 秒时强制停止 Guest。

破坏性动作必须同时满足配置开关和本机授权文件存在，例如：

```text
/etc/powercheck/armed
```

Web 远程操作不能自行创建该文件。

### 11.6 WOL 测试

WOL 测试先发送到测试网络或本机 UDP 接收器，验证两分钟任务和 30 秒间隔。确认后再填入真实设备 MAC 和广播地址。

## 12. 默认配置

```yaml
safety:
  mode: dry-run
  shutdown_enabled: false
  emergency_force_enabled: false
  host_poweroff_enabled: false
  require_arm_file: "/etc/powercheck/armed"

monitor:
  interval_seconds: 5
  probe_timeout_seconds: 3
  max_parallel_probes: 8
  nut_on_battery_confirm_seconds: 30
  total_network_failure_confirm_seconds: 60
  recovery_success_count: 3

power_failure:
  total_budget_seconds: 300
  emergency_reserve_seconds: 60

metrics:
  sample_interval_seconds: 60
  raw_retention_days: 30
  aggregate_retention_days: 365

shutdown:
  graceful_guest_timeout_seconds: 180
  force_remaining_guests: true

wol:
  startup_wait_seconds: 120
  duration_seconds: 120
  interval_seconds: 30
  stop_when_online: false
  allow_parallel_tasks: false

web:
  listen: "0.0.0.0:8443"
  tls_enabled: true

notifications:
  pushplus_enabled: false
  max_retries: 3
```

## 13. v0.1 验收要求

- 模拟模式可快速执行全部 300 秒场景，不使用真实等待时间；
- 只演练模式绝不执行 Guest 或宿主机关机；
- 未同时启用配置开关和本机授权文件时，破坏性动作必须被拒绝；
- 短暂网络中断不会触发关机；
- NAS/NUT 单独故障不会触发关机；
- 只有外网中断不会触发关机；
- 每 5 秒生成一轮并行检测快照，单次失败不会触发关机；
- 同一轮探测在统一超时后再判断，不允许检测轮次重叠；
- 完整停电条件持续 60 秒后进入安全关机；
- NUT 持续报告 `OB/LB` 30 秒后进入安全关机；
- `T` 从第一次出现停电证据时开始，不从 PowerCheck Node 启动时开始；
- 300 秒总时间不会因 PowerCheck Node 重启而重新计算；
- Web 能无损测试 QEMU Guest Agent，并显示当前结果和最后测试时间；
- 仪表板实时更新失败时能回退到轮询；
- 历史指标按 60 秒采样，不因 5 秒检测产生高频磁盘写入；
- PushPlus 故障或超时不会阻塞检测和关机；
- 配置修改经过差异预览、校验、备份和失败回滚；
- QEMU Guest Agent、ACPI VM 和 LXC 均能按 PVE 机制关机；
- TrueNAS 能最后关闭；
- 剩余 60 秒时只强制停止未关闭的 Guest；
- PVE 宿主机使用正常 `systemctl poweroff`；
- 唤醒机每次启动只执行一轮 120 秒 WOL；
- 再次手动唤醒只重新开启一轮 120 秒任务；
- 唤醒机突然断电后，SQLite 和设备配置能够正常恢复；
- 两台 PVE 在没有管理/唤醒机的情况下仍能独立完成关机。

## 14. v0.1 不包含

- PVE 集群、仲裁和 HA；
- 公网访问；
- 多用户和复杂权限系统；
- 自动迁移 VM；
- 根据 UPS 实时电量调整时间；
- 无限 WOL 重试；
- 任意 NUT/apcupsd 原始配置文件在线编辑；
- 真实 UPS 自检；
- Web 远程执行任意 Shell 命令；
- Web 直接远程强制关闭 PVE 宿主机。
