import { useEffect, useMemo, useState } from "react";
import {
  ArrowsClockwise,
  CheckCircle,
  Cpu,
  Flask,
  HardDrives,
  Monitor,
  Power,
  ShieldCheck,
  TestTube,
  Warning,
  X,
} from "@phosphor-icons/react";

const demoStatus = {
  node: "pve",
  result: {
    action: "status",
    all_guests_stopped: false,
    guests: [
      { vmid: 100, name: "windows", type: "qemu", status: "running", node: "pve" },
      { vmid: 200, name: "dns", type: "lxc", status: "running", node: "pve" },
      { vmid: 300, name: "truenas", type: "qemu", status: "running", node: "pve" },
    ],
  },
};

async function apiRequest(path, body) {
  const options = body
    ? {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-PowerCheck-Action": "confirmed",
        },
        body: JSON.stringify(body),
      }
    : { method: "GET" };
  const response = await fetch(path, options);
  const contentType = response.headers.get("content-type") ?? "";
  if (!contentType.includes("application/json")) {
    throw new Error(`PVE API 返回了非 JSON 响应（HTTP ${response.status}）`);
  }
  const payload = await response.json();
  if (!response.ok) {
    throw new Error(payload.error || `PVE API 请求失败（HTTP ${response.status}）`);
  }
  return payload;
}

function ConfirmDialog({ action, busy, onCancel, onConfirm }) {
  const [checked, setChecked] = useState(false);
  const [countdown, setCountdown] = useState(action.kind === "host-poweroff" ? 5 : 0);

  useEffect(() => {
    if (countdown <= 0) return undefined;
    const timer = window.setTimeout(() => setCountdown((value) => value - 1), 1000);
    return () => window.clearTimeout(timer);
  }, [countdown]);

  const dangerous = action.kind === "host-poweroff";
  const enabled = checked && countdown === 0 && !busy;
  return (
    <div className="operation-backdrop" role="presentation" onMouseDown={onCancel}>
      <section
        className={`operation-dialog ${dangerous ? "operation-dialog--danger" : ""}`}
        role="dialog"
        aria-modal="true"
        aria-labelledby="operation-title"
        onMouseDown={(event) => event.stopPropagation()}
      >
        <header>
          <span className="operation-dialog__icon">
            {dangerous ? <Warning size={25} weight="fill" /> : <ShieldCheck size={25} weight="duotone" />}
          </span>
          <div>
            <small>{dangerous ? "高风险操作" : "真实 PVE 操作"}</small>
            <h3 id="operation-title">{action.title}</h3>
          </div>
          <button className="icon-button" onClick={onCancel} aria-label="取消操作">
            <X size={19} />
          </button>
        </header>
        <div className="operation-dialog__body">
          <p>{action.description}</p>
          <dl>
            <div><dt>节点</dt><dd>{action.node}</dd></div>
            {action.vmid && <div><dt>Guest</dt><dd>{action.vmid} · {action.guestName}</dd></div>}
            <div><dt>后端保护</dt><dd>{action.protection}</dd></div>
          </dl>
          <label className="operation-check">
            <input
              type="checkbox"
              checked={checked}
              onChange={(event) => setChecked(event.target.checked)}
            />
            <span>我已核对目标，确认执行这一次真实操作</span>
          </label>
          {dangerous && countdown > 0 && (
            <p className="operation-countdown">请再检查一次，{countdown} 秒后允许确认</p>
          )}
        </div>
        <footer>
          <button className="secondary-button" onClick={onCancel} disabled={busy}>取消</button>
          <button
            className={dangerous ? "danger-button" : "primary-button"}
            onClick={onConfirm}
            disabled={!enabled}
          >
            {busy && <ArrowsClockwise className="spin" size={18} />}
            {dangerous ? "关闭 PVE 宿主机" : "确认执行"}
          </button>
        </footer>
      </section>
    </div>
  );
}

export function PVEConsole() {
  const [status, setStatus] = useState(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [demo, setDemo] = useState(false);
  const [confirmAction, setConfirmAction] = useState(null);
  const [agentResults, setAgentResults] = useState({});

  const guests = status?.result?.guests ?? [];
  const node = status?.node ?? "未知";
  const running = useMemo(
    () => guests.filter((guest) => !guest.template && guest.status !== "stopped").length,
    [guests],
  );

  async function refresh() {
    setLoading(true);
    setError("");
    try {
      const payload = await apiRequest("/api/v1/status");
      setStatus(payload);
      setDemo(false);
    } catch (requestError) {
      if (import.meta.env.DEV) {
        setStatus(demoStatus);
        setDemo(true);
        setNotice("本地界面演示：尚未连接 PVE API，不会执行真实关机。");
      } else {
        setError(requestError.message);
      }
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    refresh();
  }, []);

  async function runAgentTest(guest) {
    setBusy(`agent-${guest.vmid}`);
    setError("");
    setNotice("");
    if (demo) {
      await new Promise((resolve) => window.setTimeout(resolve, 500));
      setAgentResults((current) => ({ ...current, [guest.vmid]: "success" }));
      setNotice(`VM ${guest.vmid} 的 QEMU Guest Agent 测试成功（界面演示）。`);
      setBusy("");
      return;
    }
    try {
      const payload = await apiRequest("/api/v1/agent-test", { vmid: guest.vmid });
      setAgentResults((current) => ({ ...current, [guest.vmid]: payload.agent_result }));
      setNotice(`VM ${guest.vmid} 的 QEMU Guest Agent 测试成功。`);
    } catch (requestError) {
      setAgentResults((current) => ({ ...current, [guest.vmid]: "failure" }));
      setError(requestError.message);
    } finally {
      setBusy("");
    }
  }

  function openGuestShutdown(guest) {
    setConfirmAction({
      kind: "guest-shutdown",
      title: `安全关闭 Guest ${guest.vmid}`,
      description: "PVE 将向这个 Guest 发送正常关机请求，并在命令结束后重新检查运行状态。",
      protection: "只操作所选 VMID；不会关闭其他 Guest 或宿主机",
      node,
      vmid: guest.vmid,
      guestName: guest.name || guest.type.toUpperCase(),
    });
  }

  function openStopAll() {
    setConfirmAction({
      kind: "stopall",
      title: "安全关闭全部 Guest",
      description: "PVE 将按照自身的关机顺序关闭本节点全部 Guest，超时后不会自动硬停。",
      protection: "固定使用 --force-stop 0；完成后重新读取全部 Guest",
      node,
    });
  }

  function openHostPoweroff() {
    setConfirmAction({
      kind: "host-poweroff",
      title: `关闭宿主机 ${node}`,
      description: "这是最后一步。后端会再次读取 PVE 状态，只要仍有 Guest 运行就拒绝关机。",
      protection: "全部 Guest 必须已经停止；需要额外 5 秒确认",
      node,
    });
  }

  async function executeConfirmed() {
    const action = confirmAction;
    if (!action) return;
    setBusy(action.kind);
    setError("");
    setNotice("");
    if (demo) {
      await new Promise((resolve) => window.setTimeout(resolve, 600));
      if (action.kind === "guest-shutdown") {
        setStatus((current) => ({
          ...current,
          result: {
            ...current.result,
            guests: current.result.guests.map((guest) => (
              guest.vmid === action.vmid ? { ...guest, status: "stopped" } : guest
            )),
          },
        }));
      } else if (action.kind === "stopall") {
        setStatus((current) => ({
          ...current,
          result: {
            ...current.result,
            all_guests_stopped: true,
            guests: current.result.guests.map((guest) => ({ ...guest, status: "stopped" })),
          },
        }));
      }
      setNotice(`${action.title}已完成（界面演示，未执行真实命令）。`);
      setConfirmAction(null);
      setBusy("");
      return;
    }

    const requests = {
      "guest-shutdown": [
        "/api/v1/guest-shutdown",
        { vmid: action.vmid, confirm_vmid: action.vmid },
      ],
      stopall: ["/api/v1/stopall", { confirm_node: node }],
      "host-poweroff": [
        "/api/v1/host-poweroff",
        { confirm_node: node, confirm_poweroff: `POWER OFF ${node}` },
      ],
    };
    const [path, body] = requests[action.kind];
    try {
      await apiRequest(path, body);
      setNotice(`${action.title}已由 PVE 接受并完成状态校验。`);
      setConfirmAction(null);
      if (action.kind !== "host-poweroff") await refresh();
    } catch (requestError) {
      setError(requestError.message);
    } finally {
      setBusy("");
    }
  }

  return (
    <div className="pve-console">
      <section className="pve-console__summary">
        <div>
          <span className={`connection-badge ${error ? "connection-badge--error" : ""}`}>
            {loading ? <ArrowsClockwise className="spin" size={16} /> : <CheckCircle size={16} weight="fill" />}
            {loading ? "读取中" : demo ? "界面演示" : error ? "连接失败" : "已连接真实 PVE"}
          </span>
          <h3>{node}</h3>
          <p>{guests.length} 个 Guest，{running} 个正在运行</p>
        </div>
        <button className="secondary-button compact-button" onClick={refresh} disabled={loading || Boolean(busy)}>
          <ArrowsClockwise size={17} />刷新状态
        </button>
      </section>

      {demo && (
        <div className="console-callout console-callout--demo">
          <Flask size={21} weight="fill" />
          <span>当前是 Windows 本地界面演示。部署到 PVE 后，同一位置会显示真实 Guest。</span>
        </div>
      )}
      {error && (
        <div className="console-callout console-callout--error">
          <Warning size={21} weight="fill" />
          <span>{error}</span>
        </div>
      )}
      {notice && (
        <div className="console-callout console-callout--success">
          <CheckCircle size={21} weight="fill" />
          <span>{notice}</span>
        </div>
      )}

      <div className="guest-console-list">
        {guests.map((guest) => {
          const stopped = guest.status === "stopped";
          const isQEMU = guest.type === "qemu";
          const agentResult = agentResults[guest.vmid];
          return (
            <article className="guest-console-row" key={guest.vmid}>
              <span className="guest-console-row__icon">
                {isQEMU ? <Monitor size={23} /> : <Cpu size={23} />}
              </span>
              <div className="guest-console-row__identity">
                <strong>{guest.vmid} · {guest.name || guest.type.toUpperCase()}</strong>
                <small>{isQEMU ? "QEMU VM" : "LXC 容器"} · {guest.node || node}</small>
              </div>
              <span className={`guest-state ${stopped ? "guest-state--stopped" : "guest-state--running"}`}>
                {stopped ? "已停止" : "运行中"}
              </span>
              <span className="agent-result">
                {agentResult === "success" ? (
                  <><CheckCircle size={16} weight="fill" />Agent 正常</>
                ) : agentResult === "failure" ? (
                  <><Warning size={16} weight="fill" />Agent 不可用</>
                ) : isQEMU ? "尚未测试 Agent" : "LXC 不适用"}
              </span>
              <div className="guest-console-row__actions">
                {isQEMU && !stopped && (
                  <button
                    className="text-action"
                    onClick={() => runAgentTest(guest)}
                    disabled={Boolean(busy)}
                  >
                    {busy === `agent-${guest.vmid}` ? <ArrowsClockwise className="spin" size={16} /> : <TestTube size={16} />}
                    测试 Agent
                  </button>
                )}
                <button
                  className="shutdown-action"
                  onClick={() => openGuestShutdown(guest)}
                  disabled={stopped || Boolean(busy)}
                >
                  <Power size={16} />安全关机
                </button>
              </div>
            </article>
          );
        })}
      </div>

      <section className="node-danger-zone">
        <header>
          <div>
            <HardDrives size={22} />
            <span><strong>节点级测试</strong><small>先关闭全部 Guest，再测试宿主机关机</small></span>
          </div>
          <ShieldCheck size={22} weight="duotone" />
        </header>
        <div>
          <button className="secondary-button" onClick={openStopAll} disabled={running === 0 || Boolean(busy)}>
            <Power size={18} />安全关闭全部 Guest
          </button>
          <button className="danger-button" onClick={openHostPoweroff} disabled={running > 0 || Boolean(busy)}>
            <Warning size={18} weight="fill" />关闭 PVE 宿主机
          </button>
        </div>
        <p>“关闭 PVE 宿主机”仅在页面确认全部 Guest 已停止后可点击，后端仍会再检查一次。</p>
      </section>

      {confirmAction && (
        <ConfirmDialog
          action={confirmAction}
          busy={Boolean(busy)}
          onCancel={() => !busy && setConfirmAction(null)}
          onConfirm={executeConfirmed}
        />
      )}
    </div>
  );
}
