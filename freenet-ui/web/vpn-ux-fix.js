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
      updateBtn.textContent = 'Обновить профиль';
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

  async function waitExactState(expectedEndpoint) {
    let s = null;
    for (let i = 0; i < 30; i++) {
      try {
        s = await loadStatus();
      } catch (_) {
        s = null;
      }
      if (s) {
        const endpointOK = expectedEndpoint && s.endpoint === expectedEndpoint;
        if (endpointOK && !s.busy && !s.updater_busy && s.xray_online) return s;
      }
      await new Promise(resolve => setTimeout(resolve, 850));
    }
    return null;
  }

  async function connectExactProfile() {
    if (!exactProfile || !exactPlan || providerApplying || exactChecking) return;
    const profileID = selectedProviderID;
    const p = exactProfile;
    const pp = exactPlan;
    const expectedEndpoint = pp.endpoint || profileEndpoint(p);
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
        selectedCardText(`${j.result_unknown ? 'Результат не подтверждён' : 'Не подключено'}: ${p.name || 'Extra-профиль'}`, expectedEndpoint, parts.join(' · '));
        if (typeof showBox === 'function') showBox('notice', parts.join('\n'), 'bad');
        return;
      }

      const s = await waitExactState(expectedEndpoint);
      const accepted = !!(s && s.endpoint === expectedEndpoint && !s.busy && !s.updater_busy && s.xray_online);
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
      if (typeof showBox === 'function') showBox('notice', `Подключено: ${s.profile_label || p.name || s.country || 'VPN'}${s.city ? ' · ' + s.city : ''}\n${s.endpoint}`, 'ok');
    } catch (_) {
      selectedCardText('Связь прервалась', expectedEndpoint, 'FreeNet мог кратко перезапустить VPN. Сначала дождитесь фактического статуса; повторное подключение автоматически не запускается.');
      if (typeof showBox === 'function') showBox('notice', 'Связь прервалась во время переключения. Проверяем фактическое состояние перед любым повтором.', 'bad');
    } finally {
      exactProfile = null;
      exactPlan = null;
      providerPlanReady = false;
      providerApplying = false;
      if (typeof buttonsBusy === 'function') buttonsBusy(!!(lastStatus && (lastStatus.busy || lastStatus.updater_busy)));
      const state = mountExactConnectControls();
      if (state && state.connect) {
        state.connect.disabled = true;
        state.connect.textContent = 'Выберите сервер заново';
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

      if (dnsState) dnsState.textContent = xrayDNS ? (s.dns_out_present ? 'DNS через XKeen/Xray' : 'DNS требует внимания') : 'DNS напрямую';
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
    public: 'Публичные DNS-резолверы', interceptor: 'Системный DNS-перехват', nextdns: 'NextDNS', skydns: 'SkyDNS'
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
        if (typeof renderNetworkControls === 'function') renderNetworkControls(!!(lastStatus && (lastStatus.busy || lastStatus.updater_busy)));
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
    if (!required) { nativeEngineChoice = ''; ui.select.value = ''; return; }
    const choices = Array.isArray(plan.native_filter_engine_choices) ? plan.native_filter_engine_choices : [];
    const currentOptions = Array.from(ui.select.options).slice(1).map(o => o.value).join(',');
    if (currentOptions !== choices.join(',')) {
      ui.select.innerHTML = '<option value="">Выберите прежний режим</option>';
      choices.forEach(value => {
        const option = document.createElement('option'); option.value = value; option.textContent = nativeEngineLabels[value] || value; ui.select.appendChild(option);
      });
      if (choices.includes(nativeEngineChoice)) ui.select.value = nativeEngineChoice; else nativeEngineChoice = '';
    }
    const apply = qs('#applyNetworkBtn');
    if (apply && !nativeEngineChoice) apply.disabled = true;
    if (typeof setSummary === 'function' && !nativeEngineChoice) setSummary('networkSummary', 'Нужно подтвердить прежний DNS engine');
  }

  function patchNativeEngineMigrationFlow() {
    mountNativeEngineConfirmation();
    if (typeof loadNetworkPlan === 'function') {
      const originalLoadNetworkPlan = loadNetworkPlan;
      loadNetworkPlan = async function(...args) { const result = await originalLoadNetworkPlan(...args); syncNativeEngineConfirmation(); return result; };
    }
    if (typeof renderNetworkControls === 'function') {
      const originalRenderNetworkControls = renderNetworkControls;
      renderNetworkControls = function(...args) { originalRenderNetworkControls(...args); syncNativeEngineConfirmation(); };
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

  const policyPreview = {mode: 'geosite', query: '', selected: null, action: 'DIRECT', files: []};

  function installPolicyPreviewStyles() {
    if (qs('#policyPreviewStyles')) return;
    const style = document.createElement('style');
    style.id = 'policyPreviewStyles';
    style.textContent = '.policy-preview{margin-top:14px}.policy-tabs,.policy-actions{display:flex;gap:8px;flex-wrap:wrap}.policy-tab,.policy-action{appearance:none;border:1px solid #2b405e;background:#0c1726;color:#a9b7ca;border-radius:10px;padding:9px 12px;cursor:pointer;font-weight:700;font-size:12px}.policy-tab.active,.policy-action.active{color:#fff;border-color:#5b8cff;background:#18325a}.policy-action[data-action="DIRECT"].active{border-color:#49da92;background:#123729}.policy-action[data-action="BLOCK"].active{border-color:#ff7070;background:#3a1d27}.policy-search-row{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:8px;margin-top:10px}.policy-search-row input{width:100%;border:1px solid #2b405e;background:#09121e;color:#f4f7fb;border-radius:11px;padding:11px 12px;outline:none}.policy-results{display:grid;gap:7px;margin-top:10px}.policy-result{appearance:none;width:100%;border:1px solid #26364d;background:#09121e;color:#f4f7fb;border-radius:11px;padding:10px 12px;text-align:left;cursor:pointer}.policy-result:hover,.policy-result.active{border-color:#5b8cff;background:#13243d}.policy-result b{display:block;font-size:12px}.policy-result span{display:block;color:#8fa0b8;font-size:10px;margin-top:3px}.policy-draft{margin-top:12px;padding:12px;border:1px solid #26364d;border-radius:12px;background:#09121e}.policy-draft-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:8px;margin-top:8px}.policy-leg{padding:9px;border-radius:10px;background:#0f1c2d;border:1px solid #20334d}.policy-leg b{display:block;font-size:11px}.policy-leg span{display:block;color:#8fa0b8;font-size:10px;margin-top:3px}.policy-muted{color:#8fa0b8;font-size:11px;line-height:1.45}.policy-badge{display:inline-flex;align-items:center;border:1px solid #334967;border-radius:999px;padding:4px 8px;color:#b9c7da;font-size:10px;font-weight:700}.policy-exact{margin-top:8px}.policy-readonly{color:#f1b84b;font-size:10px;font-weight:800;letter-spacing:.08em;text-transform:uppercase}@media(max-width:760px){.policy-search-row{grid-template-columns:1fr}.policy-draft-grid{grid-template-columns:1fr}}';
    document.head.appendChild(style);
  }

  function policyModeMeta() { return policyPreview.mode === 'geoip' ? {apiKind:'geoip',exactKind:'ip',placeholder:'Например, 1.1.1.1',title:'IP / GeoIP'} : {apiKind:'geosite',exactKind:'domain',placeholder:'Например, plati.market или youtube',title:'Домен / GeoSite'}; }
  function policyOutbound(action) { if (action === 'DIRECT') return 'direct'; if (action === 'VPN') return 'vless-reality'; return 'block'; }
  function policyDNSLeg(action) { if (action === 'DIRECT') return 'dns-direct'; if (action === 'VPN') return 'dns-vless'; return 'block'; }
  function setPolicyNotice(text,bad=false){const box=qs('#policyPreviewNotice');if(!box)return;box.textContent=text||'';box.className='notice'+(text?' show '+(bad?'bad':'ok'):'');}

  function renderPolicyDraftPreview() {
    const box=qs('#policyDraftPreview'); if(!box)return; box.textContent='';
    if(!policyPreview.selected){const empty=document.createElement('div');empty.className='policy-muted';empty.textContent='Выберите точное значение или найденную GeoSite/GeoIP category. Никакие конфиги пока не изменяются.';box.appendChild(empty);return;}
    const selected=policyPreview.selected; const title=document.createElement('div'); const badge=document.createElement('span'); badge.className='policy-badge'; badge.textContent=`${selected.kind}:${selected.value}`; title.appendChild(badge);
    const grid=document.createElement('div');grid.className='policy-draft-grid';
    const selectorLeg=document.createElement('div');selectorLeg.className='policy-leg';selectorLeg.innerHTML='<b>Правило</b>';const selectorValue=document.createElement('span');selectorValue.textContent=policyPreview.action;selectorLeg.appendChild(selectorValue);grid.appendChild(selectorLeg);
    const payload=document.createElement('div');payload.className='policy-leg';payload.innerHTML='<b>Payload routing</b>';const payloadValue=document.createElement('span');payloadValue.textContent=policyOutbound(policyPreview.action);payload.appendChild(payloadValue);grid.appendChild(payload);
    const dns=document.createElement('div');dns.className='policy-leg';dns.innerHTML='<b>Split DNS</b>';const dnsValue=document.createElement('span');const hasDNSLeg=selected.kind==='domain'||selected.kind==='geosite';dnsValue.textContent=hasDNSLeg?policyDNSLeg(policyPreview.action):'не применяется к IP selector';dns.appendChild(dnsValue);grid.appendChild(dns);
    box.appendChild(title);box.appendChild(grid);const note=document.createElement('div');note.className='policy-muted';note.style.marginTop='8px';note.textContent=selected.file?`Источник category: ${selected.file}. Это browser-only draft; Apply/Save намеренно отсутствуют.`:'Точное правило. Это browser-only draft; Apply/Save намеренно отсутствуют.';box.appendChild(note);
  }

  function renderPolicyActions(){document.querySelectorAll('.policy-action').forEach(btn=>btn.classList.toggle('active',btn.dataset.action===policyPreview.action));renderPolicyDraftPreview();}
  function selectPolicyDraft(kind,value,file){policyPreview.selected={kind,value,file:file||''};document.querySelectorAll('.policy-result').forEach(btn=>btn.classList.toggle('active',btn.dataset.kind===kind&&btn.dataset.value===value&&(btn.dataset.file||'')===(file||'')));renderPolicyDraftPreview();}

  function renderPolicySearchResults(response){
    const list=qs('#policySearchResults');if(!list)return;list.textContent='';const matches=response&&Array.isArray(response.matches)?response.matches:[];let count=0;
    matches.forEach(match=>{const categories=Array.isArray(match.categories)?match.categories:[];categories.forEach(category=>{count++;const button=document.createElement('button');button.type='button';button.className='policy-result';button.dataset.kind=policyPreview.mode;button.dataset.value=String(category);button.dataset.file=String(match.file||'');const strong=document.createElement('b');strong.textContent=`${policyPreview.mode}:${category}`;const meta=document.createElement('span');meta.textContent=`Найдено в ${match.file||'geodata'} · нажмите, чтобы выбрать category`;button.appendChild(strong);button.appendChild(meta);button.addEventListener('click',()=>selectPolicyDraft(policyPreview.mode,String(category),String(match.file||'')));list.appendChild(button);});});
    if(!count){const empty=document.createElement('div');empty.className='policy-muted';empty.textContent='Подходящих categories в выбранном типе geodata не найдено. Можно использовать точное значение.';list.appendChild(empty);}const warnings=response&&Array.isArray(response.warnings)?response.warnings:[];if(warnings.length)setPolicyNotice(warnings.join('\n'),true);else setPolicyNotice(`Поиск завершён. Найдено categories: ${count}. MUTATION: NONE`);
  }

  async function loadPolicyGeoFiles(){const state=qs('#policyGeoFilesState');try{const r=await fetch('/api/geodata/files');if(r.status===401){if(typeof loadAuthStatus==='function')await loadAuthStatus();return;}const j=await r.json();if(!r.ok||!j.success)throw new Error(j.error||'geodata unavailable');policyPreview.files=Array.isArray(j.files)?j.files:[];const siteCount=policyPreview.files.filter(f=>f.kind==='geosite').length;const ipCount=policyPreview.files.filter(f=>f.kind==='geoip').length;const badCount=policyPreview.files.filter(f=>f.error).length;if(state)state.textContent=`GeoSite: ${siteCount} · GeoIP: ${ipCount}${badCount?' · с ошибкой: '+badCount:''}`;}catch(_){if(state)state.textContent='Geodata недоступна';}}

  async function runPolicyGeoSearch(){const input=qs('#policySearchInput');const searchBtn=qs('#policySearchBtn');if(!input||!searchBtn)return;const query=String(input.value||'').trim();if(!query){setPolicyNotice('Введите домен/часть домена или literal IP.',true);return;}policyPreview.query=query;policyPreview.selected=null;renderPolicyDraftPreview();searchBtn.disabled=true;searchBtn.textContent='Ищем…';setPolicyNotice('');try{const kind=policyModeMeta().apiKind;const r=await fetch(`/api/geodata/search?kind=${encodeURIComponent(kind)}&q=${encodeURIComponent(query)}`);if(r.status===401){if(typeof loadAuthStatus==='function')await loadAuthStatus();return;}const j=await r.json();if(!r.ok||!j.success)throw new Error(j.error||'search failed');renderPolicySearchResults(j);}catch(e){const list=qs('#policySearchResults');if(list)list.textContent='';setPolicyNotice(`Поиск недоступен: ${e&&e.message?e.message:'ошибка'}. Никакие настройки не изменены.`,true);}finally{searchBtn.disabled=false;searchBtn.textContent='Найти';}}

  function resetPolicyMode(mode){policyPreview.mode=mode;policyPreview.selected=null;policyPreview.query='';document.querySelectorAll('.policy-tab').forEach(btn=>btn.classList.toggle('active',btn.dataset.mode===mode));const input=qs('#policySearchInput');const meta=policyModeMeta();if(input){input.value='';input.placeholder=meta.placeholder;}const list=qs('#policySearchResults');if(list)list.textContent='';setPolicyNotice('');renderPolicyDraftPreview();}

  function mountPolicyPreview(){
    const networkPage=qs('[data-page-view="network"]');if(!networkPage||qs('#policyBuilderPreview'))return;installPolicyPreviewStyles();const existingCard=networkPage.querySelector('.card');if(!existingCard)return;
    const card=document.createElement('div');card.id='policyBuilderPreview';card.className='card policy-preview';card.innerHTML='<div class="card-head"><div><h2>Конструктор правил</h2><div class="hint">Один selector станет основой и payload routing, и Split DNS там, где это технически применимо.</div></div><div><span class="policy-readonly">Preview · MUTATION: NONE</span><div id="policyGeoFilesState" class="summary-state" style="margin-top:5px">Проверяем geodata…</div></div></div><div class="policy-tabs"><button type="button" class="policy-tab active" data-mode="geosite">Домен / GeoSite</button><button type="button" class="policy-tab" data-mode="geoip">IP / GeoIP</button></div><div class="policy-search-row"><input id="policySearchInput" type="search" autocomplete="off" spellcheck="false" placeholder="Например, plati.market или youtube"><button id="policySearchBtn" class="btn secondary" type="button">Найти</button></div><div class="policy-exact"><button id="policyUseExactBtn" class="mini-btn" type="button">Использовать точное введённое значение</button></div><div id="policySearchResults" class="policy-results"></div><div id="policyPreviewNotice" class="notice"></div><div style="margin-top:14px"><div class="eyebrow">Действие</div><div class="policy-actions" style="margin-top:7px"><button type="button" class="policy-action active" data-action="DIRECT">DIRECT</button><button type="button" class="policy-action" data-action="VPN">VPN</button><button type="button" class="policy-action" data-action="BLOCK">BLOCK</button></div></div><div id="policyDraftPreview" class="policy-draft"></div>';existingCard.insertAdjacentElement('afterend',card);
    card.querySelectorAll('.policy-tab').forEach(btn=>btn.addEventListener('click',()=>resetPolicyMode(btn.dataset.mode)));card.querySelectorAll('.policy-action').forEach(btn=>btn.addEventListener('click',()=>{policyPreview.action=btn.dataset.action;renderPolicyActions();}));qs('#policySearchBtn').addEventListener('click',runPolicyGeoSearch);qs('#policySearchInput').addEventListener('keydown',e=>{if(e.key==='Enter'){e.preventDefault();runPolicyGeoSearch();}});qs('#policyUseExactBtn').addEventListener('click',()=>{const input=qs('#policySearchInput');const value=String((input&&input.value)||'').trim();if(!value){setPolicyNotice('Сначала введите точное значение.',true);return;}const kind=policyModeMeta().exactKind;if(kind==='ip'&&!/^([0-9a-fA-F:.]+)$/.test(value)){setPolicyNotice('Для IP / GeoIP введите literal IPv4/IPv6, а не домен.',true);return;}selectPolicyDraft(kind,value,'');setPolicyNotice('Точное правило добавлено только в browser draft. MUTATION: NONE');});renderPolicyDraftPreview();loadPolicyGeoFiles();
  }

  let splitDNSCapabilityAttempts=0;
  function showSplitDNSMemoryNotice(text){let notice=qs('#splitDNSMemoryNotice');const hint=qs('#networkHint');if(!notice&&hint&&hint.parentNode){notice=document.createElement('div');notice.id='splitDNSMemoryNotice';notice.className='notice show bad';notice.style.marginTop='10px';hint.parentNode.insertBefore(notice,hint.nextSibling);}if(notice){notice.textContent=text;notice.className='notice show bad';}}
  async function mountSplitDNSMemoryGate(){const select=qs('#dnsModeSelect');if(!select)return;const option=select.querySelector('option[value="xkeen"]');if(!option)return;option.disabled=true;option.textContent='XKeen/Xray DNS — проверка ОЗУ…';try{const r=await fetch('/api/capabilities',{cache:'no-store'});if(r.status===401){if(typeof loadAuthStatus==='function')await loadAuthStatus();splitDNSCapabilityAttempts++;if(splitDNSCapabilityAttempts<20)setTimeout(mountSplitDNSMemoryGate,1500);return;}const capability=await r.json();const supported=!!(r.ok&&capability&&capability.success&&capability.split_dns_supported);const oldNotice=qs('#splitDNSMemoryNotice');if(supported){option.disabled=false;option.textContent='XKeen/Xray DNS';if(oldNotice)oldNotice.remove();return;}option.disabled=true;option.textContent='XKeen/Xray DNS — недоступно (мало ОЗУ)';const mem=Number(capability&&capability.memory_total_mib)||0;const min=Number(capability&&capability.split_dns_min_mib)||768;const reason=String((capability&&capability.reason)||'').trim();showSplitDNSMemoryNotice(reason||(mem?`XKeen/Xray DNS недоступен: обнаружено ${mem} MiB RAM, требуется не менее ${min} MiB. Используйте DNS напрямую через роутер.`:'XKeen/Xray DNS недоступен: объём RAM не удалось безопасно определить. Используйте DNS напрямую через роутер.'));}catch(_){option.disabled=true;option.textContent='XKeen/Xray DNS — недоступно';showSplitDNSMemoryNotice('FreeNet не смог подтвердить достаточный объём RAM. XKeen/Xray DNS заблокирован; используйте DNS напрямую через роутер.');}}

  function mount(){patchNavigation();patchStatusRendering();mountExactConnectControls();patchProfileSelection();patchNativeEngineMigrationFlow();mountPolicyPreview();mountSplitDNSMemoryGate();}
  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',mount);else mount();
})();

(() => {
  if (typeof applyNetworkProfile !== 'function') return;
  const applyButton = document.getElementById('applyNetworkBtn');
  if (!applyButton) return;
  const legacyApplyNetworkProfile = applyNetworkProfile;
  const wait = ms => new Promise(resolve => setTimeout(resolve, ms));

  async function reconcileNetworkTargetAfterConflict(isp, dnsMode) {
    showBox('networkNotice', 'FreeNet уже выполняет операцию. Повторный Apply не запускаем; подтверждаем фактическое состояние…');
    let status = null;
    for (let i = 0; i < 80; i++) {
      try { status = await loadStatus(); } catch (_) { status = null; }
      if (status && !status.busy && !status.updater_busy) break;
      await wait(750);
    }
    if (!status || status.busy || status.updater_busy) { showBox('networkNotice', 'FreeNet всё ещё выполняет операцию или фактический статус недоступен. Повторный Apply автоматически не запускался.', 'bad'); return; }
    try { await loadNetworkPlan(); } catch (_) { showBox('networkNotice', 'Операция завершилась, но свежий read-only план недоступен. Повторный Apply автоматически не запускался.', 'bad'); return; }
    const plan = lastNetworkPlan;
    const targetActive = !!(plan && plan.supported && plan.active && plan.isp === isp && plan.dns_mode === dnsMode);
    if (targetActive) { showBox('networkNotice', 'Целевое сетевое состояние подтверждено по фактическому read-only плану. Повторный Apply не требовался.', 'ok'); return; }
    if (plan && plan.supported && !plan.active) { showBox('networkNotice', 'Предыдущая операция завершилась, но выбранная цель не применена. План пересчитан по фактическому состоянию. Повторный Apply автоматически не запускался.', 'bad'); return; }
    showBox('networkNotice', 'Фактическое сетевое состояние не подтверждено. Повторный Apply автоматически не запускался.', 'bad');
  }

  async function reconciledNetworkApply() {
    if (networkDirty || !networkPlanReady || networkApplying) return;
    const isp = el('ispSelect').value; const dnsMode = el('dnsModeSelect').value; const delta = (lastNetworkPlan && lastNetworkPlan.expected_delta) || 'сетевой профиль';
    if (!window.confirm('Применить подтверждённый сетевой профиль?\n\n' + delta + '\n\nПри ошибке FreeNet использует транзакционный откат.')) return;
    networkApplying = true; buttonsBusy(true); showBox('networkNotice', 'Применяем сетевой профиль…');
    try {
      const r = await fetch('/api/network-profile/apply',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({operation:'network',isp,dns_mode:dnsMode,confirm:true})});
      if (r.status === 401) { await loadAuthStatus(); return; }
      const j = await r.json();
      if (!r.ok || !j.success) {
        if (r.status === 409 && String(j.error || '') === 'another FreeNet operation is already running') { await reconcileNetworkTargetAfterConflict(isp,dnsMode); return; }
        const parts=[j.error||'Сетевой профиль не применён'];if(j.primary_error)parts.push('ОСНОВНАЯ ОШИБКА: '+j.primary_error);if(j.rollback_state)parts.push('ОТКАТ: '+j.rollback_state);showBox('networkNotice',parts.join('\n'),'bad');return;
      }
      showBox('networkNotice',(j.message||'Сетевой профиль применён')+' Откат: '+(j.rollback_state||'NOT_NEEDED'),'ok');await loadStatus();await loadNetworkPlan(selectedProviderID);
    } catch (_) { await reconcileNetworkTargetAfterConflict(isp,dnsMode); }
    finally { networkApplying=false;buttonsBusy(!!(lastStatus&&(lastStatus.busy||lastStatus.updater_busy))); }
  }
  applyButton.removeEventListener('click',legacyApplyNetworkProfile);applyNetworkProfile=reconciledNetworkApply;applyButton.addEventListener('click',applyNetworkProfile);
})();

(() => {
  const qs=(s,root=document)=>root.querySelector(s);
  const fallbackProviders=[{id:'router-current',label:'Текущие DNS роутера',addresses:[]},{id:'yandex-basic',label:'Яндекс Basic',addresses:['77.88.8.8','77.88.8.1']}];
  let providerTouched=false;
  function hideLegacyNetworkChoices(){for(const id of ['ispSelect','dnsModeSelect']){const select=qs('#'+id);if(!select)continue;for(const value of ['auto','custom']){const option=select.querySelector(`option[value="${value}"]`);if(!option)continue;option.hidden=true;option.disabled=true;}}const direct=qs('#dnsModeSelect option[value="firmware"]');if(direct)direct.textContent='DNS напрямую через роутер';}
  function mountNativeDNSProviderField(){const dnsMode=qs('#dnsModeSelect');const hint=qs('#networkHint');if(!dnsMode||!hint||!hint.parentNode)return null;let field=qs('#nativeDNSProviderField');if(!field){field=document.createElement('div');field.id='nativeDNSProviderField';field.className='field';field.style.marginTop='10px';field.innerHTML='<label for="nativeDNSProviderSelect">DNS для режима напрямую</label><select id="nativeDNSProviderSelect"></select><div class="hint">«Текущие DNS роутера» сохраняет точный проверенный Native resolver этого роутера. «Яндекс Basic» использует 77.88.8.8 / 77.88.8.1. Named DNS profiles, DoT/DoH и назначения клиентов не переписываются.</div>';hint.parentNode.insertBefore(field,hint.nextSibling);qs('#nativeDNSProviderSelect').addEventListener('change',()=>{providerTouched=true;if(typeof resetSetupFinalizePlan==='function')resetSetupFinalizePlan();const mode=qs('#dnsModeSelect');if(mode)mode.dispatchEvent(new Event('change',{bubbles:true}));});}field.hidden=dnsMode.value!=='firmware';return{field,select:qs('#nativeDNSProviderSelect')};}
  function providerLabel(option){const addresses=Array.isArray(option.addresses)?option.addresses.filter(Boolean):[];return addresses.length?`${option.label} (${addresses.join(' / ')})`:option.label;}
  function syncProviderOptions(){const ui=mountNativeDNSProviderField();if(!ui)return;const plan=typeof lastNetworkPlan!=='undefined'?lastNetworkPlan:null;const options=plan&&Array.isArray(plan.native_dns_provider_options)&&plan.native_dns_provider_options.length?plan.native_dns_provider_options:fallbackProviders;const ids=options.map(option=>String(option.id||'')).filter(Boolean);const currentIDs=Array.from(ui.select.options).map(option=>option.value);const existingValue=ui.select.value;if(ids.join(',')!==currentIDs.join(',')){ui.select.textContent='';options.forEach(item=>{if(!item||!item.id)return;const option=document.createElement('option');option.value=String(item.id);option.textContent=providerLabel(item);ui.select.appendChild(option);});if(ids.includes(existingValue))ui.select.value=existingValue;}if(!providerTouched&&plan&&plan.native_dns_provider&&ids.includes(plan.native_dns_provider))ui.select.value=plan.native_dns_provider;if(!ui.select.value&&ids.includes('yandex-basic'))ui.select.value='yandex-basic';ui.field.hidden=String((qs('#dnsModeSelect')||{}).value||'')!=='firmware';}
  function selectedNativeProvider(){const select=qs('#nativeDNSProviderSelect');return(select&&select.value)||'yandex-basic';}
  function patchNetworkPlanSync(){if(typeof loadNetworkPlan!=='function')return;const previous=loadNetworkPlan;loadNetworkPlan=async function(...args){const result=await previous(...args);syncProviderOptions();const plan=typeof lastNetworkPlan!=='undefined'?lastNetworkPlan:null;if(plan&&plan.active&&plan.native_dns_provider===selectedNativeProvider())providerTouched=false;return result;};}
  function patchNetworkControlRendering(){if(typeof renderNetworkControls==='function'){const previous=renderNetworkControls;renderNetworkControls=function(...args){previous(...args);hideLegacyNetworkChoices();syncProviderOptions();};}const dnsMode=qs('#dnsModeSelect');if(dnsMode)dnsMode.addEventListener('change',()=>{const ui=mountNativeDNSProviderField();if(ui)ui.field.hidden=dnsMode.value!=='firmware';});}
  function patchNetworkFetch(){const previousFetch=window.fetch.bind(window);window.fetch=function(input,init){let requestInput=input;let requestInit=init;const url=typeof input==='string'?input:(input&&input.url)||'';if(providerTouched&&url.startsWith('/api/network-profile/plan')){try{const parsed=new URL(url,window.location.origin);parsed.searchParams.set('native_dns_provider',selectedNativeProvider());if(typeof input==='string')requestInput=parsed.pathname+parsed.search+parsed.hash;}catch(_){}}if(url==='/api/network-profile/apply'&&init&&String(init.method||'').toUpperCase()==='POST'&&typeof init.body==='string'){try{const body=JSON.parse(init.body);if(body.operation==='network'){body.native_dns_provider=selectedNativeProvider();requestInit=Object.assign({},init,{body:JSON.stringify(body)});}}catch(_){}}return previousFetch(requestInput,requestInit);};}
  function mount(){hideLegacyNetworkChoices();mountNativeDNSProviderField();syncProviderOptions();patchNetworkPlanSync();patchNetworkControlRendering();patchNetworkFetch();}
  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',mount);else mount();
})();

// Canonical deterministic local flag renderer. This layer has deliberately
// higher specificity than legacy/index and accepted-ux flag approximations so
// there is exactly one visual owner for every country in the actual Extra catalog.
(() => {
  const h = (...colors) => colors.map((c,i)=>`<rect x="0" y="${(40*i/colors.length).toFixed(3)}" width="60" height="${(40/colors.length+0.02).toFixed(3)}" fill="${c}"/>`).join('');
  const v = (...colors) => colors.map((c,i)=>`<rect x="${(60*i/colors.length).toFixed(3)}" y="0" width="${(60/colors.length+0.02).toFixed(3)}" height="40" fill="${c}"/>`).join('');
  const nordic = (base,outer,inner) => `<rect width="60" height="40" fill="${base}"/><rect x="18" width="10" height="40" fill="${outer}"/><rect y="15" width="60" height="10" fill="${outer}"/><rect x="21" width="4" height="40" fill="${inner}"/><rect y="18" width="60" height="4" fill="${inner}"/>`;
  const atlas = {
    nl:h('#ae1c28','#fff','#21468b'), at:h('#ed2939','#fff','#ed2939'), bg:h('#fff','#00966e','#d62612'),
    be:v('#111','#ffd90c','#ef3340'), ro:v('#002b7f','#fcd116','#ce1126'), fr:v('#0055a4','#fff','#ef4135'),
    de:h('#111','#dd0000','#ffce00'), hu:h('#ce2939','#fff','#477050'), ie:v('#169b62','#fff','#ff883e'),
    jp:'<rect width="60" height="40" fill="#fff"/><circle cx="30" cy="20" r="9" fill="#bc002d"/>',
    pe:v('#d91023','#fff','#d91023'), lt:h('#fdb913','#006a44','#c1272d'), it:v('#009246','#fff','#ce2b37'),
    pl:h('#fff','#dc143c'), ua:h('#0057b8','#ffd700'), ch:'<rect width="60" height="40" fill="#d52b1e"/><rect x="26" y="9" width="8" height="22" fill="#fff"/><rect x="19" y="16" width="22" height="8" fill="#fff"/>',
    ng:v('#008751','#fff','#008751'), lu:h('#ed2939','#fff','#00a1de'), si:h('#fff','#005da4','#ed1c24'), rs:h('#c6363c','#0c4076','#fff'),
    dk:nordic('#c8102e','#fff','#fff'), fi:nordic('#fff','#003580','#003580'), se:nordic('#006aa7','#fecc00','#fecc00'), no:nordic('#ba0c2f','#fff','#00205b'), is:nordic('#02529c','#fff','#dc1e35'),
    co:'<rect width="60" height="20" fill="#fcd116"/><rect y="20" width="60" height="10" fill="#003893"/><rect y="30" width="60" height="10" fill="#ce1126"/>',
    ae:'<rect width="60" height="40" fill="#fff"/><rect width="15" height="40" fill="#ff0000"/><rect x="15" width="45" height="13.334" fill="#00732f"/><rect x="15" y="26.666" width="45" height="13.334" fill="#000"/>',
    cz:'<rect width="60" height="20" fill="#fff"/><rect y="20" width="60" height="20" fill="#d7141a"/><path d="M0 0 28 20 0 40Z" fill="#11457e"/>',
    gr:'<rect width="60" height="40" fill="#0d5eaf"/><g fill="#fff"><rect y="4.44" width="60" height="4.45"/><rect y="13.33" width="60" height="4.45"/><rect y="22.22" width="60" height="4.45"/><rect y="31.11" width="60" height="4.45"/><rect x="0" y="8.9" width="24" height="4.45"/><rect x="9.8" y="0" width="4.4" height="22.3"/></g>',
    tr:'<rect width="60" height="40" fill="#e30a17"/><circle cx="25" cy="20" r="10" fill="#fff"/><circle cx="28.5" cy="20" r="8" fill="#e30a17"/><path d="m35 20 6-2-3.7 5 0-6 3.7 5Z" fill="#fff"/>',
    us:'<rect width="60" height="40" fill="#fff"/><g fill="#b22234"><rect width="60" height="3.08" y="0"/><rect width="60" height="3.08" y="6.15"/><rect width="60" height="3.08" y="12.31"/><rect width="60" height="3.08" y="18.46"/><rect width="60" height="3.08" y="24.62"/><rect width="60" height="3.08" y="30.77"/><rect width="60" height="3.08" y="36.92"/></g><rect width="28" height="21.54" fill="#3c3b6e"/><g fill="#fff"><circle cx="4" cy="4" r="1"/><circle cx="9" cy="4" r="1"/><circle cx="14" cy="4" r="1"/><circle cx="19" cy="4" r="1"/><circle cx="24" cy="4" r="1"/><circle cx="6.5" cy="8" r="1"/><circle cx="11.5" cy="8" r="1"/><circle cx="16.5" cy="8" r="1"/><circle cx="21.5" cy="8" r="1"/><circle cx="4" cy="12" r="1"/><circle cx="9" cy="12" r="1"/><circle cx="14" cy="12" r="1"/><circle cx="19" cy="12" r="1"/><circle cx="24" cy="12" r="1"/><circle cx="6.5" cy="16" r="1"/><circle cx="11.5" cy="16" r="1"/><circle cx="16.5" cy="16" r="1"/><circle cx="21.5" cy="16" r="1"/></g>',
    kr:'<rect width="60" height="40" fill="#fff"/><defs><clipPath id="krTop"><rect width="60" height="20"/></clipPath><clipPath id="krBottom"><rect y="20" width="60" height="20"/></clipPath></defs><circle cx="30" cy="20" r="8" fill="#cd2e3a" clip-path="url(#krTop)"/><circle cx="30" cy="20" r="8" fill="#0047a0" clip-path="url(#krBottom)"/><circle cx="26" cy="20" r="4" fill="#0047a0"/><circle cx="34" cy="20" r="4" fill="#cd2e3a"/><g stroke="#111" stroke-width="2"><path d="M10 9h10M10 13h10M10 17h10M40 23h10M40 27h10M40 31h10M11 25h4m2 0h4M11 29h10M11 33h4m2 0h4M39 7h4m2 0h4M39 11h4m2 0h4M39 15h10"/></g>',
    kz:'<rect width="60" height="40" fill="#00afca"/><circle cx="33" cy="14" r="6" fill="#f6cf33"/><g stroke="#f6cf33" stroke-width="1.2"><path d="M33 5v4M33 19v4M24 14h4M38 14h4M27 8l3 3M36 17l3 3M39 8l-3 3M30 17l-3 3"/></g><path d="M22 25c7 7 16 7 23 0-4 2-7 1-11-2-3 3-7 4-12 2Z" fill="#f6cf33"/><path d="M7 3v34M11 4v32M7 8l4 3-4 3 4 3-4 3 4 3-4 3 4 3-4 3" fill="none" stroke="#f6cf33" stroke-width="1.6"/>',
    ar:'<rect width="60" height="40" fill="#74acdf"/><rect y="13.333" width="60" height="13.334" fill="#fff"/><circle cx="30" cy="20" r="4" fill="#f6b40e"/><g stroke="#f6b40e"><path d="M30 13v4M30 23v4M23 20h4M33 20h4M25 15l3 3M32 22l3 3M35 15l-3 3M28 22l-3 3"/></g>',
    br:'<rect width="60" height="40" fill="#009b3a"/><path d="m30 5 23 15-23 15L7 20Z" fill="#ffdf00"/><circle cx="30" cy="20" r="9" fill="#002776"/><path d="M22 18c7-2 13-1 17 2" fill="none" stroke="#fff" stroke-width="1.5"/><g fill="#fff"><circle cx="27" cy="17" r="1"/><circle cx="32" cy="23" r="1"/><circle cx="36" cy="17" r="1"/></g>',
    ca:'<rect width="60" height="40" fill="#fff"/><rect width="15" height="40" fill="#d80621"/><rect x="45" width="15" height="40" fill="#d80621"/><path d="m30 7 2 6 5-2-2 5 5 2-5 3 2 7-7-4-7 4 2-7-5-3 5-2-2-5 5 2Z" fill="#d80621"/>',
    sg:'<rect width="60" height="20" fill="#ef3340"/><rect y="20" width="60" height="20" fill="#fff"/><circle cx="16" cy="10" r="7" fill="#fff"/><circle cx="19" cy="10" r="6" fill="#ef3340"/><g fill="#fff"><circle cx="24" cy="5" r="1"/><circle cx="27" cy="8" r="1"/><circle cx="26" cy="12" r="1"/><circle cx="22" cy="14" r="1"/><circle cx="21" cy="9" r="1"/></g>',
    il:'<rect width="60" height="40" fill="#fff"/><rect y="5" width="60" height="4" fill="#0038b8"/><rect y="31" width="60" height="4" fill="#0038b8"/><path d="m30 12 7 12H23Zm0 16-7-12h14Z" fill="none" stroke="#0038b8" stroke-width="1.8"/>',
    mx:'<rect width="20" height="40" fill="#006847"/><rect x="20" width="20" height="40" fill="#fff"/><rect x="40" width="20" height="40" fill="#ce1126"/><circle cx="30" cy="20" r="4" fill="#8b6b34"/><path d="M24 25c4 3 8 3 12 0" fill="none" stroke="#006847" stroke-width="1.4"/>',
    my:'<rect width="60" height="40" fill="#fff"/><g fill="#cc0001"><rect width="60" height="3.08" y="0"/><rect width="60" height="3.08" y="6.15"/><rect width="60" height="3.08" y="12.31"/><rect width="60" height="3.08" y="18.46"/><rect width="60" height="3.08" y="24.62"/><rect width="60" height="3.08" y="30.77"/><rect width="60" height="3.08" y="36.92"/></g><rect width="30" height="22" fill="#010066"/><circle cx="12" cy="11" r="7" fill="#ffcc00"/><circle cx="15" cy="10" r="6" fill="#010066"/><path d="m23 5 1.3 3 3.2.2-2.4 2.2.7 3-2.8-1.5-2.8 1.5.7-3-2.4-2.2 3.2-.2Z" fill="#ffcc00"/>',
    hk:'<rect width="60" height="40" fill="#de2910"/><g fill="#fff" transform="translate(30 20)"><ellipse rx="2.2" ry="8" transform="rotate(0) translate(0 -7)"/><ellipse rx="2.2" ry="8" transform="rotate(72) translate(0 -7)"/><ellipse rx="2.2" ry="8" transform="rotate(144) translate(0 -7)"/><ellipse rx="2.2" ry="8" transform="rotate(216) translate(0 -7)"/><ellipse rx="2.2" ry="8" transform="rotate(288) translate(0 -7)"/></g>',
    gb:'<rect width="60" height="40" fill="#012169"/><path d="M0 0 60 40M60 0 0 40" stroke="#fff" stroke-width="10"/><path d="M0 0 60 40M60 0 0 40" stroke="#c8102e" stroke-width="5"/><path d="M30 0v40M0 20h60" stroke="#fff" stroke-width="14"/><path d="M30 0v40M0 20h60" stroke="#c8102e" stroke-width="8"/>',
    au:'<rect width="60" height="40" fill="#012169"/><g transform="scale(.5)"><path d="M0 0 60 40M60 0 0 40" stroke="#fff" stroke-width="10"/><path d="M0 0 60 40M60 0 0 40" stroke="#c8102e" stroke-width="5"/><path d="M30 0v40M0 20h60" stroke="#fff" stroke-width="14"/><path d="M30 0v40M0 20h60" stroke="#c8102e" stroke-width="8"/></g><g fill="#fff"><path d="m15 28 1.5 3 3.5.4-2.5 2.4.6 3.4-3.1-1.7-3.1 1.7.6-3.4-2.5-2.4 3.5-.4Z"/><circle cx="44" cy="9" r="1.7"/><circle cx="50" cy="17" r="1.5"/><circle cx="42" cy="24" r="1.5"/><circle cx="51" cy="30" r="1.5"/></g>',
    za:'<rect width="60" height="40" fill="#007749"/><path d="M0 0 24 20 0 40V31l13-11L0 9Z" fill="#000"/><path d="M0 5 19 20 0 35" fill="none" stroke="#ffb81c" stroke-width="8"/><path d="M0 2 22 20 0 38" fill="none" stroke="#fff" stroke-width="4"/><path d="M22 16H60V0H13Z" fill="#de3831"/><path d="M22 24H60V40H13Z" fill="#002395"/>',
    sk:'<rect width="60" height="40" fill="#fff"/><rect y="13.333" width="60" height="13.334" fill="#0b4ea2"/><rect y="26.667" width="60" height="13.333" fill="#ee1c25"/><path d="M18 9h10v15c0 5-5 7-5 7s-5-2-5-7Z" fill="#fff" stroke="#ee1c25"/><path d="M23 13v12M19 17h8M20 21h6" stroke="#0b4ea2" stroke-width="1.4"/>',
    hr:'<rect width="60" height="13.333" fill="#ff0000"/><rect y="13.333" width="60" height="13.334" fill="#fff"/><rect y="26.667" width="60" height="13.333" fill="#171796"/><path d="M25 12h10v14H25Z" fill="#fff" stroke="#d00"/><path d="M25 12h5v7h-5m10-7h-5v7h5m-10 7h5v-7h-5m10 7h-5v-7h5" fill="#d00"/>',
    pt:'<rect width="24" height="40" fill="#046a38"/><rect x="24" width="36" height="40" fill="#da291c"/><circle cx="24" cy="20" r="5" fill="#ffcd00"/><circle cx="24" cy="20" r="3" fill="#fff" stroke="#003399"/>',
    cn:'<rect width="60" height="40" fill="#de2910"/><path d="m10 6 1.6 4.8h5l-4 3 1.5 4.8-4.1-3-4.1 3 1.5-4.8-4-3h5Z" fill="#ffde00"/>'
  };
  const style = document.createElement('style');
  style.id = 'freenetCanonicalAllFlags';
  style.textContent = Object.entries(atlas).map(([code,body]) => {
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 60 40" preserveAspectRatio="none">${body}</svg>`;
    const uri = `url("data:image/svg+xml,${encodeURIComponent(svg)}")`;
    return `html body #controlCenter .flag-icon.flag-${code},html body #controlCenter .flag-hero.flag-${code}{background-color:transparent!important;background-image:${uri}!important;background-repeat:no-repeat!important;background-position:center!important;background-size:100% 100%!important;box-sizing:border-box!important;overflow:hidden!important}html body #controlCenter .flag-icon.flag-${code}::before,html body #controlCenter .flag-icon.flag-${code}::after,html body #controlCenter .flag-hero.flag-${code}::before,html body #controlCenter .flag-hero.flag-${code}::after{content:none!important;display:none!important}`;
  }).join('\n');
  document.head.appendChild(style);
})();
