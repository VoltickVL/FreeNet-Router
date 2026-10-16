(() => {
  const qs = (selector, root = document) => root.querySelector(selector);
  const wait = ms => new Promise(resolve => setTimeout(resolve, ms));
  let updatePlan = null;
  let updatePolling = false;
  let authFetchWrapped = false;
  let subscriptionMounted = false;

  const visibilityRule = '#authSection[hidden],#controlCenter[hidden]{display:none!important}';
  if (!qs('#freenetVisibilityGuard')) {
    const visibilityGuard = document.createElement('style');
    visibilityGuard.id = 'freenetVisibilityGuard';
    visibilityGuard.textContent = visibilityRule;
    document.head.appendChild(visibilityGuard);
  }

  const shellIconPaths = {
    provider: '<ellipse cx="12" cy="6" rx="7.5" ry="3"/><path d="M4.5 6v6c0 1.7 3.4 3 7.5 3s7.5-1.3 7.5-3V6"/><path d="M4.5 12v6c0 1.7 3.4 3 7.5 3s7.5-1.3 7.5-3v-6"/>',
    dns: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c2.4 2.5 3.6 5.5 3.6 9S14.4 18.5 12 21M12 3c-2.4-2.5-3.6-5.5-3.6-9S9.6 18.5 12 21"/>',
    overview: '<path d="M4 10.5 12 4l8 6.5v8a1.5 1.5 0 0 1-1.5 1.5h-13A1.5 1.5 0 0 1 4 18.5v-8Z"/><path d="M9.5 20v-6h5v6"/>',
    subscription: '<circle cx="12" cy="12" r="8.5"/><circle cx="12" cy="12" r="4.5"/><path d="M12 3.5v2M20.5 12h-2M12 20.5v-2M3.5 12h2"/>',
    network: '<path d="M4 8h14m0 0-3-3m3 3-3 3M20 16H6m0 0 3-3m-3 3 3 3"/>',
    automation: '<path d="M5 12a7 7 0 0 1 12-4.9L19 9"/><path d="M19 5v4h-4M19 12a7 7 0 0 1-12 4.9L5 15"/><path d="M5 19v-4h4"/>',
    access: '<path d="M12 3 19 6v5c0 4.5-3 7.7-7 10-4-2.3-7-5.5-7-10V6l7-3Z"/><path d="m8.8 12.2 2 2 4.4-4.4"/>',
    vpn: '<path d="M5 8.5h14M7.5 4.5h9M7.5 12.5h9M5 16.5h14M9 20.5h6"/>',
    system: '<rect x="4" y="4" width="16" height="16" rx="3"/><path d="M8 9h8M8 13h8M8 17h5"/>',
    profiles: '<path d="m12 3 8 4-8 4-8-4 8-4Z"/><path d="m4 12 8 4 8-4M4 17l8 4 8-4"/>',
    refresh: '<path d="M20 11a8 8 0 1 0-2.3 5.7"/><path d="M20 5v6h-6"/>',
    clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
    key: '<path d="M9.5 14.5 6 18v2h2l1.5-1.5L11 20l2-2-1.5-1.5 2.2-2.2"/><circle cx="16.5" cy="8.5" r="4.5"/>',
    history: '<path d="M8 6h11M8 12h11M8 18h11"/><circle cx="4" cy="6" r="1"/><circle cx="4" cy="12" r="1"/><circle cx="4" cy="18" r="1"/>',
    info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v6M12 7h.01"/>',
    shield: '<path d="M12 3 19 6v5c0 4.5-3 7.7-7 10-4-2.3-7-5.5-7-10V6l7-3Z"/><path d="m8.8 12.2 2 2 4.4-4.4"/>',
    save: '<path d="M5 4h11l3 3v13H5V4Z"/><path d="M8 4v6h7V4M8 20v-6h8v6"/>'
  };

  function shellSVG(name) {
    const paths = shellIconPaths[name] || shellIconPaths.overview;
    return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths}</svg>`;
  }

  function injectStyles() {
    if ((document.documentElement.dataset.freenetStyleaccepteduxjs1 === "1")) return;
    document.documentElement.dataset.freenetStyleaccepteduxjs1 = "1";
  }

  function decorateTopbarFact(fact) {
    if (!fact || fact.dataset.freenetShell === '1') return;
    const directChildren = Array.from(fact.children);
    const label = directChildren.find(node => node.tagName === 'SPAN' && !node.classList.contains('fn-top-fact-icon'));
    const value = directChildren.find(node => node.tagName === 'STRONG');
    if (!label || !value) return;
    fact.dataset.freenetShell = '1';
    fact.classList.add('fn-shell-fact');
    const icon = fact.querySelector(':scope > .fn-top-fact-icon');
    const copy = document.createElement('span');
    copy.className = 'fn-shell-fact-copy';
    copy.append(label, value);
    if (icon) fact.append(icon, copy);
    else fact.append(copy);
  }

  function normalizeTopbarProductCopy() {
    const summary = qs('#overviewApprovedTop');
    if (!summary) return;
    summary.querySelectorAll('.overview-approved-fact.fn-shell-fact').forEach(fact => {
      const label = fact.querySelector('.fn-shell-fact-copy>span');
      const value = fact.querySelector('.fn-shell-fact-copy>strong');
      if (!label || !value || label.textContent.trim().toUpperCase() !== 'DNS') return;
      fact.classList.add('fn-shell-dns');
      const raw = value.textContent.trim();
      if (/xkeen\s*\/\s*xray/i.test(raw) || /^Раздельный(?: DNS)?$/i.test(raw)) value.textContent = 'Раздельный';
      else if (/^Прямой$/i.test(raw) || /^DNS напрямую(?: через роутер)?$/i.test(raw) || /^Штатный DNS роутера$/i.test(raw) || /^DNS через роутер$/i.test(raw) || /^Через роутер$/i.test(raw)) value.textContent = 'Прямой';
    });
  }

  function mountBrandWordmark(brand) {
    if (!brand) return;
    brand.classList.add('fn-brand-lockup');
    if (brand.querySelector(':scope > .fn-brand-wordmark')) return;
    const wordmark = document.createElement('span');
    wordmark.className = 'fn-brand-wordmark';
    wordmark.setAttribute('aria-label', 'FreeNet');
    wordmark.innerHTML = '<span class="fn-brand-free" aria-hidden="true">Free</span><span class="fn-brand-net" aria-hidden="true">Net</span>';
    brand.replaceChildren(wordmark);
  }

  function mountShellChrome() {
    const xkeen = qs('#topXkeenLink');
    if (xkeen) {
      xkeen.hidden = true;
      xkeen.tabIndex = -1;
      xkeen.setAttribute('aria-hidden', 'true');
    }

    const summary = qs('#overviewApprovedTop');
    if (summary) {
      summary.classList.add('fn-shell-summary');
      const facts = summary.querySelectorAll('.overview-approved-fact');
      decorateTopbarFact(facts[0]);
      decorateTopbarFact(facts[1]);
      const update = qs('#topFreenetUpdate');
      if (update && update.parentNode !== summary) summary.appendChild(update);
      normalizeTopbarProductCopy();
    }

    mountBrandWordmark(qs('.sidebar>.brand'));
    mountBrandWordmark(qs('#authSection .brand'));

    document.querySelectorAll('.nav-btn[data-page]').forEach(button => {
      const icon = button.querySelector('.nav-icon');
      if (!icon || icon.dataset.freenetShell === '1') return;
      icon.dataset.freenetShell = '1';
      icon.innerHTML = shellSVG(button.dataset.page || 'overview');
    });
  }

  document.addEventListener('freenet:controls-busy', () => queueMicrotask(normalizeTopbarProductCopy));
  document.addEventListener('freenet:settings-v3-updated', () => queueMicrotask(normalizeTopbarProductCopy));

  function mountRememberMe() {
    const card = qs('#authSection .auth-card');
    if (!card) return;
    if (!qs('#authRememberRow')) {
      const row = document.createElement('label');
      row.id = 'authRememberRow';
      row.className = 'fn-remember';
      row.innerHTML = '<input id="authRemember" type="checkbox"><span>Запомнить меня на этом устройстве</span>';
      const actions = qs('.auth-actions', card);
      if (actions) card.insertBefore(row, actions);
      else card.appendChild(row);
    }
    if (authFetchWrapped) return;
    authFetchWrapped = true;
    const previousFetch = window.fetch.bind(window);
    window.fetch = function(input, init) {
      const url = typeof input === 'string' ? input : (input && input.url) || '';
      let path = url;
      try { path = new URL(url, location.href).pathname; } catch (_) {}
      const method = String((init && init.method) || (input && input.method) || 'GET').toUpperCase();
      if (path.startsWith('/api/auth/')) {
        init = Object.assign({}, init || {}, {credentials: 'include'});
      }
      if ((path === '/api/auth/login' || path === '/api/auth/setup') && method === 'POST' && init && typeof init.body === 'string') {
        try {
          const body = JSON.parse(init.body);
          body.remember = !!qs('#authRemember')?.checked;
          init = Object.assign({}, init, {body: JSON.stringify(body)});
        } catch (_) {}
      }
      return previousFetch(input, init);
    };
  }

  function subscriptionConfigured() {
    return !!(lastStatus && lastStatus.subscription_configured === true);
  }

  function syncSubscriptionPage() {
    if (!subscriptionMounted) return;
    const state = qs('#subscriptionState');
    const count = qs('#extraCount');
    const input = qs('#subscriptionInput');
    const configured = subscriptionConfigured();
    if (state) {
      state.textContent = configured ? 'Активна' : 'Не настроена';
      state.classList.toggle('ok', configured);
    }
    if (input) input.placeholder = configured ? 'https://••••••••••••••••••••' : 'Вставьте HTTPS key-link';
    const infoState = qs('#fnSubscriptionInfoState');
    if (infoState) {
      infoState.textContent = configured ? 'Активна' : 'Не настроена';
      infoState.classList.toggle('ok', configured);
    }
    const infoCount = qs('#fnSubscriptionInfoCount');
    if (infoCount) infoCount.textContent = count?.textContent || '—';
  }

  function setSubscriptionNotice(text, tone = '') {
    const notice = qs('#subscriptionNotice');
    if (!notice) return;
    notice.textContent = text || '';
    notice.className = text ? 'notice show' + (tone ? ' ' + tone : '') : 'notice';
  }

  function applySubscriptionActionCatalog(actionData) {
    const profiles = Array.isArray(actionData && actionData.profiles) ? actionData.profiles : [];
    if (profiles.length && typeof window.renderExtraProfiles === 'function') {
      window.renderExtraProfiles({
        extra_profiles: profiles,
        profiles_stale: !!actionData.profiles_stale,
        profiles_error: actionData.profiles_stale ? 'Используется последний успешный список Extra-профилей.' : ''
      });
    }
    return Number(actionData && actionData.profiles_available) || profiles.length || 0;
  }

  async function refreshSubscriptionScheduleState() {
    try {
      const stateResponse = await fetch('/api/settings-v3', {cache:'no-store'});
      const stateData = await stateResponse.json();
      if (stateResponse.ok && stateData && stateData.success !== false) {
        document.dispatchEvent(new CustomEvent('freenet:settings-v3-updated', {detail:stateData}));
      }
    } catch (_) {}
  }

  async function checkSubscription(label = 'Проверка подписки') {
    setSubscriptionNotice('Проверяем подписку и список Extra-профилей…');
    try {
      const response = await fetch('/api/settings-v3/action', {
        method:'POST',
        headers:{'Content-Type':'application/json'},
        body:JSON.stringify({action:'subscription_check'})
      });
      const actionData = await response.json().catch(() => ({}));
      const count = applySubscriptionActionCatalog(actionData);
      if (!response.ok || actionData.success === false) throw new Error(actionData.error || 'subscription check failed');
      const shown = count > 0 ? String(count) : '—';
      setSubscriptionNotice(`Подписка доступна. Зарубежных Extra-профилей: ${shown}.`, 'ok');
    } catch (_) {
      setSubscriptionNotice('Свежий список получить не удалось. Рабочее VPN-подключение не изменено; последний успешный список сохранён.', 'bad');
    }
    await refreshSubscriptionScheduleState();
    syncSubscriptionPage();
  }

  window.refreshSubscriptionCatalog = checkSubscription;

  function bindSubscriptionActions(saveButton, refreshButton) {
    const checkButton = qs('#checkSubscriptionBtn');
    if (checkButton && checkButton.dataset.freenetSubscriptionBound !== '1') {
      checkButton.dataset.freenetSubscriptionBound = '1';
      checkButton.addEventListener('click', () => checkSubscription());
    }
    if (refreshButton && refreshButton.dataset.freenetSubscriptionBound !== '1') {
      refreshButton.dataset.freenetSubscriptionBound = '1';
      refreshButton.addEventListener('click', async event => {
        event.preventDefault();
        event.stopImmediatePropagation();
        if (refreshButton.disabled) return;
        await checkSubscription('Обновление списка');
      }, true);
    }
    const journalButton = qs('#fnSubscriptionJournalBtn');
    if (journalButton && journalButton.dataset.freenetSubscriptionBound !== '1') {
      journalButton.dataset.freenetSubscriptionBound = '1';
      journalButton.addEventListener('click', () => {
        if (typeof window.openFreeNetJournal === 'function') window.openFreeNetJournal('subscription');
        else if (typeof window.setPage === 'function') window.setPage('journal');
      });
    }
    if (saveButton && saveButton.dataset.freenetSubscriptionBound !== '1') {
      saveButton.dataset.freenetSubscriptionBound = '1';
      saveButton.addEventListener('click', async event => {
        event.preventDefault();
        event.stopImmediatePropagation();
        if (saveButton.disabled || typeof window.saveSubscription !== 'function') return;
        await window.saveSubscription();
        syncSubscriptionPage();
      }, true);
    }
  }

  function mountSubscriptionPage() {
    if (subscriptionMounted) return;
    const page = qs('[data-page-view="subscription"]');
    if (!page) return;
    const summary = qs('#subscriptionSummary', page);
    const state = qs('#subscriptionState', page);
    const count = qs('#extraCount', page);
    const updater = qs('#subscriptionUpdaterState', page);
    const input = qs('#subscriptionInput', page);
    const save = qs('#saveSubscriptionBtn', page);
    const refresh = qs('#refreshProfilesBtn', page);
    const notice = qs('#subscriptionNotice', page);
    if (![summary,state,count,updater,input,save,refresh,notice].every(Boolean)) return;

    subscriptionMounted = true;
    page.classList.add('subscription-approved');
    page.innerHTML = `
      <div class="page-head"><div><h1>Подписка</h1><p>Управление ключом и списком Extra-профилей.</p></div><div id="fnSubscriptionSummaryMount"></div></div>
      <div class="fn-sub-stats">
        <section class="fn-sub-stat fn-sub-state-card"><span class="fn-sub-stat-icon" aria-hidden="true">${shellSVG('subscription')}</span><div class="fn-sub-stat-copy"><span class="fn-sub-label">Статус подписки</span><div id="fnSubscriptionStateMount"></div><div class="fn-sub-provider"><span>VPN-провайдер</span>BlancVPN</div></div></section>
        <section class="fn-sub-stat"><span class="fn-sub-stat-icon">${shellSVG('profiles')}</span><div class="fn-sub-stat-copy"><span class="fn-sub-label">Extra-профили</span><div id="fnSubscriptionCountMount"></div><div class="fn-sub-meta">доступно</div></div></section>
        <section class="fn-sub-stat"><span class="fn-sub-stat-icon">${shellSVG('refresh')}</span><div class="fn-sub-stat-copy"><span class="fn-sub-label">Обновление</span><div id="fnSubscriptionUpdaterMount"></div><div id="fnSubscriptionLastAction" class="fn-sub-last"><span>Последнее обновление</span><strong>Нет данных</strong></div></div></section>
        <section class="fn-sub-stat fn-sub-next"><span class="fn-sub-stat-icon">${shellSVG('clock')}</span><div class="fn-sub-stat-copy"><span class="fn-sub-label">Следующее обновление</span><div class="fn-sub-value small">Вручную</div><div class="fn-sub-meta">AUTO VPN будет настраиваться отдельно</div><div id="fnSubscriptionRefreshMount"></div></div></section>
      </div>
      <section class="fn-sub-key">
        <div class="fn-sub-key-main"><div class="fn-sub-heading"><span class="fn-sub-section-icon">${shellSVG('key')}</span><h2>Ключ-подписка</h2></div><div class="fn-sub-input-wrap"><span class="fn-sub-input-icon">${shellSVG('key')}</span><div id="fnSubscriptionInputMount" style="display:contents"></div></div><div class="fn-sub-key-note">${shellSVG('shield')}<span>Ключ хранится только локально на роутере. Сохранённое значение никогда не выводится обратно в Control Center, API или журнал.</span></div></div>
        <div class="fn-sub-key-actions"><div id="fnSubscriptionSaveMount"></div><button id="checkSubscriptionBtn" class="btn secondary" type="button">${shellSVG('shield')}<span>Проверить подписку</span></button></div>
        <div id="fnSubscriptionNoticeMount" style="display:contents"></div>
      </section>
      <div class="fn-sub-lower">
        <section class="fn-sub-history"><div class="fn-sub-heading"><span class="fn-sub-section-icon">${shellSVG('history')}</span><div><h2>Общий журнал</h2><div class="fn-sub-meta">Ручные и плановые проверки подписки вместе с остальными событиями FreeNet</div></div></div><p class="fn-sub-meta">История хранится на роутере и одинакова для всех браузеров.</p><button id="fnSubscriptionJournalBtn" class="btn secondary fn-sub-history-toggle" type="button">Открыть журнал подписки</button></section>
        <section class="fn-sub-info"><div class="fn-sub-heading"><span class="fn-sub-section-icon">${shellSVG('info')}</span><h2>Информация</h2></div><dl class="fn-sub-info-grid"><dt>VPN-провайдер</dt><dd>BlancVPN</dd><dt>Тип подписки</dt><dd>Extra-профили</dd><dt>Доступно профилей</dt><dd id="fnSubscriptionInfoCount">—</dd><dt>Статус</dt><dd id="fnSubscriptionInfoState">—</dd></dl></section>
      </div>`;

    qs('#fnSubscriptionSummaryMount', page).appendChild(summary);
    summary.hidden = true;
    state.className = 'fn-sub-value small';
    qs('#fnSubscriptionStateMount', page).appendChild(state);
    count.className = 'fn-sub-value';
    qs('#fnSubscriptionCountMount', page).appendChild(count);
    updater.className = 'fn-sub-value small';
    qs('#fnSubscriptionUpdaterMount', page).appendChild(updater);
    input.type = 'password';
    input.autocomplete = 'new-password';
    input.spellcheck = false;
    input.removeAttribute('value');
    qs('#fnSubscriptionInputMount', page).appendChild(input);
    save.className = 'btn primary';
    save.innerHTML = `${shellSVG('save')}<span>Сохранить изменения</span>`;
    qs('#fnSubscriptionSaveMount', page).appendChild(save);
    refresh.className = 'btn secondary';
    refresh.innerHTML = `${shellSVG('refresh')}<span>Обновить сейчас</span>`;
    qs('#fnSubscriptionRefreshMount', page).appendChild(refresh);
    qs('#fnSubscriptionNoticeMount', page).appendChild(notice);

    bindSubscriptionActions(save, refresh);
    syncSubscriptionPage();
    document.addEventListener('freenet:controls-busy', syncSubscriptionPage);
  }

  // Move the existing credential-bearing form and its original event handlers.
  // Do not clone a subscription key, create a second save path, or expose it in state.
  function relocateSubscriptionToSettings() {
    if (!subscriptionMounted) return false;
    const host = qs('#fn3SubscriptionMount');
    const legacy = qs('[data-page-view="subscription"]');
    if (!host || !legacy) return false;
    if (host.dataset.subscriptionIntegrated === '1') return true;
    const mounts = ['fn3SubStatusMount','fn3SubCountMount','fn3SubUpdateMount','fn3SubKeyMount','fn3SubJournalMount'].map(id => qs('#' + id, host));
    const [status, count, updater, keySection, journal] = [
      qs('#subscriptionState'), qs('#extraCount'), qs('#subscriptionUpdaterState'),
      qs('.fn-sub-key', legacy), qs('#fnSubscriptionJournalBtn')
    ];
    if (mounts.some(node => !node) || [status,count,updater,keySection,journal].some(node => !node)) return false;
    mounts[0].appendChild(status);
    mounts[1].appendChild(count);
    mounts[2].appendChild(updater);
    mounts[3].appendChild(keySection);
    mounts[4].appendChild(journal);
    host.dataset.subscriptionIntegrated = '1';
    syncSubscriptionPage();
    return true;
  }

  function ensureSubscriptionSettingsMount() {
    if (relocateSubscriptionToSettings()) return;
    // Both modules can mount in either order. Use the canonical Settings
    // lifecycle signal and the existing data signal, never DOM polling.
    document.addEventListener('freenet:settings-v3-mounted', relocateSubscriptionToSettings);
    document.addEventListener('freenet:settings-v3-updated', relocateSubscriptionToSettings);
    queueMicrotask(relocateSubscriptionToSettings);
  }

  function ensurePopover() {
    let root = qs('#freenetUpdatePopover');
    if (root) return root;
    root = document.createElement('section');
    root.id = 'freenetUpdatePopover';
    root.className = 'fn-update-popover';
    root.hidden = true;
    root.setAttribute('role', 'dialog');
    root.setAttribute('aria-label', 'Обновление FreeNet');
    root.innerHTML = `
      <div class="fn-update-arrow"></div>
      <div class="fn-update-head"><div class="fn-update-title"><span class="fn-update-badge">↑</span><span>Обновление FreeNet</span></div><button id="fnUpdateClose" class="fn-update-close" type="button" aria-label="Закрыть">×</button></div>
      <div class="fn-update-versions"><span>Текущая версия:</span><strong id="fnUpdateCurrent">—</strong><span>Доступна новая версия:</span><strong id="fnUpdateLatest" class="available">проверяем…</strong></div>
      <p class="fn-update-copy">Проверка и обновление выполняются прямо здесь, без отдельной страницы.</p>
      <div class="fn-update-notes"><b id="fnUpdateNotesTitle">Что нового</b><span id="fnUpdateNotes">Проверяем описание релиза…</span></div>
      <div id="fnUpdateStatus" class="fn-update-status">Нажмите «Проверить», чтобы сверить последний опубликованный релиз.</div>
      <progress id="fnUpdateProgress" class="fn-update-progress" hidden></progress>
      <div class="fn-update-actions"><button id="fnUpdateCheck" class="btn secondary" type="button">Проверить</button><button id="fnUpdateApply" class="btn fn-update-apply" type="button" disabled>Обновить</button></div>`;
    document.body.appendChild(root);
    qs('#fnUpdateClose').addEventListener('click', () => { if (!updatePolling) root.hidden = true; });
    qs('#fnUpdateCheck').addEventListener('click', checkUpdate);
    qs('#fnUpdateApply').addEventListener('click', startUpdate);
    return root;
  }

  function positionPopover() {
    const control = qs('#topFreenetUpdate');
    const popover = ensurePopover();
    if (!control || popover.hidden) return;
    const rect = control.getBoundingClientRect();
    const width = Math.min(350, window.innerWidth - 24);
    let left = rect.right - width;
    if (left < 12) left = 12;
    if (left + width > window.innerWidth - 12) left = window.innerWidth - width - 12;
    popover.style.left = `${left}px`;
    popover.style.top = `${rect.bottom + 8}px`;
  }

  function setStatus(text, tone = '') {
    const node = qs('#fnUpdateStatus');
    if (!node) return;
    node.textContent = text || '';
    node.className = 'fn-update-status' + (tone ? ' ' + tone : '');
  }

  function setProgress(active) {
    const progress = qs('#fnUpdateProgress');
    if (!progress) return;
    progress.hidden = !active;
    if (active) progress.removeAttribute('value');
  }

  function setBusy(busy) {
    const check = qs('#fnUpdateCheck');
    const apply = qs('#fnUpdateApply');
    if (check) check.disabled = busy;
    if (apply) apply.disabled = busy || !(updatePlan && updatePlan.success && updatePlan.ready && updatePlan.update_available && updatePlan.target_tag);
  }

  function renderPlan(value) {
    updatePlan = value;
    qs('#fnUpdateCurrent').textContent = value.current_version || '—';
    const latest = qs('#fnUpdateLatest');
    latest.textContent = value.update_available ? (value.latest_version || value.target_tag || 'доступна') : 'не требуется';
    latest.className = value.update_available ? 'available' : '';
    const version = value.latest_version || value.target_tag || value.current_version || 'релиз';
    const title = qs('#fnUpdateNotesTitle');
    const notes = qs('#fnUpdateNotes');
    if (title) title.textContent = value.update_available ? `Что нового в ${version}` : `Что вошло в ${version}`;
    if (notes) {
      const items = String(value.release_notes || '').split(' | ').map(item => item.trim()).filter(Boolean);
      notes.textContent = items.length ? items.map(item => `• ${item}`).join('\n') : 'Для этого релиза отдельное описание не опубликовано.';
    }
    qs('#fnUpdateApply').disabled = !(value.success && value.ready && value.update_available && value.target_tag);
    qs('#topFreenetUpdate')?.classList.toggle('update-available', !!value.update_available);
  }

  async function checkUpdate() {
    const root = ensurePopover();
    root.hidden = false;
    positionPopover();
    setBusy(true);
    setProgress(true);
    setStatus('Проверяем последний опубликованный релиз FreeNet…');
    try {
      const response = await fetch('/api/system/update/plan', {cache: 'no-store'});
      const value = await response.json();
      if (!response.ok || !value.success) throw new Error(value.error || 'Не удалось проверить обновление');
      renderPlan(value);
      setStatus(value.update_available ? `Доступно ${value.latest_version || value.target_tag}. SHA-256: ${value.manifest_verified ? 'проверен' : 'не подтверждён'}.` : 'Установлена актуальная версия FreeNet.', value.manifest_verified || !value.update_available ? 'ok' : '');
    } catch (error) {
      setStatus(error?.message || 'Не удалось проверить обновление.', 'bad');
    } finally {
      setProgress(false);
      setBusy(false);
    }
  }

  async function waitForVersion(target) {
    setStatus(`FreeNet перезапускается. Ждём подтверждения версии ${target}…`);
    for (let i = 0; i < 90; i++) {
      try {
        const response = await fetch('/versionz', {cache: 'no-store', signal: AbortSignal.timeout(5000)});
        const version = (await response.text()).trim();
        if (response.ok && version === target) {
          setStatus(`FreeNet ${target} установлен и подтверждён. Перезагружаем интерфейс…`, 'ok');
          setTimeout(() => location.reload(), 800);
          return;
        }
      } catch (_) {}
      await wait(1000);
    }
    setStatus(`Целевая версия ${target} не подтверждена. Не запускайте обновление повторно до проверки фактического состояния.`, 'bad');
  }

  async function targetVersionIsLive(target) {
    const expected = String(target || '').trim();
    if (!expected) return false;
    try {
      const response = await fetch('/versionz', {cache: 'no-store', signal: AbortSignal.timeout(5000)});
      if (!response.ok) return false;
      return (await response.text()).trim() === expected;
    } catch (_) {
      return false;
    }
  }

  async function pollUpdate(target) {
    const started = Date.now();
    const reconcileAfter = started + 5 * 60 * 1000;
    const hardDeadline = started + 10 * 60 * 1000;
    while (updatePolling && Date.now() < hardDeadline) {
      if (Date.now() >= reconcileAfter && await targetVersionIsLive(target)) {
        updatePolling = false;
        setStatus('Целевая версия уже запущена. Перезагружаем интерфейс без повторного запуска update…', 'ok');
        setTimeout(() => location.reload(), 250);
        return;
      }
      try {
        const response = await fetch('/api/system/update/state', {cache: 'no-store', signal: AbortSignal.timeout(10000)});
        if (response.status === 401) {
          updatePolling = false;
          await waitForVersion(target);
          return;
        }
        if (response.ok) {
          const state = await response.json();
          const lines = [state.message || state.state || 'Обновление выполняется', state.primary_error ? `Основная ошибка: ${state.primary_error}` : '', state.rollback_state ? `Откат: ${state.rollback_state}` : ''].filter(Boolean);
          const terminalBad = state.state === 'FAILED' || state.state === 'ROLLBACK_FAILED';
          setStatus(lines.join('\n'), state.state === 'SUCCESS' ? 'ok' : terminalBad ? 'bad' : '');
          if (state.state === 'SUCCESS') {
            updatePolling = false;
            await waitForVersion(target);
            return;
          }
          if (terminalBad) {
            updatePolling = false;
            setProgress(false);
            setBusy(false);
            return;
          }
        }
      } catch (_) {
        setStatus('FreeNet кратко недоступен во время перезапуска. Ожидаем возвращения Control Center…');
      }
      await wait(1400);
    }
    if (await targetVersionIsLive(target)) {
      updatePolling = false;
      setStatus('Целевая версия уже запущена. Перезагружаем интерфейс без повторного запуска update…', 'ok');
      setTimeout(() => location.reload(), 250);
      return;
    }
    updatePolling = false;
    setProgress(false);
    setBusy(false);
    setStatus('Результат обновления пока неизвестен. FreeNet не подтвердил целевую версию за десять минут; повторный запуск update не выполняйте до проверки состояния.', 'bad');
  }

  async function startUpdate() {
    if (!updatePlan || !updatePlan.success || !updatePlan.ready || !updatePlan.update_available || !updatePlan.target_tag || updatePolling) return;
    updatePolling = true;
    setBusy(true);
    setProgress(true);
    setStatus(`Устанавливаем ${updatePlan.target_tag}: создаём backup, проверяем staging и применяем обновление…`);
    try {
      const response = await fetch('/api/system/update/apply', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({target_tag: updatePlan.target_tag})});
      const result = await response.json();
      if (!response.ok || !result.success) throw new Error(result.error || 'Не удалось запустить обновление');
      await pollUpdate(updatePlan.target_tag);
    } catch (error) {
      updatePolling = false;
      setProgress(false);
      setBusy(false);
      setStatus(error?.message || 'Обновление не запущено.', 'bad');
    }
  }

  function openPopover() {
    const root = ensurePopover();
    root.hidden = false;
    positionPopover();
    const versionText = qs('#topFreenetUpdate .fn-version-copy strong')?.textContent || qs('#version')?.textContent || '—';
    if (qs('#fnUpdateCurrent').textContent === '—') qs('#fnUpdateCurrent').textContent = String(versionText).replace(/^FreeNet UI\s*/i, '');
    checkUpdate();
  }

  function bindUpdateControl() {
    if (document.documentElement.dataset.freenetAcceptedUpdateBound === '1') return;
    document.documentElement.dataset.freenetAcceptedUpdateBound = '1';
    document.addEventListener('click', event => {
      const control = event.target?.closest?.('#topFreenetUpdate');
      if (!control) return;
      if (control.dataset.freenetVersionManager === '1') return;
      event.preventDefault();
      event.stopImmediatePropagation();
      openPopover();
    }, true);
    document.addEventListener('click', event => {
      const root = qs('#freenetUpdatePopover');
      if (!root || root.hidden || updatePolling) return;
      const control = event.target?.closest?.('#topFreenetUpdate');
      if (control || root.contains(event.target)) return;
      root.hidden = true;
    });
    window.addEventListener('resize', positionPopover);
    window.addEventListener('scroll', positionPopover, true);
  }

  function mount() {
    injectStyles();
    mountShellChrome();
    requestAnimationFrame(mountShellChrome);
    mountRememberMe();
    mountSubscriptionPage();
    ensureSubscriptionSettingsMount();
    ensurePopover();
    bindUpdateControl();
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mount, {once: true});
  else mount();
})();

// Final shell polish: deterministic vector brand + presentation-only VPN selector cleanup.
(() => {
  const q = (selector, root = document) => root.querySelector(selector);

  const brandLockupSVG = `
    <svg viewBox="0 0 140 28" role="img" aria-label="FreeNet" focusable="false">
      <g fill="none" stroke="#72a8ff" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
        <path d="M14 2.8 25.2 14 14 25.2 2.8 14 14 2.8Z"/>
        <path d="M9.7 18.2V9.8l8.6 8.4V9.8"/>
      </g>
      <g fill="none" stroke="#f7f9ff" stroke-width="2.75" stroke-linecap="round" stroke-linejoin="round">
        <path d="M34 5v18M34 5h12M34 13h9.5"/>
        <path d="M51 11.5V23M51 16c1.2-3.1 3.3-4.6 6.5-4.6"/>
        <path d="M72.5 18H61.8c.4-4.5 2.5-7 5.6-7 3.2 0 5.2 2.4 5.2 6.2H62c.5 3.8 2.7 5.8 5.9 5.8 1.9 0 3.3-.5 4.6-1.5"/>
        <path d="M88.5 18H77.8c.4-4.5 2.5-7 5.6-7 3.2 0 5.2 2.4 5.2 6.2H78c.5 3.8 2.7 5.8 5.9 5.8 1.9 0 3.3-.5 4.6-1.5"/>
      </g>
      <g fill="none" stroke="#7baeff" stroke-width="2.75" stroke-linecap="round" stroke-linejoin="round">
        <path d="M95 23V5l12 18V5"/>
        <path d="M122.5 18h-10.7c.4-4.5 2.5-7 5.6-7 3.2 0 5.2 2.4 5.2 6.2H112c.5 3.8 2.7 5.8 5.9 5.8 1.9 0 3.3-.5 4.6-1.5"/>
        <path d="M132 7v11.8c0 2.8 1.5 4.2 4.5 4.2M127.5 12h10"/>
      </g>
    </svg>`;

  function installFinalPolishStyles() {
    if (q('#freenetFinalShellPolishStyles')) return;
    document.documentElement.dataset.freenetStyleaccepteduxjs2 = "1";
  }

  function mountVectorBrand(brand) {
    if (!brand) return;
    const node = document.createElement('span');
    node.className = 'fn-brand-wordmark fn-brand-lockup-svg';
    node.innerHTML = brandLockupSVG;
    brand.classList.add('fn-brand-lockup');
    brand.setAttribute('aria-label', 'FreeNet');
    brand.replaceChildren(node);
  }

  function cleanProfileLabel(value) {
    let text = String(value || '').trim();
    text = text.replace(/^[\u{1F1E6}-\u{1F1FF}]{2}\s*/u, '');
    text = text.replace(/^[A-Za-z]{2}\s+/, '');
    return text.trim();
  }

  function polishProfilePresentation() {
    const trigger = q('#profilesTriggerText');
    if (trigger) {
      let hasSelection = false;
      try { hasSelection = !!selectedProviderID; } catch (_) {}
      trigger.textContent = hasSelection ? (cleanProfileLabel(trigger.textContent) || 'Выбрать VPN-сервер') : 'Выбрать VPN-сервер';
    }
    document.querySelectorAll('#profilesMenu .profile-option-main').forEach(node => {
      node.textContent = cleanProfileLabel(node.textContent) || 'VPN-сервер';
    });
    const selectedTitle = q('#selectedProfileCard strong');
    if (selectedTitle) {
      selectedTitle.textContent = selectedTitle.textContent.replace(/(^[^:]+:\s*)([A-Za-z]{2}\s+)/, '$1');
    }
  }

  function patchProfileRenderer() {
    try {
      if (typeof renderProfileOptions === 'function' && !renderProfileOptions.__freenetShellPolish) {
        const previous = renderProfileOptions;
        const wrapped = function() {
          const result = previous.apply(this, arguments);
          polishProfilePresentation();
          return result;
        };
        wrapped.__freenetShellPolish = true;
        renderProfileOptions = wrapped;
      }
    } catch (_) {}
    const trigger = q('#profilesTrigger');
    if (trigger && trigger.dataset.freenetShellPolish !== '1') {
      trigger.dataset.freenetShellPolish = '1';
      trigger.addEventListener('click', () => queueMicrotask(polishProfilePresentation));
    }
    const search = q('#profileSearch');
    if (search && search.dataset.freenetShellPolish !== '1') {
      search.dataset.freenetShellPolish = '1';
      search.addEventListener('input', () => queueMicrotask(polishProfilePresentation));
    }
    polishProfilePresentation();
  }

  function installFinalShellPolish() {
    installFinalPolishStyles();
    mountVectorBrand(q('.sidebar>.brand'));
    mountVectorBrand(q('#authSection .brand'));
    patchProfileRenderer();
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', installFinalShellPolish, {once: true});
  else installFinalShellPolish();
  requestAnimationFrame(installFinalShellPolish);
})();

// Selector metadata only. Flag pixels are owned exclusively by window.FreeNetFlags.
(() => {
  if (!(document.documentElement.dataset.freenetStyleaccepteduxjs3 === "1")) {
    document.documentElement.dataset.freenetStyleaccepteduxjs3 = "1";
  }
})();

