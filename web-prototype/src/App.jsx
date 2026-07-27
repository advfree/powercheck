import { useEffect, useMemo, useState } from "react";
import {
  ArrowsClockwise,
  BatteryCharging,
  Bell,
  Broadcast,
  CaretRight,
  CheckCircle,
  Clock,
  CloudArrowUp,
  Cube,
  Desktop,
  FileCode,
  Flask,
  Gear,
  HardDrives,
  House,
  Info,
  Lightning,
  ListBullets,
  Monitor,
  Play,
  Power,
  ShieldCheck,
  TestTube,
  Timer,
  Warning,
  WifiHigh,
  X,
} from "@phosphor-icons/react";
import { PVEConsole } from "./PVEConsole.jsx";

const navItems = [
  { id: "overview", label: "总览", icon: House },
  { id: "nodes", label: "PVE 节点", icon: HardDrives },
  { id: "guests", label: "Guest 检测", icon: Monitor },
  { id: "wol", label: "WOL 设备", icon: Power },
  { id: "events", label: "事件", icon: Bell },
  { id: "settings", label: "设置", icon: Gear },
];

const nodes = [
  {
    id: "p7920",
    name: "Dell P7920",
    ip: "10.0.0.11",
    running: 12,
    total: 14,
    sampled: "22:34:58",
  },
  {
    id: "mini",
    name: "Dell Mini",
    ip: "10.0.0.12",
    running: 6,
    total: 8,
    sampled: "22:34:52",
  },
];

const initialEvents = [
  {
    type: "success",
    title: "市电恢复：市电状态正常（OL）",
    note: "系统状态已恢复",
    time: "22:33:10",
  },
  {
    type: "info",
    title: "演练开始（DRY-RUN）",
    note: "当前为演练模式，不会执行真实关机",
    time: "22:30:00",
  },
  {
    type: "warning",
    title: "市电中断检测",
    note: "检测到市电中断，进入演练流程（T0 触发）",
    time: "22:30:00",
  },
  {
    type: "success",
    title: "系统启动完成",
    note: "所有服务已就绪",
    time: "22:28:14",
  },
  {
    type: "info",
    title: "NUT 连接正常",
    note: "已连接到 Synology NUT（192.168.1.50）",
    time: "22:28:13",
  },
];

const wolDevices = [
  { name: "NAS-Backup", mac: "00:11:32:AA:BB:01", status: "在线", seen: "22:34:12" },
  { name: "Office-PC", mac: "00:11:32:AA:BB:02", status: "在线", seen: "22:33:45" },
  { name: "Dev-Server", mac: "00:11:32:AA:BB:03", status: "在线", seen: "22:12:08" },
  { name: "Media-PC", mac: "00:11:32:AA:BB:04", status: "离线", seen: "—" },
  { name: "Test-Node", mac: "00:11:32:AA:BB:05", status: "离线", seen: "—" },
];

const statusItems = [
  {
    label: "市电状态",
    value: "市电正常",
    hint: "供电稳定",
    icon: Lightning,
    tone: "success",
  },
  {
    label: "NUT 状态",
    value: "OL",
    hint: "在线（市电模式）",
    icon: BatteryCharging,
    tone: "success",
  },
  {
    label: "当前 T0",
    value: "无活动",
    hint: "未触发停电事件",
    icon: Clock,
    tone: "neutral",
  },
  {
    label: "剩余预算（估算）",
    value: "300 秒",
    hint: "可用关机时间",
    icon: Timer,
    tone: "success",
  },
  {
    label: "当前模式说明",
    value: "AUTO DRY-RUN",
    hint: "手动测试可分级真实执行",
    icon: Flask,
    tone: "warning",
  },
];

function EventIcon({ type }) {
  const Icon = type === "warning" ? Warning : type === "info" ? Info : CheckCircle;
  return (
    <span className={`event-icon event-icon--${type}`}>
      <Icon size={18} weight="bold" />
    </span>
  );
}

function AppModal({ title, onClose, children }) {
  return (
    <div className="modal-backdrop" role="presentation" onMouseDown={onClose}>
      <section
        className="modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="modal-title"
        onMouseDown={(event) => event.stopPropagation()}
      >
        <header className="modal__header">
          <div>
            <p className="eyebrow">只演练模式</p>
            <h2 id="modal-title">{title}</h2>
          </div>
          <button className="icon-button" onClick={onClose} aria-label="关闭">
            <X size={20} />
          </button>
        </header>
        {children}
      </section>
    </div>
  );
}

export function App() {
  const [activeNav, setActiveNav] = useState("overview");
  const [isScanning, setIsScanning] = useState(false);
  const [lastUpdate, setLastUpdate] = useState("2026-07-26 22:35:18");
  const [events, setEvents] = useState(initialEvents);
  const [modal, setModal] = useState(null);
  const [notice, setNotice] = useState("");
  const [drawer, setDrawer] = useState(null);
  const [timing, setTiming] = useState({
    sampleInterval: 5,
    nutConfirm: 30,
    networkConfirm: 60,
    totalBudget: 300,
    emergencyReserve: 60,
  });
  const [appliedTiming, setAppliedTiming] = useState({
    sampleInterval: 5,
    nutConfirm: 30,
    networkConfirm: 60,
    totalBudget: 300,
    emergencyReserve: 60,
  });
  const [configTargets, setConfigTargets] = useState({
    p7920: true,
    mini: true,
  });
  const [configRevision, setConfigRevision] = useState(3);
  const [configApplied, setConfigApplied] = useState("v0.1-demo.3 · 已存在于 2 台 PVE");
  const activeLabel = useMemo(
    () => navItems.find((item) => item.id === activeNav)?.label ?? "总览",
    [activeNav],
  );
  const selectedTargetCount = Object.values(configTargets).filter(Boolean).length;
  const emergencyAt = timing.totalBudget - timing.emergencyReserve;
  const appliedEmergencyAt = appliedTiming.totalBudget - appliedTiming.emergencyReserve;
  const configIsDirty = JSON.stringify(timing) !== JSON.stringify(appliedTiming);
  const timingError = useMemo(() => {
    if (timing.sampleInterval < 1) return "检测间隔不能小于 1 秒。";
    if (timing.nutConfirm < timing.sampleInterval) return "NUT 确认时间不能短于一次检测间隔。";
    if (timing.networkConfirm < timing.sampleInterval) return "断网确认时间不能短于一次检测间隔。";
    if (timing.emergencyReserve < 10) return "建议至少预留 10 秒给 Guest 强停和宿主机关机。";
    if (emergencyAt <= Math.max(timing.nutConfirm, timing.networkConfirm)) {
      return "紧急停止时间必须晚于停电确认和安全关机启动时间。";
    }
    return "";
  }, [emergencyAt, timing]);

  function closeDrawer() {
    setDrawer(null);
    setActiveNav("overview");
  }

  useEffect(() => {
    if (!drawer) return undefined;

    function handleKeyDown(event) {
      if (event.key === "Escape") closeDrawer();
    }

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [drawer]);

  function selectNav(id) {
    setActiveNav(id);
    if (id === "overview") {
      setDrawer(null);
      return;
    }
    setDrawer(id);
  }

  function runScan() {
    if (isScanning) return;
    setIsScanning(true);
    setNotice("正在并行检测 NUT、PVE、内网与外网…");
    window.setTimeout(() => {
      const time = "22:35:23";
      setLastUpdate(`2026-07-26 ${time}`);
      setEvents((current) => [
        {
          type: "success",
          title: "手动检测完成：全部正常",
          note: "NUT、2 台 PVE、内网和外网均可达",
          time,
        },
        ...current.slice(0, 4),
      ]);
      setNotice("检测完成：所有目标正常，本次未产生关机动作。");
      setIsScanning(false);
    }, 1200);
  }

  function testAgents() {
    setNotice("Agent 测试完成：18 / 18 个已启用的 QEMU Guest Agent 响应正常。");
  }

  function updateTiming(key, value) {
    setTiming((current) => ({
      ...current,
      [key]: Math.max(0, Number(value) || 0),
    }));
  }

  function toggleConfigTarget(id) {
    setConfigTargets((current) => ({ ...current, [id]: !current[id] }));
  }

  function applyTimingConfig() {
    if (timingError || selectedTargetCount === 0) return;
    const nextRevision = configRevision + 1;
    setConfigRevision(nextRevision);
    setAppliedTiming({ ...timing });
    setConfigApplied(`v0.1-demo.${nextRevision} · 已写入 ${selectedTargetCount} 台 PVE`);
    setNotice(
      `时间配置已下发到 ${selectedTargetCount} 台 PVE，并原子写入本地 /etc/powercheck/config.yaml。断网后仍会独立执行。`,
    );
    setEvents((current) => [
      {
        type: "info",
        title: `配置下发成功：${selectedTargetCount} 台 PVE`,
        note: `总预算 ${timing.totalBudget} 秒，紧急阶段 T+${emergencyAt}`,
        time: "22:36:04",
      },
      ...current.slice(0, 4),
    ]);
  }

  return (
    <div className="app-shell">
      <aside className="sidebar" aria-label="主导航">
        <div className="brand">
          <span className="brand__mark">
            <ShieldCheck size={29} weight="duotone" />
          </span>
          <span>
            <strong>PowerCheck</strong>
            <small>运维控制台</small>
          </span>
        </div>
        <nav>
          {navItems.map(({ id, label, icon: Icon }) => (
            <button
              key={id}
              className={`nav-item ${activeNav === id ? "nav-item--active" : ""}`}
              onClick={() => selectNav(id)}
              aria-current={activeNav === id ? "page" : undefined}
            >
              <Icon size={21} weight={activeNav === id ? "fill" : "regular"} />
              <span>{label}</span>
            </button>
          ))}
        </nav>
        <div className="sidebar__footer">
          <span className="live-dot" aria-hidden="true" />
          管理器在线
        </div>
      </aside>

      <main className="workspace">
        <header className="topbar">
          <button className="mode-pill" onClick={() => setModal("dryrun")}>
            <Flask size={19} weight="fill" />
            <strong>DRY-RUN</strong>
            <span>· 只演练</span>
          </button>
          <div className="topbar__meta">
            <Clock size={18} />
            <span>最后更新：</span>
            <time>{lastUpdate}</time>
            <span className="system-ok">
              <CheckCircle size={17} weight="fill" />
              系统正常
            </span>
          </div>
        </header>

        <div className="mobile-heading">
          <div className="brand">
            <ShieldCheck size={26} weight="duotone" />
            <strong>PowerCheck</strong>
          </div>
          <span>{activeLabel}</span>
        </div>

        <section className="status-band" aria-label="电力安全状态">
          {statusItems.map(({ label, value, hint, icon: Icon, tone }) => {
            const displayValue = label === "剩余预算（估算）"
              ? `${appliedTiming.totalBudget} 秒`
              : value;
            return (
              <article className={`status-item status-item--${tone}`} key={label}>
                <Icon size={31} weight="duotone" />
                <div>
                  <span>{label}</span>
                  <strong>{displayValue}</strong>
                  <small>{hint}</small>
                </div>
              </article>
            );
          })}
        </section>

        {notice && (
          <div className="notice" role="status">
            {isScanning ? (
              <ArrowsClockwise className="spin" size={19} />
            ) : (
              <CheckCircle size={19} weight="fill" />
            )}
            <span>{notice}</span>
            {!isScanning && (
              <button onClick={() => setNotice("")} aria-label="关闭通知">
                <X size={16} />
              </button>
            )}
          </div>
        )}

        <section className="surface node-surface">
          <header className="surface__header">
            <h1>PVE 节点状态 <span>（2）</span></h1>
            <button className="text-button" onClick={() => setDrawer("nodes")}>
              查看节点详情 <CaretRight size={16} />
            </button>
          </header>
          <div className="node-table" role="table" aria-label="PVE 节点状态">
            <div className="node-row node-row--head" role="row">
              <span>节点</span>
              <span>状态</span>
              <span>Guest 运行中</span>
              <span>Agent 健康</span>
              <span>UPS 保护来源</span>
              <span>备注</span>
            </div>
            {nodes.map((node) => (
              <button
                className="node-row node-row--data"
                role="row"
                key={node.id}
                onClick={() => setDrawer(node.id)}
              >
                <span className="node-name">
                  <HardDrives size={31} weight="duotone" />
                  <span>
                    <strong>{node.name}</strong>
                    <small>{node.ip}</small>
                  </span>
                </span>
                <span className="good-cell">
                  <CheckCircle size={16} weight="fill" /> 在线
                  <small>正常运行</small>
                </span>
                <span className="count-cell">
                  <Cube size={22} />
                  <strong>{node.running}</strong>
                  <small>/ {node.total}</small>
                </span>
                <span className="good-cell">
                  <ShieldCheck size={20} weight="fill" />
                  健康
                  <small>最近：{node.sampled}</small>
                </span>
                <span className="source-cell">
                  <BatteryCharging size={22} />
                  <span>
                    Synology NUT
                    <small>192.168.1.50</small>
                  </span>
                </span>
                <span>运行正常</span>
              </button>
            ))}
          </div>
        </section>

        <div className="dashboard-grid">
          <section className="surface events-surface">
            <header className="surface__header">
              <h2>最近事件</h2>
            </header>
            <div className="event-list">
              {events.map((event, index) => (
                <article className="event-row" key={`${event.time}-${index}`}>
                  <EventIcon type={event.type} />
                  <span className="event-line" aria-hidden="true" />
                  <div>
                    <strong>{event.title}</strong>
                    <small>{event.note}</small>
                  </div>
                  <time>{event.time}</time>
                </article>
              ))}
            </div>
            <button className="surface__footer-link" onClick={() => setDrawer("events")}>
              查看全部事件
            </button>
          </section>

          <section className="surface wol-surface">
            <header className="surface__header">
              <h2>WOL 管理器状态</h2>
            </header>
            <div className="wol-summary">
              <div className="wol-state">
                <WifiHigh size={35} weight="duotone" />
                <span>
                  <small>服务状态</small>
                  <strong>运行中</strong>
                </span>
              </div>
              <dl>
                <div>
                  <dt>监听端口</dt>
                  <dd>9（UDP）</dd>
                </div>
                <div>
                  <dt>运行时长</dt>
                  <dd>2 天 14 小时</dd>
                </div>
              </dl>
            </div>
            <div className="device-overview">
              <h3>设备概览</h3>
              <div className="device-metrics">
                <span><small>设备总数</small><strong>5</strong></span>
                <span><small>在线</small><strong className="text-success">3</strong></span>
                <span><small>离线</small><strong>2</strong></span>
                <span><small>最近唤醒</small><strong>1 <em>（今日）</em></strong></span>
              </div>
              <div className="device-table">
                {wolDevices.map((device) => (
                  <div className="device-row" key={device.name}>
                    <span className={device.status === "在线" ? "live-dot" : "offline-dot"} />
                    <strong>{device.name}</strong>
                    <span>{device.mac}</span>
                    <span className={device.status === "在线" ? "text-success" : ""}>{device.status}</span>
                    <time>{device.seen}</time>
                  </div>
                ))}
              </div>
            </div>
            <button className="surface__footer-link" onClick={() => setDrawer("wol")}>
              管理 WOL 设备
            </button>
          </section>
        </div>

        <footer className="actions-bar">
          <button className="primary-button" onClick={runScan} disabled={isScanning}>
            {isScanning ? <ArrowsClockwise className="spin" size={20} /> : <Play size={20} weight="fill" />}
            {isScanning ? "检测中…" : "运行一次检测"}
          </button>
          <button className="secondary-button" onClick={() => setModal("drill")}>
            <ListBullets size={20} />
            查看演练详情
          </button>
          <span className="timezone">时区：Asia/Shanghai　© 2026 PowerCheck</span>
        </footer>
      </main>

      {drawer && (
        <>
          <div
            className="drawer-backdrop"
            role="presentation"
            onMouseDown={closeDrawer}
          />
          <aside
            className={`drawer ${drawer === "settings" ? "drawer--settings" : ""} ${drawer === "guests" ? "drawer--console" : ""}`}
            aria-label={`${activeLabel}详情`}
          >
            <header>
              <div>
                <p className="eyebrow">{drawer === "settings" ? "下发到 PVE 本地" : "演示面板"}</p>
                <h2>{drawer === "p7920" ? "Dell P7920" : drawer === "mini" ? "Dell Mini" : activeLabel}</h2>
              </div>
              <button className="icon-button" onClick={closeDrawer} aria-label="关闭详情">
                <X size={20} />
              </button>
            </header>
            <div className="drawer__content">
              {drawer === "nodes" || drawer === "p7920" || drawer === "mini" ? (
                <>
                  <div className="drawer-stat">
                    <span><Desktop size={22} /> 节点连接</span><strong className="text-success">正常</strong>
                  </div>
                  <div className="drawer-stat">
                    <span><Cube size={22} /> Guest 状态</span><strong>全部可读取</strong>
                  </div>
                  <div className="drawer-stat">
                    <span><TestTube size={22} /> Agent 测试</span><strong className="text-success">健康</strong>
                  </div>
                  <button className="primary-button primary-button--full" onClick={testAgents}>测试所有 Agent</button>
                </>
              ) : drawer === "guests" ? (
                <PVEConsole />
              ) : drawer === "settings" ? (
                <div className="config-panel">
                  <div className="local-config-callout">
                    <ShieldCheck size={24} weight="duotone" />
                    <div>
                      <strong>计时逻辑运行在每台 PVE 本地</strong>
                      <p>首次安装即启用下方默认值，不必先打开 Web。Web 只负责后续编辑和下发；即使唤醒机、NAS 或网络消失，PVE 仍按最后一次有效配置继续判断和关机。</p>
                    </div>
                  </div>

                  <div className="config-file">
                    <FileCode size={19} />
                    <span>
                      PVE 本地配置
                      <code>/etc/powercheck/config.yaml</code>
                    </span>
                    <strong>{configIsDirty ? "有未下发修改" : configApplied}</strong>
                  </div>

                  <fieldset className="config-section">
                    <legend>检测与关机时间</legend>
                    <div className="config-grid">
                      <label>
                        <span>检测间隔</span>
                        <span className="number-input"><input type="number" min="1" value={timing.sampleInterval} onChange={(event) => updateTiming("sampleInterval", event.target.value)} /><em>秒</em></span>
                        <small>并行采样 NUT、内网和外网</small>
                      </label>
                      <label>
                        <span>NUT OB/LB 确认</span>
                        <span className="number-input"><input type="number" min="1" value={timing.nutConfirm} onChange={(event) => updateTiming("nutConfirm", event.target.value)} /><em>秒</em></span>
                        <small>持续达到此时间才确认</small>
                      </label>
                      <label>
                        <span>全网络中断确认</span>
                        <span className="number-input"><input type="number" min="1" value={timing.networkConfirm} onChange={(event) => updateTiming("networkConfirm", event.target.value)} /><em>秒</em></span>
                        <small>NUT、内网和外网均不可达</small>
                      </label>
                      <label>
                        <span>总关机预算</span>
                        <span className="number-input"><input type="number" min="30" value={timing.totalBudget} onChange={(event) => updateTiming("totalBudget", event.target.value)} /><em>秒</em></span>
                        <small>从首次停电证据 T+0 开始</small>
                      </label>
                      <label>
                        <span>紧急关机预留</span>
                        <span className="number-input"><input type="number" min="10" value={timing.emergencyReserve} onChange={(event) => updateTiming("emergencyReserve", event.target.value)} /><em>秒</em></span>
                        <small>留给强停 Guest 和宿主机关机</small>
                      </label>
                    </div>
                  </fieldset>

                  <fieldset className="config-section">
                    <legend>下发目标</legend>
                    <div className="target-list">
                      {nodes.map((node) => (
                        <label className="target-option" key={node.id}>
                          <input type="checkbox" checked={configTargets[node.id]} onChange={() => toggleConfigTarget(node.id)} />
                          <span><strong>{node.name}</strong><small>{node.ip} · 独立 PVE</small></span>
                          <em>{configTargets[node.id] ? "将下发" : "不修改"}</em>
                        </label>
                      ))}
                    </div>
                  </fieldset>

                  <section className="timeline-preview" aria-label="时间线预览">
                    <h3>待下发的本地执行时间线</h3>
                    <div><span>T+0</span><p><strong>首次停电证据</strong><small>开始在 PVE 本地累计时间</small></p></div>
                    <div><span>T+{timing.networkConfirm}</span><p><strong>网络推断确认并安全关闭 Guest</strong><small>NUT OB/LB 路径在 T+{timing.nutConfirm} 确认</small></p></div>
                    <div><span>T+{emergencyAt}</span><p><strong>紧急停止仍未关闭的 Guest</strong><small>剩余 {timing.emergencyReserve} 秒用于宿主机完成关机</small></p></div>
                    <div><span>T+{timing.totalBudget}</span><p><strong>总预算边界</strong><small>宿主机应在此之前完成断电</small></p></div>
                  </section>

                  {timingError && <p className="config-error"><Warning size={17} weight="fill" />{timingError}</p>}
                  {selectedTargetCount === 0 && <p className="config-error"><Warning size={17} weight="fill" />至少选择一台 PVE。</p>}

                  <button
                    className="primary-button primary-button--full"
                    onClick={applyTimingConfig}
                    disabled={Boolean(timingError) || selectedTargetCount === 0}
                  >
                    <CloudArrowUp size={20} weight="bold" />
                    保存并下发到 {selectedTargetCount} 台 PVE
                  </button>
                </div>
              ) : (
                <>
                  <div className="drawer-empty">
                    {drawer === "wol" ? <Broadcast size={42} /> : drawer === "events" ? <Bell size={42} /> : <Gear size={42} />}
                    <h3>{activeLabel}</h3>
                    <p>此区域展示了导航与抽屉交互。后续接入真实 API 时沿用当前信息结构。</p>
                  </div>
                </>
              )}
            </div>
          </aside>
        </>
      )}

      {modal === "dryrun" && (
        <AppModal title="自动流程 DRY-RUN 安全说明" onClose={() => setModal(null)}>
          <div className="modal__body">
            <div className="safety-callout">
              <Flask size={25} weight="fill" />
              <div>
                <strong>自动停电触发尚未启用真实执行</strong>
                <p>自动流程仍只显示 WOULD RUN；“Guest 检测”中的手动测试按钮经过确认后会执行真实 PVE 命令。</p>
              </div>
            </div>
            <ul className="check-list">
              <li><CheckCircle size={18} weight="fill" /> NUT、PVE 和网络使用真实只读检测</li>
              <li><CheckCircle size={18} weight="fill" /> QEMU Guest Agent 测试不会关闭 VM</li>
              <li><CheckCircle size={18} weight="fill" /> 危险命令被只读白名单拒绝</li>
            </ul>
          </div>
        </AppModal>
      )}

      {modal === "drill" && (
        <AppModal title="最近一次停电演练" onClose={() => setModal(null)}>
          <div className="modal__body">
            <ol className="drill-timeline">
              <li><span>T+0</span><div><strong>检测到停电证据</strong><small>NUT、内网与外网均不可达</small></div></li>
              <li><span>T+{appliedTiming.networkConfirm}</span><div><strong>计划安全关闭 Guest</strong><small>WOULD RUN: pvenode stopall</small></div></li>
              <li><span>T+{appliedEmergencyAt}</span><div><strong>计划紧急停止剩余 Guest</strong><small>预留 {appliedTiming.emergencyReserve} 秒完成宿主机关机</small></div></li>
              <li><span>T+{appliedTiming.totalBudget}</span><div><strong>总关机预算边界</strong><small>以上计时由 PVE 本地 PowerCheck Node 执行</small></div></li>
            </ol>
          </div>
        </AppModal>
      )}
    </div>
  );
}
