(() => {
  const pendingTitles = new Set(['Требуется проверка состояния', 'Связь прервалась']);
  const flagPrefix = /^[\u{1F1E6}-\u{1F1FF}]{2}\s+/u;
  let routingPanelMounted = false;
  let routingDraft = null;
  let routingValidated = false;

  function qs(selector, root = document) {
    return root.querySelector(selector);
  }

  function qsa(selector, root = document) {
    return Array.from(root.querySelectorAll(selector));
  }

  function deferMountGuards() {
    if (typeof queueMicrotask === 'function') queueMicrotask(mountGuards);
    else Promise.resolve().then(mountGuards).catch(() => {});
  }

  function pendingEndpoint() {
    const card = qs('#selectedProfileCard');
    if (!card) return '';
    const title = String(card.querySelector('strong')?.textContent || '').trim();
    if (!pendingTitles.has(title)) return '';
    const endpoint = String(card.querySelector('.selected-endpoint')?.textContent || '').trim();
    return endpoint && endpoint !== '—' ? endpoint : '';
  }

  function hideStaleNotice(id) {
    const box = document.getElementById(id);
    if (!box) return;
    const text = String(box.textContent || '');
    if (
      text.includes('Фактическое состояние VPN после подключения не подтверждено') ||
      text.includes('Связь прервалась во время переключения') ||
      id === 'providerNotice'
    ) {
      if (typeof hideBox === 'function') hideBox(id);
      else box.hidden = true;
    }
  }

  function closeTopbarSelector() {
    const trigger = qs('#profilesTrigger');
    const menu = qs('#profilesMenu');
    const search = qs('#profileSearch');
    const text = qs('#profilesTriggerText');

    if (search) {
      search.value = '';
      search.blur();
      search.dispatchEvent(new Event('input', {bubbles: true}));
    }
    if (menu) {
      menu.hidden = true;
      menu.classList.remove('open', 'show', 'active');
      menu.setAttribute('aria-hidden', 'true');
    }
    if (trigger) {
      trigger.classList.remove('open', 'active');
      trigger.setAttribute('aria-expanded', 'false');
      trigger.blur();
    }
    if (text && !String(text.textContent || '').trim()) text.textContent = 'Выбрать VPN-сервер';
  }

  function finishPendingManualSwitch() {
    if (typeof selectedProviderID !== 'undefined') selectedProviderID = '';
    if (typeof selectedProviderName !== 'undefined') selectedProviderName = '';
    if (typeof providerPlanReady !== 'undefined') providerPlanReady = false;
    if (typeof providerApplied !== 'undefined') providerApplied = true;

    if (typeof renderProfileOptions === 'function') renderProfileOptions();
    if (typeof renderSelectedProfile === 'function') renderSelectedProfile(null);
    else {
      const card = qs('#selectedProfileCard');
      if (card) {
        card.textContent = '';
        card.hidden = true;
      }
    }

    const quick = qs('#quickActionsSection');
    const exactRow = qs('#exactConnectRow');
    const routine = quick && quick.querySelector('.action-row:not(#exactConnectRow)');
    if (exactRow) exactRow.hidden = true;
    if (routine) routine.hidden = false;

    const connect = qs('#exactConnectBtn');
    if (connect) {
      connect.disabled = true;
      connect.textContent = 'Выберите сервер заново';
    }

    closeTopbarSelector();
    if (typeof closeProfileMenu === 'function') closeProfileMenu();
    hideStaleNotice('providerNotice');
    hideStaleNotice('notice');
  }

  function reconcilePendingManualSwitch(status) {
    const expectedEndpoint = pendingEndpoint();
    if (!expectedEndpoint || !status) return false;
    const accepted = status.endpoint === expectedEndpoint && !status.busy && !status.updater_busy && status.xray_online === true;
    if (!accepted) return false;
    finishPendingManualSwitch();
    return true;
  }

  function clearSelectorAfterSuccessfulStatus(status) {
    if (!status || status.xray_online !== true || status.busy || status.updater_busy) return;
    const menu = qs('#profilesMenu');
    const search = qs('#profileSearch');
    const hasOpenMenu = !!(menu && !menu.hidden && getComputedStyle(menu).display !== 'none');
    const hasSearch = !!(search && String(search.value || '').trim());
    if (!hasOpenMenu && !hasSearch) return;
    closeTopbarSelector();
  }

  function patchSubscriptionCopy() {
    qsa('.fn-sub-next .fn-sub-meta').forEach(node => {
      if (String(node.textContent || '').includes('AUTO VPN')) {
        node.textContent = 'Автообновление списка Extra-профилей пока не включено.';
      }
    });
  }

  function settingsGearSVG() {
    return '<svg viewBox="0 0 24 24" width="19" height="19" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 15.5A3.5 3.5 0 1 0 12 8a3.5 3.5 0 0 0 0 7.5Z"></path><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06A1.65 1.65 0 0 0 15 19.4a1.65 1.65 0 0 0-1 .6 1.65 1.65 0 0 0-.36 1.05V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 8.6 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.6 15a1.65 1.65 0 0 0-.6-1 1.65 1.65 0 0 0-1.05-.36H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 8.6a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.6c.38-.16.72-.39 1-.7A1.65 1.65 0 0 0 10.36 3V3a2 2 0 1 1 4 0v.09A1.65 1.65 0 0 0 15.4 4.6a1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9c.16.38.39.72.7 1 .31.28.69.44 1.1.44H21a2 2 0 1 1 0 4h-.09A1.65 1.65 0 0 0 19.4 15Z"></path></svg>';
  }

  function ensureSettingsNavStable() {
    const nav = qs('.nav');
    if (!nav) return;
    let settings = qs('.nav-btn[data-page="settings"]', nav);
    if (!settings) {
      const before = qs('.nav-btn[data-page="network"]', nav) || qs('.nav-btn[data-page="routing"]', nav);
      settings = document.createElement('button');
      settings.type = 'button';
      settings.className = 'nav-btn';
      settings.dataset.page = 'settings';
      settings.innerHTML = `<span class="nav-icon">${settingsGearSVG()}</span><span>Настройки</span>`;
      settings.addEventListener('click', () => {
        location.hash = '#settings';
        if (typeof showPage === 'function') showPage('settings');
      });
      nav.insertBefore(settings, before || null);
    }
    settings.hidden = false;
    settings.removeAttribute('aria-hidden');
    settings.style.removeProperty('display');
    const icon = qs('.nav-icon', settings);
    if (icon) icon.innerHTML = settingsGearSVG();
  }

  function stripLeadingFlagText(node) {
    if (!node || !node.childNodes || node.querySelector?.('.flag-icon')) return;
    node.childNodes.forEach(child => {
      if (child.nodeType === Node.TEXT_NODE && flagPrefix.test(child.textContent || '')) {
        child.textContent = String(child.textContent || '').replace(flagPrefix, '');
      }
    });
  }

  function normalizeCurrentVPNFlags() {
    qsa('.card, .hero, #settingsPage, [data-page-view="settings"], [data-page-view="overview"]').forEach(root => {
      if (!String(root.textContent || '').includes('Текущий VPN')) return;
      qsa('h1,h2,h3,h4,strong,b,.country-name,.profile-name,.current-vpn-name,.fn-current-vpn-title', root).forEach(stripLeadingFlagText);
    });
  }

  async function apiJSON(path, options = {}) {
    const response = await fetch(path, Object.assign({cache: 'no-store'}, options));
    let body = {};
    try { body = await response.json(); } catch (_) {}
    if (!response.ok || body.success === false) throw new Error(body.error || `HTTP ${response.status}`);
    return body;
  }

  function routingPage() {
    const byHash = location.hash === '#routing' || location.hash === '#network';
    const candidates = qsa('.page.active, [data-page-view="network"], [data-page-view="routing"], .page');
    return candidates.find(page => byHash && String(page.textContent || '').includes('Маршрутизация')) || null;
  }

  function routingNotice(text, type = '') {
    const node = qs('#frnRoutingApplyNotice');
    if (!node) return;
    node.textContent = text || '';
    node.className = `notice show${type ? ' ' + type : ''}`;
  }

  function routingSetBusy(busy) {
    qsa('#frnRoutingLoad,#frnRoutingValidate,#frnRoutingApply').forEach(button => { button.disabled = !!busy; });
  }

  function syncRoutingApplyButtons() {
    const apply = qs('#frnRoutingApply');
    const validate = qs('#frnRoutingValidate');
    if (apply) apply.disabled = !routingValidated;
    if (validate) validate.disabled = !routingDraft;
  }

  function renderRoutingDraft(body) {
    routingDraft = {
      routing: body.routing || {routing: {domainStrategy: 'AsIs', rules: []}},
      policy: body.policy || {policy: {}}
    };
    routingValidated = false;
    const preview = qs('#frnRoutingPreview');
    if (preview) {
      const routingHash = body.routing_sha256 ? String(body.routing_sha256).slice(0, 12) : 'new';
      const policyHash = body.policy_sha256 ? String(body.policy_sha256).slice(0, 12) : 'new';
      preview.textContent = `Managed sections loaded: 05_routing.json ${routingHash} · 06_policy.json ${policyHash}\nRaw 04_outbounds and credentials are not shown. Apply requires validation first.`;
    }
    routingNotice('Live managed routing/policy загружены. Это preview: live config ещё не изменён.', 'ok');
    syncRoutingApplyButtons();
  }

  async function routingLoad() {
    routingSetBusy(true);
    routingNotice('Загружаю managed routing/policy…');
    try {
      const body = await apiJSON('/api/routing/config');
      if (body.mutation !== 'NONE') throw new Error('routing config read contract violated');
      renderRoutingDraft(body);
    } catch (error) {
      routingNotice(`Не удалось загрузить routing config: ${error.message || error}`, 'bad');
    } finally {
      routingSetBusy(false);
      syncRoutingApplyButtons();
    }
  }

  async function routingValidate() {
    if (!routingDraft) return routingLoad();
    routingSetBusy(true);
    routingNotice('Проверяю candidate через Xray validation…');
    try {
      const body = await apiJSON('/api/routing/validate', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(routingDraft)
      });
      if (body.mutation !== 'NONE' || body.xray_valid !== true) throw new Error('candidate validation was not accepted');
      routingDraft = {routing: body.routing || routingDraft.routing, policy: body.policy || routingDraft.policy};
      routingValidated = true;
      routingNotice('Candidate валиден. Можно применить через controlled apply: snapshot → atomic write → post-check → rollback при failure.', 'ok');
    } catch (error) {
      routingValidated = false;
      routingNotice(`Candidate не принят: ${error.message || error}. Live config не изменён.`, 'bad');
    } finally {
      routingSetBusy(false);
      syncRoutingApplyButtons();
    }
  }

  function describeApplyResult(body) {
    const mutation = body.mutation || (body.success ? 'APPLIED' : 'FAILED');
    const rollback = body.rollback || 'NOT_NEEDED';
    const snapshot = body.snapshot || body.snapshot_path || '';
    const parts = [`Result: ${mutation}`, `Rollback: ${rollback}`];
    if (snapshot) parts.push(`Snapshot: ${snapshot}`);
    if (body.error) parts.push(`Error: ${body.error}`);
    if (body.message) parts.push(String(body.message));
    if (mutation === 'STOP' || rollback === 'FAILED' || rollback === 'UNKNOWN') parts.push('STOP: дальнейшие routing/DNS/VPN mutation запрещены до проверки состояния.');
    return parts.join('\n');
  }

  async function routingApply() {
    if (!routingDraft || !routingValidated) { routingNotice('Сначала загрузите и проверьте candidate.', 'bad'); return; }
    routingSetBusy(true);
    routingNotice('Применяю routing transaction…');
    try {
      const body = await apiJSON('/api/routing/apply', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(routingDraft)
      });
      routingValidated = false;
      routingNotice(describeApplyResult(body), body.success === false || body.mutation === 'STOP' ? 'bad' : 'ok');
    } catch (error) {
      routingNotice(`Apply failed: ${error.message || error}. Если rollback неизвестен — STOP до проверки фактического состояния.`, 'bad');
    } finally {
      routingSetBusy(false);
      syncRoutingApplyButtons();
    }
  }

  function mountRoutingApplyPanel() {
    const page = routingPage();
    if (!page || qs('#frnRoutingApplyPanel', page)) return;
    routingPanelMounted = true;
    const panel = document.createElement('div');
    panel.id = 'frnRoutingApplyPanel';
    panel.className = 'card flat';
    panel.style.marginTop = '14px';
    panel.innerHTML = `
      <div class="card-head">
        <div>
          <h2 class="card-title-lg">Routing v2 apply</h2>
          <p class="hint">Controlled apply поверх backend transaction: preview → validation → snapshot → apply → post-check → rollback/STOP.</p>
        </div>
        <span class="page-kicker">LIVE MUTATION: CONTROLLED</span>
      </div>
      <div class="quick-layout" style="grid-template-columns:repeat(3,minmax(0,1fr));margin-top:10px">
        <button type="button" class="btn secondary" id="frnRoutingLoad">Загрузить preview</button>
        <button type="button" class="btn secondary" id="frnRoutingValidate" disabled>Проверить candidate</button>
        <button type="button" class="btn primary" id="frnRoutingApply" disabled>Применить routing</button>
      </div>
      <div id="frnRoutingPreview" class="endpoint" style="align-items:flex-start;white-space:pre-wrap;font-size:12px;color:var(--muted);margin-top:12px">Нажмите «Загрузить preview». Raw outbounds, subscription URL, UUID и Reality credentials не выводятся.</div>
      <div id="frnRoutingApplyNotice" class="notice show">Backend apply уже доступен; эта панель подключает его к пользовательскому UI.</div>
    `;
    const firstCard = qs('.card', page);
    if (firstCard && firstCard.parentNode) firstCard.parentNode.insertBefore(panel, firstCard.nextSibling);
    else page.appendChild(panel);
    qs('#frnRoutingLoad', panel)?.addEventListener('click', routingLoad);
    qs('#frnRoutingValidate', panel)?.addEventListener('click', routingValidate);
    qs('#frnRoutingApply', panel)?.addEventListener('click', routingApply);
  }

  function mountGuards() {
    patchSubscriptionCopy();
    ensureSettingsNavStable();
    normalizeCurrentVPNFlags();
    mountRoutingApplyPanel();
  }

  if (typeof updateStatusViews === 'function') {
    const previousUpdateStatusViews = updateStatusViews;
    updateStatusViews = function(status) {
      const result = previousUpdateStatusViews.apply(this, arguments);
      reconcilePendingManualSwitch(status);
      clearSelectorAfterSuccessfulStatus(status);
      mountGuards();
      return result;
    };
  }

  document.addEventListener('click', event => {
    if (!event.target?.closest?.('#profilesTrigger, #profilesMenu')) return;
    deferMountGuards();
  }, true);
  document.addEventListener('input', event => {
    if (event.target?.matches?.('#profileSearch')) deferMountGuards();
  }, true);
  window.addEventListener('hashchange', deferMountGuards);

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mountGuards, {once: true});
  else mountGuards();
  requestAnimationFrame(mountGuards);
})();
