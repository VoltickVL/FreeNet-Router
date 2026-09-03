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

  function mountUpdate() {
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
      <p class="hint" style="margin-top:0">FreeNet проверит точный GitHub Release, создаст резервную копию, проверит SHA-256 и staging, обновит только файлы FreeNet, перезапустит Control Center и подтвердит фактическую версию. XKeen/Xray, подписка и сетевые настройки этим действием не изменяются.</p>
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

  function setUpdateSummary(text, type = '') {
    const n = qs('#webUpdateSummary');
    if (!n) return;
    n.textContent = text;
    n.className = 'summary-state' + (type ? ' ' + type : '');
  }

  function updateNotice(text, type = '') {
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
    setUpdateSummary(p.update_available ? `Доступно ${p.latest_version}` : 'Актуальная версия', p.update_available ? '' : 'ok');
  }

  async function checkUpdate() {
    const btn = qs('#webUpdateCheckBtn');
    btn.disabled = true;
    qs('#webUpdateApplyBtn').disabled = true;
    updateNotice('Проверяем последний опубликованный релиз FreeNet…');
    setUpdateSummary('Проверяем…');
    try {
      const r = await fetch('/api/system/update/plan', {cache: 'no-store'});
      const p = await r.json();
      if (!r.ok || !p.success) throw new Error(p.error || 'Не удалось проверить обновление');
      renderPlan(p);
      updateNotice(p.update_available ? `Обновление ${p.target_tag} готово к установке после вашего подтверждения.` : 'Установлена актуальная версия FreeNet.', 'ok');
    } catch (e) {
      setUpdateSummary('Проверка не удалась', 'bad');
      updateNotice(e.message || 'Ошибка проверки обновления', 'bad');
    } finally {
      btn.disabled = false;
    }
  }

  async function applyUpdate() {
    if (!plan || !plan.update_available || !plan.target_tag) return;
    if (!window.confirm(`Установить ${plan.target_tag}? FreeNet создаст резервную копию и кратко перезапустит только Control Center.`)) return;
    qs('#webUpdateCheckBtn').disabled = true;
    qs('#webUpdateApplyBtn').disabled = true;
    setUpdateSummary('Запускаем обновление…');
    updateNotice('Запускаем безопасное обновление. Страница может кратко потерять связь во время перезапуска FreeNet.');
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
      setUpdateSummary('Не запущено', 'bad');
      updateNotice(e.message || 'Ошибка запуска обновления', 'bad');
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
    setUpdateSummary(stateText(s.state), terminalGood ? 'ok' : terminalBad ? 'bad' : '');
    const lines = [stateText(s.state), s.message || '', s.primary_error ? `Основная ошибка: ${s.primary_error}` : '', s.rollback_state ? `Откат: ${s.rollback_state}` : ''].filter(Boolean);
    updateNotice(lines.join('\n'), terminalGood ? 'ok' : terminalBad ? 'bad' : '');
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
      setUpdateSummary('FreeNet перезапускается…');
    }
    setTimeout(() => pollState(target), 1400);
  }

  async function waitForVersion(target) {
    for (let i = 0; i < 30; i++) {
      try {
        const r = await fetch('/versionz', {cache: 'no-store'});
        const v = (await r.text()).trim();
        if (r.ok && v === target) {
          updateNotice(`FreeNet ${target} установлен и принят. Перезагружаем интерфейс…`, 'ok');
          setTimeout(() => location.reload(), 700);
          return;
        }
      } catch (_) {}
      await new Promise(resolve => setTimeout(resolve, 1000));
    }
    updateNotice(`Обновление завершено, но браузер не подтвердил ${target}. Обновите страницу вручную.`, 'bad');
  }

  function mountNetworkDraftFlow() {
    const page = qs('[data-page-view="network"]');
    const isp = qs('#ispSelect');
    const dns = qs('#dnsModeSelect');
    const oldSave = qs('#saveNetworkBtn');
    const oldPlan = qs('#planNetworkBtn');
    const oldApply = qs('#applyNetworkBtn');
    if (!page || !isp || !dns || !oldPlan || !oldApply) return;

    const intro = qs('.page-head p', page);
    if (intro) intro.textContent = 'Выберите интернет-провайдера и DNS. Сначала FreeNet проверит изменения без записи настроек, затем применит их одной транзакцией.';
    if (oldSave) oldSave.hidden = true;
    const firmwareOption = dns.querySelector('option[value="firmware"]');
    if (firmwareOption) firmwareOption.textContent = 'DNS напрямую через роутер';
    if (window.dnsLabels) window.dnsLabels.firmware = 'DNS напрямую через роутер';

    const planButton = oldPlan.cloneNode(true);
    planButton.textContent = 'Проверить изменения';
    oldPlan.replaceWith(planButton);
    const applyButton = oldApply.cloneNode(true);
    applyButton.textContent = 'Применить';
    oldApply.replaceWith(applyButton);

    const draftParams = (profileID = '') => {
      const q = new URLSearchParams();
      q.set('isp', isp.value);
      q.set('dns_mode', dns.value);
      if (profileID) q.set('provider_profile_id', profileID);
      return q.toString();
    };

    window.renderNetworkControls = function(serverBusy = false) {
      const state = window.networkState();
      const save = qs('#saveNetworkBtn');
      const planBtn = qs('#planNetworkBtn');
      const applyBtn = qs('#applyNetworkBtn');
      if (save) save.hidden = true;
      if (planBtn) {
        planBtn.hidden = false;
        planBtn.disabled = serverBusy || window.networkChecking || window.networkApplying;
      }
      if (applyBtn) {
        applyBtn.hidden = state !== 'changes';
        applyBtn.disabled = serverBusy || state !== 'changes' || window.networkApplying;
      }
      if (state === 'active') window.setSummary('networkSummary', 'Выбранный профиль уже активен', 'ok');
      else if (state === 'changes') window.setSummary('networkSummary', 'Изменения проверены — можно применить');
      else if (state === 'blocked' || state === 'error') window.setSummary('networkSummary', 'Применение заблокировано', 'bad');
      else if (state === 'dirty') window.setSummary('networkSummary', 'Изменения ещё не проверены');
      else if (state === 'checking') window.setSummary('networkSummary', 'Проверяем изменения…');
      else window.setSummary('networkSummary', 'Проверьте выбранные настройки');
    };

    window.loadNetworkPlan = async function(profileID = window.selectedProviderID) {
      if (!window.authAuthenticated) return;
      window.networkChecking = true;
      window.networkPlanReady = false;
      window.networkPlanError = false;
      window.lastNetworkPlan = null;
      window.providerPlanReady = false;
      window.showBox('networkPlan', 'Проверяем выбранные настройки без сохранения и без изменений runtime…');
      window.buttonsBusy(!!(window.lastStatus && (window.lastStatus.busy || window.lastStatus.updater_busy)));
      try {
        const r = await fetch('/api/network-profile/plan?' + draftParams(profileID), {cache: 'no-store'});
        if (r.status === 401) {
          await window.loadAuthStatus();
          return;
        }
        const j = await r.json();
        if (!r.ok || !j.success) {
          window.networkPlanError = true;
          window.showBox('networkPlan', j.error || 'Не удалось проверить изменения', 'bad');
          window.renderExtraProfiles(j);
          if (profileID) window.showBox('providerPlan', 'Не удалось проверить выбранный VPN-профиль', 'bad');
          return;
        }
        window.networkDirty = false;
        window.lastNetworkPlan = j;
        window.networkPlanReady = !!(j.supported && !j.active);
        window.showBox('networkPlan', window.formatNetworkPlan(j), j.supported ? 'ok' : 'bad');
        window.renderExtraProfiles(j);
        if (profileID) {
          const pp = j.provider_plan;
          window.providerPlanReady = !!(pp && pp.success && pp.candidate_xray_valid && pp.mutation === 'NONE' && !pp.error);
          window.showBox('providerPlan', window.formatProviderPlan(pp), window.providerPlanReady ? 'ok' : 'bad');
          window.setSummary('providerSummary', window.providerPlanReady ? 'VPN-кандидат проверен' : 'VPN-кандидат заблокирован', window.providerPlanReady ? 'ok' : 'bad');
        } else {
          window.hideBox('providerPlan');
        }
      } catch (_) {
        window.networkPlanError = true;
        window.showBox('networkPlan', 'Не удалось проверить изменения: нет связи с FreeNet', 'bad');
        window.renderExtraProfiles(null);
        if (profileID) window.showBox('providerPlan', 'Нет связи при проверке VPN-профиля', 'bad');
      } finally {
        window.networkChecking = false;
        window.renderNetworkControls(!!(window.lastStatus && (window.lastStatus.busy || window.lastStatus.updater_busy)));
        window.buttonsBusy(!!(window.lastStatus && (window.lastStatus.busy || window.lastStatus.updater_busy)));
      }
    };

    async function applyDraft() {
      if (window.networkDirty || !window.networkPlanReady || window.networkApplying) return;
      const delta = (window.lastNetworkPlan && window.lastNetworkPlan.expected_delta) || 'выбранный сетевой профиль';
      if (!window.confirm('Применить проверенные настройки?\n\n' + delta + '\n\nFreeNet сначала сделает резервную копию. Активный ISP/DNS будет сохранён только после успешной проверки результата.')) return;
      window.networkApplying = true;
      window.buttonsBusy(true);
      window.showBox('networkNotice', 'Применяем и проверяем сетевые настройки…');
      try {
        const r = await fetch('/api/network-profile/apply', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({operation: 'network', isp: isp.value, dns_mode: dns.value, confirm: true})
        });
        if (r.status === 401) {
          await window.loadAuthStatus();
          return;
        }
        const j = await r.json();
        if (!r.ok || !j.success) {
          const parts = [j.error || 'Сетевые настройки не применены'];
          if (j.primary_error) parts.push('ОСНОВНАЯ ОШИБКА: ' + j.primary_error);
          if (j.rollback_state) parts.push('ОТКАТ: ' + j.rollback_state);
          if (j.rollback_state === 'FAILED/UNKNOWN') parts.push('Дальнейшие изменения остановлены до проверки фактического состояния.');
          window.showBox('networkNotice', parts.join('\n'), 'bad');
          return;
        }
        window.networkDirty = false;
        window.networkPlanReady = false;
        window.showBox('networkNotice', j.message || 'Сетевые настройки применены и проверены.', 'ok');
        const s = await window.loadStatus();
        if (s) {
          isp.value = s.isp || j.isp || isp.value;
          dns.value = s.dns_mode || j.dns_mode || dns.value;
        }
        await window.loadNetworkPlan(window.selectedProviderID);
      } catch (_) {
        window.networkPlanError = true;
        window.showBox('networkNotice', 'Связь с FreeNet прервалась во время применения. Не повторяйте операцию вслепую; сначала проверьте фактический статус.', 'bad');
      } finally {
        window.networkApplying = false;
        window.buttonsBusy(!!(window.lastStatus && (window.lastStatus.busy || window.lastStatus.updater_busy)));
      }
    }

    const markDraft = () => {
      window.networkDirty = true;
      window.networkPlanReady = false;
      window.networkPlanError = false;
      window.lastNetworkPlan = null;
      window.hideBox('networkNotice');
      window.hideBox('networkPlan');
      window.resetSetupFinalizePlan();
      window.renderNetworkControls(!!(window.lastStatus && (window.lastStatus.busy || window.lastStatus.updater_busy)));
    };

    planButton.addEventListener('click', () => window.loadNetworkPlan(window.selectedProviderID));
    applyButton.addEventListener('click', applyDraft);
    isp.addEventListener('change', markDraft);
    dns.addEventListener('change', markDraft);
    window.renderNetworkControls(false);
  }

  function mount() {
    mountUpdate();
    mountNetworkDraftFlow();
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mount);
  else mount();
})();