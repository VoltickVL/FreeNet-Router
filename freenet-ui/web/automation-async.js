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
    setNotice(`AUTO VPN: идёт проверка · ${elapsed} с. Операция выполняется в фоне; повторно запускать её не нужно.`);
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

  async function runCheck(button) {
    if (active) return;
    active = true;
    const oldDisabled = button.disabled;
    const oldHTML = button.innerHTML;
    const oldAriaBusy = button.getAttribute('aria-busy');
    const startedAt = Date.now();
    button.disabled = true;
    button.setAttribute('aria-busy', 'true');
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

  document.addEventListener('click', event => {
    const button = event.target?.closest?.('#fnCheckNow');
    if (!button) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    runCheck(button);
  }, true);
})();
