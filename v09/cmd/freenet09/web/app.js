/* FreeNet 0.9 — UI for official XKeen/Xray evidence, no custom VPN engine. */
(() => {
  "use strict";

  const views = new Set(["overview", "vpn", "routing", "dns", "xkeen", "xray", "updates", "recovery", "diagnostics"]);
  const selectors = document.querySelectorAll("a[data-view]");
  const feedback = document.getElementById("feedback");
  const refresh = document.getElementById("refresh");
  const mobileSheet = document.getElementById("mobile-sheet");
  const moreButton = document.getElementById("more-toggle");
  const closeMore = document.getElementById("more-close");
  const toggleMore = (open) => {
    if (mobileSheet) mobileSheet.hidden = !open;
    if (moreButton) moreButton.setAttribute("aria-expanded", open ? "true" : "false");
  };
  moreButton?.addEventListener("click", () => toggleMore(Boolean(mobileSheet?.hidden)));
  closeMore?.addEventListener("click", () => toggleMore(false));

  function activate(view, fromNavigation) {
    if (!views.has(view)) view = "overview";
    document.querySelectorAll("section.view").forEach(section => {
      section.hidden = section.id !== view;
    });
    selectors.forEach(link => {
      const current = link.dataset.view === view;
      link.classList.toggle("is-active", current);
      if (current) link.setAttribute("aria-current", "page");
      else link.removeAttribute("aria-current");
    });
    toggleMore(false);
    if (fromNavigation) history.replaceState(null, "", "#" + view);
    window.scrollTo({ top: 0, behavior: "auto" });
  }

  selectors.forEach(link => {
    link.addEventListener("click", event => {
      event.preventDefault();
      activate(link.dataset.view, true);
    });
  });
  activate(location.hash.slice(1), false);
  window.addEventListener("hashchange", () => activate(location.hash.slice(1), false));

  function setField(name, text) {
    document.querySelectorAll('[data-field="' + name + '"]').forEach(el => {
      el.textContent = String(text);
    });
  }

  function renderList(target, items, kind) {
    const holder = document.getElementById(target);
    if (!holder) return;
    if (!Array.isArray(items)) return;
    const fragment = document.createDocumentFragment();
    if (items.length === 0) {
      const blank = document.createElement("div");
      blank.className = "empty-state";
      blank.textContent = kind === "balancer" ? "Балансировщики не настроены." : "В native-конфигурации нет outbound.";
      fragment.append(blank);
    }
    for (const item of items) {
      const line = document.createElement("div");
      line.className = "list-line";
      const text = document.createElement("div");
      const name = document.createElement("strong");
      name.textContent = String(item.tag || "без названия");
      const caption = document.createElement("small");
      caption.textContent = kind === "balancer"
        ? String(item.strategy || "not-specified") + " · selector: " + Number(item.selectors || 0)
        : String(item.protocol || "other") + (item.in_balancer ? " · в пуле" : "");
      const chip = document.createElement("span");
      chip.className = "state-chip neutral";
      chip.textContent = kind === "balancer" ? String(Number(item.servers || 0)) + " узлов" : "CONFIG";
      text.append(name, caption);
      line.append(text, chip);
      fragment.append(line);
    }
    // No HTML injection: Xray names are only assigned through textContent.
    holder.replaceChildren(fragment);
  }

  function status(message) {
    if (!feedback) return;
    feedback.hidden = false;
    feedback.textContent = message;
  }

  async function readJSON(url) {
    const response = await fetch(url, { cache: "no-store", credentials: "same-origin" });
    if (!response.ok && response.status !== 409) throw new Error("HTTP " + response.status);
    return response.json();
  }

  function nativeVersionLabel(item) {
    if (item?.state === "VERSION_DETECTED" &&
        /^[0-9]{1,4}\.[0-9]{1,4}(?:\.[0-9]{1,4})?$/.test(item.version || "")) {
      return item.version;
    }
    return item?.state || "UNKNOWN";
  }

  async function refreshEvidence(silent = false) {
    if (refresh) refresh.disabled = true;
    try {
      const [overview, topology, system, versions] = await Promise.all([
        readJSON("/api/overview"),
        readJSON("/api/native/xray/topology"),
        readJSON("/api/native/system"),
        readJSON("/api/native/versions")
      ]);
      setField("xkeenVersion", nativeVersionLabel(versions.xkeen));
      setField("xrayVersion", nativeVersionLabel(versions.xray));
      setField("binaryXray", overview.xray || "UNKNOWN");
      setField("legacyFreeNet", overview.legacy_freenet || "UNKNOWN");
      setField("topologyState", topology.state || "STOP");
      setField("serverCount", Array.isArray(topology.servers) ? topology.servers.length : 0);
      setField("balancerCount", Array.isArray(topology.balancers) ? topology.balancers.length : 0);
      setField("xrayProcess", system.xray_process || "UNKNOWN");
      setField("xkeenInit", system.xkeen_init || "UNKNOWN");
      setField("xkeenConfig", system.xkeen_config || "UNKNOWN");
      setField("dnsState", system.dns?.state || "UNKNOWN");
      setField("dnsCount", system.dns?.count ?? 0);
      setField("dnsMode", system.dns?.mode || "NOT_VERIFIED");
      setField("routingState", system.routing?.state || "UNKNOWN");
      setField("routingCount", system.routing?.count ?? 0);
      setField("routingMode", system.routing?.mode || "NOT_VERIFIED");
      setField("geoCount", system.geodata?.files ?? 0);
      setField("geoCron", system.xkeen_geo_cron || "UNKNOWN");
      setField("speedCron", system.xkeen_balancer_cron || "UNKNOWN");
      if (topology.state === "STOP") {
        const safeNote = document.createElement("div");
        safeNote.className = "empty-state";
        safeNote.textContent = "Конфигурация не подтверждена. STOP — без догадок.";
        document.getElementById("outbounds-list")?.replaceChildren(safeNote);
        document.getElementById("balancers-list")?.replaceChildren(safeNote.cloneNode(true));
      } else {
        renderList("outbounds-list", topology.servers, "outbound");
        renderList("balancers-list", topology.balancers, "balancer");
      }
      if (!silent) status("Данные обновлены. Версии получены из официальных CLI; VPN по-прежнему NOT_VERIFIED.");
    } catch (_) {
      setField("xkeenVersion", "UNKNOWN");
      setField("xrayVersion", "UNKNOWN");
      if (!silent) status("Не удалось обновить native-данные. VPN не подтверждён.");
    } finally {
      if (refresh) refresh.disabled = false;
    }
  }

  refresh?.addEventListener("click", () => refreshEvidence(false));
  refreshEvidence(true);

  // iOS standalone install works via Safari > Share > Add to Home Screen.
  // PWA shell assets may be cached, but NO API data, configs or credentials.
  if ("serviceWorker" in navigator &&
      (location.protocol === "https:" || location.hostname === "127.0.0.1" || location.hostname === "localhost")) {
    window.addEventListener("load", () => {
      navigator.serviceWorker.register("/sw.js", { scope: "/" }).catch(() => {
        // Service worker is optional; the web panel works without it.
      });
    });
  }
})();
