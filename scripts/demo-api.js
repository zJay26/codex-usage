(function (root) {
  "use strict";

  root.CODEX_USAGE_DEMO = true;
  // Keep incomplete accounting examples available for diagnostics, while the
  // default demo represents a healthy ledger with fully priced current models.
  const params = new URL(root.location.href).searchParams;
  const diagnostics = params.get("scenario") === "diagnostics";
  // The promo film uses a larger, heavier-use ledger; the default demo stays unchanged.
  const promo = params.get("scenario") === "promo";
  const promoEnglish = (params.get("lang") || "").startsWith("en");
  const now = new Date();
  let pricingOverrides = {};

  const baseUsage = {
    input: 8_740_000,
    cached_input: 5_210_000,
    cache_write_input: 240_000,
    output: 1_430_000,
    reasoning_output: 620_000,
    total: 10_170_000
  };
  const catalog = [
    { model: "gpt-6-astra", display_name: "GPT-6 Astra", input_usd_per_million: "10.00", cached_input_usd_per_million: "1.00", cache_write_input_usd_per_million: "12.50", output_usd_per_million: "50.00", source: "#synthetic-pricing" },
    { model: "gpt-6-sol", display_name: "GPT-6 Sol", input_usd_per_million: "2.00", cached_input_usd_per_million: "0.20", cache_write_input_usd_per_million: "2.50", output_usd_per_million: "10.00", source: "#synthetic-pricing" },
    { model: "gpt-6-luna", display_name: "GPT-6 Luna", input_usd_per_million: "0.10", cached_input_usd_per_million: "0.01", cache_write_input_usd_per_million: "0.125", output_usd_per_million: "0.50", source: "#synthetic-pricing" },
    { model: "gpt-5.6-sol", display_name: "GPT-5.6 Sol", input_usd_per_million: "5.00", cached_input_usd_per_million: "0.50", cache_write_input_usd_per_million: "6.25", output_usd_per_million: "30.00", source: "#synthetic-pricing" },
    { model: "gpt-5.6-terra", display_name: "GPT-5.6 Terra", input_usd_per_million: "2.00", cached_input_usd_per_million: "0.20", cache_write_input_usd_per_million: "2.50", output_usd_per_million: "12.00", source: "#synthetic-pricing" },
    { model: "gpt-5.4", display_name: "GPT-5.4", input_usd_per_million: "2.50", cached_input_usd_per_million: "0.25", output_usd_per_million: "15.00", source: "#synthetic-pricing" }
  ];
  const models = promo ? [
    { key: "gpt-6-sol", share: .52, events: 9_610, sessions: 182 },
    { key: "gpt-6-astra", share: .27, events: 4_870, sessions: 94 },
    { key: "gpt-6-luna", share: .13, events: 2_740, sessions: 121 },
    { key: "gpt-5.6-terra", share: .08, events: 1_242, sessions: 37 }
  ] : [
    { key: diagnostics ? "gpt-5.4" : "gpt-6-sol", share: .57, events: 71, sessions: 15 },
    { key: diagnostics ? "gpt-5.6-terra" : "gpt-6-astra", share: .31, events: 32, sessions: 8 },
    { key: diagnostics ? "codex-auto-review" : "gpt-6-luna", share: .12, events: 11, sessions: 3 }
  ];
  const agents = promo ? [
    { key: "main", share: .71, events: 12_380, sessions: 214 },
    { key: "subagent", share: .21, events: 4_960, sessions: 97 },
    { key: "guardian", share: .05, events: 812, sessions: 23 },
    { key: "memory", share: .03, events: 310, sessions: 12 }
  ] : [
    { key: "main", share: .75, events: 80, sessions: 20 },
    { key: "subagent", share: .17, events: 21, sessions: 6 },
    { key: "guardian", share: .05, events: 8, sessions: 4 },
    { key: "memory", share: .03, events: 5, sessions: 2 }
  ];
  const sources = promo ? [
    { key: "codex_desktop", share: .64, events: 11_730, sessions: 226 },
    { key: "codex_cli_rs", share: .36, events: 6_732, sessions: 120 }
  ] : [
    { key: "codex_desktop", share: .68, events: 70, sessions: 16 },
    { key: "codex_cli_rs", share: .32, events: 44, sessions: 10 }
  ];
  const projects = promo ? [
    { key: "~/code/payments-api", share: .31, events: 5_720, sessions: 96 },
    { key: "~/code/web-console", share: .22, events: 4_060, sessions: 81 },
    { key: "~/code/mobile-app", share: .14, events: 2_590, sessions: 52 },
    { key: "~/code/data-pipeline", share: .12, events: 2_210, sessions: 41 },
    { key: "~/code/ml-experiments", share: .09, events: 1_660, sessions: 29 },
    { key: "~/code/infra", share: .08, events: 1_480, sessions: 33 },
    { key: "~/code/docs-site", share: .04, events: 742, sessions: 14 }
  ] : [
    { key: "synthetic://visual-lab", share: .62, events: 70, sessions: 12 },
    { key: "synthetic://service-api", share: .38, events: 44, sessions: 8 }
  ];
  const threads = promo ? [] : [
    { key: "Dashboard localization", share: .61, events: 12, sessions: 1 },
    { key: "JSONL fork replay audit", share: .39, events: 9, sessions: 1 }
  ];
  const sessions = promo ? [] : [
    { session_id: "demo-session-a", title: "Dashboard localization", project_path: "synthetic://visual-lab", model: models[0].key, source: "codex_desktop", agent_type: "main", share: .48, confidence: "exact", hoursAgo: 0 },
    { session_id: "demo-session-b", title: "JSONL fork replay audit", project_path: "synthetic://service-api", model: models[1].key, source: "codex_cli_rs", agent_type: "subagent", share: .31, confidence: diagnostics ? "gap_fallback" : "exact", hoursAgo: 2 },
    { session_id: "demo-session-c", title: "Local data-quality review", project_path: "synthetic://visual-lab", model: models[2].key, source: "codex_desktop", agent_type: "guardian", share: .21, confidence: "exact", hoursAgo: 7 }
  ];

  const pad = (value) => String(value).padStart(2, "0");
  const dateKey = (date) => `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
  const hourKey = (date) => `${dateKey(date)}T${pad(date.getHours())}`;
  const rangeDate = (value) => new Date(/^\d{4}-\d{2}-\d{2}$/.test(value) ? `${value}T00:00:00` : value);
  function requestedBounds(url, fallbackStart = null, fallbackEnd = null) {
    let start = url.searchParams.get("since") ? rangeDate(url.searchParams.get("since")) : null;
    let end = url.searchParams.get("until") ? rangeDate(url.searchParams.get("until")) : null;
    const selectedDate = url.searchParams.get("date");
    if (/^\d{4}-\d{2}-\d{2}$/.test(selectedDate || "")) {
      const dayStart = rangeDate(selectedDate);
      const dayEnd = new Date(dayStart);
      dayEnd.setDate(dayEnd.getDate() + 1);
      if (!start || dayStart > start) start = dayStart;
      if (!end || dayEnd < end) end = dayEnd;
    }
    return { start: start || fallbackStart, end: end || fallbackEnd };
  }
  const scaledUsage = (usage, multiplier) => Object.fromEntries(Object.entries(usage).map(([key, value]) => [key, Math.round(value * multiplier)]));
  const jsonResponse = (value, status = 200) => new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json; charset=utf-8", "Cache-Control": "no-store" }
  });


  const usageKeys = ["input", "cached_input", "cache_write_input", "output", "reasoning_output", "total"];
  const zeroUsage = () => Object.fromEntries(usageKeys.map((key) => [key, 0]));
  const addUsage = (a, b) => Object.fromEntries(usageKeys.map((key) => [key, (a[key] || 0) + (b[key] || 0)]));
  // ---- Promo ledger -------------------------------------------------------
  // Usage is generated hour by hour from a deterministic workday rhythm, so every
  // view (range totals, daily bars, hours, calendar, breakdowns) agrees exactly.
  const promoHourWeights = [.3, .14, .05, .02, .02, .03, .06, .22, .62, 1.3, 1.72, 1.6, .86, .74, 1.42, 1.82, 1.94, 1.7, 1.28, .9, .84, .92, .8, .5];
  const promoHourSum = promoHourWeights.reduce((sum, weight) => sum + weight, 0);
  const promoWeekday = [.3, 1.02, 1.12, 1.08, 1.18, .94, .38];
  const promoSpecialDays = { "2026-09-17": 1.85, "2026-09-06": 0, "2026-08-23": 0, "2026-08-09": 0 };
  const promoDaySeed = (date) => Math.round(new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime() / 86_400_000);
  function promoDayFactor(date) {
    const seed = promoDaySeed(date);
    const special = promoSpecialDays[dateKey(date)];
    const age = (now - date) / 86_400_000;
    const trend = .8 + .2 * Math.max(0, Math.min(1, 1 - age / 60));
    return promoWeekday[date.getDay()] * (1 + .15 * Math.sin(seed * 1.37) + .07 * Math.sin(seed * .41 + 1)) * trend * (special ?? 1);
  }
  const promoFastShare = (date) => .22 + .11 * Math.sin(promoDaySeed(date) * .9);
  function promoHourRate(hourStart) {
    const hourSeed = Math.round(hourStart.getTime() / 3_600_000);
    return promoDayFactor(hourStart) * promoHourWeights[hourStart.getHours()] / promoHourSum * (1 + .12 * Math.sin(hourSeed * .77));
  }
  // Raw token and Fast weights between two instants, never past "now".
  function promoRaw(start, end) {
    const stop = Math.min(+end, +now);
    let total = 0, fast = 0;
    const cursor = new Date(start);
    cursor.setMinutes(0, 0, 0);
    for (; +cursor < stop; cursor.setHours(cursor.getHours() + 1)) {
      const next = new Date(cursor);
      next.setHours(next.getHours() + 1);
      const overlap = Math.max(0, Math.min(stop, +next) - Math.max(+start, +cursor)) / (next - cursor);
      if (!overlap) continue;
      const value = promoHourRate(cursor) * overlap;
      total += value;
      fast += value * promoFastShare(cursor);
    }
    return { total, fast };
  }
  const promoScale = promo ? 2_380_000_000 / promoRaw(new Date(now.getFullYear(), now.getMonth(), now.getDate() - 29), now).total : 1;
  const promoComposition = (total) => {
    const input = Math.round(total * .979);
    const output = total - input;
    return { input, cached_input: Math.round(input * .938), cache_write_input: Math.round(input * .006), output, reasoning_output: Math.round(output * .52), total };
  };
  function promoUsage(start, end, scale = 1) {
    const raw = promoRaw(start, end);
    return { usage: promoComposition(Math.round(raw.total * promoScale * scale)), fast_share: raw.total ? raw.fast / raw.total : 0 };
  }
  function promoBounds(url) {
    const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const { start, end } = requestedBounds(url);
    return { start: start || new Date(today.getFullYear(), today.getMonth(), today.getDate() - 29), end: end || new Date(+now + 1) };
  }
  // Session titles read like real tasks, in the viewer's language.
  const promoSessionRows = [
    ["7f3a9c21", "", "重构订单状态机，补齐退款分支", "Refactor the order state machine and add refund paths", "payments-api", "gpt-6-sol", "codex_desktop", "main", 128, .3],
    ["7f3a9c21-s1", "7f3a9c21", "梳理退款相关的单元测试", "Map out the refund unit tests", "payments-api", "gpt-6-luna", "codex_desktop", "subagent", 42, .4],
    ["7f3a9c21-s2", "7f3a9c21", "排查幂等键冲突", "Investigate idempotency key collisions", "payments-api", "gpt-6-sol", "codex_desktop", "subagent", 36, .6],
    ["7f3a9c21-g1", "7f3a9c21", "审查数据库迁移脚本", "Review the database migration scripts", "payments-api", "gpt-6-astra", "codex_desktop", "guardian", 12, .5],
    ["b41e0d87", "", "修复支付回调重复入账", "Fix duplicate ledger entries from payment webhooks", "payments-api", "gpt-6-astra", "codex_cli_rs", "main", 96, 2],
    ["b41e0d87-s1", "b41e0d87", "复现支付回调重放场景", "Reproduce payment webhook replays", "payments-api", "gpt-6-sol", "codex_cli_rs", "subagent", 31, 2.2],
    ["2c9d5f10", "", "Web 控制台新增用量图表", "Add usage charts to the web console", "web-console", "gpt-6-sol", "codex_desktop", "main", 88, 4],
    ["2c9d5f10-s1", "2c9d5f10", "调整暗色主题配色", "Tune the dark theme palette", "web-console", "gpt-6-luna", "codex_desktop", "subagent", 18, 4.3],
    ["2c9d5f10-s2", "2c9d5f10", "补充图表 E2E 测试", "Add end-to-end tests for charts", "web-console", "gpt-6-sol", "codex_desktop", "subagent", 27, 4.6],
    ["e8a1467b", "", "移动端登录改用 Passkey", "Move mobile sign-in to passkeys", "mobile-app", "gpt-6-astra", "codex_desktop", "main", 74, 7],
    ["e8a1467b-s1", "e8a1467b", "对比 iOS 与 Android 行为差异", "Compare iOS and Android behavior", "mobile-app", "gpt-6-luna", "codex_desktop", "subagent", 16, 7.2],
    ["5d02bb39", "", "数据管道改为增量同步", "Make the data pipeline sync incrementally", "data-pipeline", "gpt-6-sol", "codex_cli_rs", "main", 69, 22],
    ["5d02bb39-s1", "5d02bb39", "压测批量写入", "Load-test batched writes", "data-pipeline", "gpt-6-sol", "codex_cli_rs", "subagent", 22, 22.4],
    ["5d02bb39-m1", "5d02bb39", "总结上周的调优记录", "Summarize last week's tuning notes", "data-pipeline", "gpt-6-luna", "codex_cli_rs", "memory", 6, 22.1],
    ["90fe13a4", "", "Terraform 拆分生产与预发环境", "Split production and staging in Terraform", "infra", "gpt-5.6-terra", "codex_cli_rs", "main", 51, 26],
    ["48e2f7d0", "", "文档站迁移到新主题", "Move the docs site to the new theme", "docs-site", "gpt-6-luna", "codex_desktop", "main", 23, 30],
    ["3b7c68e2", "", "升级依赖并修复破坏性变更", "Upgrade dependencies and fix breaking changes", "web-console", "gpt-6-sol", "codex_desktop", "main", 64, 49],
    ["c61d0a95", "", "排查支付服务内存泄漏", "Track down a memory leak in the payment service", "payments-api", "gpt-6-astra", "codex_cli_rs", "main", 58, 52],
    ["a0c93e5b", "", "推荐模型特征回填", "Backfill features for the ranking model", "ml-experiments", "gpt-6-astra", "codex_cli_rs", "main", 47, 70],
    ["6f18d2c4", "", "API 网关增加限流", "Add rate limiting to the API gateway", "infra", "gpt-6-sol", "codex_cli_rs", "main", 39, 75],
    ["d7b40a19", "", "CI 迁移到 GitHub Actions", "Move CI to GitHub Actions", "infra", "gpt-5.6-terra", "codex_cli_rs", "main", 33, 96],
    ["1e6c9f73", "", "修复 Safari 日期选择器", "Fix the date picker on Safari", "web-console", "gpt-6-luna", "codex_desktop", "main", 12, 100],
    ["8a5b2d06", "", "支付订单导出 CSV", "Export payment orders to CSV", "payments-api", "gpt-6-sol", "codex_desktop", "main", 29, 120]
  ];
  if (promo) {
    for (const [id, parent, zh, en, project, model, source, agent, millions, hoursAgo] of promoSessionRows) {
      sessions.push({ session_id: `rollout-${id}`, parent: parent ? `rollout-${parent}` : "", title: promoEnglish ? en : zh, project_path: `~/code/${project}`, model, source, agent_type: agent, tokens: millions * 1e6 + (millions * 7_919) % 1e6, confidence: "exact", hoursAgo });
    }
    const roots = sessions.filter((item) => !item.parent);
    const rootTotal = roots.reduce((sum, item) => sum + item.tokens, 0);
    for (const item of roots.slice(0, 8)) threads.push({ key: item.title, share: item.tokens / rootTotal * .8, events: Math.round(item.tokens / 190_000), sessions: 1 + sessions.filter((child) => child.parent === item.session_id).length });
  }

  function modeUsage(usage, mode, share = .25) {
    const fast = scaledUsage(usage, share);
    fast.total = fast.input + fast.output;
    const regular = Object.fromEntries(usageKeys.map((key) => [key, usage[key] - fast[key]]));
    const unknown = promo ? zeroUsage() : scaledUsage(regular, .2); unknown.total = unknown.input + unknown.output;
    if (mode === "fast") return { regular: zeroUsage(), fast, unknown: zeroUsage() };
    if (mode === "regular") return { regular, fast: zeroUsage(), unknown };
    if (mode === "unknown") return { regular: unknown, fast: zeroUsage(), unknown };
    return { regular, fast, unknown };
  }
  function demoEstimate(modes, name, weighted) {
    const estimate = { usd: "0.000000000", regular_input_usd: "0.000000000", cached_input_usd: "0.000000000", cache_write_input_usd: "0.000000000", output_usd: "0.000000000", regular_mode_usd: "0.000000000", fast_mode_usd: "0.000000000", standard_base_usd: "0.000000000", fast_surcharge_usd: "0.000000000", priced_tokens: 0, unpriced_tokens: 0, coverage_ratio: 0, reasons: [] };
    const names = name ? [{ key: name, share: 1 }] : models;
    const unpriced = (kind, model, tokens) => {
      if (!tokens) return;
      estimate.unpriced_tokens += tokens;
      const existing = estimate.reasons.find((item) => item.kind === kind && item.model === model);
      if (existing) existing.tokens += tokens;
      else estimate.reasons.push({kind, model, tokens, detail: "Synthetic pricing evidence is incomplete."});
    };
    for (const part of names) {
      const override = pricingOverrides[part.key];
      const alias = override?.alias_of || part.key;
      const rate = override?.input_usd_per_million != null ? override : catalog.find((item) => item.model === alias);
      for (const mode of ["regular", "fast"]) {
        const u = scaledUsage(modes[mode], part.share);
        const total = u.input + u.output;
        if (!rate) { unpriced("unknown_model", part.key, total); continue; }
        const categories = {
          regular_input_usd: (u.input-u.cached_input-u.cache_write_input)*Number(rate.input_usd_per_million)/1e6,
          cached_input_usd: u.cached_input*Number(rate.cached_input_usd_per_million)/1e6,
          cache_write_input_usd: u.cache_write_input*Number(rate.cache_write_input_usd_per_million||0)/1e6,
          output_usd: u.output*Number(rate.output_usd_per_million)/1e6
        };
        const base = Object.values(categories).reduce((sum, amount) => sum+amount, 0);
        estimate.standard_base_usd = (Number(estimate.standard_base_usd)+base).toFixed(9);
        const missingWrite = rate.cache_write_input_usd_per_million == null ? u.cache_write_input : 0;
        unpriced("cache_write_rate_missing", part.key, missingWrite);
        const factor = {"gpt-6-astra":2.5,"gpt-6-sol":2.5,"gpt-6-luna":2.5,"gpt-5.6-sol":2.5,"gpt-5.6-terra":2.5,"gpt-5.6-luna":2.5,"gpt-5.5":2.5,"gpt-5.4":2}[alias];
        if (weighted && mode === "fast" && !factor) { unpriced("fast_multiplier_missing", part.key, total-missingWrite); continue; }
        const multiplier = weighted && mode === "fast" ? factor : 1;
        for (const [key, amount] of Object.entries(categories)) estimate[key] = (Number(estimate[key])+amount*multiplier).toFixed(9);
        estimate.usd = (Number(estimate.usd)+base*multiplier).toFixed(9);
        estimate[mode === "fast" ? "fast_mode_usd" : "regular_mode_usd"] = (Number(estimate[mode === "fast" ? "fast_mode_usd" : "regular_mode_usd"])+base*multiplier).toFixed(9);
        estimate.fast_surcharge_usd = (Number(estimate.fast_surcharge_usd)+base*(multiplier-1)).toFixed(9);
        estimate.priced_tokens += total - missingWrite;
      }
    }
    const total = estimate.priced_tokens+estimate.unpriced_tokens;
    estimate.coverage_ratio = total ? estimate.priced_tokens/total : 0;
    return estimate;
  }
  function withModes(payload, url) {
    if (Array.isArray(payload)) return payload.map((row) => withModes(row, url));
    if (!payload || typeof payload !== "object") return payload;
    for (const key of ["points", "models", "items"]) if (payload[key]) payload[key] = withModes(payload[key], url);
    if (payload.usage) {
      payload.modes = modeUsage(payload.usage, url.searchParams.get("mode"), payload.fast_share ?? .25);
      delete payload.fast_share;
      payload.usage = addUsage(payload.modes.regular, payload.modes.fast);
      if (payload.grand_total != null) payload.grand_total = payload.usage.total;
      if (payload.estimate) payload.estimate = demoEstimate(payload.modes, payload.model || (models.some((m) => m.key === payload.key) ? payload.key : url.searchParams.get("model")), url.searchParams.get("cost_basis") === "codex_fast_weighted");
    }
    if (payload.summary && payload.points) {
      payload.modes = {regular:zeroUsage(),fast:zeroUsage(),unknown:zeroUsage()};
      for (const point of payload.points) for (const mode of ["regular","fast","unknown"]) payload.modes[mode]=addUsage(payload.modes[mode],point.modes[mode]);
      payload.summary = demoEstimate(payload.modes, url.searchParams.get("model"), url.searchParams.get("cost_basis") === "codex_fast_weighted");
      payload.basis = url.searchParams.get("cost_basis") || "current_standard_api_text_token_prices";
      payload.fast_rules_as_of = "2026-09-23";
    }
    return payload;
  }

  function filterScale(url) {
    let scale = 1;
    for (const key of ["model", "source", "agent_type", "project", "confidence"]) {
      if (url.searchParams.get(key)) scale *= .42;
    }
    return scale;
  }

  function summary(url) {
    if (promo) {
      const { start, end } = promoBounds(url);
      const { usage, fast_share } = promoUsage(start, end, filterScale(url));
      return { usage, fast_share, unattributed: zeroUsage(), grand_total: usage.total,
        event_count: Math.round(usage.total / 128_000), session_count: Math.min(346, Math.max(1, Math.round(usage.total / 6_900_000))), coverage_incomplete: false };
    }
    if (url.searchParams.get("fill_days") === "0" || /T\d{2}:\d{2}$/.test(url.searchParams.get("since") || "")) {
      const points = dailyPoints(url);
      const usage = points.reduce((sum, point) => addUsage(sum, point.usage), zeroUsage());
      return { usage, unattributed: zeroUsage(), grand_total: usage.total,
        event_count: points.filter((point) => point.usage.total > 0).length,
        session_count: usage.total ? sessions.length : 0, coverage_incomplete: false };
    }
    const { start, end } = requestedBounds(url);
    const dayScale = start && end ? Math.min(1, Math.max(1 / 24, (end - start) / 86_400_000) / 30) : 1;
    const usage = scaledUsage(baseUsage, dayScale * filterScale(url));
    return {
      usage,
      unattributed: scaledUsage(baseUsage, 0),
      grand_total: usage.total,
      event_count: Math.max(1, Math.round(114 * dayScale * filterScale(url))),
      session_count: Math.max(1, Math.round(26 * dayScale * filterScale(url))),
      coverage_incomplete: false
    };
  }

  function dailyPoints(url) {
    if (promo) {
      const { start, end } = promoBounds(url);
      const points = [];
      for (let date = new Date(start.getFullYear(), start.getMonth(), start.getDate()), index = 0; date < end && index < 120; date.setDate(date.getDate() + 1), index++) {
        const next = new Date(date.getFullYear(), date.getMonth(), date.getDate() + 1);
        const { usage, fast_share } = promoUsage(new Date(Math.max(+date, +start)), new Date(Math.min(+next, +end)), filterScale(url));
        points.push({ date: dateKey(date), time: new Date(date).toISOString(), usage, fast_share, estimate: {} });
      }
      return points;
    }
    const { start, end } = requestedBounds(
      url,
      new Date(now.getFullYear(), now.getMonth(), now.getDate() - 29),
      new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1)
    );
    const points = [];
    let index = 0;
    const firstDay = new Date(start);
    firstDay.setHours(0, 0, 0, 0);
    for (let date = firstDay; date < end && index < 120; date.setDate(date.getDate() + 1), index++) {
      const seed = Math.floor(date.getTime() / 86_400_000);
      const nextDay = new Date(date);
      nextDay.setDate(nextDay.getDate() + 1);
      const fraction = Math.max(0, Math.min(+end, +nextDay) - Math.max(+start, +date)) / (nextDay - date);
      const total = seed % 11 === 3 ? 0 : Math.round((180_000 + (Math.sin(seed * .73) + 1.25) * 118_000 + (seed % 31) * 4_100) * filterScale(url) * fraction);
      const input = Math.round(total * .82);
      const output = total - input;
      const usage = { input, cached_input: Math.round(input * .56), cache_write_input: Math.round(input * .025), output, reasoning_output: Math.round(output * .37), total };
      const custom = Boolean(pricingOverrides["codex-auto-review"]);
      const priced = Math.round(total * (custom ? 1 : .88));
      const unpriced = total - priced;
      const usd = (priced / 1_000_000 * 3.18).toFixed(9);
      points.push({
        date: dateKey(date),
        time: new Date(date).toISOString(),
        usage,
        estimate: {
          usd,
          regular_input_usd: usd,
          cached_input_usd: "0.000000000",
          cache_write_input_usd: "0.000000000",
          output_usd: "0.000000000",
          priced_tokens: priced,
          unpriced_tokens: unpriced,
          coverage_ratio: total ? priced / total : 0,
          reasons: unpriced ? [{ kind: "unknown_model", model: "codex-auto-review", tokens: unpriced, detail: "Synthetic model has no public API rate or local override." }] : []
        }
      });
    }
    return points;
  }

  function hourlyPoints(url) {
    const currentHour = new Date(now);
    currentHour.setMinutes(0, 0, 0);
    let { start, end } = requestedBounds(url, new Date(currentHour.getTime() - 24 * 60 * 60_000), currentHour);
    if(url.searchParams.get("complete_hours")==="1" && end>currentHour) end=currentHour;
    const points = [];
    let index = 0;
    if (promo) {
      for (let date = new Date(start); date < end && index < 168; date = new Date(date.getTime() + 60 * 60_000), index++) {
        const { usage, fast_share } = promoUsage(date, new Date(date.getTime() + 60 * 60_000), filterScale(url));
        points.push({ date: hourKey(date), time: date.toISOString(), usage, fast_share });
      }
      return points;
    }
    for (let date = new Date(start); date < end && index < 168; date = new Date(date.getTime() + 60 * 60_000), index++) {
      const seed = Math.floor(date.getTime() / 3_600_000);
      const total = seed % 13 === 4 ? 0 : Math.round((38_000 + (Math.sin(seed * .61) + 1.2) * 29_000 + (seed % 17) * 900) * filterScale(url));
      const input = Math.round(total * .81);
      const output = total - input;
      points.push({
        date: hourKey(date),
        time: date.toISOString(),
        usage: { input, cached_input: Math.round(input * .54), cache_write_input: Math.round(input * .02), output, reasoning_output: Math.round(output * .35), total }
      });
    }
    return points;
  }

  const fastShareOf = (points) => {
    const total = points.reduce((sum, point) => sum + point.usage.total, 0);
    return total ? points.reduce((sum, point) => sum + point.usage.total * (point.fast_share ?? .25), 0) / total : 0;
  };
  function costEstimate(url) {
    const points = dailyPoints(url);
    const usage = points.reduce((sum, point) => {
      for (const key of Object.keys(point.usage)) sum[key] = (sum[key] || 0) + point.usage[key];
      return sum;
    }, {});
    const pricedTokens = points.reduce((sum, point) => sum + (point.estimate.priced_tokens ?? point.usage.total), 0);
    const unpricedTokens = points.reduce((sum, point) => sum + (point.estimate.unpriced_tokens || 0), 0);
    const totalCost = points.reduce((sum, point) => sum + Number(point.estimate.usd || 0), 0);
    const estimate = {
      usd: totalCost.toFixed(9), regular_input_usd: totalCost.toFixed(9), cached_input_usd: "0.000000000", cache_write_input_usd: "0.000000000", output_usd: "0.000000000",
      priced_tokens: pricedTokens, unpriced_tokens: unpricedTokens,
      coverage_ratio: pricedTokens + unpricedTokens ? pricedTokens / (pricedTokens + unpricedTokens) : 0,
      reasons: unpricedTokens ? [{ kind: "unknown_model", model: "codex-auto-review", tokens: unpricedTokens, detail: "Synthetic model has no public API rate or local override." }] : []
    };
    return {
      basis: "current_standard_api_text_token_prices", currency: "USD", catalog_as_of: "2026-09-23", bucket: "day", summary: estimate, points,
      models: models.map((item) => {
        const itemUsage = scaledUsage(usage, item.share);
        if (promo) return { key: item.key, usage: itemUsage, fast_share: fastShareOf(points), estimate: {} };
        const unknown = item.key === "codex-auto-review" && !pricingOverrides[item.key];
        return { key: item.key, usage: itemUsage, estimate: { ...estimate, usd: unknown ? "0.000000000" : (totalCost * item.share).toFixed(9), priced_tokens: unknown ? 0 : itemUsage.total, unpriced_tokens: unknown ? itemUsage.total : 0, coverage_ratio: unknown ? 0 : 1, reasons: unknown ? estimate.reasons : [] } };
      })
    };
  }

  function breakdown(url) {
    const dimension = url.searchParams.get("dimension") || "model";
    const source = { model: models, source: sources, agent_type: agents, project: projects, thread: threads }[dimension] || models;
    const requested = url.searchParams.get(dimension);
    const entries = requested ? source.filter((item) => item.key === requested) : source;
    if (promo) {
      const { usage, fast_share } = summary(url);
      const eventScale = usage.total / 2_380_000_000;
      return { dimension, items: entries.map((item) => ({ key: item.key, usage: scaledUsage(usage, item.share), fast_share, events: Math.max(1, Math.round(item.events * eventScale)), sessions: Math.max(1, Math.round(item.sessions * Math.min(1, eventScale * 1.6))) })) };
    }
    return { dimension, items: entries.map((item) => ({ key: item.key, usage: scaledUsage(baseUsage, item.share * filterScale(url)), events: item.events, sessions: item.sessions })) };
  }

  function sessionPayload(url) {
    return { items: sessions.filter((item) => {
      for (const key of ["model", "source", "agent_type", "project", "confidence"]) {
        const expected = url.searchParams.get(key);
        const actual = key === "project" ? item.project_path : item[key];
        if (expected && actual !== expected) return false;
      }
      const sessionID = url.searchParams.get("session_id");
      if (sessionID && item.session_id !== sessionID) return false;
      const query = (url.searchParams.get("q") || "").trim().toLocaleLowerCase();
      if (query && ![item.title, item.session_id, item.project_path, item.model, item.source]
        .some((value) => String(value || "").toLocaleLowerCase().includes(query))) return false;
      return true;
    }).map(({ share, hoursAgo, tokens, parent, ...item }) => {
      if (promo) {
        return { ...item, usage: promoComposition(tokens), fast_share: .12 + (tokens / 1e6 % 7) / 25, estimate: {}, parent_id: parent, last_usage: new Date(now.getTime() - hoursAgo * 3_600_000).toISOString() };
      }
      const usage = scaledUsage(baseUsage, share);
      const usd = (usage.total / 1_000_000 * 3.18).toFixed(9);
      return {
        ...item,
        usage,
        estimate: {
          usd, regular_input_usd: usd, cached_input_usd: "0.000000000", cache_write_input_usd: "0.000000000", output_usd: "0.000000000",
          priced_tokens: usage.total, unpriced_tokens: 0, coverage_ratio: 1, reasons: []
        },
        last_usage: new Date(now.getTime() - hoursAgo * 3_600_000).toISOString()
      };
    }) };
  }

  function pricingPayload() {
    return {
      basis: "current_standard_api_text_token_prices",
      currency: "USD",
      catalog_as_of: "2026-09-23",
      catalog,
      overrides: pricingOverrides,
      unpriced_models: !diagnostics || pricingOverrides["codex-auto-review"] ? [] : [{ key: "codex-auto-review", usage: scaledUsage(baseUsage, .12), events: 9, sessions: 3 }]
    };
  }

  const promoUpdate = promo && params.get("update") === "available";
  const demoUpdates = promo ? { current_version: "2.7.1", latest_version: promoUpdate ? "2.8.0" : "2.7.1", release_url: "https://github.com/zJay26/codex-usage/releases", auto_check: true, available: promoUpdate, can_install: true, phase: "idle", download_dir: "~/Downloads/codex-usage", default_download_dir: "~/Downloads/codex-usage", custom_download_dir: "", can_open_download_dir: false } : { current_version: "2.7.1-demo", latest_version: "2.7.1", release_url: "https://github.com/zJay26/codex-usage/releases", auto_check: true, available: false, can_install: false, phase: "idle", download_dir: "C:\\Users\\Demo\\Downloads\\codex-usage", default_download_dir: "C:\\Users\\Demo\\Downloads\\codex-usage", custom_download_dir: "", can_open_download_dir: false };
  async function syntheticFetch(input, init = {}) {
    const raw = typeof input === "string" ? input : input.url;
    const url = new URL(raw, root.location.href);
    if (!url.pathname.includes("/api/v1/")) return root.fetch.__codexUsageOriginal(input, init);
    const endpoint = url.pathname.slice(url.pathname.indexOf("/api/v1/"));
    const method = String(init.method || (typeof input !== "string" && input.method) || "GET").toUpperCase();
    if (endpoint.startsWith("/api/v1/updates")) {
      if (endpoint.endsWith("/preferences")) {
        const body = JSON.parse(init.body || "{}");
        if (Object.hasOwn(body,"auto_check")) demoUpdates.auto_check = Boolean(body.auto_check);
        if (Object.hasOwn(body,"download_dir")) { demoUpdates.custom_download_dir = body.download_dir.trim(); demoUpdates.download_dir = demoUpdates.custom_download_dir || demoUpdates.default_download_dir; }
      }
      if (endpoint.endsWith("/install")) return jsonResponse({ error: "Updates are unavailable in the synthetic demo" }, 409);
      return jsonResponse(demoUpdates);
    }
    if (endpoint === "/api/v1/status") return jsonResponse({
      version: "2.7.1-demo", scanning: false,
      status: {
        machine: promo ? { id: "workstation", label: "Workstation · Windows 11", hostname: "workstation", os: "windows", arch: "amd64" } : { id: "synthetic-machine", label: "Synthetic Windows · demo", hostname: "synthetic-host", os: "windows", arch: "amd64" },
        last_scan: now.toISOString(), accounting_mode: "jsonl_only", otel_active: false,
        event_count: promo ? 18_462 : 114, session_count: promo ? 346 : 26, warning_count: diagnostics ? 2 : 0, data_revision: "demo-1",
        codex_homes: [{ path: promo ? "~/.codex" : "synthetic://codex-home", last_scan: now.toISOString(), files_scanned: promo ? 346 : 26 }]
      }
    });
    if (endpoint === "/api/v1/summary") return jsonResponse(withModes(summary(url),url));
    if (endpoint === "/api/v1/cost-estimate") return jsonResponse(withModes(costEstimate(url),url));
    if (endpoint === "/api/v1/timeseries") {
      const bucket = url.searchParams.get("bucket") === "hour" ? "hour" : "day";
      const points = bucket === "hour" ? hourlyPoints(url) : dailyPoints(url).map((point) => ({ time: point.time, date: point.date, usage: point.usage }));
      const window = bucket === "hour" && url.searchParams.get("date") ? (()=>{const start=rangeDate(url.searchParams.get("date"));const end=new Date(start.getTime()+points.length*3600000);return {date:dateKey(start),start:start.toISOString(),end:end.toISOString(),complete_hours:points.length};})() : null;
      return jsonResponse(withModes({ bucket, points, window },url));
    }
    if (endpoint === "/api/v1/breakdown") return jsonResponse(withModes(breakdown(url),url));
    if (endpoint === "/api/v1/dimensions") return jsonResponse({
      models: models.map((item) => item.key),
      sources: sources.map((item) => item.key),
      projects: projects.map((item) => item.key)
    });
    if (endpoint === "/api/v1/session-tree" && promo) {
      const payload = withModes(sessionPayload(url), url);
      const children = (id) => payload.items.filter((item) => item.parent_id === id);
      const items = payload.items.filter((item) => !item.parent_id).flatMap((root) => {
        const kids = children(root.session_id);
        const subtree_usage = kids.reduce((sum, kid) => addUsage(sum, kid.usage), root.usage);
        return [{ ...root, depth: 0, children: kids.length, subtree_usage }, ...kids.map((kid) => ({ ...kid, depth: 1, children: 0, subtree_usage: { ...kid.usage } }))];
      });
      return jsonResponse({ items, root_count: items.filter((item) => !item.depth).length, offset: 0, limit: 20 });
    }
    if (endpoint === "/api/v1/session-tree") {
      const payload=withModes(sessionPayload(url),url);
      const items=payload.items.map((item,index)=>({...item,depth:index%3===0?0:1,children:index%3===0?Math.min(2,payload.items.length-index-1):0,parent_id:index%3===0?"":payload.items[index-index%3].session_id,subtree_usage:{...item.usage}}));
      items.forEach((item,index)=>{if(index%3!==0){const parent=items[index-index%3];parent.subtree_usage=addUsage(parent.subtree_usage,item.usage);}});
      return jsonResponse({items,root_count:Math.ceil(items.length/3),offset:0,limit:20});
    }
    if (endpoint === "/api/v1/sessions") {
      const payload = withModes(sessionPayload(url),url);
      if (["0", "false"].includes((url.searchParams.get("include_estimate") || "").toLowerCase())) {
        payload.items = payload.items.map(({ estimate, ...item }) => item);
      }
      return jsonResponse(payload);
    }
    if (endpoint === "/api/v1/session-estimates") {
      const payload = withModes(sessionPayload(url),url);
      return jsonResponse({ items: payload.items.map((item) => ({ session_id: item.session_id, estimate: item.estimate })) });
    }
    if (endpoint === "/api/v1/warnings") return jsonResponse({ items: diagnostics ? [
      { created_at: now.toISOString(), first_seen: new Date(now.getTime() - 86_400_000).toISOString(), occurrences: 1, kind: "fork_replay_detected", path: "synthetic://rollout", detail: "Synthetic copied parent history was skipped and the JSONL index was rebuilt." },
      { created_at: now.toISOString(), occurrences: 1, kind: "cumulative_gap_fallback", path: "synthetic://rollout", detail: "Synthetic cumulative boundary could not be fully verified; last_token_usage conservatively filled the delta." }
    ] : [] });
    if (endpoint === "/api/v1/pricing" && method === "GET") return jsonResponse(pricingPayload());
    if (endpoint === "/api/v1/pricing/overrides" && method === "PUT") {
      try {
        pricingOverrides = JSON.parse(init.body || "{}").overrides || {};
        return jsonResponse(pricingPayload());
      } catch {
        return jsonResponse({ error: "Invalid synthetic request body" }, 400);
      }
    }
    if (endpoint === "/api/v1/rescan" && method === "POST") return jsonResponse({ homes: 1, files: 26, records: 940, events_inserted: 2, duplicates: 14, warnings: 0 });
    if (endpoint === "/api/v1/export") return jsonResponse(demoExportRows(url));
    return jsonResponse({ error: `No synthetic adapter for ${method} ${endpoint}` }, 404);
  }

  const originalFetch = root.fetch.bind(root);
  syntheticFetch.__codexUsageOriginal = originalFetch;
  root.fetch = syntheticFetch;

  function demoExportRows(url) {
    return withModes(sessionPayload(url),url).items.flatMap((item) => {
      const knownRegular = Object.fromEntries(usageKeys.map(key=>[key,item.modes.regular[key]-item.modes.unknown[key]]));
      return [["standard",knownRegular],["fast",item.modes.fast],["unknown",item.modes.unknown]].filter(([,usage])=>usage.total>0).map(([mode,usage])=>({
        session_id:item.session_id,turn_id:item.session_id+"-"+mode,project_path:item.project_path,model:item.model,source:item.source,
        total:usage.total,usage,service_mode:mode,service_tier:mode==="fast"?"priority":mode==="standard"?"default":"",mode_source:"synthetic",mode_assumed:mode==="unknown"
      }));
    });
  }
  function exportData(format, requestPath) {
    const rows = demoExportRows(new URL(requestPath || root.location.href,root.location.href));
    if (format === "csv") {
      const columns = ["session_id", "turn_id", "project_path", "model", "source", "total", "service_mode", "service_tier", "mode_source", "mode_assumed"];
      const csv = [columns.join(","), ...rows.map((row) => columns.map((key) => JSON.stringify(row[key])).join(","))].join("\n");
      return `data:text/csv;charset=utf-8,${encodeURIComponent(csv)}`;
    }
    return `data:application/json;charset=utf-8,${encodeURIComponent(JSON.stringify(rows, null, 2))}`;
  }

  root.CodexUsageDemo = {
    synthetic: true,
    createExportURL: (format, requestPath) => exportData(format, requestPath),
    reset: () => { pricingOverrides = {}; }
  };
})(globalThis);
