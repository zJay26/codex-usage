import { expect } from "@playwright/test";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { assertSynthetic, fixedTime, openDemo } from "../media-lib.mjs";

const scale = 2;
const scenario = "&scenario=promo";
// The promo labels its synthetic data itself, so the demo notice is left out of the stills.
const stillStyle = "#demoBanner{display:none!important}*,*::before,*::after{caret-color:transparent!important;scroll-behavior:auto!important}";
const hideStyle = selectors => `${selectors}{color:transparent!important;-webkit-text-fill-color:transparent!important}`;
const searchTerms = { "zh-CN": ["支", "支付"], en: ["pay", "payment"] };
const rangeStart = "2026-09-18T10:00";

// Captures crisp Dashboard stills for every beat of the promo, plus the on-screen
// rectangles the camera, spotlight, cursor and labels are anchored to.
export async function captureAssets(browser, baseURL, locale, outDir, viewport) {
  await mkdir(outDir, { recursive: true });
  const contextOptions = { viewport, deviceScaleFactor: scale, colorScheme: "light", timezoneId: "Asia/Shanghai", reducedMotion: "reduce" };
  const shots = {};
  const errors = [];
  const warnings = [];

  async function open(options = {}) {
    const context = await browser.newContext(contextOptions);
    const { page, errors: pageErrors } = await openDemo(context, baseURL, locale, { query: scenario + (options.query || ""), time: options.time });
    await page.addStyleTag({ content: stillStyle });
    await page.locator('[data-overview-range="30d"]').click();
    await settle(page);
    return {
      page,
      async close() {
        warnings.push(...await page.evaluate(() => [...new Set(window.mediaWarnings)]));
        errors.push(...pageErrors);
        await context.close();
      }
    };
  }
  async function settle(page, ms = 450) {
    await page.waitForLoadState("networkidle");
    await delay(ms);
  }
  async function rect(page, target) {
    const locator = typeof target === "string" ? page.locator(target).first() : target;
    const box = await locator.evaluate(node => {
      const r = node.getBoundingClientRect();
      return { x: r.x, y: r.y, w: r.width, h: r.height };
    });
    return Object.fromEntries(Object.entries(box).map(([key, value]) => [key, Math.round(value)]));
  }
  async function scrollTo(page, selector, top = 90) {
    await page.locator(selector).first().evaluate((node, top) => window.scrollTo(0, window.scrollY + node.getBoundingClientRect().top - top), top);
    await delay(250);
  }
  async function shot(page, name, focus = {}, extra = {}) {
    await page.mouse.move(viewport.width - 4, viewport.height - 4);
    await delay(300);
    assertSynthetic(await page.locator("body").innerText());
    await expect(page.locator("#coverageBanner")).toBeHidden();
    const file = `${name}.png`;
    await page.screenshot({ path: path.join(outDir, file) });
    const rects = {};
    for (const [key, target] of Object.entries(focus)) rects[key] = await rect(page, target);
    shots[name] = { file, width: viewport.width, height: viewport.height, focus: rects, ...extra };
  }
  // The typography of a value, so the promo can count it up in place.
  async function textStyle(page, selector) {
    return page.locator(selector).evaluate(node => {
      const style = getComputedStyle(node);
      const r = node.getBoundingClientRect();
      return {
        text: node.textContent.trim(),
        x: Math.round(r.x + parseFloat(style.paddingLeft)), y: Math.round(r.y + parseFloat(style.paddingTop)),
        font: { fontFamily: style.fontFamily, fontSize: style.fontSize, fontWeight: style.fontWeight, letterSpacing: style.letterSpacing, lineHeight: style.lineHeight, color: style.color, fontVariantNumeric: style.fontVariantNumeric, fontFeatureSettings: style.fontFeatureSettings }
      };
    });
  }

  // Overview: totals and cost, then the 30-day daily bars.
  {
    const { page, close } = await open();
    const heroFocus = { hero: ".hero-metrics", totalCard: ".token-metric", costCard: ".cost-metric", total: "#overviewTotal", cost: "#overviewCost" };
    const counters = { total: await textStyle(page, "#overviewTotal"), cost: await textStyle(page, "#overviewCost") };
    await shot(page, "overview", heroFocus, { counters });
    const blank = await page.addStyleTag({ content: hideStyle("#overviewTotal,#overviewCost") });
    await shot(page, "overviewBlank", heroFocus);
    await blank.evaluate(node => node.remove());

    await scrollTo(page, "#usageTrendPanel", 80);
    const dailyFocus = { panel: "#usageTrendPanel", belt: "#pulseScroll", inspector: "#pulseInspector", peak: '[data-pulse-date="2026-09-17"]' };
    const bars = await page.addStyleTag({ content: ".pulse-bar,.pulse-fast-bar{visibility:hidden!important}" });
    await shot(page, "dailyBlank", dailyFocus);
    await bars.evaluate(node => node.remove());
    await shot(page, "daily", dailyFocus);
    await page.locator('[data-pulse-date="2026-09-17"]').click();
    await settle(page);
    await shot(page, "dailyPeak", dailyFocus);

    // Hourly drill-down on the busiest hour of the day.
    await page.locator("#trendHourlyTab").click();
    await settle(page);
    const hourlyFocus = { panel: "#usageTrendPanel", chart: "#hourlyChart", ledger: "#hourlyLedger" };
    const peakIndex = await page.locator("#hourlyPoints [data-hour-point]").evaluateAll(nodes => {
      let best = 0;
      nodes.forEach((node, index) => { if (node.getBoundingClientRect().y < nodes[best].getBoundingClientRect().y) best = index; });
      return best;
    });
    const peakPoint = page.locator(`#hourlyPoints [data-hour-point="${peakIndex}"]`);
    await shot(page, "hourly", { ...hourlyFocus, peak: peakPoint });
    await peakPoint.click();
    await settle(page);
    await shot(page, "hourlyPeak", { ...hourlyFocus, peak: peakPoint });

    // Minute-precision range: since the last reset, until now.
    await page.locator("#trendDailyTab").click();
    await page.evaluate(() => window.scrollTo(0, 0));
    await page.locator('[data-overview-range="custom"]').click();
    await settle(page);
    const rangeFocus = { form: "#customRangeForm", start: "#rangeStart", end: "#rangeEnd", now: "#rangeNow", query: "#queryRange", hero: ".hero-metrics", total: "#overviewTotal" };
    await shot(page, "range0", rangeFocus);
    await page.locator("#rangeStart").fill(rangeStart);
    await shot(page, "range1", rangeFocus);
    await page.locator("#rangeNow").click();
    await shot(page, "range2", rangeFocus);
    await page.locator("#queryRange").click();
    await expect(page.locator("#overviewExactTotal")).toBeVisible();
    await settle(page);
    await shot(page, "range3", { ...rangeFocus, exact: "#overviewExactTotal", totalCard: ".token-metric" });

    // Calendar with the Standard / Fast split of one day.
    await page.locator("#tab-daily").click();
    await settle(page);
    const dayCard = await page.locator("#dayTotal").evaluateHandle(node => node.closest(".surface") || node.parentElement.parentElement);
    const calendarFocus = { grid: "#calendarGrid", cell: '[data-calendar-date="2026-09-17"]', dayCard, dayTotal: "#dayTotal", dayModes: "#dayModes", composition: "#dayComposition" };
    await shot(page, "calendar", calendarFocus);
    await page.locator('[data-calendar-date="2026-09-17"]').click();
    await expect(page.locator("#dayTotal")).not.toHaveText("0");
    await settle(page);
    await shot(page, "calendarPeak", calendarFocus);

    // Details: by model, then by project.
    await page.locator("#tab-details").click();
    await page.locator('[data-dimension="model"]').click();
    await settle(page);
    await shot(page, "models", { panel: "#breakdownPanel", list: "#detailBreakdown", projectTab: '[data-dimension="project"]' });
    await page.locator('[data-dimension="project"]').click();
    await settle(page);
    await shot(page, "projects", { panel: "#breakdownPanel", list: "#detailBreakdown", projectTab: '[data-dimension="project"]' });

    // Session list and search.
    await scrollTo(page, "#sessionsPanel", 70);
    const sessionFocus = { panel: "#sessionsPanel", search: "#sessionSearch", rows: "#sessionRows" };
    await shot(page, "sessions", sessionFocus);
    const terms = searchTerms[locale] || searchTerms.en;
    for (const [index, term] of terms.entries()) {
      await page.locator("#sessionSearch").fill(term);
      await delay(400);
      await settle(page);
      await shot(page, `search${index + 1}`, sessionFocus);
    }
    await page.locator("#sessionSearchClear").click();
    await settle(page);

    // Task tree: collapsed, then expanded to show the subagents.
    await page.locator('[data-session-view="tree"]').click();
    await settle(page);
    await scrollTo(page, "#sessionsPanel", 70);
    const toggle = page.locator("[data-tree-toggle]").first();
    await expect(toggle).toBeVisible();
    // Re-rendering replaces rows, so the row is located afresh for each still.
    const row = '#sessionRows .session-row:has([data-tree-toggle]) .task-name';
    await toggle.click();
    await expect(toggle).toHaveAttribute("aria-expanded", "false");
    await settle(page, 300);
    await shot(page, "tree0", { panel: "#sessionsPanel", rows: "#sessionRows", toggle, row });
    await toggle.click();
    await expect(toggle).toHaveAttribute("aria-expanded", "true");
    await settle(page, 300);
    const children = await page.locator("#sessionRows").evaluateHandle(rows => {
      const all = [...rows.children];
      const first = all.findIndex(node => node.querySelector("[data-tree-toggle]"));
      const next = all.findIndex((node, index) => index > first && node.querySelector("[data-tree-toggle]"));
      const wrap = document.createElement("div");
      const top = all[first].getBoundingClientRect().top, bottom = all[(next > 0 ? next : all.length) - 1].getBoundingClientRect().bottom;
      const r = rows.getBoundingClientRect();
      Object.assign(wrap.style, { position: "fixed", left: `${r.left}px`, top: `${top}px`, width: `${r.width}px`, height: `${bottom - top}px`, pointerEvents: "none" });
      document.body.append(wrap);
      return wrap;
    });
    await shot(page, "tree1", { panel: "#sessionsPanel", rows: "#sessionRows", toggle, row, group: children, subtotal: ".tree-subtotal" });
    await children.evaluate(node => node.remove());

    // Export dialog.
    await page.locator("#exportButton").click();
    await expect(page.locator("#exportJson")).toHaveAttribute("href", /^data:application\/json/);
    await delay(300);
    await shot(page, "export", { dialog: "#exportDialog", json: "#exportJson", csv: "#exportCsv" });
    await page.locator("#exportDialog [data-close]").click();

    // Light theme, then dark.
    await page.locator("#tab-overview").click();
    await page.locator('[data-overview-range="30d"]').click();
    await page.evaluate(() => window.scrollTo(0, 0));
    await settle(page);
    await shot(page, "light", { hero: ".hero-metrics", theme: "#themeButton", locale: "#localeButton" });
    await page.locator("#themeButton").click();
    await settle(page);
    await shot(page, "dark", { hero: ".hero-metrics", theme: "#themeButton", locale: "#localeButton" });
    await page.locator("#themeButton").click();
    await close();
  }

  // Usage keeps counting on its own: the same view a minute and two minutes later.
  for (const [index, minutes] of [0, 1, 2].entries()) {
    const { page, close } = await open({ time: new Date(+fixedTime + minutes * 60_000) });
    await page.locator('[data-overview-range="today"]').click();
    await settle(page);
    await shot(page, `live${index}`, { hero: ".hero-metrics", totalCard: ".token-metric", total: "#overviewTotal" });
    await close();
  }

  // Software updates: the banner, then the dialog.
  {
    const { page, close } = await open({ query: "&update=available" });
    await expect(page.locator("#updateBanner")).toBeVisible();
    await shot(page, "update0", { banner: "#updateBanner", review: "#reviewUpdate" });
    await page.locator("#reviewUpdate").click();
    await expect(page.locator("#updateDialog")).toBeVisible();
    await delay(300);
    await shot(page, "update1", { dialog: "#updateDialog", versions: ".update-versions", status: "#updateStatus", install: "#installUpdate" });
    await close();
  }

  {
    const context = await browser.newContext({ ...contextOptions, viewport: { width: 390, height: 844 }, deviceScaleFactor: 3, isMobile: true, hasTouch: true });
    const mobile = await openDemo(context, baseURL, locale, { query: scenario });
    await mobile.page.addStyleTag({ content: stillStyle });
    await mobile.page.locator('[data-overview-range="30d"]').click();
    await settle(mobile.page);
    assertSynthetic(await mobile.page.locator("body").innerText());
    await mobile.page.screenshot({ path: path.join(outDir, "mobile.png") });
    shots.mobile = { file: "mobile.png", width: 390, height: 844, focus: {} };
    errors.push(...mobile.errors);
    warnings.push(...await mobile.page.evaluate(() => [...new Set(window.mediaWarnings)]));
    await context.close();
  }

  if (warnings.length || errors.length) throw new Error(JSON.stringify({ locale, warnings, errors }));
  const manifest = { locale, viewport, scale, shots };
  await writeFile(path.join(outDir, "manifest.json"), JSON.stringify(manifest, null, 2) + "\n");
  return manifest;
}
