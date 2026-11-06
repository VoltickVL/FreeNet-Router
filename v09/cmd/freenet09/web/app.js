/* FreeNet 0.9 — UI for official XKeen/Xray evidence, no custom VPN engine. */
(() => {
  "use strict";

  const views = new Set(["overview", "vpn", "routing", "dns", "xkeen", "xray", "updates", "subscription", "diagnostics"]);
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
    if (!silent) setField("xrayValidation", "НЕ ЗАПУСКАЛАСЬ");
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

  const nativeValidateButtons = document.querySelectorAll('[data-action="validate-xray"]');
  async function validateNativeXrayFromBrowser() {
    nativeValidateButtons.forEach(button => { button.disabled = true; });
    setField("xrayValidation", "ПРОВЕРЯЕТСЯ");
    try {
      const result = await readJSON("/api/native/xray/validate");
      const allowed = new Set(["VALIDATED_CONFIG_ONLY", "REJECTED", "UNKNOWN", "TIMEOUT", "MISSING", "BUSY"]);
      const state = allowed.has(result?.state) ? result.state : "UNKNOWN";
      setField("xrayValidation", state);
      status("Штатная проверка Xray: " + state + ". VPN/HTTPS по-прежнему NOT_VERIFIED.");
    } catch (_) {
      setField("xrayValidation", "UNKNOWN");
      status("Не удалось проверить конфигурацию Xray. Изменения не выполнялись.");
    } finally {
      nativeValidateButtons.forEach(button => { button.disabled = false; });
    }
  }
  nativeValidateButtons.forEach(button =>
    button.addEventListener("click", validateNativeXrayFromBrowser));

  const subscriptionButton = document.getElementById("subscription-preview");
  const subscriptionURL = document.getElementById("subscription-url");
  const subscriptionStatus = document.getElementById("subscription-state");
  const subscriptionResult = document.getElementById("subscription-preview-result");
  const subscriptionCount = document.getElementById("subscription-node-count");
  const subscriptionList = document.getElementById("subscription-nodes-list");

  function showSubscriptionStatus(label) {
    if (subscriptionStatus) subscriptionStatus.textContent = label;
  }
  async function previewSubscription() {
    const url = subscriptionURL?.value.trim() || "";
    if (!url.startsWith("https://")) {
      showSubscriptionStatus("STOP");
      status("Нужна действительная HTTPS-ссылка подписки.");
      return;
    }
    if (subscriptionButton) subscriptionButton.disabled = true;
    if (subscriptionResult) subscriptionResult.hidden = true;
    if (subscriptionList) subscriptionList.replaceChildren();
    showSubscriptionStatus("ПРОВЕРЯЕТСЯ");
    try {
      const response = await fetch("/api/subscription/preview", {
        method: "POST", cache: "no-store", credentials: "same-origin",
        redirect: "error",
        headers: {
          "Content-Type": "application/json",
          "X-FreeNet-Action": "preview-subscription"
        },
        body: JSON.stringify({ url })
      });
      const result = await response.json();
      if (!response.ok || result?.state !== "PREVIEW_ONLY" ||
          !Array.isArray(result.servers) || result.applied !== false) {
        throw new Error("subscription-preview-not-accepted");
      }
      const fragment = document.createDocumentFragment();
      for (const item of result.servers) {
        const line = document.createElement("div");
        line.className = "list-line";
        const text = document.createElement("div");
        const label = document.createElement("strong");
        label.textContent = String(item.name || "Сервер");
        const protocol = document.createElement("small");
        protocol.textContent = String(item.protocol || "native Xray");
        text.append(label, protocol);
        const chip = document.createElement("span");
        chip.className = "state-chip neutral";
        chip.textContent = "CONFIG ONLY";
        line.append(text, chip);
        fragment.append(line);
      }
      subscriptionList?.replaceChildren(fragment);
      if (subscriptionCount) subscriptionCount.textContent = String(result.count);
      if (subscriptionResult) subscriptionResult.hidden = false;
      showSubscriptionStatus("ПРОЧИТАНО");
      status("Серверы разобраны. Это только предпросмотр: Xray не изменён.");
    } catch (_) {
      showSubscriptionStatus("STOP");
      status("Подписка недоступна или содержит неподдерживаемый формат. Xray не изменён.");
    } finally {
      if (subscriptionURL) subscriptionURL.value = "";
      if (subscriptionButton) subscriptionButton.disabled = false;
    }
  }
  subscriptionButton?.addEventListener("click", previewSubscription);
  subscriptionURL?.addEventListener("keydown", event => {
    if (event.key === "Enter") {
      event.preventDefault();
      previewSubscription();
    }
  });

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
