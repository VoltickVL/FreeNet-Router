(() => {
  'use strict';

  const qs = (s, root = document) => root.querySelector(s);
  let catalog = null;
  let serviceState = null;
  let selectedVersion = '';
  let coreValidation = null;
  let applying = false;
  let topbarVersionLoading = false;
  let topbarVersionAttempts = 0;
  let topbarRetryTimer = null;
  let topbarReadController = null;
  let topbarReadGeneration = 0;
  const topbarMaxAttempts = 4;
  let serviceMutationUnknown = false;
  // Poll only the read-only receipt while a detached core operation runs.
  // No timer can issue a second apply, and closing/changing views cancels it.
  let coreReceiptPollTimer = null;
  let coreReceiptPollTarget = '';
  let coreReceiptPollStartedAt = 0;
  let coreReceiptPollEpoch = 0;
  const coreReceiptPollInterval = 1500;
  const coreReceiptPollMaxWait = 5 * 60 * 1000;

  

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
    mountXKeenTopbarChip();
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
      qs('#xkeenTopbarVersion') && (qs('#xkeenTopbarVersion').textContent='вход требуется');
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
    stopCoreReceiptPolling();
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

  // Only finite server reason codes are translated. Raw validator output,
  // paths or arbitrary diagnostics are never injected into the UI.
  function coreValidationText(reason) {
    const code = String(reason || '').match(/^reason=([a-z0-9-]+)/)?.[1] || '';
    const labels = {
      'xray-live-config-unavailable': 'Рабочий каталог Xray недоступен.',
      'xray-live-config-path-unavailable': 'Не подтверждён путь к рабочей конфигурации Xray.',
      'xray-live-test-timeout': 'Проверка текущего Xray завершилась по тайм-ауту.',
      'xray-live-test-idle': 'Проверка Xray не продвигалась по CPU к исходному сроку: безопасный STOP.',
      'xray-live-test-progress-timeout': 'Проверка Xray выполнялась, но не завершилась до предельного срока: STOP.',
      'xray-candidate-test-idle': 'Проверка новой версии Xray не продвигалась по CPU: STOP без замены.',
      'xray-candidate-test-progress-timeout': 'Проверка новой версии Xray превысила ограниченный срок: STOP без замены.',
      'xray-readonly-probe-unavailable': 'Безопасная проверка Xray сейчас недоступна.',
      'xray-test-executable-unavailable': 'Исполняемый файл Xray недоступен для проверки.',
      'xray-live-config-test-failed': 'Фактическая конфигурация не прошла проверку установленным Xray.',
      'xray-current-version-unavailable': 'Установленная версия Xray недоступна для проверки.',
      'xray-core-update-running': 'Другая операция обновления Xray ещё выполняется.',
      'xray-recovery-stop': 'Восстановление Xray не подтверждено: STOP.',
      'xray-diagnostic-mutation-fence': 'Обновление или аварийное восстановление Xray активно: диагностика остановлена.',
      'xray-diagnostic-probes-busy': 'Фоновые диагностические процессы Xray не завершились вовремя.',
      'xray-diagnostic-already-running': 'Диагностика Xray уже выполняется.',
      'xray-diagnostic-operation-busy': 'Другая операция FreeNet выполняется.',
      'xray-diagnostic-binary-unavailable': 'Установленный бинарник Xray недоступен.',
      'xray-diagnostic-config-unavailable': 'Рабочая конфигурация Xray недоступна.',
      'xray-diagnostic-workspace-unavailable': 'Не удалось подготовить временную область проверки.',
      'xray-diagnostic-probe-unavailable': 'Безопасный диагностический процесс недоступен.',
      'xray-diagnostic-child-start-failed': 'Диагностический процесс Xray не смог стартовать.',
      'xray-diagnostic-output-unavailable': 'Не удалось безопасно подготовить вывод проверки Xray.',
      'xray-diagnostic-canceled': 'Проверка прервана без изменения Xray.'
    };
    return labels[code] || 'Проверка фактической конфигурации Xray не подтверждена.';
  }

  function renderCatalog() {
    stopCoreReceiptPolling();
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

    const preflight = document.createElement('div');
    preflight.className = 'xcm-selection ' + (coreValidation?.ready ? 'ready' : 'bad');
    preflight.textContent = coreValidation?.ready && coreValidation.validation === 'DEFERRED' && !coreValidation.valid
      ? 'Xray доступен. FreeNet проверит официальный архив и версию, сохранит прежнее ядро и выполнит собственный перезапуск Xray через FreeNet. При ошибке восстановит предыдущий бинарник.'
      : coreValidationText(coreValidation?.reason) + ' Обновление не запускается.';
    target.appendChild(preflight);
    if (coreValidation?.reason === 'reason=xray-recovery-stop') {
      const recoveryButton = document.createElement('button');
      recoveryButton.type = 'button';
      recoveryButton.className = 'btn secondary';
      recoveryButton.textContent = 'Подтвердить восстановленный Xray';
      recoveryButton.addEventListener('click', async () => {
        recoveryButton.disabled = true;
        try {
          await acknowledgeCoreRecovery(current);
          await loadCatalog();
        } catch (error) {
          preflight.textContent = error?.message || 'Рабочий Xray не подтверждён; операция не разрешена.';
          recoveryButton.disabled = false;
        }
      });
      target.appendChild(recoveryButton);
    }

    // Opt-in semantic test of the ALREADY installed Xray binary. No download,
    // restart or config edit. The answer contains finite codes and timings only.
    const diagnosticButton = document.createElement('button');
    diagnosticButton.type = 'button';
    diagnosticButton.className = 'btn secondary';
    diagnosticButton.id = 'xcmReadOnlyLiveDiagnostic';
    diagnosticButton.textContent = 'Диагностика Xray (без обновления)';
    const diagnosticResult = document.createElement('div');
    diagnosticResult.id = 'xcmReadOnlyLiveResult';
    diagnosticResult.className = 'xcm-selection';
    diagnosticResult.setAttribute('role', 'status');
    diagnosticResult.setAttribute('aria-live', 'polite');
    diagnosticResult.textContent = 'Проверка выполняется только по нажатию, без изменения VPN.';
    diagnosticButton.addEventListener('click', async () => {
      if (applying) return;
      applying = true;
      diagnosticButton.disabled = true;
      diagnosticResult.textContent = 'Проверяю установленный Xray на фактической конфигурации без изменения бинарника…';
      const controller = new AbortController();
      const deadline = setTimeout(() => controller.abort(), 60000);
      try {
        const response = await fetch('/api/xray/core/read-only-test', {cache:'no-store', signal:controller.signal});
        const result = await response.json();
        const child = result.child_started === true ? 'да' : 'нет';
        const elapsed = Math.max(0, Number(result.child_ms || 0));
        const userCPU = Math.max(0, Number(result.user_cpu_ms || 0));
        const systemCPU = Math.max(0, Number(result.system_cpu_ms || 0));
        const phase = String(result.phase || 'unknown');
        const admission = Math.max(0, Number(result.admission_ms || 0));
        const prep = Math.max(0, Number(result.preparation_ms || 0));
        const code = result.exit_code === undefined ? '—' : String(result.exit_code);
        const evidence = 'Стадия: ' + phase + '; child запущен: ' + child +
          '; admission: ' + admission + ' мс; подготовка: ' + prep +
          ' мс; исполнение: ' + elapsed + ' мс; CPU user/system: ' +
          userCPU + '/' + systemCPU + ' мс; exit: ' + code + '.';
        if (response.ok && result.success === true && result.mutation === 'NONE' && result.valid === true) {
          diagnosticResult.className = 'xcm-selection ready';
          diagnosticResult.textContent = 'Проверка установленного Xray PASS. ' + evidence +
            ' Это дополнительная диагностика; при обычном обновлении тяжёлая проверка не выполняется.';
        } else {
          diagnosticResult.className = 'xcm-selection bad';
          diagnosticResult.textContent = coreValidationText(result.reason) + ' ' + evidence +
            ' Xray не изменён. Повторная установка до выяснения причины запрещена.';
        }
      } catch (_) {
        diagnosticResult.className = 'xcm-selection bad';
        diagnosticResult.textContent = 'Квитанция read-only проверки недоступна. Xray не изменён; это не результат PASS.';
      } finally {
        clearTimeout(deadline);
        applying = false;
        diagnosticButton.disabled = false;
      }
    });
    // Advanced diagnosis stays available without becoming a compulsory
    // step or consuming the version selector's main screen space.
    const advanced = document.createElement('details');
    advanced.className = 'xcm-advanced-diagnostic';
    const advancedSummary = document.createElement('summary');
    advancedSummary.textContent = 'Дополнительно: диагностика Xray';
    advanced.append(advancedSummary, diagnosticButton, diagnosticResult);
    target.appendChild(advanced);

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
      if (!coreValidation?.ready || coreValidation.validation !== 'DEFERRED') {
        selection.className = 'xcm-selection bad';
        selection.textContent = 'Выбор версии сохранён. Безопасные условия обновления Xray не подтверждены.';
        return;
      }
      selection.className = 'xcm-selection ready';
      selection.textContent = `Выбрана ${selected.version}. FreeNet проверит SHA-256 и версию, сохранит прежнее ядро и применит обновление собственным механизмом FreeNet. При сбое восстановит старый бинарник; неопределённое состояние остановит дальнейшие изменения.`;
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
    stopCoreReceiptPolling();
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

  async function acknowledgeCoreRecovery(expectedVersion) {
    const current = String(expectedVersion || catalog?.current_version || serviceState?.version || '').trim();
    if (!current || applying) throw new Error('Не удалось определить установленную версию Xray.');
    applying = true;
    try {
      const response = await fetch('/api/xray/core/recover', {
        method: 'POST',
        headers: {'Content-Type':'application/json'},
        cache: 'no-store',
        body: JSON.stringify({expected_version:current})
      });
      const result = await response.json();
      if (!response.ok || result.success !== true || result.mutation !== 'LATCH_ONLY' || result.version !== current) {
        throw new Error(coreValidationText(result.reason));
      }
      coreValidation = null;
    } finally {
      applying = false;
    }
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
    progress.textContent = 'Получаю официальные версии и проверяю готовность Xray без изменений…';
    target.appendChild(progress);
    // Catalog and cheap native readiness GETs run in parallel. The
    // version-changing worker performs hash/backup/native restart/rollback;
    // expensive xray run -test is opt-in diagnostics only.
    coreValidation = null;
    const validationPromise = fetch('/api/xray/core/validation', {cache:'no-store'})
      .then(async response => {
        const result = await response.json();
        return response.ok && result.success && result.mutation === 'NONE'
          ? result : {success:false, valid:false, reason:'reason=xray-validation-unavailable'};
      })
      .catch(() => ({success:false, valid:false, reason:'reason=xray-validation-unavailable'}));
    try {
      const response = await fetch('/api/xray/core/catalog', {cache:'no-store'});
      const result = await response.json();
      if (!response.ok || !result.success) throw new Error(result.error || 'Каталог Xray недоступен');
      coreValidation = await validationPromise;
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

  // A disconnected HTTP client does not cancel the server-side core update.
  // Reconcile its terminal receipt, not merely the service's current version.
  async function readCoreUpdateReceipt() {
    const response = await fetch('/api/xray/core/operation', {cache:'no-store'});
    if (!response.ok) throw new Error('Квитанция обновления Xray недоступна');
    return response.json();
  }

  function stopCoreReceiptPolling() {
    clearTimeout(coreReceiptPollTimer);
    coreReceiptPollTimer = null;
    coreReceiptPollTarget = '';
    coreReceiptPollStartedAt = 0;
    coreReceiptPollEpoch += 1;
  }

  function scheduleCoreReceiptPolling(version) {
    if (!version || qs('#xrayCoreManager')?.hidden) return;
    if (coreReceiptPollTarget !== version) {
      stopCoreReceiptPolling();
      coreReceiptPollTarget = version;
      coreReceiptPollStartedAt = Date.now();
    }
    clearTimeout(coreReceiptPollTimer);
    if (Date.now() - coreReceiptPollStartedAt >= coreReceiptPollMaxWait) {
      // A polling deadline is NOT an Xray operation failure. Preserve the
      // explicit read-only refresh button and stop automatic network calls.
      const box = body().querySelector('.xcm-progress');
      if (box) {
        box.className = 'xcm-result';
        box.textContent = 'Автоматическая проверка результата заняла слишком много времени. Это не означает ошибку обновления. Нажмите «Проверить результат»; повторную установку не запускайте.';
      }
      stopCoreReceiptPolling();
      return;
    }
    const epoch = coreReceiptPollEpoch;
    coreReceiptPollTimer = setTimeout(async () => {
      coreReceiptPollTimer = null;
      if (epoch !== coreReceiptPollEpoch || qs('#xrayCoreManager')?.hidden) return;
      if (document.visibilityState === 'hidden') {
        scheduleCoreReceiptPolling(version);
        return;
      }
      try {
        // Polling only reads /api/xray/core/operation. Never repeat /apply.
        const reconciled = await renderCoreUpdateReceipt(version, epoch);
        if (!reconciled && epoch === coreReceiptPollEpoch) scheduleCoreReceiptPolling(version);
      } catch (_) {
        // Temporary read error is not proof of failure or rollback.
        if (epoch === coreReceiptPollEpoch) scheduleCoreReceiptPolling(version);
      }
    }, coreReceiptPollInterval);
  }

  async function renderCoreUpdateReceipt(expectedVersion, pollEpoch) {
    const receipt = await readCoreUpdateReceipt();
    if (qs('#xrayCoreManager')?.hidden ||
        (pollEpoch !== undefined && pollEpoch !== coreReceiptPollEpoch)) return false;
    if (!receipt || !['RUNNING', 'SUCCESS', 'ROLLED_BACK', 'FAILED', 'STOP'].includes(receipt.state)) return false;
    if (expectedVersion && receipt.target_version !== expectedVersion) return false;
    if (receipt.state !== 'RUNNING') stopCoreReceiptPolling();
    const target = body(), footer = actions();
    if (receipt.state === 'SUCCESS' && receipt.result?.success) {
      const message = receipt.result.message || ('Xray ' + receipt.target_version + ' установлен и проверен.');
      try {
        const current = await fetchServiceSnapshot();
        if (!qs('#xrayCoreManager')?.hidden) renderServiceHome(current, message);
      } catch (_) {
        // A valid update receipt does not establish that the browser can
        // currently reach the service. Do not synthesize a green status.
        clearNode(target); clearNode(footer);
        const box = document.createElement('div');
        box.className = 'xcm-result bad stop';
        box.textContent = 'Xray обновлён, но текущее состояние Control Center недоступно. Перепроверьте соединение.';
        target.appendChild(box);
        addButton(footer, 'Проверить состояние', 'primary', async () => {
          try { await renderCoreUpdateReceipt(receipt.target_version); }
          catch (_) { box.textContent = 'Квитанция и состояние Xray временно недоступны. Повторное изменение запрещено.'; }
        });
        addButton(footer, 'Закрыть', '', closeManager);
      }
      return true;
    }
    clearNode(target); clearNode(footer);
    const box = document.createElement('div');
    box.className = receipt.state === 'RUNNING' ? 'xcm-progress' : 'xcm-result bad' + (receipt.state === 'STOP' ? ' stop' : '');
    const detail = String(receipt.result?.error || '');
    switch (receipt.state) {
      case 'RUNNING':
        box.textContent = 'Обновление Xray продолжается на роутере. Результат проверяется автоматически; повторную установку не запускайте.';
        scheduleCoreReceiptPolling(receipt.target_version);
        break;
      case 'ROLLED_BACK':
        box.textContent = 'Новая версия не принята; прежняя версия Xray восстановлена и проверена. ' + detail;
        break;
      case 'STOP':
        box.textContent = 'STOP: восстановление Xray не подтверждено. Следующие изменения запрещены до диагностики. ' + detail;
        break;
      default:
        box.textContent = 'Xray не обновлён. ' + (detail || 'Результат изменения не подтверждён.');
    }
    target.appendChild(box);
    if (receipt.state === 'RUNNING') {
      addButton(footer, 'Проверить результат', 'primary', async () => {
        try { await renderCoreUpdateReceipt(expectedVersion); }
        catch (_) { box.textContent = 'Статус Xray недоступен. Повторную установку не запускайте.'; }
      });
    } else {
      if (receipt.state === 'STOP') {
        addButton(footer, 'Проверить восстановленный Xray', 'primary', async () => {
          try {
            await acknowledgeCoreRecovery(receipt.result?.previous_version || serviceState?.version);
            await loadCatalog();
          } catch (error) {
            box.textContent = (error?.message || 'Восстановление Xray не подтверждено.') +
              ' Предыдущая квитанция STOP сохранена.';
          }
        });
      }
      addButton(footer, 'Журнал', '', openJournal);
    }
    addButton(footer, 'Закрыть', '', closeManager);
    requestAnimationFrame(positionManager);
    return true;
  }

  async function applyRelease(release) {
    if (applying) return;
    stopCoreReceiptPolling();
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
        // An HTTP response can be non-terminal while the detached router
        // worker is still validating or restoring Xray. Never announce a
        // terminal failure before reconciling the same operation's receipt.
        try {
          const receipt = await readCoreUpdateReceipt();
          if (receipt?.target_version === release.version &&
              ['RUNNING', 'SUCCESS', 'FAILED', 'ROLLED_BACK', 'STOP'].includes(receipt.state)) {
            if (await renderCoreUpdateReceipt(release.version)) return;
          }
        } catch (_) {
          // Receipt unavailable: the request outcome alone is not sufficient
          // to prove a terminal worker result.
        }
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
      // A browser disconnect does not mean rollback or failure. Read the
      // operation receipt owned by the detached FreeNet worker.
      let reconciled = false;
      try { reconciled = await renderCoreUpdateReceipt(release.version); } catch (_) {}
      if (!reconciled) {
        clearNode(target); clearNode(footer);
        const box = document.createElement('div');
        box.className = 'xcm-result bad stop';
        box.textContent = 'Связь прервалась, статус обновления Xray пока неизвестен. Не запускайте установку повторно.';
        target.appendChild(box);
        addButton(footer, 'Проверить результат', 'primary', async () => {
          try { if (!await renderCoreUpdateReceipt(release.version)) throw new Error('not available'); }
          catch (_) { box.textContent = 'Квитанция Xray пока недоступна. Повторное изменение запрещено.'; }
        });
        addButton(footer, 'Закрыть', '', closeManager);
      }
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
      let receipt = null;
      try { receipt = await readCoreUpdateReceipt(); } catch (_) {}
      if (receipt && (receipt.state === 'RUNNING' || receipt.state === 'STOP')) {
        if (await renderCoreUpdateReceipt(receipt.target_version)) return;
      }
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

  // XKeen sits next to the Xray version chip, but has a separate
  // transactional owner: no Xray action or command is reused as an updater.
  let xkeenPanelOpen = false;
  let xkeenPanelBusy = false;
  let xkeenPollTimer = null;
  let xkeenPlan = null;

  function xkeenManagerRoot() {
    let root = qs('#xkeenUpdateManager');
    if (root) return root;
    root = document.createElement('div');
    root.id = 'xkeenUpdateManager';
    root.className = 'xcm-root';
    root.hidden = true;
    root.innerHTML = `<div class="xcm-backdrop"></div><section class="xcm-modal" role="dialog" aria-modal="false" aria-labelledby="xkeenUpdateHeading" tabindex="-1">
      <div class="xcm-head"><div><div class="xcm-kicker">XKeen</div><h3 id="xkeenUpdateHeading">Управление XKeen</h3>
      <div class="xcm-subtitle">Проверка, резервная копия и безопасное обновление</div></div>
      <button id="xkeenUpdateClose" class="xcm-close" type="button" aria-label="Закрыть">×</button></div>
      <div id="xkeenUpdateBody"></div><div id="xkeenUpdateActions" class="xcm-actions"></div>
    </section>`;
    document.body.appendChild(root);
    qs('#xkeenUpdateClose',root).addEventListener('click',closeXKeenManager);
    return root;
  }

  function xkeenManagerPosition() {
    const root = qs('#xkeenUpdateManager');
    if (!root || root.hidden) return;
    const dialog = qs('.xcm-modal',root);
    const chip = qs('#xkeenTopbarChip');
    if (!dialog || !chip) return;
    const vw = window.visualViewport?.width || window.innerWidth;
    const vh = window.visualViewport?.height || window.innerHeight;
    const width = Math.min(540, vw-24);
    const rect = chip.getBoundingClientRect();
    dialog.style.width = width+'px';
    dialog.style.left = Math.max(12,Math.min(vw-width-12,rect.right-width))+'px';
    const height = Math.min(dialog.scrollHeight || 560,Math.max(220,vh-24));
    dialog.style.top = Math.max(12,Math.min(vh-height-12,rect.bottom+8))+'px';
  }

  function xkeenStopPoll() {
    if (xkeenPollTimer !== null) clearTimeout(xkeenPollTimer);
    xkeenPollTimer = null;
  }

  function closeXKeenManager() {
    if (xkeenPanelBusy) return;
    xkeenStopPoll();
    const root = qs('#xkeenUpdateManager');
    if (root) {root.hidden = true;root.classList.remove('xcm-open');}
    qs('#xkeenTopbarChip')?.setAttribute('aria-expanded','false');
    xkeenPanelOpen = false;
    xkeenPlan = null;
  }

  function xkeenSay(message, className) {
    const target = qs('#xkeenUpdateBody');
    if (!target) return;
    const node = document.createElement('p');
    node.className = className || 'xcm-subtitle';
    node.textContent = message;
    target.appendChild(node);
  }

  function xkeenButtons() {
    const body = qs('#xkeenUpdateBody',xkeenManagerRoot());
    const footer = qs('#xkeenUpdateActions',xkeenManagerRoot());
    clearNode(body);clearNode(footer);
    return footer;
  }

  function xkeenReason(code) {
    const known = {
      'xray-production-identity-unverified':'Роль работающего Xray не подтверждена. Установка запрещена без аппаратной диагностики.',
      'freenet-operation-or-recovery-busy':'Другой процесс FreeNet или незавершённое восстановление блокирует установку.',
      'xkeen-installed-channel-not-stable':'Установлен XKeen Beta или неизвестный канал. Автопереход на Stable запрещён.',
      'xkeen-snapshot-space-unavailable':'Недостаточно подтверждённого места для резервной копии.',
      'xkeen-upstream-unverified':'Не удалось проверить официальный релиз XKeen.',
      'xkeen-no-upgrade-available':'Установленная версия не старее официальной Stable.',
      'xkeen-installation-unverified':'Файлы существующего XKeen определены неоднозначно.',
      'xkeen-current-version-unverified':'Версия установленного XKeen недоступна.',
      'xkeen-prior-operation-not-terminal':'Предыдущее обновление не получило завершённой квитанции.',
      'plan-expired-or-runtime-unverified':'План или состояние рабочего Xray изменились. Установка не начата.'
    };
    return known[code] || 'Проверка безопасности не завершена. Изменение XKeen не выполнено.';
  }

  async function xkeenReadJSON(path, options) {
    const controller = new AbortController();
    const timer = setTimeout(()=>controller.abort(),options?.timeout || 12000);
    try {
      const response = await fetch(path,{cache:'no-store',credentials:'same-origin',
        ...(options?.method ? {method:options.method, headers:{'Content-Type':'application/json'},body:JSON.stringify(options.body)} : {}),
        signal:controller.signal});
      let body = null;
      try {body=await response.json();}catch(_){}
      if(!body || typeof body !== 'object') throw new Error('Ответ XKeen не подтверждён');
      return {response,body};
    } finally {clearTimeout(timer);}
  }

  async function xkeenRefreshTopbar() {
    if (!authenticatedTopbar()) return;
    const chip = qs('#xkeenTopbarChip');
    if (!chip) return;
    try {
      const {response,body} = await xkeenReadJSON('/api/xkeen/installed',{timeout:5500});
      if (!response.ok || !body.success || !/^\d+(?:\.\d+){1,3}$/.test(body.installed_version)) throw new Error('version');
      qs('#xkeenTopbarVersion').textContent = 'v'+body.installed_version;
    } catch(_) {
      qs('#xkeenTopbarVersion').textContent = 'неизвестно';
    }
  }

  function mountXKeenTopbarChip() {
    const xray = qs('#xrayTopbarChip');
    if (!xray || !xray.parentNode) return;
    let chip = qs('#xkeenTopbarChip');
    if (!chip) {
      chip = document.createElement('button');
      chip.id = 'xkeenTopbarChip';
      chip.type = 'button';
      chip.className = 'fn-xray-topbar fn-xkeen-topbar';
      chip.title = 'Проверка и обновление XKeen';
      chip.setAttribute('aria-haspopup','dialog');
      chip.setAttribute('aria-controls','xkeenUpdateManager');
      chip.setAttribute('aria-expanded','false');
      chip.innerHTML = `<span class="fn-xray-topbar-icon">${topbarIcon()}</span><span class="fn-xray-chip"><small>XKeen</small><strong id="xkeenTopbarVersion">версия…</strong></span>`;
      chip.addEventListener('click',event=>{event.preventDefault();void openXKeenManager();});
    }
    if (chip.previousElementSibling !== xray) xray.parentNode.insertBefore(chip,xray.nextSibling || null);
  }

  function xkeenReceiptText(s) {
    if (s.state === 'SUCCESS') return 'Файлы XKeen установлены и проверены. Xray не перезапускался, существующие сетевые настройки сохранены.';
    if (s.state === 'ROLLED_BACK') return 'Обновление не применено. Предыдущая версия XKeen восстановлена.';
    if (s.state === 'STOP') return 'Состояние предыдущей операции неопределённо. Новое обновление запрещено.';
    if (s.state === 'FAILED') return 'Установка не выполнена. Исходный XKeen сохранён.';
    return 'Операция XKeen: '+String(s.state || 'неизвестно');
  }

  function xkeenShowReceipt(record) {
    const footer = xkeenButtons();
    xkeenSay(xkeenReceiptText(record),'xcm-subtitle');
    if (record.previous) xkeenSay('Предыдущая версия: '+record.previous);
    if (record.target) xkeenSay('Целевая версия: '+record.target);
    xkeenSay('Откат: '+String(record.rollback || 'неизвестно'));
    if (record.state === 'STOP' && /^\d+(?:\.\d+){1,3}$/.test(record.previous || '')) {
      addButton(footer,'Восстановить предыдущий XKeen','',()=>{
        void xkeenRecover(record.previous);
      });
    }
    if (record.state !== 'STOP' && record.state !== 'RUNNING') {
      addButton(footer,'Проверить снова','',()=>{void xkeenCheckPlan();});
    }
    addButton(footer,'Закрыть','',closeXKeenManager);
    xkeenManagerPosition();
  }

  async function xkeenRecover(expected) {
    if (xkeenPanelBusy || !window.confirm('Восстановить предыдущие файлы XKeen из проверенного локального снимка? Работающий Xray не будет перезапущен.')) return;
    xkeenPanelBusy=true;
    try {
      const {body}=await xkeenReadJSON('/api/xkeen/update/recover',{method:'POST',body:{expected_version:expected},timeout:12000});
      xkeenShowReceipt(body);
      await xkeenRefreshTopbar();
    } catch(_) {xkeenButtons();xkeenSay('Восстановление не подтверждено. STOP. Повторное обновление запрещено.');}
    finally{xkeenPanelBusy=false;}
  }

  async function xkeenPollReceipt() {
    xkeenStopPoll();
    if (!xkeenPanelOpen) return;
    try {
      const {body}=await xkeenReadJSON('/api/xkeen/update/state',{timeout:9000});
      if(body.state === 'RUNNING') {
        const footer=xkeenButtons();
        xkeenSay('Проверяем установку и резервную копию… Не закрывайте соединение до квитанции.');
        addButton(footer,'Закрыть','',closeXKeenManager);
        xkeenPollTimer=setTimeout(()=>{void xkeenPollReceipt();},1600);
      } else {
        xkeenShowReceipt(body);
        if(body.state === 'SUCCESS') await xkeenRefreshTopbar();
      }
    } catch(_) {
      xkeenButtons();xkeenSay('Квитанция XKeen недоступна. Не запускайте обновление повторно.');
      const footer=qs('#xkeenUpdateActions');
      addButton(footer,'Повторить чтение квитанции','',()=>void xkeenPollReceipt());
      addButton(footer,'Закрыть','',closeXKeenManager);
    }
  }

  async function xkeenApplyPlan() {
    if (xkeenPanelBusy || !xkeenPlan?.ready) return;
    if (!window.confirm('Обновить файлы XKeen до Stable '+xkeenPlan.target_version+'? FreeNet сохранит исходные файлы, проверит Xray и выполнит откат при ошибке. Xray не перезапускается.')) return;
    const plan = xkeenPlan;
    xkeenPanelBusy=true;
    const footer=xkeenButtons();
    xkeenSay('Запускаем контролируемое обновление XKeen. После отправки нельзя повторять Apply по тайм-ауту.');
    addButton(footer,'Закрыть','',closeXKeenManager);
    try {
      await xkeenReadJSON('/api/xkeen/update/apply',{method:'POST',
        body:{installed_version:plan.installed_version,target_version:plan.target_version,expected_sha256:plan.expected_sha256},
        timeout:9000});
    } catch(_) {
      // A lost browser response is UNKNOWN, never an authorization to retry.
    } finally {
      xkeenPanelBusy=false;
      void xkeenPollReceipt();
    }
  }

  async function xkeenCheckPlan() {
    if (!xkeenPanelOpen || xkeenPanelBusy) return;
    xkeenPlan=null;
    const footer=xkeenButtons();
    xkeenSay('Читаем установленный XKeen и официальный Stable. Ничего не устанавливается.');
    addButton(footer,'Закрыть','',closeXKeenManager);
    try {
      const {body}=await xkeenReadJSON('/api/xkeen/update/plan',{timeout:14000});
      xkeenPlan=body;
      const actions=xkeenButtons();
      const safe = v => /^\d+(?:\.\d+){1,3}$/.test(v||'') ? v : 'не подтверждена';
      xkeenSay('Установлено: '+safe(body.installed_version)+' · канал '+(body.installed_channel === 'stable'?'Stable':body.installed_channel === 'beta'?'Beta':'неизвестен'));
      xkeenSay('Официальная Stable: '+safe(body.target_version));
      if(body.ready && body.success && body.expected_sha256?.length === 64 &&
        body.mutation === 'NONE' && body.runtime_change === 'NONE') {
        xkeenSay('План подтверждён: предыдущие файлы будут сохранены, работающий Xray проверен.');
        addButton(actions,'Обновить до '+body.target_version,'primary',()=>void xkeenApplyPlan());
      } else {
        xkeenSay(body.state === 'up-to-date' ? 'Последняя Stable уже установлена.' : xkeenReason(body.reason),'xcm-subtitle');
      }
      addButton(actions,'Обновить проверку','',()=>void xkeenCheckPlan());
      addButton(actions,'Закрыть','',closeXKeenManager);
      xkeenManagerPosition();
    } catch(_) {
      const actions=xkeenButtons();
      xkeenSay('Невозможно достоверно проверить обновление XKeen. Изменений нет.');
      addButton(actions,'Проверить снова','',()=>void xkeenCheckPlan());
      addButton(actions,'Закрыть','',closeXKeenManager);
    }
  }

  async function openXKeenManager() {
    if (xkeenPanelOpen) return;
    closeManager();
    const root=xkeenManagerRoot();
    root.hidden=false;root.classList.add('xcm-open');
    xkeenPanelOpen=true;
    qs('#xkeenTopbarChip')?.setAttribute('aria-expanded','true');
    void xkeenRefreshTopbar();
    xkeenManagerPosition();
    const footer=xkeenButtons();
    xkeenSay('Читаем квитанцию предыдущей операции XKeen…');
    addButton(footer,'Закрыть','',closeXKeenManager);
    try {
      const {body}=await xkeenReadJSON('/api/xkeen/update/state',{timeout:9000});
      if(body.state === 'RUNNING') {void xkeenPollReceipt();return;}
      if(body.state === 'STOP') {xkeenShowReceipt(body);return;}
      await xkeenCheckPlan();
    } catch(_) {
      const actions=xkeenButtons();
      xkeenSay('Статус XKeen не подтверждён. Применение запрещено.');
      addButton(actions,'Закрыть','',closeXKeenManager);
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
      const xray = qs('#xrayTopbarChip');
      const xkeen = qs('#xkeenTopbarChip');
      if ((!xray || !xkeen) && qs('.topbar')) hydrateTopbar();
      else if (xray && xkeen && xkeen.previousElementSibling !== xray) {
        // Other canonical topbar owners may reorder VPN/DNS after our mount.
        // Reconcile adjacency idempotently without re-fetching or changing state.
        mountXKeenTopbarChip();
      }
    });
    observer.observe(document.body, {childList:true, subtree:true});
  };
  document.addEventListener('freenet:auth-changed', topbarAuthenticationChanged);
  document.addEventListener('visibilitychange', resumeTopbarRead);
  window.addEventListener('online', resumeTopbarRead);
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', bootTopbar, {once:true});
  else bootTopbar();
  window.addEventListener('hashchange', hydrateTopbar);
  window.addEventListener('resize', () => {positionManager();xkeenManagerPosition();});
  window.visualViewport?.addEventListener('resize', () => {positionManager();xkeenManagerPosition();});
  document.addEventListener('click', event => {
    const root = qs('#xrayCoreManager');
    if (!root || root.hidden || applying) return;
    if (event.target.closest?.('#xrayTopbarChip') || event.target.closest?.('#xrayCoreManager .xcm-modal')) return;
    closeManager();
  }, true);
  document.addEventListener('click', event => {
    if (!xkeenPanelOpen || xkeenPanelBusy) return;
    if(event.target.closest?.('#xkeenTopbarChip') || event.target.closest?.('#xkeenUpdateManager .xcm-modal')) return;
    closeXKeenManager();
  },true);
  document.addEventListener('keydown', event => {
    if(event.key === 'Escape' && xkeenPanelOpen && !xkeenPanelBusy) {event.preventDefault();closeXKeenManager();}
  });
  document.addEventListener('keydown', event => {
    if (event.key !== 'Escape' || qs('#xrayCoreManager')?.hidden || applying) return;
    event.preventDefault();
    closeManager();
  });
})();