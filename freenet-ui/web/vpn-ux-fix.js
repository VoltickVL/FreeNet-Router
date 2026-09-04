(() => {
  const qs = (s, root = document) => root.querySelector(s);
  let exactProfile = null;
  let exactPlan = null;
  let exactChecking = false;

  function profileCode(p) {
    const direct = String((p && p.country_code) || '').trim().toLowerCase();
    if (/^[a-z]{2}$/.test(direct)) return direct;
    const name = String((p && p.name) || '').trim();
    const m = name.match(/^([A-Za-z]{2})\b/);
    return m ? m[1].toLowerCase() : '';
  }

  function profileEndpoint(p) {
    if (typeof formatProfileEndpoint === 'function') return formatProfileEndpoint(p);
    if (p && p.endpoint) return String(p.endpoint);
    if (p && p.address && p.port) return `${p.address}:${p.port}`;
    return '—';
  }

  function selectedCardText(title, endpoint, note) {
    const card = qs('#selectedProfileCard');
    if (!card) return;
    card.textContent = '';
    const strong = document.createElement('strong');
    const ep = document.createElement('span');
    const hint = document.createElement('span');
    strong.textContent = title;
    ep.className = 'selected-endpoint';
    ep.textContent = endpoint || '—';
    hint.className = 'selected-note';
    hint.textContent = note || '';
    card.appendChild(strong);
    card.appendChild(ep);
    card.appendChild(hint);
  }

  function mountExactConnectControls() {
    const quick = qs('#quickActionsSection');
    const routine = quick && quick.querySelector('.action-row:not(#exactConnectRow)');
    if (!quick || !routine) return null;

    const updateBtn = qs('#updateBtn');
    const rotateBtn = qs('#rotateBtn');
    if (updateBtn) {
      updateBtn.textContent = 'Обновить текущий VPN-профиль';
      updateBtn.title = 'Получить свежие данные текущего активного VPN-профиля из подписки без намеренной смены сервера';
      updateBtn.classList.remove('primary');
      updateBtn.classList.add('secondary');
    }
    if (rotateBtn) {
      rotateBtn.textContent = 'Сменить сервер';
      rotateBtn.title = 'Выбрать другой endpoint внутри текущей активной группы';
      rotateBtn.classList.remove('primary');
      rotateBtn.classList.add('secondary');
    }

    let row = qs('#exactConnectRow');
    if (!row) {
      row = document.createElement('div');
      row.id = 'exactConnectRow';
      row.className = 'action-row';
      row.hidden = true;
      row.innerHTML = '<button id="exactConnectBtn" class="btn primary" type="button" disabled>Подключиться</button><button id="exactCancelBtn" class="btn secondary" type="button">Сбросить выбор</button>';
      routine.parentNode.insertBefore(row, routine);
      qs('#exactConnectBtn').addEventListener('click', connectExactProfile);
      qs('#exactCancelBtn').addEventListener('click', clearExactSelection);
    }
    return {row, routine, connect: qs('#exactConnectBtn')};
  }

  function showExactMode(enabled) {
    const controls = mountExactConnectControls();
    if (!controls) return;
    controls.row.hidden = !enabled;
    controls.routine.hidden = enabled;
  }

  function clearExactSelection() {
    exactProfile = null;
    exactPlan = null;
    exactChecking = false;
    selectedProviderID = '';
    selectedProviderName = '';
    providerPlanReady = false;
    if (typeof renderProfileOptions === 'function') renderProfileOptions();
    if (typeof renderSelectedProfile === 'function') renderSelectedProfile(null);
    showExactMode(false);
    if (typeof hideBox === 'function') hideBox('providerNotice');
  }

  async function selectExactProfile(p) {
    if (!p || !p.id || exactChecking || providerApplying) return;
    exactProfile = p;
    exactPlan = null;
    exactChecking = true;
    selectedProviderID = p.id;
    selectedProviderName = p.name || 'Extra-профиль';
    providerPlanReady = false;
    providerApplied = false;
    if (typeof resetSetupFinalizePlan === 'function') resetSetupFinalizePlan();
    if (typeof hideBox === 'function') hideBox('providerNotice');
    if (typeof closeProfileMenu === 'function') closeProfileMenu();
    if (typeof renderProfileOptions === 'function') renderProfileOptions();
    showExactMode(true);

    const controls = mountExactConnectControls();
    if (controls && controls.connect) {
      controls.connect.disabled = true;
      controls.connect.textContent = 'Проверяем…';
    }
    selectedCardText(`Выбрано: ${selectedProviderName}`, profileEndpoint(p), 'FreeNet автоматически проверяет сервер перед подключением. Дополнительных действий не требуется.');

    try {
      await loadNetworkPlan(selectedProviderID);
      const pp = lastNetworkPlan && lastNetworkPlan.provider_plan;
      if (!providerPlanReady || !pp || !pp.success || !pp.candidate_xray_valid || pp.mutation !== 'NONE' || pp.error) {
        const reason = (pp && pp.error) || 'сервер не прошёл безопасную read-only проверку';
        selectedCardText(`Не удалось подготовить: ${selectedProviderName}`, profileEndpoint(p), reason);
        if (controls && controls.connect) {
          controls.connect.disabled = true;
          controls.connect.textContent = 'Подключение недоступно';
        }
        if (typeof showBox === 'function') showBox('providerNotice', `Не удалось проверить выбранный VPN-сервер: ${reason}`, 'bad');
        return;
      }
      exactPlan = pp;
      selectedCardText(`Выбрано для подключения: ${selectedProviderName}`, pp.endpoint || profileEndpoint(p), 'Готово. Нажмите «Подключиться». Проверка выполнена автоматически.');
      if (controls && controls.connect) {
        controls.connect.disabled = false;
        controls.connect.textContent = 'Подключиться';
      }
    } catch (_) {
      selectedCardText(`Не удалось подготовить: ${selectedProviderName}`, profileEndpoint(p), 'Нет связи с FreeNet или подпиской. Текущий VPN не изменён.');
      if (controls && controls.connect) {
        controls.connect.disabled = true;
        controls.connect.textContent = 'Подключение недоступно';
      }
    } finally {
      exactChecking = false;
    }
  }

  async function waitExactState(expectedEndpoint, expectedCode) {
    let s = null;
    for (let i = 0; i < 30; i++) {
      try {
        s = await loadStatus();
      } catch (_) {
        s = null;
      }
      if (s) {
        const endpointOK = !expectedEndpoint || s.endpoint === expectedEndpoint;
        const countryOK = !expectedCode || s.country_code === expectedCode;
        if (endpointOK && countryOK && s.xray_online && s.dns_out_present) return s;
      }
      await new Promise(resolve => setTimeout(resolve, 850));
    }
    return s;
  }

  async function connectExactProfile() {
    if (!exactProfile || !exactPlan || providerApplying || exactChecking) return;
    const profileID = selectedProviderID;
    const p = exactProfile;
    const pp = exactPlan;
    const expectedEndpoint = pp.endpoint || profileEndpoint(p);
    const expectedCode = profileCode(p);
    const controls = mountExactConnectControls();

    providerApplying = true;
    if (controls && controls.connect) {
      controls.connect.disabled = true;
      controls.connect.textContent = 'Подключаем…';
    }
    if (typeof buttonsBusy === 'function') buttonsBusy(true);
    if (typeof hideBox === 'function') hideBox('notice');
    selectedCardText(`Подключаем: ${p.name || 'Extra-профиль'}`, expectedEndpoint, 'Применяем и подтверждаем фактическое состояние VPN…');

    try {
      const r = await fetch('/api/network-profile/apply', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({operation: 'provider', profile_id: profileID, confirm: true})
      });
      if (r.status === 401) {
        if (typeof loadAuthStatus === 'function') await loadAuthStatus();
        return;
      }
      const j = await r.json();
      if (!r.ok || !j.success) {
        const parts = [j.error || 'VPN-профиль не подключён'];
        if (j.primary_error) parts.push('Основная ошибка: ' + j.primary_error);
        if (j.rollback_state) parts.push('Откат: ' + j.rollback_state);
        selectedCardText(`Не подключено: ${p.name || 'Extra-профиль'}`, expectedEndpoint, parts.join(' · '));
        if (typeof showBox === 'function') showBox('notice', parts.join('\n'), 'bad');
        return;
      }

      const s = await waitExactState(expectedEndpoint, expectedCode);
      const accepted = !!(s && s.endpoint === expectedEndpoint && (!expectedCode || s.country_code === expectedCode) && s.xray_online && s.dns_out_present);
      if (!accepted) {
        const actual = s ? `${s.country || 'страна не определена'} · ${s.endpoint || 'endpoint неизвестен'}` : 'фактический статус недоступен';
        selectedCardText('Требуется проверка состояния', expectedEndpoint, `FreeNet завершил apply, но live-state не совпал: ${actual}. Повторное подключение автоматически не запускается.`);
        if (typeof showBox === 'function') showBox('notice', 'Фактическое состояние после подключения не подтверждено. Не повторяйте операцию вслепую.', 'bad');
        return;
      }

      providerApplied = true;
      exactProfile = null;
      exactPlan = null;
      selectedProviderID = '';
      selectedProviderName = '';
      providerPlanReady = false;
      if (typeof renderProfileOptions === 'function') renderProfileOptions();
      if (typeof renderSelectedProfile === 'function') renderSelectedProfile(null);
      showExactMode(false);
      if (typeof loadNetworkPlan === 'function') await loadNetworkPlan('');
      if (typeof showBox === 'function') showBox('notice', `Подключено: ${s.country || p.name || 'VPN'}${s.city ? ' · ' + s.city : ''}\n${s.endpoint}`, 'ok');
    } catch (_) {
      selectedCardText('Связь прервалась', expectedEndpoint, 'FreeNet мог кратко перезапустить VPN. Сначала дождитесь фактического статуса; повторное подключение автоматически не запускается.');
      if (typeof showBox === 'function') showBox('notice', 'Связь прервалась во время переключения. Проверяем фактическое состояние перед любым повтором.', 'bad');
    } finally {
      providerApplying = false;
      if (typeof buttonsBusy === 'function') buttonsBusy(!!(lastStatus && (lastStatus.busy || lastStatus.updater_busy)));
      const state = mountExactConnectControls();
      if (state && state.connect && exactProfile && exactPlan) {
        state.connect.disabled = false;
        state.connect.textContent = 'Подключиться';
      }
    }
  }

  function patchProfileSelection() {
    if (typeof selectProviderProfile !== 'function') return;
    selectProviderProfile = selectExactProfile;
  }

  function patchNavigation() {
    const vpnNav = qs('.nav-btn[data-page="vpn"]');
    if (vpnNav) vpnNav.remove();
    const vpnPage = qs('[data-page-view="vpn"]');
    if (vpnPage) vpnPage.remove();
    if (typeof pageLabels === 'object') delete pageLabels.vpn;
    if (location.hash === '#vpn' && typeof setPage === 'function') setPage('overview');
  }

  function mount() {
    patchNavigation();
    mountExactConnectControls();
    patchProfileSelection();
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mount);
  else mount();
})();