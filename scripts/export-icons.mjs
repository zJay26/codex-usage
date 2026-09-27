import { chromium } from "@playwright/test";
import { readFile, mkdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const assets = path.join(root, "internal", "web", "static");
const svg = await readFile(path.join(assets, "icon.svg"), "utf8");
const channel = process.env.PLAYWRIGHT_CHANNEL;
const browser = await chromium.launch({ headless: true, ...(channel ? { channel } : {}) });

try {
  const page = await browser.newPage({ deviceScaleFactor: 1 });
  await page.setContent(`<!doctype html><style>html,body{margin:0;width:100%;height:100%;overflow:hidden}svg{display:block;width:100%;height:100%}</style>${svg}`);
  for (const [name, size] of [["favicon-32.png", 32], ["apple-touch-icon.png", 180]]) {
    await page.setViewportSize({ width: size, height: size });
    await page.screenshot({ path: path.join(assets, name), omitBackground: true });
  }
  const branding = path.join(root, "docs", "branding");
  await mkdir(branding, { recursive: true });
  await page.setViewportSize({ width: 512, height: 512 });
  await page.screenshot({ path: path.join(branding, "icon.png"), omitBackground: true });
  console.log("Exported D8 browser, touch and preview icons from icon.svg");
} finally {
  await browser.close();
}
