// Deterministic promo composition: every frame is a pure function of time, so the
// renderer can seek frame by frame and capture identical output on every run.
(() => {
  const P = window.PROMO;
  const zh = P.locale.startsWith("zh");
  const vertical = P.format === "vertical";
  const W = vertical ? 1080 : 1920;
  const H = vertical ? 1920 : 1080;
  const scene = Object.fromEntries(P.timeline.scenes.map(s => [s.id, s]));
  const shots = P.assets.shots;
  const dims = { w: shots.overview.width, h: shots.overview.height };

  const copy = zh ? {
    hook: { title: ["Codex 额度猛掉，", "Token 到底花在哪了？"], unit: "TOKENS", credits: "额度" },
    brand: { tagline: "让 Codex 用量一目了然" },
    blocks: {
      total: ["总览", ["用了多少、花了多少，全都清楚"], "费用按公开 API 单价估算，并非真实账单", ["用了多少、花了多少，", "全都清楚"]],
      daily: ["每日用量", ["每天用了多少 Token 一眼看清"], null, ["每天用了多少 Token", "一眼看清"]],
      hourly: ["小时", ["按小时统计，找到用量高峰"], null, ["按小时统计，", "找到用量高峰"]],
      range: ["时间区间", ["查询任意时间区间，", "看看上次重置到现在用了多少 Token"], null, ["查询任意时间区间，", "看看上次重置到现在", "用了多少 Token"]],
      calendar: ["月历", ["每天的常规与 Fast，分开算清"], "Fast 费用按额度倍率折算", ["每天的常规与 Fast，", "分开算清"]],
      breakdown: ["明细", ["哪个模型、哪个项目最费 Token？"], null, ["哪个模型、哪个项目", "最费 Token？"]],
      sessions: ["Session", ["每次会话都有记录，一搜就找到"], null, ["每次会话都有记录，", "一搜就找到"]],
      tree: ["任务树", ["主任务和 Subagent，用量分开算"], "子任务的合计也一并列出", ["主任务和 Subagent，", "用量分开算"]],
      export: ["导出", ["一键导出 JSON / CSV"]],
      live: ["自动统计", ["用量自动统计，随时最新"]],
      update: ["软件更新", ["新版本提醒，一键升级"]],
      interface: ["界面", ["明暗、中英、手机，都顺手"]]
    },
    labels: { total: "总 Token", cost: "API 等价成本", peakDay: "用量最高的一天", peakHour: "高峰时段", lastReset: "上次重置", now: "现在", sinceReset: "上次重置至今", split: "常规 / Fast", byModel: "按模型", byProject: "按项目", search: "搜索", matches: "匹配的会话", mainTask: "主任务", subtotal: "含子任务合计", formats: "JSON / CSV", live: "自动更新", newVersion: "发现新版本" },
    privacy: { title: ["本地运行，", "不上传用量和对话"], chips: ["开源 · MIT", "Windows · Linux · macOS", "单文件"] },
    cta: { title: "免费开源，一条命令安装", demo: "在线试用" },
    badge: "演示画面使用合成数据",
    play: "观看宣传片"
  } : {
    hook: { title: ["Codex credits draining fast?", "Where are all those tokens going?"], unit: "TOKENS", credits: "CREDITS" },
    brand: { tagline: "Your Codex usage, crystal clear." },
    blocks: {
      total: ["Overview", ["See your usage and its API cost in one view."], "Estimated at public API rates, not your actual bill."],
      daily: ["Daily", ["Daily token usage, at a glance."]],
      hourly: ["Hourly", ["Track usage by the hour and find your peaks."]],
      range: ["Custom range", ["Query any time range —", "like everything since your last reset."]],
      calendar: ["Calendar", ["Standard vs. Fast, split for every day."], "Fast costs include the credit multiplier."],
      breakdown: ["Breakdown", ["Which models and projects burn the most?"]],
      sessions: ["Sessions", ["Every session on record. Find any one in seconds."]],
      tree: ["Task tree", ["Main tasks and subagents, tallied separately."], "Subtree totals are shown too."],
      export: ["Export", ["Export to JSON or CSV in one click."]],
      live: ["Live", ["Usage stays up to date, automatically."]],
      update: ["Updates", ["New version? Update in one click."]],
      interface: ["Interface", ["Light or dark, English or Chinese, desktop or phone."]]
    },
    labels: { total: "Total tokens", cost: "API-equivalent cost", peakDay: "Busiest day", peakHour: "Peak hour", lastReset: "Last reset", now: "Now", sinceReset: "Since last reset", split: "Standard / Fast", byModel: "By model", byProject: "By project", search: "Search", matches: "Matching sessions", mainTask: "Main task", subtotal: "Subtree total", formats: "JSON / CSV", live: "Live", newVersion: "Update available" },
    privacy: { title: ["Runs locally.", "Your usage and chats", "never leave your machine."], chips: ["Open source · MIT", "Windows · Linux · macOS", "Single binary"] },
    cta: { title: "Free and open source. Install with one command.", demo: "Live demo" },
    badge: "Synthetic demo data",
    play: "Watch the promo"
  };

  // ---- math helpers -------------------------------------------------------
  const clamp = (x, a = 0, b = 1) => Math.min(b, Math.max(a, x));
  const prog = (t, a, b) => b <= a ? (t >= a ? 1 : 0) : clamp((t - a) / (b - a));
  const lerp = (a, b, p) => a + (b - a) * p;
  const ease = {
    out: p => 1 - (1 - p) ** 3,
    inOut: p => p < .5 ? 4 * p ** 3 : 1 - (-2 * p + 2) ** 3 / 2,
    back: p => { const c = 1.45; return 1 + (c + 1) * (p - 1) ** 3 + c * (p - 1) ** 2; },
    in: p => p ** 2.2
  };
  const union = (...rects) => {
    const x = Math.min(...rects.map(r => r.x)), y = Math.min(...rects.map(r => r.y));
    return { x, y, w: Math.max(...rects.map(r => r.x + r.w)) - x, h: Math.max(...rects.map(r => r.y + r.h)) - y };
  };
  const lerpRect = (a, b, p) => ({ x: lerp(a.x, b.x, p), y: lerp(a.y, b.y, p), w: lerp(a.w, b.w, p), h: lerp(a.h, b.h, p) });
  const pad = (r, n) => ({ x: r.x - n, y: r.y - n, w: r.w + 2 * n, h: r.h + 2 * n });
  const env = (t, a, b, inDur = .5, outDur = .3) => {
    const i = ease.out(prog(t, a, a + inDur));
    const o = ease.inOut(prog(t, b, b + outDur));
    return { i, o, v: i * (1 - o) };
  };
  const $ = (sel, root = document) => root.querySelector(sel);
  const el = (tag, cls, text) => { const n = document.createElement(tag); if (cls) n.className = cls; if (text != null) n.textContent = text; return n; };
  const set = (node, style) => { for (const [k, v] of Object.entries(style)) node.style[k] = v; };

  // ---- layout -----------------------------------------------------------------
  const L = vertical ? {
    copy: { x: 72, y: 168, w: 936 },
    panel: { x: 60, y: 580, w: 960, h: 1206 },
    phone: { x: 640, y: 1060, h: 700 },
    corner: { x: 72, y: 70 },
    badge: { x: 72, y: 1852 }
  } : {
    copy: { x: 160, y: 58, w: 1280 },
    panel: { x: 160, y: 272, w: 1600, h: 740 },
    phone: { x: 1480, y: 330, h: 690 },
    corner: { x: 1510, y: 60 },
    badge: { x: 1400, y: 118 }
  };
  const chromeH = 46;
  const view = { w: L.panel.w, h: L.panel.h - chromeH };
  const coverScale = Math.max(view.w / dims.w, view.h / dims.h);

  // ---- build DOM --------------------------------------------------------------
  const stage = $("#stage");
  document.documentElement.classList.add(vertical ? "vertical" : "landscape");
  document.documentElement.lang = zh ? "zh-CN" : "en";
  set(stage, { width: `${W}px`, height: `${H}px` });
  const lookup = key => key.split(".").reduce((o, k) => o[k], copy);
  for (const node of document.querySelectorAll("[data-copy]")) node.textContent = lookup(node.dataset.copy);
  for (const node of document.querySelectorAll("[data-lines]")) for (const line of lookup(node.dataset.lines)) node.append(el("span", "line", line));
  for (const slot of document.querySelectorAll(".icon-slot")) slot.innerHTML = P.icon;

  // Brand reveal: a stroke mask draws the copper Z along its centre line.
  const brandSvg = $("#brand svg");
  const mask = document.createElementNS("http://www.w3.org/2000/svg", "mask");
  mask.id = "zDraw";
  mask.setAttribute("maskUnits", "userSpaceOnUse");
  mask.innerHTML = '<rect x="0" y="0" width="256" height="256" fill="black"/><path d="M50 44 H208 L44 167 Q33 175 33 186 V207 Q33 210 37 210 H230" fill="none" stroke="white" stroke-width="26" stroke-linecap="round" stroke-linejoin="round" pathLength="1" stroke-dasharray="1 1"/>';
  brandSvg.querySelector("defs").append(mask);
  [...brandSvg.querySelectorAll(":scope > path")].at(-1).setAttribute("mask", "url(#zDraw)");
  const zStroke = mask.querySelector("path");
  const brandBars = [...brandSvg.querySelectorAll("g path")];
  for (const bar of brandBars) set(bar, { transformBox: "fill-box", transformOrigin: "50% 100%" });

  const hookBars = $(".hook-bars");
  const barCount = vertical ? 28 : 56;
  for (let i = 0; i < barCount; i++) hookBars.append(el("i"));

  const corner = $("#corner"), badge = $("#badge");
  set(corner, { left: `${L.corner.x}px`, top: `${L.corner.y}px` });
  set(badge, { left: `${L.badge.x}px`, top: `${L.badge.y}px`, width: vertical ? "auto" : "360px" });

  const panel = $("#panel");
  set(panel, { left: `${L.panel.x}px`, top: `${L.panel.y}px`, width: `${view.w}px`, height: `${L.panel.h}px` });
  set($(".viewport"), { width: `${view.w}px`, height: `${view.h}px` });
  const content = $("#content");
  set(content, { width: `${dims.w}px`, height: `${dims.h}px` });
  const layers = {};
  const addLayer = (name, src) => {
    const img = el("img");
    img.src = src;
    img.alt = "";
    set(img, { width: `${dims.w}px`, height: `${dims.h}px`, visibility: "hidden" });
    content.append(img);
    layers[name] = img;
  };
  for (const [name, shot] of Object.entries(shots)) if (name !== "mobile") addLayer(name, P.assets.base + shot.file);
  addLayer("otherDark", P.other.base + P.other.shots.dark.file);
  const reveal = $("#reveal");
  set(reveal, { width: `${dims.w}px`, height: `${dims.h}px`, zIndex: 4 });

  // ---- beat sheet ---------------------------------------------------------------
  // Each feature is a short sequence: the camera settles on one area, the rest
  // dims, a label names what matters, and a cursor performs the action.
  const R = (still, key) => shots[still].focus[key];
  const K = copy.labels;
  const beats = { copy: [], layers: [], cams: [], spots: [], labels: [], cursor: [], counters: [], reveals: [] };
  const S = id => scene[id].start;
  const block = (id, number, a, b) => beats.copy.push({ a, b, number, data: copy.blocks[id] });
  const layer = (t, name, fade = .35, kind = "fade") => beats.layers.push({ t, name, fade, kind });
  const cam = (t, dur, rect) => beats.cams.push({ t, dur, rect });
  const spot = (t, dur, rect) => beats.spots.push({ t, dur, rect });
  const label = (a, b, rect, text, extra = {}) => beats.labels.push({ a, b, rect, text, ...extra });
  const click = (t, rect) => beats.cursor.push({ t, rect });

  { // 01 totals and cost, counted up in place
    const t = S("total");
    block("total", "01", t + .15, scene.total.end - .3);
    layer(t, "overviewBlank", 0);
    const hero = R("overview", "hero");
    cam(t, 0, { x: hero.x - 48, y: hero.y - 130, w: hero.w + 96, h: hero.h + 178 });
    beats.counters.push({ a: t + .7, b: t + 2.1, off: t + 2.2, key: "total" }, { a: t + .8, b: t + 2.2, off: t + 2.2, key: "cost" });
    layer(t + 2.2, "overview", 0);
    spot(t + 2.3, .4, pad(R("overview", "totalCard"), 6));
    label(t + 2.4, scene.total.end - .2, R("overview", "total"), K.total);
    spot(t + 3.05, .45, pad(R("overview", "costCard"), 6));
    label(t + 3.15, scene.total.end - .2, R("overview", "cost"), K.cost);
  }
  { // 02 daily bars for the last 30 days
    const t = S("daily");
    block("daily", "02", t + .1, scene.daily.end - .3);
    spot(t, .3, null);
    layer(t, "dailyBlank", .4);
    cam(t, .8, pad(R("daily", "panel"), 14));
    beats.reveals.push({ a: t + .55, b: t + 1.6, rect: pad(R("daily", "belt"), 4), name: "daily" });
    layer(t + 1.6, "daily", 0);
    click(t + 2.1, R("daily", "peak"));
    layer(t + 2.15, "dailyPeak", .2);
    spot(t + 2.2, .35, pad(R("dailyPeak", "peak"), 6));
    label(t + 2.3, scene.daily.end - .2, R("dailyPeak", "peak"), K.peakDay);
  }
  { // 03 hourly line, click the busiest hour
    const t = S("hourly");
    block("hourly", "03", t + .1, scene.hourly.end - .3);
    spot(t, .3, null);
    layer(t, "hourly", .4);
    cam(t, .8, pad(R("hourly", "panel"), 14));
    click(t + 1.35, R("hourly", "peak"));
    layer(t + 1.4, "hourlyPeak", .25);
    spot(t + 1.6, .45, pad(R("hourlyPeak", "ledger"), 8));
    label(t + 1.75, scene.hourly.end - .2, R("hourlyPeak", "ledger"), K.peakHour);
  }
  { // 04 minute-precision range: since the last reset, until now
    const t = S("range");
    block("range", "04", t + .1, scene.range.end - .3);
    spot(t, .3, null);
    layer(t, "range0", .4);
    cam(t, .8, pad(union(R("range0", "form"), R("range3", "hero")), 20));
    click(t + 1.0, R("range0", "start"));
    layer(t + 1.05, "range1", .15);
    spot(t + 1.0, .3, pad(R("range1", "start"), 6));
    label(t + 1.1, scene.range.end - .2, R("range1", "start"), K.lastReset);
    click(t + 1.8, R("range1", "now"));
    layer(t + 1.85, "range2", .15);
    spot(t + 1.8, .3, pad(union(R("range2", "end"), R("range2", "now")), 6));
    label(t + 1.9, scene.range.end - .2, R("range2", "end"), K.now);
    click(t + 2.45, R("range2", "query"));
    layer(t + 2.5, "range3", .25);
    spot(t + 2.75, .45, pad(R("range3", "totalCard"), 6));
    label(t + 2.9, scene.range.end - .2, R("range3", "total"), K.sinceReset, { place: "bottom" });
  }
  { // 05 calendar and the Standard / Fast split of one day
    const t = S("calendar");
    block("calendar", "05", t + .1, scene.calendar.end - .3);
    spot(t, .3, null);
    layer(t, "calendar", .4);
    cam(t, .8, pad(union(R("calendar", "grid"), R("calendar", "dayCard")), 14));
    click(t + 1.3, R("calendar", "cell"));
    layer(t + 1.35, "calendarPeak", .25);
    spot(t + 1.6, .45, pad(union(R("calendarPeak", "dayTotal"), R("calendarPeak", "dayModes"), R("calendarPeak", "composition")), 10));
    label(t + 1.75, scene.calendar.end - .2, union(R("calendarPeak", "dayTotal"), R("calendarPeak", "dayModes")), K.split, { place: "left" });
  }
  { // 06 breakdown by model, then by project
    const t = S("breakdown");
    block("breakdown", "06", t + .1, scene.breakdown.end - .3);
    spot(t, .3, null);
    layer(t, "models", .4);
    cam(t, .8, pad(R("models", "panel"), 16));
    spot(t + .9, .4, pad(R("models", "list"), 6));
    label(t + 1.0, t + 1.9, R("models", "list"), K.byModel);
    click(t + 2.05, R("models", "projectTab"));
    layer(t + 2.1, "projects", .25);
    spot(t + 2.2, .4, pad(R("projects", "list"), 6));
    label(t + 2.35, scene.breakdown.end - .2, R("projects", "list"), K.byProject);
  }
  { // 07 session list and search
    const t = S("sessions");
    block("sessions", "07", t + .1, scene.sessions.end - .3);
    spot(t, .3, null);
    layer(t, "sessions", .4);
    cam(t, .8, pad(R("sessions", "panel"), 12));
    click(t + 1.0, R("sessions", "search"));
    spot(t + 1.0, .3, pad(R("sessions", "search"), 6));
    label(t + 1.1, t + 2.2, R("sessions", "search"), K.search);
    layer(t + 1.45, "search1", .12);
    layer(t + 1.9, "search2", .12);
    cam(t + 2.1, .7, pad(union(R("search2", "search"), R("search2", "rows")), 24));
    spot(t + 2.25, .45, pad(R("search2", "rows"), 6));
    label(t + 2.4, scene.sessions.end - .2, R("search2", "rows"), K.matches);
  }
  { // 08 task tree: expand a main task to reveal its subagents
    const t = S("tree");
    block("tree", "08", t + .1, scene.tree.end - .3);
    spot(t, .3, null);
    layer(t, "tree0", .4);
    cam(t, .8, pad(union(R("tree0", "row"), R("tree1", "group")), 90));
    click(t + 1.15, R("tree0", "toggle"));
    layer(t + 1.2, "tree1", .25);
    spot(t + 1.45, .45, pad(R("tree1", "group"), 4));
    label(t + 1.6, scene.tree.end - .2, R("tree1", "row"), K.mainTask);
    label(t + 1.85, scene.tree.end - .2, R("tree1", "subtotal"), K.subtotal, { place: "left" });
  }
  { // 09 export, live usage, and software updates: three equal segments
    const t = S("extras");
    const d = (scene.extras.end - t) / 3;
    const pair = union(R("export", "json"), R("export", "csv"));
    block("export", "09", t + .1, t + d - .4);
    spot(t, .3, null);
    layer(t, "export", .4);
    cam(t, .7, pad(R("export", "dialog"), 40));
    spot(t + .7, .35, pad(pair, 8));
    label(t + .8, t + d - .2, pair, K.formats);

    const u = t + d;
    block("live", "09", u + .1, u + d - .4);
    spot(u - .1, .25, null);
    layer(u, "live0", .35);
    cam(u, .6, pad(R("live0", "hero"), 30));
    spot(u + .5, .35, pad(R("live0", "totalCard"), 6));
    label(u + .6, u + d - .2, R("live0", "total"), K.live, { dot: true });
    layer(u + 1.2, "live1", .2);
    layer(u + 1.9, "live2", .2);

    const v = t + 2 * d;
    block("update", "09", v + .1, scene.extras.end - .3);
    spot(v - .1, .25, null);
    layer(v, "update0", .35);
    cam(v, .5, pad(R("update0", "banner"), 60));
    click(v + .85, R("update0", "review"));
    layer(v + .9, "update1", .25);
    cam(v + .9, .6, pad(R("update1", "dialog"), 30));
    spot(v + 1.4, .35, pad(union(R("update1", "versions"), R("update1", "status")), 8));
    label(v + 1.5, scene.extras.end - .2, R("update1", "versions"), K.newVersion);
  }
  { // 10 theme, language and phone
    const t = S("interface");
    block("interface", "10", t + .1, scene.interface.end - .35);
    spot(t, .3, null);
    layer(t, "light", .4);
    cam(t, .7, { x: 0, y: 0, w: dims.w, h: dims.h });
    click(t + 1.3, R("light", "theme"));
    layer(t + 1.35, "dark", .8, "wipe");
    click(t + 2.8, R("dark", "locale"));
    layer(t + 2.85, "otherDark", .35);
  }
  beats.cursor.sort((a, b) => a.t - b.t);
  if (beats.reveals.length) reveal.src = layers[beats.reveals[0].name].src;

  // ---- renderers ----------------------------------------------------------------
  const copyRoot = $("#copy");
  const copyNodes = beats.copy.map(item => {
    // Portrait frames use hand-set Chinese line breaks where the default would orphan a character.
    const [kicker, landscapeLines, note, portraitLines] = item.data;
    const lines = vertical && portraitLines ? portraitLines : landscapeLines;
    const node = el("div", "copy-block");
    set(node, { left: `${L.copy.x}px`, top: `${L.copy.y}px`, width: `${L.copy.w}px` });
    const k = el("p", "kicker");
    k.append(el("b", null, item.number), el("span", null, kicker));
    const h2 = el("h2");
    for (const line of lines) h2.append(el("span", "line", line));
    const parts = [k, h2];
    if (note) parts.push(el("p", "note", note));
    node.append(...parts);
    copyRoot.append(node);
    return { ...item, node, parts };
  });
  function renderCopy(t) {
    for (const item of copyNodes) {
      const visible = t > item.a - .05 && t < item.b + .5;
      item.node.style.visibility = visible ? "visible" : "hidden";
      if (!visible) continue;
      item.parts.forEach((part, i) => {
        const e = env(t, item.a + i * .08, item.b + i * .02, .5, .28);
        set(part, { opacity: e.v, transform: `translateY(${(1 - e.i) * 34 - e.o * 24}px)` });
      });
    }
  }

  function cameraAt(t) {
    let rect = beats.cams[0].rect;
    for (const { t: start, dur, rect: target } of beats.cams) {
      if (t < start) break;
      rect = dur ? lerpRect(rect, target, ease.inOut(prog(t, start, start + dur))) : target;
    }
    const s = clamp(Math.min(view.w / rect.w, view.h / rect.h), coverScale, 1.6);
    let tx = view.w / 2 - (rect.x + rect.w / 2) * s;
    // Focus areas sit in the upper part of the frame, like a scrolled page.
    let ty = -rect.y * s + Math.max(0, view.h - rect.h * s) * .15;
    tx = clamp(tx, view.w - dims.w * s, 0);
    ty = clamp(ty, view.h - dims.h * s, 0);
    return { s, tx, ty };
  }
  const toView = (cam, x, y) => ({ x: cam.tx + x * cam.s, y: cam.ty + y * cam.s });

  const counterNodes = beats.counters.map(item => {
    const style = shots.overview.counters[item.key];
    const node = el("div", "counter");
    set(node, { left: `${style.x}px`, top: `${style.y}px`, ...style.font });
    content.append(node);
    const value = item.key === "cost" ? Number(style.text.replace(/[$,]/g, "")) : parseToken(style.text);
    return { ...item, node, value, final: style.text };
  });
  function parseToken(text) {
    const match = text.match(/^([\d.]+)([KMBT]?)$/);
    return Number(match[1]) * ({ "": 1, K: 1e3, M: 1e6, B: 1e9, T: 1e12 }[match[2]]);
  }
  // Mirrors the Dashboard's compact number and currency formats.
  function formatToken(number) {
    const a = Math.abs(number);
    if (a >= 1e9) return `${(number / 1e9).toFixed(a >= 1e10 ? 1 : 2)}B`;
    if (a >= 1e6) return `${(number / 1e6).toFixed(a >= 1e7 ? 1 : 2)}M`;
    if (a >= 1e3) return `${(number / 1e3).toFixed(a >= 1e4 ? 1 : 2)}K`;
    return Math.round(number).toLocaleString("en-US");
  }
  const formatUSD = amount => `$${amount.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;

  const labelNodes = beats.labels.map(item => {
    const node = el("div", `label ${item.place || "top"}`);
    if (item.dot) node.append(el("i", "dot"));
    node.append(document.createTextNode(item.text));
    $("#labels").append(node);
    return { ...item, node };
  });

  function renderPanel(t) {
    const e = env(t, S("total"), scene.interface.end - .45, .9, .4);
    panel.style.display = e.v > .001 ? "block" : "none";
    if (e.v <= .001) return;
    set(panel, { opacity: e.v, transform: `perspective(2200px) translateY(${(1 - e.i) * 150 + e.o * 60}px) rotateX(${(1 - e.i) * 14}deg) scale(${(.95 + .05 * e.i) * (1 - e.o * .05)})` });
    const cam = cameraAt(t);
    set(content, { transform: `translate(${cam.tx}px, ${cam.ty}px) scale(${cam.s})` });

    // Stills: the active one fades (or wipes) in over the previous one.
    let current = null, previous = null, p = 1, kind = "fade";
    for (const k of beats.layers) {
      if (t < k.t) break;
      if (k.name !== current) previous = current;
      current = k.name;
      p = k.fade ? ease.inOut(prog(t, k.t, k.t + k.fade)) : 1;
      kind = k.kind;
    }
    for (const [name, img] of Object.entries(layers)) {
      let opacity = 0, z = 0, clip = "none";
      if (name === current) {
        z = 2;
        if (kind === "wipe") { opacity = 1; const x = p * 135; clip = `polygon(0 0, ${x}% 0, ${x - 35}% 100%, 0 100%)`; }
        else opacity = p;
      } else if (name === previous && p < 1) { opacity = 1; z = 1; }
      set(img, { opacity, zIndex: z, clipPath: clip, visibility: opacity > 0 ? "visible" : "hidden" });
    }

    // Bars grow in from the left.
    const r = beats.reveals.find(item => t >= item.a && t < item.b);
    reveal.style.visibility = r ? "visible" : "hidden";
    if (r) {
      const x = r.rect.x + r.rect.w * ease.inOut(prog(t, r.a, r.b));
      reveal.style.clipPath = `polygon(${r.rect.x}px ${r.rect.y}px, ${x}px ${r.rect.y}px, ${x}px ${r.rect.y + r.rect.h}px, ${r.rect.x}px ${r.rect.y + r.rect.h}px)`;
    }

    for (const c of counterNodes) {
      const visible = t < c.off;
      c.node.style.visibility = visible ? "visible" : "hidden";
      if (!visible) continue;
      const value = c.value * ease.out(prog(t, c.a, c.b));
      c.node.textContent = t >= c.b ? c.final : c.key === "cost" ? formatUSD(value) : formatToken(value);
    }

    // Spotlight: dims everything but the focus area.
    let rect = null, prevRect = null, sp = 1;
    for (const k of beats.spots) {
      if (t < k.t) break;
      prevRect = rect;
      rect = k.rect;
      sp = k.dur ? ease.inOut(prog(t, k.t, k.t + k.dur)) : 1;
    }
    const spotNode = $("#spot");
    let spotRect = rect, spotOpacity = 1;
    if (rect && prevRect) spotRect = lerpRect(prevRect, rect, sp);
    else if (rect) spotOpacity = sp;
    else if (prevRect) { spotRect = prevRect; spotOpacity = 1 - sp; }
    spotNode.style.visibility = spotRect && spotOpacity > .001 ? "visible" : "hidden";
    if (spotRect) {
      const a = toView(cam, spotRect.x, spotRect.y);
      set(spotNode, { left: `${a.x}px`, top: `${a.y}px`, width: `${spotRect.w * cam.s}px`, height: `${spotRect.h * cam.s}px`, opacity: spotOpacity });
    }

    for (const item of labelNodes) {
      const e = env(t, item.a, item.b, .35, .25);
      item.node.style.visibility = e.v > .001 ? "visible" : "hidden";
      if (e.v <= .001) continue;
      const a = toView(cam, item.rect.x, item.rect.y);
      const b = toView(cam, item.rect.x + item.rect.w, item.rect.y + item.rect.h);
      const w = item.node.offsetWidth, h = item.node.offsetHeight;
      let place = item.place || "top";
      if (place === "top" && a.y - h - 18 < 8) place = "bottom";
      if (place === "left" && a.x - w - 20 < 8) place = "top";
      for (const name of ["top", "bottom", "left"]) item.node.classList.toggle(name, name === place);
      let x = clamp(a.x, 12, view.w - w - 12);
      let y = place === "bottom" ? Math.min(b.y + 18, view.h - h - 12) : a.y - h - 18;
      if (place === "left") { x = a.x - w - 20; y = clamp((a.y + b.y - h) / 2, 12, view.h - h - 12); }
      const slide = (1 - e.i) * 10;
      set(item.node, { opacity: e.v, transform: place === "left" ? `translate(${x - slide}px, ${y}px)` : `translate(${x}px, ${y + (place === "bottom" ? -slide : slide)}px)` });
    }

    renderCursor(t, cam);
  }

  function renderCursor(t, cam) {
    const cursor = $("#cursor"), ripple = $("#ripple");
    const points = beats.cursor;
    const center = r => toView(cam, r.x + r.w / 2, r.y + r.h / 2);
    let pos = null, opacity = 0, press = 1;
    const next = points.findIndex(point => point.t > t);
    const target = next === -1 ? null : points[next];
    const last = next === -1 ? points.at(-1) : next > 0 ? points[next - 1] : null;
    // Consecutive clicks glide from one control to the next without hiding the pointer.
    const chained = Boolean(target && last && target.t - last.t < 1.6);
    if (target && target.t - t <= .6) {
      const to = center(target.rect);
      const from = chained ? center(last.rect) : { x: to.x + 170, y: to.y + 150 };
      const p = ease.inOut(prog(t, target.t - .55, target.t));
      pos = { x: lerp(from.x, to.x, p), y: lerp(from.y, to.y, p) };
      opacity = chained ? 1 : prog(t, target.t - .6, target.t - .4);
    } else if (last && t - last.t < 1.6) {
      pos = center(last.rect);
      opacity = chained ? 1 : 1 - prog(t, last.t + .6, last.t + .9);
    }
    cursor.style.visibility = pos && opacity > .001 ? "visible" : "hidden";
    if (last && t - last.t < .2) press = 1 - .16 * Math.sin(Math.PI * (t - last.t) / .2);
    if (pos) set(cursor, { opacity, transform: `translate(${pos.x - 3}px, ${pos.y - 2}px) scale(${press})` });
    const rp = last ? prog(t, last.t, last.t + .5) : 1;
    ripple.style.visibility = last && rp < 1 ? "visible" : "hidden";
    if (last && rp < 1) {
      const c = center(last.rect);
      const r = 10 + 36 * ease.out(rp);
      set(ripple, { left: `${c.x - r}px`, top: `${c.y - r}px`, width: `${2 * r}px`, height: `${2 * r}px`, opacity: 1 - rp });
    }
  }

  const phone = $("#phone");
  $("#phone img").src = P.assets.base + shots.mobile.file;
  const phoneScale = L.phone.h / 884;
  function renderPhone(t) {
    const e = env(t, S("interface") + 3.7, scene.interface.end - .45, .7, .4);
    phone.style.visibility = e.v > .001 ? "visible" : "hidden";
    set(phone, { opacity: e.v, transform: `translate(${L.phone.x + (1 - e.i) * 280}px, ${L.phone.y + e.o * 60}px) scale(${phoneScale}) rotate(${(1 - e.i) * 7}deg)` });
  }

  const bgLight = $(".bg-light"), bgDark = $(".bg-dark");
  function renderBackground(t) {
    const open = ease.inOut(prog(t, scene.brand.end - .35, scene.brand.end + .3));
    const close = ease.inOut(prog(t, S("privacy") - .3, S("privacy") + .25));
    const r = 140 * open * (1 - close);
    set(bgLight, { clipPath: `circle(${r}% at 50% ${vertical ? 42 : 46}%)`, visibility: r > 0 ? "visible" : "hidden" });
    const drift = Math.sin(t * .3);
    for (const [bg, dx] of [[bgLight, 1], [bgDark, -1]]) {
      set($(".glow-a", bg), { transform: `translate(${W * .55 + drift * 80 * dx}px, ${-H * .35 + t * 5}px)` });
      set($(".glow-b", bg), { transform: `translate(${-W * .3 - drift * 60 * dx}px, ${H * .45 - t * 4}px)` });
    }
    set($(".grid", bgLight), { transform: `translate(${(t * 6) % 34}px, ${(t * 4) % 34}px)` });
    const e = env(t, S("total") + .2, S("privacy") - .5, .5, .3);
    set(corner, { opacity: e.v });
    set(badge, { opacity: e.v * .9 });
  }

  function renderHook(t) {
    const hook = $("#hook");
    const end = scene.hook.end;
    const out = ease.inOut(prog(t, end - .45, end));
    set(hook, { opacity: 1 - out, transform: `scale(${1 + out * .06})`, visibility: t < end + .05 ? "visible" : "hidden" });
    const parts = [...$(".hook-title").children, $(".hook-gauges")];
    parts.forEach((node, i) => {
      const p = ease.out(prog(t, .1 + i * .16, .7 + i * .16));
      set(node, { opacity: p, transform: `translateY(${(1 - p) * 40}px)` });
    });
    const grow = ease.in(prog(t, .5, end - .35));
    $("#hookCount").textContent = Math.round(186_402_917 * grow).toLocaleString("en-US");
    const left = 1 - .88 * grow;
    set($("#hookMeter"), { width: `${left * 100}%`, background: left > .5 ? "var(--teal-bright)" : left > .25 ? "var(--copper-bright)" : "#e0654f" });
    $("#hookPercent").textContent = `${Math.round(left * 100)}%`;
    $("#hookPercent").style.color = left > .5 ? "#bfe6df" : left > .25 ? "var(--copper-bright)" : "#f08a74";
    [...hookBars.children].forEach((bar, i) => {
      const n = .5 + .5 * Math.sin(i * 1.7 + t * 2.2) * Math.cos(i * .63 - t * 1.3);
      const h = (.12 + .88 * grow) * (.25 + .75 * n) * (.55 + .45 * Math.sin(i / barCount * Math.PI));
      bar.style.transform = `scaleY(${h})`;
    });
  }

  function renderBrand(t) {
    const b = scene.brand.start, end = scene.brand.end;
    const node = $("#brand");
    const out = ease.inOut(prog(t, end - .55, end - .2));
    set(node, { visibility: t > b - .05 && t < end ? "visible" : "hidden", opacity: 1 - out, transform: `scale(${1 + out * .05})` });
    const pop = ease.back(prog(t, b, b + .45));
    set($(".brand-icon"), { transform: `scale(${.35 + .65 * pop}) rotate(${(1 - pop) * -8}deg)`, opacity: prog(t, b, b + .12) });
    brandBars.forEach((bar, i) => set(bar, { transform: `scaleY(${ease.back(prog(t, b + .1 + i * .07, b + .4 + i * .07))})` }));
    zStroke.setAttribute("stroke-dashoffset", String(1 - ease.inOut(prog(t, b + .35, b + .8))));
    [$("#brand h1"), $("#brand .byline"), $(".brand-tagline")].forEach((w, i) => {
      const p = ease.out(prog(t, b + .45 + i * .12, b + .95 + i * .12));
      set(w, { opacity: p, transform: `translate${vertical ? "Y" : "X"}(${(1 - p) * (vertical ? 40 : -50)}px)` });
    });
  }

  const chipNodes = copy.privacy.chips.map(text => $(".privacy-chips").appendChild(el("li", null, text)));
  const ringNodes = [...document.querySelectorAll(".rings i")];
  function renderPrivacy(t) {
    const node = $("#privacy");
    const pv = S("privacy"), end = scene.privacy.end;
    const visible = t > pv - .1 && t < end + .05;
    node.style.visibility = visible ? "visible" : "hidden";
    if (!visible) return;
    const out = ease.inOut(prog(t, end - .35, end));
    set(node, { opacity: 1 - out, transform: `scale(${1 - out * .04})` });
    [$(".lock"), ...$("#privacy h1").children].forEach((p, i) => {
      const e = ease.out(prog(t, pv + .15 + i * .1, pv + .7 + i * .1));
      set(p, { opacity: e, transform: `translateY(${(1 - e) * 40}px) scale(${i ? 1 : .7 + .3 * ease.back(prog(t, pv + .15, pv + .7))})` });
    });
    chipNodes.forEach((chip, i) => {
      const e = ease.back(prog(t, pv + .8 + i * .18, pv + 1.3 + i * .18));
      set(chip, { opacity: clamp(e), transform: `translateY(${(1 - e) * 30}px) scale(${.92 + .08 * e})` });
    });
    const base = vertical ? 360 : 300, span = vertical ? 700 : 820;
    ringNodes.forEach((ring, i) => {
      const phase = ((t - pv) * .42 + i / ringNodes.length) % 1;
      const d = base + phase * span;
      set(ring, { width: `${d}px`, height: `${d}px`, transform: `translate(${-d / 2}px, ${-d / 2 - (vertical ? 160 : 120)}px)`, opacity: (1 - phase) * .3 * prog(t, pv, pv + .6) });
    });
  }

  const command = "./codex-usage install";
  const ctaParts = [$(".cta-icon"), $("#cta h1"), $(".cta-tagline"), $(".cta-title"), $(".terminal"), ...document.querySelectorAll(".cta-links li")];
  function renderCTA(t) {
    const ct = S("cta");
    $("#cta").style.visibility = t > ct - .05 ? "visible" : "hidden";
    ctaParts.forEach((part, i) => {
      const at = ct + .1 + i * .12;
      const p = i === 0 ? ease.back(prog(t, at, at + .6)) : ease.out(prog(t, at, at + .55));
      set(part, { opacity: clamp(p * 1.4), transform: i === 0 ? `scale(${.4 + .6 * p})` : `translateY(${(1 - p) * 34}px)` });
    });
    const chars = Math.round(command.length * prog(t, ct + .9, ct + 1.7));
    $("#typed").textContent = command.slice(0, chars);
    $(".caret").style.opacity = chars < command.length || Math.floor((t - ct) * 2.2) % 2 === 0 ? "1" : "0";
  }

  function render(t) {
    renderBackground(t);
    renderHook(t);
    renderBrand(t);
    renderCopy(t);
    renderPanel(t);
    renderPhone(t);
    renderPrivacy(t);
    renderCTA(t);
  }

  const images = [...document.images];
  window.promo = {
    ready: Promise.all([document.fonts.ready, ...images.filter(img => img.src).map(img => img.decode())]),
    seek(t) { render(t); },
    // The README poster links to the video, so it carries a play badge.
    poster(on) { $("#play").style.visibility = on ? "visible" : "hidden"; }
  };
})();
