import { chromium, expect } from "@playwright/test";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { once } from "node:events";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { spawn, spawnSync } from "node:child_process";
import { setTimeout as delay } from "node:timers/promises";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const mediaDir = path.join(repoRoot, "docs", "media");
const imagesDir = path.join(repoRoot, "docs", "images");
const reviewDir = path.join(repoRoot, "dist", "media-review");
const tempDir = await mkdtemp(path.join(tmpdir(), "codex-usage-media-"));
const viewport = { width: 1440, height: 1000 };
const fixedTime = new Date("2026-09-23T16:30:00+08:00");
const port = await new Promise((resolve, reject) => {
  const probe = createServer();
  probe.once("error", reject);
  probe.listen(0, "127.0.0.1", () => {
    const { port } = probe.address();
    probe.close(error => error ? reject(error) : resolve(port));
  });
});
const baseURL = `http://127.0.0.1:${port}/codex-usage/`;

function run(command, args, capture = false) {
  const result = spawnSync(command, args, { cwd: repoRoot, stdio: capture ? "pipe" : "inherit", encoding: "utf8", windowsHide: true });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`${command} failed: ${result.stderr || result.status}`);
  return result.stdout;
}

function assertSynthetic(text) {
  for (const pattern of [/[A-Z]:\\Users\\/i, /\/home\/[a-z0-9._-]+\//i, /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i]) {
    if (pattern.test(text)) throw new Error(`Sensitive-looking value matched ${pattern}`);
  }
}

async function waitForServer() {
  const deadline = Date.now() + 10_000;
  while (Date.now() < deadline) {
    try { if ((await fetch(baseURL)).ok) return; } catch {}
    await delay(100);
  }
  throw new Error("media demo server did not become ready");
}

async function openDemo(context, locale) {
  const page = await context.newPage();
  await page.clock.setFixedTime(fixedTime);
  const errors = [];
  page.on("pageerror", error => errors.push(error.message));
  page.on("request", request => {
    if (new URL(request.url()).origin !== new URL(baseURL).origin) errors.push(`External request: ${request.url()}`);
  });
  await page.goto(`${baseURL}?lang=${locale}`, { waitUntil: "networkidle" });
  await expect(page.locator("#overviewTotal")).not.toHaveText("—");
  await expect(page.locator("#overviewCoverage")).toContainText("100");
  await expect(page.locator("#coverageBanner")).toBeHidden();
  await page.evaluate(() => {
    window.mediaWarnings = [];
    // Observe transient errors between chapters as well as settled screens.
    setInterval(() => {
      for (const selector of ["#coverageBanner", "#toast.error", "#customRangeError", "#updateError"]) {
        const node = document.querySelector(selector);
        if (node?.checkVisibility() && node.textContent.trim()) window.mediaWarnings.push(node.textContent.trim());
      }
    }, 40);
  });
  return { page, errors };
}

async function recordDemo(browser, locale) {
  const chapterDir = path.join(reviewDir, locale);
  await mkdir(chapterDir, { recursive: true });
  const context = await browser.newContext({ viewport, deviceScaleFactor: 1, colorScheme: "light", timezoneId: "Asia/Shanghai", recordVideo: { dir: tempDir, size: viewport } });
  const { page, errors } = await openDemo(context, locale);
  const video = page.video();
  // A recording-only pointer; product layout and content are untouched.
  await page.evaluate(() => {
    const cursor = document.createElement("div");
    cursor.id = "mediaCursor";
    cursor.style.cssText = "position:fixed;left:0;top:0;width:22px;height:28px;pointer-events:none;transform:translate(1060px,310px);filter:drop-shadow(0 2px 2px #0005);margin:0;padding:0;border:0;background:transparent;overflow:visible";
    cursor.innerHTML = '<svg width="22" height="28" viewBox="0 0 22 28"><path d="M2 1L2 22L7 17L11 26L15 24L11 15L19 15Z" fill="#172226" stroke="white" stroke-width="1.6"/></svg>';
    cursor.setAttribute("popover", "manual");
    document.body.append(cursor);
    cursor.showPopover();
  });
  let pointer = { x: 1060, y: 310 };
  const chapters = [];
  const started = Date.now();
  async function move(locator) {
    await expect(locator).toBeVisible();
    const box = await locator.boundingBox();
    const target = { x: box.x + box.width / 2, y: box.y + box.height / 2 };
    if (target.y < 15 || target.y > viewport.height - 20) throw new Error(`Target outside viewport: ${locator} at ${target.y}; add a deliberate scroll`);
    const duration = Math.min(340, Math.max(160, Math.hypot(target.x - pointer.x, target.y - pointer.y) * .32));
    await page.evaluate(async ({ from, to, duration }) => {
      const cursor = document.querySelector("#mediaCursor");
      cursor.hidePopover();
      cursor.showPopover();
      await cursor.animate([
        { transform: `translate(${from.x}px,${from.y}px)` },
        { transform: `translate(${to.x}px,${to.y}px)` }
      ], { duration, easing: "cubic-bezier(.22,.65,.3,1)", fill: "forwards" }).finished;
    }, { from: pointer, to: target, duration });
    await page.mouse.move(target.x, target.y);
    pointer = target;
  }
  async function click(selector, pause = 400) {
    const locator = typeof selector === "string" ? page.locator(selector) : selector;
    await move(locator);
    await page.evaluate(() => document.querySelector("#mediaCursor svg").animate([
      { transform: "scale(1)" }, { transform: "scale(.84)" }, { transform: "scale(1)" }
    ], { duration: 180 }));
    await locator.click();
    await delay(pause);
  }
  async function scrollTo(selector, top = 95) {
    await page.locator(selector).evaluate((node, top) => window.scrollTo({ top: window.scrollY + node.getBoundingClientRect().top - top, behavior: "smooth" }), top);
    await delay(500);
  }
  async function chapter(name) {
    assertSynthetic(await page.locator("body").innerText());
    await expect(page.locator("#coverageBanner")).toBeHidden();
    const time = Number(((Date.now() - started) / 1000).toFixed(2));
    chapters.push({ name, time });
    await page.screenshot({ path: path.join(chapterDir, `${String(chapters.length).padStart(2, "0")}-${name}.png`) });
    console.log(`${locale}: ${name} @ ${time}s`);
  }

  try {
    await delay(950);
    await chapter("overview");
    await click('[data-overview-range="30d"]', 550);
    await click('[data-overview-range="custom"]', 250);
    await move(page.locator("#rangeStart"));
    await page.locator("#rangeStart").fill("2026-09-22T09:00");
    await delay(220);
    await move(page.locator("#rangeEnd"));
    await page.locator("#rangeEnd").fill("2026-09-23T16:30");
    await delay(220);
    await click("#queryRange", 850);
    await expect(page.locator("#overviewExactTotal")).toBeVisible();
    await chapter("minute-range");

    await click('[data-overview-range="7d"]', 250);
    await scrollTo("#usageTrendPanel");
    await click("#trendHourlyTab", 600);
    await click(page.locator("#hourlyPoints .hour-point:not(.zero)").nth(8), 700);
    await chapter("hourly-drilldown");
    await click("#tab-daily", 650);
    await click('[data-calendar-date="2026-09-22"]', 750);
    await expect(page.locator("#dayTotal")).not.toHaveText("0");
    await chapter("daily-calendar");

    await click("#tab-details", 450);
    await click('[data-dimension="project"]', 500);
    await chapter("project-breakdown");
    await scrollTo("#sessionsPanel", 140);
    await click('[data-session-view="tree"]', 750);
    const toggle = page.locator("[data-tree-toggle]").first();
    await chapter("task-tree");
    await click(toggle, 350);
    await expect(toggle).toHaveAttribute("aria-expanded", "false");
    await click(toggle, 500);
    await expect(toggle).toHaveAttribute("aria-expanded", "true");
    await click('[data-session-view="list"]', 200);
    await click("#sessionSearch", 80);
    await page.locator("#sessionSearch").pressSequentially("local", { delay: 70 });
    await delay(600);
    await chapter("session-search");
    await click("#sessionSearchClear", 180);
    await click("#filterButton", 350);
    await move(page.locator("#filterMode"));
    await page.locator("#filterMode").selectOption("fast");
    await delay(400);
    await click("#applyFilters", 650);
    await chapter("fast-filter");
    await click("#exportButton", 700);
    await expect(page.locator("#exportJson")).toHaveAttribute("href", /^data:application\/json/);
    await chapter("json-csv-export");
    await click("#exportDialog [data-close]", 150);
    await scrollTo("#filterContext");
    await click("#clearFilters", 150);

    await click("#tab-overview", 350);
    await click("#pricingButton", 300);
    await click(".catalog-disclosure summary", 800);
    await chapter("model-pricing");
    await click("#pricingDialog .dialog-close", 200);
    await click("#themeButton", 800);
    await chapter("dark-theme");
    await click("#localeButton", 700);
    await chapter("bilingual");
    await click("#localeButton", 150);
    await click("#themeButton", 250);
    await click("#trendDailyTab", 150);
    await page.evaluate(() => window.scrollTo({ top: 0, behavior: "smooth" }));
    await delay(450);
    await move(page.locator("#overviewTotal"));
    await delay(650);
    const warnings = await page.evaluate(() => [...new Set(window.mediaWarnings)]);
    if (warnings.length || errors.length) throw new Error(JSON.stringify({ warnings, errors }));
    const contentSeconds = (Date.now() - started) / 1000;
    await page.close();
    await context.close();
    const source = path.join(tempDir, `demo-${locale}.webm`);
    await video.saveAs(source);
    const metadata = JSON.parse(run("ffprobe", ["-v", "error", "-show_entries", "format=duration", "-of", "json", source], true));
    const trimStart = Math.max(0, Number(metadata.format.duration) - contentSeconds);
    const mp4 = path.join(mediaDir, `codex-usage-demo-${locale === "en" ? "en" : "zh"}.mp4`);
    run("ffmpeg", ["-hide_banner", "-loglevel", "error", "-y", "-ss", String(trimStart), "-i", source, "-t", String(contentSeconds), "-c:v", "libx264", "-crf", "20", "-pix_fmt", "yuv420p", "-movflags", "+faststart", "-an", mp4]);
    const gif = path.join(mediaDir, `codex-usage-demo${locale === "en" ? "-en" : ""}.gif`);
    const encodedGif = path.join(tempDir, `${locale}.gif`);
    // Keep real timestamps and 20 fps motion; hold still frames efficiently.
    run("ffmpeg", ["-hide_banner", "-loglevel", "error", "-y", "-ss", String(trimStart), "-i", source, "-t", String(contentSeconds), "-filter_complex", "fps=20,scale=1120:-2:flags=lanczos,mpdecimate,split[s0][s1];[s0]palettegen=max_colors=128:stats_mode=diff[p];[s1][p]paletteuse=dither=none:diff_mode=rectangle", "-fps_mode", "vfr", "-loop", "0", encodedGif]);
    const frameTimes = run("ffprobe", ["-v", "error", "-select_streams", "v", "-show_entries", "packet=pts_time", "-of", "csv=p=0", encodedGif], true).trim().split(/\s+/).map(Number);
    const finalDelay = Math.max(5, Math.round((contentSeconds - frameTimes.at(-1)) * 100));
    run("ffmpeg", ["-hide_banner", "-loglevel", "error", "-y", "-i", encodedGif, "-c:v", "copy", "-loop", "0", "-final_delay", String(finalDelay), gif]);
    const gifSeconds = Number(JSON.parse(run("ffprobe", ["-v", "error", "-show_entries", "format=duration", "-of", "json", gif], true)).format.duration);
    if (Math.abs(gifSeconds - contentSeconds) > .1) throw new Error(`GIF timing changed: ${gifSeconds} vs ${contentSeconds}`);
    for (const file of [gif, mp4]) {
      const { length } = await readFile(file);
      if (length < 10_000 || length > 18_000_000) throw new Error(`Unexpected media size: ${file}: ${length}`);
    }
    await writeFile(path.join(chapterDir, "chapters.json"), JSON.stringify({ locale, viewport, contentSeconds, gifSeconds, gifMaxFPS: 20, warnings, errors, chapters }, null, 2) + "\n");
  } finally {
    await context.close();
  }
}

run(process.execPath, ["scripts/build-demo.mjs", "dist/pages"]);
for (const directory of [mediaDir, imagesDir, reviewDir]) await mkdir(directory, { recursive: true });
const server = spawn(process.execPath, ["scripts/serve-static.mjs", "dist/pages", String(port), "codex-usage"], { cwd: repoRoot, stdio: "ignore", windowsHide: true });
try {
  await waitForServer();
  const browser = await chromium.launch({ headless: true, ...(process.env.PLAYWRIGHT_CHANNEL ? { channel: process.env.PLAYWRIGHT_CHANNEL } : {}) });
  try {
    const socialContext = await browser.newContext({ viewport: { width: 1280, height: 640 }, deviceScaleFactor: 1 });
    const social = await socialContext.newPage();
    await social.goto(pathToFileURL(path.join(repoRoot, "scripts", "social-preview.html")).href);
    assertSynthetic(await social.locator("body").innerText());
    await social.screenshot({ path: path.join(mediaDir, "social-preview.png") });
    await socialContext.close();
    const desktopContext = await browser.newContext({ viewport, colorScheme: "light", timezoneId: "Asia/Shanghai", deviceScaleFactor: 1 });
    const { page: desktop } = await openDemo(desktopContext, "zh-CN");
    assertSynthetic(await desktop.locator("body").innerText());
    await desktop.screenshot({ path: path.join(imagesDir, "dashboard.png"), fullPage: true });
    await desktop.setViewportSize({ width: 390, height: 844 });
    await desktop.screenshot({ path: path.join(imagesDir, "dashboard-mobile.png"), fullPage: true });
    await desktopContext.close();
    for (const locale of process.env.MEDIA_LOCALE ? [process.env.MEDIA_LOCALE] : ["zh-CN", "en"]) await recordDemo(browser, locale);
  } finally {
    await browser.close();
  }
  console.log(`Generated media in ${mediaDir}; chapter screenshots and audit in ${reviewDir}`);
} finally {
  if (server.exitCode === null && server.signalCode === null) {
    const exited = once(server, "exit");
    server.kill();
    await Promise.race([exited, delay(5_000)]);
  }
  const tempRoot = path.resolve(tmpdir()) + path.sep;
  if (!path.resolve(tempDir).startsWith(tempRoot) || !path.basename(tempDir).startsWith("codex-usage-media-")) throw new Error("Unexpected capture cleanup path");
  await rm(tempDir, { recursive: true, force: true });
}
