# PowerCheck

PowerCheck 是面向 Proxmox VE（PVE）和 Linux 主机的断电保护工具。它结合 NUT、网络连通性和 PVE Guest 状态，提供安全演练、只读检查、受控关机与 Web 管理界面。

## 功能

- 使用虚拟时间模拟完整断电流程，不执行真实命令。
- 只读检查 NUT、PVE Guest 和 QEMU Guest Agent。
- 在 PVE 本机按顺序关闭 VM/LXC，并在关机前复查状态。
- 提供带登录认证的 PVE Web 控制台。
- 由独立 Manager 汇总状态、事件、UPS 历史和 WOL 任务。
- 支持 NUT 事件触发的 Linux 主机关机服务。

## 安全边界

- 模拟和 Dry-run 永远不会执行关机命令。
- 真实 PVE 操作只能在 PVE 本机执行，Manager 不代理关机接口。
- 写操作必须同时提供 `-execute` 和精确的节点或 VMID 确认参数。
- 紧急强停还需要额外的 `-emergency` 解锁参数。
- 宿主机关机前必须确认所有 Guest 已停止。
- Web/API 只应部署在可信内网、VPN 或受保护的反向代理后，禁止直接暴露到公网。

> 仓库中的 `192.0.2.0/24` 地址和 `02:00:00:00:00:xx` MAC 均为示例值。部署时请使用本地配置，不要提交真实凭据、设备地址或网络拓扑。

## 环境要求

- Go 1.26 或更高版本
- Linux（真实 NUT/PVE 操作）
- Node.js 与 npm（构建 Web 控制台）

## 快速验证

运行全部 Go 测试：

```bash
go test ./...
```

运行全部模拟场景：

```bash
go run ./cmd/powercheck-sim -all
```

运行内置 Dry-run 演示：

```bash
go run ./cmd/powercheck-dryrun \
  -demo \
  -config powercheck-dryrun.example.json
```

以上命令不会执行真实关机。

## 构建

构建 Go 程序：

```bash
go build ./cmd/...
```

构建 Web 控制台：

```bash
cd web-prototype
npm ci
npm run build
npm run test:sites
```

## 配置示例

- `powercheck.example.json`：模拟器时间与关机策略。
- `powercheck-dryrun.example.json`：PVE、NUT 与网络只读检查。
- `powercheck-outage.example.json`：正式 Guard 的断电响应参数。
- `powercheck-wol.example.json`：Manager 的 WOL 设备清单。

复制示例文件后再填写真实环境信息。实际配置、密码文件和运行状态不应提交到仓库。

## 真实操作

先执行只读检查：

```bash
sudo powercheck-pve -node <NODE> -action status
sudo powercheck-pve -node <NODE> -action agent-test -vmid <VMID>
```

真实写操作具有破坏性。请先阅读 [PVE_POWER_V0.1_REQUIREMENTS.md](PVE_POWER_V0.1_REQUIREMENTS.md)，并在测试节点逐级验证确认参数、Guest 顺序和超时配置。

## 主要目录

- `cmd/`：模拟器、Dry-run、PVE 执行器、Manager 和主机 Guard。
- `internal/`：状态机、配置、探测、PVE/NUT、Web、WOL 与事件实现。
- `scenarios/`：可重复执行的断电模拟场景。
- `scripts/`：安装、systemd、NUT 和 Web 启用脚本。
- `web-prototype/`：Web 控制台源码与测试。

## 持续集成

GitHub Actions 会执行 Go 格式检查、`go vet`、全部 Go 测试、Web 构建和发布配置校验。
