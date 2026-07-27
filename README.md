# PowerCheck

PowerCheck 是面向 PVE 和常开 Linux 唤醒机的断电保护项目。目前完成了状态机模拟器、假 PVE 执行器和真实只读 Dry-run。项目尚未包含真实关机执行器。

## 第一阶段能验证什么

- NUT 连续报告 `OB/LB` 30 秒后确认停电。
- NUT 不可达，且内网和多个外网目标全部不可达 60 秒后确认停电。
- NUT/NAS 单点故障或仅外网故障不会触发关机。
- 恢复后连续 3 次健康采样会取消尚未确认的停电。
- 以首次停电证据为 `T0`，在 `T+240 秒`强制停止剩余 Guest 并请求关闭宿主机。
- 守护程序重启后恢复状态，倒计时不会重新开始。
- 探测可并发执行，同一节点不允许两轮探测重叠。
- WOL 默认只在 2 分钟窗口内尝试 4 次：0、30、60、90 秒。

PVE 节点首次安装后立即使用内置安全默认值：检测间隔 5 秒、NUT
确认 30 秒、全网络中断确认 60 秒、总关机预算 300 秒、紧急预留
60 秒。首次运行不依赖 Web 控制台下发配置；Web 中保存成功的配置
只会替换节点本地的最后一次有效配置。

模拟器使用虚拟时间，完整的 250 秒停电过程会瞬间跑完。

## 第二阶段能验证什么

- 假 VM 和 LXC 清单及关机顺序，支持让 TrueNAS 最后关闭。
- QEMU Guest Agent 成功、失败、超时、未启用和 LXC 不适用。
- Agent 测试不会改变 Guest 运行状态，并保存最近测试结果与时间。
- 正常关机使用 Agent、ACPI 或 LXC 关机机制。
- `pvenode stopall` 成功或失败。
- Guest 正常退出、关机卡住以及紧急强制停止。
- 强停失败时，安全护栏阻止宿主机关机。
- 宿主机正常关机成功或失败。

第二阶段的所有命令都只是保存在内存中的预期事件。

## 第三阶段能验证什么

- 通过 PVE 本机只读 API 命令读取当前节点的 VM、LXC、名称和运行状态。
- 通过 NUT `upsc` 读取 `ups.status`、电量、电压、负载和运行时间等原始变量。
- 同一轮并发检测 NUT、PVE、内网和多个外网目标。
- 使用真实 `qm agent <VMID> ping` 无损测试 QEMU Guest Agent。
- 连续监测时运行真实状态机，但所有关机动作只写入 `would_run`。
- 读取失败会记录在 `issues`，PVE 读取失败时不会误报“所有 Guest 已停止”。
- `pve_node` 限制只处理当前独立 PVE 节点，不会混入其他节点 Guest。

真实命令执行层只有以下白名单：

```text
pvesh get /cluster/resources --type vm --output-format json
qm agent <VMID> ping
upsc <UPS名称@NAS地址>
ping <目标>
```

`qm stop`、`pct stop`、`pvenode stopall`、`systemctl poweroff`、`upscmd` 和参数注入都会在启动进程前被拒绝。

## 运行

在 PowerShell 中，可以从任意目录运行：

```powershell
$go = 'C:\Program Files\Go\bin\go.exe'
& $go -C C:\Users\1\Documents\PVE test ./...
& $go -C C:\Users\1\Documents\PVE run ./cmd/powercheck-sim -all
```

查看 TrueNAS 最后关闭的场景：

```powershell
& $go -C C:\Users\1\Documents\PVE run ./cmd/powercheck-sim -scenario scenarios/pve-graceful-order.json
```

查看 QEMU Guest Agent 测试：

```powershell
& $go -C C:\Users\1\Documents\PVE run ./cmd/powercheck-sim -scenario scenarios/pve-agent-tests.json
```

使用自定义时间配置：

```powershell
& $go -C C:\Users\1\Documents\PVE run ./cmd/powercheck-sim -all -config powercheck.example.json
```

配置文件全部使用“秒”，可修改检测间隔、两种确认时间、总预算、紧急预留时间和恢复所需的连续成功次数。未知字段或不合理的时间组合会被拒绝，避免拼写错误被静默忽略。

## Dry-run

先在 Windows 使用内置命令输出测试完整读取链路，不连接 PVE：

```powershell
& $go -C C:\Users\1\Documents\PVE run ./cmd/powercheck-dryrun -demo -config powercheck-dryrun.example.json
& $go -C C:\Users\1\Documents\PVE run ./cmd/powercheck-dryrun -demo -agent-test 100 -config powercheck-dryrun.example.json
```

部署到 PVE 前复制并修改 `powercheck-dryrun.example.json`：

- `pve_node`：该 PVE 在界面中显示的节点名称；
- `nut_target`：群晖广播的 `UPS名称@群晖IP`；
- `lan_targets`：至少一个，建议群晖和网关；
- `wan_targets`：至少两个，建议使用不同服务商目标。

在 PVE 上确认 `pvesh`、`qm`、`upsc` 和 `ping` 可用后，单轮读取：

```bash
./powercheck-dryrun -config ./powercheck-dryrun.json
```

持续 Dry-run：

```bash
./powercheck-dryrun -config ./powercheck-dryrun.json -watch
```

无损测试指定 VM 的 QEMU Guest Agent：

```bash
./powercheck-dryrun -config ./powercheck-dryrun.json -agent-test 100
```

默认只采样一轮，无法满足 30/60 秒持续条件；必须显式使用 `-watch` 才会持续监测。无论哪种方式，都不会执行输出中的 `would_run`。

## GitHub 发布与快速更新

推送普通提交时，GitHub Actions 会执行 Go 格式检查、`go vet`、全部
Go 测试、Web 构建、浏览器交互测试和发布配置校验。推送形如 `v0.1.0`
的标签后，Release 流水线会为 Linux/Windows 的 amd64、arm64 生成
归档和 `checksums.txt`。

当前 Release 只包含模拟器和只读 Dry-run；真实关机节点程序尚未完成。

Linux 首次安装最新 Release：

```bash
curl -fsSL https://github.com/advfree/powercheck/releases/latest/download/install.sh | sudo sh
```

以后更新：

```bash
sudo powercheck-update
```

安装器会先验证 SHA256，再备份旧二进制、原子替换并执行版本健康检查。
检查失败会恢复旧二进制。它不会覆盖 `/etc/powercheck` 配置或
`/var/lib/powercheck` 中的停电状态。

固定安装某个版本：

```bash
sudo POWERCHECK_VERSION=v0.1.0 powercheck-update
```

## 场景文件

`scenarios` 中的 JSON 文件描述：

- 起始 NUT、内网、外网和 Guest 状态；
- 指定秒数发生的状态变化；
- 可选的节点程序重启时间；
- 应当产生的动作及其精确时间。

新增或修改场景后，`go test ./...` 会自动逐个验证。测试中的“关机”只是状态机输出的事件，当前代码没有连接 `pvenode`、`qm`、`pct`、`systemctl poweroff` 或其他系统命令。

## 目录

- `internal/core`：纯状态机和时间预算。
- `internal/configfile`：可校验的 JSON 时间配置。
- `internal/fakepve`：不执行真实命令的 PVE、VM 和 LXC 内存模拟器。
- `internal/readonlyexec`：严格只读命令白名单和无 Shell 执行层。
- `internal/pvereader`：PVE Guest 清单及 QEMU Agent 只读测试。
- `internal/nutreader`：NUT `upsc` 输出解析。
- `internal/reachability`：Linux/Windows 单次 Ping。
- `internal/dryrun`：并发真实快照和只记录动作的会话。
- `internal/probe`：带超时、并发上限和防重叠的探测调度器。
- `internal/sim`：JSON 场景加载、虚拟时间运行和结果比对。
- `internal/wol`：有限 WOL 重试窗口计算。
- `cmd/powercheck-sim`：命令行模拟器。
- `cmd/powercheck-dryrun`：真实读取、永不关机的命令行程序。
- `PVE_POWER_V0.1_REQUIREMENTS.md`：v0.1 需求文档。
