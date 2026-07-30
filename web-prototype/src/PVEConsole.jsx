import { useEffect, useMemo, useState } from "react";
import {
  ArrowsClockwise,
  CheckCircle,
  Cpu,
  Flask,
  Monitor,
  TestTube,
  Warning,
} from "@phosphor-icons/react";
import { pveApiRequest } from "./pveApi.js";

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

export function PVEConsole() {
  const [status, setStatus] = useState(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [demo, setDemo] = useState(false);
  const [agentResults, setAgentResults] = useState({});

  const guests = status?.result?.guests ?? [];
  const node = status?.node ?? "未知";
  const running = useMemo(
    () => guests.filter((guest) => !guest.template && guest.status !== "stopped").length,
    [guests],
  );
  const qemuGuests = useMemo(
    () => guests.filter((guest) => guest.type === "qemu" && !guest.template),
    [guests],
  );
  const successfulAgents = qemuGuests.filter(
    (guest) => agentResults[guest.vmid] === "success",
  ).length;
  const agentsTested = Object.keys(agentResults).length > 0;

  function applyAgentSnapshot(payload) {
    setAgentResults(Object.fromEntries(
      (payload.results ?? []).map((result) => [result.vmid, result.status]),
    ));
  }

  async function refresh() {
    setLoading(true);
    setError("");
    try {
      const payload = await pveApiRequest("/api/v1/status");
      setStatus(payload);
      setDemo(false);
      try {
        applyAgentSnapshot(await pveApiRequest("/api/v1/agent-status"));
      } catch (agentError) {
        setNotice(`Guest 状态已刷新，但 Agent 检测失败：${agentError.message}`);
      }
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
      const payload = await pveApiRequest("/api/v1/agent-test", { vmid: guest.vmid });
      setAgentResults((current) => ({ ...current, [guest.vmid]: payload.agent_result }));
      setNotice(`VM ${guest.vmid} 的 QEMU Guest Agent 测试成功。`);
    } catch (requestError) {
      setAgentResults((current) => ({ ...current, [guest.vmid]: "failure" }));
      setError(requestError.message);
    } finally {
      setBusy("");
    }
  }

  async function runAllAgentTests() {
    if (demo) {
      const results = Object.fromEntries(
        qemuGuests.map((guest) => [guest.vmid, guest.status === "stopped" ? "stopped" : "success"]),
      );
      setAgentResults(results);
      setNotice("本地演示 Agent 检测已完成；生产环境会调用 PVE 真实接口。");
      return;
    }
    setBusy("agent-all");
    setError("");
    setNotice("");
    try {
      const payload = await pveApiRequest("/api/v1/agent-status");
      applyAgentSnapshot(payload);
      const tested = (payload.results ?? []).filter((result) => result.status !== "stopped");
      const succeeded = tested.filter((result) => result.status === "success").length;
      const failed = tested.length - succeeded;
      setNotice(
        `真实 Agent 检测完成：${succeeded} 台正常${failed ? `，${failed} 台不可用` : ""}。`,
      );
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
        <div className="pve-console__summary-actions">
          <span className="agent-summary">
            {agentsTested
              ? `Agent ${successfulAgents}/${qemuGuests.filter((guest) => guest.status !== "stopped").length} 正常`
              : "Agent 待检测"}
          </span>
          <button className="text-action" onClick={runAllAgentTests} disabled={loading || Boolean(busy)}>
            {busy === "agent-all" ? <ArrowsClockwise className="spin" size={16} /> : <TestTube size={16} />}
            测试全部 Agent
          </button>
          <button className="secondary-button compact-button" onClick={refresh} disabled={loading || Boolean(busy)}>
            <ArrowsClockwise size={17} />刷新状态
          </button>
        </div>
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
                ) : agentResult === "failure" || agentResult === "timeout" ? (
                  <><Warning size={16} weight="fill" />Agent 不可用</>
                ) : agentResult === "stopped" ? "VM 已停止" : isQEMU ? "尚未测试 Agent" : "LXC 不适用"}
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
              </div>
            </article>
          );
        })}
      </div>

      <section className="console-callout">
        <Monitor size={21} />
        <span>此页面只读取 PVE 状态并测试 Agent；停电判断和关机由 PVE 本机完成。</span>
      </section>
    </div>
  );
}
