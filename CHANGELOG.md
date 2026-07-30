# Changelog

## 未发布

### 新增

- PowerCheck 服务日志使用独立的 `powercheck` journald 命名空间，默认只保留 1 天，并限制持久日志最多占用 20 MB。
- “最近事件”合并 Manager 与 PVE 本机 Guard 的真实事件，记录 UPS/PVE 状态变化、Guest Agent、WOL、配置、自动关机阶段和 DRY-RUN 模拟，持久化保留 24 小时。
- PVE 本机新增显式解锁的真实自动守护模式：NUT `OB/LB` 连续 30 秒，或 NUT 不可达且 LAN/WAN 全断连续 30 秒后，先给 Guest 45 秒正常关机窗口，再进入紧急强停并复检后关闭宿主机。
- PVE 断电配置和 Ubuntu Manager 界面的默认时间同步为现场 Guard 策略：5 秒采样、30 秒确认、120 秒总预算、45 秒紧急预留和 45 秒 Guest 正常关机超时；Manager 仍不会远程发送关机命令。
- Ubuntu Manager 主机新增独立的正式本机关机 Guard：NUT `OB/LB` 连续 30 秒，或 NUT、LAN 与 WAN 全部不可达连续 30 秒后，只关闭 Manager 所在 Linux 主机，不调用任何 PVE API。
- Ubuntu Manager 可从服务端固定设备清单发送 WOL Magic Packet。
- WOL 默认在两分钟内按 0、30、60、90 秒重试，同一设备不允许并行任务。
- Web 控制台可查看真实 WOL 配置、发送进度、数据包数量和最近错误。
- Ubuntu Manager 每 30 秒采集一次 NUT 状态并保留 1 天负载历史。
- UPS 负载详情提供 1 小时、6 小时和 24 小时曲线。
- 电池详情区分理论额定能量与运行时估算，并结合关机预算、自检和年龄依据给出更换建议。
- Dell P7920 节点详情可直接进入断电响应时间设置，并按当前配置运行一次 NUT 断电模拟。
- PVE API 可将正式 Guard 时间参数原子保存到 `/etc/powercheck/outage-config.json`，Guard 在正常状态自动加载新版本。
- 断电模拟使用 PVE 本机纯状态机和真实只读 Guest 清单生成 `would_run` 时间线。

### 安全

- 自动守护只能在 Linux 上使用，并同时要求 `-execute`、本机节点精确确认、`-emergency` 与 `AUTO SHUTDOWN <node>` 确认短语；公网单点故障、NUT 单点不可达或 LAN 单点故障不会触发关机。
- PVE API-only 模式不注册任何 Guest 或宿主机关机路由，并可限制只接受 Manager 来源地址。
- 自动 Guard 在同一次开机内持久化断电状态，服务重启不会重置确认计时；关机操作受 T+75 与 T+120 硬截止时间约束。
- Ubuntu Manager 的 PVE 代理改为严格方法与路径白名单，只允许会话、状态、Agent 测试、配置保存和 DRY-RUN 模拟；Guest 关机、全部关机及宿主机关机请求不会转发到 PVE。
- Ubuntu Manager 的 Guest 页面移除所有关机按钮，只保留状态监测和 Agent 测试；真实停电关机必须由 PVE 本机完成。
- WOL API 必须通过现有 PVE 管理员会话鉴权，并提供与设备 ID 完全匹配的确认头。
- 浏览器不能提交任意 MAC、广播地址或 UDP 端口；目标只能来自 Manager 本地配置。
- WOL 仍是人工操作，不会由自动停电流程触发；Dell P7920 当前联网网卡不支持 WOL，界面明确显示使用 RTC 后备。
- 电池运行时能量是估算值，不会伪装成受控放电测试得到的实测容量。
- 配置 API 固定接受 `mode: production`；模拟 API 始终单独返回 DRY-RUN 时间线。
- 配置保存和断电模拟使用不同的精确节点确认头；模拟测试断言 PVE 写执行器调用次数为零。

## v0.2.0 — 2026-07-27

### 新增

- Web 页面内管理员登录、退出和 12 小时服务端会话。
- 使用 PBKDF2-SHA256 加盐保存密码哈希，不再保留 Web 明文密码。
- 连续登录失败限制、HttpOnly/SameSite Cookie 和现有操作确认保护。
- 跟随系统、亮色、深色三种主题，并在浏览器中记住选择。
- 桌面端和移动端账户、主题及退出入口。
- `powercheck-web-enable --reset-password` 本机密码恢复命令。

### 安全与兼容

- QEMU Guest Agent 测试在后端确认目标是本机、非模板的 QEMU VM。
- 升级已有 Web 安装时自动迁移旧密码文件、更新 systemd 服务并重启。
- PVE 写命令仍使用固定白名单，宿主机关机前仍强制复核 Guest 状态。

### 当前边界

- 自动 NUT + 网络停电判断仍为 **DRY-RUN**，不会自动执行真实关机。
- “Guest 检测”中的人工按钮可以在逐级确认后执行真实 PVE 命令。
- 当前 PVE Web 服务因调用本机 PVE CLI 仍以 root 运行，只能放在可信内网、
  VPN 或受保护的反向代理后，禁止直接暴露到公网。
