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
    selectedCardText(`Выбрано: ${selectedProviderName}`, profileEndpoint(p), 'FreeNet проверяет VPN-сервер перед подключением. ISP и DNS при этом не изменяются.');

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
      selectedCardText(`Выбрано для подключения: ${selectedProviderName}`, pp.endpoint || profileEndpoint(p), 'Готово. Нажмите «Подключиться». ISP и текущий DNS-режим сохранятся.');
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
        if (endpointOK && countryOK && s.xray_online) return s;
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
    selectedCardText(`Подключаем: ${p.name || 'Extra-профиль'}`, expectedEndpoint, 'Применяем VPN-профиль и подтверждаем фактический endpoint. ISP/DNS остаются без изменений.');

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
      const accepted = !!(s && s.endpoint === expectedEndpoint && (!expectedCode || s.country_code === expectedCode) && s.xray_online);
      if (!accepted) {
        const actual = s ? `${s.country || 'страна не определена'} · ${s.endpoint || 'endpoint неизвестен'}` : 'фактический статус недоступен';
        selectedCardText('Требуется проверка состояния', expectedEndpoint, `FreeNet завершил apply, но live-state VPN не совпал: ${actual}. Повторное подключение автоматически не запускается.`);
        if (typeof showBox === 'function') showBox('notice', 'Фактическое состояние VPN после подключения не подтверждено. Не повторяйте операцию вслепую.', 'bad');
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

  function ensureLegacyVPNStatusNodes() {
    if (qs('#vpnPageCurrent') && qs('#vpnPageEndpoint')) return;
    let compat = qs('#legacyVpnStatusCompat');
    if (!compat) {
      compat = document.createElement('div');
      compat.id = 'legacyVpnStatusCompat';
      compat.hidden = true;
      compat.innerHTML = '<span id="vpnPageCurrent"></span><div id="vpnPageEndpoint"><strong></strong></div><span id="providerSummaryLine"></span>';
      document.body.appendChild(compat);
    }
  }

  function patchStatusRendering() {
    if (typeof updateStatusViews !== 'function') return;
    const originalUpdateStatusViews = updateStatusViews;
    updateStatusViews = function(s) {
      ensureLegacyVPNStatusNodes();
      originalUpdateStatusViews(s);
      if (!s) return;

      const xrayDNS = s.dns_mode === 'xkeen';
      const dnsHealthy = xrayDNS ? !!s.dns_out_present : true;
      const healthy = !!s.xray_online && dnsHealthy;
      const dnsHealth = qs('#dnsHealth');
      const dnsState = qs('#dnsState');
      const topDot = qs('#topDot');
      const topStatus = qs('#topStatus');
      const systemHealth = qs('#systemHealth');
      const quickGuard = qs('#quickNetworkGuard');

      if (dnsState) {
        dnsState.textContent = xrayDNS ? (s.dns_out_present ? 'DNS через XKeen/Xray' : 'DNS требует внимания') : 'DNS напрямую';
      }
      if (dnsHealth) dnsHealth.className = 'health ' + (dnsHealthy ? 'ok' : 'bad');
      if (topDot) topDot.className = 'dot ok';
      if (topStatus) topStatus.textContent = 'FreeNet доступен';
      if (systemHealth) systemHealth.textContent = healthy ? 'Система работает' : 'Требует внимания';
      if (typeof setSummary === 'function') setSummary('quickActionState', s.country ? (s.country + ' · ' + (s.endpoint || '—')) : 'VPN не определён', healthy ? 'ok' : 'bad');
      if (quickGuard) {
        const dnsLabel = xrayDNS ? 'XKeen/Xray DNS' : 'DNS напрямую';
        quickGuard.textContent = 'VPN-действия не меняют ISP и DNS. Текущий DNS-режим: ' + dnsLabel + '.';
      }
    };
  }

  function patchNavigation() {
    const vpnNav = qs('.nav-btn[data-page="vpn"]');
    if (vpnNav) vpnNav.remove();
    const vpnPage = qs('[data-page-view="vpn"]');
    if (vpnPage) vpnPage.remove();
    if (typeof pageLabels === 'object') delete pageLabels.vpn;
    if (location.hash === '#vpn' && typeof setPage === 'function') setPage('overview');
  }

  let nativeEngineChoice = '';
  const nativeEngineLabels = {
    public: 'Публичные DNS-резолверы',
    interceptor: 'Системный DNS-перехват',
    nextdns: 'NextDNS',
    skydns: 'SkyDNS'
  };

  function mountNativeEngineConfirmation() {
    const networkHint = qs('#networkHint');
    if (!networkHint) return null;
    let box = qs('#nativeEngineConfirmation');
    if (!box) {
      box = document.createElement('div');
      box.id = 'nativeEngineConfirmation';
      box.className = 'field';
      box.hidden = true;
      box.style.marginTop = '10px';
      box.innerHTML = '<label for="nativeEngineSelect">Прежний native DNS engine</label><select id="nativeEngineSelect"><option value="">Выберите прежний режим</option></select><div class="hint">Одноразовая миграция старой конфигурации. FreeNet не угадывает потерянное состояние: выберите режим DNS-фильтра, который был активен до перехода на OPKG/Xray. После подтверждения FreeNet сохранит baseline и дальнейшие переключения будут автоматическими.</div>';
      networkHint.parentNode.insertBefore(box, networkHint.nextSibling);
      qs('#nativeEngineSelect').addEventListener('change', e => {
        nativeEngineChoice = String(e.target.value || '').trim();
        if (typeof renderNetworkControls === 'function') {
          renderNetworkControls(!!(lastStatus && (lastStatus.busy || lastStatus.updater_busy)));
        }
      });
    }
    return {box, select: qs('#nativeEngineSelect')};
  }

  function syncNativeEngineConfirmation() {
    const ui = mountNativeEngineConfirmation();
    if (!ui) return;
    const plan = typeof lastNetworkPlan !== 'undefined' ? lastNetworkPlan : null;
    const required = !!(plan && plan.native_filter_engine_confirm_required);
    ui.box.hidden = !required;
    if (!required) {
      nativeEngineChoice = '';
      ui.select.value = '';
      return;
    }

    const choices = Array.isArray(plan.native_filter_engine_choices) ? plan.native_filter_engine_choices : [];
    const currentOptions = Array.from(ui.select.options).slice(1).map(o => o.value).join(',');
    if (currentOptions !== choices.join(',')) {
      ui.select.innerHTML = '<option value="">Выберите прежний режим</option>';
      choices.forEach(value => {
        const option = document.createElement('option');
        option.value = value;
        option.textContent = nativeEngineLabels[value] || value;
        ui.select.appendChild(option);
      });
      if (choices.includes(nativeEngineChoice)) ui.select.value = nativeEngineChoice;
      else nativeEngineChoice = '';
    }

    const apply = qs('#applyNetworkBtn');
    if (apply && !nativeEngineChoice) apply.disabled = true;
    if (typeof setSummary === 'function' && !nativeEngineChoice) {
      setSummary('networkSummary', 'Нужно подтвердить прежний DNS engine');
    }
  }

  function patchNativeEngineMigrationFlow() {
    mountNativeEngineConfirmation();

    if (typeof loadNetworkPlan === 'function') {
      const originalLoadNetworkPlan = loadNetworkPlan;
      loadNetworkPlan = async function(...args) {
        const result = await originalLoadNetworkPlan(...args);
        syncNativeEngineConfirmation();
        return result;
      };
    }

    if (typeof renderNetworkControls === 'function') {
      const originalRenderNetworkControls = renderNetworkControls;
      renderNetworkControls = function(...args) {
        originalRenderNetworkControls(...args);
        syncNativeEngineConfirmation();
      };
    }

    const originalFetch = window.fetch.bind(window);
    window.fetch = function(input, init) {
      const url = typeof input === 'string' ? input : (input && input.url) || '';
      if (url === '/api/network-profile/apply' && init && String(init.method || '').toUpperCase() === 'POST' && typeof init.body === 'string') {
        try {
          const body = JSON.parse(init.body);
          const plan = typeof lastNetworkPlan !== 'undefined' ? lastNetworkPlan : null;
          if (body.operation === 'network' && plan && plan.native_filter_engine_confirm_required && nativeEngineChoice) {
            body.native_filter_engine = nativeEngineChoice;
            init = Object.assign({}, init, {body: JSON.stringify(body)});
          }
        } catch (_) {}
      }
      return originalFetch(input, init);
    };

    syncNativeEngineConfirmation();
  }

  const policyPreview = {
    mode: 'geosite',
    query: '',
    selected: null,
    action: 'DIRECT',
    files: []
  };

  function installPolicyPreviewStyles() {
    if (qs('#policyPreviewStyles')) return;
    const style = document.createElement('style');
    style.id = 'policyPreviewStyles';
    style.textContent = '.policy-preview{margin-top:14px}.policy-tabs,.policy-actions{display:flex;gap:8px;flex-wrap:wrap}.policy-tab,.policy-action{appearance:none;border:1px solid #2b405e;background:#0c1726;color:#a9b7ca;border-radius:10px;padding:9px 12px;cursor:pointer;font-weight:700;font-size:12px}.policy-tab.active,.policy-action.active{color:#fff;border-color:#5b8cff;background:#18325a}.policy-action[data-action="DIRECT"].active{border-color:#49da92;background:#123729}.policy-action[data-action="BLOCK"].active{border-color:#ff7070;background:#3a1d27}.policy-search-row{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:8px;margin-top:10px}.policy-search-row input{width:100%;border:1px solid #2b405e;background:#09121e;color:#f4f7fb;border-radius:11px;padding:11px 12px;outline:none}.policy-results{display:grid;gap:7px;margin-top:10px}.policy-result{appearance:none;width:100%;border:1px solid #26364d;background:#09121e;color:#f4f7fb;border-radius:11px;padding:10px 12px;text-align:left;cursor:pointer}.policy-result:hover,.policy-result.active{border-color:#5b8cff;background:#13243d}.policy-result b{display:block;font-size:12px}.policy-result span{display:block;color:#8fa0b8;font-size:10px;margin-top:3px}.policy-draft{margin-top:12px;padding:12px;border:1px solid #26364d;border-radius:12px;background:#09121e}.policy-draft-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:8px;margin-top:8px}.policy-leg{padding:9px;border-radius:10px;background:#0f1c2d;border:1px solid #20334d}.policy-leg b{display:block;font-size:11px}.policy-leg span{display:block;color:#8fa0b8;font-size:10px;margin-top:3px}.policy-muted{color:#8fa0b8;font-size:11px;line-height:1.45}.policy-badge{display:inline-flex;align-items:center;border:1px solid #334967;border-radius:999px;padding:4px 8px;color:#b9c7da;font-size:10px;font-weight:700}.policy-exact{margin-top:8px}.policy-readonly{color:#f1b84b;font-size:10px;font-weight:800;letter-spacing:.08em;text-transform:uppercase}@media(max-width:760px){.policy-search-row{grid-template-columns:1fr}.policy-draft-grid{grid-template-columns:1fr}}';
    document.head.appendChild(style);
  }

  function policyModeMeta() {
    return policyPreview.mode === 'geoip'
      ? {apiKind: 'geoip', exactKind: 'ip', placeholder: 'Например, 1.1.1.1', title: 'IP / GeoIP'}
      : {apiKind: 'geosite', exactKind: 'domain', placeholder: 'Например, plati.market или youtube', title: 'Домен / GeoSite'};
  }

  function policyOutbound(action) {
    if (action === 'DIRECT') return 'direct';
    if (action === 'VPN') return 'vless-reality';
    return 'block';
  }

  function policyDNSLeg(action) {
    if (action === 'DIRECT') return 'dns-direct';
    if (action === 'VPN') return 'dns-vless';
    return 'block';
  }

  function setPolicyNotice(text, bad = false) {
    const box = qs('#policyPreviewNotice');
    if (!box) return;
    box.textContent = text || '';
    box.className = 'notice' + (text ? ' show ' + (bad ? 'bad' : 'ok') : '');
  }

  function renderPolicyDraftPreview() {
    const box = qs('#policyDraftPreview');
    if (!box) return;
    box.textContent = '';
    if (!policyPreview.selected) {
      const empty = document.createElement('div');
      empty.className = 'policy-muted';
      empty.textContent = 'Выберите точное значение или найденную GeoSite/GeoIP category. Никакие конфиги пока не изменяются.';
      box.appendChild(empty);
      return;
    }

    const selected = policyPreview.selected;
    const title = document.createElement('div');
    const badge = document.createElement('span');
    badge.className = 'policy-badge';
    badge.textContent = `${selected.kind}:${selected.value}`;
    title.appendChild(badge);
    const grid = document.createElement('div');
    grid.className = 'policy-draft-grid';

    const selectorLeg = document.createElement('div');
    selectorLeg.className = 'policy-leg';
    selectorLeg.innerHTML = '<b>Правило</b>';
    const selectorValue = document.createElement('span');
    selectorValue.textContent = policyPreview.action;
    selectorLeg.appendChild(selectorValue);
    grid.appendChild(selectorLeg);

    const payload = document.createElement('div');
    payload.className = 'policy-leg';
    payload.innerHTML = '<b>Payload routing</b>';
    const payloadValue = document.createElement('span');
    payloadValue.textContent = policyOutbound(policyPreview.action);
    payload.appendChild(payloadValue);
    grid.appendChild(payload);

    const dns = document.createElement('div');
    dns.className = 'policy-leg';
    dns.innerHTML = '<b>Split DNS</b>';
    const dnsValue = document.createElement('span');
    const hasDNSLeg = selected.kind === 'domain' || selected.kind === 'geosite';
    dnsValue.textContent = hasDNSLeg ? policyDNSLeg(policyPreview.action) : 'не применяется к IP selector';
    dns.appendChild(dnsValue);
    grid.appendChild(dns);

    box.appendChild(title);
    box.appendChild(grid);
    const note = document.createElement('div');
    note.className = 'policy-muted';
    note.style.marginTop = '8px';
    note.textContent = selected.file ? `Источник category: ${selected.file}. Это browser-only draft; Apply/Save намеренно отсутствуют.` : 'Точное правило. Это browser-only draft; Apply/Save намеренно отсутствуют.';
    box.appendChild(note);
  }

  function renderPolicyActions() {
    document.querySelectorAll('.policy-action').forEach(btn => {
      btn.classList.toggle('active', btn.dataset.action === policyPreview.action);
    });
    renderPolicyDraftPreview();
  }

  function selectPolicyDraft(kind, value, file) {
    policyPreview.selected = {kind, value, file: file || ''};
    document.querySelectorAll('.policy-result').forEach(btn => {
      btn.classList.toggle('active', btn.dataset.kind === kind && btn.dataset.value === value && (btn.dataset.file || '') === (file || ''));
    });
    renderPolicyDraftPreview();
  }

  function renderPolicySearchResults(response) {
    const list = qs('#policySearchResults');
    if (!list) return;
    list.textContent = '';
    const matches = response && Array.isArray(response.matches) ? response.matches : [];
    let count = 0;
    matches.forEach(match => {
      const categories = Array.isArray(match.categories) ? match.categories : [];
      categories.forEach(category => {
        count++;
        const button = document.createElement('button');
        button.type = 'button';
        button.className = 'policy-result';
        button.dataset.kind = policyPreview.mode;
        button.dataset.value = String(category);
        button.dataset.file = String(match.file || '');
        const strong = document.createElement('b');
        strong.textContent = `${policyPreview.mode}:${category}`;
        const meta = document.createElement('span');
        meta.textContent = `Найдено в ${match.file || 'geodata'} · нажмите, чтобы выбрать category`;
        button.appendChild(strong);
        button.appendChild(meta);
        button.addEventListener('click', () => selectPolicyDraft(policyPreview.mode, String(category), String(match.file || '')));
        list.appendChild(button);
      });
    });
    if (!count) {
      const empty = document.createElement('div');
      empty.className = 'policy-muted';
      empty.textContent = 'Подходящих categories в выбранном типе geodata не найдено. Можно использовать точное значение.';
      list.appendChild(empty);
    }
    const warnings = response && Array.isArray(response.warnings) ? response.warnings : [];
    if (warnings.length) setPolicyNotice(warnings.join('\n'), true);
    else setPolicyNotice(`Поиск завершён. Найдено categories: ${count}. MUTATION: NONE`);
  }

  async function loadPolicyGeoFiles() {
    const state = qs('#policyGeoFilesState');
    try {
      const r = await fetch('/api/geodata/files');
      if (r.status === 401) {
        if (typeof loadAuthStatus === 'function') await loadAuthStatus();
        return;
      }
      const j = await r.json();
      if (!r.ok || !j.success) throw new Error(j.error || 'geodata unavailable');
      policyPreview.files = Array.isArray(j.files) ? j.files : [];
      const siteCount = policyPreview.files.filter(f => f.kind === 'geosite').length;
      const ipCount = policyPreview.files.filter(f => f.kind === 'geoip').length;
      const badCount = policyPreview.files.filter(f => f.error).length;
      if (state) state.textContent = `GeoSite: ${siteCount} · GeoIP: ${ipCount}${badCount ? ' · с ошибкой: ' + badCount : ''}`;
    } catch (_) {
      if (state) state.textContent = 'Geodata недоступна';
    }
  }

  async function runPolicyGeoSearch() {
    const input = qs('#policySearchInput');
    const searchBtn = qs('#policySearchBtn');
    if (!input || !searchBtn) return;
    const query = String(input.value || '').trim();
    if (!query) {
      setPolicyNotice('Введите домен/часть домена или literal IP.', true);
      return;
    }
    policyPreview.query = query;
    policyPreview.selected = null;
    renderPolicyDraftPreview();
    searchBtn.disabled = true;
    searchBtn.textContent = 'Ищем…';
    setPolicyNotice('');
    try {
      const kind = policyModeMeta().apiKind;
      const r = await fetch(`/api/geodata/search?kind=${encodeURIComponent(kind)}&q=${encodeURIComponent(query)}`);
      if (r.status === 401) {
        if (typeof loadAuthStatus === 'function') await loadAuthStatus();
        return;
      }
      const j = await r.json();
      if (!r.ok || !j.success) throw new Error(j.error || 'search failed');
      renderPolicySearchResults(j);
    } catch (e) {
      const list = qs('#policySearchResults');
      if (list) list.textContent = '';
      setPolicyNotice(`Поиск недоступен: ${e && e.message ? e.message : 'ошибка'}. Никакие настройки не изменены.`, true);
    } finally {
      searchBtn.disabled = false;
      searchBtn.textContent = 'Найти';
    }
  }

  function resetPolicyMode(mode) {
    policyPreview.mode = mode;
    policyPreview.selected = null;
    policyPreview.query = '';
    document.querySelectorAll('.policy-tab').forEach(btn => btn.classList.toggle('active', btn.dataset.mode === mode));
    const input = qs('#policySearchInput');
    const meta = policyModeMeta();
    if (input) {
      input.value = '';
      input.placeholder = meta.placeholder;
    }
    const list = qs('#policySearchResults');
    if (list) list.textContent = '';
    setPolicyNotice('');
    renderPolicyDraftPreview();
  }

  function mountPolicyPreview() {
    const networkPage = qs('[data-page-view="network"]');
    if (!networkPage || qs('#policyBuilderPreview')) return;
    installPolicyPreviewStyles();
    const existingCard = networkPage.querySelector('.card');
    if (!existingCard) return;

    const card = document.createElement('div');
    card.id = 'policyBuilderPreview';
    card.className = 'card policy-preview';
    card.innerHTML = '<div class="card-head"><div><h2>Конструктор правил</h2><div class="hint">Один selector станет основой и payload routing, и Split DNS там, где это технически применимо.</div></div><div><span class="policy-readonly">Preview · MUTATION: NONE</span><div id="policyGeoFilesState" class="summary-state" style="margin-top:5px">Проверяем geodata…</div></div></div><div class="policy-tabs"><button type="button" class="policy-tab active" data-mode="geosite">Домен / GeoSite</button><button type="button" class="policy-tab" data-mode="geoip">IP / GeoIP</button></div><div class="policy-search-row"><input id="policySearchInput" type="search" autocomplete="off" spellcheck="false" placeholder="Например, plati.market или youtube"><button id="policySearchBtn" class="btn secondary" type="button">Найти</button></div><div class="policy-exact"><button id="policyUseExactBtn" class="mini-btn" type="button">Использовать точное введённое значение</button></div><div id="policySearchResults" class="policy-results"></div><div id="policyPreviewNotice" class="notice"></div><div style="margin-top:14px"><div class="eyebrow">Действие</div><div class="policy-actions" style="margin-top:7px"><button type="button" class="policy-action active" data-action="DIRECT">DIRECT</button><button type="button" class="policy-action" data-action="VPN">VPN</button><button type="button" class="policy-action" data-action="BLOCK">BLOCK</button></div></div><div id="policyDraftPreview" class="policy-draft"></div>';
    existingCard.insertAdjacentElement('afterend', card);

    card.querySelectorAll('.policy-tab').forEach(btn => btn.addEventListener('click', () => resetPolicyMode(btn.dataset.mode)));
    card.querySelectorAll('.policy-action').forEach(btn => btn.addEventListener('click', () => {
      policyPreview.action = btn.dataset.action;
      renderPolicyActions();
    }));
    qs('#policySearchBtn').addEventListener('click', runPolicyGeoSearch);
    qs('#policySearchInput').addEventListener('keydown', e => {
      if (e.key === 'Enter') {
        e.preventDefault();
        runPolicyGeoSearch();
      }
    });
    qs('#policyUseExactBtn').addEventListener('click', () => {
      const input = qs('#policySearchInput');
      const value = String((input && input.value) || '').trim();
      if (!value) {
        setPolicyNotice('Сначала введите точное значение.', true);
        return;
      }
      const kind = policyModeMeta().exactKind;
      if (kind === 'ip' && !/^([0-9a-fA-F:.]+)$/.test(value)) {
        setPolicyNotice('Для IP / GeoIP введите literal IPv4/IPv6, а не домен.', true);
        return;
      }
      selectPolicyDraft(kind, value, '');
      setPolicyNotice('Точное правило добавлено только в browser draft. MUTATION: NONE');
    });

    renderPolicyDraftPreview();
    loadPolicyGeoFiles();
  }

  function mount() {
    patchNavigation();
    patchStatusRendering();
    mountExactConnectControls();
    patchProfileSelection();
    patchNativeEngineMigrationFlow();
    mountPolicyPreview();
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mount);
  else mount();
})();