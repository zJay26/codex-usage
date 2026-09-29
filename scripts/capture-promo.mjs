import { chromium } from "@playwright/test";
import { mkdir, mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import { once } from "node:events";
import { tmpdir } from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { spawn } from "node:child_process";
import { assertSynthetic, repoRoot, run, startDemoServer } from "./media-lib.mjs";
import { captureAssets } from "./promo/assets.mjs";
import { renderMusic } from "./promo/music.mjs";

const promoDir = path.join(repoRoot, "scripts", "promo");
const mediaDir = path.join(repoRoot, "docs", "media");
const reviewDir = path.join(repoRoot, "dist", "promo-review");
const timeline = JSON.parse(await readFile(path.join(promoDir, "timeline.json"), "utf8"));
const locales = process.env.MEDIA_LOCALE ? [process.env.MEDIA_LOCALE] : ["zh-CN", "en"];
const formats = process.env.PROMO_FORMAT ? [process.env.PROMO_FORMAT] : ["landscape", "vertical"];
// PROMO_PREVIEW=1 renders only the review keyframes, for fast iteration on the composition.
const previewOnly = process.env.PROMO_PREVIEW === "1";
const sizes = { landscape: { width: 1920, height: 1080 }, vertical: { width: 1080, height: 1920 } };
// Dashboard viewport captured for each format; the portrait cut uses a narrower, taller page.
const stillViewports = { landscape: { width: 1440, height: 900 }, vertical: { width: 1000, height: 1150 } };
const frameCount = Math.round(timeline.duration * timeline.fps);
const tempDir = await mkdtemp(path.join(tmpdir(), "codex-usage-promo-"));
// PROMO_ASSETS keeps captured Dashboard stills between runs while iterating on the composition.
const assetsDir = process.env.PROMO_ASSETS ? path.resolve(process.env.PROMO_ASSETS) : path.join(tempDir, "assets");

function outputName(locale, format) {
  return `codex-usage-promo-${locale === "en" ? "en" : "zh"}${format === "vertical" ? "-vertical" : ""}.mp4`;
}

// One review still per second, named after its scene, plus the poster time.
function reviewTimes() {
  const times = [];
  for (let t = .5; t < timeline.duration; t += 1) times.push({ name: timeline.scenes.findLast(s => s.start <= t).id, t });
  return [...times, { name: "poster", t: timeline.poster }];
}

async function openStage(browser, locale, format, manifests) {
  const size = sizes[format];
  const context = await browser.newContext({ viewport: size, deviceScaleFactor: 1, colorScheme: "dark" });
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", error => errors.push(error.message));
  page.on("console", message => { if (message.type() === "error") errors.push(message.text()); });
  page.on("request", request => { if (!request.url().startsWith("file:")) errors.push(`External request: ${request.url()}`); });
  const otherLocale = locale === "en" ? "zh-CN" : "en";
  const other = manifests[`${otherLocale}-${format}`] || manifests[`${locale}-${format}`];
  const icon = (await readFile(path.join(repoRoot, "internal", "web", "static", "icon.svg"), "utf8")).replace(/<\/?title>[^<]*|<desc>[^<]*<\/desc>/g, "");
  await page.addInitScript(data => { window.PROMO = data; }, { timeline, locale, format, icon, assets: manifests[`${locale}-${format}`], other });
  await page.goto(pathToFileURL(path.join(promoDir, "promo.html")).href);
  await page.evaluate(() => window.promo.ready);
  return { context, page, errors };
}

async function renderVideo(browser, locale, format, manifests, audio) {
  const { context, page, errors } = await openStage(browser, locale, format, manifests);
  const review = path.join(reviewDir, `${locale}-${format}`);
  await mkdir(review, { recursive: true });
  try {
    for (const { name, t } of reviewTimes()) {
      await page.evaluate(t => window.promo.seek(t), t);
      await page.screenshot({ path: path.join(review, `${t.toFixed(2).padStart(5, "0")}-${name}.png`) });
    }
    assertSynthetic(await page.locator("body").innerText());
    if (format === "landscape") {
      await page.evaluate(t => { window.promo.seek(t); window.promo.poster(true); }, timeline.poster);
      const poster = path.join(tempDir, `poster-${locale}.png`);
      await page.screenshot({ path: poster });
      await page.evaluate(() => window.promo.poster(false));
      run("ffmpeg", ["-hide_banner", "-loglevel", "error", "-y", "-i", poster, "-q:v", "3", path.join(mediaDir, `codex-usage-promo-poster${locale === "en" ? "-en" : ""}.jpg`)]);
    }
    if (previewOnly) return null;

    const output = path.join(mediaDir, outputName(locale, format));
    const ffmpeg = spawn("ffmpeg", [
      "-hide_banner", "-loglevel", "error", "-y",
      "-f", "image2pipe", "-framerate", String(timeline.fps), "-c:v", "png", "-i", "pipe:0",
      "-i", audio,
      "-map", "0:v", "-map", "1:a",
      "-vf", "scale=out_color_matrix=bt709:out_range=tv,format=yuv420p",
      "-c:v", "libx264", "-preset", "slow", "-crf", "18", "-profile:v", "high", "-g", String(timeline.fps * 2),
      "-colorspace", "bt709", "-color_primaries", "bt709", "-color_trc", "bt709", "-color_range", "tv",
      "-c:a", "aac", "-b:a", "192k", "-shortest", "-movflags", "+faststart", output
    ], { cwd: repoRoot, stdio: ["pipe", "inherit", "inherit"], windowsHide: true });
    const exited = once(ffmpeg, "exit");
    const cdp = await context.newCDPSession(page);
    const started = Date.now();
    for (let frame = 0; frame < frameCount; frame++) {
      await page.evaluate(t => window.promo.seek(t), frame / timeline.fps);
      const { data } = await cdp.send("Page.captureScreenshot", { format: "png", optimizeForSpeed: true });
      if (!ffmpeg.stdin.write(Buffer.from(data, "base64"))) await once(ffmpeg.stdin, "drain");
      if (frame % 150 === 149) console.log(`${locale} ${format}: ${frame + 1}/${frameCount} frames (${((Date.now() - started) / 1000).toFixed(0)}s)`);
    }
    ffmpeg.stdin.end();
    const [code] = await exited;
    if (code !== 0) throw new Error(`ffmpeg exited with ${code} for ${output}`);
    if (errors.length) throw new Error(JSON.stringify({ locale, format, errors }));
    return verify(output, format);
  } finally {
    await context.close();
  }
}

async function verify(file, format) {
  const probe = JSON.parse(run("ffprobe", ["-v", "error", "-count_frames", "-show_entries", "stream=codec_type,codec_name,width,height,avg_frame_rate,nb_read_frames:format=duration", "-of", "json", file], true));
  const video = probe.streams.find(s => s.codec_type === "video");
  const audio = probe.streams.find(s => s.codec_type === "audio");
  const { width, height } = sizes[format];
  const { size } = await stat(file);
  const checks = {
    resolution: video.width === width && video.height === height,
    fps: video.avg_frame_rate === `${timeline.fps}/1`,
    frames: Number(video.nb_read_frames) === frameCount,
    duration: Math.abs(Number(probe.format.duration) - timeline.duration) < .1,
    audio: audio?.codec_name === "aac",
    size: size > 200_000 && size < 30_000_000
  };
  const failed = Object.entries(checks).filter(([, ok]) => !ok).map(([name]) => name);
  if (failed.length) throw new Error(`${file} failed checks: ${failed.join(", ")} ${JSON.stringify(probe)}`);
  return { file: path.relative(repoRoot, file), bytes: size, duration: Number(probe.format.duration), frames: Number(video.nb_read_frames) };
}

await mkdir(mediaDir, { recursive: true });
await mkdir(reviewDir, { recursive: true });
try {
  const browser = await chromium.launch({ headless: true, ...(process.env.PLAYWRIGHT_CHANNEL ? { channel: process.env.PLAYWRIGHT_CHANNEL } : {}) });
  try {
    const manifests = {};
    const server = await startDemoServer();
    try {
      // Both languages are captured so the interface scene can show the language switch.
      for (const locale of ["zh-CN", "en"]) for (const format of formats) {
        const dir = path.join(assetsDir, `${locale}-${format}`);
        const cached = process.env.PROMO_ASSETS ? await readFile(path.join(dir, "manifest.json"), "utf8").then(JSON.parse, () => null) : null;
        const manifest = cached || await captureAssets(browser, server.baseURL, locale, dir, stillViewports[format]);
        manifests[`${locale}-${format}`] = { ...manifest, base: pathToFileURL(dir).href + "/" };
        console.log(`${locale} ${format}: captured ${Object.keys(manifest.shots).length} Dashboard stills`);
      }
    } finally {
      await server.stop();
    }
    const audio = path.join(tempDir, "music.wav");
    if (!previewOnly) await renderMusic(timeline, audio, tempDir);
    const results = [];
    for (const locale of locales) for (const format of formats) {
      const result = await renderVideo(browser, locale, format, manifests, audio);
      if (result) { results.push(result); console.log(`Rendered ${result.file} (${(result.bytes / 1e6).toFixed(1)} MB)`); }
    }
    await writeFile(path.join(reviewDir, "render.json"), JSON.stringify({ timeline, results }, null, 2) + "\n");
  } finally {
    await browser.close();
  }
  console.log(`Review keyframes in ${reviewDir}`);
} finally {
  const tempRoot = path.resolve(tmpdir()) + path.sep;
  if (!path.resolve(tempDir).startsWith(tempRoot) || !path.basename(tempDir).startsWith("codex-usage-promo-")) throw new Error("Unexpected promo cleanup path");
  await rm(tempDir, { recursive: true, force: true });
}
