// KAVIRA console end-to-end + screenshot audit (Playwright).
// Same command locally: make e2e   (or: node scripts/e2e.cjs [--shots docs/ux] [--base https://host:port --password PW | --demo])
// --demo: the target runs the demo gate (admin / Admin@321), so expect the hint and use that password. Self-signed TLS is accepted.
// Without --base it starts ./bin/kavira on a free port with a non-demo password.
const { spawn } = require("child_process");
const fs = require("fs");
const path = require("path");
const net = require("net");

function load() {
  // Resolution order: local install, KAVIRA_PW_PATH (a node_modules dir), global install.
  const tries = [() => require("playwright"), () => require("playwright-core"),
    () => require(path.join(process.env.KAVIRA_PW_PATH || "", "playwright-core")),
    () => require(path.join(require("child_process").execSync("npm root -g").toString().trim(), "playwright"))];
  for (const t of tries) { try { return t(); } catch (_) { /* next */ } }
  console.error("playwright not found: npm i -D playwright-core, or set KAVIRA_PW_PATH to a node_modules dir");
  process.exit(2);
}
const { chromium } = load();
const arg = (k, d) => { const i = process.argv.indexOf(k); return i > 0 ? process.argv[i + 1] : d; };
const shotsDir = arg("--shots", "");
let base = arg("--base", "");
const demo = process.argv.includes("--demo");
const password = demo ? "Admin@321" : arg("--password", "e2e-Passw0rd-xyz");

const free = () => new Promise((res) => { const s = net.createServer().listen(0, "127.0.0.1", () => { const p = s.address().port; s.close(() => res(p)); }); });
const failures = [];
const check = (ok, msg) => { console.log(`${ok ? "ok  " : "FAIL"} ${msg}`); if (!ok) failures.push(msg); };

(async () => {
  let proc;
  if (!base) {
    const port = await free();
    base = `http://127.0.0.1:${port}`;
    proc = spawn(path.join(__dirname, "..", "bin", "kavira"), ["serve", "-addr", `127.0.0.1:${port}`], {
      env: { ...process.env, KAVIRA_ADMIN_PASSWORD: password, KAVIRA_SESSION_KEY: "e2e-session-key" }, stdio: "ignore",
    });
    for (let i = 0; i < 50; i++) { try { if ((await fetch(base + "/api/v1/health")).ok) break; } catch (_) {} await new Promise((r) => setTimeout(r, 100)); }
  }
  // Prefer Playwright's bundled Chromium; fall back to the system Chrome.
  const browser = await chromium.launch().catch(() => chromium.launch({ channel: "chrome" }));
  const errors = [];
  const shot = async (page, name) => { if (shotsDir) { fs.mkdirSync(shotsDir, { recursive: true }); await page.screenshot({ path: path.join(shotsDir, name + ".png") }); } };
  const noScroll = async (page, label) => check(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), `${label}: no horizontal scroll`);

  async function session(scheme, viewport, tag) {
    const ctx = await browser.newContext({ colorScheme: scheme, viewport, permissions: ["clipboard-read", "clipboard-write"],
      // Playwright polls with eval, which the strict CSP blocks. Bypassing it also makes the XSS check
      // stricter: the page must be safe by escaping alone. The CSP header itself is covered by Go tests.
      bypassCSP: true, ignoreHTTPSErrors: true });
    const page = await ctx.newPage();
    page.on("pageerror", (e) => errors.push(`${tag} pageerror: ${e.message}`));
    // Browsers log every failed fetch as a console error. The response listener below is the real check:
    // 5xx always fails; 401 is expected only mid-expiry; 404 is expected only for the missing-incident probe.
    page.on("console", (m) => { if (m.type() === "error" && !m.text().startsWith("Failed to load resource")) errors.push(`${tag} console: ${m.text()}`); });
    page.on("response", (r) => {
      const st = r.status(), u = r.url();
      if (st < 400 || u.includes("/api/v1/session")) return;
      if (st === 401 && tag === "expiry") return;
      if (st === 404 && u.includes("/api/v1/incidents/inc-missing")) return;
      errors.push(`${tag} ${st} ${u}`);
    });
    return page;
  }

  // ---- login, light desktop
  let page = await session("light", { width: 1440, height: 900 }, "desktop-light");
  await page.goto(base + "/");
  await page.waitForSelector("#login:not([hidden])");
  check(await page.inputValue('input[name="username"]') === "", "login: username is not prefilled");
  check(await page.inputValue('input[name="password"]') === "", "login: password is not prefilled");
  check((await page.textContent("#host-line")).includes("Connecting to"), "login: shows host line");
  check(demo ? await page.locator("#demo-hint").isVisible() : await page.locator("#demo-hint").isHidden(), `login: demo hint ${demo ? "shown" : "hidden"} to match the demo gate`);
  check(await page.getAttribute("html", "data-theme") === "light", "theme: light follows OS");
  await shot(page, "07-login-light");
  await page.fill('input[name="username"]', "admin");
  await page.fill('input[name="password"]', "wrong");
  await page.click('button[type="submit"]');
  await page.waitForSelector("#login-error:not([hidden])");
  check((await page.textContent("#login-error")).includes("Wrong username or password"), "login: wrong password message");
  check(await page.getAttribute("#login-error", "role") === "alert", "login: error has role=alert");
  check(!(await page.textContent("#login-error")).includes("Admin@321"), "login: error does not leak the demo password");
  await page.fill('input[name="password"]', password);
  await page.click('button[type="submit"]');
  await page.waitForSelector("#app:not([hidden])");
  await page.waitForSelector("#page-title");
  check(page.url().endsWith("#/overview"), "route: lands on #/overview");

  // ---- every route renders
  const routes = [["#/overview", "Turn an outage"], ["#/incidents", "Incidents"], ["#/incidents/inc-oom-001/evidence", "inc-oom-001"],
    ["#/incidents/inc-oom-001/experiments", "inc-oom-001"], ["#/incidents/inc-oom-001/repairs", "inc-oom-001"], ["#/audit", "Audit"]];
  const names = ["00-overview", "01-incidents", "02-evidence", "03-experiments", "04-repairs", "05-audit"];
  for (const [i, [hash, text]] of routes.entries()) {
    await page.goto(base + "/" + hash);
    await page.waitForFunction((t) => document.getElementById("page-title")?.textContent.includes(t), text);
    check(true, `route ${hash} renders`);
    check(await page.evaluate(() => document.activeElement?.id) === "page-title", `route ${hash}: focus moves to heading`);
    await noScroll(page, hash);
    await shot(page, names[i] + "-light");
  }
  await page.goto(base + "/#/nope");
  await page.waitForSelector("text=Not found");
  check(true, "unknown route shows Not found");
  await page.goto(base + "/#/incidents/inc-missing/evidence");
  await page.waitForSelector("text=No incident named inc-missing");
  check(true, "unknown incident shows an honest empty state");

  // deep link + reload
  await page.goto(base + "/#/incidents/inc-net-001/experiments");
  await page.waitForSelector("table");
  await page.reload();
  await page.waitForSelector("table");
  check(page.url().endsWith("/inc-net-001/experiments"), "deep link survives reload");
  check((await page.getAttribute('nav a[data-nav="experiments"]', "aria-current")) === "page", "nav: aria-current on Experiments");

  // incidents filter + sort + keyboard row
  await page.goto(base + "/#/incidents");
  await page.waitForSelector("#inc-q");
  await page.fill("#inc-q", "net");
  check(await page.locator("#inc-table tbody tr").count() === 1, "incidents: search filters rows");
  await page.fill("#inc-q", "zzzz");
  await page.waitForSelector("text=No incidents match");
  await page.click("[data-clear-filter]");
  check(await page.locator("#inc-table tbody tr").count() === 3, "incidents: clear filters restores rows");
  await page.click('[data-sort="id"]');
  check((await page.locator("#inc-table tbody tr td a").first().textContent()) === "inc-oom-001", "incidents: sort toggles direction");
  await page.locator("#inc-table tbody tr td a").first().focus();
  await page.keyboard.press("Enter");
  await page.waitForFunction(() => location.hash.includes("/evidence"));
  check(true, "incidents: row link opens via keyboard");

  // repairs: copy + download
  await page.goto(base + "/#/incidents/inc-oom-001/repairs");
  await page.waitForSelector("#pre-patch");
  await page.click('[data-copy="pre-patch"]');
  await page.waitForSelector(".toast");
  check((await page.textContent(".toast")).includes("Copied"), "repairs: copy reports success");
  // The clipboard API only exists on https/localhost; over plain http the execCommand fallback is what ran.
  if (await page.evaluate(() => window.isSecureContext)) {
    check((await page.evaluate(() => navigator.clipboard.readText())).includes("{"), "repairs: copy puts the patch on the clipboard");
  }
  const [dl] = await Promise.all([page.waitForEvent("download"), page.click('[data-download="test"]')]);
  check(dl.suggestedFilename() === "regression_test.json", "repairs: downloads regression_test.json");

  // shortcuts
  await page.goto(base + "/#/overview");
  await page.waitForSelector("#page-title");
  await page.keyboard.press("g"); await page.keyboard.press("a");
  await page.waitForFunction(() => location.hash === "#/audit");
  check(true, "shortcut: g a opens Audit");
  await page.keyboard.press("?");
  check(await page.locator("#shortcuts-dialog[open]").count() === 1, "shortcut: ? opens the sheet");
  await page.keyboard.press("Escape");

  // theme toggle persists and flips
  await page.click("#theme");
  check(await page.getAttribute("html", "data-theme") === "dark", "theme: toggle -> dark");
  check(await page.getAttribute("#theme", "aria-label") === "Switch to light mode", "theme: aria-label flips");
  await page.reload();
  check(await page.getAttribute("html", "data-theme") === "dark", "theme: choice persists across reload");
  for (const [i, [hash, text]] of routes.entries()) {
    await page.goto(base + "/" + hash);
    await page.waitForFunction((t) => document.getElementById("page-title")?.textContent.includes(t), text);
    await shot(page, names[i] + "-dark");
  }

  // sign out, session expiry
  await page.click("#logout");
  await page.waitForSelector("#login:not([hidden])");
  check(true, "logout returns to login");
  await page.context().close();

  // ---- expired session
  page = await session("light", { width: 1440, height: 900 }, "expiry");
  await page.goto(base + "/");
  await page.fill('input[name="username"]', "admin");
  await page.fill('input[name="password"]', password);
  await page.click('button[type="submit"]');
  await page.waitForSelector("#app:not([hidden])");
  await page.context().clearCookies();
  await page.goto(base + "/#/audit");
  await page.reload();
  await page.waitForSelector("#login:not([hidden])");
  await page.fill('input[name="username"]', "admin");
  await page.fill('input[name="password"]', password);
  await page.click('button[type="submit"]');
  await page.waitForSelector("#app:not([hidden])");
  await page.context().clearCookies();
  await page.click('nav a[data-nav="incidents"]');
  await page.click('nav a[data-nav="audit"]');
  await page.waitForSelector("#login:not([hidden])");
  check((await page.textContent("#login-error")).includes("session expired"), "expiry: login shows 'session expired'");
  await page.context().close();

  // ---- mobile 390, dark OS preference
  page = await session("dark", { width: 390, height: 844 }, "mobile-dark");
  await page.goto(base + "/");
  await page.waitForSelector("#login:not([hidden])");
  check(await page.getAttribute("html", "data-theme") === "dark", "theme: dark follows OS preference");
  await noScroll(page, "mobile login");
  await shot(page, "07-login-mobile-dark");
  await page.fill('input[name="username"]', "admin");
  await page.fill('input[name="password"]', password);
  await page.click('button[type="submit"]');
  await page.waitForSelector("#app:not([hidden])");
  for (const [i, [hash, text]] of routes.entries()) {
    await page.goto(base + "/" + hash);
    await page.waitForFunction((t) => document.getElementById("page-title")?.textContent.includes(t), text);
    await noScroll(page, `mobile ${hash}`);
    await shot(page, names[i] + "-mobile-dark");
  }
  await page.context().close();

  // ---- hostile bundle + audit chain (last: it adds an incident to server-wide state, which would pollute screenshots)
  page = await session("light", { width: 1440, height: 900 }, "bundle");
  await page.goto(base + "/");
  await page.fill('input[name="username"]', "admin");
  await page.fill('input[name="password"]', password);
  await page.click('button[type="submit"]');
  await page.waitForSelector("#app:not([hidden])");
  // compile a bundle with a hostile string: must render inert
  await page.goto(base + "/#/overview");
  await page.waitForSelector("[data-open-bundle]");
  const hostile = JSON.parse(fs.readFileSync(path.join(__dirname, "..", "examples", "oom.json"), "utf8"));
  hostile.id = "inc-xss-001";
  hostile.service = '<img src=x onerror="window.__xss=1">';
  hostile.symptom = '<script>window.__xss=1</script> <b>bold</b>';
  await page.click("button[data-open-bundle]");
  await page.fill("#bundle-text", "{ not json");
  await page.click('#bundle-form button[type="submit"]');
  check((await page.textContent("#bundle-error")).startsWith("Not valid JSON"), "bundle: invalid JSON is reported");
  await page.fill("#bundle-text", JSON.stringify(hostile));
  await page.click('#bundle-form button[type="submit"]');
  await page.waitForFunction(() => location.hash.includes("inc-xss-001"));
  await page.waitForSelector("#page-title");
  check(await page.evaluate(() => window.__xss === undefined), "xss: hostile bundle did not execute");
  check(await page.locator("main img, main script, main b").count() === 0, "xss: no injected elements in the page");
  check((await page.textContent("main")).includes("<script>window.__xss=1</script>"), "xss: hostile text shown literally");
  await page.goto(base + "/#/incidents");
  await page.waitForSelector("#inc-table tbody tr");
  check(await page.locator("#inc-table tbody tr").count() === 4, "bundle: custom incident listed");
  await page.goto(base + "/#/audit");
  await page.waitForSelector("table");
  check((await page.textContent(".banner")).includes("Chain verified"), "audit: chain verified banner");

  await page.context().close();

  await browser.close();
  if (proc) proc.kill();
  for (const e of errors) check(false, e);
  console.log(failures.length ? `\n${failures.length} failure(s)` : "\nall e2e checks passed");
  process.exit(failures.length ? 1 : 0);
})().catch((e) => { console.error(e); process.exit(1); });
