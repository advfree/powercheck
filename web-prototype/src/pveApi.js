export async function pveApiRequest(path, body, requestOptions = {}) {
  const options = body
    ? {
        method: requestOptions.method ?? "POST",
        headers: {
          "Content-Type": "application/json",
          "X-PowerCheck-Action": requestOptions.action ?? "confirmed",
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
  if (response.status === 401) {
    window.dispatchEvent(new CustomEvent("powercheck:unauthorized"));
  }
  if (!response.ok) {
    throw new Error(payload.error || `PVE API 请求失败（HTTP ${response.status}）`);
  }
  return payload;
}
