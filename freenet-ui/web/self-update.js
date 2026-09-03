(() => {
  const qs = (s, root = document) => root.querySelector(s);
  let plan = null;
  let polling = false;

  function stateText(state) {
    const labels = {
      IDLE: 'Готово к проверке',
      CHECKING: 'Проверяем релиз и SHA-256',
      SNAPSHOT: 'Создаём резервную копию',
      UPDATING: 'Устанавливаем обновление',
      RECONNECTING: 'FreeNet перезапускается и проверяет результат',
      SUCCESS: 'Обновление успешно установлено',
      FAILED: 'Обновление отменено, предыдущее состояние восстановлено',
      ROLLBACK_FAILED: 'Откат не подтверждён — дальнейшие изменения остановлены',
      BUSY: 'Обновление уже выполняется'
    };
    return labels[state] || state || 'Неизвестное состояние';
  }

  function mount() {
    const grid = qs('[data-page-view="system"] .grid-equal');
    if (!grid || grid.children.length < 2) return;
    const card = grid.children[1];
    card.innerHTML = `
      <div class="card-head">
        <h2>Обновление FreeNet</h2>
        <div id="webUpdateSummary" class="summary-state">Готово к проверке</div>
      </div>
      <div class="status-strip" style="margin-bottom:10px">
        <div class="status-pill"><b>Установлено</b><span id="webUpdateCurrent">—</span></div>
        <div class="status-pill"><b>Доступно</b><span id="webUpdateLatest">—</span></div>
        <div class="status-pill"><b>SHA-256</b><span id="webUpdateManifest">—</span></div>
      </div>
      <p class="hint" style="margin-top:0">FreeNet проверит точный GitHub Release, создаст backup, проверит SHA-256 и staging, обновит только FreeNet-owned файлы, перезапустит Control Center и подтвердит фактическую версию. XKeen/Xray, подписка и сетевые настройки этим действием не изменяются.</p>
      <div class="action-row">
        <button id="webUpdateCheckBtn" class="btn secondary" type="button">Проверить обновление</button>
        <button id="webUpdateApplyBtn" class="btn primary" type="button" disabled>Обновить</button>
      </div>
      <details class="details" id="webUpdateDetails"><summary>Что изменится</summary><div id="webUpdatePlan" class="notice"></div></details>
      <div id="webUpdateNotice" class="notice"></div>`;

    qs('#webUpdateCheckBtn').addEventListener('click', checkUpdate);
    qs('#webUpdateApplyBtn').addEventListener('click', applyUpdate);
    loadState();
  }

  function setSummary(text, type = '') {
    const n = qs('#webUpdateSummary');
    if (!n) return;
    n.textContent = text;
    n.className = 'summary-state' + (type ? ' ' + type : '');
  }

  function notice(text, type = '') {
    const n = qs('#webUpdateNotice');
    if (!n) return;
    n.textContent = text || '';
    n.className = text ? 'notice show' + (type ? ' ' + type : '') : 'notice';
  }

  function renderPlan(p) {
    plan = p;
    qs('#webUpdateCurrent').textContent = p.current_version || '—';
    qs('#webUpdateLatest').textContent = p.latest_version || '—';
    qs('#webUpdateManifest').textContent = p.manifest_verified ? 'проверен' : 'нет';
    const text = [
      p.update_available ? `Доступно обновление ${p.current_version} → ${p.latest_version}` : 'Установлена актуальная версия.',
      p.expected_delta ? `Изменится: ${p.expected_delta}` : '',
      p.expected_no_delta ? `Не изменится: ${p.expected_no_delta}` : ''
    ].filter(Boolean).join('\n');
    const box = qs('#webUpdatePlan');
    box.textContent = text;
    box.className = 'notice show';
    const apply = qs('#webUpdateApplyBtn');
    apply.disabled = !(p.success && p.ready && p.update_available && p.target_tag);
    apply.textContent = p.update_available && p.target_tag ? `Обновить до ${p.target_tag}` : 'Обновить';
    setSummary(p.update_available ? `Доступно ${p.latest_version}` : 'Актуальная версия', p.update_available ? '' : 'ok');
  }

  async function checkUpdate() {
    const btn = qs('#webUpdateCheckBtn');
    btn.disabled = true;
    qs('#webUpdateApplyBtn').disabled = true;
    notice('Проверяем последний подписанный релиз FreeNet…');
    setSummary('Проверяем…');
    try {
      const r = await fetch('/api/system/update/plan', {cache: 'no-store'});
      const p = await r.json();
      if (!r.ok || !p.success) throw new Error(p.error || 'Не удалось проверить обновление');
      renderPlan(p);
      notice(p.update_available ? `Обновление ${p.target_tag} готово к установке после вашего подтверждения.` : 'Установлена актуальная версия FreeNet.', 'ok');
    } catch (e) {
      setSummary('Проверка не удалась', 'bad');
      notice(e.message || 'Ошибка проверки обновления', 'bad');
    } finally {
      btn.disabled = false;
    }
  }

  async function applyUpdate() {
    if (!plan || !plan.update_available || !plan.target_tag) return;
    if (!window.confirm(`Установить ${plan.target_tag}? FreeNet создаст backup и кратко перезапустит только Control Center.`)) return;
    qs('#webUpdateCheckBtn').disabled = true;
    qs('#webUpdateApplyBtn').disabled = true;
    setSummary('Запускаем обновление…');
    notice('Запускаем transactional update. Страница может кратко потерять связь во время перезапуска FreeNet.');
    try {
      const r = await fetch('/api/system/update/apply', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({target_tag: plan.target_tag})
      });
      const j = await r.json();
      if (!r.ok || !j.success) throw new Error(j.error || 'Не удалось запустить обновление');
      polling = true;
      pollState(plan.target_tag);
    } catch (e) {
      setSummary('Не запущено', 'bad');
      notice(e.message || 'Ошибка запуска обновления', 'bad');
      qs('#webUpdateCheckBtn').disabled = false;
      qs('#webUpdateApplyBtn').disabled = false;
    }
  }

  async function loadState() {
    try {
      const r = await fetch('/api/system/update/state', {cache: 'no-store'});
      if (!r.ok) return;
      const s = await r.json();
      if (s.state && s.state !== 'IDLE') renderState(s);
    } catch (_) {}
  }

  function renderState(s) {
    const terminalGood = s.state === 'SUCCESS';
    const terminalBad = s.state === 'FAILED' || s.state === 'ROLLBACK_FAILED';
    setSummary(stateText(s.state), terminalGood ? 'ok' : terminalBad ? 'bad' : '');
    const lines = [stateText(s.state), s.message || '', s.primary_error ? `Основная ошибка: ${s.primary_error}` : '', s.rollback_state ? `Откат: ${s.rollback_state}` : ''].filter(Boolean);
    notice(lines.join('\n'), terminalGood ? 'ok' : terminalBad ? 'bad' : '');
  }

  async function pollState(target) {
    if (!polling) return;
    let state = null;
    try {
      const r = await fetch('/api/system/update/state', {cache: 'no-store'});
      if (r.ok) {
        state = await r.json();
        renderState(state);
        if (state.state === 'SUCCESS') {
          polling = false;
          await waitForVersion(target);
          return;
        }
        if (state.state === 'FAILED' || state.state === 'ROLLBACK_FAILED') {
          polling = false;
          qs('#webUpdateCheckBtn').disabled = false;
          return;
        }
      }
    } catch (_) {
      setSummary('FreeNet перезапускается…');
    }
    setTimeout(() => pollState(target), 1400);
  }

  async function waitForVersion(target) {
    for (let i = 0; i < 30; i++) {
      try {
        const r = await fetch('/versionz', {cache: 'no-store'});
        const v = (await r.text()).trim();
        if (r.ok && v === target) {
          notice(`FreeNet ${target} установлен и принят. Перезагружаем интерфейс…`, 'ok');
          setTimeout(() => location.reload(), 700);
          return;
        }
      } catch (_) {}
      await new Promise(resolve => setTimeout(resolve, 1000));
    }
    notice(`Обновление завершено, но браузер не подтвердил ${target}. Обновите страницу вручную.`, 'bad');
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mount);
  else mount();
})();
