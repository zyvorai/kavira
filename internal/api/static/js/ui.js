// Rendering helpers. `h` is a tagged template that HTML-escapes every
// interpolation unless it is wrapped in raw() or is itself the result of h``.
class Raw { constructor(s) { this.s = s; } toString() { return this.s; } }
export const raw = (s) => new Raw(s);

export function esc(v) {
  return String(v ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}
function part(v) {
  if (v instanceof Raw) return v.s;
  if (Array.isArray(v)) return v.map(part).join("");
  if (v === null || v === undefined || v === false) return "";
  return esc(v);
}
export function h(strings, ...vals) {
  return new Raw(strings.reduce((out, s, i) => out + s + (i < vals.length ? part(vals[i]) : ""), ""));
}

const VERDICTS = {
  repair_held: "Repair held",
  repair_failed: "Repair failed",
  not_reproduced: "Not reproduced",
  unsupported: "Unsupported",
};
const CLASSES = { container_oom: "Container OOM", network_timeout: "Network timeout", config_regression: "Configuration regression" };

export const sentence = (v) => {
  const t = String(v ?? "").replace(/_/g, " ");
  return t.charAt(0).toUpperCase() + t.slice(1);
};
export const verdictLabel = (v) => VERDICTS[v] || sentence(v || "untested");
export const classLabel = (c) => CLASSES[c] || sentence(c);

export const pill = (v) => h`<span class="pill ${String(v || "untested").replace(/[^a-z_-]/gi, "")}">${verdictLabel(v)}</span>`;

const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
export function relTime(iso) {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "";
  const s = Math.round((t - Date.now()) / 1000);
  for (const [unit, sec] of [["day", 86400], ["hour", 3600], ["minute", 60]]) {
    if (Math.abs(s) >= sec) return rtf.format(Math.round(s / sec), unit);
  }
  return "just now";
}
export const when = (iso) => h`<time datetime="${iso}" title="${iso}">${relTime(iso)}</time>`;

export const bytes = (n) => {
  if (!Number.isFinite(n)) return "";
  const u = ["B", "KiB", "MiB", "GiB"];
  let i = 0;
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return `${+n.toFixed(1)} ${u[i]}`;
};

export const pageHero = ({ eyebrow, title, lede, actions }) => h`
  <header class="page-hero">
    <p class="kicker">${eyebrow}</p>
    <h1 id="page-title" tabindex="-1">${title}</h1>
    ${lede ? h`<p class="lede muted">${lede}</p>` : ""}
    ${actions ? h`<div class="hero-actions">${actions}</div>` : ""}
  </header>`;

export const listEmpty = ({ title, text, action }) => h`
  <div class="card list-empty">
    <h3>${title}</h3><p>${text}</p>${action || ""}
  </div>`;

export const skeleton = (rows = 4) => h`<div class="card" aria-busy="true" aria-label="Loading">${
  Array.from({ length: rows }, (_, i) => h`<div class="skeleton"></div>`)
}</div>`;

export const errorCard = (err) => h`
  <div class="card list-empty" role="alert">
    <h3>Something went wrong</h3><p>${err?.message || "Unexpected error."}</p>
    <button type="button" data-retry>Try again</button>
  </div>`;

export function toast(msg, kind = "info") {
  const stack = document.getElementById("toasts");
  if (!stack) return;
  const el = document.createElement("div");
  el.className = `toast ${kind}`;
  el.textContent = msg;
  stack.append(el);
  setTimeout(() => el.remove(), 4000);
}

// navigator.clipboard only exists in secure contexts (https or localhost). A console served over plain
// http from another host needs the execCommand fallback, or Copy would silently do nothing.
export async function copyText(text) {
  try {
    if (navigator.clipboard && window.isSecureContext) await navigator.clipboard.writeText(text);
    else legacyCopy(text);
    toast("Copied to clipboard");
  } catch (_) {
    toast("Copy failed. Select the text and copy it manually.", "error");
  }
}
function legacyCopy(text) {
  const ta = document.createElement("textarea");
  ta.value = text;
  ta.setAttribute("readonly", "");
  ta.className = "visually-hidden";
  document.body.append(ta);
  ta.select();
  const ok = document.execCommand("copy");
  ta.remove();
  if (!ok) throw new Error("copy command refused");
}

export function download(name, data, type = "application/json") {
  const blob = new Blob([typeof data === "string" ? data : JSON.stringify(data, null, 2) + "\n"], { type });
  const a = document.createElement("a");
  a.href = URL.createObjectURL(blob);
  a.download = name;
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(a.href), 1000);
}
