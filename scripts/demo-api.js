(function (root) {
  "use strict";

  root.CODEX_USAGE_DEMO = true;
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
    { model: "gpt-5.6-sol", display_name: "GPT-5.6 Sol", input_usd_per_million: "5.00", cached_input_usd_per_million: "0.50", cache_write_input_usd_per_million: "6.25", output_usd_per_million: "30.00", source: "#synthetic-pricing" },
    { model: "gpt-5.6-terra", display_name: "GPT-5.6 Terra", input_usd_per_million: "2.00", cached_input_usd_per_million: "0.20", cache_write_input_usd_per_million: "2.50", output_usd_per_million: "12.00", source: "#synthetic-pricing" },
    { model: "gpt-5.4", display_name: "GPT-5.4", input_usd_per_million: "2.50", cached_input_usd_per_million: "0.25", output_usd_per_million: "15.00", source: "#synthetic-pricing" }
  ];
  const models = [
    { key: "gpt-5.4", share: .57, events: 71, sessions: 15 },
    { key: "gpt-5.6-terra", share: .31, events: 32, sessions: 8 },
    { key: "codex-auto-review", share: .12, events: 11, sessions: 3 }
  ];
  const agents = [
    { key: "main", share: .75, events: 80, sessions: 20 },
    { key: "subagent", share: .17, events: 21, sessions: 6 },
    { key: "guardian", share: .05, events: 8, sessions: 4 },
    { key: "memory", share: .03, events: 5, sessions: 2 }
  ];
  const sources = [
    { key: "codex_desktop", share: .68, events: 70, sessions: 16 },
    { key: "codex_cli_rs", share: .32, events: 44, sessions: 10 }
  ];
  const projects = [
    { key: "synthetic://visual-lab", share: .62, events: 70, sessions: 12 },
    { key: "synthetic://service-api", share: .38, events: 44, sessions: 8 }
  ];
  const threads = [
    { key: "Dashboard localization", share: .61, events: 12, sessions: 1 },
    { key: "JSONL fork replay audit", share: .39, events: 9, sessions: 1 }
  ];
  const sessions = [
    { session_id: "demo-session-a", title: "Dashboard localization", project_path: "synthetic://visual-lab", model: "gpt-5.4", source: "codex_desktop", agent_type: "main", share: .48, confidence: "exact", hoursAgo: 0 },
    { session_id: "demo-session-b", title: "JSONL fork replay audit", project_path: "synthetic://service-api", model: "gpt-5.6-terra", source: "codex_cli_rs", agent_type: "subagent", share: .31, confidence: "gap_fallback", hoursAgo: 2 },
    { session_id: "demo-session-c", title: "Local data-quality review", project_path: "synthetic://visual-lab", model: "gpt-5.4", source: "codex_desktop", agent_type: "guardian", share: .21, confidence: "exact", hoursAgo: 7 }
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
  function modeUsage(usage, mode) {
    const fast = scaledUsage(usage, .25);
    fast.total = fast.input + fast.output;
    const regular = Object.fromEntries(usageKeys.map((key) => [key, usage[key] - fast[key]]));
    const unknown = scaledUsage(regular, .2); unknown.total = unknown.input + unknown.output;
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
        const factor = {"gpt-6-astra":2.5,"gpt-5.6-sol":2.5,"gpt-5.6-terra":2.5,"gpt-5.6-luna":2.5,"gpt-5.5":2.5,"gpt-5.4":2}[alias];
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
      payload.modes = modeUsage(payload.usage, url.searchParams.get("mode"));
      payload.usage = addUsage(payload.modes.regular, payload.modes.fast);
      if (payload.grand_total != null) payload.grand_total = payload.usage.total;
      if (payload.estimate) payload.estimate = demoEstimate(payload.modes, payload.model || (models.some((m) => m.key === payload.key) ? payload.key : url.searchParams.get("model")), url.searchParams.get("cost_basis") === "codex_fast_weighted");
    }
    if (payload.summary && payload.points) {
      payload.modes = {regular:zeroUsage(),fast:zeroUsage(),unknown:zeroUsage()};
      for (const point of payload.points) for (const mode of ["regular","fast","unknown"]) payload.modes[mode]=addUsage(payload.modes[mode],point.modes[mode]);
      payload.summary = demoEstimate(payload.modes, url.searchParams.get("model"), url.searchParams.get("cost_basis") === "codex_fast_weighted");
      payload.basis = url.searchParams.get("cost_basis") || "current_standard_api_text_token_prices";
      payload.fast_rules_as_of = "2026-09-07";
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
    const { start, end } = requestedBounds(
      url,
      new Date(now.getFullYear(), now.getMonth(), now.getDate() - 29),
      new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1)
    );
    const points = [];
    let index = 0;
    for (let date = new Date(start); date < end && index < 120; date.setDate(date.getDate() + 1), index++) {
      const seed = Math.floor(date.getTime() / 86_400_000);
      const total = seed % 11 === 3 ? 0 : Math.round((180_000 + (Math.sin(seed * .73) + 1.25) * 118_000 + (seed % 31) * 4_100) * filterScale(url));
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

  function costEstimate(url) {
    const points = dailyPoints(url);
    const usage = points.reduce((sum, point) => {
      for (const key of Object.keys(point.usage)) sum[key] = (sum[key] || 0) + point.usage[key];
      return sum;
    }, {});
    const pricedTokens = points.reduce((sum, point) => sum + point.estimate.priced_tokens, 0);
    const unpricedTokens = points.reduce((sum, point) => sum + point.estimate.unpriced_tokens, 0);
    const totalCost = points.reduce((sum, point) => sum + Number(point.estimate.usd), 0);
    const estimate = {
      usd: totalCost.toFixed(9), regular_input_usd: totalCost.toFixed(9), cached_input_usd: "0.000000000", cache_write_input_usd: "0.000000000", output_usd: "0.000000000",
      priced_tokens: pricedTokens, unpriced_tokens: unpricedTokens,
      coverage_ratio: pricedTokens + unpricedTokens ? pricedTokens / (pricedTokens + unpricedTokens) : 0,
      reasons: unpricedTokens ? [{ kind: "unknown_model", model: "codex-auto-review", tokens: unpricedTokens, detail: "Synthetic model has no public API rate or local override." }] : []
    };
    return {
      basis: "current_standard_api_text_token_prices", currency: "USD", catalog_as_of: "2026-09-05", bucket: "day", summary: estimate, points,
      models: models.map((item) => {
        const itemUsage = scaledUsage(usage, item.share);
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
    }).map(({ share, hoursAgo, ...item }) => {
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
      catalog_as_of: "2026-09-05",
      catalog,
      overrides: pricingOverrides,
      unpriced_models: pricingOverrides["codex-auto-review"] ? [] : [{ key: "codex-auto-review", usage: scaledUsage(baseUsage, .12), events: 9, sessions: 3 }]
    };
  }

  const demoUpdates = { current_version: "2.6.0-demo", latest_version: "2.6.0", release_url: "https://github.com/zJay26/codex-usage/releases", auto_check: true, available: false, can_install: false, phase: "idle" };
  async function syntheticFetch(input, init = {}) {
    const raw = typeof input === "string" ? input : input.url;
    const url = new URL(raw, root.location.href);
    if (!url.pathname.includes("/api/v1/")) return root.fetch.__codexUsageOriginal(input, init);
    const endpoint = url.pathname.slice(url.pathname.indexOf("/api/v1/"));
    const method = String(init.method || (typeof input !== "string" && input.method) || "GET").toUpperCase();
    if (endpoint.startsWith("/api/v1/updates")) {
      if (endpoint.endsWith("/preferences")) demoUpdates.auto_check = Boolean(JSON.parse(init.body || "{}").auto_check);
      if (endpoint.endsWith("/install")) return jsonResponse({ error: "Updates are unavailable in the synthetic demo" }, 409);
      return jsonResponse(demoUpdates);
    }
    if (endpoint === "/api/v1/status") return jsonResponse({
      version: "2.6.0-demo", scanning: false,
      status: {
        machine: { id: "synthetic-machine", label: "Synthetic Windows · demo", hostname: "synthetic-host", os: "windows", arch: "amd64" },
        last_scan: now.toISOString(), accounting_mode: "jsonl_only", otel_active: false,
        event_count: 114, session_count: 26, warning_count: 2, data_revision: "demo-1",
        codex_homes: [{ path: "synthetic://codex-home", last_scan: now.toISOString(), files_scanned: 26 }]
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
    if (endpoint === "/api/v1/warnings") return jsonResponse({ items: [
      { created_at: now.toISOString(), first_seen: new Date(now.getTime() - 86_400_000).toISOString(), occurrences: 1, kind: "fork_replay_detected", path: "synthetic://rollout", detail: "Synthetic copied parent history was skipped and the JSONL index was rebuilt." },
      { created_at: now.toISOString(), occurrences: 1, kind: "cumulative_gap_fallback", path: "synthetic://rollout", detail: "Synthetic cumulative boundary could not be fully verified; last_token_usage conservatively filled the delta." }
    ] });
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
