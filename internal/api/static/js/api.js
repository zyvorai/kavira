// Thin fetch wrapper. Errors carry the HTTP status; 401 on an authed call
// fires `kavira-auth-expired` so the shell can return to the login screen.
export class ApiError extends Error {
  constructor(message, status) {
    super(message);
    this.status = status;
  }
}

export async function api(path, { method = "GET", body, quiet401 = false } = {}) {
  let res;
  try {
    res = await fetch(path, {
      method,
      credentials: "same-origin",
      headers: body === undefined ? {} : { "Content-Type": "application/json" },
      body: body === undefined ? undefined : typeof body === "string" ? body : JSON.stringify(body),
    });
  } catch (_) {
    throw new ApiError("Could not reach the KAVIRA server.", 0);
  }
  if (res.status === 401 && !quiet401) window.dispatchEvent(new Event("kavira-auth-expired"));
  if (!res.ok) {
    let msg = res.statusText || `HTTP ${res.status}`;
    try { msg = (await res.json()).error || msg; } catch (_) { /* non-JSON body */ }
    throw new ApiError(msg, res.status);
  }
  return res.json();
}
