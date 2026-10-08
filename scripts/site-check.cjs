// Browser check of the built Pages site, served under the real /kavira/ sub-path (as GitHub Pages does).
// Same command locally: make site-check      (or: ./scripts/build-site.sh && node scripts/site-check.cjs [--shots dir] [--base URL])
// --base https://zyvorai.github.io/kavira/ checks the live site instead of serving _site locally.
const http = require("http");
const fs = require("fs");
const path = require("path");

function load() {
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
const siteDir = path.resolve(arg("--dir", path.join(__dirname, "..", "_site")));
let base = arg("--base", "");
const PREFIX = "/kavira/";
const MIME = { ".html": "text/html", ".css": "text/css", ".js": "text/javascript", ".json": "application/json", ".svg": "image/svg+xml", ".png": "image/png", ".jpg": "image/jpeg", ".gif": "image/gif", ".txt": "text/plain", ".xml": "application/xml" };

const failures = [];
const check = (ok, msg) => { console.log(`${ok ? "ok  " : "FAIL"} ${msg}`); if (!ok) failures.push(msg); };

function serve() {
  return new Promise((resolve) => {
    const srv = http.createServer((req, res) => {
      let p = decodeURIComponent(req.url.split("?")[0]);
      if (!p.startsWith(PREFIX)) { res.writeHead(404); return res.end("outside the project path"); }
      p = p.slice(PREFIX.length) || "index.html";
      if (p.endsWith("/")) p += "index.html";
      let file = path.join(siteDir, p);
      if (!file.startsWith(siteDir) || !fs.existsSync(file)) { file = path.join(siteDir, "404.html"); res.statusCode = 404; }
      res.setHeader("Content-Type", MIME[path.extname(file)] || "application/octet-stream");
      res.end(fs.readFileSync(file));
    }).listen(0, "127.0.0.1", () => resolve({ srv, url: `http://127.0.0.1:${srv.address().port}${PREFIX}` }));
  });
}

(async () => {
  let srv;
  if (!base) { const s = await serve(); srv = s.srv; base = s.url; }
  if (!base.endsWith("/")) base += "/";
  const browser = await chromium.launch().catch(() => chromium.launch({ channel: "chrome" }));
  const shot = async (page, name) => { if (shotsDir) { fs.mkdirSync(shotsDir, { recursive: true }); await page.screenshot({ path: path.join(shotsDir, name + ".png"), fullPage: false }); } };

  async function open(scheme, viewport, tag) {
    const ctx = await browser.newContext({ colorScheme: scheme, viewport, permissions: ["clipboard-read", "clipboard-write"] });
    const page = await ctx.newPage();
    const bad = [];
    page.on("pageerror", (e) => bad.push(`${tag} pageerror: ${e.message}`));
    page.on("console", (m) => { if (m.type() === "error" && !m.text().startsWith("Failed to load resource")) bad.push(`${tag} console: ${m.text()}`); });
    page.on("response", (r) => { if (r.status() >= 400 && !/\/404\.html$/.test(r.url()) && !r.url().includes("incident-inc-missing.json")) bad.push(`${tag} ${r.status()} ${r.url()}`); });
    return { ctx, page, bad };
  }

  // ---------- landing, desktop, light
  let { ctx, page, bad } = await open("light", { width: 1440, height: 900 }, "landing");
  await page.goto(base);
  check((await page.title()).includes("KAVIRA"), "landing: title");
  check(await page.locator("h1").innerText().then((t) => t.includes("reproducible test")), "landing: h1");
  check(await page.getAttribute("html", "data-theme") === "light", "landing: follows OS light");
  await shot(page, "site-landing-light");
  // images all load (lazy ones too, after scrolling)
  await page.evaluate(async () => { for (let y = 0; y < document.body.scrollHeight; y += 700) { window.scrollTo(0, y); await new Promise((r) => setTimeout(r, 60)); } });
  await page.waitForTimeout(500);
  // Only visible images: the other theme's screenshots are display:none, and browsers never load those.
  const brokenImgs = () => page.evaluate(() => [...document.images].filter((i) => i.offsetParent !== null && (!i.complete || i.naturalWidth === 0)).map((i) => i.getAttribute("src")));
  const broken = await brokenImgs();
  check(broken.length === 0, `landing: every visible image loads${broken.length ? " (broken: " + broken.join(", ") + ")" : ""}`);
  // theme toggle flips and persists
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.click("#theme");
  check(await page.getAttribute("html", "data-theme") === "dark", "landing: theme toggle -> dark");
  check(await page.getAttribute("#theme", "aria-label") === "Switch to light mode", "landing: toggle label flips");
  await page.reload();
  check(await page.getAttribute("html", "data-theme") === "dark", "landing: theme persists across reload");
  await page.evaluate(async () => { for (let y = 0; y < document.body.scrollHeight; y += 700) { window.scrollTo(0, y); await new Promise((r) => setTimeout(r, 60)); } });
  await page.waitForTimeout(500);
  check(await page.locator(".for-dark").first().isVisible() && await page.locator(".for-light").first().isHidden(), "landing: dark mode swaps in the dark screenshots");
  const brokenDark = await brokenImgs();
  check(brokenDark.length === 0, `landing: dark screenshots load${brokenDark.length ? " (broken: " + brokenDark.join(", ") + ")" : ""}`);
  await page.evaluate(() => window.scrollTo(0, 0));
  await shot(page, "site-landing-dark");
  // verdict explorer: click and keyboard
  await page.locator("#verdicts").scrollIntoViewIfNeeded();
  await page.click("#t-nr");
  check(await page.locator("#p-nr").isVisible() && await page.locator("#p-held").isHidden(), "verdicts: click selects a panel");
  check((await page.locator("#p-nr pre").innerText()).includes("not_reproduced"), "verdicts: not_reproduced shows its real CLI line");
  await page.focus("#t-nr");
  await page.keyboard.press("ArrowRight");
  check(await page.getAttribute("#t-failed", "aria-selected") === "true", "verdicts: ArrowRight moves to the next tab");
  await page.keyboard.press("End");
  check(await page.getAttribute("#t-unsup", "aria-selected") === "true", "verdicts: End goes to the last tab");
  await page.keyboard.press("Home");
  check(await page.getAttribute("#t-held", "aria-selected") === "true" && await page.locator("#p-held").isVisible(), "verdicts: Home goes to the first tab");
  // copy buttons
  await page.locator("#quickstart .copy").first().click();
  await page.waitForSelector("#toast:not([hidden])");
  check((await page.locator("#toast").innerText()).includes("Copied"), "quickstart: copy reports success");
  check((await page.evaluate(() => navigator.clipboard.readText())).includes("kavira compile"), "quickstart: clipboard holds the commands");
  // scroll-spy
  await page.locator("#classes").scrollIntoViewIfNeeded();
  await page.waitForTimeout(400);
  check(await page.locator('#sections a[href="#classes"]').getAttribute("aria-current") === "true", "nav: scroll-spy marks the section in view");
  check(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), "landing: no horizontal scroll (desktop)");
  // metadata
  const meta = await page.evaluate(() => ({
    og: document.querySelector('meta[property="og:image"]')?.content, canonical: document.querySelector('link[rel=canonical]')?.href,
    ld: !!document.querySelector('script[type="application/ld+json"]'), desc: document.querySelector('meta[name=description]')?.content?.length > 50,
  }));
  check(!!meta.og && meta.og.endsWith("/social/kavira-hero-dark.jpg") && !!meta.canonical && meta.ld && meta.desc, "landing: og:image, canonical, JSON-LD, description");
  for (const f of ["robots.txt", "sitemap.xml", "favicon.svg", "social/kavira-hero-dark.jpg"]) {
    const r = await page.request.get(base + f);
    check(r.ok(), `landing: ${f} is served`);
  }
  const r404 = await page.request.get(base + "no-such-page");
  check(r404.status() === 404 && (await r404.text()).includes("Nothing reproduced"), "404: unknown path serves the 404 page");
  for (const b of bad) check(false, b);
  await ctx.close();

  // ---------- landing, phone
  ({ ctx, page, bad } = await open("dark", { width: 390, height: 844 }, "landing-mobile"));
  await page.goto(base);
  check(await page.getAttribute("html", "data-theme") === "dark", "mobile: follows OS dark");
  check(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), "mobile: no horizontal scroll");
  await shot(page, "site-landing-mobile");
  for (const b of bad) check(false, b);
  await ctx.close();

  // ---------- the live demo (the real console on precompiled results)
  ({ ctx, page, bad } = await open("light", { width: 1440, height: 900 }, "demo"));
  await page.goto(base + "demo/");
  await page.waitForSelector("#app:not([hidden])");
  check(await page.locator("#login").isHidden(), "demo: opens signed in, no login form");
  check((await page.locator(".demo-banner").innerText()).includes("Static demo"), "demo: banner says it is static");
  check(await page.locator(".metric-band b").first().innerText() === "3", "demo: overview shows the 3 precompiled incidents");
  await page.click('nav a[data-nav="incidents"]');
  await page.waitForSelector("#inc-table tbody tr");
  check(await page.locator("#inc-table tbody tr").count() === 3, "demo: incidents table has 3 rows");
  await page.locator("#inc-table tbody tr td a", { hasText: "inc-oom-001" }).click();
  await page.waitForSelector(".kv");
  check((await page.locator("#page-title").innerText()).includes("inc-oom-001"), "demo: incident opens on Evidence");
  await page.click('.tabs a:has-text("Experiments")');
  await page.waitForSelector("table");
  check(await page.locator("tbody tr").count() >= 2, "demo: experiments table renders arms");
  await shot(page, "site-demo-experiments");
  await page.click('.tabs a:has-text("Repairs")');
  await page.waitForSelector("#pre-patch");
  await page.click('[data-copy="pre-patch"]');
  check((await page.locator(".toast").first().innerText()).includes("Copied"), "demo: copy works");
  const [dl] = await Promise.all([page.waitForEvent("download"), page.click('[data-download="test"]')]);
  check(dl.suggestedFilename() === "regression_test.json", "demo: downloads regression_test.json");
  await page.goto(base + "demo/#/overview");
  await page.waitForSelector("[data-compile]");
  await page.click('[data-compile="network-timeout"]');
  await page.waitForFunction(() => location.hash.includes("inc-net-001"));
  check(true, "demo: compiling a bundled example opens its result");
  await page.goto(base + "demo/#/overview");
  await page.click("button[data-open-bundle]");
  await page.fill("#bundle-text", '{"id":"inc-custom","class":"container_oom"}');
  await page.click('#bundle-form button[type="submit"]');
  await page.waitForSelector("#bundle-error:not([hidden])");
  check((await page.textContent("#bundle-error")).includes("kavira serve"), "demo: a custom bundle explains it needs the real binary");
  await page.keyboard.press("Escape");
  await page.goto(base + "demo/#/audit");
  await page.waitForSelector("table");
  check((await page.textContent(".banner")).includes("Chain verified"), "demo: audit chain verified");
  await page.click("#theme");
  check(await page.getAttribute("html", "data-theme") === "dark", "demo: theme toggle works");
  await page.goto(base + "demo/#/incidents/inc-missing/evidence");
  await page.waitForSelector("text=No incident named inc-missing");
  check(true, "demo: unknown incident shows the honest empty state");
  await page.goto(base + "demo/#/overview");
  await page.waitForSelector("#page-title");
  await shot(page, "site-demo-overview");
  await page.click("#logout");
  await page.waitForURL((u) => u.pathname === PREFIX && !u.pathname.includes("demo"), { timeout: 10000 }).catch(() => {});
  check(new URL(page.url()).pathname === PREFIX, "demo: Sign out returns to the landing page");
  for (const b of bad) check(false, b);
  await ctx.close();

  ({ ctx, page, bad } = await open("light", { width: 390, height: 844 }, "demo-mobile"));
  await page.goto(base + "demo/");
  await page.waitForSelector("#app:not([hidden])");
  check(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), "demo mobile: no horizontal scroll");
  for (const b of bad) check(false, b);
  await ctx.close();

  await browser.close();
  if (srv) srv.close();
  console.log(failures.length ? `\n${failures.length} failure(s)` : "\nall site checks passed");
  process.exit(failures.length ? 1 : 0);
})().catch((e) => { console.error(e); process.exit(1); });
