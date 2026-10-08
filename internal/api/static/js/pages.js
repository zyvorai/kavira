import { api } from "./api.js";
import { href } from "./router.js";
import { h, raw, pill, when, relTime, classLabel, verdictLabel, sentence, bytes, pageHero, listEmpty, skeleton } from "./ui.js";

const EXPLAIN = {
  repair_held: "The original condition reproduced the symptom, and with one factor changed the repair arm held within the bound. A repair package was written.",
  repair_failed: "The symptom reproduced, but the repair arm did not hold. No repair package is written, because KAVIRA does not invent a repair that execution did not confirm.",
  not_reproduced: "The captured condition did not reproduce the symptom, so the run stopped. No repair package: there is nothing to repair yet.",
  unsupported: "This incident class is outside the three v0 classes. KAVIRA reports that instead of guessing.",
};

export function overview(ctx) {
  const n = (v) => ctx.incidents.filter((i) => i.verdict === v).length;
  const known = new Map(ctx.incidents.map((i) => [i.id, i]));
  return h`
    <section class="page-hero story">
      <p class="kicker">Zyvor · execution is the verdict</p>
      <h1 id="page-title" tabindex="-1">Turn an outage into a reproducible test.</h1>
      <p class="lede muted">KAVIRA compiles captured evidence into an isolated reproducer, one-factor experiments, and a repair package. The proposer lists explanations; only execution decides. Exact replay is not claimed.</p>
      <div class="hero-actions"><button type="button" class="primary" data-open-bundle>Compile your own bundle</button>
        <a class="text-link" href="${href.incidents}">View incidents</a></div>
    </section>
    <div class="metric-band" role="group" aria-label="Summary">
      <div><span>Incidents</span><b>${ctx.incidents.length}</b></div>
      <div><span>Repair held</span><b>${n("repair_held")}</b></div>
      <div><span>Not reproduced</span><b>${n("not_reproduced")}</b></div>
      <div><span>Repair failed</span><b>${n("repair_failed")}</b></div>
      <div><span>Audit entries</span><b>${ctx.audit.length}</b></div>
    </div>
    <h2 class="section-title">Compile an example</h2>
    <div class="grid">${ctx.fixtures.map((f) => {
      const done = known.get(f.id);
      return h`<article class="card">
        <p class="kicker">${classLabel(f.class)}</p>
        <h3>${f.service} <span class="muted">· ${f.id}</span></h3>
        <p>${f.symptom}</p>
        <div class="row">${done ? pill(done.verdict) : h`<span class="muted small">Not compiled yet</span>`}
          <button type="button" data-compile="${f.name}">Compile</button></div>
      </article>`;
    })}</div>
    <p class="banner">Neighbors: Zyntra ranks a human-approved action, Verixa rehearses simulated agent actions, KubeFlight preflights a deploy. KAVIRA reconstructs a failure that already happened.</p>`;
}

export function incidentRows(ctx) {
  const { q, verdict, sort, dir } = ctx.filter;
  const needle = q.trim().toLowerCase();
  let rows = ctx.incidents.filter((i) =>
    (!verdict || i.verdict === verdict) &&
    (!needle || [i.id, i.service, i.class, i.summary, i.symptom].join(" ").toLowerCase().includes(needle)));
  rows = rows.slice().sort((a, b) => String(a[sort] ?? "").localeCompare(String(b[sort] ?? "")) * (dir === "desc" ? -1 : 1));
  if (!rows.length) {
    return h`<div class="list-empty">${ctx.incidents.length
      ? h`<h3>No incidents match</h3><p>Clear the search or the verdict filter.</p><button type="button" data-clear-filter>Clear filters</button>`
      : h`<h3>No incidents yet</h3><p>Compile an example or paste your own evidence bundle.</p><button type="button" class="primary" data-open-bundle>Compile a bundle</button>`}</div>`;
  }
  const th = (key, label) => h`<th scope="col" aria-sort="${sort === key ? (dir === "desc" ? "descending" : "ascending") : "none"}"><button type="button" class="sort" data-sort="${key}">${label}${sort === key ? (dir === "desc" ? " ↓" : " ↑") : ""}</button></th>`;
  return h`<div class="table-wrap"><table>
    <caption class="visually-hidden">Incidents</caption>
    <thead><tr>${th("id", "ID")}${th("service", "Service")}${th("class", "Class")}${th("verdict", "Verdict")}<th scope="col">Summary</th></tr></thead>
    <tbody>${rows.map((i) => h`<tr class="clickable" data-href="${href.incident(i.id)}">
      <td><a href="${href.incident(i.id)}">${i.id}</a></td><td>${i.service}</td><td>${classLabel(i.class)}</td><td>${pill(i.verdict)}</td><td>${i.summary}</td>
    </tr>`)}</tbody></table></div>`;
}

export function incidents(ctx) {
  const f = ctx.filter;
  const verdicts = ["repair_held", "repair_failed", "not_reproduced", "unsupported"];
  return h`${pageHero({ eyebrow: "Browse", title: "Incidents", lede: "Every incident compiled in this process. Sort by any column, or search." })}
    <div class="toolbar">
      <label class="visually-hidden" for="inc-q">Search incidents</label>
      <input id="inc-q" class="input-field grow" type="search" placeholder="Search id, service, class, summary" value="${f.q}" autocomplete="off" />
      <label class="visually-hidden" for="inc-v">Filter by verdict</label>
      <select id="inc-v" class="input-field">
        <option value="">All verdicts</option>
        ${verdicts.map((v) => h`<option value="${v}" ${f.verdict === v ? raw("selected") : ""}>${verdictLabel(v)}</option>`)}
      </select>
    </div>
    <div class="card" id="inc-table">${incidentRows(ctx)}</div>`;
}

const TABS = [["evidence", "Evidence"], ["experiments", "Experiments"], ["repairs", "Repairs"]];

function incidentHead(i, tab) {
  return h`<nav class="crumbs" aria-label="Breadcrumb"><a href="${href.incidents}">Incidents</a> <span aria-hidden="true">/</span> <span>${i.id}</span></nav>
    <header class="page-hero">
      <p class="kicker">${classLabel(i.class)} · ${i.service}</p>
      <h1 id="page-title" tabindex="-1">${i.id} ${pill(i.verdict)}</h1>
      <p class="lede muted">${i.symptom}</p>
    </header>
    <div class="tabs" role="navigation" aria-label="Incident sections">${TABS.map(([k, label]) =>
      h`<a href="${href.incident(i.id, k)}" ${k === tab ? raw('aria-current="page"') : ""}>${label}</a>`)}</div>`;
}

const kv = (rows) => h`<dl class="kv">${rows.filter(([, v]) => v !== undefined && v !== "" && v !== 0).map(([k, v]) => h`<dt>${k}</dt><dd>${v}</dd>`)}</dl>`;

function evidence(i, b) {
  const ev = b?.evidence || {};
  return h`<div class="split">
    <div class="stack">
      <div class="card"><p class="kicker">Authority</p><h3>${sentence(i.authority)} decides</h3>
        <p>${i.summary}</p>
        ${kv([["Exact replay", i.exact_replay ? "Yes" : "No, reconstructed"], ["Compiled", when(i.compiled_at)]])}</div>
      <div class="card"><p class="kicker">Captured evidence</p>
        ${kv([
          ["Memory limit", ev.memory_limit_bytes ? bytes(ev.memory_limit_bytes) : ""],
          ["Observed peak", ev.observed_peak_bytes ? bytes(ev.observed_peak_bytes) : ""],
          ["Restarts", ev.restart_count],
          ["Observed p99", ev.p99_ms ? `${ev.p99_ms} ms` : ""],
          ["Deploy change", ev.deploy_change],
        ])}
        ${ev.notes?.length ? h`<ul>${ev.notes.map((n) => h`<li>${n}</li>`)}</ul>` : ""}</div>
    </div>
    <div class="stack">
      <div class="card"><p class="kicker">Competing explanations</p>
        <p class="small">The proposer is not a verdict. Each explanation names the experiment that would test it.</p>
        <ul class="hyp">${(i.hypotheses || []).map((x) => h`<li><strong>${x.name}</strong> ${pill(x.status)}<br>${x.explanation}<br>
          <span class="muted small">${x.experiment} Authoritative: ${x.authoritative ? "yes" : "no"}</span></li>`)}</ul></div>
      ${Object.keys(i.measurements || {}).length ? h`<div class="card"><p class="kicker">Measurements</p>${kv(Object.entries(i.measurements).map(([k, v]) => [sentence(k), String(v)]))}</div>` : ""}
    </div>
  </div>`;
}

function experiments(i) {
  const rows = i.experiments || [];
  return h`<p class="banner ${i.verdict === "repair_held" ? "" : "warn"}"><strong>${verdictLabel(i.verdict)}.</strong> ${EXPLAIN[i.verdict] || ""}</p>
    <div class="card">${rows.length ? h`<div class="table-wrap"><table>
      <caption class="visually-hidden">Experiment arms for ${i.id}</caption>
      <thead><tr><th scope="col">Arm</th><th scope="col">Factor</th><th scope="col">Change</th><th scope="col">Result</th><th scope="col">Failures</th><th scope="col">p99 ms</th><th scope="col">Detail</th></tr></thead>
      <tbody>${rows.map((e) => h`<tr><td>${e.name}</td><td>${e.factor}</td><td>${e.changed}</td><td>${pill(e.result)}</td><td>${e.failures}/${e.attempts}</td><td>${e.p99_ms}</td><td><code>${e.detail}</code></td></tr>`)}</tbody></table></div>`
      : h`<div class="list-empty"><h3>No experiments ran</h3><p>The run stopped before any arm executed.</p></div>`}</div>
    <p class="muted small">One factor changes between the original arm and a repair arm. Pass or fail comes from the runner, not from the proposer.</p>`;
}

function repairs(i) {
  if (!i.repair) {
    return h`<div class="card list-empty"><h3>No repair package</h3>
      <p>Verdict: ${verdictLabel(i.verdict)}. ${EXPLAIN[i.verdict] || ""}</p>
      <a class="button" href="${href.incident(i.id, "experiments")}">See the experiments</a></div>`;
  }
  const block = (id, title, body) => h`<div class="card"><div class="row"><p class="kicker">${title}</p>
      <button type="button" class="ghost small" data-copy="${id}">Copy</button></div><pre id="${id}" tabindex="0">${body}</pre></div>`;
  return h`<div class="row page-actions"><p class="muted grow">${i.repair.instructions}</p>
      <button type="button" data-download="package">Download package</button>
      <button type="button" class="primary" data-download="test">Download regression_test.json</button></div>
    <div class="grid">
      ${block("pre-patch", "Patch", JSON.stringify(i.repair.patch, null, 2))}
      ${block("pre-test", "Regression test", JSON.stringify(i.repair.regression_test, null, 2))}
      ${block("pre-repro", "Reproduce", i.reproduce || "")}
    </div>
    <p class="muted small">Run it: <code>kavira test -f regression_test.json</code></p>`;
}

export async function incident(ctx, route) {
  let data;
  try {
    data = await api(`/api/v1/incidents/${encodeURIComponent(route.id)}`);
  } catch (err) {
    if (err.status === 404) return { html: notFound(`No incident named ${route.id}.`) };
    throw err;
  }
  const i = data.incident;
  ctx.current = i;
  const body = { evidence, experiments, repairs }[route.tab];
  if (!body) return { html: notFound("Unknown section.") };
  return { html: h`${incidentHead(i, route.tab)}${body(i, data.bundle)}`, incident: i };
}

export function audit(ctx) {
  return h`${pageHero({ eyebrow: "Browse", title: "Audit", lede: "Hash-chained compile log for this process. Not a production ledger." })}
    <p class="banner ${ctx.chainOK ? "info" : "warn"}" role="status">${ctx.chainOK ? "Chain verified: every entry hashes its predecessor." : "Chain broken: an entry does not match its hash."}</p>
    <div class="card">${ctx.audit.length ? h`<div class="table-wrap"><table>
      <caption class="visually-hidden">Audit log</caption>
      <thead><tr><th scope="col">When</th><th scope="col">Action</th><th scope="col">Incident</th><th scope="col">Verdict</th><th scope="col">Hash</th></tr></thead>
      <tbody>${ctx.audit.slice().reverse().map((a) => h`<tr><td>${when(a.at)}</td><td>${sentence(a.action)}</td>
        <td><a href="${href.incident(a.id)}">${a.id}</a></td><td>${pill(a.verdict)}</td>
        <td><code title="${a.hash}">${a.hash.slice(0, 12)}</code> <button type="button" class="ghost small" data-copy-text="${a.hash}" aria-label="Copy full hash">Copy</button></td></tr>`)}</tbody></table></div>`
      : h`<div class="list-empty"><h3>Nothing audited yet</h3><p>Every compile adds an entry.</p></div>`}</div>`;
}

export function notFound(text = "That page does not exist.") {
  return h`<div class="card list-empty"><h1 id="page-title" tabindex="-1" class="h3">Not found</h1><p>${text}</p>
    <a class="button primary" href="${href.overview}">Back to overview</a></div>`;
}

export { skeleton, listEmpty, relTime };
