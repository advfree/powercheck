# Changelog

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
