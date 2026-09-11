(() => {
  const qs = (selector, root = document) => root.querySelector(selector);
  const wait = ms => new Promise(resolve => setTimeout(resolve, ms));
  let updatePlan = null;
  let updatePolling = false;
  let authFetchWrapped = false;

  function injectStyles() {
    if (qs('#freenetAcceptedUXStyles')) return;
    const style = document.createElement('style');
    style.id = 'freenetAcceptedUXStyles';
    style.textContent = `
      .auth-wrap{position:fixed!important;inset:0!important;max-width:none!important;margin:0!important;display:grid!important;place-items:center!important;padding:24px!important;background:radial-gradient(circle at 68% -15%,#17345c 0,#0d1d32 42%,#081523 72%,#07101b 100%)!important;backdrop-filter:none!important}
      .auth-card{width:min(440px,calc(100vw - 32px));background:linear-gradient(180deg,#112137,#0d1a2c)!important}
      .fn-remember{display:flex;align-items:center;gap:9px;margin:11px 2px 0;color:#b5c4d9;font-size:12.5px;cursor:pointer;user-select:none}
      .fn-remember input{width:16px;height:16px;accent-color:#4b85ff;cursor:pointer}
      .fn-update-popover{position:fixed;z-index:1300;width:min(350px,calc(100vw - 24px));padding:15px;border:1px solid #31577d;border-radius:17px;background:linear-gradient(155deg,#102746,#0b1b31);box-shadow:0 22px 70px rgba(0,0,0,.48);color:#f5f8ff}
      .fn-update-popover[hidden]{display:none!important}.fn-update-arrow{position:absolute;top:-7px;right:29px;width:13px;height:13px;transform:rotate(45deg);background:#102746;border-left:1px solid #31577d;border-top:1px solid #31577d}
      .fn-update-head{display:flex;align-items:center;justify-content:space-between;gap:12px;position:relative}.fn-update-title{display:flex;align-items:center;gap:9px;font-size:16px;font-weight:800}.fn-update-badge{display:grid;place-items:center;width:27px;height:27px;border-radius:50%;background:#31e5a2;color:#052319;font-size:18px;font-weight:900}
      .fn-update-close{appearance:none;border:0;background:transparent;color:#aac0dc;font-size:23px;line-height:1;cursor:pointer;padding:2px 4px}.fn-update-versions{display:grid;grid-template-columns:auto 1fr;gap:7px 13px;margin-top:16px;font-size:13px}.fn-update-versions span{color:#b3c2d6}.fn-update-versions strong{color:#f7f9fd}.fn-update-versions strong.available{color:#36e3a2}
      .fn-update-copy{margin:11px 0 0;color:#c6d2e2;font-size:12.5px;line-height:1.5}.fn-update-safe{display:flex;align-items:flex-start;gap:9px;margin-top:12px;padding:10px 11px;border:1px solid rgba(54,227,162,.32);border-radius:11px;background:rgba(20,112,81,.18);color:#d7faec;font-size:11.5px;line-height:1.4}.fn-update-safe b{display:block;color:#42e7aa}.fn-update-safe-icon{font-size:18px;line-height:1}
      .fn-update-status{margin-top:11px;padding:9px 10px;border:1px solid #294969;border-radius:10px;background:#09192b;color:#b9c9dc;font-size:11.5px;line-height:1.45;white-space:pre-line}.fn-update-status.ok{border-color:rgba(54,227,162,.32);color:#c9f6e4}.fn-update-status.bad{border-color:rgba(255,92,106,.38);color:#ffd1d6}.fn-update-progress{width:100%;margin-top:10px;accent-color:#36e3a2}
      .fn-update-actions{display:grid;grid-template-columns:1fr 1fr;gap:9px;margin-top:12px}.fn-update-actions button{min-height:40px;justify-content:center;text-align:center}.fn-update-actions .fn-update-apply{background:linear-gradient(180deg,#32e5a2,#20c88a);border-color:#4cf0b2;color:#06251a}.fn-update-actions .fn-update-apply:disabled{background:#173329;border-color:#28533f;color:#6f9b88}
      @media(max-width:600px){.fn-update-popover{right:12px!important;left:12px!important;width:auto!important}.fn-update-arrow{display:none}}
    `;
    document.head.appendChild(style);
  }

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
      <div class="fn-update-safe"><span class="fn-update-safe-icon">◆</span><span><b>Обновление безопасно</b>Backup, SHA-256, staging и проверка после перезапуска сохраняются. VPN, DNS и routing не меняются.</span></div>
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

  async function pollUpdate(target) {
    const deadline = Date.now() + 5 * 60 * 1000;
    while (updatePolling && Date.now() < deadline) {
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
    updatePolling = false;
    setProgress(false);
    setBusy(false);
    setStatus('Результат обновления пока неизвестен. Не запускайте обновление повторно до проверки фактической версии.', 'bad');
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
    mountRememberMe();
    ensurePopover();
    bindUpdateControl();
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mount, {once: true});
  else mount();
})();
