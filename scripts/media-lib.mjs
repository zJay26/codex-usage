import { expect } from "@playwright/test";
import { once } from "node:events";
import { createServer } from "node:net";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawn, spawnSync } from "node:child_process";
import { setTimeout as delay } from "node:timers/promises";

export const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
export const fixedTime = new Date("2026-09-23T16:30:00+08:00");

export function freePort() {
  return new Promise((resolve, reject) => {
    const probe = createServer();
    probe.once("error", reject);
    probe.listen(0, "127.0.0.1", () => {
      const { port } = probe.address();
      probe.close(error => error ? reject(error) : resolve(port));
    });
  });
}

export function run(command, args, capture = false) {
  const result = spawnSync(command, args, { cwd: repoRoot, stdio: capture ? "pipe" : "inherit", encoding: "utf8", windowsHide: true, maxBuffer: 64 * 1024 * 1024 });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`${command} failed: ${result.stderr || result.status}`);
  return result.stdout;
}

export function assertSynthetic(text) {
  for (const pattern of [/[A-Z]:\\Users\\/i, /\/home\/[a-z0-9._-]+\//i, /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i]) {
    if (pattern.test(text)) throw new Error(`Sensitive-looking value matched ${pattern}`);
  }
}

// Builds the static demo and serves it on a free loopback port until stop() is called.
export async function startDemoServer() {
  const port = await freePort();
  const baseURL = `http://127.0.0.1:${port}/codex-usage/`;
  run(process.execPath, ["scripts/build-demo.mjs", "dist/pages"]);
  const server = spawn(process.execPath, ["scripts/serve-static.mjs", "dist/pages", String(port), "codex-usage"], { cwd: repoRoot, stdio: "ignore", windowsHide: true });
  const deadline = Date.now() + 10_000;
  for (;;) {
    try { if ((await fetch(baseURL)).ok) break; } catch {}
    if (Date.now() > deadline) throw new Error("media demo server did not become ready");
    await delay(100);
  }
  async function stop() {
    if (server.exitCode !== null || server.signalCode !== null) return;
    const exited = once(server, "exit");
    server.kill();
    await Promise.race([exited, delay(5_000)]);
  }
  return { baseURL, stop };
}

export async function openDemo(context, baseURL, locale, { query = "", time = fixedTime } = {}) {
  const page = await context.newPage();
  await page.clock.setFixedTime(time);
  const errors = [];
  page.on("pageerror", error => errors.push(error.message));
  page.on("request", request => {
    if (new URL(request.url()).origin !== new URL(baseURL).origin) errors.push(`External request: ${request.url()}`);
  });
  await page.goto(`${baseURL}?lang=${locale}${query}`, { waitUntil: "networkidle" });
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
