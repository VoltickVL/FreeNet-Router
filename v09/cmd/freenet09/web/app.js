/* FreeNet 0.9 — UI for official XKeen/Xray evidence, no custom VPN engine. */
(() => {
  "use strict";

  const views = new Set(["overview", "vpn", "routing", "dns", "xkeen", "xray", "updates", "subscription", "diagnostics"]);
  const selectors = document.querySelectorAll("a[data-view]");
  const feedback = document.getElementById("feedback");
  const refresh = document.getElementById("refresh");
  const logoutButton = document.getElementById("admin-logout");
  let secureControlCenterAuthenticated = false;
  let detectedXKeenVersion = "";
  function updateNativeBalancerButtons() {
    const actions = document.getElementById("xkeen-balancer-actions");
    if (actions) actions.hidden = !(secureControlCenterAuthenticated && detectedXKeenVersion === "2.2");
    const state = document.getElementById("xkeen-balancer-state");
    if (state && detectedXKeenVersion === "2.1") state.textContent = "НЕТ В XKEEN 2.1";
  }
  fetch("/api/auth/status", {cache:"no-store",credentials:"same-origin"}).then(r => {
    if (!r.ok) return null;
    return r.json();
  }).then(result => {
    secureControlCenterAuthenticated = Boolean(result?.authenticated && location.protocol === "https:");
    updateNativeBalancerButtons();
    if (result?.authenticated && logoutButton) logoutButton.hidden = false;
    if (secureControlCenterAuthenticated) {
      const actions = document.getElementById("subscription-apply-actions");
      if (actions) actions.hidden = false;
      loadNativeRouteTargets().then(loadAppliedSubscriptionStatus);
      const nativeButtons = document.getElementById("xkeen-service-actions");
      if (nativeButtons) nativeButtons.hidden = false;
      refreshXKeenService();
    }
  }).catch(() => {});
  logoutButton?.addEventListener("click", async () => {
    logoutButton.disabled = true;
    try {
      const result = await fetch("/api/auth/logout", {
        method:"POST", credentials:"same-origin", cache:"no-store",
        headers:{"X-FreeNet-Action":"logout"}
      });
      if (!result.ok) throw new Error("logout-unavailable");
      window.location.replace("/");
    } catch (_) {
      logoutButton.disabled = false;
    }
  });

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

  const nativeXKeenState = document.getElementById("xkeen-service-state");
  const nativeXKeenButtons = document.querySelectorAll("[data-xkeen-action]");
  async function refreshXKeenService() {
    if (!nativeXKeenState) return;
    try {
      const response = await fetch("/api/native/xkeen/service", {
        method:"GET",credentials:"same-origin",cache:"no-store"
      });
      const report = await response.json();
      nativeXKeenState.textContent = response.ok
        ? String(report.state || "UNKNOWN") : "UNKNOWN";
    } catch (_) {
      nativeXKeenState.textContent = "UNKNOWN";
    }
  }
  async function nativeXKeenAction(action) {
    if (!["start","stop","restart"].includes(action)) return;
    const label = {start:"запустить",stop:"остановить",restart:"перезапустить"}[action];
    if (!window.confirm("Штатно " + label + " XKeen? При остановке может прерваться VPN. Продолжить?")) return;
    nativeXKeenButtons.forEach(b => {b.disabled=true;});
    if (nativeXKeenState) nativeXKeenState.textContent="ВЫПОЛНЯЕТСЯ";
    try {
      const response=await fetch("/api/native/xkeen/service",{
        method:"POST",cache:"no-store",credentials:"same-origin",redirect:"error",
        headers:{"Content-Type":"application/json","X-FreeNet-Action":"xkeen-service"},
        body:JSON.stringify({action})
      });
      const report=await response.json();
      const state=String(report?.state || "UNKNOWN");
      if (nativeXKeenState) nativeXKeenState.textContent=state;
      status("XKeen: "+state+". Клиентский VPN/HTTPS: NOT_VERIFIED.");
    } catch (_) {
      if (nativeXKeenState) nativeXKeenState.textContent="UNKNOWN";
      status("STOP: состояние службы неизвестно. Не повторяйте изменение без проверки.");
    } finally {
      nativeXKeenButtons.forEach(b => {b.disabled=false;});
    }
  }
  nativeXKeenButtons.forEach(button => button.addEventListener("click",() =>
    nativeXKeenAction(button.dataset.xkeenAction)));

  const nativeBalancerState = document.getElementById("xkeen-balancer-state");
  const nativeBalancerButtons = document.querySelectorAll("[data-xkeen-balancer]");
  async function nativeBalancerAction(action) {
    if (!["next","prev","auto"].includes(action)) return;
    const label = {next:"следующую ноду",prev:"предыдущую ноду",auto:"автоматический выбор"}[action];
    if (!window.confirm("Вызвать оригинальный XKeen 2.2 для переключения: " + label + "? Соединение VPN может измениться. Продолжить?")) return;
    nativeBalancerButtons.forEach(b => {b.disabled=true;});
    if (nativeBalancerState) nativeBalancerState.textContent="ВЫПОЛНЯЕТСЯ";
    try {
      const response=await fetch("/api/native/xkeen/balancer", {
        method:"POST",credentials:"same-origin",cache:"no-store",redirect:"error",
        headers:{"Content-Type":"application/json","X-FreeNet-Action":"xkeen-balancer"},
        body:JSON.stringify({action})
      });
      const report=await response.json();
      const state=response.ok ? String(report?.state || "UNKNOWN") : "STOP";
      if (nativeBalancerState) nativeBalancerState.textContent=state;
      status(response.ok
        ? "Команда XKeen принята; фактический выбор ноды и клиентский VPN/HTTPS: NOT_VERIFIED."
        : "STOP: штатная операция не подтверждена. Повтор без диагностики запрещён.");
    } catch (_) {
      if (nativeBalancerState) nativeBalancerState.textContent="UNKNOWN";
      status("STOP: состояние переключения неизвестно. Не повторяйте операцию без проверки.");
    } finally {
      nativeBalancerButtons.forEach(b => {b.disabled=false;});
    }
  }
  nativeBalancerButtons.forEach(button => button.addEventListener("click", () =>
    nativeBalancerAction(button.dataset.xkeenBalancer)));

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
      const [overview, topology, system, versions, canaryReadiness] = await Promise.all([
        readJSON("/api/overview"),
        readJSON("/api/native/xray/topology"),
        readJSON("/api/native/system"),
        readJSON("/api/native/versions"),
        readJSON("/api/native/canary-readiness")
      ]);
      const canaryState=document.getElementById("canary-readiness-state");
      const canaryReason=document.getElementById("canary-readiness-reason");
      if (canaryState) canaryState.textContent=String(canaryReadiness.state || "STOP");
      if (canaryReason) canaryReason.textContent=String(canaryReadiness.reason || "NATIVE_STATE_UNKNOWN");
      detectedXKeenVersion = versions.xkeen?.state === "VERSION_DETECTED" ? String(versions.xkeen.version || "") : "";
      updateNativeBalancerButtons();
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
  const subscriptionApplyButton = document.getElementById("subscription-apply");
  const subscriptionRefreshButton = document.getElementById("subscription-refresh");
  const subscriptionAppliedStatus = document.getElementById("subscription-applied-status");
  const subscriptionTarget = document.getElementById("subscription-target");
  let routeTargets = [];
  let pendingPreviewURL = "";
  let lockedAfterRollbackFailure = false;

  async function loadNativeRouteTargets() {
    if (!subscriptionTarget) return;
    subscriptionTarget.disabled = true;
    subscriptionTarget.replaceChildren(new Option("Проверка штатной маршрутизации...", ""));
    routeTargets = [];
    try {
      const response = await fetch("/api/subscription/targets", {
        method:"GET",cache:"no-store",credentials:"same-origin"
      });
      const report = await response.json();
      if (!response.ok || report?.state !== "CONFIG_ONLY" || !Array.isArray(report.targets)) {
        throw new Error("native-routing-unavailable");
      }
      const safe = report.targets.filter(t =>
        typeof t.balancer === "string" && typeof t.selector === "string" &&
        t.balancer.length > 0 && t.selector.length > 0);
      if (!safe.length) throw new Error("empty-native-balancers");
      routeTargets = safe;
      const options = document.createDocumentFragment();
      const prompt = new Option(safe.length === 1
        ? "Выбран единственный штатный balancer"
        : "Выберите штатный balancer", "");
      options.append(prompt);
      for (const [i, item] of safe.entries()) {
        options.append(new Option(item.balancer + " · selector " + item.selector, String(i)));
      }
      subscriptionTarget.replaceChildren(options);
      subscriptionTarget.disabled = false;
      if (safe.length === 1) subscriptionTarget.value = "0";
      if (pendingPreviewURL && !lockedAfterRollbackFailure &&
          subscriptionTarget.value !== "" && subscriptionApplyButton) {
        subscriptionApplyButton.disabled = false;
      }
    } catch (_) {
      subscriptionTarget.replaceChildren(new Option(
        "STOP: нет подходящего штатного Xray balancer / routing", ""));
      subscriptionTarget.disabled = true;
      if (subscriptionApplyButton) subscriptionApplyButton.disabled = true;
    }
  }
  subscriptionTarget?.addEventListener("change", () => {
    if (subscriptionApplyButton) {
      subscriptionApplyButton.disabled = !pendingPreviewURL ||
        subscriptionTarget.value === "" || lockedAfterRollbackFailure;
    }
  });

  function nativeApplyMessage(result) {
    const state = result?.state || "STOP";
    if (state === "CONFIG_APPLIED_NOT_EGRESS_VERIFIED") return "Конфигурация применена. Клиентский VPN-маршрут ещё НЕ ПРОВЕРЕН.";
    if (state === "ROLLED_BACK") return "Применение не прошло, предыдущий управляемый фрагмент восстановлен.";
    if (state === "ROLLBACK_FAILED") return "STOP: состояние Xray НЕИЗВЕСТНО. Новые изменения заблокированы.";
    if (state === "NOT_CONFIGURED") return "Подписка ещё не настроена.";
    return "Не удалось применить: STOP. Рабочие настройки не изменять.";
  }

  async function loadAppliedSubscriptionStatus() {
    try {
      const response = await fetch("/api/subscription/status", {
        method:"GET", cache:"no-store", credentials:"same-origin"
      });
      if (!response.ok) return;
      const result = await response.json();
      if (subscriptionAppliedStatus) {
        subscriptionAppliedStatus.textContent = result.state === "CONFIG_APPLIED_NOT_EGRESS_VERIFIED"
          ? "BALANCER " + String(result.balancer || "?") + " · EGRESS НЕ ПРОВЕРЕН"
          : String(result.state || "STOP");
        const savedIndex = routeTargets.findIndex(t =>
          t.balancer === result.balancer && t.selector === result.selector);
        if (savedIndex >= 0 && subscriptionTarget) subscriptionTarget.value = String(savedIndex);
      }
    } catch (_) {
      if (subscriptionAppliedStatus) subscriptionAppliedStatus.textContent = "UNKNOWN";
    }
  }

  async function submitNativeSubscription(refreshMode) {
    if (lockedAfterRollbackFailure) return;
    if (!refreshMode && !pendingPreviewURL) return;
    const chosen = routeTargets[Number(subscriptionTarget?.value)];
    if (!refreshMode && (subscriptionTarget?.value === "" || !chosen)) {
      status("STOP: сначала выберите существующий штатный Xray balancer.");
      return;
    }
    if (!window.confirm(refreshMode
      ? "Обновить сохранённую подписку и перезапустить штатный XKeen/Xray?"
      : "Применить просмотренную подписку через штатный Xray и XKeen?")) return;
    subscriptionApplyButton.disabled = true;
    subscriptionRefreshButton.disabled = true;
    showSubscriptionStatus("ПРИМЕНЯЕТСЯ");
    try {
      const response = await fetch("/api/subscription/apply", {
        method:"POST", cache:"no-store", credentials:"same-origin",
        redirect:"error",
        headers:{
          "Content-Type":"application/json",
          "X-FreeNet-Action":"apply-subscription"
        },
        body: JSON.stringify(refreshMode ? {refresh:true} : {
          url:pendingPreviewURL,balancer:chosen.balancer,selector:chosen.selector
        })
      });
      const result = await response.json();
      const message = nativeApplyMessage(result);
      status(message);
      showSubscriptionStatus(result.state || "STOP");
      if (result.state === "ROLLBACK_FAILED") lockedAfterRollbackFailure = true;
      if (response.ok && result.state === "CONFIG_APPLIED_NOT_EGRESS_VERIFIED") {
        await loadAppliedSubscriptionStatus();
        refreshEvidence(true);
      }
    } catch (_) {
      showSubscriptionStatus("UNKNOWN");
      status("Результат применения не подтверждён. Не повторять изменение до проверки состояния.");
      lockedAfterRollbackFailure = true;
    } finally {
      pendingPreviewURL = "";
      if (subscriptionURL) subscriptionURL.value = "";
      subscriptionApplyButton.disabled = true;
      subscriptionRefreshButton.disabled = lockedAfterRollbackFailure;
    }
  }
  subscriptionApplyButton?.addEventListener("click",() => submitNativeSubscription(false));
  subscriptionRefreshButton?.addEventListener("click",() => submitNativeSubscription(true));


  function showSubscriptionStatus(label) {
    if (subscriptionStatus) subscriptionStatus.textContent = label;
  }
  async function previewSubscription() {
    pendingPreviewURL = "";
    if (subscriptionApplyButton) subscriptionApplyButton.disabled = true;
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
      pendingPreviewURL = url;
      if (subscriptionApplyButton && !lockedAfterRollbackFailure &&
          subscriptionTarget?.value !== "" &&
          !document.getElementById("subscription-apply-actions")?.hidden) {
        subscriptionApplyButton.disabled = false;
      }
      status("Серверы разобраны. Предпросмотр выполнен, Xray ещё не изменён.");
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
