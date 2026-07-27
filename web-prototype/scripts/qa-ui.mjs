import { spawn } from "node:child_process";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

const chrome =
  process.env.CHROME_PATH ??
  "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe";
const target = process.env.QA_URL ?? "http://127.0.0.1:4173/";
const profile = await mkdtemp(join(tmpdir(), "powercheck-qa-"));
const chromeArguments = [
  "--headless=new",
  "--disable-gpu",
  "--hide-scrollbars",
  "--remote-debugging-port=0",
  `--user-data-dir=${profile}`,
  "--window-size=1440,1024",
  target,
];

if (process.platform === "linux") {
  // GitHub-hosted runners do not provide Chrome's normal desktop sandbox setup.
  chromeArguments.unshift("--disable-dev-shm-usage", "--no-sandbox");
}

const child = spawn(
  chrome,
  chromeArguments,
  { stdio: "ignore", windowsHide: true },
);

const delay = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

async function waitFor(getValue, timeout = 8000) {
  const started = Date.now();
  let lastError;
  while (Date.now() - started < timeout) {
    try {
      const value = await getValue();
      if (value) return value;
    } catch (error) {
      lastError = error;
    }
    await delay(100);
  }
  throw lastError ?? new Error("Timed out waiting for Chrome");
}

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

try {
  const port = await waitFor(async () => {
    const contents = await readFile(join(profile, "DevToolsActivePort"), "utf8");
    return contents.trim().split(/\r?\n/)[0];
  });

  const page = await waitFor(async () => {
    const response = await fetch(`http://127.0.0.1:${port}/json/list`);
    const pages = await response.json();
    return pages.find((item) => item.type === "page" && item.url.startsWith(target));
  });

  const socket = new WebSocket(page.webSocketDebuggerUrl);
  await new Promise((resolve, reject) => {
    socket.addEventListener("open", resolve, { once: true });
    socket.addEventListener("error", reject, { once: true });
  });

  let nextId = 0;
  const pending = new Map();
  const consoleErrors = [];
  socket.addEventListener("message", ({ data }) => {
    const message = JSON.parse(data);
    if (message.id && pending.has(message.id)) {
      const { resolve, reject } = pending.get(message.id);
      pending.delete(message.id);
      if (message.error) reject(new Error(message.error.message));
      else resolve(message.result);
      return;
    }
    if (message.method === "Runtime.exceptionThrown") {
      consoleErrors.push(message.params.exceptionDetails.text);
    }
    if (
      message.method === "Log.entryAdded" &&
      ["error", "warning"].includes(message.params.entry.level)
    ) {
      consoleErrors.push(message.params.entry.text);
    }
  });

  const send = (method, params = {}) =>
    new Promise((resolve, reject) => {
      const id = ++nextId;
      pending.set(id, { resolve, reject });
      socket.send(JSON.stringify({ id, method, params }));
    });

  const evaluate = async (expression) => {
    const result = await send("Runtime.evaluate", {
      expression,
      awaitPromise: true,
      returnByValue: true,
    });
    if (result.exceptionDetails) {
      throw new Error(
        result.exceptionDetails.exception?.description ??
          result.exceptionDetails.text,
      );
    }
    return result.result.value;
  };

  await send("Runtime.enable");
  await send("Log.enable");
  await send("Page.enable");
  await waitFor(() => evaluate("document.readyState === 'complete'"));
  await waitFor(() => evaluate("Boolean(document.querySelector('.mode-pill'))"));
  await send("Emulation.setDeviceMetricsOverride", {
    width: 1440,
    height: 1024,
    deviceScaleFactor: 1,
    mobile: false,
  });
  await delay(150);

  const checks = [];
  const desktopCapture = await send("Page.captureScreenshot", {
    format: "png",
    fromSurface: true,
  });
  await writeFile(
    join(process.cwd(), "qa-desktop.png"),
    Buffer.from(desktopCapture.data, "base64"),
  );

  await evaluate("document.querySelector('.mode-pill').click()");
  assert(
    await waitFor(() => evaluate("document.querySelector('#modal-title')?.textContent")),
    "DRY-RUN modal did not open",
  );
  checks.push("DRY-RUN safety modal");
  await evaluate("document.querySelector('.modal .icon-button').click()");

  await evaluate("document.querySelector('.secondary-button').click()");
  const drillTitle = await waitFor(() =>
    evaluate("document.querySelector('#modal-title')?.textContent"),
  );
  assert(drillTitle.includes("停电演练"), "Drill modal content is incorrect");
  checks.push("drill detail modal");
  await evaluate("document.querySelector('.modal .icon-button').click()");

  await evaluate("document.querySelector('.node-row--data').click()");
  const drawerTitle = await waitFor(() =>
    evaluate("document.querySelector('.drawer h2')?.textContent"),
  );
  assert(drawerTitle === "Dell P7920", "Node detail drawer is incorrect");
  await evaluate("document.querySelector('.drawer__content').click()");
  assert(
    await evaluate("Boolean(document.querySelector('.drawer'))"),
    "Clicking inside the drawer closed it",
  );
  await evaluate(
    "document.querySelector('.drawer-backdrop').dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))",
  );
  await waitFor(() => evaluate("!document.querySelector('.drawer')"));
  checks.push("PVE node drawer and outside-click close");

  await evaluate("document.querySelector('.node-row--data').click()");
  await waitFor(() => evaluate("Boolean(document.querySelector('.drawer'))"));
  await evaluate(
    "window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))",
  );
  await waitFor(() => evaluate("!document.querySelector('.drawer')"));
  checks.push("drawer Escape close");

  await evaluate(
    "Array.from(document.querySelectorAll('.nav-item')).find((item) => item.textContent.includes('Guest 检测')).click()",
  );
  await waitFor(() => evaluate("Boolean(document.querySelector('.pve-console'))"));
  await waitFor(() =>
    evaluate("document.querySelector('.connection-badge')?.textContent.includes('界面演示')"),
  );
  assert(
    await evaluate("document.querySelectorAll('.guest-console-row').length === 3"),
    "Guest console did not show the demo PVE inventory",
  );
  await evaluate("document.querySelector('.text-action').click()");
  await waitFor(() =>
    evaluate("document.querySelector('.console-callout--success')?.textContent.includes('Agent 测试成功')"),
  );
  await evaluate("document.querySelector('.shutdown-action').click()");
  await waitFor(() => evaluate("Boolean(document.querySelector('.operation-dialog'))"));
  assert(
    await evaluate("document.querySelector('.operation-dialog h3').textContent.includes('Guest 100')"),
    "Guest shutdown confirmation targets the wrong VMID",
  );
  await evaluate("document.querySelector('.operation-check input').click()");
  await evaluate("document.querySelector('.operation-dialog .primary-button').click()");
  await waitFor(() =>
    evaluate("document.querySelector('.guest-console-row .guest-state').textContent.includes('已停止')"),
  );
  await evaluate("document.querySelector('.node-danger-zone .secondary-button').click()");
  await waitFor(() => evaluate("Boolean(document.querySelector('.operation-dialog'))"));
  await evaluate("document.querySelector('.operation-check input').click()");
  await evaluate("document.querySelector('.operation-dialog .primary-button').click()");
  await waitFor(() =>
    evaluate("Array.from(document.querySelectorAll('.guest-state')).every((item) => item.textContent.includes('已停止'))"),
  );
  await evaluate("document.querySelector('.node-danger-zone .danger-button').click()");
  await waitFor(() => evaluate("Boolean(document.querySelector('.operation-dialog--danger'))"));
  assert(
    await evaluate("document.querySelector('.operation-dialog--danger .danger-button').disabled"),
    "Host poweroff confirmation was immediately enabled",
  );
  await evaluate("document.querySelector('.operation-dialog .icon-button').click()");
  await waitFor(() => evaluate("!document.querySelector('.operation-dialog')"));
  checks.push("PVE Guest web tests and guarded host poweroff");
  await evaluate(
    "document.querySelector('.drawer-backdrop').dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))",
  );
  await waitFor(() => evaluate("!document.querySelector('.drawer')"));

  await evaluate(
    "Array.from(document.querySelectorAll('.nav-item')).find((item) => item.textContent.includes('设置')).click()",
  );
  await waitFor(() => evaluate("Boolean(document.querySelector('.config-panel'))"));
  assert(
    await evaluate("document.querySelector('.local-config-callout').textContent.includes('PVE 本地')"),
    "Settings do not explain local PVE execution",
  );
  await evaluate(`(() => {
    const input = document.querySelectorAll('.config-grid input')[3];
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;
    setter.call(input, '360');
    input.dispatchEvent(new Event('input', { bubbles: true }));
  })()`);
  await waitFor(() =>
    evaluate("document.querySelector('.timeline-preview').textContent.includes('T+300')"),
  );
  assert(
    await evaluate("document.querySelectorAll('.status-item strong')[3].textContent.includes('300')"),
    "Unapplied draft changed the active overview budget",
  );
  const settingsCapture = await send("Page.captureScreenshot", {
    format: "png",
    fromSurface: true,
  });
  await writeFile(
    join(process.cwd(), "qa-settings.png"),
    Buffer.from(settingsCapture.data, "base64"),
  );
  await evaluate("document.querySelector('.config-panel .primary-button').click()");
  await waitFor(() =>
    evaluate("document.querySelector('.notice')?.textContent.includes('配置已下发')"),
  );
  await waitFor(() =>
    evaluate("document.querySelectorAll('.status-item strong')[3].textContent.includes('360')"),
  );
  assert(
    await evaluate("document.querySelector('.config-file > strong').textContent.includes('2 台 PVE')"),
    "Applied configuration status was not updated",
  );
  checks.push("editable timing configuration and local PVE apply feedback");
  await evaluate(
    "document.querySelector('.drawer-backdrop').dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))",
  );
  await waitFor(() => evaluate("!document.querySelector('.drawer')"));

  await evaluate("document.querySelector('.primary-button').click()");
  const scanning = await evaluate(
    "document.querySelector('.primary-button').textContent.includes('检测中')",
  );
  assert(scanning, "Scan did not enter its loading state");
  const notice = await waitFor(
    () => evaluate("document.querySelector('.notice')?.textContent.includes('检测完成')"),
    4000,
  );
  assert(notice, "Scan did not finish");
  checks.push("manual scan loading and success states");

  const desktopOverflow = await evaluate(
    "document.documentElement.scrollWidth > window.innerWidth",
  );
  assert(!desktopOverflow, "Desktop viewport has horizontal overflow");
  checks.push("desktop viewport overflow");

  await send("Page.reload", { ignoreCache: true });
  await delay(400);
  await waitFor(() => evaluate("Boolean(document.querySelector('.mode-pill'))"));
  await send("Emulation.setDeviceMetricsOverride", {
    width: 390,
    height: 844,
    deviceScaleFactor: 1,
    mobile: true,
  });
  await delay(150);
  const mobileLayout = await evaluate(`({
    overflow: document.documentElement.scrollWidth > window.innerWidth,
    sidebarBottom: getComputedStyle(document.querySelector('.sidebar')).bottom,
    firstNodeWidth: document.querySelector('.node-row--data').getBoundingClientRect().width,
    viewport: window.innerWidth
  })`);
  assert(!mobileLayout.overflow, "Mobile viewport has horizontal overflow");
  assert(mobileLayout.sidebarBottom === "0px", "Mobile bottom navigation is not active");
  assert(
    mobileLayout.firstNodeWidth <= mobileLayout.viewport,
    "Mobile node cards exceed the viewport",
  );
  const mobileCapture = await send("Page.captureScreenshot", {
    format: "png",
    fromSurface: true,
  });
  await writeFile(
    join(process.cwd(), "qa-mobile.png"),
    Buffer.from(mobileCapture.data, "base64"),
  );
  checks.push("mobile responsive layout");

  assert(consoleErrors.length === 0, `Browser console issues: ${consoleErrors.join("; ")}`);
  checks.push("browser console");

  console.log(
    JSON.stringify(
      {
        result: "passed",
        target,
        checks,
        consoleErrors,
      },
      null,
      2,
    ),
  );
  socket.close();
} finally {
  child.kill();
  await delay(500);
  await rm(profile, {
    recursive: true,
    force: true,
    maxRetries: 5,
    retryDelay: 200,
  }).catch(() => {
    // Chrome can briefly retain a spelling dictionary file on Windows.
    // The operating system will clear this disposable temp profile later.
  });
}
