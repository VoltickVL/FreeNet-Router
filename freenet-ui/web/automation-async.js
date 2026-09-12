(() => {
  'use strict';

  const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
  const q = selector => document.querySelector(selector);
  let active = false;

  function setNotice(text, kind = '') {
    const node = q('#fnSettingsNotice');
    if (!node) return;
    node.textContent = text || '';
    node.classList.toggle('ok', kind === 'ok');
    node.classList.toggle('bad', kind === 'bad');
  }

  function localizeResult(value) {
    const key = String(value || '').trim().toLowerCase();
    const labels = {
      updated: 'Endpoint обновлён',
      switched: 'VPN переключён',
      success: 'Успешно',
      same: 'Без изменений',
      no_new: 'Без изменений',
      worse: 'Без изменений',
      ambiguous: 'Без изменений',
      uncertain: 'Без изменений',
      cooldown: 'Без изменений',
      candidate: 'Найден кандидат',
      failed: 'Ошибка'
    };
    return labels[key] || value || 'Проверка завершена';
  }

  function localizeReason(value) {
    const text = String(value || '').trim();
    const map = new Map([
      ['fresh subscription has no endpoint for the current logical profile', 'В свежей подписке нет нового endpoint для текущего logical-профиля.'],
      ['current endpoint is already actual; update is not required', 'Текущий endpoint уже актуален; обновление не требуется.'],
      ['fresh subscription contains multiple matches for the current logical profile; no endpoint was chosen', 'Для текущего logical-профиля найдено несколько неоднозначных вариантов; изменений нет.'],
      ['fresh endpoint did not pass bounded TCP preflight', 'Новый endpoint не прошёл предварительную проверку; текущий сохранён.'],
      ['fresh endpoint is materially slower on bounded preflight; current endpoint preserved', 'Новый endpoint заметно хуже текущего; переключение не выполнялось.']
    ]);
    return map.get(text) || text;
  }

  async function readJSON(response) {
    const type = (response.headers.get('content-type') || '').toLowerCase();
    if (!type.includes('application/json')) {
      throw new Error(`FreeNet вернул некорректный ответ HTTP ${response.status}. Проверьте состояние операции через журнал.`);
    }
    return response.json();
  }

  async function startCheck() {
    const response = await fetch('/api/automation', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({action: 'check'})
    });
    const data = await readJSON(response);
    if (!response.ok && response.status !== 202) {
      throw new Error(data.error || 'Не удалось запустить AUTO VPN проверку.');
    }
    return data;
  }

  function renderProgress(button, startedAt) {
    const elapsed = Math.max(0, Math.floor((Date.now() - startedAt) / 1000));
    button.textContent = `Проверяем… ${elapsed} с`;
  }

  async function pollCheck(button, startedAt) {
    const deadline = Date.now() + 330000;
    while (Date.now() < deadline) {
      renderProgress(button, startedAt);
      await sleep(1500);
      const response = await fetch('/api/automation/check', {cache: 'no-store'});
      const data = await readJSON(response);
      if (!response.ok) throw new Error(data.error || 'Не удалось получить состояние AUTO VPN проверки.');
      if (data.active) continue;
      if (!data.success) throw new Error(data.error || data.operation?.error || 'AUTO VPN проверка завершилась ошибкой.');
      return data;
    }
    throw new Error('AUTO VPN проверка ещё выполняется дольше ожидаемого. Результат сохранится в журнале после завершения.');
  }

  function qualityJobID() {
    const raw = globalThis.crypto?.randomUUID?.().replace(/-/g, '') || `${Date.now()}${Math.random().toString(16).slice(2)}`;
    return `current_${raw}`.slice(0, 48);
  }

  async function refreshCurrentQualityAfterSwitch(reason) {
    const id = qualityJobID();
    try {
      const startResponse = await fetch(`/api/vpn/current-quality?job=start&id=${encodeURIComponent(id)}`, {cache: 'no-store'});
      const started = await readJSON(startResponse);
      if (!startResponse.ok && startResponse.status !== 202) {
        throw new Error(started.error || 'Не удалось запустить проверку нового текущего VPN.');
      }
      setNotice(`VPN переключён: ${reason || 'новый профиль применён'}. Проверяем метрики нового текущего VPN в фоне…`, 'ok');
      const deadline = Date.now() + 90000;
      while (Date.now() < deadline) {
        await sleep(1500);
        const response = await fetch(`/api/vpn/current-quality?job=status&id=${encodeURIComponent(id)}`, {cache: 'no-store'});
        const job = await readJSON(response);
        if (!response.ok) throw new Error(job.error || 'Не удалось получить метрики нового текущего VPN.');
        if (job.state === 'running') continue;
        if (job.state === 'failed') throw new Error(job.error || 'Проверка нового текущего VPN завершилась ошибкой.');
        setNotice(`VPN переключён: ${reason || 'новый профиль применён'}. Метрики нового текущего VPN подтверждены.`, 'ok');
        return;
      }
      throw new Error('Проверка метрик нового текущего VPN выполняется дольше ожидаемого.');
    } catch (_) {
      setNotice(`VPN переключён: ${reason || 'новый профиль применён'}. Метрики нового VPN можно обновить кнопкой «Проверить текущий VPN» на Обзоре.`, 'ok');
    }
  }

  async function runCheck(button) {
    if (active) return;
    active = true;
    const oldDisabled = button.disabled;
    const oldHTML = button.innerHTML;
    const oldAriaBusy = button.getAttribute('aria-busy');
    const startedAt = Date.now();
    button.disabled = true;
    button.setAttribute('aria-busy', 'true');
    setNotice('');
    renderProgress(button, startedAt);
    try {
      const started = await startCheck();
      if (started.active === false) {
        setNotice('AUTO VPN проверка завершена.', 'ok');
        return;
      }
      const done = await pollCheck(button, startedAt);
      const snapshot = done.automation || {};
      const result = localizeResult(snapshot.last_result);
      const reason = localizeReason(snapshot.last_reason);
      setNotice(reason ? `${result}: ${reason}` : result, 'ok');
      if (String(snapshot.last_result || '').toLowerCase() === 'switched') {
        void refreshCurrentQualityAfterSwitch(reason);
      }
    } catch (error) {
      setNotice(error?.message || 'AUTO VPN проверка завершилась ошибкой.', 'bad');
    } finally {
      active = false;
      button.disabled = oldDisabled;
      button.innerHTML = oldHTML;
      if (oldAriaBusy === null) button.removeAttribute('aria-busy');
      else button.setAttribute('aria-busy', oldAriaBusy);
    }
  }

  function hideLegacyProviderFact() {
    const value = q('#topISPValue');
    const fact = value?.closest?.('.overview-approved-fact');
    if (fact) fact.remove();
  }

  function settingsRouteRequested() {
    const hash = location.hash.slice(1);
    const path = location.pathname.replace(/\/+$/, '').split('/').pop();
    return hash === 'settings' || path === 'settings' || !!q('[data-page-view="settings"].active');
  }

  function ensureSettingsV3VisibilityGuard() {
    if (q('#freenetSettingsV3VisibilityGuard')) return;
    const style = document.createElement('style');
    style.id = 'freenetSettingsV3VisibilityGuard';
    style.textContent = '.fn3-page:not(.active){display:none!important}';
    document.head.appendChild(style);
  }

  function prepareSettingsRoute() {
    hideLegacyProviderFact();
    ensureSettingsV3VisibilityGuard();
    if (!settingsRouteRequested()) return;
    const page = q('[data-page-view="settings"]');
    if (page && !page.classList.contains('active') && typeof window.setPage === 'function') {
      window.setPage('settings');
    }
  }

  function settleSettingsV3(attempt = 0) {
    hideLegacyProviderFact();
    const page = q('[data-page-view="settings"]');
    if (page?.dataset.settingsV3 === '1') {
      if (settingsRouteRequested() && !page.classList.contains('active') && typeof window.setPage === 'function') {
        window.setPage('settings');
      }
      return;
    }
    if (attempt < 30) setTimeout(() => settleSettingsV3(attempt + 1), 50);
  }

  function bridgeSettingsNavigation() {
    try {
      if (typeof pageLabels === 'object' && pageLabels) {
        pageLabels.journal = 'Журнал';
        delete pageLabels.system;
      }
    } catch (_) {}
    prepareSettingsRoute();
    setTimeout(() => settleSettingsV3(), 0);
    document.addEventListener('click', event => {
      const button = event.target?.closest?.('.nav-btn[data-page="settings"]');
      if (!button) return;
      setTimeout(() => {
        hideLegacyProviderFact();
        window.dispatchEvent(new Event('hashchange'));
        settleSettingsV3();
      }, 0);
    });
    window.addEventListener('hashchange', () => {
      if (settingsRouteRequested()) setTimeout(() => settleSettingsV3(), 0);
    });
  }

  document.addEventListener('click', event => {
    const button = event.target?.closest?.('#fnCheckNow');
    if (!button) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    runCheck(button);
  }, true);

  bridgeSettingsNavigation();
})();
