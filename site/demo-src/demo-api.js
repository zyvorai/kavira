// Replaces js/api.js in the published demo. The rest of the console is the real code, unchanged.
// Every response comes from data/*.json, which build-site.sh records from the real API of a local server.
// Same exports as the real js/api.js: ApiError and api().
export class ApiError extends Error {
  constructor(message, status) {
    super(message);
    this.status = status;
  }
}

// This file is published as demo/js/api.js, so the recorded data is one level up.
const data = new URL("../data/", import.meta.url);

async function load(name) {
  let res;
  try { res = await fetch(new URL(name, data)); } catch (_) { throw new ApiError("Could not load the demo data.", 0); }
  if (!res.ok) throw new ApiError(res.status === 404 ? "not found" : res.statusText, res.status);
  return res.json();
}

export async function api(path, { method = "GET", body } = {}) {
  const p = String(path).replace(/^\/?api\/v1\//, "");
  if (p === "session") return method === "GET" ? { authenticated: true } : { user: "admin" };
  if (p === "health") return load("health.json");
  if (p === "logout") {
    location.assign(new URL("../", location.href).href); // back to the landing page
    return new Promise(() => {});
  }
  if (p === "incidents") return load("incidents.json");
  if (p === "audit") return load("audit.json");
  if (p === "fixtures") return load("fixtures.json");
  if (p.startsWith("incidents/")) {
    try { return await load(`incident-${encodeURIComponent(decodeURIComponent(p.slice("incidents/".length)))}.json`); }
    catch (e) { throw e.status === 404 ? new ApiError("incident not found", 404) : e; }
  }
  if (p === "compile" && method === "POST") {
    const name = typeof body === "object" && body ? body.fixture : undefined;
    const { fixtures } = await load("fixtures.json");
    const fx = name && fixtures.find((f) => f.name === name);
    if (!fx) throw new ApiError("The static demo only has the three bundled examples. To compile your own bundle, run kavira serve locally.", 400);
    return load(`incident-${fx.id}.json`);
  }
  throw new ApiError("not found", 404);
}

// A slim banner, so nobody mistakes this for a running server.
const style = document.createElement("style");
style.textContent = `.demo-banner{display:flex;gap:12px;align-items:center;justify-content:center;flex-wrap:wrap;
  padding:8px 14px;font:500 13px/1.4 var(--font-display,system-ui);background:var(--info-bg,#eaf6fc);color:var(--info-text,#0a6b99);border-bottom:1px solid var(--hairline,#0001)}
  .demo-banner a{color:inherit;font-weight:650}`;
document.head.append(style);
const banner = document.createElement("div");
banner.className = "demo-banner";
banner.setAttribute("role", "note");
banner.innerHTML = 'Static demo: the real KAVIRA console on precompiled results. <a href="../">About KAVIRA</a> <a href="../#quickstart">Run it yourself</a>';
document.body.prepend(banner);
