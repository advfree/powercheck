import { useEffect, useMemo, useState } from "react";
import {
  ArrowRight,
  LockKey,
  ShieldCheck,
  User,
} from "@phosphor-icons/react";
import { App } from "./App.jsx";
import { ThemeControl, themeOptions } from "./ThemeControl.jsx";

function initialTheme() {
  const saved = window.localStorage.getItem("powercheck-theme");
  return themeOptions.some((option) => option.value === saved) ? saved : "system";
}

function resolvedTheme(preference, systemDark) {
  if (preference === "system") return systemDark ? "dark" : "light";
  return preference;
}

async function sessionRequest(method, body) {
  const response = await fetch("/api/v1/session", {
    method,
    credentials: "same-origin",
    headers: body ? { "Content-Type": "application/json" } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  });
  const contentType = response.headers.get("content-type") ?? "";
  if (!contentType.includes("application/json")) {
    throw new Error(`认证服务返回了非 JSON 响应（HTTP ${response.status}）`);
  }
  const payload = await response.json();
  if (!response.ok) {
    throw new Error(payload.error || "登录失败");
  }
  return payload;
}

function LoginPage({ theme, onThemeChange, onAuthenticated }) {
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function submit(event) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      if (import.meta.env.DEV) {
        await new Promise((resolve) => window.setTimeout(resolve, 350));
        if (username !== "admin" || password !== "powercheck-demo") {
          throw new Error("演示账号或密码错误");
        }
        onAuthenticated({
          authenticated: true,
          username: "admin",
          node: "界面演示",
          secure_transport: true,
          demo: true,
        });
        return;
      }
      onAuthenticated(await sessionRequest("POST", { username, password }));
    } catch (requestError) {
      setError(requestError.message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="login-page">
      <header className="login-page__top">
        <div className="brand brand--login">
          <span className="brand__mark"><ShieldCheck size={29} weight="duotone" /></span>
          <span><strong>PowerCheck</strong><small>安全运维控制台</small></span>
        </div>
        <ThemeControl preference={theme} onChange={onThemeChange} />
      </header>

      <section className="login-card" aria-labelledby="login-title">
        <span className="login-card__icon"><LockKey size={28} weight="duotone" /></span>
        <p className="eyebrow">受保护的管理入口</p>
        <h1 id="login-title">登录 PowerCheck</h1>
        <p className="login-card__intro">
          登录后才能查看节点状态和执行 PVE 操作。关机操作仍需要单独确认。
        </p>
        <form onSubmit={submit}>
          <label className="login-field">
            <span>账户</span>
            <span className="login-field__input">
              <User size={19} />
              <input
                name="username"
                autoComplete="username"
                value={username}
                onChange={(event) => setUsername(event.target.value)}
                required
              />
            </span>
          </label>
          <label className="login-field">
            <span>密码</span>
            <span className="login-field__input">
              <LockKey size={19} />
              <input
                name="password"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                required
                autoFocus
              />
            </span>
          </label>
          {error && <p className="login-error" role="alert">{error}</p>}
          <button className="primary-button login-submit" type="submit" disabled={busy}>
            {busy ? "正在验证…" : "登录"}
            {!busy && <ArrowRight size={19} weight="bold" />}
          </button>
        </form>
        {import.meta.env.DEV && (
          <p className="demo-credential">
            本地界面演示：<strong>admin</strong> / <strong>powercheck-demo</strong>
          </p>
        )}
      </section>
      <p className="login-page__footer">请仅通过可信局域网、VPN 或 HTTPS 访问</p>
    </main>
  );
}

export function AuthGate() {
  const media = useMemo(() => window.matchMedia("(prefers-color-scheme: dark)"), []);
  const [systemDark, setSystemDark] = useState(media.matches);
  const [theme, setTheme] = useState(initialTheme);
  const [session, setSession] = useState(null);
  const [checking, setChecking] = useState(!import.meta.env.DEV);

  useEffect(() => {
    function updateSystemTheme(event) {
      setSystemDark(event.matches);
    }
    media.addEventListener("change", updateSystemTheme);
    return () => media.removeEventListener("change", updateSystemTheme);
  }, [media]);

  useEffect(() => {
    const applied = resolvedTheme(theme, systemDark);
    document.documentElement.dataset.theme = applied;
    document.documentElement.style.colorScheme = applied;
    window.localStorage.setItem("powercheck-theme", theme);
  }, [systemDark, theme]);

  useEffect(() => {
    if (import.meta.env.DEV) return undefined;
    let active = true;
    sessionRequest("GET")
      .then((result) => {
        if (active) setSession(result);
      })
      .catch(() => {
        if (active) setSession(null);
      })
      .finally(() => {
        if (active) setChecking(false);
      });
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    function expired() {
      setSession(null);
    }
    window.addEventListener("powercheck:unauthorized", expired);
    return () => window.removeEventListener("powercheck:unauthorized", expired);
  }, []);

  async function logout() {
    if (!session?.demo) {
      try {
        await sessionRequest("DELETE");
      } catch {
        // Clearing the local UI state is still safe if the server already
        // expired or restarted and no longer knows the session.
      }
    }
    setSession(null);
  }

  if (checking) {
    return (
      <main className="auth-loading" aria-live="polite">
        <ShieldCheck size={36} weight="duotone" />
        <span>正在检查登录状态…</span>
      </main>
    );
  }
  if (!session) {
    return (
      <LoginPage
        theme={theme}
        onThemeChange={setTheme}
        onAuthenticated={setSession}
      />
    );
  }
  return (
    <App
      session={session}
      onLogout={logout}
      theme={theme}
      onThemeChange={setTheme}
    />
  );
}
