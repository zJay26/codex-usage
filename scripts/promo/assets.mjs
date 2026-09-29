import { expect } from "@playwright/test";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { assertSynthetic, openDemo } from "../media-lib.mjs";

const scale = 2;
// The promo labels its synthetic data itself, so the demo notice is left out of the stills.
const stillStyle = "#demoBanner{display:none!important}*,*::before,*::after{caret-color:transparent!important;scroll-behavior:auto!important}";

// Captures crisp Dashboard stills plus the on-screen rectangles the promo camera zooms into.
export async function captureAssets(browser, baseURL, locale, outDir, viewport) {
  await mkdir(outDir, { recursive: true });
  const context = await browser.newContext({ viewport, deviceScaleFactor: scale, colorScheme: "light", timezoneId: "Asia/Shanghai", reducedMotion: "reduce" });
  const { page, errors } = await openDemo(context, baseURL, locale);
  await page.addStyleTag({ content: stillStyle });
  const shots = {};

  async function rect(selector) {
    const box = await page.locator(selector).first().evaluate(node => {
      const r = node.getBoundingClientRect();
      return { x: r.x, y: r.y, w: r.width, h: r.height };
    });
    return Object.fromEntries(Object.entries(box).map(([key, value]) => [key, Math.round(value)]));
  }
  async function scrollTo(selector, top = 80) {
    await page.locator(selector).first().evaluate((node, top) => window.scrollTo(0, window.scrollY + node.getBoundingClientRect().top - top), top);
    await delay(250);
  }
  async function shot(name, focus) {
    await page.mouse.move(viewport.width - 4, viewport.height - 4);
    await delay(350);
    assertSynthetic(await page.locator("body").innerText());
    await expect(page.locator("#coverageBanner")).toBeHidden();
    const file = `${name}.png`;
    await page.screenshot({ path: path.join(outDir, file) });
    const focusRects = {};
    for (const [key, selector] of Object.entries(focus)) focusRects[key] = await rect(selector);
    shots[name] = { file, width: viewport.width, height: viewport.height, focus: focusRects };
  }

  await shot("overview", { hero: ".hero-metrics", total: "#overviewTotal", cost: "#overviewCost", totalCard: ".token-metric", costCard: ".cost-metric" });
  const values = {
    total: (await page.locator("#overviewTotal").innerText()).trim(),
    cost: (await page.locator("#overviewCost").innerText()).trim(),
    coverage: (await page.locator("#overviewCoverage").innerText()).trim()
  };

  await page.locator('[data-overview-range="custom"]').click();
  await page.locator("#rangeStart").fill("2026-09-22T09:00");
  await page.locator("#rangeEnd").fill("2026-09-23T16:30");
  await page.locator("#queryRange").click();
  await expect(page.locator("#overviewExactTotal")).toBeVisible();
  await shot("range", { form: "#customRangeForm", hero: ".hero-metrics", exact: "#overviewExactTotal" });

  await page.locator('[data-overview-range="7d"]').click();
  await scrollTo("#usageTrendPanel", 90);
  await page.locator("#trendHourlyTab").click();
  await page.locator("#hourlyPoints .hour-point:not(.zero)").nth(8).click();
  await delay(300);
  await shot("hourly", { panel: "#usageTrendPanel", chart: "#hourlyChart" });

  await page.locator("#trendDailyTab").click();
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.locator("#tab-daily").click();
  await page.locator('[data-calendar-date="2026-09-22"]').click();
  await expect(page.locator("#dayTotal")).not.toHaveText("0");
  await shot("calendar", { calendar: "#calendarGrid", day: "#dayTotal" });

  await page.locator("#tab-details").click();
  await page.locator('[data-dimension="project"]').click();
  await delay(300);
  await shot("projects", { panel: "#breakdownPanel" });

  await scrollTo("#sessionsPanel", 70);
  await page.locator('[data-session-view="tree"]').click();
  await expect(page.locator("[data-tree-toggle]").first()).toBeVisible();
  await delay(300);
  await shot("tree", { panel: "#sessionsPanel", rows: "#sessionRows" });

  await page.locator('[data-session-view="list"]').click();
  await page.locator("#sessionSearch").fill("local");
  await delay(700);
  await shot("search", { panel: "#sessionsPanel", search: "#sessionSearch" });
  await page.locator("#sessionSearchClear").click();

  await page.locator("#tab-overview").click();
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.locator("#filterButton").click();
  await page.locator("#filterMode").selectOption("fast");
  await page.locator("#applyFilters").click();
  await delay(600);
  await shot("fast", { chips: "#filterContext", hero: ".hero-metrics" });

  await page.locator("#exportButton").click();
  await expect(page.locator("#exportJson")).toHaveAttribute("href", /^data:application\/json/);
  await shot("export", { dialog: "#exportDialog" });
  await page.locator("#exportDialog [data-close]").click();
  await page.locator("#clearFilters").click();
  await delay(400);

  await page.locator("#pricingButton").click();
  await page.locator(".catalog-disclosure summary").click();
  await delay(300);
  await shot("pricing", { dialog: "#pricingDialog", catalog: "#pricingCatalog" });
  await page.locator("#pricingDialog .dialog-close").click();

  await page.evaluate(() => window.scrollTo(0, 0));
  await delay(200);
  await shot("light", { hero: ".hero-metrics" });
  await page.locator("#themeButton").click();
  await delay(500);
  await shot("dark", { hero: ".hero-metrics" });
  await page.locator("#themeButton").click();
  await delay(300);

  const warnings = await page.evaluate(() => [...new Set(window.mediaWarnings)]);
  await context.close();

  const mobileContext = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 3, isMobile: true, hasTouch: true, colorScheme: "light", timezoneId: "Asia/Shanghai", reducedMotion: "reduce" });
  const mobile = await openDemo(mobileContext, baseURL, locale);
  await mobile.page.addStyleTag({ content: stillStyle });
  await delay(400);
  assertSynthetic(await mobile.page.locator("body").innerText());
  await mobile.page.screenshot({ path: path.join(outDir, "mobile.png") });
  shots.mobile = { file: "mobile.png", width: 390, height: 844, focus: {} };
  errors.push(...mobile.errors);
  warnings.push(...await mobile.page.evaluate(() => [...new Set(window.mediaWarnings)]));
  await mobileContext.close();

  if (warnings.length || errors.length) throw new Error(JSON.stringify({ locale, warnings, errors }));
  const manifest = { locale, viewport, scale, values, shots };
  await writeFile(path.join(outDir, "manifest.json"), JSON.stringify(manifest, null, 2) + "\n");
  return manifest;
}
