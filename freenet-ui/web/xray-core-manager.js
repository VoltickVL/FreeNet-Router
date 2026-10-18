(() => {
  'use strict';

  const qs = (s, root = document) => root.querySelector(s);
  let catalog = null;
  let serviceState = null;
  let selectedVersion = '';
  let applying = false;
  let topbarVersionLoading = false;
  let topbarVersionAttempts = 0;
  let topbarRetryTimer = null;
  let topbarReadController = null;
  let topbarReadGeneration = 0;
  const topbarMaxAttempts = 4;
  let serviceMutationUnknown = false;

  

  function rootMarkup() {
    const root = document.createElement('div');
    root.id = 'xrayCoreManager';
    root.className = 'xcm-root';
    root.hidden = true;
    root.innerHTML = `
      <div class="xcm-backdrop"></div>
      <section class="xcm-modal" role="dialog" aria-modal="false" aria-labelledby="xcmTitle" tabindex="-1">
        <div class="xcm-head"><div><div class="xcm-kicker">Xray</div><h3 id="xcmTitle">Версия Xray Core</h3><div class="xcm-subtitle">Выбор версии, затем проверка и установка</div></div><button id="xcmClose" class="xcm-close" type="button" aria-label="Закрыть">×</button></div>
        <div id="xcmBody"></div>
        <div id="xcmActions" class="xcm-actions"></div>
      </section>`;
    document.body.appendChild(root);
    qs('#xcmClose', root).addEventListener('click', closeManager);
    return root;
  }

  function ensureRoot() { return qs('#xrayCoreManager') || rootMarkup(); }

  function topbarIcon() {
    return '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 3 20 8v8l-8 5-8-5V8l8-5Z"/><path d="m8.5 9.5 7 5m0-5-7 5"/></svg>';
  }

  function syncTopbarVersion(value) {
    const node = qs('#xrayTopbarVersion');
    if (!node) return;
    node.textContent = compactVersion(value) || String(value || '').trim() || 'версия…';
  }

  function syncTopbarService(result) {
    const chip = qs('#xrayTopbarChip');
    const offline = result && result.success && result.online === false;
    chip?.classList.toggle('offline', !!offline);
    if (offline) {
      syncTopbarVersion('остановлен');
      return;
    }
    syncTopbarVersion(result?.version || result?.current_version || '');
  }

  function syncServiceSurfaces(result) {
    if (!result || result.success === false) return;
    serviceState = result;
    syncTopbarService(result);
    syncConfigStudioService(result);
    document.dispatchEvent(new CustomEvent('freenet:xray-service-changed', {detail:result}));
  }

  function mountTopbarChip() {
    const topbar = qs('.topbar.overview-approved') || qs('.topbar');
    if (!topbar) return null;
    let chip = qs('#xrayTopbarChip');
    if (!chip) {
      chip = document.createElement('button');
      chip.id = 'xrayTopbarChip';
      chip.type = 'button';
      chip.className = 'fn-xray-topbar';
      chip.title = 'Управление Xray';
      chip.setAttribute('aria-haspopup', 'dialog');
      chip.setAttribute('aria-controls', 'xrayCoreManager');
      chip.setAttribute('aria-expanded', 'false');
      chip.innerHTML = `<span class="fn-xray-topbar-icon">${topbarIcon()}</span><span class="fn-xray-chip"><small>Xray</small><strong id="xrayTopbarVersion">версия…</strong></span>`;
      chip.addEventListener('click', event => { event.preventDefault(); openManager(); });
    }
    const facts = qs('#overviewApprovedTop');
    if (facts && chip.parentNode !== facts) facts.insertBefore(chip, facts.firstChild);
    else if (!facts && chip.parentNode !== topbar) {
      const actions = qs('.top-actions');
      topbar.insertBefore(chip, actions || null);
    }
    if (serviceState) syncTopbarService(serviceState);
    return chip;
  }

  function authenticatedTopbar() {
    if (typeof authAuthenticated === 'boolean') return authAuthenticated;
    return qs('#controlCenter')?.hidden === false;
  }

  function resetTopbarRead() {
    topbarReadGeneration += 1;
    clearTimeout(topbarRetryTimer);
    topbarRetryTimer = null;
    topbarReadController?.abort();
    topbarReadController = null;
    topbarVersionLoading = false;
    topbarVersionAttempts = 0;
  }

  async function refreshTopbarVersion() {
    if (!authenticatedTopbar() || topbarVersionLoading || topbarRetryTimer !== null || topbarVersionAttempts >= topbarMaxAttempts) return;
    const chip = mountTopbarChip();
    if (!chip) return; // The mount observer retries when the shell arrives.
    const generation = topbarReadGeneration;
    const controller = new AbortController();
    topbarReadController = controller;
    topbarVersionLoading = true;
    topbarVersionAttempts += 1;
    const timeout = setTimeout(() => controller.abort(), 5000);
    try {
      const response = await fetch('/api/xray/service', {cache:'no-store', signal:controller.signal});
      if (generation !== topbarReadGeneration || !authenticatedTopbar()) return;
      if (response.status === 401) {
        serviceState = null;
        syncTopbarVersion('вход требуется');
        return; // Authentication, not a timer, restarts the request budget.
      }
      const result = await response.json();
      if (generation !== topbarReadGeneration || !authenticatedTopbar()) return;
      if (!response.ok || !result?.success || (result.online !== false && !compactVersion(result.version || result.current_version))) {
        throw new Error('Xray status is not available');
      }
      chip.title = 'Управление Xray';
      // Background hydration never clears an unresolved mutation guard.
      syncServiceSurfaces(result);
    } catch (_) {
      if (generation !== topbarReadGeneration || !authenticatedTopbar()) return;
      if (!serviceState) syncTopbarVersion('недоступно');
      chip.title = 'Не удалось прочитать состояние Xray; это не означает остановку сервиса';
      if (topbarVersionAttempts < topbarMaxAttempts) {
        topbarRetryTimer = setTimeout(() => {
          topbarRetryTimer = null;
          refreshTopbarVersion();
        }, 700 * topbarVersionAttempts);
      }
    } finally {
      clearTimeout(timeout);
      if (generation === topbarReadGeneration && topbarReadController === controller) {
        topbarReadController = null;
        topbarVersionLoading = false;
      }
    }
  }

  function hydrateTopbar() {
    mountTopbarChip();
    if (!serviceState) refreshTopbarVersion();
  }

  function topbarAuthenticationChanged() {
    resetTopbarRead();
    serviceState = null;
    if (authenticatedTopbar()) {
      syncTopbarVersion('версия…');
      hydrateTopbar();
    } else {
      syncTopbarVersion('вход требуется');
    }
  }

  function resumeTopbarRead() {
    if (!authenticatedTopbar() || document.visibilityState === 'hidden') return;
    resetTopbarRead();
    refreshTopbarVersion();
  }

  function body() { return qs('#xcmBody', ensureRoot()); }
  function actions() { return qs('#xcmActions', ensureRoot()); }

  function setManagerHeading(title, subtitle) {
    const root = ensureRoot();
    const heading = qs('#xcmTitle', root);
    const sub = qs('.xcm-subtitle', root);
    if (heading) heading.textContent = title;
    if (sub) sub.textContent = subtitle;
  }

  function clearNode(node) { while (node.firstChild) node.removeChild(node.firstChild); }
  function addButton(parent, text, className, handler, disabled = false) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = `xcm-btn${className ? ' ' + className : ''}`;
    button.textContent = text;
    button.disabled = !!disabled;
    button.addEventListener('click', handler);
    parent.appendChild(button);
    return button;
  }

  function compactVersion(raw) {
    const match = String(raw || '').match(/\bv?\d+(?:\.\d+){1,3}(?:[-+][0-9A-Za-z.-]+)?/);
    if (!match) return '';
    return match[0].startsWith('v') ? match[0] : `v${match[0]}`;
  }

  function formatDate(raw) {
    const date = new Date(raw);
    if (Number.isNaN(date.getTime())) return '';
    return date.toLocaleDateString('ru-RU', {day:'2-digit', month:'2-digit', year:'numeric'});
  }

  function friendlyTime(raw) {
    const date = new Date(raw);
    if (Number.isNaN(date.getTime())) return String(raw || '');
    return date.toLocaleString('ru-RU', {day:'2-digit', month:'2-digit', hour:'2-digit', minute:'2-digit'});
  }

  function openJournal() {
    closeManager();
    const button = qs('[data-page="access"]');
    if (button) {
      button.click();
      return;
    }
    location.hash = '#access';
  }

  async function fetchServiceSnapshot() {
    const response = await fetch('/api/xray/service', {cache:'no-store'});
    let result = {};
    try { result = await response.json(); } catch (_) {}
    if (!response.ok || !result.success) throw new Error(result.error || 'Состояние Xray недоступно');
    serviceMutationUnknown = false;
    syncServiceSurfaces(result);
    return result;
  }

  let focusBeforeOpen = null;

  function positionManager() {
    const modal = qs('#xrayCoreManager .xcm-modal');
    const chip = qs('#xrayTopbarChip');
    if (!modal || !chip || qs('#xrayCoreManager')?.hidden) return;
    const vw = window.visualViewport?.width || window.innerWidth;
    const vh = window.visualViewport?.height || window.innerHeight;
    const rect = chip.getBoundingClientRect();
    const width = Math.min(540, vw - 24);
    modal.style.width = width + 'px';
    const left = Math.max(12, Math.min(vw - width - 12, rect.right - width));
    modal.style.left = left + 'px';
    const desiredTop = rect.bottom + 8;
    const modalHeight = Math.min(modal.scrollHeight || 650, Math.max(220, vh - 24));
    const top = Math.max(12, Math.min(desiredTop, vh - modalHeight - 12));
    modal.style.top = top + 'px';
  }

  function closeManager() {
    if (applying) return;
    const root = qs('#xrayCoreManager');
    if (!root || root.hidden) return;
    root.hidden = true;
    root.classList.remove('xcm-open');
    qs('#xrayTopbarChip')?.setAttribute('aria-expanded', 'false');
    if (focusBeforeOpen?.isConnected) focusBeforeOpen.focus({preventScroll:true});
    focusBeforeOpen = null;
  }

  function actionLabel(release) {
    if (!catalog || !release || release.current) return 'Установлено';
    const currentIndex = catalog.releases.findIndex(item => item.current);
    const targetIndex = catalog.releases.findIndex(item => item.version === release.version);
    if (currentIndex >= 0 && targetIndex >= 0) {
      if (targetIndex < currentIndex) return `Обновить до ${release.version}`;
      if (targetIndex > currentIndex) return `Откатить до ${release.version}`;
    }
    if (release.latest) return `Обновить до ${release.version}`;
    return `Установить ${release.version}`;
  }

  function renderCatalog() {
    setManagerHeading('Версия Xray Core', 'Выбор версии, затем проверка и установка');
    const target = body();
    const footer = actions();
    clearNode(target); clearNode(footer);

    const summary = document.createElement('div');
    summary.className = 'xcm-summary';
    const current = catalog.current_version || '—';
    const latest = catalog.latest_version || '—';
    syncTopbarVersion(catalog.current_version || '');
    summary.innerHTML = '<div>Установлена <b></b></div><div>Доступна <b></b></div>';
    const summaryValues = summary.querySelectorAll('b');
    summaryValues[0].textContent = current;
    summaryValues[1].textContent = latest;
    target.appendChild(summary);

    const search = document.createElement('input');
    search.type = 'search';
    search.className = 'xcm-search';
    search.autocomplete = 'off';
    search.placeholder = 'Найти версию Xray';
    search.setAttribute('aria-label', 'Поиск версии Xray');
    target.appendChild(search);

    const list = document.createElement('div');
    list.className = 'xcm-list';
    target.appendChild(list);

    const selection = document.createElement('div');
    selection.className = 'xcm-selection';
    target.appendChild(selection);

    const releases = Array.isArray(catalog.releases) ? catalog.releases : [];

    const renderFooter = () => {
      clearNode(footer);
      addButton(footer, 'Назад', '', () => renderServiceHome(serviceState));
      addButton(footer, 'Закрыть', '', closeManager);
      const selected = releases.find(item => item.version === selectedVersion);
      if (!selected) {
        selection.className = 'xcm-selection';
        selection.textContent = 'Выберите версию из списка.';
        return;
      }
      if (selected.current) {
        selection.className = 'xcm-selection ready';
        selection.textContent = `${selected.version} установлена сейчас.`;
        return;
      }
      if (!selected.asset?.available) {
        selection.className = 'xcm-selection bad';
        selection.textContent = 'Эта версия недоступна для архитектуры роутера.';
        return;
      }
      selection.className = 'xcm-selection ready';
      selection.textContent = `Выбрана ${selected.version}. После установки FreeNet автоматически проверит Xray и при ошибке выполнит откат.`;
      addButton(footer, actionLabel(selected), 'primary', () => applyRelease(selected));
    };

    const draw = () => {
      clearNode(list);
      const query = search.value.trim().toLowerCase();
      const filtered = releases.filter(release => !query || String(release.version || '').toLowerCase().includes(query));
      const visible = filtered.slice(0, 5);
      if (!visible.length) {
        const empty = document.createElement('div');
        empty.className = 'xcm-empty';
        empty.textContent = releases.length ? 'Версии по этому фильтру не найдены.' : 'Доступные версии Xray не найдены.';
        list.appendChild(empty);
        return;
      }
      visible.forEach(release => {
        const option = document.createElement('button');
        option.type = 'button';
        option.className = `xcm-release${release.version === selectedVersion ? ' selected' : ''}`;
        option.disabled = !release.asset?.available && !release.current;
        option.dataset.version = release.version || '';

        const main = document.createElement('div');
        main.className = 'xcm-release-main';
        const version = document.createElement('span');
        version.className = 'xcm-release-version';
        version.textContent = release.version || '—';
        main.appendChild(version);
        if (release.current) {
          const badge = document.createElement('span');
          badge.className = 'xcm-badge current';
          badge.textContent = 'Текущая';
          main.appendChild(badge);
        }
        if (release.latest) {
          const badge = document.createElement('span');
          badge.className = 'xcm-badge latest';
          badge.textContent = 'Последняя';
          main.appendChild(badge);
        }
        if (release.prerelease) {
          const badge = document.createElement('span');
          badge.className = 'xcm-badge preview';
          badge.textContent = 'Предрелиз';
          main.appendChild(badge);
        }

        const date = document.createElement('div');
        date.className = 'xcm-release-date';
        date.textContent = formatDate(release.published_at);
        option.append(main, date);
        option.addEventListener('click', () => {
          selectedVersion = release.version;
          draw();
          renderFooter();
        });
        list.appendChild(option);
      });
    };

    search.addEventListener('input', draw);
    draw();
    renderFooter();
    requestAnimationFrame(positionManager);
  }

  function syncConfigStudioService(result) {
    const status = qs('#csServiceStatus');
    const button = qs('#csRestartXray');
    const version = qs('#csServiceVersion');
    if (status) {
      status.textContent = result?.online ? 'Работает' : 'Остановлен';
      status.className = `cs-service-status ${result?.online ? 'ok' : 'bad'}`;
    }
    if (button) {
      button.textContent = 'Открыть Xray';
      button.disabled = false;
    }
    if (version && result?.version) version.textContent = `${compactVersion(result.version) || result.version} ▾`;
  }

  function renderServiceEvents(target, events) {
    const title = document.createElement('div');
    title.className = 'xcm-section-title';
    title.textContent = 'Последние события';
    target.appendChild(title);

    const list = document.createElement('div');
    list.className = 'xcm-service-events';
    const rows = Array.isArray(events) ? events.slice(0, 5) : [];
    if (!rows.length) {
      const empty = document.createElement('div');
      empty.className = 'xcm-empty';
      empty.textContent = 'Действий Xray пока нет.';
      list.appendChild(empty);
    } else {
      rows.forEach(event => {
        const row = document.createElement('div');
        const ok = String(event?.result || '').toLowerCase() === 'success';
        row.className = `xcm-service-event${ok ? '' : ' bad'}`;
        const at = document.createElement('span');
        at.textContent = friendlyTime(event?.at);
        const result = document.createElement('b');
        result.textContent = ok ? 'Успешно' : 'Ошибка';
        const message = document.createElement('span');
        message.textContent = String(event?.message || '');
        row.append(at, result, message);
        list.appendChild(row);
      });
    }
    target.appendChild(list);
  }

  function renderServiceHome(result, notice = '') {
    serviceState = result || serviceState || {success:true,online:false,events:[]};
    syncServiceSurfaces(serviceState);
    setManagerHeading('Xray', 'Сервис, версия и последние события');
    const target = body();
    const footer = actions();
    clearNode(target); clearNode(footer);

    const card = document.createElement('div');
    card.className = 'xcm-service-card';

    const statusRow = document.createElement('div');
    statusRow.className = 'xcm-service-row';
    const statusLabel = document.createElement('span');
    statusLabel.className = 'xcm-service-label';
    statusLabel.textContent = 'Состояние';
    const statusValue = document.createElement('strong');
    statusValue.className = `xcm-service-value ${serviceState.online ? 'ok' : 'bad'}`;
    statusValue.textContent = serviceState.online ? 'Работает' : 'Остановлен';
    statusRow.append(statusLabel, statusValue);

    const versionRow = document.createElement('div');
    versionRow.className = 'xcm-service-row';
    const versionLabel = document.createElement('span');
    versionLabel.className = 'xcm-service-label';
    versionLabel.textContent = 'Версия';
    const versionValue = document.createElement('strong');
    versionValue.className = 'xcm-service-value';
    versionValue.textContent = compactVersion(serviceState.version) || String(serviceState.version || '—');
    versionRow.append(versionLabel, versionValue);
    card.append(statusRow, versionRow);
    target.appendChild(card);

    if (notice) {
      const box = document.createElement('div');
      box.className = 'xcm-result ok';
      box.textContent = notice;
      target.appendChild(box);
    }

    const controls = document.createElement('div');
    controls.className = 'xcm-service-actions';
    if (serviceState.online) {
      addButton(controls, 'Перезапустить', '', () => controlService('restart'));
      addButton(controls, 'Остановить', '', () => controlService('stop'));
      addButton(controls, 'Обновить', '', loadCatalog);
    } else {
      addButton(controls, 'Запустить Xray', 'primary', () => controlService('start'));
    }
    addButton(controls, 'Журнал', '', openJournal);
    target.appendChild(controls);

    renderServiceEvents(target, serviceState.events);
    addButton(footer, 'Закрыть', '', closeManager);
    requestAnimationFrame(positionManager);
  }

  function renderOfflineRecovery(result) {
    renderServiceHome(result);
  }

  async function mutateService(action) {
    if (serviceMutationUnknown) {
      throw new Error('Состояние Xray не подтверждено. Сначала перечитайте фактическое состояние.');
    }
    if (applying) throw new Error('Операция Xray уже выполняется.');

    applying = true;
    try {
      const response = await fetch('/api/xray/service', {
        method:'POST',
        headers:{'Content-Type':'application/json'},
        body:JSON.stringify({action}),
        cache:'no-store'
      });
      let result = {};
      try { result = await response.json(); } catch (_) {}
      if (!response.ok || !result.success) {
        serviceMutationUnknown = true;
        throw new Error(result.error || 'Операция Xray не завершена');
      }
      serviceMutationUnknown = false;
      syncServiceSurfaces(result);
      return result;
    } catch (error) {
      serviceMutationUnknown = true;
      throw error;
    } finally {
      applying = false;
    }
  }

  async function controlService(action) {
    if (applying) return;
    const target = body();
    const footer = actions();
    clearNode(target);
    clearNode(footer);

    const labels = {
      start: ['Проверяю конфигурацию и запускаю Xray…', 'Xray запущен и работает.'],
      stop: ['Останавливаю Xray…', 'Xray остановлен.'],
      restart: ['Проверяю конфигурацию и перезапускаю Xray…', 'Xray перезапущен и снова работает.']
    };
    const label = labels[action] || ['Выполняю операцию Xray…', 'Операция Xray завершена.'];
    const progress = document.createElement('div');
    progress.className = 'xcm-progress';
    progress.textContent = label[0];
    target.appendChild(progress);

    try {
      const result = await mutateService(action);
      renderServiceHome(result, result.message || label[1]);
      return result;
    } catch (error) {
      clearNode(target);
      clearNode(footer);
      const box = document.createElement('div');
      box.className = 'xcm-result bad stop';
      box.textContent = (error?.message || 'Состояние Xray не подтверждено.') + ' Перед следующей операцией перечитайте фактическое состояние.';
      target.appendChild(box);
      addButton(footer, 'Закрыть', '', closeManager);
      addButton(footer, 'Проверить состояние', 'primary', async () => {
        try {
          const result = await fetchServiceSnapshot();
          renderServiceHome(result);
        } catch (readError) {
          box.textContent = (readError?.message || 'Состояние Xray недоступно.') + ' Следующая mutation остаётся заблокированной.';
        }
      });
      return null;
    } finally {
      requestAnimationFrame(positionManager);
    }
  }

  async function recoverOfflineXray() {
    return controlService('start');
  }

  async function loadCatalog() {
    if (applying || !serviceState?.online) return;
    applying = true;
    const target = body();
    const footer = actions();
    clearNode(target);
    clearNode(footer);
    setManagerHeading('Версия Xray Core', 'Выбор версии, затем проверка и установка');
    const progress = document.createElement('div');
    progress.className = 'xcm-progress';
    progress.textContent = 'Получаю список официальных версий Xray…';
    target.appendChild(progress);
    try {
      const response = await fetch('/api/xray/core/catalog', {cache:'no-store'});
      const result = await response.json();
      if (!response.ok || !result.success) throw new Error(result.error || 'Каталог Xray недоступен');
      catalog = result;
      selectedVersion = result.latest_version && result.latest_version !== result.current_version
        ? result.latest_version
        : (result.current_version || '');
      applying = false;
      renderCatalog();
    } catch (error) {
      clearNode(target);
      clearNode(footer);
      const box = document.createElement('div');
      box.className = 'xcm-error';
      box.textContent = error.message || 'Каталог Xray недоступен.';
      target.appendChild(box);
      addButton(footer, 'Назад', '', () => renderServiceHome(serviceState));
      addButton(footer, 'Закрыть', '', closeManager);
    } finally {
      applying = false;
      requestAnimationFrame(positionManager);
    }
  }

  async function applyRelease(release) {
    if (applying) return;
    applying = true;
    const target = body(); const footer = actions(); clearNode(target); clearNode(footer);
    const progress = document.createElement('div'); progress.className = 'xcm-progress'; progress.textContent = `Устанавливаем Xray ${release.version}…`; target.appendChild(progress);
    try {
      const response = await fetch('/api/xray/core/apply', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({target_version:release.version}), cache:'no-store'});
      let result = {}; try { result = await response.json(); } catch (_) {}
      clearNode(target);
      const box = document.createElement('div');
      if (response.ok && result.success) {
        const message = result.message || `Xray ${release.version} установлен.`;
        const chip = qs('#csServiceVersion'); if (chip) chip.textContent = `${result.current_version || release.version} ▾`;
        syncTopbarVersion(result.current_version || release.version);
        try {
          const current = await fetchServiceSnapshot();
          renderServiceHome(current, message);
        } catch (_) {
          renderServiceHome(Object.assign({}, serviceState || {}, {success:true, online:true, version:result.current_version || release.version}), message);
        }
      } else {
        const rollbackFailed = result.rollback === 'FAILED';
        box.className = `xcm-result bad${rollbackFailed ? ' stop' : ''}`;
        const prefix = rollbackFailed ? 'Операция остановлена. Автоматический откат не подтверждён. ' : result.rollback === 'SUCCESS' ? 'Изменение отменено, предыдущая версия восстановлена. ' : '';
        box.textContent = prefix + (result.error || 'Не удалось изменить версию Xray.');
        target.appendChild(box);
        addButton(footer, 'Закрыть', '', closeManager);
        addButton(footer, 'Проверить состояние', 'primary', async () => {
          try {
            const current = await fetchServiceSnapshot();
            renderServiceHome(current);
          } catch (_) {
            box.textContent = box.textContent + ' Состояние Xray пока недоступно.';
          }
        });
      }
    } catch (_) {
      clearNode(target);
      clearNode(footer);
      const box = document.createElement('div');
      box.className = 'xcm-result bad stop';
      box.textContent = 'Связь с FreeNet прервалась во время операции. Не повторяйте установку вслепую: сначала перечитайте фактическое состояние Xray.';
      target.appendChild(box);
      addButton(footer, 'Закрыть', '', closeManager);
      addButton(footer, 'Проверить состояние', 'primary', async () => {
        try {
          const current = await fetchServiceSnapshot();
          renderServiceHome(current);
        } catch (_) {
          box.textContent = 'Состояние Xray пока не подтверждено. Следующая mutation остаётся заблокированной.';
        }
      });
    } finally {
      applying = false;
    }
  }

  async function openManager() {
    ;
    const root = ensureRoot();
    if (!root.hidden) return;
    focusBeforeOpen = document.activeElement;
    root.hidden = false;
    root.classList.add('xcm-open');
    qs('#xrayTopbarChip')?.setAttribute('aria-expanded', 'true');
    positionManager();
    requestAnimationFrame(() => qs('.xcm-modal', root)?.focus({preventScroll:true}));
    selectedVersion = '';
    const target = body();
    const footer = actions();
    clearNode(target);
    clearNode(footer);
    setManagerHeading('Xray', 'Проверка состояния сервиса');
    const progress = document.createElement('div');
    progress.className = 'xcm-progress';
    progress.textContent = 'Проверяю состояние Xray…';
    target.appendChild(progress);
    try {
      const currentService = await fetchServiceSnapshot();
      renderServiceHome(currentService);
    } catch (error) {
      clearNode(target);
      clearNode(footer);
      const box = document.createElement('div');
      box.className = 'xcm-error';
      box.textContent = error.message || 'Состояние Xray недоступно.';
      target.appendChild(box);
      addButton(footer, 'Закрыть', '', closeManager);
    }
  }

  window.FreeNetXrayControl = Object.assign(window.FreeNetXrayControl || {}, {
    refresh: fetchServiceSnapshot,
    action: mutateService,
    open: openManager,
    openVersions: async () => {
      await openManager();
      if (serviceState?.online) await loadCatalog();
    },
    openJournal,
    getState: () => serviceState,
    mutationBlocked: () => serviceMutationUnknown || applying
  });

  ;
  document.addEventListener('click', event => {
    const chip = event.target.closest?.('#csServiceVersion,#csRestartXray');
    if (!chip) return;
    event.preventDefault();
    openManager();
  });
  const bootTopbar = () => {
    hydrateTopbar();
    // A delayed shell or recreated topbar must hydrate independently of clicks.
    const observer = new MutationObserver(() => {
      if (!qs('#xrayTopbarChip') && qs('.topbar')) hydrateTopbar();
    });
    observer.observe(document.body, {childList:true, subtree:true});
  };
  document.addEventListener('freenet:auth-changed', topbarAuthenticationChanged);
  document.addEventListener('visibilitychange', resumeTopbarRead);
  window.addEventListener('online', resumeTopbarRead);
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', bootTopbar, {once:true});
  else bootTopbar();
  window.addEventListener('hashchange', hydrateTopbar);
  window.addEventListener('resize', positionManager);
  window.visualViewport?.addEventListener('resize', positionManager);
  document.addEventListener('click', event => {
    const root = qs('#xrayCoreManager');
    if (!root || root.hidden || applying) return;
    if (event.target.closest?.('#xrayTopbarChip') || event.target.closest?.('#xrayCoreManager .xcm-modal')) return;
    closeManager();
  }, true);
  document.addEventListener('keydown', event => {
    if (event.key !== 'Escape' || qs('#xrayCoreManager')?.hidden || applying) return;
    event.preventDefault();
    closeManager();
  });
})();