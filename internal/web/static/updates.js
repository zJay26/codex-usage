(() => {
  let status = null;
  let pending = false;
  let timer;
  let initialVersion;
  let generation = 0;
  const busy = () => ["downloading", "installing"].includes(status?.phase);
  const dialog = $("#updateDialog");

  function render() {
    if (!status) return;
    $("#updateCurrent").textContent = `v${status.current_version}`;
    $("#updateLatest").textContent = status.latest_version ? `v${status.latest_version}` : "—";
    $("#autoCheckUpdates").checked = status.auto_check;
    $("#autoCheckUpdates").disabled = pending;
    $("#checkUpdates").disabled = pending || status.checking || busy();
    $("#installUpdate").disabled = pending || status.checking || busy() || !status.available || !status.can_install;
    $("#updatePortable").classList.toggle("hidden", status.can_install);
    const phase = status.checking || pending ? "checking" : busy() || ["rolled_back", "failed"].includes(status.phase) ? status.phase : status.available ? "available" : status.phase === "updated" ? "updated" : status.latest_version ? "latestAlready" : "unchecked";
    $("#updateStatus").textContent = t(`update.${phase}`, { version: status.latest_version });
    $("#updateError").textContent = status.error || "";
    $("#updateError").classList.toggle("hidden", !status.error);
    if (/^https:\/\/github\.com\/zJay26\/codex-usage\/releases(?:\/tag\/v\d+\.\d+\.\d+)?$/.test(status.release_url)) $("#updateReleaseLink").href = status.release_url;
    const dismissed = readStorage("codex-usage-update-dismissed") === status.latest_version;
    $("#updateBanner").classList.toggle("hidden", !status.available || dismissed || busy());
    $("#updateBannerText").textContent = t("update.available", { version: status.latest_version });
  }

  function schedule() {
    clearTimeout(timer);
    timer = setTimeout(refresh, busy() ? 2000 : document.hidden ? 600000 : 30000);
  }

  async function refresh() {
    if (pending) return schedule();
    const requestGeneration = generation;
    try {
      const next = await api("/api/v1/updates");
      if (requestGeneration !== generation || pending) return;
      status = next;
      initialVersion ||= status.current_version;
      if (initialVersion !== status.current_version && status.phase === "updated") { location.reload(); return; }
      render();
    } catch {
      $("#updateStatus").textContent = t(busy() ? "update.reconnecting" : "update.unavailable");
    } finally { schedule(); }
  }

  async function action(endpoint, body) {
    const previousAutoCheck = status?.auto_check;
    if (endpoint === "preferences") status.auto_check = body.auto_check;
    generation++;
    pending = true;
    render();
    try {
      status = await api(`/api/v1/updates/${endpoint}`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
    } catch (error) {
      if (endpoint === "preferences") status.auto_check = previousAutoCheck;
      if (endpoint === "install") status.phase = "installing";
      toast(error.message, true);
    } finally { pending = false; render(); schedule(); }
  }

  ["#updateButton", "#reviewUpdate"].forEach((id) => $(id).addEventListener("click", () => { openDialog(dialog); refresh(); }));
  $("#dismissUpdate").addEventListener("click", () => { writeStorage("codex-usage-update-dismissed", status.latest_version); render(); });
  $("#checkUpdates").addEventListener("click", () => action("check", {}));
  $("#autoCheckUpdates").addEventListener("change", (event) => action("preferences", { auto_check: event.target.checked }));
  $("#installUpdate").addEventListener("click", () => {
    if (!status?.available || !status.can_install || busy() || pending) return;
    action("install", { version: status.latest_version, confirm: true });
  });
  window.addEventListener("codex-usage-locale-change", render);
  document.addEventListener("visibilitychange", () => { if (!document.hidden) refresh(); else schedule(); });
  refresh();
})();
