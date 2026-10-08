import { api, ApiError } from "./api.js";
import { parse, href, go } from "./router.js";
import * as pages from "./pages.js";
import { h, pill, toast, copyText, download, errorCard, skeleton } from "./ui.js";

const $ = (id) => document.getElementById(id);
const ctx = {
  incidents: [], audit: [], chainOK: true, fixtures: [], selected: null, current: null,
  filter: { q: "", verdict: "", sort: "id", dir: "asc" },
};
let authed = false;
let renderToken = 0;

/* ---------- shell: login vs app ---------- */
function showLogin(notice) {
  authed = false;
  $("app").hidden = true;
  $("login").hidden = false;
  const err = $("login-error");
  err.hidden = !notice;
  err.textContent = notice || "";
  $("login-form").elements.password.value = "";
  $("login-form").elements.username.focus();
}
function showApp() {
  $("login").hidden = true;
  $("app").hidden = false;
  authed = true;
}

$("host-line").textContent = `Connecting to ${location.host}`;
window.KaviraTheme.sync();
$("theme").addEventListener("click", () => window.KaviraTheme.toggle());

window.addEventListener("kavira-auth-expired", () => {
  if (authed) showLogin("Your session expired. Sign in again.");
});

$("login-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const btn = e.submitter || e.target.querySelector("button[type=submit]");
  const err = $("login-error");
  err.hidden = true;
  btn.disabled = true;
  btn.textContent = "Signing in…";
  const f = e.target.elements;
  try {
    await api("/api/v1/session", { method: "POST", body: { username: f.username.value.trim(), password: f.password.value }, quiet401: true });
    await start();
  } catch (ex) {
    err.hidden = false;
    err.textContent = ex.status === 401 ? "Wrong username or password."
      : ex.status === 0 ? `Could not reach the KAVIRA server at ${location.host}.`
      : ex.message;
    f.password.select();
  } finally {
    btn.disabled = false;
    btn.textContent = "Sign in";
  }
});

$("logout").addEventListener("click", async () => {
  try { await api("/api/v1/logout", { method: "POST", body: {} }); } catch (_) { /* leaving anyway */ }
  location.hash = "";
  showLogin();
});

/* ---------- data ---------- */
async function loadData() {
  const [inc, aud, fx] = await Promise.all([api("/api/v1/incidents"), api("/api/v1/audit"), api("/api/v1/fixtures")]);
  ctx.incidents = inc.incidents || [];
  ctx.audit = aud.audit || [];
  ctx.chainOK = aud.chain_ok !== false;
  ctx.fixtures = fx.fixtures || [];
  if (!ctx.incidents.some((i) => i.id === ctx.selected)) ctx.selected = ctx.incidents[0]?.id || null;
}

async function compile(payload) {
  const btn = document.activeElement;
  if (btn?.tagName === "BUTTON") { btn.disabled = true; btn.dataset.label = btn.textContent; btn.textContent = "Compiling…"; }
  try {
    const out = await api("/api/v1/compile", { method: "POST", body: payload });
    await loadData();
    ctx.selected = out.incident.id;
    toast(`${out.incident.id}: ${out.incident.verdict.replace(/_/g, " ")}`, "success");
    go(href.incident(out.incident.id));
    if (location.hash === href.incident(out.incident.id)) render();
  } catch (ex) {
    toast(ex.message, "error");
    throw ex;
  } finally {
    if (btn?.tagName === "BUTTON" && btn.dataset.label) { btn.disabled = false; btn.textContent = btn.dataset.label; }
  }
}

/* ---------- routing + render ---------- */
function setNav(route) {
  const sel = ctx.selected;
  const tabLink = (tab) => (sel ? href.incident(sel, tab) : href.incidents);
  const links = {
    overview: href.overview, incidents: href.incidents, evidence: tabLink("evidence"),
    experiments: tabLink("experiments"), repairs: tabLink("repairs"), audit: href.audit,
  };
  document.querySelectorAll("nav a[data-nav]").forEach((a) => {
    const k = a.dataset.nav;
    a.setAttribute("href", links[k]);
    const on = route.page === k || (route.page === "incident" && route.tab === k);
    if (on) a.setAttribute("aria-current", "page"); else a.removeAttribute("aria-current");
  });
  const chip = $("selected-chip");
  const cur = ctx.incidents.find((i) => i.id === sel);
  chip.hidden = !cur;
  if (cur) { chip.setAttribute("href", href.incident(cur.id)); chip.querySelector("span").textContent = cur.id; }
}

async function render() {
  if (!authed) return;
  const route = parse();
  const token = ++renderToken;
  if (route.page === "incident") ctx.selected = route.id;
  setNav(route);
  const main = $("main");
  main.setAttribute("aria-busy", "true");
  main.innerHTML = skeleton(5).toString();
  let html;
  let title = "KAVIRA";
  try {
    if (route.page === "overview") { html = pages.overview(ctx); title = "Overview"; }
    else if (route.page === "incidents") { html = pages.incidents(ctx); title = "Incidents"; }
    else if (route.page === "audit") { await loadData(); html = pages.audit(ctx); title = "Audit"; }
    else if (route.page === "incident") {
      const out = await pages.incident(ctx, route);
      html = out.html; title = out.incident ? `${out.incident.id} · ${route.tab}` : "Not found";
      if (out.incident && !ctx.incidents.some((i) => i.id === out.incident.id)) await loadData();
    } else { html = pages.notFound(); title = "Not found"; }
  } catch (ex) {
    if (token !== renderToken) return;
    html = errorCard(ex); title = "Error";
  }
  if (token !== renderToken) return; // a newer navigation won
  setNav(route);
  main.innerHTML = html.toString();
  main.removeAttribute("aria-busy");
  document.title = `${title} · KAVIRA`;
  const hd = $("page-title");
  if (hd) hd.focus({ preventScroll: true });
  window.scrollTo(0, 0);
}
window.addEventListener("hashchange", render);

function refreshTable() {
  const box = $("inc-table");
  if (box) box.innerHTML = pages.incidentRows(ctx).toString();
}

/* ---------- delegated events ---------- */
document.addEventListener("click", async (e) => {
  const t = e.target.closest("[data-compile],[data-open-bundle],[data-copy],[data-copy-text],[data-download],[data-sort],[data-clear-filter],[data-retry],tr[data-href]");
  if (!t) return;
  const d = t.dataset;
  if (d.compile) compile({ fixture: d.compile }).catch(() => {});
  else if ("openBundle" in d) openBundle();
  else if (d.copy) copyText($(d.copy).textContent);
  else if (d.copyText) copyText(d.copyText);
  else if (d.download) {
    const r = ctx.current?.repair;
    if (!r) return;
    if (d.download === "test") download("regression_test.json", r.regression_test);
    else download(`${ctx.current.id}-repair-package.json`, { id: ctx.current.id, verdict: ctx.current.verdict, patch: r.patch, regression_test: r.regression_test, reproduce: ctx.current.reproduce, instructions: r.instructions });
  } else if (d.sort) {
    const f = ctx.filter;
    f.dir = f.sort === d.sort && f.dir === "asc" ? "desc" : "asc";
    f.sort = d.sort;
    refreshTable();
  } else if ("clearFilter" in d) {
    ctx.filter.q = ""; ctx.filter.verdict = "";
    $("inc-q").value = ""; $("inc-v").value = "";
    refreshTable();
  } else if ("retry" in d) render();
  else if (d.href && !e.target.closest("a,button")) location.hash = d.href;
});
document.addEventListener("input", (e) => {
  if (e.target.id === "inc-q") { ctx.filter.q = e.target.value; refreshTable(); }
});
document.addEventListener("change", (e) => {
  if (e.target.id === "inc-v") { ctx.filter.verdict = e.target.value; refreshTable(); }
});

/* ---------- bundle dialog ---------- */
function openBundle() {
  $("bundle-error").hidden = true;
  $("bundle-dialog").showModal();
  $("bundle-text").focus();
}
$("bundle-file").addEventListener("change", async (e) => {
  const file = e.target.files[0];
  if (file) $("bundle-text").value = await file.text();
});
$("bundle-cancel").addEventListener("click", () => $("bundle-dialog").close());
$("bundle-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const err = $("bundle-error");
  let parsed;
  try { parsed = JSON.parse($("bundle-text").value); } catch (ex) {
    err.hidden = false; err.textContent = `Not valid JSON: ${ex.message}`; return;
  }
  try {
    await compile(parsed);
    $("bundle-dialog").close();
  } catch (ex) {
    err.hidden = false; err.textContent = ex.message;
  }
});

/* ---------- keyboard shortcuts ---------- */
let chord = false;
document.addEventListener("keydown", (e) => {
  if (!authed || e.metaKey || e.ctrlKey || e.altKey) return;
  if (e.target.closest("input,textarea,select,dialog[open]")) return;
  if (chord) {
    chord = false;
    const to = { o: href.overview, i: href.incidents, a: href.audit };
    const sel = ctx.selected;
    if (sel) Object.assign(to, { e: href.incident(sel, "evidence"), x: href.incident(sel, "experiments"), r: href.incident(sel, "repairs") });
    if (to[e.key]) { e.preventDefault(); go(to[e.key]); }
    return;
  }
  if (e.key === "g") { chord = true; setTimeout(() => (chord = false), 1200); }
  else if (e.key === "/") {
    e.preventDefault();
    if (parse().page !== "incidents") go(href.incidents);
    setTimeout(() => $("inc-q")?.focus(), 50);
  } else if (e.key === "?") $("shortcuts-dialog").showModal();
});
document.querySelectorAll("[data-close-dialog]").forEach((b) => b.addEventListener("click", () => b.closest("dialog").close()));

/* ---------- boot ---------- */
async function start() {
  await loadData();
  showApp();
  if (!location.hash) location.hash = href.overview; else render();
  if (location.hash === href.overview) render();
}

async function boot() {
  try {
    const health = await api("/api/v1/health", { quiet401: true });
    $("demo-hint").hidden = !health.demo_gate;
  } catch (_) { /* login screen reports reachability on submit */ }
  try {
    const who = await api("/api/v1/session", { quiet401: true });
    if (who.authenticated) await start(); else showLogin();
  } catch (ex) {
    showLogin(ex instanceof ApiError && ex.status === 0 ? `Could not reach the KAVIRA server at ${location.host}.` : "");
  }
}
boot();
