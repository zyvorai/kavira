// Records the README demo: sign in, compile an example, walk evidence -> experiments -> repairs.
// Same command locally: make demo   (needs playwright-core, ffmpeg, and a built ./bin/kavira)
const { spawn, execFileSync } = require("child_process");
const fs = require("fs");
const net = require("net");
const os = require("os");
const path = require("path");

function load() {
  for (const t of [() => require("playwright"), () => require("playwright-core"), () => require(path.join(process.env.KAVIRA_PW_PATH || "", "playwright-core"))]) {
    try { return t(); } catch (_) { /* next */ }
  }
  console.error("playwright not found: npm i -D playwright-core, or set KAVIRA_PW_PATH"); process.exit(2);
}
const { chromium } = load();
const out = process.argv[2] || path.join(__dirname, "..", "docs", "ux", "kavira-demo.gif");
const pw = "demo-Passw0rd-xyz";
const free = () => new Promise((r) => { const s = net.createServer().listen(0, "127.0.0.1", () => { const p = s.address().port; s.close(() => r(p)); }); });
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

(async () => {
  const port = await free();
  const base = `http://127.0.0.1:${port}`;
  const proc = spawn(path.join(__dirname, "..", "bin", "kavira"), ["serve", "-addr", `127.0.0.1:${port}`], { env: { ...process.env, KAVIRA_ADMIN_PASSWORD: pw, KAVIRA_SESSION_KEY: "demo" }, stdio: "ignore" });
  for (let i = 0; i < 50; i++) { try { if ((await fetch(base + "/api/v1/health")).ok) break; } catch (_) {} await sleep(100); }
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "kavira-demo-"));
  const browser = await chromium.launch().catch(() => chromium.launch({ channel: "chrome" }));
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 720 }, colorScheme: "light", bypassCSP: true });
  const page = await ctx.newPage();
  // Deterministic frame capture (Playwright video needs its own ffmpeg download). hold(ms) records ms/125 frames,
  // played back at 8 fps, so timing in the GIF does not depend on how fast screenshots run.
  const FPS = 8;
  let n = 0;
  const snap = () => page.screenshot({ path: path.join(dir, `f${String(n++).padStart(5, "0")}.png`) });
  const hold = async (ms) => { for (let i = 0; i < Math.max(1, Math.round((ms / 1000) * FPS)); i++) await snap(); };
  const typeInto = async (sel, text) => { for (const ch of text) { await page.type(sel, ch); await snap(); } };
  await page.goto(base + "/");
  await hold(900);
  await typeInto('input[name="username"]', "admin");
  await typeInto('input[name="password"]', pw);
  await hold(300);
  await page.click('button[type="submit"]');
  await page.waitForSelector("[data-compile]");
  await hold(1300);
  await page.click('[data-compile="oom"]');
  await page.waitForFunction(() => location.hash.includes("inc-oom-001"));
  await page.waitForSelector(".kv");
  await hold(1800);
  await page.click('.tabs a:has-text("Experiments")');
  await page.waitForSelector("table");
  await hold(2000);
  await page.click('.tabs a:has-text("Repairs")');
  await page.waitForSelector("#pre-patch");
  await hold(2200);
  await page.click("#theme");
  await hold(1600);
  await ctx.close();
  await browser.close();
  proc.kill();
  const pal = path.join(dir, "pal.png");
  const vf = "scale=960:-1:flags=lanczos";
  const input = ["-framerate", String(FPS), "-i", path.join(dir, "f%05d.png")];
  execFileSync("ffmpeg", ["-y", "-loglevel", "error", ...input, "-vf", `${vf},palettegen=stats_mode=diff`, pal]);
  execFileSync("ffmpeg", ["-y", "-loglevel", "error", ...input, "-i", pal, "-lavfi", `${vf}[x];[x][1:v]paletteuse=dither=bayer:bayer_scale=4`, out]);
  fs.rmSync(dir, { recursive: true, force: true });
  console.log(`wrote ${path.relative(process.cwd(), out)} (${(fs.statSync(out).size / 1024).toFixed(0)} KiB)`);
})().catch((e) => { console.error(e); process.exit(1); });
