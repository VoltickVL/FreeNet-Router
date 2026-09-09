(() => {
  const previousFetch = window.fetch.bind(window);
  const wait = ms => new Promise(resolve => setTimeout(resolve, ms));

  function mutationMeta(input, init) {
    const url = typeof input === 'string' ? input : (input && input.url) || '';
    const method = String((init && init.method) || (input && input.method) || 'GET').toUpperCase();
    if (method !== 'POST' || !init || typeof init.body !== 'string') return null;
    try {
      const body = JSON.parse(init.body);
      if (url === '/api/action' && body && typeof body.action === 'string' && body.action) {
        return {kind: 'quick', target: body.action, startedAt: Date.now()};
      }
      if (url === '/api/network-profile/apply' && body && body.operation === 'provider' && typeof body.profile_id === 'string' && body.profile_id) {
        return {kind: 'provider', target: body.profile_id, startedAt: Date.now()};
      }
      if (url === '/api/vpn/current-refresh' && body && body.confirm === true) {
        return {kind: 'refresh', target: 'current', startedAt: Date.now()};
      }
    } catch (_) {}
    return null;
  }

  function matchingFreshOperation(op, meta) {
    if (!op || op.kind !== meta.kind || op.target !== meta.target) return false;
    const started = Date.parse(op.started_at || '');
    return Number.isFinite(started) && started >= meta.startedAt - 5000;
  }

  function jsonResponse(status, body) {
    return new Response(JSON.stringify(body), {status, headers: {'Content-Type': 'application/json; charset=utf-8'}});
  }

  async function readOperationState(timeoutMS = 7000) {
    try {
      const response = await previousFetch('/api/operation/state', {cache: 'no-store', signal: AbortSignal.timeout(timeoutMS)});
      if (!response.ok) return null;
      const body = await response.json();
      return body && body.success ? body : null;
    } catch (_) {
      return null;
    }
  }

  function terminalResponse(op, meta) {
    if (!matchingFreshOperation(op, meta)) return null;
    if (op.state === 'success' && op.result === 'SUCCESS') {
      if (meta.kind === 'quick') {
        return jsonResponse(200, {success: true, action: meta.target, operation_id: op.id, message: op.message || 'VPN-действие выполнено'});
      }
      if (meta.kind === 'refresh') {
        return jsonResponse(200, {success: true, outcome: 'applied', applied: true, mutation: 'APPLIED',
          operation_id: op.id, rollback_state: 'NOT_NEEDED', message: op.message || 'Свежий endpoint применён и проверен'});
      }
      return jsonResponse(200, {
        success: true, applied: true, operation: 'provider', profile_id: meta.target,
        operation_id: op.id, rollback_state: 'NOT_NEEDED', message: op.message || 'VPN-профиль применён и проверен'
      });
    }
    if (op.state === 'failed' && op.result === 'FAIL') {
      return jsonResponse(502, {
        success: false,
        operation: meta.kind === 'provider' ? 'provider' : undefined,
        profile_id: meta.kind === 'provider' ? meta.target : undefined,
        action: meta.kind === 'quick' ? meta.target : undefined,
        operation_id: op.id,
        error: op.error || 'VPN-операция завершилась ошибкой'
      });
    }
    return null;
  }

  async function reconcile(meta) {
    const deadline = Date.now() + (meta.kind === 'refresh' ? 240000 : 120000);
    while (Date.now() < deadline) {
      const state = await readOperationState(Math.max(1, Math.min(7000, deadline - Date.now())));
      const op = state && state.operation;
      if (op) {
        const terminal = terminalResponse(op, meta);
        if (terminal) return terminal;
        if (state.active && !matchingFreshOperation(op, meta)) return null;
      }
      await wait(750);
    }
    return null;
  }

  window.fetch = async function(input, init) {
    const meta = mutationMeta(input, init);
    if (!meta) return previousFetch(input, init);
    try {
      const response = await previousFetch(input, init);
      let body = null;
      try { body = await response.clone().json(); } catch (_) {}
      const conflict = response.status === 409 || response.status === 423;
      const current = body && body.current_operation;
      const ambiguous = !body || typeof body.success !== 'boolean';
      if ((conflict && current && matchingFreshOperation(current, meta)) || ambiguous) {
        const reconciled = await reconcile(meta);
        if (reconciled) return reconciled;
        return jsonResponse(202, {success: false, result_unknown: true,
          error: 'Результат переключения пока не подтверждён. Не повторяйте операцию.'});
      }
      return response;
    } catch (error) {
      const reconciled = await reconcile(meta);
      if (reconciled) return reconciled;
      throw error;
    }
  };
})();

(() => {
  const qs = (selector, root = document) => root.querySelector(selector);
  const wait = ms => new Promise(resolve => setTimeout(resolve, ms));
  let recommendation = null;
  let alternatives = [];
  let currentQuality = null;
  let scanBusy = false;
  let applyBusy = false;
  let externalBusy = false;
  let scanMode = false;
  let topbarSyncTimer = null;

  function setText(node, value) { if (node) node.textContent = value || ''; }

  const iconPaths = {
    speed: '<path d="M12 3v12m0 0 5-5m-5 5-5-5"/><path d="M5 21h14"/>',
    http: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
    tcp: '<circle cx="12" cy="5" r="2"/><circle cx="5" cy="16" r="2"/><circle cx="19" cy="16" r="2"/><path d="M10.8 6.7 6.2 14M13.2 6.7l4.6 7.3M7 16h10"/>',
    jitter: '<path d="M3 13h3l2-6 3 11 3-13 2 8h5"/>',
    search: '<circle cx="11" cy="11" r="7"/><path d="m16.5 16.5 4 4"/>',
    refresh: '<path d="M20 7v5h-5"/><path d="M4 17v-5h5"/><path d="M18.5 10A7 7 0 0 0 6.7 6.7L4 9M5.5 14A7 7 0 0 0 17.3 17.3L20 15"/>',
    swap: '<path d="M7 7h13l-3-3m3 3-3 3M17 17H4l3 3m-3-3 3-3"/>',
    check: '<circle cx="12" cy="12" r="9"/><path d="m8 12 2.5 2.5L16 9"/>',
    info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v6M12 7h.01"/>',
    trophy: '<path d="M8 4h8v4a4 4 0 0 1-8 0V4Z"/><path d="M8 6H5v1a4 4 0 0 0 4 4M16 6h3v1a4 4 0 0 1-4 4M12 12v4M9 20h6M10 16h4"/>',
    compare: '<path d="M12 3 19 6v5c0 4.5-3 7.7-7 10-4-2.3-7-5.5-7-10V6l7-3Z"/><path d="M9 12h6"/>',
    alert: '<circle cx="12" cy="12" r="9"/><path d="M12 7v6M12 17h.01"/>',
    retry: '<path d="M20 11a8 8 0 1 0-2.3 5.7"/><path d="M20 5v6h-6"/>'
  };

  function makeIcon(name, className = '') {
    const span = document.createElement('span');
    span.className = `fn-icon${className ? ' ' + className : ''}`;
    span.setAttribute('aria-hidden', 'true');
    span.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round">${iconPaths[name] || iconPaths.info}</svg>`;
    return span;
  }

  function setButtonLabel(button, label, iconName = '') {
    if (!button) return;
    button.textContent = '';
    if (iconName) button.appendChild(makeIcon(iconName, 'button-icon'));
    const text = document.createElement('span'); text.textContent = label; button.appendChild(text);
  }

  function stripProfilePrefix(value) {
    let text = String(value || '').trim();
    text = text.replace(/^[\u{1F1E6}-\u{1F1FF}]{2}\s*/u, '');
    text = text.replace(/^[A-Za-z]{2}\s+/, '');
    return text.trim();
  }

  function profileCode(profile) {
    const direct = String(profile && profile.country_code || '').trim().toLowerCase();
    if (/^[a-z]{2}$/.test(direct)) return direct;
    const raw = String(profile && profile.name || '').trim();
    const ascii = raw.match(/^([A-Za-z]{2})\s+/);
    if (ascii) return ascii[1].toLowerCase();
    const chars = Array.from(raw);
    if (chars.length >= 2) {
      const a = chars[0].codePointAt(0), b = chars[1].codePointAt(0), base = 0x1F1E6;
      if (a >= base && a <= 0x1F1FF && b >= base && b <= 0x1F1FF) {
        return String.fromCharCode(97 + a - base, 97 + b - base);
      }
    }
    return '';
  }

  function isRussianProfile(profile) {
    const code = profileCode(profile);
    const name = String(profile && profile.name || '').trim().toLowerCase();
    return code === 'ru' || name.includes('russia') || name.includes('росси');
  }

  function installRussianProfileFilter() {
    if (typeof renderExtraProfiles === 'function' && !renderExtraProfiles.__freenetForeignOnly) {
      const previous = renderExtraProfiles;
      const wrapped = function(plan) {
        if (plan && Array.isArray(plan.extra_profiles)) {
          plan = Object.assign({}, plan, {extra_profiles: plan.extra_profiles.filter(profile => !isRussianProfile(profile))});
        }
        return previous(plan);
      };
      wrapped.__freenetForeignOnly = true;
      renderExtraProfiles = wrapped;
    }
    try {
      if (Array.isArray(extraProfiles)) {
        extraProfiles = extraProfiles.filter(profile => !isRussianProfile(profile));
        if (typeof renderProfileOptions === 'function') renderProfileOptions();
      }
    } catch (_) {}
  }

  function patchCrossPlatformFlags() {
    try {
      if (typeof countryFlagCodes !== 'undefined' && countryFlagCodes && typeof countryFlagCodes.add === 'function') {
        ['sk','za','si','rs','is','lu'].forEach(code => countryFlagCodes.add(code));
      }
    } catch (_) {}
    if (typeof makeCountryMarker === 'function' && !makeCountryMarker.__freenetCSSFlags) {
      const wrapped = function(code) {
        const safe = String(code || '').trim().toLowerCase();
        const node = document.createElement('span');
        if (/^[a-z]{2}$/.test(safe)) {
          node.className = `flag-icon flag-${safe}`;
          node.setAttribute('aria-hidden', 'true');
        } else {
          node.className = 'country-code-badge';
          node.textContent = 'VPN';
        }
        return node;
      };
      wrapped.__freenetCSSFlags = true;
      makeCountryMarker = wrapped;
    }
  }

  function injectStyle() {
    if (qs('#FreeNetApprovedOverview')) return;
    const style = document.createElement('style');
    style.id = 'FreeNetApprovedOverview';
    style.textContent = `
      :root{--fn-blue:#2d77ff;--fn-blue2:#5598ff;--fn-green:#36e3a2;--fn-red:#ff5c6a;--fn-card:#0e1e31;--fn-card2:#10243a;--fn-border:#294b70;--fn-muted:#a9bbd2;--fn-deep:#081523}
      body{font-size:15px}.content{width:min(1248px,calc(100% - 38px));padding:15px 0 22px}.topbar.overview-approved{height:64px;padding:0 24px;background:rgba(6,14,25,.97);border-bottom:1px solid #223650}.page[data-page-view="overview"] .page-head{margin:0 0 13px;align-items:center}.page[data-page-view="overview"] .page-head h1{font-size:30px;line-height:1.1}.page[data-page-view="overview"] .page-kicker,.page[data-page-view="overview"] .page-head p{display:none}.page[data-page-view="overview"]>.grid-equal{display:none!important}.content:has(.page.active[data-page-view="overview"])>.footer{display:none!important}
      .overview-hero-source{display:none!important}.overview-compact-grid{grid-template-columns:1fr!important;gap:13px}.overview-approved-top{margin-left:auto;display:flex;align-items:center;gap:30px}.overview-approved-fact{display:grid;gap:1px}.overview-approved-fact span{font-size:11px;color:#83a2ca;font-weight:760}.overview-approved-fact strong{font-size:13px;color:#f5f8ff;white-space:nowrap}.top-status{font-size:12px}.top-status .dot.ok{background:var(--fn-green);box-shadow:0 0 15px rgba(52,226,160,.7)}
      #quickActionsSection{align-self:start;padding:20px 20px 18px;border-radius:20px;border:1px solid #294968;background:linear-gradient(150deg,#102238,#0b1829);box-shadow:none;overflow:visible}#quickActionsSection>.card-head{display:none!important}
      .best-v4-shell{display:grid;grid-template-columns:minmax(312px,342px) minmax(0,1fr);gap:0 26px;align-items:start}.vpn-current-panel{min-width:0;padding:8px 24px 0 0;border-right:1px solid #304861}.vpn-alternatives-panel{min-width:0;padding-left:0}.best-v4-current{display:flex;align-items:flex-start;gap:12px}.best-v4-current-main{min-width:0}.best-v4-label{color:#a9bad1;font-size:12px;font-weight:800}.best-v4-name{font-size:23px;font-weight:790;line-height:1.2;margin-top:5px;color:#f8faff;overflow-wrap:anywhere;letter-spacing:-.015em}.best-v4-endpoint{margin:10px 0 17px;color:#8ea8c9;font:12px ui-monospace,SFMono-Regular,Menlo,Consolas,monospace}.vpn-current-panel #bestCurrentFlag{width:33px;height:23px;margin-top:3px;border-radius:4px}
      .fn-icon{display:inline-grid;place-items:center;flex:0 0 auto;width:20px;height:20px;color:#6aa2ff}.fn-icon svg{display:block;width:100%;height:100%}.button-icon{width:18px;height:18px;color:currentColor}.metric-icon{width:24px;height:24px}.vpn-current-panel .metric-icon{width:23px;height:23px}.status-icon{width:18px;height:18px}.chip-icon{width:14px;height:14px}.manual-search-icon{position:absolute;left:13px;top:50%;transform:translateY(-50%);z-index:1;width:18px;height:18px;color:#7095c5}.fn-sr-only{position:absolute!important;width:1px!important;height:1px!important;padding:0!important;margin:-1px!important;overflow:hidden!important;clip:rect(0,0,0,0)!important;white-space:nowrap!important;border:0!important}
      .best-v4-metrics{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:0}.vpn-current-panel .best-v4-metrics{grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}.best-v4-pill{min-width:0;display:grid;grid-template-columns:30px minmax(0,1fr);grid-template-rows:auto auto;column-gap:8px;align-items:center}.best-v4-pill>.metric-icon{grid-row:1/3}.best-v4-pill>.metric-copy{min-width:0}.best-v4-pill span.metric-label{display:block;color:#adbed3;font-size:11px;line-height:1.15}.metric-value-line{display:flex;align-items:center;gap:7px;min-width:0;margin-top:4px;flex-wrap:wrap}.best-v4-pill b{display:block;font-size:17px;line-height:1.1;white-space:nowrap;color:#f7f9fd}.best-v4-pill.speed b{color:#55e9aa}.metric-delta{display:inline-flex;align-items:center;min-height:19px;padding:2px 6px;border-radius:999px;font-size:9px;font-weight:780;white-space:nowrap}.metric-delta.good{background:rgba(20,132,93,.38);color:#64e8b2}.metric-delta.bad{background:rgba(121,42,52,.36);color:#ff98a1}
      .vpn-current-panel .best-v4-pill{min-height:78px;padding:12px 11px;border:1px solid #315985;border-radius:11px;background:#0d1c2e}.vpn-current-panel .best-v4-pill span.metric-label{font-size:11px}.vpn-current-panel .best-v4-pill b{font-size:20px}.vpn-current-panel .best-v4-pill.speed b{font-size:20px}.best-quality{margin:12px 0 11px;color:#9eb2cb;font-size:12px;line-height:1.35}.vpn-current-panel>#bestServerCheckCurrent{width:100%;min-height:48px;justify-content:center;background:linear-gradient(180deg,#347eff,#2367e7);border-color:#69a0ff;border-radius:11px;font-size:14px;box-shadow:0 8px 22px rgba(27,94,218,.18)}.vpn-current-panel>.action-row{display:grid;grid-template-columns:1fr 1fr;gap:9px;margin-top:9px}.vpn-current-panel>.action-row[hidden]{display:none}.vpn-current-panel>.action-row .btn{min-height:43px;padding:7px 9px;justify-content:center;text-align:center;font-size:12px;background:#0f2035;border-color:#315275;border-radius:11px}.vpn-current-panel>.action-row .btn .button-icon{width:18px;height:18px;color:#6e9fff}
      .current-health{margin-top:12px;padding:11px 12px;border:1px solid rgba(52,226,160,.62);border-radius:11px;background:linear-gradient(90deg,rgba(15,112,80,.26),rgba(13,70,60,.14));color:#bdf9df;font-size:12px;line-height:1.4;display:flex;gap:10px;align-items:flex-start}.current-health:before{content:'✓';flex:0 0 23px;width:23px;height:23px;border-radius:50%;display:grid;place-items:center;background:#36e3a2;color:#052416;font-weight:950;font-size:14px}.current-health.neutral{border-color:#385473;background:#0c1a2b;color:#a9bad1}.current-health.neutral:before{content:'i';background:#4e7eae;color:#eef6ff}.current-help{margin:10px 0 0;color:#96abc5;font-size:11px;line-height:1.48}
      .vpn-section-head{display:flex;align-items:center;justify-content:space-between;gap:14px;margin-bottom:12px}.vpn-section-head h3{margin:0;font-size:20px;line-height:1.15}.vpn-section-head .hint{margin-top:4px;font-size:12px;color:#9eb3cc}.vpn-section-head #bestServerRefresh{min-height:48px;padding:9px 18px;border-radius:11px;background:linear-gradient(180deg,#347eff,#2469e9);border-color:#69a0ff;font-size:14px;box-shadow:0 8px 22px rgba(27,94,218,.18)}.vpn-section-head #bestServerRefresh .button-icon{width:19px;height:19px}.vpn-empty{padding:39px 24px;border:1px dashed #385472;border-radius:13px;color:#aab9cb;font-size:13px;line-height:1.5;background:rgba(7,18,31,.18)}
      .best-v4-result,.best-v4-result.show{padding:0;border:0;background:none}.best-v4-result.show{display:grid;gap:12px}.vpn-option{min-height:145px;padding:13px 14px 12px;border:1px solid #315276;background:linear-gradient(155deg,#122941,#0e1e32);border-radius:14px;box-shadow:inset 0 1px rgba(255,255,255,.018)}.vpn-option.vpn-best{border-color:#1fd697;background:linear-gradient(155deg,rgba(12,83,68,.74),rgba(8,33,43,.97));box-shadow:inset 0 0 0 1px rgba(25,207,137,.13),0 0 22px rgba(22,193,132,.04)}.vpn-option.vpn-rejected{border-color:#db4858;background:linear-gradient(155deg,rgba(69,27,39,.76),rgba(19,27,42,.97))}.vpn-option-head{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:11px}.vpn-option-title{display:flex;align-items:center;gap:11px;min-width:0}.vpn-option-title .flag-icon{width:34px;height:24px;border-radius:4px}.vpn-option-title h4{font-size:17px;line-height:1.2;margin:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;letter-spacing:-.01em}.vpn-option-actions{display:flex;align-items:center;gap:10px;flex-shrink:0}.vpn-state-badge{display:inline-flex;align-items:center;gap:7px;min-height:34px;padding:6px 12px;border:1px solid #405e80;border-radius:999px;color:#c1cfe0;background:#14263b;font-size:11px;font-weight:820;white-space:nowrap}.vpn-best .vpn-state-badge{border-color:#20d895;color:#5cebb3;background:rgba(13,90,65,.42)}.vpn-rejected .vpn-state-badge{border-color:#df5060;color:#ff7885;background:rgba(100,28,41,.43)}.vpn-option .btn{min-height:40px;padding:7px 14px;border-radius:10px;font-size:12px;justify-content:center}.vpn-best .vpn-option-apply{background:linear-gradient(180deg,#347eff,#2367e7);border-color:#69a0ff}.vpn-option-retry{background:#17293e;border-color:#49698d}.vpn-option .best-v4-metrics{margin:0}.vpn-option .best-v4-pill{min-height:58px;padding:4px 12px 4px 8px;border:0;border-right:1px solid #334a65;border-radius:0;background:none;grid-template-columns:27px minmax(0,1fr);column-gap:8px}.vpn-option .best-v4-pill:last-child{border-right:0}.vpn-option .best-v4-pill span.metric-label{font-size:10px}.vpn-option .best-v4-pill b{font-size:16px}.vpn-option .metric-icon{width:23px;height:23px}.best-v4-reason{display:flex;flex-wrap:wrap;gap:6px;margin-top:10px;font-size:11px;line-height:1.25}.vpn-detail-chip{display:inline-flex;align-items:center;gap:6px;min-height:28px;padding:4px 9px;border:1px solid #3a5877;border-radius:8px;background:#0c1b2d;color:#bdcad9;white-space:nowrap}.vpn-detail-chip.ok{border-color:rgba(52,226,160,.42);color:#a4edcb;background:rgba(12,81,58,.23)}.vpn-detail-chip.bad{border-color:rgba(255,95,109,.53);color:#ffa0a8;background:rgba(104,28,42,.27)}.vpn-rejected-note{margin:9px 0 0;color:#b9c6d7;font-size:11px;line-height:1.4}
      .best-v4-status{margin-top:12px;padding:10px 12px;border:1px solid #31699f;border-radius:11px;background:linear-gradient(90deg,#0c3155,#0a2846);color:#c3daf4;font-size:11px;line-height:1.4;min-height:0}.best-v4-status:empty{display:none}.best-v4-status.busy{color:#d5e7fb}.best-v4-status.summary{display:flex;align-items:flex-start;gap:10px}.best-v4-status.summary .status-icon{flex:0 0 22px;width:22px;height:22px;color:#8fc5ff}.best-v4-status.summary .summary-copy{display:grid;gap:2px}.best-v4-status.summary .summary-main{color:#c6dcf4}.best-v4-status.summary .summary-sub{color:#9fb8d4}.vpn-measure-note{display:none}
      #bestServerAdvanced{margin:0 4px;padding:0;display:grid;grid-template-columns:1fr;gap:8px;background:transparent;border:0;min-height:0!important}#bestServerAdvanced h3{margin:0;font-size:18px;line-height:1.2}#bestServerAdvanced:before{content:'ПОИСК ПО СТРАНЕ ИЛИ ГОРОДУ';font-size:10px;font-weight:860;letter-spacing:.1em;color:#9fb2ca;order:1}#bestServerAdvanced h3{order:0}#bestServerAdvanced #profilesList.profiles{order:2;display:grid!important;grid-template-columns:minmax(260px,.88fr) minmax(360px,1.45fr);gap:10px;align-items:end;margin:0!important;min-height:0!important}#profilesList .field{position:relative;padding:0;border:0;background:none;min-height:0!important}#profilesList .field label{display:none}#profilesList .profile-combobox{margin:0!important;min-height:0!important}#profilesList input,#profilesTrigger{min-height:48px;background:#0b1929;border-color:#315276;border-radius:10px;font-size:12px}#profilesList input{padding-left:42px}#profilesList #selectedProfileCard{grid-column:1/-1;margin:0;padding:8px 10px;border-color:#2f4d6e;background:#0c1a2b;font-size:10px}#bestServerAdvanced .action-row{order:3;display:flex;gap:8px;margin:0}#bestServerAdvanced .action-row[hidden]{display:none}#bestServerAdvanced .btn{min-height:39px;padding:7px 11px;font-size:11px}#bestServerAdvanced:has(#exactConnectRow[hidden]) #selectedProfileCard{display:none}#quickNetworkGuard{display:none}
      .flag-dk{background:linear-gradient(to right,transparent 0 31%,#fff 31% 42%,transparent 42%),linear-gradient(to bottom,transparent 0 42%,#fff 42% 57%,transparent 57%),#c8102e}.flag-no{background:linear-gradient(to right,transparent 0 28%,#fff 28% 46%,transparent 46%),linear-gradient(to bottom,transparent 0 38%,#fff 38% 63%,transparent 63%),#ba0c2f}.flag-no:after{content:'';position:absolute;inset:0;background:linear-gradient(to right,transparent 0 33%,#00205b 33% 41%,transparent 41%),linear-gradient(to bottom,transparent 0 45%,#00205b 45% 56%,transparent 56%)}.flag-se{background:linear-gradient(to right,transparent 0 31%,#fecc00 31% 42%,transparent 42%),linear-gradient(to bottom,transparent 0 43%,#fecc00 43% 58%,transparent 58%),#006aa7}.flag-fi{background:linear-gradient(to right,transparent 0 30%,#003580 30% 45%,transparent 45%),linear-gradient(to bottom,transparent 0 40%,#003580 40% 60%,transparent 60%),#fff}.flag-is{background:linear-gradient(to right,transparent 0 29%,#fff 29% 48%,transparent 48%),linear-gradient(to bottom,transparent 0 37%,#fff 37% 64%,transparent 64%),#02529c}.flag-is:after{content:'';position:absolute;inset:0;background:linear-gradient(to right,transparent 0 35%,#dc1e35 35% 42%,transparent 42%),linear-gradient(to bottom,transparent 0 46%,#dc1e35 46% 56%,transparent 56%)}.flag-ch{background:#d52b1e}.flag-ch:before{content:'';position:absolute;left:39%;top:18%;width:22%;height:64%;background:#fff}.flag-ch:after{content:'';position:absolute;left:23%;top:39%;width:54%;height:22%;background:#fff}.flag-hr{background:linear-gradient(to bottom,#ff0000 0 33.33%,#fff 33.33% 66.66%,#171796 66.66%)}.flag-hr:after{content:'';position:absolute;left:41%;top:27%;width:18%;height:43%;background:repeating-conic-gradient(#e5232e 0 25%,#fff 0 50%) 0/5px 5px;border:1px solid #1d4d9b}.flag-sk{background:linear-gradient(to bottom,#fff 0 33.33%,#0b4ea2 33.33% 66.66%,#ee1c25 66.66%)}.flag-za{background:linear-gradient(to bottom,#de3831 0 43%,#fff 43% 57%,#002395 57%)}.flag-za:before{content:'';position:absolute;inset:0;background:#007749;clip-path:polygon(0 18%,48% 50%,0 82%,0 64%,28% 50%,0 36%)}.flag-si{background:linear-gradient(to bottom,#fff 0 33.33%,#0056a4 33.33% 66.66%,#ed1c24 66.66%)}.flag-rs{background:linear-gradient(to bottom,#c6363c 0 33.33%,#0c4076 33.33% 66.66%,#fff 66.66%)}.flag-lu{background:linear-gradient(to bottom,#ed2939 0 33.33%,#fff 33.33% 66.66%,#00a1de 66.66%)}
      @media(max-width:1180px){.content{width:calc(100% - 30px)}#quickActionsSection{padding:17px 16px 15px}.best-v4-shell{grid-template-columns:minmax(292px,310px) minmax(0,1fr);gap:0 18px}.vpn-current-panel{padding-right:18px}.best-v4-name{font-size:21px}.vpn-current-panel .best-v4-pill{padding-left:9px;padding-right:8px;grid-template-columns:26px minmax(0,1fr)}.vpn-current-panel .best-v4-pill b{font-size:18px}.vpn-option{padding:11px 11px 10px;min-height:132px}.vpn-option-title h4{font-size:15px}.vpn-option-actions{gap:7px}.vpn-state-badge{padding:5px 9px;font-size:10px}.vpn-option .btn{padding:6px 10px}.vpn-option .best-v4-pill{padding-left:5px;padding-right:7px;grid-template-columns:22px minmax(0,1fr);column-gap:6px}.vpn-option .metric-icon{width:20px;height:20px}.vpn-option .best-v4-pill b{font-size:15px}.vpn-detail-chip{min-height:25px;padding:3px 7px;font-size:10px}.overview-approved-top{gap:16px}}
      @media(max-width:820px){.content{width:calc(100% - 20px);padding-top:10px}.topbar.overview-approved{padding:0 14px}.overview-approved-top{display:none}.best-v4-shell{grid-template-columns:1fr;gap:14px}.vpn-current-panel{border-right:0;border-bottom:1px solid #30435d;padding:0 0 16px}.vpn-current-panel .best-v4-metrics,.vpn-option .best-v4-metrics{grid-template-columns:repeat(2,minmax(0,1fr));gap:8px}.vpn-current-panel .best-v4-pill b,.vpn-current-panel .best-v4-pill.speed b{font-size:16px}.vpn-option .best-v4-pill{border-right:0;border:1px solid #294665;border-radius:9px;padding:8px;grid-template-columns:23px minmax(0,1fr);column-gap:6px}.vpn-option .best-v4-pill b{font-size:14px}.metric-delta{display:none}.vpn-section-head{align-items:flex-start}.vpn-section-head #bestServerRefresh{min-height:42px;padding:8px 11px}.vpn-option-head{align-items:flex-start}.vpn-option-actions{flex-direction:column;align-items:flex-end}.vpn-option-title h4{white-space:normal}.vpn-detail-chip{white-space:normal}#bestServerAdvanced{margin:0}#bestServerAdvanced #profilesList.profiles{grid-template-columns:1fr}.best-v4-name{font-size:20px}}
      @media(min-width:821px) and (max-height:820px){.content{padding-top:8px}.page[data-page-view="overview"] .page-head{margin-bottom:7px}.page[data-page-view="overview"] .page-head h1{font-size:26px}.overview-compact-grid{gap:7px}#quickActionsSection{padding:11px 13px 9px}.best-v4-shell{grid-template-columns:minmax(265px,285px) minmax(0,1fr);gap:0 15px}.vpn-current-panel{padding:2px 14px 0 0}.best-v4-name{font-size:18px}.best-v4-endpoint{margin:5px 0 8px}.vpn-current-panel .best-v4-metrics{gap:6px}.vpn-current-panel .best-v4-pill{min-height:54px;padding:6px;grid-template-columns:20px minmax(0,1fr);column-gap:5px}.vpn-current-panel .metric-icon{width:18px;height:18px}.vpn-current-panel .best-v4-pill b{font-size:14px}.best-quality{margin:5px 0;font-size:10px}.vpn-current-panel>#bestServerCheckCurrent{min-height:34px;font-size:11px}.vpn-current-panel>.action-row{margin-top:5px;gap:6px}.vpn-current-panel>.action-row .btn{min-height:30px;font-size:9px}.current-health{margin-top:5px;padding:5px 7px;font-size:9px}.current-health:before{width:18px;height:18px;flex-basis:18px;font-size:11px}.current-help{margin-top:4px;font-size:8.5px;line-height:1.25}.vpn-section-head{margin-bottom:5px}.vpn-section-head h3{font-size:16px}.vpn-section-head .hint{font-size:9px}.vpn-section-head #bestServerRefresh{min-height:34px;font-size:11px}.best-v4-result.show{gap:5px}.vpn-option{min-height:0;padding:6px 8px}.vpn-option-head{margin-bottom:4px}.vpn-option-title .flag-icon{width:25px;height:17px}.vpn-option-title h4{font-size:12px}.vpn-state-badge{min-height:23px;padding:2px 6px;font-size:8px}.vpn-option .btn{min-height:26px;padding:3px 7px;font-size:9px}.vpn-option .best-v4-pill{min-height:34px;padding:0 5px;grid-template-columns:16px minmax(0,1fr);column-gap:4px}.vpn-option .metric-icon{width:15px;height:15px}.vpn-option .best-v4-pill span.metric-label{font-size:7px}.vpn-option .best-v4-pill b{font-size:11px}.metric-delta{display:none}.best-v4-reason{margin-top:3px;gap:3px}.vpn-detail-chip{min-height:17px;padding:1px 4px;font-size:7.5px}.chip-icon{width:10px;height:10px}.vpn-rejected-note{display:none}.best-v4-status{margin-top:5px;padding:5px 7px;font-size:8px}#bestServerAdvanced{gap:3px}#bestServerAdvanced h3{font-size:14px}#bestServerAdvanced:before{font-size:7px}#profilesList input,#profilesTrigger{min-height:31px;font-size:9px}#bestServerAdvanced .btn{min-height:29px}}
    `;
    document.head.appendChild(style);
  }

  function dnsLabel(status) { return status && status.dns_mode === 'xkeen' ? 'XKeen/Xray DNS' : 'DNS напрямую'; }
  function healthState(status) {
    if (!status) return {healthy: false, label: 'Проверяем состояние'};
    if (!status.xray_online) return {healthy: false, label: 'VPN не работает'};
    if (status.dns_mode === 'xkeen' && !status.dns_out_present) return {healthy: false, label: 'DNS требует внимания'};
    return {healthy: true, label: status.dns_mode === 'xkeen' ? 'VPN + DNS OK' : 'VPN OK · DNS напрямую'};
  }

  function flagClass(code) {
    const value = String(code || '').trim().toLowerCase();
    return /^[a-z]{2}$/.test(value) ? `flag-${value}` : 'flag-unknown';
  }

  function makeRenderedFlag(code) {
    const flag = document.createElement('span');
    flag.className = `flag-icon ${flagClass(code)}`;
    flag.setAttribute('aria-hidden', 'true');
    return flag;
  }

  function profileDisplayName(profile, fallback) {
    return stripProfilePrefix(profile && profile.name || fallback || 'VPN') || 'VPN';
  }

  function statusDisplayName(status) {
    const raw = status && (status.profile_label || (status.country ? `${status.country}${status.city ? ', ' + status.city : ''}` : 'Профиль не определён'));
    return stripProfilePrefix(raw) || 'Профиль не определён';
  }

  function mountOverviewTopbar() {
    injectStyle();
    const topbar = qs('.topbar');
    const actions = qs('.top-actions');
    const overview = qs('.page[data-page-view="overview"]');
    const grid = overview && overview.querySelector('.grid-2');
    const hero = overview && overview.querySelector('.hero');
    if (!topbar || !actions || !grid || !hero) return;
    topbar.classList.add('overview-approved');
    grid.classList.add('overview-compact-grid');
    hero.classList.add('overview-hero-source');
    if (!qs('#overviewApprovedTop')) {
      const summary = document.createElement('div');
      summary.id = 'overviewApprovedTop';
      summary.className = 'overview-approved-top';
      summary.innerHTML = '<div class="overview-approved-fact"><span>Провайдер</span><strong id="topISPValue">—</strong></div><div class="overview-approved-fact"><span>DNS</span><strong id="topDNSValue">—</strong></div>';
      topbar.insertBefore(summary, actions);
    }
    syncOverviewTopbar();
    if (!topbarSyncTimer) topbarSyncTimer = setInterval(syncOverviewTopbar, 2500);
  }

  function renderOverviewTopbarFromStatus(status) {
    if (!status) return;
    setText(qs('#topISPValue'), status.isp_label || status.isp || 'Не определён');
    setText(qs('#topDNSValue'), dnsLabel(status));
    const state = healthState(status);
    const dot = qs('#topDot');
    if (dot) dot.className = 'dot ' + (state.healthy ? 'ok' : 'bad');
    setText(qs('#topStatus'), state.label);
    renderCurrentIdentity(status);
  }

  function syncOverviewTopbar() {
    try {
      if (typeof lastStatus !== 'undefined' && lastStatus) renderOverviewTopbarFromStatus(lastStatus);
    } catch (_) {}
  }

  function installOverviewStatusHook() {
    if (typeof updateStatusViews !== 'function' || updateStatusViews.__freenetApprovedHook) return;
    const previous = updateStatusViews;
    const wrapped = function(status) {
      const result = previous.apply(this, arguments);
      queueMicrotask(() => renderOverviewTopbarFromStatus(status));
      return result;
    };
    wrapped.__freenetApprovedHook = true;
    updateStatusViews = wrapped;
  }

  function metric(candidate, key) {
    if (!candidate) return '—';
    if (key === 'speed') {
      const n = Number(candidate.download_mbps || 0);
      return n > 0 ? `${n.toFixed(n >= 100 ? 0 : 1)} Мбит/с` : '—';
    }
    if (key === 'http') return candidate.application_rtt_ms ? `${candidate.application_rtt_ms} мс` : '—';
    if (key === 'tcp') return candidate.tcp_rtt_ms ? `${candidate.tcp_rtt_ms} мс` : '—';
    if (key === 'jitter') return Number.isFinite(Number(candidate.jitter_ms)) ? `${candidate.jitter_ms} мс` : '—';
    return '—';
  }

  function httpDelta(candidate, baseline) {
    const value = Number(candidate && candidate.application_rtt_ms || 0);
    const current = Number(baseline && baseline.application_rtt_ms || 0);
    if (!value || !current || candidate === baseline || candidate.current) return null;
    const diff = current - value;
    if (!diff) return {text: 'так же', tone: ''};
    return {text: `на ${Math.abs(diff)} мс ${diff > 0 ? 'быстрее' : 'медленнее'}`, tone: diff > 0 ? 'good' : 'bad'};
  }

  function metricPill(label, value, key, trustedSpeed, delta = null) {
    const node = document.createElement('div');
    node.className = 'best-v4-pill' + (key === 'speed' && trustedSpeed ? ' speed' : '');
    node.dataset.metric = key;
    node.appendChild(makeIcon(key, 'metric-icon'));
    const copy = document.createElement('div'); copy.className = 'metric-copy';
    const name = document.createElement('span'); name.className = 'metric-label'; name.textContent = label;
    const valueLine = document.createElement('div'); valueLine.className = 'metric-value-line';
    const number = document.createElement('b'); number.textContent = value; valueLine.appendChild(number);
    if (delta && delta.text) {
      const note = document.createElement('span'); note.className = `metric-delta${delta.tone ? ' ' + delta.tone : ''}`; note.textContent = delta.text; valueLine.appendChild(note);
    }
    copy.append(name, valueLine); node.appendChild(copy);
    return node;
  }

  function renderMetrics(root, candidate, baseline = null) {
    if (!root) return;
    root.textContent = '';
    root.appendChild(metricPill('Скорость VPN', metric(candidate, 'speed'), 'speed', candidate?.eligible === true));
    root.appendChild(metricPill('Отклик сайтов', metric(candidate, 'http'), 'http', false, httpDelta(candidate, baseline)));
    root.appendChild(metricPill('Связь с сервером', metric(candidate, 'tcp'), 'tcp', false));
    root.appendChild(metricPill('Стабильность', metric(candidate, 'jitter'), 'jitter', false));
  }

  function currentCandidate(data) {
    return Array.isArray(data && data.candidates) ? data.candidates.find(item => item && item.current) || null : null;
  }

  function renderCurrentIdentity(status) {
    if (!status) return;
    setText(qs('#bestCurrentName'), statusDisplayName(status));
    setText(qs('#bestCurrentEndpoint'), status.endpoint || '—');
    const flag = qs('#bestCurrentFlag');
    if (flag) {
      flag.className = `flag-icon ${flagClass(status.country_code)}`;
      flag.hidden = !status.country_code;
    }
    if (!applyBusy && currentQuality && currentQuality.endpoint !== status.endpoint) {
      currentQuality = null;
      clearAlternatives('Текущий сервер изменился. Подберите варианты заново.');
      renderMetrics(qs('#bestCurrentMetrics'), null);
      renderCurrentHealth(null);
      setText(qs('#bestCurrentQuality'), 'Качество ещё не проверено');
    }
  }

  function renderCurrentHealth(candidate) {
    const box = qs('#bestCurrentHealth');
    if (!box) return;
    if (!candidate) {
      box.className = 'current-health neutral';
      box.textContent = 'Проверка качества ещё не выполнялась.';
      return;
    }
    if (candidate.eligible) {
      box.className = 'current-health';
      box.textContent = 'Текущий VPN работает стабильно.\nСкорость и отклик в норме.';
    } else {
      box.className = 'current-health neutral';
      box.textContent = 'VPN работает, но для полной оценки качества данных пока недостаточно.';
    }
  }

  function renderCurrentQuality(data) {
    const candidate = currentCandidate(data);
    if (candidate) currentQuality = candidate;
    const shown = candidate || currentQuality;
    if (candidate) {
      setText(qs('#bestCurrentName'), profileDisplayName(candidate, 'Текущий VPN'));
      setText(qs('#bestCurrentEndpoint'), candidate.endpoint || '—');
      const flag = qs('#bestCurrentFlag');
      if (flag) {
        flag.className = `flag-icon ${flagClass(profileCode(candidate))}`;
        flag.hidden = !profileCode(candidate);
      }
    }
    renderMetrics(qs('#bestCurrentMetrics'), shown);
    renderCurrentHealth(shown);
    const stamp = new Date(data && data.scanned_at || Date.now());
    const time = Number.isNaN(stamp.getTime()) ? '' : stamp.toLocaleTimeString('ru-RU', {hour: '2-digit', minute: '2-digit'});
    if (shown) {
      const measured = Number(shown.download_mbps) > 0;
      setText(qs('#bestCurrentQuality'), `Последняя проверка: ${time || 'сейчас'}${measured ? ' · ' + metric(shown, 'speed') : ''}`);
      setText(qs('#bestServerStatus'), candidate ? 'Проверка текущего VPN завершена.' : 'Проверка текущего VPN завершена. Показан последний подтверждённый замер.');
    } else {
      setText(qs('#bestCurrentQuality'), 'Недостаточно данных для оценки');
      setText(qs('#bestServerStatus'), 'Текущий профиль не удалось определить. Другие серверы не проверялись.');
    }
    if (shown?.download_issue && Number(shown.download_mbps || 0) <= 0) setText(qs('#bestCurrentQuality'), 'Замер скорости: ' + shown.download_issue);
  }

  function recommendationReason(data, candidate) {
    if (!candidate) return [];
    const current = currentCandidate(data) || currentQuality;
    const parts = [];
    const speed = Number(candidate.download_mbps || 0), currentSpeed = Number(current && current.download_mbps || 0);
    const http = Number(candidate.application_rtt_ms || 0), currentHTTP = Number(current && current.application_rtt_ms || 0);
    if (speed > 0 && currentSpeed > 0) {
      const delta = Math.round((speed - currentSpeed) * 10) / 10;
      parts.push(delta === 0 ? 'Скорость такая же' : `Скорость ${delta > 0 ? '+' : '−'}${Math.abs(delta).toFixed(1)} Мбит/с`);
    }
    if (http > 0 && currentHTTP > 0) {
      parts.push(http === currentHTTP ? 'Отклик такой же' : `Отклик ${http < currentHTTP ? 'быстрее' : 'медленнее'} на ${Math.abs(currentHTTP - http)} мс`);
    }
    return parts;
  }

  function appendDetailChip(root, text, tone = '') {
    if (!root || !text) return;
    const chip = document.createElement('span');
    chip.className = 'vpn-detail-chip' + (tone ? ' ' + tone : '');
    if (tone === 'ok') chip.appendChild(makeIcon('check', 'chip-icon'));
    if (tone === 'bad') chip.appendChild(makeIcon('alert', 'chip-icon'));
    const label = document.createElement('span'); label.textContent = String(text).replace(/[.]$/, ''); chip.appendChild(label);
    root.appendChild(chip);
  }

  function friendlyRejection(text) {
    const value = String(text || '').trim();
    const lower = value.toLowerCase();
    if (lower.includes('timeout') || lower.includes('тайма')) return 'Таймауты при замере';
    if (lower.includes('стабил') || lower.includes('jitter') || lower.includes('колеб')) return 'Нестабильное соединение';
    if (lower.includes('speedtest') && lower.includes('0/')) return 'Speedtest не завершён';
    if (lower.includes('скорость') && lower.includes('не измер')) return 'Скорость не измерена';
    return value.length > 42 ? value.slice(0, 39) + '…' : value;
  }

  function clearAlternatives(message) {
    alternatives = [];
    recommendation = null;
    const box = qs('#bestServerResult');
    if (box) { box.textContent = ''; box.classList.remove('show'); }
    const empty = qs('#bestServerEmpty');
    if (empty) { empty.hidden = false; empty.textContent = message || 'Подберите серверы, чтобы увидеть проверенные варианты.'; }
    const status = qs('#bestServerStatus'); if (status) status.classList.remove('summary');
  }

  function comparisonCandidates(data, current) {
    const seen = new Set([data && data.current_endpoint, current && current.endpoint].filter(Boolean));
    const pool = (Array.isArray(data && data.candidates) ? data.candidates : []).filter(candidate => {
      if (!candidate || !candidate.tested || candidate.current || isRussianProfile(candidate) || !candidate.id || !candidate.endpoint || seen.has(candidate.endpoint)) return false;
      if (!candidate.available && !candidate.reachable) return false;
      seen.add(candidate.endpoint);
      return true;
    });
    const preferred = pool.filter(candidate => candidate.eligible === true);
    const diagnostic = pool.filter(candidate => candidate.eligible !== true);
    return preferred.concat(diagnostic).slice(0, 3);
  }

  function sameCandidate(a, b) {
    return !!(a && b && ((a.id && b.id && a.id === b.id) || (a.endpoint && b.endpoint && a.endpoint === b.endpoint)));
  }

  function stateForCandidate(data, candidate) {
    if (!candidate.eligible) return {kind: 'rejected', label: 'Не прошёл проверку', icon: 'alert'};
    if (data && data.recommendation && !data.recommendation.current && sameCandidate(data.recommendation, candidate)) {
      return {kind: 'best', label: 'Лучший вариант', icon: 'trophy'};
    }
    return {kind: 'comparison', label: 'Для сравнения', icon: 'compare'};
  }

  function renderSummaryStatus(data, states) {
    const status = qs('#bestServerStatus');
    if (!status) return;
    const best = states.filter(state => state.kind === 'best').length;
    const comparisons = states.filter(state => state.kind === 'comparison').length;
    const rejected = states.filter(state => state.kind === 'rejected').length;
    status.textContent = ''; status.classList.add('summary');
    status.appendChild(makeIcon('info', 'status-icon'));
    const copy = document.createElement('div'); copy.className = 'summary-copy';
    const main = document.createElement('div'); main.className = 'summary-main';
    const pieces = [];
    if (best) pieces.push(`${best} подходит`);
    if (comparisons) pieces.push(`${comparisons} для сравнения`);
    if (rejected) pieces.push(`${rejected} не прошёл проверку`);
    main.textContent = `Проверено профилей: ${data.profiles_scanned || 0}. Показано ${states.length} вариант${states.length === 1 ? '' : states.length < 5 ? 'а' : 'ов'}: ${pieces.join(', ') || 'нет подходящих'}.`;
    copy.appendChild(main);
    if (data.recommendation && data.recommendation.current) {
      const sub = document.createElement('div'); sub.className = 'summary-sub'; sub.textContent = 'Текущий VPN остаётся предпочтительным.'; copy.appendChild(sub);
    }
    status.appendChild(copy);
  }

  function renderBestResult(data) {
    clearAlternatives();
    const current = currentCandidate(data);
    if (current) renderCurrentQuality({scanned_at: data.scanned_at, candidates: [current]});
    const baseline = current || currentQuality;
    alternatives = comparisonCandidates(data, current);
    const box = qs('#bestServerResult');
    if (!alternatives.length) {
      clearAlternatives('Проверенных зарубежных вариантов для сравнения сейчас нет. Текущий VPN сохранён.');
      setText(qs('#bestServerStatus'), data && data.message || 'Сравнение сейчас недоступно.');
      return;
    }
    const empty = qs('#bestServerEmpty'); if (empty) empty.hidden = true;
    box.classList.add('show');
    const states = [];
    alternatives.forEach((candidate, index) => {
      const state = stateForCandidate(data, candidate); states.push(state);
      const row = document.createElement('article');
      row.className = `vpn-option vpn-${state.kind}`;
      const head = document.createElement('div'); head.className = 'vpn-option-head';
      const title = document.createElement('div'); title.className = 'vpn-option-title';
      title.appendChild(makeRenderedFlag(profileCode(candidate)));
      const name = document.createElement('h4'); name.textContent = profileDisplayName(candidate, 'VPN');
      if (index === 0) name.id = 'bestServerName';
      title.appendChild(name); head.appendChild(title);
      const actions = document.createElement('div'); actions.className = 'vpn-option-actions';
      const badge = document.createElement('span'); badge.className = 'vpn-state-badge'; badge.append(makeIcon(state.icon, 'status-icon'), document.createTextNode(state.label)); actions.appendChild(badge);
      if (candidate.eligible) {
        const button = document.createElement('button'); button.type = 'button'; button.className = 'btn secondary vpn-option-apply'; button.dataset.candidateId = candidate.id;
        if (index === 0) button.id = 'bestServerApply';
        button.textContent = state.kind === 'best' ? 'Переключиться' : 'Использовать';
        button.setAttribute('aria-label', `${button.textContent}: ${name.textContent}`);
        actions.appendChild(button);
      } else {
        const retry = document.createElement('button'); retry.type = 'button'; retry.className = 'btn secondary vpn-option-retry'; retry.dataset.candidateId = candidate.id; retry.textContent = 'Проверить снова'; actions.appendChild(retry);
      }
      head.appendChild(actions);
      const metrics = document.createElement('div'); metrics.className = 'best-v4-metrics'; renderMetrics(metrics, candidate, baseline);
      const reason = document.createElement('div'); reason.className = 'best-v4-reason'; if (index === 0) reason.id = 'bestServerReason';
      const legacyDeltas = document.createElement('span'); legacyDeltas.className = 'fn-sr-only comparison-deltas'; legacyDeltas.textContent = recommendationReason(data, candidate).join(' · '); reason.appendChild(legacyDeltas);
      if (candidate.eligible) {
        appendDetailChip(reason, `Speedtest ${candidate.media_samples || 0}/4`, 'ok');
        appendDetailChip(reason, `Сервисы ${candidate.service_ok || 0}/${candidate.service_total || 0}`, 'ok');
        appendDetailChip(reason, 'Доступен', 'ok');
      } else {
        appendDetailChip(reason, `Speedtest ${candidate.media_samples || 0}/4`, 'bad');
        appendDetailChip(reason, `Сервисы ${candidate.service_ok || 0}/${candidate.service_total || 0}`, candidate.service_ok === candidate.service_total && candidate.service_total > 0 ? 'ok' : 'bad');
        const diagnostics = Array.isArray(candidate.rejections) && candidate.rejections.length ? candidate.rejections.slice(0, 2) : ['Недостаточно подтверждённых данных'];
        diagnostics.forEach(text => appendDetailChip(reason, friendlyRejection(text), 'bad'));
        const originals = document.createElement('span'); originals.className = 'fn-sr-only rejection-originals'; originals.textContent = (candidate.rejections || []).join(' · '); reason.appendChild(originals);
      }
      row.append(head, metrics, reason);
      if (!candidate.eligible) {
        const note = document.createElement('p'); note.className = 'vpn-rejected-note'; note.textContent = 'Сервер не прошёл все проверки и не рекомендуется для переключения.'; row.appendChild(note);
      }
      box.appendChild(row);
    });
    renderSummaryStatus(data, states);
  }

  function mountBestServerUI() {
    const quick = qs('#quickActionsSection');
    if (!quick) return null;
    const countries = quick.querySelector('.quick-layout'); if (countries) countries.remove();
    const alternativesTitle = qs('.vpn-section-head h3'); if (alternativesTitle) alternativesTitle.textContent = 'Результаты проверки';
    const alternativesHint = qs('.vpn-section-head .hint'); if (alternativesHint) alternativesHint.textContent = 'Топ-3 варианта на основе реальных измерений';
    const profilesList = qs('#profilesList');
    const profileLabel = profilesList && profilesList.querySelector('label[for="profileSearch"]'); if (profileLabel) profileLabel.textContent = 'Поиск по стране или городу';
    const update = qs('#updateBtn'); if (update) { setButtonLabel(update, 'Обновить и проверить', 'refresh'); update.title = 'Получить свежий endpoint текущего профиля и автоматически проверить качество'; update.classList.remove('primary'); update.classList.add('secondary'); }
    const rotate = qs('#rotateBtn'); if (rotate) { setButtonLabel(rotate, 'Сменить сервер', 'swap'); rotate.classList.remove('primary'); rotate.classList.add('secondary'); }
    const currentPanel = qs('.vpn-current-panel');
    const routine = update && update.closest('.action-row');
    if (routine && currentPanel && routine.parentNode !== currentPanel) currentPanel.appendChild(routine);
    const currentCheck = qs('#bestServerCheckCurrent'); if (currentCheck) { currentCheck.classList.remove('secondary'); currentCheck.classList.add('primary'); }
    const refresh = qs('#bestServerRefresh'); if (refresh && !scanBusy) setButtonLabel(refresh, 'Подобрать серверы', 'search');
    if (currentPanel && !qs('#bestCurrentHealth')) {
      const health = document.createElement('div'); health.id = 'bestCurrentHealth'; health.className = 'current-health neutral'; health.textContent = 'Проверка качества ещё не выполнялась.';
      currentPanel.appendChild(health);
      const help = document.createElement('p'); help.id = 'bestCurrentHelp'; help.className = 'current-help'; help.textContent = 'FreeNet сравнивает реальный отклик через каждый VPN, затем глубоко проверяет лучшие варианты. Скорость — короткий тест загрузки, не скорость тарифа. Российские серверы исключены из поиска.';
      currentPanel.appendChild(help);
    }
    const measureNote = qs('.vpn-measure-note'); if (measureNote) measureNote.remove();
    const status = qs('#bestServerStatus'), alternativesPanel = qs('.vpn-alternatives-panel');
    if (status && alternativesPanel && status.parentNode !== alternativesPanel) alternativesPanel.appendChild(status);
    const guard = qs('#quickNetworkGuard');
    let manual = qs('#bestServerAdvanced');
    const topbar = qs('.topbar.overview-approved') || qs('.topbar');
    const topSummary = qs('#overviewApprovedTop');
    const topActions = qs('.top-actions');
    if (profilesList && !manual) {
      manual = document.createElement('section'); manual.id = 'bestServerAdvanced';
    }
    if (manual) manual.classList.add('fn-topbar-vpn-picker');
    if (manual && topbar && manual.parentNode !== topbar) topbar.insertBefore(manual, topSummary || topActions || null);
    if (manual && profilesList && profilesList.parentNode !== manual) manual.appendChild(profilesList);
    const exact = qs('#exactConnectRow'); if (manual && exact && exact.parentNode !== manual) manual.appendChild(exact);
    if (manual && guard && guard.parentNode !== manual) manual.appendChild(guard);
    const searchField = profilesList && profilesList.querySelector('.field');
    if (searchField && !searchField.querySelector('.manual-search-icon')) searchField.prepend(makeIcon('search', 'manual-search-icon'));
    renderMetrics(qs('#bestCurrentMetrics'), currentQuality); renderCurrentHealth(currentQuality);
    try { if (typeof lastStatus !== 'undefined' && lastStatus) renderCurrentIdentity(lastStatus); } catch (_) {}
    return qs('#bestServerShell');
  }

  function setBusy(mode) {
    scanMode = mode;
    scanBusy = !!mode;
    const current = qs('#bestServerCheckCurrent'), best = qs('#bestServerRefresh');
    if (current) { current.disabled = scanBusy || applyBusy || externalBusy; current.textContent = mode === 'current' ? 'Проверяем текущий…' : 'Проверить текущий VPN'; }
    if (best) { best.disabled = scanBusy || applyBusy || externalBusy; setButtonLabel(best, mode === 'best' ? 'Подбираем…' : 'Подобрать серверы', 'search'); }
    document.querySelectorAll('.vpn-option-apply,.vpn-option-retry').forEach(button => { button.disabled = scanBusy || applyBusy || externalBusy; });
    const manual = qs('#bestServerAdvanced'); if (manual) manual.inert = scanBusy || applyBusy || externalBusy;
    const maintenance = qs('.vpn-current-panel>.action-row'); if (maintenance) maintenance.inert = scanBusy || applyBusy || externalBusy;
    const status = qs('#bestServerStatus'); if (status) status.classList.toggle('busy', !!mode || applyBusy);
    qs('#bestServerShell')?.setAttribute('aria-busy', String(scanBusy || applyBusy));
  }

  async function requestQuality(path, mode) {
    const id = crypto.randomUUID(), started = Date.now();
    const panel = document.createElement('div'); panel.id = 'fnQualityProgress'; panel.setAttribute('role','dialog'); panel.setAttribute('aria-modal','true'); panel.setAttribute('aria-label','Проверка VPN'); panel.style.cssText='position:fixed;inset:0;z-index:950;background:#030911cc;display:grid;place-items:center;padding:20px;backdrop-filter:blur(5px)';
    const card = document.createElement('section'); card.style.cssText='width:min(440px,100%);box-sizing:border-box;padding:28px;background:#101e30;border:1px solid #304963;border-radius:18px;color:#e5eefb';
    const title=document.createElement('h2');title.style.cssText='font-size:20px;margin:0 0 16px';title.textContent=mode==='current'?'Проверяем текущий VPN':'Подбираем серверы';
    const stage=document.createElement('p');stage.textContent='Запускаем проверку на роутере…';stage.setAttribute('aria-live','polite');
    const progress=document.createElement('progress');progress.style.cssText='width:100%;accent-color:#5189ff';progress.setAttribute('aria-label','Проверка выполняется');
    const timer=document.createElement('p');timer.style.cssText='font-size:13px;color:#9eb4d2';
    const tick=()=>{timer.textContent=`Прошло ${Math.floor((Date.now()-started)/1000)} с · текущий VPN не переключается`;};tick();
    card.append(title,stage,progress,timer);panel.append(card);document.body.append(panel);
    const controls=qs('#controlCenter'),wasInert=controls?.inert,focused=document.activeElement;if(controls)controls.inert=true;card.tabIndex=-1;card.focus();const ticker=setInterval(tick,1000);
    try {
      const readState=()=>fetch(`${path}?job=status&id=${encodeURIComponent(id)}`,{cache:'no-store',signal:AbortSignal.timeout(10000)});
      let response;
      try{response=await fetch(`${path}?job=start&id=${encodeURIComponent(id)}`,{cache:'no-store',signal:AbortSignal.timeout(10000)});}catch(_){stage.textContent='Восстанавливаем связь и проверяем состояние задачи…';response=await readState();}
      while(response.status===202){
        const job=await response.json();if(job.id!==id||job.mode!==mode)throw new Error('Quality job identity mismatch');
        if(job.state==='completed'&&job.result)return new Response(JSON.stringify(job.result),{status:200});
        if(job.state==='failed')return new Response(JSON.stringify({success:false,error:job.error||'Проверка не завершена'}),{status:503});
        if(job.state!=='running')throw new Error('Invalid quality job state');
        stage.textContent=job.stage==='quality'?`Глубоко проверяем лучшие VPN · завершено ${job.completed} из ${job.total}`:job.stage==='preflight'?`Сравниваем реальный отклик через VPN · завершено ${job.completed} из ${job.total}`:job.stage==='tcp'?'Проверяем доступность серверов…':'Получаем профили подписки…';
        if((job.stage==='quality'||job.stage==='preflight')&&job.total>0){progress.max=job.total;progress.value=job.completed}else progress.removeAttribute('value');
        if(Date.now()-started>180000)throw new DOMException('Quality job timeout','TimeoutError');await wait(1000);response=await readState();
      }
      return response;
    } finally {clearInterval(ticker);panel.remove();if(controls)controls.inert=wasInert;if(focused?.isConnected)focused.focus();}
  }

  async function scanCurrentVPN() {
    if (scanBusy || applyBusy || externalBusy) return;
    try {
      mountBestServerUI(); setBusy('current'); setText(qs('#bestServerStatus'),'Проверяем текущий VPN… Это может занять до минуты.');
      const response=await requestQuality('/api/vpn/current-quality','current');const body=await response.json().catch(()=>null);
      if(!response.ok||!body||!body.success){setText(qs('#bestServerStatus'),(body&&body.error)||'Не удалось проверить текущий VPN.');return;}renderCurrentQuality(body);
    } catch (_) {setText(qs('#bestServerStatus'),'Не удалось завершить проверку текущего VPN. Проверьте связь с FreeNet.');} finally {setBusy(false);}
  }

  async function scanBestServer() {
    if (scanBusy || applyBusy || externalBusy) return;
    try {
      mountBestServerUI();setBusy('best');clearAlternatives('Сравниваем реальный отклик и измеряем качество…');setText(qs('#bestServerStatus'),'Сравниваем зарубежные серверы… Текущий VPN продолжает работать.');
      const response=await requestQuality('/api/vpn/best-foreign','best');const body=await response.json().catch(()=>null);
      if(!response.ok||!body||body.success!==true||!Array.isArray(body.candidates)){
        clearAlternatives('Подбор не завершён. Наличие подходящих замен пока неизвестно.');
        const detail=body&&typeof body.error==='string'?body.error:response.status===504?'Шлюз не дождался ответа FreeNet.':response.status===502?'Шлюз не получил корректный ответ FreeNet.':response.status===401?'Требуется войти в FreeNet заново.':response.ok?'Ответ FreeNet не содержит результатов проверки.':'Сервер вернул ошибку.';
        setText(qs('#bestServerStatus'),`Подбор не завершён (HTTP ${response.status}). ${detail}`);return;
      }
      renderBestResult(body);if(body.partial)setText(qs('#bestServerStatus'),'Проверка завершена в пределах лимита времени. Показаны только измеренные варианты; часть кандидатов не проверена.');
    } catch(error){clearAlternatives('Подбор не завершён. Наличие подходящих замен пока неизвестно.');setText(qs('#bestServerStatus'),error&&error.name==='TimeoutError'?'Подбор не завершён: превышено время ожидания ответа (180 с).':'Подбор не завершён: связь с FreeNet прервалась.');} finally {setBusy(false);}
  }

  async function waitForEndpoint(expected) {
    for(let i=0;i<35;i++){try{const response=await fetch('/api/status',{cache:'no-store'});if(response.ok){const status=await response.json();if(status&&!status.busy&&!status.updater_busy&&status.xray_online&&status.endpoint===expected)return status;}}catch(_){}await wait(850);}return null;
  }

  async function applyCandidate(candidate) {
    if(applyBusy||scanBusy||externalBusy||!candidate||candidate.current||!candidate.id||!candidate.eligible||isRussianProfile(candidate))return;
    recommendation=candidate;applyBusy=true;setBusy(false);
    const apply=Array.from(document.querySelectorAll('.vpn-option-apply')).find(button=>button.dataset.candidateId===candidate.id);if(apply){apply.disabled=true;apply.textContent='Переключаем…';}
    setText(qs('#bestServerStatus'),'Переключаем VPN и проверяем соединение…');const expectedEndpoint=candidate.endpoint;
    try{
      const response=await fetch('/api/network-profile/apply',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({operation:'provider',profile_id:candidate.id,confirm:true})});const body=await response.json().catch(()=>null);
      if(!response.ok||!body||!body.success){const detail=body&&(body.primary_error||body.error);setText(qs('#bestServerStatus'),detail||'Результат переключения не подтверждён. Не повторяйте операцию.');if(!body||body.result_unknown)recommendation=null;return;}
      const status=await waitForEndpoint(expectedEndpoint);if(!status){setText(qs('#bestServerStatus'),'Переключение ещё не подтверждено. Проверьте состояние системы перед повторной попыткой.');return;}
      recommendation=null;currentQuality=null;renderOverviewTopbarFromStatus(status);renderMetrics(qs('#bestCurrentMetrics'),null);renderCurrentHealth(null);setText(qs('#bestCurrentQuality'),'Качество ещё не проверено');setText(qs('#bestServerStatus'),'VPN переключён. Соединение проверено.');
    }catch(_){recommendation=null;setText(qs('#bestServerStatus'),'Связь прервалась во время переключения. Результат не подтверждён — проверьте состояние системы перед повторной попыткой.');}finally{clearAlternatives('Результаты подбора израсходованы. Для нового переключения подберите серверы снова.');applyBusy=false;setBusy(false);}
  }

  async function retryCandidate(candidate, button) {
    if (scanBusy || applyBusy || externalBusy || !candidate || !candidate.id) return;
    const original = button && button.textContent;
    try {
      setBusy('retry');
      if (button) { button.disabled = true; button.textContent = 'Проверяем…'; }
      setText(qs('#bestServerStatus'), `Повторно проверяем только ${profileDisplayName(candidate, 'этот сервер')}… Текущий VPN не изменяется.`);
      const response = await fetch(`/api/vpn/best-candidate?id=${encodeURIComponent(candidate.id)}`, {cache:'no-store', signal:AbortSignal.timeout(50000)});
      const body = await response.json().catch(()=>null);
      if (!response.ok || !body || body.success !== true || !Array.isArray(body.candidates)) {
        setText(qs('#bestServerStatus'), (body && (body.message || body.error)) || 'Повторная проверка не завершена. Текущий VPN не изменён.');
        return;
      }
      const updated = body.candidates.find(item => item && item.id === candidate.id);
      if (!updated) {
        setText(qs('#bestServerStatus'), 'Повторная проверка не вернула выбранный сервер. Текущий VPN не изменён.');
        return;
      }
      alternatives = alternatives.map(item => item.id === candidate.id ? updated : item);
      const candidates = currentQuality ? [Object.assign({}, currentQuality, {current:true}), ...alternatives] : alternatives.slice();
      renderBestResult(Object.assign({}, body, {candidates, profiles_scanned: 1, profiles_total: 1}));
      setText(qs('#bestServerStatus'), body.message || (updated.eligible ? 'Сервер прошёл повторную проверку.' : 'Сервер снова не прошёл все проверки.'));
    } catch (_) {
      setText(qs('#bestServerStatus'), 'Повторная проверка прервалась. Текущий VPN не изменён.');
    } finally {
      if (button && button.isConnected) { button.disabled = false; if (original) button.textContent = original; }
      setBusy(false);
    }
  }

  async function refreshCurrentVPN() {
    if (scanBusy || applyBusy || externalBusy) return;
    const update = qs('#updateBtn');
    applyBusy = true;
    setBusy(false);
    if (update) { update.disabled = true; setButtonLabel(update, 'Проверяем обновление…', 'refresh'); }
    setText(qs('#bestServerStatus'), 'Получаем свежий endpoint и сравниваем его с текущим VPN до переключения…');
    try {
      const response = await fetch('/api/vpn/current-refresh', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({confirm:true})});
      const body = await response.json().catch(()=>null);
      if (!response.ok || !body || body.success !== true) {
        const detail = body && (body.primary_error || body.error || body.message);
        setText(qs('#bestServerStatus'), detail || 'Безопасное обновление не завершено. Не повторяйте операцию до проверки состояния.');
        return;
      }
      if (body.current) renderCurrentQuality({scanned_at:new Date().toISOString(), candidates:[Object.assign({}, body.current, {current:true})]});
      if (body.outcome === 'applied') {
        const expected = body.candidate && body.candidate.endpoint;
        const status = expected ? await waitForEndpoint(expected) : null;
        if (!status) {
          setText(qs('#bestServerStatus'), 'Свежий endpoint применён, но live-state ещё не подтверждён в браузере. Не повторяйте операцию.');
          return;
        }
        currentQuality = body.candidate ? Object.assign({}, body.candidate, {current:true}) : null;
        renderOverviewTopbarFromStatus(status);
        if (currentQuality) renderCurrentQuality({scanned_at:new Date().toISOString(), candidates:[currentQuality]});
        clearAlternatives('Текущий endpoint обновлён. Для нового сравнения подберите серверы снова.');
      }
      setText(qs('#bestServerStatus'), body.message || ({no_new:'Нового endpoint нет. Текущий VPN сохранён.',current_better:'Текущий VPN лучше. Переключение не выполнялось.',check_failed:'Свежий endpoint не прошёл проверку. Текущий VPN сохранён.',applied:'Свежий endpoint применён и проверен.'}[body.outcome] || 'Проверка завершена.'));
    } catch (_) {
      setText(qs('#bestServerStatus'), 'Связь прервалась во время безопасного обновления. Проверьте фактическое состояние перед повтором.');
    } finally {
      applyBusy = false;
      setBusy(false);
      if (update) { update.disabled = externalBusy; setButtonLabel(update, 'Обновить и проверить', 'refresh'); }
    }
  }

  function installUpdateQualityFollowup() {
    if(typeof act!=='function'||act.__freenetUpdateQualityFollowup)return;const previous=act;const wrapped=async function(action){if(action==='update')return refreshCurrentVPN();return previous(action);};wrapped.__freenetUpdateQualityFollowup=true;act=wrapped;
  }

  function installBestServerActionDelegation() {
    const root=document.documentElement;if(!root||root.dataset.freenetBestServerActions==='1')return;root.dataset.freenetBestServerActions='1';
    document.addEventListener('freenet:controls-busy',event=>{externalBusy=!!event.detail;setBusy(scanMode);});
    document.addEventListener('click',event=>{const origin=event.target;if(!origin||typeof origin.closest!=='function')return;const button=origin.closest('#bestServerCheckCurrent,#bestServerRefresh,.vpn-option-apply,.vpn-option-retry');if(!button||button.disabled)return;event.preventDefault();if(button.id==='bestServerCheckCurrent'){void scanCurrentVPN();return}if(button.id==='bestServerRefresh'){void scanBestServer();return}if(button.matches('.vpn-option-retry')){const candidate=alternatives.find(item=>item.id===button.dataset.candidateId);if(candidate)void retryCandidate(candidate,button);return}if(button.matches('.vpn-option-apply')){const candidate=alternatives.find(item=>item.eligible&&item.id===button.dataset.candidateId);if(candidate)void applyCandidate(candidate);}},true);
  }

  function start() {
    installRussianProfileFilter();patchCrossPlatformFlags();installBestServerActionDelegation();installUpdateQualityFollowup();mountOverviewTopbar();installOverviewStatusHook();mountBestServerUI();syncOverviewTopbar();
  }

  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',start,{once:true});else start();
})();

// Runtime polish pass: visual-only. Best Server selection/apply semantics above are unchanged.
(() => {
  const q = (selector, root = document) => root.querySelector(selector);
  let scheduled = false;

  const flagSVG = code => {
    const common = 'viewBox="0 0 30 20" width="30" height="20" preserveAspectRatio="none" aria-hidden="true" focusable="false"';
    const maps = {
      pl: '<rect width="30" height="10" fill="#fff"/><rect y="10" width="30" height="10" fill="#dc143c"/>',
      ru: '<rect width="30" height="6.667" fill="#fff"/><rect y="6.667" width="30" height="6.666" fill="#0039a6"/><rect y="13.333" width="30" height="6.667" fill="#d52b1e"/>',
      lt: '<rect width="30" height="6.667" fill="#fdb913"/><rect y="6.667" width="30" height="6.666" fill="#006a44"/><rect y="13.333" width="30" height="6.667" fill="#c1272d"/>',
      de: '<rect width="30" height="6.667" fill="#000"/><rect y="6.667" width="30" height="6.666" fill="#dd0000"/><rect y="13.333" width="30" height="6.667" fill="#ffce00"/>',
      nl: '<rect width="30" height="6.667" fill="#ae1c28"/><rect y="6.667" width="30" height="6.666" fill="#fff"/><rect y="13.333" width="30" height="6.667" fill="#21468b"/>',
      at: '<rect width="30" height="6.667" fill="#ed2939"/><rect y="6.667" width="30" height="6.666" fill="#fff"/><rect y="13.333" width="30" height="6.667" fill="#ed2939"/>',
      bg: '<rect width="30" height="6.667" fill="#fff"/><rect y="6.667" width="30" height="6.666" fill="#00966e"/><rect y="13.333" width="30" height="6.667" fill="#d62612"/>',
      fr: '<rect width="10" height="20" fill="#0055a4"/><rect x="10" width="10" height="20" fill="#fff"/><rect x="20" width="10" height="20" fill="#ef4135"/>',
      it: '<rect width="10" height="20" fill="#009246"/><rect x="10" width="10" height="20" fill="#fff"/><rect x="20" width="10" height="20" fill="#ce2b37"/>',
      dk: '<rect width="30" height="20" fill="#c8102e"/><rect x="9" width="4" height="20" fill="#fff"/><rect y="8" width="30" height="4" fill="#fff"/>',
      fi: '<rect width="30" height="20" fill="#fff"/><rect x="9" width="4" height="20" fill="#003580"/><rect y="8" width="30" height="4" fill="#003580"/>',
      se: '<rect width="30" height="20" fill="#006aa7"/><rect x="9" width="4" height="20" fill="#fecc00"/><rect y="8" width="30" height="4" fill="#fecc00"/>',
      no: '<rect width="30" height="20" fill="#ba0c2f"/><rect x="8" width="6" height="20" fill="#fff"/><rect y="7" width="30" height="6" fill="#fff"/><rect x="10" width="2" height="20" fill="#00205b"/><rect y="9" width="30" height="2" fill="#00205b"/>',
      hr: '<rect width="30" height="6.667" fill="#ff0000"/><rect y="6.667" width="30" height="6.666" fill="#fff"/><rect y="13.333" width="30" height="6.667" fill="#171796"/><path d="M12 6h6v7c0 2-1.3 3.2-3 4-1.7-.8-3-2-3-4z" fill="#fff" stroke="#174e9c" stroke-width=".6"/><rect x="12.4" y="6.4" width="1.3" height="1.3" fill="#e31d2b"/><rect x="15" y="6.4" width="1.3" height="1.3" fill="#e31d2b"/><rect x="13.7" y="7.7" width="1.3" height="1.3" fill="#e31d2b"/><rect x="16.3" y="7.7" width="1.3" height="1.3" fill="#e31d2b"/><rect x="12.4" y="9" width="1.3" height="1.3" fill="#e31d2b"/><rect x="15" y="9" width="1.3" height="1.3" fill="#e31d2b"/>',
      sk: '<rect width="30" height="6.667" fill="#fff"/><rect y="6.667" width="30" height="6.666" fill="#0b4ea2"/><rect y="13.333" width="30" height="6.667" fill="#ee1c25"/><path d="M8 5h7v7.2c0 2.1-1.4 3.4-3.5 4.5C9.4 15.6 8 14.3 8 12.2z" fill="#ee1c25" stroke="#fff" stroke-width=".65"/><path d="M11.5 7v5m-2-3h4m-3.4 2h2.8" stroke="#fff" stroke-width=".7" stroke-linecap="round"/><path d="M9.2 13.2c1.4-.9 3.2-.9 4.6 0" stroke="#0b4ea2" stroke-width="1" fill="none"/>',
      si: '<rect width="30" height="6.667" fill="#fff"/><rect y="6.667" width="30" height="6.666" fill="#0056a4"/><rect y="13.333" width="30" height="6.667" fill="#ed1c24"/><path d="M8.5 4.8h5.4v5.6c0 1.8-1.1 3-2.7 3.8-1.6-.8-2.7-2-2.7-3.8z" fill="#0056a4" stroke="#fff" stroke-width=".5"/>',
      rs: '<rect width="30" height="6.667" fill="#c6363c"/><rect y="6.667" width="30" height="6.666" fill="#0c4076"/><rect y="13.333" width="30" height="6.667" fill="#fff"/><path d="M8.5 5h5.5v7c0 1.7-1.1 2.8-2.7 3.6-1.7-.8-2.8-1.9-2.8-3.6z" fill="#c6363c" stroke="#fff" stroke-width=".5"/>',
      ua: '<rect width="30" height="10" fill="#0057b7"/><rect y="10" width="30" height="10" fill="#ffd700"/>',
      jp: '<rect width="30" height="20" fill="#fff"/><circle cx="15" cy="10" r="5" fill="#bc002d"/>',
      ch: '<rect width="30" height="20" fill="#d52b1e"/><rect x="13" y="5" width="4" height="10" fill="#fff"/><rect x="10" y="8" width="10" height="4" fill="#fff"/>'
    };
    const body = maps[code];
    return body ? `<svg ${common}>${body}</svg>` : '';
  };

  function cleanFlag(node) {
    if (!node || !node.classList || !node.classList.contains('flag-icon')) return;
    const token = Array.from(node.classList).find(name => /^flag-[a-z]{2}$/.test(name));
    if (!token) return;
    const code = token.slice(5);
    const svg = flagSVG(code);
    if (!svg) return;
    if (node.dataset.fnFlagCode === code && node.classList.contains('fn-clean-flag') && node.querySelector('svg')) return;
    node.dataset.fnFlagCode = code;
    node.classList.add('fn-clean-flag');
    node.innerHTML = svg;
  }

  function polishFlags() {
    document.querySelectorAll('.flag-icon').forEach(cleanFlag);
  }

  function polishBestAlternative() {
    const rows = Array.from(document.querySelectorAll('#bestServerResult .vpn-option'));
    rows.forEach(row => row.classList.remove('fn-best-alternative'));
    if (!rows.length || rows.some(row => row.classList.contains('vpn-best'))) return;
    const first = rows.find(row => !row.classList.contains('vpn-rejected') && row.querySelector('.vpn-option-apply'));
    if (!first) return;
    first.classList.add('fn-best-alternative');
    const badge = first.querySelector('.vpn-state-badge');
    if (badge && badge.textContent.trim() === 'Для сравнения') badge.textContent = 'Лучший из вариантов';
  }

  function polishTopbar() {
    q('#fnManualShortcut')?.remove();
    const manual = q('#bestServerAdvanced');
    const topbar = q('.topbar.overview-approved') || q('.topbar');
    const summary = q('#overviewApprovedTop');
    const actions = q('.top-actions');
    if (manual) manual.classList.add('fn-topbar-vpn-picker');
    if (manual && topbar && manual.parentNode !== topbar) topbar.insertBefore(manual, summary || actions || null);
    const exact = q('#exactConnectRow'); if (manual && exact && exact.parentNode !== manual) manual.appendChild(exact);
  }

  function fitCurrentMetrics() {
    document.querySelectorAll('.vpn-current-panel .best-v4-pill b').forEach(node => {
      node.classList.toggle('fn-long-value', node.textContent.trim().length >= 9);
    });
  }

  function run() {
    scheduled = false;
    polishFlags();
    polishBestAlternative();
    polishTopbar();
    fitCurrentMetrics();
  }

  function schedule() {
    if (scheduled) return;
    scheduled = true;
    requestAnimationFrame(run);
  }

  function install() {
    if (q('#FreeNetFinalRuntimePolish')) return;
    const style = document.createElement('style');
    style.id = 'FreeNetFinalRuntimePolish';
    style.textContent = `
      .vpn-current-panel .best-v4-pill{grid-template-columns:20px minmax(0,1fr)!important;column-gap:7px!important;padding-left:9px!important;padding-right:8px!important;overflow:hidden}
      .vpn-current-panel .metric-icon{width:20px!important;height:20px!important}.vpn-current-panel .metric-copy,.vpn-current-panel .metric-value-line{min-width:0;max-width:100%}
      .vpn-current-panel .best-v4-pill b{font-size:18px!important;letter-spacing:-.035em;max-width:100%;white-space:nowrap}.vpn-current-panel .best-v4-pill b.fn-long-value{font-size:16.5px!important;letter-spacing:-.05em}
      .vpn-option.fn-best-alternative{border-color:#22d99a!important;background:linear-gradient(155deg,rgba(11,78,65,.62),rgba(9,31,43,.97))!important;box-shadow:inset 0 0 0 1px rgba(34,217,154,.22),0 0 0 1px rgba(34,217,154,.08),0 0 26px rgba(34,217,154,.07)!important}
      .vpn-option.fn-best-alternative .vpn-state-badge{border-color:#22d99a!important;color:#63edb8!important;background:rgba(13,90,65,.46)!important}.vpn-option.fn-best-alternative .vpn-state-badge:before{content:'★';font-size:12px;color:#63edb8}
      .flag-icon.fn-clean-flag{position:relative!important;display:inline-block!important;box-sizing:border-box!important;padding:0!important;background:none!important;background-image:none!important;overflow:hidden!important;border:1px solid rgba(145,173,207,.32)!important;border-radius:4px!important;line-height:0!important;isolation:isolate}.flag-icon.fn-clean-flag:before,.flag-icon.fn-clean-flag:after{content:none!important;display:none!important;background:none!important}.flag-icon.fn-clean-flag>svg{position:absolute;inset:0;width:100%;height:100%;display:block}
      .top-actions{gap:10px!important}.topbar.overview-approved{overflow:visible!important;gap:14px!important}.topbar.overview-approved .top-status{display:none!important}.overview-approved-top{margin-left:0!important;gap:18px!important}
      #bestServerAdvanced.fn-topbar-vpn-picker{position:relative;display:block!important;flex:1 1 560px;max-width:620px;min-width:390px;margin:0 0 0 auto!important;padding:0!important;border:0!important;background:transparent!important;min-height:0!important;z-index:60}#bestServerAdvanced.fn-topbar-vpn-picker:before,#bestServerAdvanced.fn-topbar-vpn-picker>h3{display:none!important}
      #bestServerAdvanced.fn-topbar-vpn-picker #profilesList.profiles{display:grid!important;grid-template-columns:minmax(170px,.82fr) minmax(220px,1.18fr);gap:8px;align-items:center;width:100%;margin:0!important;min-height:0!important}#bestServerAdvanced.fn-topbar-vpn-picker #profilesList .field{position:relative;padding:0;border:0;background:none;min-height:0!important}#bestServerAdvanced.fn-topbar-vpn-picker #profilesList .field label{display:none!important}#bestServerAdvanced.fn-topbar-vpn-picker #profilesList .profile-combobox{margin:0!important;min-height:0!important;position:relative}#bestServerAdvanced.fn-topbar-vpn-picker #profileSearch,#bestServerAdvanced.fn-topbar-vpn-picker #profilesTrigger{min-height:36px!important;height:36px!important;background:#0b1929;border:1px solid #315276;border-radius:9px;font-size:11px;color:#dce8f7}#bestServerAdvanced.fn-topbar-vpn-picker #profileSearch{padding-left:38px}#bestServerAdvanced.fn-topbar-vpn-picker .manual-search-icon{left:11px;width:16px;height:16px}#bestServerAdvanced.fn-topbar-vpn-picker #profilesMenu{z-index:120}#bestServerAdvanced.fn-topbar-vpn-picker #profilesError{grid-column:1/-1;margin:0;position:absolute;top:42px;left:0;right:0;z-index:122}#bestServerAdvanced.fn-topbar-vpn-picker #selectedProfileCard{position:absolute;top:43px;left:0;right:222px;z-index:121;margin:0;padding:8px 10px;border:1px solid #315276;border-radius:10px;background:#0c1a2b;box-shadow:0 12px 30px rgba(0,0,0,.38);font-size:10px}#bestServerAdvanced.fn-topbar-vpn-picker:has(#exactConnectRow[hidden]) #selectedProfileCard{display:none!important}#bestServerAdvanced.fn-topbar-vpn-picker #exactConnectRow{position:absolute;top:43px;right:0;z-index:122;width:214px;display:grid;grid-template-columns:1fr 1fr;gap:6px;margin:0;padding:7px;border:1px solid #315276;border-radius:10px;background:#0c1a2b;box-shadow:0 12px 30px rgba(0,0,0,.38)}#bestServerAdvanced.fn-topbar-vpn-picker #exactConnectRow[hidden]{display:none!important}#bestServerAdvanced.fn-topbar-vpn-picker #exactConnectRow .btn{min-height:34px;padding:5px 7px;font-size:9px}#bestServerAdvanced.fn-topbar-vpn-picker #quickNetworkGuard{display:none!important}
      @media(max-width:1180px){.vpn-current-panel .best-v4-pill b{font-size:16.5px!important}.vpn-current-panel .best-v4-pill b.fn-long-value{font-size:15px!important}#bestServerAdvanced.fn-topbar-vpn-picker{min-width:320px;max-width:520px}.overview-approved-top{gap:12px!important}.overview-approved-fact span{font-size:9px!important}.overview-approved-fact strong{font-size:11px!important}}
      @media(max-width:900px){.overview-approved-top{display:none!important}#bestServerAdvanced.fn-topbar-vpn-picker{max-width:none;min-width:300px}}
      @media(max-width:820px){.vpn-current-panel .best-v4-pill b,.vpn-current-panel .best-v4-pill b.fn-long-value{font-size:15px!important;letter-spacing:-.035em}.topbar.overview-approved{height:auto!important;min-height:64px;flex-wrap:wrap;padding-bottom:8px!important}#bestServerAdvanced.fn-topbar-vpn-picker{order:20;flex:1 0 100%;max-width:none;min-width:0;margin:0!important}#bestServerAdvanced.fn-topbar-vpn-picker #profilesList.profiles{grid-template-columns:1fr 1.15fr}}
      @media(max-width:560px){#bestServerAdvanced.fn-topbar-vpn-picker #profilesList.profiles{grid-template-columns:1fr}#bestServerAdvanced.fn-topbar-vpn-picker #selectedProfileCard,#bestServerAdvanced.fn-topbar-vpn-picker #exactConnectRow{position:static;width:auto;grid-column:1/-1;margin-top:6px}#bestServerAdvanced.fn-topbar-vpn-picker #profilesError{position:static;grid-column:1/-1}}
      @media(min-width:821px) and (max-height:820px){.vpn-current-panel .best-v4-pill b,.vpn-current-panel .best-v4-pill b.fn-long-value{font-size:13px!important;letter-spacing:-.03em}}
    `;
    document.head.appendChild(style);
    const observer = new MutationObserver(schedule);
    observer.observe(document.documentElement, {subtree:true,childList:true,attributes:true,attributeFilter:['class','hidden'],characterData:true});
    schedule();
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', install, {once:true}); else install();
})();


// Issue #347: approved Overview render contract.
// UI-only correction over the v0.3.5 Best Server engine. Backend quality,
// transactional apply and Operation Coordinator semantics stay unchanged.
(() => {
  const q = (selector, root = document) => root.querySelector(selector);
  let scheduled = false;
  let rememberedEndpoint = '';
  let rememberedFlag = '';

  function flagCode(node) {
    if (!node || !node.classList) return '';
    const token = Array.from(node.classList).find(name => /^flag-[a-z]{2}$/.test(name));
    return token ? token.slice(5) : '';
  }

  function preserveCurrentFlag() {
    const flag = q('#bestCurrentFlag');
    const endpoint = String(q('#bestCurrentEndpoint')?.textContent || '').trim();
    if (!flag || !endpoint || endpoint === '—') return;
    const code = flagCode(flag);
    if (endpoint !== rememberedEndpoint) {
      rememberedEndpoint = endpoint;
      rememberedFlag = code;
      return;
    }
    if (code) {
      rememberedFlag = code;
      return;
    }
    if (!rememberedFlag) return;
    Array.from(flag.classList)
      .filter(name => /^flag-[a-z]{2}$/.test(name) || name === 'flag-unknown')
      .forEach(name => flag.classList.remove(name));
    flag.classList.add(`flag-${rememberedFlag}`);
    flag.hidden = false;
  }

  function svgIcon(kind) {
    const span = document.createElement('span');
    span.className = 'fn-top-fact-icon';
    span.setAttribute('aria-hidden', 'true');
    span.innerHTML = kind === 'provider'
      ? '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><ellipse cx="12" cy="5" rx="7" ry="3"/><path d="M5 5v6c0 1.7 3.1 3 7 3s7-1.3 7-3V5M5 11v6c0 1.7 3.1 3 7 3s7-1.3 7-3v-6"/></svg>'
      : '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c2.4 2.5 3.6 5.5 3.6 9S14.4 18.5 12 21M12 3C9.6 5.5 8.4 8.5 8.4 12S9.6 18.5 12 21"/></svg>';
    return span;
  }

  function polishTopbar() {
    const facts = q('#overviewApprovedTop');
    if (facts) {
      facts.classList.add('fn-render-facts');
      facts.querySelectorAll('.overview-approved-fact').forEach((item, index) => {
        item.classList.add('fn-render-fact');
        if (!item.querySelector('.fn-top-fact-icon')) item.prepend(svgIcon(index === 0 ? 'provider' : 'dns'));
      });
    }
    const xkeen = q('#topXkeenLink');
    if (xkeen) {
      xkeen.classList.add('fn-render-xkeen');
      xkeen.textContent = 'XKeen UI ↗';
    }
    const trigger = q('#profilesTriggerText');
    if (trigger) {
      const value = trigger.textContent.trim();
      if (!value || value === 'Выбрать VPN' || value === 'Загружаем профили…') trigger.textContent = 'Ручной выбор Extra-профиля';
    }
    const search = q('#profileSearch');
    if (search) search.placeholder = 'Страна, город или endpoint';
  }

  function endpointButton(button) {
    if (!button || button.querySelector('.fn-endpoint-copy')) return;
    button.textContent = '';
    button.classList.add('fn-endpoint-refresh');
    const icon = document.createElement('span');
    icon.className = 'fn-endpoint-icon';
    icon.setAttribute('aria-hidden', 'true');
    icon.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9"><path d="M20 7v5h-5M4 17v-5h5M18.5 10A7 7 0 0 0 6.7 6.7L4 9M5.5 14A7 7 0 0 0 17.3 17.3L20 15"/></svg>';
    const copy = document.createElement('span');
    copy.className = 'fn-endpoint-copy';
    copy.innerHTML = '<strong>Обновить endpoint</strong><small>Новый IP в этой же локации</small>';
    button.append(icon, copy);
    button.title = 'Получить новый endpoint этой же VPN-локации; FreeNet сначала сравнит качество и сохранит текущий, если новый хуже';
  }

  function polishCurrentActions() {
    const update = q('#updateBtn');
    const rotate = q('#rotateBtn');
    if (rotate) {
      rotate.hidden = true;
      rotate.setAttribute('aria-hidden', 'true');
      rotate.tabIndex = -1;
    }
    if (update && !update.disabled && !/Проверяем/.test(update.textContent)) endpointButton(update);
    const row = update?.closest('.action-row');
    if (row) row.classList.add('fn-current-actions');
  }

  function polishPrimaryCopy() {
    const refresh = q('#bestServerRefresh');
    if (refresh && !refresh.disabled && refresh.textContent.trim() !== 'Подобрать варианты') {
      const icon = refresh.querySelector('.fn-icon');
      refresh.textContent = '';
      if (icon) refresh.appendChild(icon);
      const label = document.createElement('span');
      label.textContent = 'Подобрать варианты';
      refresh.appendChild(label);
    }
    const empty = q('#bestServerEmpty');
    if (empty && /Подобрать серверы/.test(empty.textContent)) empty.textContent = empty.textContent.replaceAll('Подобрать серверы', 'Подобрать варианты');
    const pageCopy = q('.page[data-page-view="overview"] .page-head p');
    if (pageCopy) pageCopy.textContent = 'Проверка VPN, сравнение серверов и рекомендации';
  }

  function ensureConnectionBadge() {
    const current = q('.best-v4-current');
    if (!current) return;
    let badge = q('#bestCurrentConnection');
    if (!badge) {
      badge = document.createElement('span');
      badge.id = 'bestCurrentConnection';
      badge.className = 'fn-current-connected';
      current.appendChild(badge);
    }
    let online = true;
    try { if (typeof lastStatus !== 'undefined' && lastStatus) online = !!lastStatus.xray_online; } catch (_) {}
    badge.classList.toggle('offline', !online);
    badge.textContent = online ? '● Подключен' : '● Нет соединения';
  }

  function incompleteCard(row) {
    if (!row?.classList.contains('vpn-rejected')) return false;
    const text = row.textContent || '';
    const services = text.match(/Сервисы\s+(\d+)\/(\d+)/i);
    const servicesOK = !!(services && Number(services[2]) > 0 && Number(services[1]) === Number(services[2]));
    const speedIncomplete = /Скорость не измерена|Speedtest не заверш|Speedtest\s+[0-3]\/4/i.test(text);
    return servicesOK && speedIncomplete;
  }

  function polishCandidates() {
    const rows = Array.from(document.querySelectorAll('#bestServerResult .vpn-option'));
    rows.forEach(row => {
      const incomplete = incompleteCard(row);
      row.classList.toggle('fn-incomplete', incomplete);
      const badge = row.querySelector('.vpn-state-badge');
      if (incomplete && badge && badge.textContent.trim() !== 'Проверка не завершена') badge.textContent = 'Проверка не завершена';
      const note = row.querySelector('.vpn-rejected-note');
      if (incomplete && note) note.textContent = 'Проверка не завершена. Текущий VPN не изменён; при необходимости можно проверить этот вариант снова.';
      const apply = row.querySelector('.vpn-option-apply');
      if (apply && !apply.disabled && apply.textContent.trim() !== 'Использовать') {
        apply.textContent = 'Использовать';
        const name = row.querySelector('.vpn-option-title h4')?.textContent?.trim() || 'VPN';
        apply.setAttribute('aria-label', `Использовать: ${name}`);
      }
    });

    const status = q('#bestServerStatus.summary .summary-main');
    if (!status) return;
    const incomplete = rows.filter(row => row.classList.contains('fn-incomplete')).length;
    if (!incomplete) return;
    const hard = rows.filter(row => row.classList.contains('vpn-rejected') && !row.classList.contains('fn-incomplete')).length;
    const best = rows.filter(row => row.classList.contains('vpn-best') || row.classList.contains('fn-best-alternative')).length;
    const comparison = rows.filter(row => row.querySelector('.vpn-option-apply') && !row.classList.contains('vpn-best') && !row.classList.contains('fn-best-alternative')).length;
    const scanned = (status.textContent.match(/Проверено профилей:\s*(\d+)/) || [, '0'])[1];
    const parts = [];
    if (best) parts.push(`${best} лучший`);
    if (comparison) parts.push(`${comparison} для сравнения`);
    if (incomplete) parts.push(`${incomplete} с незавершённой проверкой`);
    if (hard) parts.push(`${hard} не прошёл проверку`);
    status.textContent = `Проверено профилей: ${scanned}. Показано ${rows.length}: ${parts.join(', ')}.`;
  }

  function run() {
    scheduled = false;
    preserveCurrentFlag();
    polishTopbar();
    polishCurrentActions();
    polishPrimaryCopy();
    ensureConnectionBadge();
    polishCandidates();
  }

  function schedule() {
    if (scheduled) return;
    scheduled = true;
    requestAnimationFrame(run);
  }

  function install() {
    if (q('#FreeNetIssue347RenderContract')) return;
    const style = document.createElement('style');
    style.id = 'FreeNetIssue347RenderContract';
    style.textContent = `
      .page[data-page-view="overview"] .page-head{align-items:flex-start!important;margin-bottom:15px!important}.page[data-page-view="overview"] .page-head p{display:block!important;margin:5px 0 0!important;color:#8fa8c7!important;font-size:12px!important}.page[data-page-view="overview"] .page-head h1{font-size:31px!important}
      .topbar.overview-approved{height:72px!important;padding:0 24px!important;gap:16px!important;background:#071321f5!important;border-bottom-color:#29445f!important}.topbar.overview-approved .top-left{flex:0 0 96px}.topbar.overview-approved .top-title{font-size:14px!important}.topbar.overview-approved .top-actions{flex:0 0 auto!important}
      #bestServerAdvanced.fn-topbar-vpn-picker{flex:1 1 610px!important;max-width:720px!important;min-width:410px!important;margin-left:auto!important}#bestServerAdvanced.fn-topbar-vpn-picker #profilesList.profiles{grid-template-columns:minmax(230px,.9fr) minmax(300px,1.2fr)!important;gap:10px!important}#bestServerAdvanced.fn-topbar-vpn-picker #profileSearch,#bestServerAdvanced.fn-topbar-vpn-picker #profilesTrigger{height:42px!important;min-height:42px!important;border-radius:11px!important;background:#0a1b2d!important;border-color:#315678!important;font-size:12px!important}#bestServerAdvanced.fn-topbar-vpn-picker #profileSearch{padding-left:40px!important}
      .overview-approved-top.fn-render-facts{gap:9px!important;display:flex!important}.fn-render-fact{position:relative!important;display:grid!important;grid-template-columns:28px auto!important;grid-template-rows:auto auto!important;column-gap:8px!important;min-width:130px!important;min-height:45px!important;padding:7px 11px!important;border:1px solid #294b70!important;border-radius:11px!important;background:linear-gradient(180deg,#0d2135,#0a1929)!important}.fn-render-fact .fn-top-fact-icon{grid-row:1/3;align-self:center;width:22px;height:22px;color:#5ca2ff}.fn-top-fact-icon svg{width:100%;height:100%;display:block}.fn-render-fact>span:not(.fn-top-fact-icon){font-size:9px!important;line-height:1.05;color:#829ab9!important}.fn-render-fact>strong{font-size:11px!important;line-height:1.15!important;align-self:start}.fn-render-xkeen{height:42px!important;display:inline-flex!important;align-items:center!important;padding:0 13px!important;border-radius:11px!important;border-color:#294b70!important;background:#0d1d30!important;color:#a9bdd5!important}
      #quickActionsSection{padding:22px 22px 20px!important;border-radius:18px!important;background:linear-gradient(150deg,#10253d,#0b1a2c)!important}.best-v4-shell{grid-template-columns:minmax(330px,355px) minmax(0,1fr)!important;gap:0 28px!important}.vpn-current-panel{padding:7px 27px 0 0!important}.best-v4-current{align-items:flex-start!important}.fn-current-connected{margin-left:auto;flex:0 0 auto;display:inline-flex;align-items:center;min-height:27px;padding:4px 9px;border:1px solid rgba(39,218,151,.35);border-radius:999px;background:rgba(15,99,71,.3);color:#7cecc0;font-size:9px;font-weight:800;white-space:nowrap}.fn-current-connected.offline{border-color:#a35b61;background:#48232c;color:#ff9da5}
      .vpn-current-panel>.action-row.fn-current-actions{grid-template-columns:1fr!important}.vpn-current-panel>.action-row.fn-current-actions #rotateBtn{display:none!important}.fn-endpoint-refresh{width:100%!important;justify-content:flex-start!important;text-align:left!important;padding:8px 14px!important}.fn-endpoint-icon{width:21px;height:21px;color:#86b6ff;flex:0 0 21px}.fn-endpoint-icon svg{display:block;width:100%;height:100%}.fn-endpoint-copy{display:grid;gap:2px;min-width:0}.fn-endpoint-copy strong{font-size:12px;color:#f2f7ff}.fn-endpoint-copy small{font-size:9px;font-weight:560;color:#86a0bf}
      .vpn-option.fn-incomplete{border-color:#c88a22!important;background:linear-gradient(155deg,rgba(79,55,20,.72),rgba(25,28,38,.98))!important;box-shadow:inset 0 0 0 1px rgba(218,153,43,.09)!important}.vpn-option.fn-incomplete .vpn-state-badge{border-color:#d39a32!important;color:#ffd27d!important;background:rgba(111,72,15,.38)!important}.vpn-option.fn-incomplete .vpn-detail-chip.bad{border-color:rgba(210,150,46,.55)!important;color:#efc778!important;background:rgba(91,62,19,.3)!important}.vpn-option.fn-incomplete .vpn-rejected-note{color:#c9b58e!important}
      .vpn-best .vpn-option-apply,.vpn-option.fn-best-alternative .vpn-option-apply{background:linear-gradient(180deg,#2ee4a5,#18bd83)!important;border-color:#62efbf!important;color:#052c20!important;font-weight:850!important}.vpn-option:not(.vpn-best):not(.fn-best-alternative) .vpn-option-apply{background:linear-gradient(180deg,#347eff,#2367e7)!important;border-color:#69a0ff!important}
      @media(max-width:1250px){.topbar.overview-approved{gap:10px!important}#bestServerAdvanced.fn-topbar-vpn-picker{min-width:330px!important;max-width:560px!important}.fn-render-fact{min-width:112px!important}.best-v4-shell{grid-template-columns:minmax(300px,320px) minmax(0,1fr)!important;gap:0 20px!important}}
      @media(max-width:1000px){.overview-approved-top.fn-render-facts{display:none!important}#bestServerAdvanced.fn-topbar-vpn-picker{max-width:none!important}.topbar.overview-approved .top-left{flex-basis:78px}}
      @media(max-width:820px){.topbar.overview-approved{height:auto!important;min-height:64px!important;flex-wrap:wrap!important}.best-v4-shell{grid-template-columns:1fr!important}.vpn-current-panel{padding-right:0!important}#bestServerAdvanced.fn-topbar-vpn-picker{order:20!important;flex:1 0 100%!important;width:100%!important;max-width:none!important;min-width:0!important;margin:0!important}}
      @media(max-width:560px){#bestServerAdvanced.fn-topbar-vpn-picker #profilesList.profiles{grid-template-columns:1fr!important;width:100%!important}}
    `;
    document.head.appendChild(style);
    const observer = new MutationObserver(schedule);
    observer.observe(document.documentElement, {subtree:true, childList:true, attributes:true, attributeFilter:['class','hidden','disabled'], characterData:true});
    schedule();
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', install, {once:true}); else install();
})();


// Issue #350: v0.3.7 final render-scale alignment.
// Desktop typography and alignment correction only; responsive/mobile contracts remain intact.
(() => {
  function installV037Scale() {
    if (document.getElementById('FreeNetV037FinalScale')) return;
    const style = document.createElement('style');
    style.id = 'FreeNetV037FinalScale';
    style.textContent = `
      @media (min-width:1181px) and (min-height:821px){
        .content{width:min(1370px,calc(100% - 46px))!important;padding-top:18px!important}
        .page[data-page-view="overview"] .page-head{margin-bottom:17px!important}
        .page[data-page-view="overview"] .page-head h1{font-size:34px!important;line-height:1.08!important}
        .page[data-page-view="overview"] .page-head p{font-size:13px!important;line-height:1.4!important;margin-top:6px!important}
        #quickActionsSection{padding:25px 25px 23px!important;border-radius:20px!important}
        .best-v4-shell{grid-template-columns:minmax(365px,390px) minmax(0,1fr)!important;gap:0 32px!important}
        .vpn-current-panel{padding:9px 31px 0 0!important}
        .best-v4-label{font-size:13px!important}
        .best-v4-name{font-size:25px!important;line-height:1.18!important}
        .best-v4-endpoint{font-size:13px!important;margin:11px 0 18px!important}
        .vpn-current-panel #bestCurrentFlag{width:36px!important;height:25px!important;margin-top:4px!important}
        .vpn-current-panel .best-v4-metrics{gap:11px!important}
        .vpn-current-panel .best-v4-pill{min-height:82px!important;padding:13px 12px!important}
        .vpn-current-panel .best-v4-pill span.metric-label{font-size:12px!important}
        .vpn-current-panel .best-v4-pill b{font-size:21px!important}
        .best-quality{font-size:13px!important;line-height:1.42!important;margin:13px 0 12px!important}
        .vpn-current-panel>#bestServerCheckCurrent{min-height:50px!important;font-size:15px!important}
        .fn-endpoint-refresh{min-height:46px!important;padding:9px 15px!important}
        .fn-endpoint-copy strong{font-size:13px!important}
        .fn-endpoint-copy small{font-size:10px!important}
        .current-health{font-size:13px!important;line-height:1.42!important;padding:12px 13px!important}
        .current-help{font-size:12px!important;line-height:1.5!important}
        .vpn-section-head{margin-bottom:14px!important;align-items:center!important}
        .vpn-section-head h3{font-size:22px!important}
        .vpn-section-head .hint{font-size:13px!important}
        .vpn-section-head #bestServerRefresh{min-height:50px!important;font-size:15px!important;padding:10px 20px!important}
        .best-v4-result.show{gap:13px!important}
        .vpn-option{min-height:153px!important;padding:15px 16px 13px!important}
        .vpn-option-head{margin-bottom:12px!important;align-items:center!important}
        .vpn-option-title h4{font-size:18px!important}
        .vpn-option-title .flag-icon{width:36px!important;height:25px!important}
        .vpn-state-badge{min-height:35px!important;font-size:12px!important;padding:6px 13px!important;align-items:center!important}
        .vpn-option .btn{min-height:41px!important;font-size:13px!important;padding:8px 15px!important}
        .vpn-option .best-v4-pill{min-height:61px!important;padding:5px 13px 5px 9px!important}
        .vpn-option .best-v4-pill span.metric-label{font-size:11px!important}
        .vpn-option .best-v4-pill b{font-size:17px!important}
        .best-v4-reason{font-size:12px!important;gap:7px!important;margin-top:11px!important}
        .vpn-detail-chip{min-height:29px!important;font-size:11px!important;padding:4px 10px!important}
        .vpn-rejected-note{font-size:12px!important;line-height:1.42!important}
        .best-v4-status{font-size:12px!important;line-height:1.42!important;padding:11px 13px!important}
        .overview-approved-top.fn-render-facts{align-items:center!important;gap:10px!important}
        .fn-render-fact{min-width:142px!important;min-height:48px!important;padding:8px 12px!important;align-items:center!important}
        .fn-render-fact>span:not(.fn-top-fact-icon){font-size:10px!important}
        .fn-render-fact>strong{font-size:12px!important;line-height:1.2!important}
        .fn-render-xkeen{height:44px!important;font-size:12px!important}
        #bestServerAdvanced.fn-topbar-vpn-picker #profileSearch,#bestServerAdvanced.fn-topbar-vpn-picker #profilesTrigger{height:44px!important;min-height:44px!important;font-size:13px!important}
      }
    `;
    document.head.appendChild(style);
  }
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', installV037Scale, {once:true}); else installV037Scale();
})();
