export async function managerApiRequest(path, options = {}) {
  const request = {
    method: options.method ?? "GET",
    headers: {
      Accept: "application/json",
    },
  };
  if (options.body) {
    request.headers["Content-Type"] = "application/json";
    request.body = JSON.stringify(options.body);
  }
  if (options.action) {
    request.headers["X-PowerCheck-Action"] = options.action;
  }

  const response = await fetch(path, request);
  const contentType = response.headers.get("content-type") ?? "";
  if (!contentType.includes("application/json")) {
    throw new Error(`管理器 API 返回了非 JSON 响应（HTTP ${response.status}）`);
  }
  const payload = await response.json();
  if (response.status === 401) {
    window.dispatchEvent(new CustomEvent("powercheck:unauthorized"));
  }
  if (!response.ok) {
    throw new Error(payload.error || `管理器 API 请求失败（HTTP ${response.status}）`);
  }
  return payload;
}
