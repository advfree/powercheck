import { useCallback, useEffect, useMemo, useState } from "react";
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
  SignOut,
  TestTube,
  Timer,
  UserCircle,
  Warning,
  WifiHigh,
  X,
} from "@phosphor-icons/react";
import { PVEConsole } from "./PVEConsole.jsx";
import { ThemeControl } from "./ThemeControl.jsx";
import { managerApiRequest } from "./managerApi.js";
import { pveApiRequest } from "./pveApi.js";

const navItems = [
  { id: "overview", label: "总览", icon: House },
  { id: "nodes", label: "设备状态", icon: HardDrives },
  { id: "guests", label: "Guest 检测", icon: Monitor },
  { id: "wol", label: "WOL 设备", icon: Power },
  { id: "events", label: "事件", icon: Bell },
  { id: "settings", label: "设置", icon: Gear },
];

const nutSource = {
  name: "Synology NUT",
  ip: "192.168.1.200",
  manager: "192.168.1.99",
};

const baseNodes = [
  {
    id: "p7920",
    name: "Dell P7920",
    ip: "192.168.1.66",
    kind: "pve",
    status: "WOL 已登记",
    statusHint: "PVE API-only",
    running: 0,
    total: 7,
    agent: "未测试",
    sampled: "只读状态正常",
    protection: {
      name: "Synology NUT",
      detail: "192.168.1.200 · PVE 本机直读",
    },
    note: "唯一 PVE 节点；当前联网网卡不支持 WOL，使用 23:00 RTC 后备；关机命令在本机执行",
  },
  {
    id: "desktop",
    name: "台式机 ZHAO",
    ip: "192.168.1.170",
    kind: "desktop",
    status: "在线",
    statusHint: "Realtek 2.5GbE",
    running: "—",
    total: "—",
    agent: "不适用",
    sampled: "本机网卡已核实",
    protection: {
      name: "仅登记 WOL",
      detail: "未接入关机执行",
    },
    note: "普通 Windows 台式机；由 192.168.1.99 提供手动 WOL",
  },
];

const statusItems = [
  {
    label: "UPS 活动",
    value: "检测中",
    hint: "实时只读",
    icon: Broadcast,
    tone: "neutral",
  },
  {
    label: "市电状态",
    value: "检测中",
    hint: "等待 NUT 状态",
    icon: Lightning,
    tone: "neutral",
  },
  {
    label: "UPS 负载",
    value: "—",
    hint: "占额定容量",
    icon: Lightning,
    tone: "neutral",
  },
  {
    label: "UPS 电量",
    value: "—",
    hint: "battery.charge",
    icon: BatteryCharging,
    tone: "neutral",
  },
  {
    label: "预计续航",
    value: "—",
    hint: "battery.runtime",
    icon: Timer,
    tone: "neutral",
  },
];

function createDemoUPSHistory(hours) {
  const now = Date.now();
  const points = Array.from({ length: 48 }, (_, index) => {
    const progress = index / 47;
    const load = 49 + Math.sin(progress * Math.PI * 4) * 5 + (index % 9 === 0 ? 6 : 0);
    return {
      checked_at: new Date(now - (47 - index) * (hours * 3600000 / 47)).toISOString(),
      connected: true,
      load_percent: Number(load.toFixed(1)),
      load_percent_max: Number(Math.min(100, load + 2.5).toFixed(1)),
      charge_percent: 100,
      runtime_seconds: Math.round(370 - load * 0.8),
    };
  });
  return {
    connected: true,
    latest: {
      manufacturer: "American Power Conversion",
      model: "Back-UPS BK650M2-CH",
      ups_status: "OL",
      ups_load_percent: 55,
      ups_realpower_nominal_watts: 390,
      battery_charge: 100,
      battery_runtime_seconds: 326,
      battery_voltage: 13.5,
      battery_voltage_nominal: 12,
      battery_type: "PbAc",
      ups_manufactured_date: "2021/11/20",
      self_test_result: "Done and passed",
      checked_at: new Date(now).toISOString(),
    },
    points,
    assessment: {
      level: "observe",
      title: "建议准备更换并做受控放电测试",
      reasons: [
        "当前预计续航比 120 秒关机预算高 206 秒。",
        "铅酸电池记录已超过约 3 年，应关注续航趋势。",
        "UPS 最近一次自检结果为通过。",
        "运行时能量是根据当前负载和 UPS 续航估算，不等同于放电测试容量。",
      ],
      theoretical_energy_wh: 50.4,
      estimated_usable_energy_wh: 19.4,
      estimated_load_watts: 214.5,
      energy_estimate_method: "额定瓦数 × NUT 负载百分比",
      runtime_margin_seconds: 206,
      shutdown_budget_seconds: 120,
      battery_age_years: 4.7,
      battery_age_basis: "UPS 生产日期（仅作为电池年龄上限）",
      replacement_battery: "APCRBC153",
      specification_source: "Schneider Electric APCRBC153: 12V 4.2Ah",
    },
  };
}

function formatCheckedAt(value = new Date()) {
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  }).format(new Date(value)).replaceAll("/", "-");
}

function formatEventTime(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  const now = new Date();
  const sameDay = date.getFullYear() === now.getFullYear()
    && date.getMonth() === now.getMonth()
    && date.getDate() === now.getDate();
  return new Intl.DateTimeFormat("zh-CN", {
    month: sameDay ? undefined : "2-digit",
    day: sameDay ? undefined : "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  }).format(date).replaceAll("/", "-");
}

function formatRuntime(seconds) {
  if (!Number.isFinite(seconds)) return "—";
  const minutes = Math.floor(seconds / 60);
  const remainder = seconds % 60;
  return `${minutes}分 ${remainder}秒`;
}

function describePower(status = "") {
  const flags = new Set(status.split(/\s+/));
  if (flags.has("LB")) return { value: "低电量", hint: status, tone: "warning" };
  if (flags.has("OB")) return { value: "电池供电", hint: status, tone: "warning" };
  if (flags.has("OL")) return { value: "市电在线", hint: status, tone: "success" };
  return { value: status || "未知", hint: "NUT 状态未识别", tone: "neutral" };
}

function describeWOLJob(job, enabled = true) {
  if (!enabled) return { label: "硬件不可用", tone: "error" };
  if (!job) return { label: "尚未唤醒", tone: "neutral" };
  if (job.state === "running") {
    return {
      label: `发送中 ${job.attempts}/${job.total_attempts}`,
      tone: "running",
    };
  }
  if (job.state === "completed") {
    return {
      label: `已发送 ${job.packets_sent}/${job.total_attempts}`,
      tone: "success",
    };
  }
  return {
    label: job.state === "cancelled" ? "任务已取消" : "发送失败",
    tone: "error",
  };
}

function formatEnergy(value) {
  if (!Number.isFinite(value)) return "资料不足";
  return `${value.toFixed(1)} Wh · ${(value / 1000).toFixed(3)} 度`;
}

function formatDecimal(value, suffix = "") {
  if (!Number.isFinite(value)) return "—";
  return `${value.toFixed(1)}${suffix}`;
}

function timingFromConfig(config) {
  return {
    sampleInterval: config.interval_seconds,
    nutConfirm: config.nut_confirm_seconds,
    networkConfirm: config.network_confirm_seconds,
    totalBudget: config.total_budget_seconds,
    emergencyReserve: config.emergency_reserve_seconds,
  };
}

function describeSimulationStep(kind) {
  const descriptions = {
    outage_candidate_started: "检测到停电证据，开始累计响应时间",
    graceful_shutdown: "计划安全关闭全部 Guest",
    emergency_stop_remaining: "计划紧急停止仍在运行的 Guest",
    host_poweroff_requested: "计划请求宿主机正常关机",
  };
  return descriptions[kind] ?? kind;
}

function UPSHistoryChart({ points }) {
  const samples = points.filter((point) => Number.isFinite(point.load_percent));
  if (samples.length < 2) {
    return (
      <div className="ups-chart-empty">
        历史数据正在积累，至少需要两个采样点。
      </div>
    );
  }
  const width = 720;
  const height = 220;
  const left = 38;
  const right = 18;
  const top = 18;
  const bottom = 28;
  const plotWidth = width - left - right;
  const plotHeight = height - top - bottom;
  const coordinate = (point, index, field) => {
    const x = left + (index / (samples.length - 1)) * plotWidth;
    const value = Math.max(0, Math.min(100, point[field] ?? point.load_percent));
    const y = top + (1 - value / 100) * plotHeight;
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  };
  const averageLine = samples.map((point, index) => coordinate(point, index, "load_percent")).join(" ");
  const maximumLine = samples.map((point, index) => coordinate(point, index, "load_percent_max")).join(" ");
  const firstTime = new Date(samples[0].checked_at).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });
  const lastTime = new Date(samples.at(-1).checked_at).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });

  return (
    <figure className="ups-chart">
      <svg viewBox={`0 0 ${width} ${height}`} role="img" aria-label="UPS 负载百分比历史曲线">
        {[0, 25, 50, 75, 100].map((value) => {
          const y = top + (1 - value / 100) * plotHeight;
          return (
            <g key={value}>
              <line className="ups-chart__grid" x1={left} x2={width - right} y1={y} y2={y} />
              <text className="ups-chart__axis" x={left - 8} y={y + 4} textAnchor="end">{value}%</text>
            </g>
          );
        })}
        <polyline className="ups-chart__maximum" points={maximumLine} />
        <polyline className="ups-chart__average" points={averageLine} />
      </svg>
      <figcaption>
        <span>{firstTime}</span>
        <span><i className="chart-key chart-key--average" />平均负载</span>
        <span><i className="chart-key chart-key--maximum" />区间峰值</span>
        <span>{lastTime}</span>
      </figcaption>
    </figure>
  );
}

function EventIcon({ type }) {
  const Icon = type === "warning" ? Warning : type === "info" ? Info : CheckCircle;
  return (
    <span className={`event-icon event-icon--${type}`}>
      <Icon size={18} weight="bold" />
    </span>
  );
}

function AppModal({ title, eyebrow = "只演练模式", wide = false, onClose, children }) {
  return (
    <div className="modal-backdrop" role="presentation" onMouseDown={onClose}>
      <section
        className={`modal ${wide ? "modal--wide" : ""}`}
        role="dialog"
        aria-modal="true"
        aria-labelledby="modal-title"
        onMouseDown={(event) => event.stopPropagation()}
      >
        <header className="modal__header">
          <div>
            <p className="eyebrow">{eyebrow}</p>
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

export function App({ session, onLogout, theme, onThemeChange }) {
  const [activeNav, setActiveNav] = useState("overview");
  const [isScanning, setIsScanning] = useState(false);
  const [nutStatus, setNutStatus] = useState({ loading: true, connected: false });
  const [pveStatus, setPVEStatus] = useState(null);
  const [agentSnapshot, setAgentSnapshot] = useState(null);
  const [pveStatusError, setPVEStatusError] = useState("");
  const [agentStatusError, setAgentStatusError] = useState("");
  const [wolStatus, setWOLStatus] = useState(null);
  const [wolStatusError, setWOLStatusError] = useState("");
  const [wakeDevice, setWakeDevice] = useState(null);
  const [wakeBusy, setWakeBusy] = useState("");
  const [upsDetail, setUPSDetail] = useState("");
  const [upsHistoryHours, setUPSHistoryHours] = useState(24);
  const [upsHistory, setUPSHistory] = useState(null);
  const [upsHistoryError, setUPSHistoryError] = useState("");
  const [upsHistoryLoading, setUPSHistoryLoading] = useState(false);
  const [lastUpdate, setLastUpdate] = useState("等待首次采样");
  const [events, setEvents] = useState([]);
  const [eventsError, setEventsError] = useState("");
  const [modal, setModal] = useState(null);
  const [notice, setNotice] = useState("");
  const [drawer, setDrawer] = useState(null);
  const [timing, setTiming] = useState({
    sampleInterval: 5,
    nutConfirm: 30,
    networkConfirm: 30,
    totalBudget: 120,
    emergencyReserve: 45,
  });
  const [appliedTiming, setAppliedTiming] = useState({
    sampleInterval: 5,
    nutConfirm: 30,
    networkConfirm: 30,
    totalBudget: 120,
    emergencyReserve: 45,
  });
  const [configTargets, setConfigTargets] = useState({
    p7920: true,
  });
  const [configRevision, setConfigRevision] = useState(0);
  const [configApplied, setConfigApplied] = useState("正在读取 PVE 本地配置");
  const [configAdvanced, setConfigAdvanced] = useState({
    recoverySuccessCount: 3,
    guestShutdownTimeout: 45,
  });
  const [configError, setConfigError] = useState("");
  const [configBusy, setConfigBusy] = useState(false);
  const [simulationResult, setSimulationResult] = useState(null);
  const activeLabel = useMemo(
    () => navItems.find((item) => item.id === activeNav)?.label ?? "总览",
    [activeNav],
  );
  const nodes = useMemo(() => {
    const guests = pveStatus?.result?.guests ?? [];
    const activeGuests = guests.filter((guest) => !guest.template && guest.status !== "stopped");
    const runningQEMU = activeGuests.filter((guest) => guest.type === "qemu");
    const agentResults = agentSnapshot?.results ?? [];
    const healthyAgents = runningQEMU.filter(
      (guest) => agentResults.some(
        (result) => result.vmid === guest.vmid && result.status === "success",
      ),
    ).length;
    return baseNodes.map((node) => {
      if (node.id !== "p7920") return node;
      let agent = "未测试";
      if (agentSnapshot) {
        agent = runningQEMU.length === 0 || healthyAgents === runningQEMU.length
          ? "健康"
          : `${healthyAgents}/${runningQEMU.length} 正常`;
      } else if (agentStatusError) {
        agent = "检测失败";
      }
      return {
        ...node,
        status: pveStatusError ? "不可读" : pveStatus ? "在线" : "检测中",
        statusHint: pveStatusError ? "PVE API 读取失败" : "PVE API-only",
        running: pveStatus ? activeGuests.length : "—",
        total: pveStatus ? guests.filter((guest) => !guest.template).length : "—",
        agent,
        sampled: agentSnapshot?.checked_at
          ? formatCheckedAt(agentSnapshot.checked_at)
          : agentStatusError || "等待真实检测",
      };
    });
  }, [agentSnapshot, agentStatusError, pveStatus, pveStatusError]);
  const wolDevices = wolStatus?.devices ?? [];
  const activeWOLJobs = wolDevices.filter((device) => device.job?.state === "running").length;
  const sentWOLPackets = wolDevices.reduce(
    (total, device) => total + (device.job?.packets_sent ?? 0),
    0,
  );
  const systemHealth = useMemo(() => {
    if (pveStatusError || (!nutStatus.loading && !nutStatus.connected)) {
      return { label: "部分异常", tone: "warning" };
    }
    if (!pveStatus || nutStatus.loading) {
      return { label: "正在检测", tone: "neutral" };
    }
    return { label: "系统正常", tone: "success" };
  }, [nutStatus.connected, nutStatus.loading, pveStatus, pveStatusError]);
  const upsHistoryStats = useMemo(() => {
    const loads = (upsHistory?.points ?? [])
      .map((point) => point.load_percent)
      .filter(Number.isFinite);
    const peaks = (upsHistory?.points ?? [])
      .map((point) => point.load_percent_max)
      .filter(Number.isFinite);
    return {
      average: loads.length ? loads.reduce((sum, value) => sum + value, 0) / loads.length : null,
      maximum: peaks.length ? Math.max(...peaks) : null,
    };
  }, [upsHistory]);
  const pveNodes = nodes.filter((node) => node.kind === "pve");
  const targetNode = pveStatus?.node ?? session.node;
  const selectedTargetCount = Object.values(configTargets).filter(Boolean).length;
  const emergencyAt = timing.totalBudget - timing.emergencyReserve;
  const appliedEmergencyAt = appliedTiming.totalBudget - appliedTiming.emergencyReserve;
  const configIsDirty = JSON.stringify(timing) !== JSON.stringify(appliedTiming);
  const latestUPS = upsHistory?.latest?.checked_at ? upsHistory.latest : nutStatus;
  const batteryAssessment = upsHistory?.assessment;
  const timingError = useMemo(() => {
    if (timing.sampleInterval < 5) return "检测间隔不能小于 5 秒。";
    if (timing.sampleInterval > 60) return "检测间隔不能超过 60 秒。";
    if (timing.nutConfirm < 5 || timing.nutConfirm > 600) return "NUT 确认时间必须在 5 到 600 秒之间。";
    if (timing.networkConfirm < 5 || timing.networkConfirm > 600) return "断网确认时间必须在 5 到 600 秒之间。";
    if (timing.totalBudget < 60 || timing.totalBudget > 3600) return "总关机预算必须在 60 到 3600 秒之间。";
    if (timing.emergencyReserve < 10) return "建议至少预留 10 秒给 Guest 强停和宿主机关机。";
    if (timing.emergencyReserve > 900) return "紧急预留不能超过 900 秒。";
    if (emergencyAt <= Math.max(timing.nutConfirm, timing.networkConfirm)) {
      return "紧急停止时间必须晚于停电确认和安全关机启动时间。";
    }
    return "";
  }, [emergencyAt, timing]);

  function closeDrawer() {
    setDrawer(null);
    setActiveNav("overview");
  }

  const refreshNUT = useCallback(async () => {
    try {
      const response = await fetch("/api/manager/v1/nut", { headers: { Accept: "application/json" } });
      const payload = await response.json();
      if (!response.ok) throw new Error(payload.error || "NUT 状态读取失败");
      setNutStatus({ loading: false, ...payload });
      setLastUpdate(formatCheckedAt(payload.checked_at));
      return true;
    } catch (error) {
      setNutStatus({ loading: false, connected: false, error: error.message });
      setLastUpdate(formatCheckedAt());
      return false;
    }
  }, []);

  const refreshPVEStatus = useCallback(async (includeAgents = false) => {
    try {
      const status = await pveApiRequest("/api/v1/status");
      setPVEStatus(status);
      setPVEStatusError("");
      if (includeAgents) {
        try {
          setAgentSnapshot(await pveApiRequest("/api/v1/agent-status"));
          setAgentStatusError("");
        } catch (agentError) {
          setAgentStatusError(agentError.message);
          return false;
        }
      }
      return true;
    } catch (error) {
      setPVEStatusError(error.message);
      return false;
    }
  }, []);

  const refreshOutageConfig = useCallback(async () => {
    try {
      const response = await pveApiRequest("/api/v1/outage-config");
      const nextTiming = timingFromConfig(response.config);
      setTiming(nextTiming);
      setAppliedTiming(nextTiming);
      setConfigAdvanced({
        recoverySuccessCount: response.config.recovery_success_count,
        guestShutdownTimeout: response.config.guest_shutdown_timeout_seconds,
      });
      setConfigRevision(response.config.revision);
      setConfigApplied(
        response.config.revision > 0
          ? `PVE 正式配置 r${response.config.revision}`
          : "PVE 正式守护默认值",
      );
      setConfigError("");
      return true;
    } catch (error) {
      setConfigError(error.message);
      setConfigApplied("PVE 配置读取失败");
      return false;
    }
  }, []);

  const refreshWOLStatus = useCallback(async () => {
    try {
      const status = await managerApiRequest("/api/manager/v1/wol");
      setWOLStatus(status);
      setWOLStatusError("");
      return true;
    } catch (error) {
      setWOLStatusError(error.message);
      return false;
    }
  }, []);

  const refreshEvents = useCallback(async () => {
    try {
      const response = await managerApiRequest("/api/manager/v1/events?limit=100");
      setEvents(Array.isArray(response.events) ? response.events : []);
      setEventsError("");
      return true;
    } catch (error) {
      setEventsError(error.message);
      return false;
    }
  }, []);

  const refreshUPSHistory = useCallback(async (hours) => {
    setUPSHistoryLoading(true);
    try {
      const report = await managerApiRequest(`/api/manager/v1/ups-history?hours=${hours}`);
      setUPSHistory(report);
      setUPSHistoryError("");
      return true;
    } catch (error) {
      if (import.meta.env.DEV) {
        setUPSHistory(createDemoUPSHistory(hours));
        setUPSHistoryError("");
        return true;
      }
      setUPSHistoryError(error.message);
      return false;
    } finally {
      setUPSHistoryLoading(false);
    }
  }, []);

  useEffect(() => {
    refreshNUT();
    const timer = window.setInterval(refreshNUT, 5000);
    return () => window.clearInterval(timer);
  }, [refreshNUT]);

  useEffect(() => {
    refreshPVEStatus(true);
    const timer = window.setInterval(() => refreshPVEStatus(false), 10000);
    return () => window.clearInterval(timer);
  }, [refreshPVEStatus]);

  useEffect(() => {
    refreshOutageConfig();
  }, [refreshOutageConfig]);

  useEffect(() => {
    refreshWOLStatus();
    const timer = window.setInterval(refreshWOLStatus, 5000);
    return () => window.clearInterval(timer);
  }, [refreshWOLStatus]);

  useEffect(() => {
    refreshEvents();
    const timer = window.setInterval(refreshEvents, 10000);
    return () => window.clearInterval(timer);
  }, [refreshEvents]);

  useEffect(() => {
    if (!upsDetail) return undefined;
    refreshUPSHistory(upsHistoryHours);
    const timer = window.setInterval(
      () => refreshUPSHistory(upsHistoryHours),
      30000,
    );
    return () => window.clearInterval(timer);
  }, [refreshUPSHistory, upsDetail, upsHistoryHours]);

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

  async function runScan() {
    if (isScanning) return;
    setIsScanning(true);
    setNotice("正在读取 Synology NUT、PVE Guest 与 Agent 实时状态…");
    const [nutSuccess, pveSuccess] = await Promise.all([
      refreshNUT(),
      refreshPVEStatus(true),
    ]);
    const success = nutSuccess && pveSuccess;
    try {
      await managerApiRequest("/api/manager/v1/events/scan", {
        method: "POST",
        action: "record-scan",
        body: { success },
      });
      await refreshEvents();
    } catch {
      // The scan result remains visible even if event persistence is temporarily unavailable.
    }
    setNotice(
      success
        ? "UPS、Guest 与 Agent 状态刷新完成，未产生任何关机或 WOL 动作。"
        : "部分状态读取失败，请查看节点和 Guest 页面。",
    );
    setIsScanning(false);
  }

  async function testAgents() {
    setNotice("正在通过 PVE 测试全部运行中 QEMU VM 的 Guest Agent…");
    try {
      const snapshot = await pveApiRequest("/api/v1/agent-status");
      setAgentSnapshot(snapshot);
      setAgentStatusError("");
      const tested = (snapshot.results ?? []).filter((result) => result.status !== "stopped");
      const healthy = tested.filter((result) => result.status === "success").length;
      await refreshEvents();
      setNotice(
        `真实 Agent 测试完成：${healthy}/${tested.length} 台运行中 QEMU VM 正常。`,
      );
    } catch (error) {
      setAgentStatusError(error.message);
      setNotice(`Agent 测试失败：${error.message}`);
    }
  }

  async function startWake() {
    if (!wakeDevice || wakeBusy) return;
    setWakeBusy(wakeDevice.id);
    try {
      await managerApiRequest(
        `/api/manager/v1/wol/${wakeDevice.id}/wake`,
        {
          method: "POST",
          action: `wake:${wakeDevice.id}`,
          body: { confirm_device: wakeDevice.id },
        },
      );
      setNotice(
        `${wakeDevice.name} 的 WOL 任务已启动；发送数据包不代表设备已经成功开机。`,
      );
      setWakeDevice(null);
      await Promise.all([refreshWOLStatus(), refreshEvents()]);
    } catch (error) {
      setNotice(`WOL 启动失败：${error.message}`);
    } finally {
      setWakeBusy("");
    }
  }

  function openUPSDetail(detail) {
    setUPSDetail(detail);
    setUPSHistoryError("");
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

  function openTimingSettings() {
    setActiveNav("settings");
    setDrawer("settings");
  }

  async function applyTimingConfig() {
    if (timingError || selectedTargetCount === 0 || configBusy) return null;
    setConfigBusy(true);
    setConfigError("");
    try {
      const response = await pveApiRequest(
        "/api/v1/outage-config",
        {
          confirm_node: targetNode,
          config: {
            mode: "production",
            revision: configRevision,
            interval_seconds: timing.sampleInterval,
            nut_confirm_seconds: timing.nutConfirm,
            network_confirm_seconds: timing.networkConfirm,
            total_budget_seconds: timing.totalBudget,
            emergency_reserve_seconds: timing.emergencyReserve,
            recovery_success_count: configAdvanced.recoverySuccessCount,
            guest_shutdown_timeout_seconds: configAdvanced.guestShutdownTimeout,
          },
        },
        {
          method: "PUT",
          action: `save-outage-config:${targetNode}`,
        },
      );
      const nextTiming = timingFromConfig(response.config);
      setTiming(nextTiming);
      setAppliedTiming(nextTiming);
      setConfigRevision(response.config.revision);
      setConfigApplied(`PVE 正式配置 r${response.config.revision}`);
      setNotice(`断电响应时间已写入 ${targetNode} 并由本机 Guard 自动应用；管理端不会发送关机命令。`);
      await refreshEvents();
      return response.config;
    } catch (error) {
      setConfigError(error.message);
      setNotice(`断电响应配置保存失败：${error.message}`);
      return null;
    } finally {
      setConfigBusy(false);
    }
  }

  async function simulateOutage({ saveFirst = false } = {}) {
    if (configBusy) return;
    if (saveFirst && !(await applyTimingConfig())) return;
    setConfigBusy(true);
    setConfigError("");
    try {
      const response = await pveApiRequest(
        "/api/v1/outage-simulation",
        {
          confirm_node: targetNode,
          scenario: "nut-ob",
        },
        {
          action: `simulate-outage:${targetNode}`,
        },
      );
      setSimulationResult(response.simulation);
      setAppliedTiming(timingFromConfig(response.simulation.config));
      setModal("simulation");
      setNotice("断电演练已完成：只生成 WOULD RUN，没有执行任何系统命令。");
      await refreshEvents();
    } catch (error) {
      setConfigError(error.message);
      setNotice(`断电模拟失败：${error.message}`);
    } finally {
      setConfigBusy(false);
    }
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
            <ShieldCheck size={19} weight="fill" />
            <strong>正式守护</strong>
            <span>· PVE 本机执行</span>
          </button>
          <div className="topbar__meta">
            <Clock size={18} />
            <span>最后更新：</span>
            <time>{lastUpdate}</time>
            <span className={`system-ok system-ok--${systemHealth.tone}`}>
              {systemHealth.tone === "warning"
                ? <Warning size={17} weight="fill" />
                : <CheckCircle size={17} weight="fill" />}
              {systemHealth.label}
            </span>
            <ThemeControl preference={theme} onChange={onThemeChange} compact />
            <span className="account-chip" title={`当前节点：${session.node}`}>
              <UserCircle size={19} weight="duotone" />
              <span>
                <strong>{session.username}</strong>
                <small>{session.node}</small>
              </span>
            </span>
            <button className="logout-button" onClick={onLogout} title="退出账户">
              <SignOut size={18} />
              <span>退出</span>
            </button>
          </div>
        </header>

        <div className="mobile-heading">
          <div className="brand">
            <ShieldCheck size={26} weight="duotone" />
            <strong>PowerCheck</strong>
          </div>
          <span className="mobile-context">
            <small>{session.username}</small>
            {activeLabel}
          </span>
        </div>

        <section className="status-band" aria-label="电力安全状态">
          {statusItems.map(({ label, value, hint, icon: Icon, tone }) => {
            let displayValue = value;
            let displayHint = hint;
            let displayTone = tone;
            if (label === "UPS 活动") {
              displayValue = nutStatus.loading
                ? "检测中"
                : nutStatus.connected
                  ? "活动"
                  : "不可读";
              displayHint = nutStatus.connected
                ? `${nutStatus.ups_name}@${nutSource.ip}`
                : `Synology NUT ${nutSource.ip}`;
              displayTone = nutStatus.connected ? "success" : nutStatus.loading ? "neutral" : "warning";
            } else if (label === "市电状态" && nutStatus.connected) {
              const power = describePower(nutStatus.ups_status);
              displayValue = power.value;
              displayHint = power.hint;
              displayTone = power.tone;
            } else if (label === "UPS 负载" && nutStatus.connected) {
              displayValue = Number.isFinite(nutStatus.ups_load_percent)
                ? `${nutStatus.ups_load_percent}%`
                : "未提供";
              displayTone = Number.isFinite(nutStatus.ups_load_percent) ? "success" : "neutral";
            } else if (label === "UPS 电量" && nutStatus.connected) {
              displayValue = Number.isFinite(nutStatus.battery_charge)
                ? `${nutStatus.battery_charge}%`
                : "未提供";
              displayTone = Number.isFinite(nutStatus.battery_charge) && nutStatus.battery_charge > 20
                ? "success"
                : "warning";
            } else if (label === "预计续航" && nutStatus.connected) {
              displayValue = formatRuntime(nutStatus.battery_runtime_seconds);
              displayTone = Number.isFinite(nutStatus.battery_runtime_seconds) ? "success" : "neutral";
            } else if (!nutStatus.loading && !nutStatus.connected) {
              displayValue = "不可读";
              displayTone = "warning";
            }
            const detail = label === "UPS 负载"
              ? "load"
              : label === "UPS 电量"
                ? "battery"
                : "";
            const StatusElement = detail ? "button" : "article";
            return (
              <StatusElement
                className={`status-item status-item--${displayTone} ${detail ? "status-item--interactive" : ""}`}
                key={label}
                onClick={detail ? () => openUPSDetail(detail) : undefined}
                type={detail ? "button" : undefined}
              >
                <Icon size={31} weight="duotone" />
                <div>
                  <span>{label}</span>
                  <strong>{displayValue}</strong>
                  <small>{displayHint}</small>
                </div>
                {detail && <CaretRight className="status-item__caret" size={17} />}
              </StatusElement>
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
            <h1>受管节点状态 <span>（{nodes.length}）</span></h1>
            <button className="text-button" onClick={() => setDrawer("nodes")}>
              查看节点详情 <CaretRight size={16} />
            </button>
          </header>
          <div className="node-table" role="table" aria-label="受管设备状态">
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
                <span className={`node-state-cell ${node.status === "在线" ? "good-cell" : ""}`}>
                  {node.status === "在线"
                    ? <CheckCircle size={16} weight="fill" />
                    : <Warning size={16} weight="fill" />}
                  {node.status}
                  <small>{node.statusHint}</small>
                </span>
                <span className="count-cell">
                  <Cube size={22} />
                  <strong>{node.running}</strong>
                  <small>/ {node.total}</small>
                </span>
                <span className={`node-state-cell ${node.agent === "健康" ? "good-cell" : ""}`}>
                  <ShieldCheck size={20} weight="fill" />
                  {node.agent}
                  <small>最近：{node.sampled}</small>
                </span>
                <span className="source-cell">
                  <BatteryCharging size={22} />
                  <span>
                    {node.protection.name}
                    <small>{node.protection.detail}</small>
                  </span>
                </span>
                <span>{node.note}</span>
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
              {events.slice(0, 5).map((event) => (
                <article className="event-row" key={event.id}>
                  <EventIcon type={event.type} />
                  <span className="event-line" aria-hidden="true" />
                  <div>
                    <strong>{event.title}</strong>
                    <small>{event.note}</small>
                  </div>
                  <time>{formatEventTime(event.created_at)}</time>
                </article>
              ))}
              {!events.length && (
                <div className="event-empty">
                  {eventsError || "正在读取真实事件…"}
                </div>
              )}
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
                  <small>配置状态</small>
                  <strong>
                    {wolStatusError
                      ? "API 不可用"
                      : wolStatus
                        ? activeWOLJobs > 0
                          ? `${activeWOLJobs} 个任务发送中`
                          : "WOL 已启用"
                        : "检测中"}
                  </strong>
                </span>
              </div>
              <dl>
                <div>
                  <dt>发送端</dt>
                  <dd>192.168.1.99</dd>
                </div>
                <div>
                  <dt>重试计划</dt>
                  <dd>
                    {wolStatus
                      ? `${wolStatus.duration_seconds}秒 / ${wolStatus.interval_seconds}秒`
                      : "读取中"}
                  </dd>
                </div>
              </dl>
            </div>
            <div className="device-overview">
              <h3>设备概览</h3>
              <div className="device-metrics">
                <span><small>设备总数</small><strong>{wolDevices.length || "—"}</strong></span>
                <span><small>已启用</small><strong className="text-success">{wolDevices.filter((device) => device.enabled).length || "—"}</strong></span>
                <span><small>进行中</small><strong>{activeWOLJobs}</strong></span>
                <span><small>已发数据包</small><strong>{sentWOLPackets}</strong></span>
              </div>
              <div className="device-table">
                {wolDevices.map((device) => (
                  <div className="device-row" key={device.id}>
                    <span className={device.job?.state === "running" ? "activity-dot" : device.enabled ? "live-dot" : "offline-dot"} />
                    <strong>{device.name}</strong>
                    <span>{device.ip} · {device.mac}</span>
                    <span className={device.job?.state === "completed" ? "text-success" : ""}>
                      {describeWOLJob(device.job, device.enabled).label}
                    </span>
                    <time>UDP {device.port}</time>
                  </div>
                ))}
                {!wolDevices.length && (
                  <div className="wol-inline-status">
                    {wolStatusError || "正在读取 WOL 设备配置…"}
                  </div>
                )}
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
            className={`drawer ${drawer === "settings" ? "drawer--settings" : ""} ${drawer === "guests" ? "drawer--console" : ""} ${drawer === "wol" ? "drawer--wol" : ""} ${drawer === "events" ? "drawer--events" : ""}`}
            aria-label={`${activeLabel}详情`}
          >
            <header>
              <div>
                <p className="eyebrow">
                  {drawer === "settings"
                    ? "下发到 PVE 本地"
                    : drawer === "wol"
                      ? "192.168.1.99 实际发送"
                      : drawer === "events"
                        ? "真实记录 · 保留 24 小时"
                        : "实时状态"}
                </p>
                <h2>{nodes.find((node) => node.id === drawer)?.name ?? activeLabel}</h2>
              </div>
              <button className="icon-button" onClick={closeDrawer} aria-label="关闭详情">
                <X size={20} />
              </button>
            </header>
            <div className="drawer__content">
              {drawer === "nodes" || nodes.some((node) => node.id === drawer) ? (
                <>
                  <div className="drawer-stat">
                    <span><Desktop size={22} /> 节点连接</span>
                    <strong>{drawer === "desktop" ? "WOL 已登记（未探测在线）" : pveStatusError ? "PVE API 异常" : "PVE API 正常"}</strong>
                  </div>
                  <div className="drawer-stat">
                    <span><BatteryCharging size={22} /> UPS 保护来源</span>
                    <strong>Synology NUT · 192.168.1.200</strong>
                  </div>
                  <div className="drawer-stat">
                    <span><Info size={22} /> 备注</span>
                    <strong>{nodes.find((node) => node.id === drawer)?.note ?? "由 192.168.1.99 统一管理"}</strong>
                  </div>
                  {drawer === "p7920" && (
                    <div className="node-detail-actions">
                      <button className="secondary-button" onClick={testAgents}>
                        <TestTube size={19} /> 测试所有 Agent
                      </button>
                      <button className="secondary-button" onClick={openTimingSettings}>
                        <Timer size={19} /> 修改断电响应时间
                      </button>
                      <button
                        className="primary-button"
                        onClick={() => simulateOutage()}
                        disabled={configBusy || Boolean(configError)}
                      >
                        <Play size={19} weight="fill" />
                        {configBusy ? "正在模拟…" : "按当前配置模拟断电"}
                      </button>
                    </div>
                  )}
                </>
              ) : drawer === "guests" ? (
                <PVEConsole />
              ) : drawer === "settings" ? (
                <div className="config-panel">
                  <div className="local-config-callout">
                    <ShieldCheck size={24} weight="duotone" />
                    <div>
                      <strong>配置保存在 Dell P7920 本机</strong>
                      <p>正式参数由 PVE 本地 Guard 自动读取：30 秒确认、T+75 秒紧急阶段、T+120 秒硬预算。此管理端只保存参数和生成安全演练时间线，不会远程发送关机命令。</p>
                    </div>
                  </div>

                  <div className="config-file">
                    <FileCode size={19} />
                    <span>
                      PVE 本地配置
                      <code>/etc/powercheck/outage-config.json</code>
                    </span>
                    <strong>{configIsDirty ? "有未下发修改" : configApplied}</strong>
                  </div>

                  <fieldset className="config-section">
                    <legend>检测与关机时间</legend>
                    <div className="config-grid">
                      <label>
                        <span>检测间隔</span>
                        <span className="number-input"><input type="number" min="5" value={timing.sampleInterval} onChange={(event) => updateTiming("sampleInterval", event.target.value)} /><em>秒</em></span>
                        <small>优先读取 NUT；仅在 NUT 失联时探测内网和外网</small>
                      </label>
                      <label>
                        <span>NUT OB/LB 确认</span>
                        <span className="number-input"><input type="number" min="5" max="600" value={timing.nutConfirm} onChange={(event) => updateTiming("nutConfirm", event.target.value)} /><em>秒</em></span>
                        <small>持续达到此时间才确认</small>
                      </label>
                      <label>
                        <span>全网络中断确认</span>
                        <span className="number-input"><input type="number" min="5" max="600" value={timing.networkConfirm} onChange={(event) => updateTiming("networkConfirm", event.target.value)} /><em>秒</em></span>
                        <small>NUT、内网和外网均不可达</small>
                      </label>
                      <label>
                        <span>总关机预算</span>
                        <span className="number-input"><input type="number" min="60" max="3600" value={timing.totalBudget} onChange={(event) => updateTiming("totalBudget", event.target.value)} /><em>秒</em></span>
                        <small>从首次停电证据 T+0 开始</small>
                      </label>
                      <label>
                        <span>紧急关机预留</span>
                        <span className="number-input"><input type="number" min="10" max="900" value={timing.emergencyReserve} onChange={(event) => updateTiming("emergencyReserve", event.target.value)} /><em>秒</em></span>
                        <small>留给强停 Guest 和宿主机关机</small>
                      </label>
                    </div>
                  </fieldset>

                  <fieldset className="config-section">
                    <legend>下发目标</legend>
                    <div className="target-list">
                      {pveNodes.map((node) => (
                        <label className="target-option" key={node.id}>
                          <input type="checkbox" checked={configTargets[node.id]} onChange={() => toggleConfigTarget(node.id)} />
                          <span><strong>{node.name}</strong><small>{node.ip} · PVE 后端</small></span>
                          <em>{configTargets[node.id] ? "将下发" : "不修改"}</em>
                        </label>
                      ))}
                    </div>
                  </fieldset>

                  <section className="timeline-preview" aria-label="时间线预览">
                    <h3>待下发的本地执行时间线</h3>
                    <div><span>T+0</span><p><strong>首次停电证据</strong><small>开始在 PVE 本地累计时间</small></p></div>
                    <div><span>{timing.nutConfirm === timing.networkConfirm ? `T+${timing.nutConfirm}` : `T+${timing.nutConfirm}/${timing.networkConfirm}`}</span><p><strong>确认停电并安全关闭 Guest</strong><small>NUT T+{timing.nutConfirm}，全网中断 T+{timing.networkConfirm}；Guest 正常关机窗口 {configAdvanced.guestShutdownTimeout} 秒</small></p></div>
                    <div><span>T+{emergencyAt}</span><p><strong>紧急停止仍未关闭的 Guest</strong><small>剩余 {timing.emergencyReserve} 秒用于宿主机完成关机</small></p></div>
                    <div><span>T+{timing.totalBudget}</span><p><strong>总预算边界</strong><small>宿主机应在此之前完成断电</small></p></div>
                  </section>

                  {timingError && <p className="config-error"><Warning size={17} weight="fill" />{timingError}</p>}
                  {selectedTargetCount === 0 && <p className="config-error"><Warning size={17} weight="fill" />至少选择一台 PVE。</p>}
                  {configError && <p className="config-error"><Warning size={17} weight="fill" />{configError}</p>}

                  <div className="config-actions">
                    <button
                      className="secondary-button"
                      onClick={applyTimingConfig}
                      disabled={Boolean(timingError) || selectedTargetCount === 0 || configBusy}
                    >
                      <CloudArrowUp size={20} weight="bold" />
                      {configBusy ? "正在写入…" : "保存到 Dell P7920"}
                    </button>
                    <button
                      className="primary-button"
                      onClick={() => simulateOutage({ saveFirst: true })}
                      disabled={Boolean(timingError) || selectedTargetCount === 0 || configBusy}
                    >
                      <Play size={20} weight="fill" />
                      {configBusy ? "处理中…" : "保存并模拟 NUT 断电"}
                    </button>
                  </div>
                </div>
              ) : drawer === "events" ? (
                <div className="event-history">
                  <div className="event-history__header">
                    <span>共 {events.length} 条</span>
                    <button className="secondary-button" onClick={refreshEvents}>
                      <ArrowsClockwise size={17} />
                      刷新
                    </button>
                  </div>
                  {eventsError && (
                    <p className="config-error"><Warning size={17} weight="fill" />{eventsError}</p>
                  )}
                  <div className="event-list event-list--full">
                    {events.map((event) => (
                      <article className="event-row" key={event.id}>
                        <EventIcon type={event.type} />
                        <span className="event-line" aria-hidden="true" />
                        <div>
                          <strong>{event.title}</strong>
                          <small>{event.note}</small>
                          <em>{event.source}</em>
                        </div>
                        <time>{formatEventTime(event.created_at)}</time>
                      </article>
                    ))}
                    {!events.length && !eventsError && (
                      <div className="event-empty">尚无真实事件记录。</div>
                    )}
                  </div>
                </div>
              ) : drawer === "wol" ? (
                <div className="wol-manager">
                  <div className="wol-manager__summary">
                    <span><Broadcast size={22} /><small>发送端</small><strong>192.168.1.99</strong></span>
                    <span><Timer size={22} /><small>发送计划</small><strong>{wolStatus ? `${wolStatus.duration_seconds} 秒` : "—"}</strong></span>
                    <span><ArrowsClockwise size={22} /><small>间隔</small><strong>{wolStatus ? `${wolStatus.interval_seconds} 秒` : "—"}</strong></span>
                  </div>

                  {wolStatusError && (
                    <p className="config-error"><Warning size={17} weight="fill" />{wolStatusError}</p>
                  )}

                  <div className="wol-device-list">
                    {wolDevices.map((device) => {
                      const job = describeWOLJob(device.job, device.enabled);
                      return (
                        <section className="wol-device" key={device.id}>
                          <div className="wol-device__icon">
                            {device.id.includes("pve") ? <HardDrives size={25} /> : <Desktop size={25} />}
                          </div>
                          <div className="wol-device__identity">
                            <strong>{device.name}</strong>
                            <small>{device.ip} · {device.mac}</small>
                            <small>{device.note}</small>
                          </div>
                          <div className={`wol-device__job wol-device__job--${job.tone}`}>
                            <span>{job.label}</span>
                            <small>{device.broadcast}:{device.port}</small>
                            {device.job?.last_error && <small>{device.job.last_error}</small>}
                          </div>
                          <button
                            className="secondary-button wol-device__wake"
                            onClick={() => setWakeDevice(device)}
                            disabled={!device.enabled || device.job?.state === "running" || Boolean(wakeBusy)}
                          >
                            <Power size={18} weight="fill" />
                            {device.job?.state === "running" ? "发送中" : "唤醒"}
                          </button>
                        </section>
                      );
                    })}
                  </div>
                  <p className="wol-disclaimer">
                    WOL 只发送 Magic Packet，无法确认目标设备是否已经完成开机。
                  </p>
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
        <AppModal title="管理端安全边界" onClose={() => setModal(null)}>
          <div className="modal__body">
            <div className="safety-callout">
              <Flask size={25} weight="fill" />
              <div>
                <strong>PVE 本地 Guard 已启用，管理端不执行关机</strong>
                <p>Dell P7920 在本机判断停电并执行保护；本页面只监测、保存配置和显示 WOULD RUN 演练结果。</p>
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
              <li><span>T+{appliedTiming.totalBudget}</span><div><strong>总关机预算边界</strong><small>以上仅为 DRY-RUN 时间线，不执行命令</small></div></li>
            </ol>
          </div>
        </AppModal>
      )}

      {modal === "simulation" && simulationResult && (
        <AppModal
          title="Dell P7920 断电模拟结果"
          eyebrow={`DRY-RUN · 配置 r${simulationResult.config.revision}`}
          wide
          onClose={() => setModal(null)}
        >
          <div className="modal__body simulation-result">
            <div className="safety-callout">
              <Flask size={25} weight="fill" />
              <div>
                <strong>模拟完成，未执行任何系统命令</strong>
                <p>以下命令来自 PVE 本机纯状态机生成的 WOULD RUN 结果，仅用于核对响应时间。</p>
              </div>
            </div>
            <dl className="simulation-summary">
              <div><dt>场景</dt><dd>NUT 持续报告 OB/LB</dd></div>
              <div><dt>采样间隔</dt><dd>{simulationResult.config.interval_seconds} 秒</dd></div>
              <div><dt>停电确认</dt><dd>{simulationResult.config.nut_confirm_seconds} 秒</dd></div>
              <div><dt>总预算</dt><dd>{simulationResult.config.total_budget_seconds} 秒</dd></div>
            </dl>
            <ol className="drill-timeline simulation-timeline">
              {simulationResult.steps.map((step, index) => (
                <li key={`${step.kind}-${step.at_seconds}-${index}`}>
                  <span>T+{step.at_seconds}</span>
                  <div>
                    <strong>{describeSimulationStep(step.kind)}</strong>
                    <small>{step.reason}</small>
                    {step.would_run?.map((command) => (
                      <code key={command}>WOULD RUN: {command}</code>
                    ))}
                  </div>
                </li>
              ))}
            </ol>
          </div>
        </AppModal>
      )}

      {upsDetail && (
        <AppModal
          title={upsDetail === "load" ? "UPS 负载历史" : "UPS 电池详情"}
          eyebrow={latestUPS?.model || "Synology NUT 实时数据"}
          wide
          onClose={() => setUPSDetail("")}
        >
          <div className="modal__body ups-detail">
            <div className="history-range" role="group" aria-label="历史时间范围">
              {[
                [1, "1 小时"],
                [6, "6 小时"],
                [24, "24 小时"],
              ].map(([hours, label]) => (
                <button
                  key={hours}
                  className={upsHistoryHours === hours ? "history-range__active" : ""}
                  onClick={() => setUPSHistoryHours(hours)}
                >
                  {label}
                </button>
              ))}
              <button
                className="history-refresh"
                onClick={() => refreshUPSHistory(upsHistoryHours)}
                disabled={upsHistoryLoading}
                title="刷新 UPS 历史"
                aria-label="刷新 UPS 历史"
              >
                <ArrowsClockwise className={upsHistoryLoading ? "spin" : ""} size={17} />
              </button>
            </div>

            {upsHistoryError && (
              <p className="config-error"><Warning size={17} weight="fill" />{upsHistoryError}</p>
            )}

            {upsDetail === "load" ? (
              <>
                <div className="ups-metric-strip">
                  <span><small>当前负载</small><strong>{formatDecimal(latestUPS?.ups_load_percent, "%")}</strong></span>
                  <span><small>估算功率</small><strong>{formatDecimal(batteryAssessment?.estimated_load_watts, " W")}</strong></span>
                  <span><small>历史平均</small><strong>{formatDecimal(upsHistoryStats.average, "%")}</strong></span>
                  <span><small>历史峰值</small><strong>{formatDecimal(upsHistoryStats.maximum, "%")}</strong></span>
                  <span><small>额定输出</small><strong>{formatDecimal(latestUPS?.ups_realpower_nominal_watts, " W")}</strong></span>
                </div>
                <UPSHistoryChart points={upsHistory?.points ?? []} />
                <div className="ups-method-note">
                  <Info size={18} />
                  <span>
                    当前 UPS 未报告实时瓦数，因此功率按“额定 {formatDecimal(latestUPS?.ups_realpower_nominal_watts, "W")} ×
                    负载百分比”估算。曲线由 `.99` 每 30 秒采样并保存 1 天。
                  </span>
                </div>
              </>
            ) : (
              <>
                <div className="battery-energy-grid">
                  <section>
                    <small>理论额定能量</small>
                    <strong>{formatEnergy(batteryAssessment?.theoretical_energy_wh)}</strong>
                    <p>{batteryAssessment?.replacement_battery || "电池型号未配置"} · {formatDecimal(latestUPS?.battery_voltage_nominal, " V")}</p>
                  </section>
                  <section>
                    <small>当前运行时估算</small>
                    <strong>{formatEnergy(batteryAssessment?.estimated_usable_energy_wh)}</strong>
                    <p>{batteryAssessment?.energy_estimate_method || "缺少负载功率"} · 非实测放电容量</p>
                  </section>
                  <section>
                    <small>电量与电压</small>
                    <strong>{formatDecimal(latestUPS?.battery_charge, "%")} · {formatDecimal(latestUPS?.battery_voltage, " V")}</strong>
                    <p>预计续航 {formatRuntime(latestUPS?.battery_runtime_seconds)}</p>
                  </section>
                </div>

                <section className={`battery-advice battery-advice--${batteryAssessment?.level || "unknown"}`}>
                  <div>
                    <BatteryCharging size={27} weight="duotone" />
                    <span><small>更换建议</small><strong>{batteryAssessment?.title || "正在评估"}</strong></span>
                  </div>
                  <dl>
                    <div><dt>关机预算</dt><dd>{batteryAssessment?.shutdown_budget_seconds ?? 120} 秒</dd></div>
                    <div><dt>当前余量</dt><dd>{Number.isFinite(batteryAssessment?.runtime_margin_seconds) ? `${batteryAssessment.runtime_margin_seconds} 秒` : "—"}</dd></div>
                    <div><dt>年龄参考</dt><dd>{Number.isFinite(batteryAssessment?.battery_age_years) ? `约 ${batteryAssessment.battery_age_years.toFixed(1)} 年` : "未知"}</dd></div>
                    <div><dt>最近自检</dt><dd>{latestUPS?.self_test_result || "未提供"}</dd></div>
                  </dl>
                </section>

                <ul className="battery-reasons">
                  {(batteryAssessment?.reasons ?? ["等待首个完整 NUT 采样。"]).map((reason) => (
                    <li key={reason}><Info size={16} />{reason}</li>
                  ))}
                </ul>

                <div className="battery-facts">
                  <span><small>UPS 型号</small><strong>{latestUPS?.model || "未提供"}</strong></span>
                  <span><small>电池类型</small><strong>{latestUPS?.battery_type || "未提供"}</strong></span>
                  <span><small>UPS 生产日期</small><strong>{latestUPS?.ups_manufactured_date || "未提供"}</strong></span>
                  <span><small>规格依据</small><strong>{batteryAssessment?.specification_source || "NUT 原始字段"}</strong></span>
                </div>
              </>
            )}
          </div>
        </AppModal>
      )}

      {wakeDevice && (
        <AppModal
          title={`唤醒 ${wakeDevice.name}`}
          eyebrow="真实 WOL 操作"
          onClose={() => !wakeBusy && setWakeDevice(null)}
        >
          <div className="modal__body">
            <div className="wake-callout">
              <Broadcast size={26} weight="duotone" />
              <div>
                <strong>将从 192.168.1.99 发送 Magic Packet</strong>
                <p>{wakeDevice.ip} · {wakeDevice.mac}</p>
              </div>
            </div>
            <dl className="wake-details">
              <div><dt>广播目标</dt><dd>{wakeDevice.broadcast}:{wakeDevice.port}</dd></div>
              <div><dt>发送次数</dt><dd>{wolStatus?.duration_seconds && wolStatus?.interval_seconds ? Math.ceil(wolStatus.duration_seconds / wolStatus.interval_seconds) : 4} 次</dd></div>
              <div><dt>发送窗口</dt><dd>{wolStatus?.duration_seconds ?? 120} 秒</dd></div>
            </dl>
            <p className="wol-disclaimer">
              该操作只负责发送唤醒数据包，不会执行关机，也不能保证目标设备完成启动。
            </p>
            <div className="modal-actions">
              <button className="secondary-button" onClick={() => setWakeDevice(null)} disabled={Boolean(wakeBusy)}>
                取消
              </button>
              <button className="primary-button" onClick={startWake} disabled={Boolean(wakeBusy)}>
                {wakeBusy ? <ArrowsClockwise className="spin" size={18} /> : <Power size={18} weight="fill" />}
                {wakeBusy ? "正在启动任务…" : "确认唤醒"}
              </button>
            </div>
          </div>
        </AppModal>
      )}
    </div>
  );
}
