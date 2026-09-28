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
    hook: { kicker: "每天都在用 Codex", title: ["Token 一路上涨，", "到底花在了哪？"], unit: "TOKENS" },
    brand: { tagline: "让 Codex 用量一目了然" },
    overview: { kicker: "01 · 总览", title: ["总量与费用，", "一眼看清"], body: "Token 构成、API 等价成本与定价覆盖率，集中在同一个 Dashboard。" },
    time: { kicker: "02 · 时间", title: ["精确到分钟，", "下钻到小时"], body: "任意起止时间查询，再顺着小时趋势与月历找到高峰。", steps: ["分钟级区间", "小时趋势", "每日月历"] },
    attribution: { kicker: "03 · 归属", title: ["从项目到", "每一个子任务"], body: "按项目、模型、来源拆分，任务树看清 Subagent 的消耗。", steps: ["按项目", "任务树", "搜索 Session"] },
    tools: { kicker: "04 · 工具", title: ["筛选、导出", "定价全都有"], body: "组合筛选即时生效，导出与费用始终跟随当前范围。", steps: ["只看 Fast", "JSON / CSV 导出", "模型定价"] },
    interface: { kicker: "05 · 界面", title: ["中英双语，", "明暗随心"], body: "字体、密度与动效可调，手机上同样顺手。", steps: ["中英双语", "明暗主题", "手机适配"] },
    privacy: { kicker: "100% 本地运行", title: ["数据只留在", "你自己的电脑"], chips: ["仅监听 127.0.0.1", "不保存 prompt 与回复", "单文件，无需数据库", "Windows · Linux · macOS"] },
    cta: { demo: "在线试用", meta: "免费开源 · MIT · 单文件安装" },
    badge: "演示画面使用合成数据",
    play: "观看 30 秒宣传片"
  } : {
    hook: { kicker: "Coding with Codex every day", title: ["The tokens keep climbing.", "Where do they go?"], unit: "TOKENS" },
    brand: { tagline: "Every token. Every task. One clear view." },
    overview: { kicker: "01 · Overview", title: ["Totals and cost", "at a glance"], body: "Token mix, API-equivalent cost, and pricing coverage in one dashboard." },
    time: { kicker: "02 · Time", title: ["Down to", "the minute"], body: "Query any exact range, then follow hourly trends and the calendar to the peak.", steps: ["Custom range", "Hourly", "Calendar"] },
    attribution: { kicker: "03 · Attribution", title: ["From projects", "to subagents"], body: "Split usage by project, model, and source. Task trees show every subagent.", steps: ["Projects", "Task tree", "Session search"] },
    tools: { kicker: "04 · Tools", title: ["Filter. Export.", "Price."], body: "Filters apply instantly; exports and costs always follow the current scope.", steps: ["Fast only", "JSON / CSV", "Model pricing"] },
    interface: { kicker: "05 · Interface", title: ["Light or dark.", "English or 中文."], body: "Adjustable type, density, and motion, and a layout that works on your phone.", steps: ["Bilingual", "Themes", "Mobile"] },
    privacy: { kicker: "100% local", title: ["Your data stays", "on your machine"], chips: ["Loopback-only server", "No prompts or replies stored", "Single binary, no database", "Windows · Linux · macOS"] },
    cta: { demo: "Live demo", meta: "FREE & OPEN SOURCE · MIT · ONE-FILE INSTALL" },
    badge: "Synthetic demo data",
    play: "Watch the 30s film"
  };

  // ---- math helpers -------------------------------------------------------
  const clamp = (x, a = 0, b = 1) => Math.min(b, Math.max(a, x));
  const prog = (t, a, b) => clamp((t - a) / (b - a));
  const lerp = (a, b, p) => a + (b - a) * p;
  const ease = {
    out: p => 1 - (1 - p) ** 3,
    inOut: p => p < .5 ? 4 * p ** 3 : 1 - (-2 * p + 2) ** 3 / 2,
    expo: p => p >= 1 ? 1 : 1 - 2 ** (-10 * p),
    back: p => { const c = 1.45; return 1 + (c + 1) * (p - 1) ** 3 + c * (p - 1) ** 2; },
    in: p => p ** 2.4
  };
  const union = (...rects) => {
    const x = Math.min(...rects.map(r => r.x)), y = Math.min(...rects.map(r => r.y));
    return { x, y, w: Math.max(...rects.map(r => r.x + r.w)) - x, h: Math.max(...rects.map(r => r.y + r.h)) - y };
  };
  const lerpRect = (a, b, p) => ({ x: lerp(a.x, b.x, p), y: lerp(a.y, b.y, p), w: lerp(a.w, b.w, p), h: lerp(a.h, b.h, p) });
  const pad = (r, n) => ({ x: r.x - n, y: r.y - n, w: r.w + 2 * n, h: r.h + 2 * n });
  // Returns in/out envelopes for an element that enters at `a` and leaves at `b`.
  const env = (t, a, b, inDur = .55, outDur = .32) => {
    const i = ease.out(prog(t, a, a + inDur));
    const o = ease.inOut(prog(t, b, b + outDur));
    return { i, o, v: i * (1 - o) };
  };
  const $ = (sel, root = document) => root.querySelector(sel);
  const el = (tag, cls, text) => { const n = document.createElement(tag); if (cls) n.className = cls; if (text != null) n.textContent = text; return n; };
  const set = (node, style) => { for (const [k, v] of Object.entries(style)) node.style[k] = v; };

  // ---- layout -----------------------------------------------------------------
  const L = vertical ? {
    copy: { x: 84, y: 178, w: 912 },
    panel: { x: 60, y: 692, w: 960 },
    cards: { x: 60, y: 720, w: 960, h: 1060 },
    phone: { x: 650, y: 1150, h: 660 },
    corner: { x: 84, y: 88 },
    badge: { x: 84, y: 1866 }
  } : {
    copy: { x: 128, y: 300, w: 560 },
    panel: { x: 748, y: 160, w: 1100 },
    cards: { x: 748, y: 150, w: 1100, h: 800 },
    phone: { x: 1560, y: 360, h: 660 },
    corner: { x: 128, y: 84 },
    badge: { x: 128, y: 1000 }
  };
  const chromeH = 46;
  const view = { w: L.panel.w, h: Math.round(L.panel.w * dims.h / dims.w) };

  // ---- build DOM --------------------------------------------------------------
  const stage = $("#stage");
  document.documentElement.classList.add(vertical ? "vertical" : "landscape");
  set(stage, { width: `${W}px`, height: `${H}px` });
  document.documentElement.lang = zh ? "zh-CN" : "en";

  const lookup = key => key.split(".").reduce((o, k) => o[k], copy);
  for (const node of document.querySelectorAll("[data-copy]")) node.textContent = lookup(node.dataset.copy);
  for (const node of document.querySelectorAll("[data-lines]")) for (const line of lookup(node.dataset.lines)) node.append(el("span", "line", line));

  for (const slot of document.querySelectorAll(".icon-slot")) slot.innerHTML = P.icon;
  // Brand reveal: a stroke mask draws the copper Z along its centre line.
  const brandSvg = $("#brand svg");
  const ns = "http://www.w3.org/2000/svg";
  const mask = document.createElementNS(ns, "mask");
  mask.id = "zDraw";
  mask.setAttribute("maskUnits", "userSpaceOnUse");
  mask.innerHTML = '<rect x="0" y="0" width="256" height="256" fill="black"/><path d="M50 44 H208 L44 167 Q33 175 33 186 V207 Q33 210 37 210 H230" fill="none" stroke="white" stroke-width="26" stroke-linecap="round" stroke-linejoin="round" pathLength="1" stroke-dasharray="1 1"/>';
  brandSvg.querySelector("defs").append(mask);
  const zPath = [...brandSvg.querySelectorAll(":scope > path")].at(-1);
  zPath.setAttribute("mask", "url(#zDraw)");
  const zStroke = mask.querySelector("path");
  const brandBars = [...brandSvg.querySelectorAll("g path")];
  for (const bar of brandBars) set(bar, { transformBox: "fill-box", transformOrigin: "50% 100%" });
  const brandTiles = [...brandSvg.querySelectorAll(":scope > rect")];

  const hookBars = $(".hook-bars");
  const barCount = vertical ? 28 : 56;
  for (let i = 0; i < barCount; i++) hookBars.append(el("i"));

  const corner = $("#corner");
  set(corner, { left: `${L.corner.x}px`, top: `${L.corner.y}px` });
  const badge = $("#badge");
  set(badge, { left: `${L.badge.x}px`, top: `${L.badge.y}px` });

  // Copy blocks for the product scenes.
  const copyRoot = $("#copy");
  const blocks = {};
  for (const id of ["overview", "time", "attribution", "tools", "interface"]) {
    const data = copy[id];
    const block = el("div", "copy-block");
    set(block, { left: `${L.copy.x}px`, top: `${L.copy.y}px`, width: `${L.copy.w}px` });
    const parts = [el("p", "kicker", data.kicker)];
    const h2 = el("h2");
    for (const line of data.title) h2.append(el("span", "line", line));
    parts.push(h2, el("p", "body", data.body));
    let steps = [];
    if (data.steps) {
      const row = el("div", "steps");
      steps = data.steps.map(s => row.appendChild(el("span", null, s)));
      parts.push(row);
    }
    block.append(...parts);
    copyRoot.append(block);
    blocks[id] = { block, parts: [parts[0], ...h2.children, ...parts.slice(2)], steps };
  }

  // Browser panel and the stills it frames.
  const panel = $("#panel");
  set(panel, { left: `${L.panel.x}px`, top: `${L.panel.y}px`, width: `${view.w}px`, height: `${view.h + chromeH}px` });
  set($(".viewport"), { width: `${view.w}px`, height: `${view.h}px` });
  const content = $("#content");
  set(content, { width: `${dims.w}px`, height: `${dims.h}px` });
  const layers = {};
  const addLayer = (name, src) => {
    const img = el("img");
    img.src = src;
    img.alt = "";
    set(img, { width: `${dims.w}px`, height: `${dims.h}px` });
    content.append(img);
    layers[name] = img;
    return img;
  };
  for (const name of ["overview", "range", "hourly", "calendar", "projects", "tree", "search", "light", "dark"]) addLayer(name, P.assets.base + shots[name].file);
  addLayer("other", P.other.base + P.other.shots.light.file);

  // ---- timings ----------------------------------------------------------------
  const o = scene.overview.start, tm = scene.time.start, at = scene.attribution.start;
  const tl = scene.tools.start, ui = scene.interface.start, pv = scene.privacy.start, ct = scene.cta.start;
  const F = n => shots[n].focus;
  const full = { x: 0, y: 0, w: dims.w, h: dims.h };
  const hero = F("overview").hero;

  // Layer cross-fades: [time, layer, duration, kind]
  const layerKeys = [
    [o, "overview", 0],
    [tm, "range", .45],
    [tm + 1.35, "hourly", .4],
    [tm + 2.7, "calendar", .4],
    [at, "projects", .45],
    [at + 1.35, "tree", .4],
    [at + 2.7, "search", .35],
    [ui, "light", 0],
    [ui + .55, "other", .4],
    [ui + 1.2, "dark", .65, "wipe"]
  ];
  // Camera moves: [start, duration, rect]
  const cameraKeys = [
    [o, 0, full],
    [o + 1.35, .95, pad(F("overview").totalCard, 20)],
    [o + 2.55, .8, pad(F("overview").costCard, 20)],
    [o + 3.6, .7, pad(hero, 60)],
    [tm, .7, pad(union(F("range").form, F("range").hero), 24)],
    [tm + 1.35, .6, pad(F("hourly").panel, 16)],
    [tm + 2.7, .6, pad(union(F("calendar").calendar, F("calendar").day), 30)],
    [at, .6, pad(F("projects").panel, 30)],
    [at + 1.35, .6, pad(F("tree").panel, 16)],
    [at + 2.7, .5, pad(union(F("search").search, { ...F("search").panel, h: Math.min(F("search").panel.h, 330) }), 24)],
    [ui, 0, full]
  ];
  // Highlight rings drawn over the stills: [layer, rect, in, out]
  const ringKeys = [
    ["overview", F("overview").total, o + 2.0, o + 2.55],
    ["overview", F("overview").cost, o + 3.1, o + 4.25],
    ["range", F("range").form, tm + .75, tm + 1.3],
    ["hourly", F("hourly").chart, tm + 1.95, tm + 2.65],
    ["calendar", F("calendar").day, tm + 3.25, tm + 3.85],
    ["search", F("search").search, at + 3.1, at + 3.8]
  ].map(([layer, rect, a, b]) => {
    const ring = el("div", "ring");
    content.append(ring);
    return { ring, rect: pad(rect, 10), a, b };
  });
  const stepTimes = {
    time: [tm, tm + 1.35, tm + 2.7],
    attribution: [at, at + 1.35, at + 2.7],
    tools: [tl + .35, tl + 1.35, tl + 2.35],
    interface: [ui + .55, ui + 1.2, ui + 1.8]
  };

  // Feature cards.
  const fast = shots.fast.focus;
  const cardSpecs = [
    { shot: "fast", rect: pad(union(fast.chips, fast.hero), 22) },
    { shot: "export", rect: shots.export.focus.dialog },
    { shot: "pricing", rect: shots.pricing.focus.dialog }
  ];
  const cards = cardSpecs.map(spec => {
    const card = el("div", "card");
    const img = el("img");
    img.src = P.assets.base + shots[spec.shot].file;
    set(img, { width: `${dims.w}px`, height: `${dims.h}px`, left: `${-spec.rect.x}px`, top: `${-spec.rect.y}px` });
    const shade = el("i", "shade");
    card.append(img, shade);
    set(card, { width: `${spec.rect.w}px`, height: `${spec.rect.h}px` });
    $("#cards").append(card);
    const s = Math.min(L.cards.w / spec.rect.w, L.cards.h / spec.rect.h, vertical ? 1.55 : 1.3);
    return { card, shade, s, x: L.cards.x + (L.cards.w - spec.rect.w * s) / 2, y: L.cards.y + (L.cards.h - spec.rect.h * s) / 2 };
  });

  // Phone.
  const phone = $("#phone");
  $("#phone img").src = P.assets.base + shots.mobile.file;
  const phoneScale = L.phone.h / 884;

  // Privacy chips and rings.
  const chipNodes = copy.privacy.chips.map(text => $(".privacy-chips").appendChild(el("li", null, text)));
  const ringNodes = [...document.querySelectorAll(".rings i")];

  const command = "./codex-usage install";
  const typed = $("#typed");
  const ctaParts = [$(".cta-icon"), $("#cta h1"), $(".cta-tagline"), $(".terminal"), ...document.querySelectorAll(".cta-links li"), $(".cta-meta")];

  const bgLight = $(".bg-light"), bgDark = $(".bg-dark");

  // ---- per-scene renderers ----------------------------------------------------
  function renderBackground(t) {
    const open = ease.inOut(prog(t, scene.brand.end - .4, scene.brand.end + .25));
    const close = ease.inOut(prog(t, pv - .3, pv + .25));
    const r = 140 * open * (1 - close);
    set(bgLight, { clipPath: `circle(${r}% at 50% ${vertical ? 42 : 46}%)`, visibility: r > 0 ? "visible" : "hidden" });
    const drift = Math.sin(t * .35);
    for (const [bg, dx] of [[bgLight, 1], [bgDark, -1]]) {
      set($(".glow-a", bg), { transform: `translate(${W * .55 + drift * 80 * dx}px, ${-H * .35 + t * 6}px)` });
      set($(".glow-b", bg), { transform: `translate(${-W * .3 - drift * 60 * dx}px, ${H * .45 - t * 5}px)` });
    }
    set($(".grid", bgLight), { transform: `translate(${(t * 6) % 34}px, ${(t * 4) % 34}px)` });
  }

  function renderHook(t) {
    const hook = $("#hook");
    const out = ease.inOut(prog(t, 2.5, 3.0));
    set(hook, { opacity: 1 - out, transform: `scale(${1 + out * .06})`, visibility: t < 3.05 ? "visible" : "hidden" });
    const parts = [$(".kicker", hook), ...$(".hook-title").children, $(".hook-counter")];
    parts.forEach((node, i) => {
      const p = ease.out(prog(t, .1 + i * .12, .7 + i * .12));
      set(node, { opacity: p, transform: `translateY(${(1 - p) * 40}px)` });
    });
    const grow = ease.in(prog(t, .35, 2.45));
    $("#hookCount").textContent = Math.round(2371512 * grow).toLocaleString("en-US");
    [...hookBars.children].forEach((bar, i) => {
      const n = .5 + .5 * Math.sin(i * 1.7 + t * 2.2) * Math.cos(i * .63 - t * 1.3);
      const h = (.12 + .88 * grow) * (.25 + .75 * n) * (.55 + .45 * Math.sin(i / barCount * Math.PI));
      set(bar, { transform: `scaleY(${h})`, height: "100%" });
    });
  }

  function renderBrand(t) {
    const b = scene.brand.start;
    const node = $("#brand");
    const out = ease.inOut(prog(t, scene.brand.end - .7, scene.brand.end - .3));
    set(node, { visibility: t > b - .05 && t < scene.brand.end + .05 ? "visible" : "hidden", opacity: 1 - out, transform: `scale(${1 + out * .05})` });
    const pop = ease.back(prog(t, b, b + .55));
    set($(".brand-icon"), { transform: `scale(${.35 + .65 * pop}) rotate(${(1 - pop) * -8}deg)`, opacity: prog(t, b, b + .15) });
    brandTiles.forEach(tile => set(tile, { opacity: 1 }));
    brandBars.forEach((bar, i) => set(bar, { transform: `scaleY(${ease.back(prog(t, b + .15 + i * .09, b + .5 + i * .09))})` }));
    zStroke.setAttribute("stroke-dashoffset", String(1 - ease.inOut(prog(t, b + .45, b + 1.0))));
    const words = [$("#brand h1"), $("#brand .byline"), $(".brand-tagline")];
    words.forEach((w, i) => {
      const p = ease.out(prog(t, b + .7 + i * .16, b + 1.3 + i * .16));
      set(w, { opacity: p, transform: `translate${vertical ? "Y" : "X"}(${(1 - p) * (vertical ? 40 : -50)}px)` });
    });
  }

  function renderCopy(t) {
    const order = ["overview", "time", "attribution", "tools", "interface"];
    for (const id of order) {
      const { block, parts, steps } = blocks[id];
      const a = scene[id].start + .15, b = scene[id].end - .4;
      const visible = t > a - .1 && t < b + .8;
      block.style.visibility = visible ? "visible" : "hidden";
      if (!visible) continue;
      parts.forEach((node, i) => {
        const e = env(t, a + i * .07, b + i * .03, .6, .3);
        set(node, { opacity: e.v, transform: `translateY(${(1 - e.i) * 46 - e.o * 34}px)` });
      });
      const times = stepTimes[id];
      steps.forEach((step, i) => {
        const on = ease.out(prog(t, times[i], times[i] + .25));
        const off = i < times.length - 1 ? ease.out(prog(t, times[i + 1], times[i + 1] + .25)) : 0;
        step.style.setProperty("--a", (on * (1 - off) * .999).toFixed(3));
      });
    }
    const e = env(t, o + .3, pv - .5, .5, .3);
    set(corner, { opacity: e.v, transform: `translateY(${(1 - e.i) * -16}px)` });
    set(badge, { opacity: e.v * .9 });
  }

  function cameraAt(t) {
    let rect = cameraKeys[0][2];
    for (const [start, dur, target] of cameraKeys) {
      if (t < start) break;
      rect = dur ? lerpRect(rect, target, ease.inOut(prog(t, start, start + dur))) : target;
    }
    const s = Math.min(view.w / rect.w, view.h / rect.h, 1.3);
    let tx = view.w / 2 - (rect.x + rect.w / 2) * s;
    // Short focus areas sit at the top, like a scrolled page, rather than cutting a heading in half.
    let ty = -rect.y * s;
    // Keep the still covering the viewport whenever it is large enough to do so.
    if (dims.w * s >= view.w) tx = clamp(tx, view.w - dims.w * s, 0);
    if (dims.h * s >= view.h) ty = clamp(ty, view.h - dims.h * s, 0);
    return { s, tx, ty };
  }

  function renderPanel(t) {
    const first = env(t, o + .05, tl - .05, 1.0, .5);
    const second = env(t, ui - .25, scene.interface.end - .5, .7, .35);
    const inP = t < tl + .5 ? first.i : second.i;
    const outP = t < tl + .5 ? first.o : second.o;
    const v = t < tl + .5 ? first.v : second.v;
    panel.style.visibility = v > .001 ? "visible" : "hidden";
    set(panel, {
      opacity: v,
      transform: `perspective(2200px) translateY(${(1 - inP) * 150 + outP * 70}px) rotateX(${(1 - inP) * 16}deg) scale(${(.94 + .06 * inP) * (1 - outP * .06)})`
    });
    const cam = cameraAt(t);
    set(content, { transform: `translate(${cam.tx}px, ${cam.ty}px) scale(${cam.s})` });

    // Determine the active layer and the one it is fading in over.
    let current = null, previous = null, p = 1, kind = "fade";
    for (const [start, name, dur, k] of layerKeys) {
      if (t < start) break;
      previous = current;
      current = name;
      p = dur ? ease.inOut(prog(t, start, start + dur)) : 1;
      kind = k || "fade";
    }
    for (const [name, img] of Object.entries(layers)) {
      let opacity = 0, z = 0, clip = "none";
      if (name === current) {
        z = 2;
        if (kind === "wipe") { opacity = 1; const x = p * 130; clip = `polygon(0 0, ${x}% 0, ${x - 30}% 100%, 0 100%)`; }
        else opacity = p;
      } else if (name === previous && p < 1) { opacity = 1; z = 1; }
      set(img, { opacity, zIndex: z, clipPath: clip, visibility: opacity > 0 ? "visible" : "hidden" });
    }
    for (const r of ringKeys) {
      const e = env(t, r.a, r.b, .3, .3);
      const grow = 1.08 - .08 * ease.back(prog(t, r.a, r.a + .4));
      set(r.ring, {
        left: `${r.rect.x}px`, top: `${r.rect.y}px`, width: `${r.rect.w}px`, height: `${r.rect.h}px`,
        borderWidth: `${3.5 / cam.s}px`, opacity: e.v, zIndex: 3,
        transform: `scale(${grow})`, visibility: e.v > 0 ? "visible" : "hidden"
      });
    }
  }

  function renderCards(t) {
    const arrive = cards.map((_, i) => prog(t, tl + .35 + i, tl + .95 + i));
    const out = ease.inOut(prog(t, scene.tools.end - .45, scene.tools.end - .1));
    cards.forEach((c, i) => {
      const p = ease.back(arrive[i]);
      const depth = arrive.slice(i + 1).reduce((sum, a) => sum + ease.out(a), 0);
      const visible = arrive[i] > 0 && out < 1;
      c.card.style.visibility = visible ? "visible" : "hidden";
      if (!visible) return;
      const s = c.s * (1 - .075 * depth) * (.9 + .1 * p);
      const w = parseFloat(c.card.style.width) * c.s, h = parseFloat(c.card.style.height) * c.s;
      const cx = c.x + w / 2, cy = c.y + h / 2;
      const rw = parseFloat(c.card.style.width) * s, rh = parseFloat(c.card.style.height) * s;
      const y = cy - rh / 2 + (1 - p) * 180 - depth * (vertical ? 70 : 54) + out * 120;
      set(c.card, {
        transform: `translate(${cx - rw / 2}px, ${y}px) scale(${s}) rotate(${(1 - p) * (i % 2 ? 4 : -4)}deg)`,
        opacity: clamp(arrive[i] * 3) * (1 - out),
        zIndex: 10 + i, borderRadius: `${18 / c.s}px`
      });
      c.shade.style.opacity = String(Math.min(.35, depth * .22));
    });
  }

  function renderPhone(t) {
    const e = env(t, ui + 1.8, scene.interface.end - .5, .7, .35);
    phone.style.visibility = e.v > .001 ? "visible" : "hidden";
    set(phone, { opacity: e.v, transform: `translate(${L.phone.x + (1 - e.i) * 260}px, ${L.phone.y + e.o * 60}px) scale(${phoneScale}) rotate(${(1 - e.i) * 7}deg)` });
  }

  function renderPrivacy(t) {
    const node = $("#privacy");
    const visible = t > pv - .1 && t < ct + .1;
    node.style.visibility = visible ? "visible" : "hidden";
    if (!visible) return;
    const out = ease.inOut(prog(t, ct - .35, ct));
    set(node, { opacity: 1 - out, transform: `scale(${1 - out * .04})` });
    const parts = [$(".lock"), $("#privacy .kicker"), ...$("#privacy h1").children];
    parts.forEach((p, i) => {
      const e = ease.out(prog(t, pv + .2 + i * .09, pv + .8 + i * .09));
      set(p, { opacity: e, transform: `translateY(${(1 - e) * 44}px) scale(${i ? 1 : .7 + .3 * ease.back(prog(t, pv + .2, pv + .8))})` });
    });
    chipNodes.forEach((chip, i) => {
      const e = ease.back(prog(t, pv + .85 + i * .2, pv + 1.35 + i * .2));
      set(chip, { opacity: clamp(e), transform: `translateY(${(1 - e) * 30}px) scale(${.92 + .08 * e})` });
    });
    const base = vertical ? 360 : 300, span = vertical ? 700 : 820;
    ringNodes.forEach((ring, i) => {
      const phase = ((t - pv) * .42 + i / ringNodes.length) % 1;
      const d = base + phase * span;
      set(ring, { width: `${d}px`, height: `${d}px`, transform: `translate(${-d / 2}px, ${-d / 2 - (vertical ? 120 : 110)}px)`, opacity: (1 - phase) * .32 * prog(t, pv, pv + .6) });
    });
  }

  function renderCTA(t) {
    const node = $("#cta");
    node.style.visibility = t > ct - .05 ? "visible" : "hidden";
    ctaParts.forEach((part, i) => {
      const at = ct + .1 + i * .13;
      const p = i === 0 ? ease.back(prog(t, at, at + .6)) : ease.out(prog(t, at, at + .55));
      set(part, { opacity: clamp(p * 1.4), transform: i === 0 ? `scale(${.4 + .6 * p})` : `translateY(${(1 - p) * 36}px)` });
    });
    const chars = Math.round(command.length * prog(t, ct + .75, ct + 1.55));
    typed.textContent = command.slice(0, chars);
    $(".caret").style.opacity = chars < command.length || Math.floor((t - ct) * 2.2) % 2 === 0 ? "1" : "0";
  }

  function render(t) {
    renderBackground(t);
    renderHook(t);
    renderBrand(t);
    renderCopy(t);
    renderPanel(t);
    renderCards(t);
    renderPhone(t);
    renderPrivacy(t);
    renderCTA(t);
  }

  const images = [...document.images];
  window.promo = {
    ready: Promise.all([document.fonts.ready, ...images.map(img => img.decode())]),
    seek(t) { render(t); },
    // The README poster links to the video, so it carries a play badge.
    poster(on) { $("#play").style.visibility = on ? "visible" : "hidden"; }
  };
})();
