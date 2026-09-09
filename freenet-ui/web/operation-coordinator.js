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
    const deadline = Date.now() + 120000;
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
      :root{--fn-blue:#2f73ff;--fn-blue2:#5593ff;--fn-green:#34e2a0;--fn-red:#ff5f6d;--fn-card:#0f1d30;--fn-card2:#12243a;--fn-border:#2d4869;--fn-muted:#a9bad1}
      body{font-size:15px}.content{width:min(1240px,calc(100% - 34px));padding:14px 0 12px}.topbar.overview-approved{height:64px;padding:0 24px;background:rgba(6,14,25,.97);border-bottom:1px solid #223650}.page[data-page-view="overview"] .page-head{margin:0 0 12px;align-items:center}.page[data-page-view="overview"] .page-head h1{font-size:31px}.page[data-page-view="overview"] .page-kicker,.page[data-page-view="overview"] .page-head p{display:none}.page[data-page-view="overview"]>.grid-equal{display:none!important}.content:has(.page.active[data-page-view="overview"])>.footer{display:none!important}
      .overview-hero-source{display:none!important}.overview-compact-grid{grid-template-columns:1fr!important}.overview-approved-top{margin-left:auto;display:flex;align-items:center;gap:28px}.overview-approved-fact{display:grid;gap:1px}.overview-approved-fact span{font-size:11px;color:#84a0c4;font-weight:750}.overview-approved-fact strong{font-size:13px;color:#f4f8ff;white-space:nowrap}.top-status{font-size:12px}.top-status .dot.ok{background:var(--fn-green);box-shadow:0 0 15px rgba(52,226,160,.7)}
      #quickActionsSection{padding:18px 20px 16px;border-radius:20px;border:1px solid #2a4667;background:linear-gradient(150deg,#102036,#0b1728);box-shadow:none;overflow:visible}#quickActionsSection>.card-head{display:none!important}
      .best-v4-shell{display:grid;grid-template-columns:minmax(278px,.74fr) minmax(0,1.75fr);gap:0 22px;align-items:start}.vpn-current-panel{min-width:0;padding:4px 22px 0 0;border-right:1px solid #30435d}.vpn-alternatives-panel{min-width:0;padding-left:0}.best-v4-current{display:flex;align-items:flex-start;gap:10px}.best-v4-current-main{min-width:0}.best-v4-label{color:#a9bad1;font-size:11px;font-weight:800}.best-v4-name{font-size:22px;font-weight:780;line-height:1.18;margin-top:5px;color:#f7f9fd;overflow-wrap:anywhere}.best-v4-endpoint{margin:8px 0 14px;color:#8fa8c5;font:11px ui-monospace,SFMono-Regular,Menlo,Consolas,monospace}.vpn-current-panel #bestCurrentFlag{width:30px;height:21px;margin-top:3px;border-radius:4px}
      .best-v4-metrics{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:9px}.vpn-current-panel .best-v4-metrics{grid-template-columns:repeat(2,minmax(0,1fr));gap:9px}.best-v4-pill{position:relative;min-width:0;padding:9px 10px 9px 34px;border:1px solid #31547b;border-radius:10px;background:#0d1b2c}.best-v4-pill:before{position:absolute;left:10px;top:50%;transform:translateY(-50%);width:17px;height:17px;display:grid;place-items:center;color:#68a0ff;font-size:17px;line-height:1}.best-v4-pill[data-metric="speed"]:before{content:'⇩'}.best-v4-pill[data-metric="http"]:before{content:'◷'}.best-v4-pill[data-metric="tcp"]:before{content:'⌘'}.best-v4-pill[data-metric="jitter"]:before{content:'⌁'}.best-v4-pill span{display:block;color:#a9bad1;font-size:10px;line-height:1.15}.best-v4-pill b{display:block;margin-top:3px;font-size:15px;line-height:1.1;white-space:nowrap}.best-v4-pill.speed b{color:#5ce9ad}.vpn-current-panel .best-v4-pill b{font-size:18px}
      .best-quality{margin:9px 0 10px;color:#9db1ca;font-size:11px;line-height:1.35}.vpn-current-panel>#bestServerCheckCurrent{width:100%;min-height:42px;justify-content:center;background:linear-gradient(180deg,#347dff,#2466e4);border-color:#6497ff;font-size:13px}.vpn-current-panel>.action-row{display:grid;grid-template-columns:1fr 1fr;gap:8px;margin-top:8px}.vpn-current-panel>.action-row[hidden]{display:none}.vpn-current-panel>.action-row .btn{min-height:36px;padding:6px 8px;justify-content:center;text-align:center;font-size:11px;background:#0f2035;border-color:#2d4c70}
      .current-health{margin-top:9px;padding:9px 11px;border:1px solid rgba(52,226,160,.55);border-radius:10px;background:rgba(16,101,77,.22);color:#bdf9df;font-size:11px;line-height:1.35;display:flex;gap:8px;align-items:flex-start}.current-health:before{content:'✓';flex:0 0 20px;width:20px;height:20px;border-radius:50%;display:grid;place-items:center;background:#31dfa0;color:#052416;font-weight:900}.current-health.neutral{border-color:#38506e;background:#0c1a2b;color:#a9bad1}.current-health.neutral:before{content:'i';background:#4e78a9;color:#e9f3ff}.current-help{margin:9px 0 0;color:#93a8c2;font-size:10px;line-height:1.45}
      .vpn-section-head{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:9px}.vpn-section-head h3{margin:0;font-size:18px}.vpn-section-head .hint{margin-top:3px;font-size:11px;color:#9eb0c8}.vpn-section-head #bestServerRefresh{min-height:42px;padding:8px 18px;border-radius:11px;background:linear-gradient(180deg,#347dff,#2466e4);border-color:#6497ff;font-size:13px}.vpn-empty{padding:34px 20px;border:1px dashed #38506d;border-radius:12px;color:#a8b7ca;font-size:13px;line-height:1.5}
      .best-v4-result,.best-v4-result.show{padding:0;border:0;background:none}.best-v4-result.show{display:grid;gap:8px}.vpn-option{padding:9px 11px;border:1px solid #304a69;background:linear-gradient(155deg,#12263e,#0e1d31);border-radius:12px}.vpn-option.vpn-best{border-color:#19cf89;background:linear-gradient(155deg,rgba(16,75,64,.72),rgba(9,31,42,.95));box-shadow:inset 0 0 0 1px rgba(25,207,137,.12)}.vpn-option.vpn-rejected{border-color:#d84454;background:linear-gradient(155deg,rgba(66,28,40,.72),rgba(19,27,42,.96))}.vpn-option-head{display:flex;align-items:center;justify-content:space-between;gap:10px;margin-bottom:6px}.vpn-option-title{display:flex;align-items:center;gap:8px;min-width:0}.vpn-option-title .flag-icon{width:28px;height:19px}.vpn-option-title h4{font-size:14px;line-height:1.25;margin:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.vpn-option-actions{display:flex;align-items:center;gap:8px;flex-shrink:0}.vpn-state-badge{display:inline-flex;align-items:center;min-height:25px;padding:4px 9px;border:1px solid #405874;border-radius:999px;color:#b8c8db;background:#122236;font-size:10px;font-weight:800;white-space:nowrap}.vpn-best .vpn-state-badge{border-color:#20d694;color:#5beab1;background:rgba(13,79,60,.42)}.vpn-rejected .vpn-state-badge{border-color:#dd4d5d;color:#ff7885;background:rgba(97,29,42,.4)}.vpn-option .btn{min-height:30px;padding:5px 11px;border-radius:9px;font-size:11px;justify-content:center}.vpn-best .vpn-option-apply{background:linear-gradient(180deg,#347dff,#2466e4);border-color:#6497ff}.vpn-option-retry{background:#17263a;border-color:#466280}.vpn-option .best-v4-metrics{margin:0;gap:7px}.vpn-option .best-v4-pill{padding:0 8px 0 0;border:0;border-right:1px solid #31445e;border-radius:0;background:none}.vpn-option .best-v4-pill:last-child{border-right:0}.vpn-option .best-v4-pill:before{display:none}.vpn-option .best-v4-pill span{font-size:9px}.vpn-option .best-v4-pill b{font-size:14px}.best-v4-reason{display:flex;flex-wrap:wrap;gap:4px 5px;margin-top:6px;font-size:10px;line-height:1.2}.vpn-detail-chip{display:inline-flex;align-items:center;min-height:19px;padding:2px 6px;border:1px solid #35516e;border-radius:7px;background:#0c1a2b;color:#b3c4d8;white-space:nowrap}.vpn-detail-chip.ok{border-color:rgba(52,226,160,.38);color:#9aebc5;background:rgba(12,75,56,.22)}.vpn-detail-chip.bad{border-color:rgba(255,95,109,.5);color:#ff9ba4;background:rgba(102,27,40,.25)}
      .best-v4-status{margin-top:9px;padding:8px 10px;border:1px solid #315b8b;border-radius:10px;background:#0d2a48;color:#b9d4f4;font-size:10px;line-height:1.35;min-height:0}.best-v4-status:empty{display:none}.best-v4-status.busy{color:#cfe2fa}.vpn-measure-note{display:none}
      #bestServerAdvanced{grid-column:1/-1;margin-top:13px;padding-top:11px;border-top:1px solid #2b415d;display:grid;grid-template-columns:1fr;gap:7px}#bestServerAdvanced h3{margin:0;font-size:16px}#bestServerAdvanced:before{content:'ПОИСК ПО СТРАНЕ ИЛИ ГОРОДУ';font-size:9px;font-weight:850;letter-spacing:.08em;color:#9eb0c8;order:1}#bestServerAdvanced h3{order:0}#bestServerAdvanced #profilesList.profiles{order:2;display:grid;grid-template-columns:minmax(210px,.75fr) minmax(320px,1.45fr);gap:9px;align-items:end;margin-top:0!important}#profilesList .field{padding:0;border:0;background:none}#profilesList .field label{display:none}#profilesList .profile-combobox{margin-top:0!important}#profilesList input,#profilesTrigger{min-height:38px;background:#0c1929;border-color:#315074;border-radius:9px;font-size:11px}#profilesList #selectedProfileCard{grid-column:1/-1;margin:0;padding:7px 9px;border-color:#2f4a69;background:#0c1929;font-size:10px}#bestServerAdvanced .action-row{order:3;display:flex;gap:8px;margin:0}#bestServerAdvanced .action-row[hidden]{display:none}#bestServerAdvanced .btn{min-height:36px;padding:6px 10px;font-size:11px}#bestServerAdvanced:has(#exactConnectRow[hidden]) #selectedProfileCard{display:none}#quickNetworkGuard{display:none}
      .flag-dk{background:#c8102e}.flag-dk:before{content:'';position:absolute;left:31%;top:0;bottom:0;width:10%;background:#fff}.flag-dk:after{content:'';position:absolute;left:0;right:0;top:43%;height:14%;background:#fff}.flag-hr{background:linear-gradient(to bottom,#ff0000 0 33.33%,#fff 33.33% 66.66%,#171796 66.66%)}.flag-hr:after{content:'';position:absolute;left:42%;top:30%;width:17%;height:38%;background:repeating-conic-gradient(#e5232e 0 25%,#fff 0 50%) 0/5px 5px;border:1px solid #1d4d9b}.flag-sk{background:linear-gradient(to bottom,#fff 0 33.33%,#0b4ea2 33.33% 66.66%,#ee1c25 66.66%)}.flag-za{background:linear-gradient(to bottom,#de3831 0 43%,#fff 43% 57%,#002395 57%)}.flag-za:before{content:'';position:absolute;inset:0;background:#007749;clip-path:polygon(0 18%,48% 50%,0 82%,0 64%,28% 50%,0 36%)}.flag-si{background:linear-gradient(to bottom,#fff 0 33.33%,#0056a4 33.33% 66.66%,#ed1c24 66.66%)}.flag-rs{background:linear-gradient(to bottom,#c6363c 0 33.33%,#0c4076 33.33% 66.66%,#fff 66.66%)}.flag-is{background:#02529c}.flag-is:before{content:'';position:absolute;left:31%;top:0;bottom:0;width:14%;background:#fff}.flag-is:after{content:'';position:absolute;left:0;right:0;top:41%;height:18%;background:#fff}.flag-lu{background:linear-gradient(to bottom,#ed2939 0 33.33%,#fff 33.33% 66.66%,#00a1de 66.66%)}
      @media(max-width:1120px){.best-v4-shell{grid-template-columns:minmax(250px,.72fr) minmax(0,1.55fr);gap:0 14px}.vpn-current-panel{padding-right:14px}.vpn-option-title h4{font-size:13px}.vpn-state-badge{display:none}.overview-approved-top{gap:16px}}
      @media(max-width:760px){.content{width:calc(100% - 20px);padding-top:10px}.topbar.overview-approved{padding:0 14px}.overview-approved-top{display:none}.best-v4-shell{grid-template-columns:1fr;gap:12px}.vpn-current-panel{border-right:0;border-bottom:1px solid #30435d;padding:0 0 14px}.vpn-current-panel .best-v4-metrics,.vpn-option .best-v4-metrics{grid-template-columns:repeat(2,minmax(0,1fr));gap:8px}.vpn-option .best-v4-pill{border-right:0}.vpn-section-head{align-items:flex-start}.vpn-section-head #bestServerRefresh{min-height:38px;padding:7px 10px}.vpn-option-head{align-items:flex-start}.vpn-option-actions{flex-direction:column;align-items:flex-end}.vpn-state-badge{display:inline-flex}.vpn-option-title h4{white-space:normal}.vpn-detail-chip{white-space:normal}#bestServerAdvanced #profilesList.profiles{grid-template-columns:1fr}.best-v4-name{font-size:20px}}
      @media(min-width:761px) and (max-height:820px){.content{padding-top:9px}.page[data-page-view="overview"] .page-head{margin-bottom:7px}.page[data-page-view="overview"] .page-head h1{font-size:26px}#quickActionsSection{padding:12px 15px 10px}.best-v4-shell{gap:0 16px}.vpn-current-panel{padding-right:15px}.best-v4-name{font-size:18px}.best-v4-endpoint{margin:5px 0 9px}.vpn-current-panel .best-v4-metrics{gap:6px}.best-v4-pill{padding-top:6px;padding-bottom:6px}.vpn-current-panel .best-v4-pill b{font-size:15px}.best-quality{margin:6px 0}.vpn-current-panel>#bestServerCheckCurrent{min-height:35px}.vpn-current-panel>.action-row{margin-top:6px}.vpn-current-panel>.action-row .btn{min-height:31px}.current-health{margin-top:6px;padding:6px 8px}.current-help{margin-top:5px;line-height:1.3}.vpn-section-head{margin-bottom:6px}.vpn-section-head h3{font-size:16px}.vpn-section-head #bestServerRefresh{min-height:35px}.best-v4-result.show{gap:5px}.vpn-option{padding:6px 8px}.vpn-option-head{margin-bottom:3px}.vpn-option .btn{min-height:25px;padding:3px 8px}.vpn-option .best-v4-pill b{font-size:12px}.best-v4-reason{margin-top:3px;gap:2px 4px}.vpn-detail-chip{min-height:16px;padding:1px 4px;font-size:9px}.best-v4-status{margin-top:5px;padding:5px 7px}#bestServerAdvanced{margin-top:7px;padding-top:6px;gap:4px}#bestServerAdvanced h3{font-size:14px}#profilesList input,#profilesTrigger{min-height:31px}#bestServerAdvanced .btn{min-height:30px}}
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

  function metricPill(label, value, key, trustedSpeed) {
    const node = document.createElement('div');
    node.className = 'best-v4-pill' + (key === 'speed' && trustedSpeed ? ' speed' : '');
    node.dataset.metric = key;
    const name = document.createElement('span'); name.textContent = label;
    const number = document.createElement('b'); number.textContent = value;
    node.append(name, number);
    return node;
  }

  function renderMetrics(root, candidate) {
    if (!root) return;
    root.textContent = '';
    root.appendChild(metricPill('Скорость VPN', metric(candidate, 'speed'), 'speed', candidate?.eligible === true));
    root.appendChild(metricPill('Отклик сайтов', metric(candidate, 'http'), 'http', false));
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
      box.textContent = 'Текущий VPN работает стабильно. Скорость и отклик подтверждены.';
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
    chip.textContent = String(text).replace(/[.]$/, '');
    root.appendChild(chip);
  }

  function clearAlternatives(message) {
    alternatives = [];
    recommendation = null;
    const box = qs('#bestServerResult');
    if (box) { box.textContent = ''; box.classList.remove('show'); }
    const empty = qs('#bestServerEmpty');
    if (empty) { empty.hidden = false; empty.textContent = message || 'Подберите серверы, чтобы увидеть проверенные варианты.'; }
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
    if (!candidate.eligible) return {kind: 'rejected', label: 'Не прошёл проверку'};
    if (data && data.recommendation && !data.recommendation.current && sameCandidate(data.recommendation, candidate)) {
      return {kind: 'best', label: 'Лучший вариант'};
    }
    return {kind: 'comparison', label: 'Для сравнения'};
  }

  function renderBestResult(data) {
    clearAlternatives();
    const current = currentCandidate(data);
    if (current) renderCurrentQuality({scanned_at: data.scanned_at, candidates: [current]});
    alternatives = comparisonCandidates(data, current);
    const box = qs('#bestServerResult');
    if (!alternatives.length) {
      clearAlternatives('Проверенных зарубежных вариантов для сравнения сейчас нет. Текущий VPN сохранён.');
      setText(qs('#bestServerStatus'), data && data.message || 'Сравнение сейчас недоступно.');
      return;
    }
    const empty = qs('#bestServerEmpty'); if (empty) empty.hidden = true;
    box.classList.add('show');
    alternatives.forEach((candidate, index) => {
      const state = stateForCandidate(data, candidate);
      const row = document.createElement('article');
      row.className = `vpn-option vpn-${state.kind}`;
      const head = document.createElement('div'); head.className = 'vpn-option-head';
      const title = document.createElement('div'); title.className = 'vpn-option-title';
      title.appendChild(makeRenderedFlag(profileCode(candidate)));
      const name = document.createElement('h4'); name.textContent = profileDisplayName(candidate, 'VPN');
      if (index === 0) name.id = 'bestServerName';
      title.appendChild(name); head.appendChild(title);
      const actions = document.createElement('div'); actions.className = 'vpn-option-actions';
      const badge = document.createElement('span'); badge.className = 'vpn-state-badge'; badge.textContent = state.label; actions.appendChild(badge);
      if (candidate.eligible) {
        const button = document.createElement('button'); button.type = 'button'; button.className = 'btn secondary vpn-option-apply'; button.dataset.candidateId = candidate.id;
        if (index === 0) button.id = 'bestServerApply';
        button.textContent = state.kind === 'best' ? 'Переключиться' : 'Использовать';
        button.setAttribute('aria-label', `${button.textContent}: ${name.textContent}`);
        actions.appendChild(button);
      } else {
        const retry = document.createElement('button'); retry.type = 'button'; retry.className = 'btn secondary vpn-option-retry'; retry.textContent = 'Проверить снова'; actions.appendChild(retry);
      }
      head.appendChild(actions);
      const metrics = document.createElement('div'); metrics.className = 'best-v4-metrics'; renderMetrics(metrics, candidate);
      const reason = document.createElement('div'); reason.className = 'best-v4-reason'; if (index === 0) reason.id = 'bestServerReason';
      if (candidate.eligible) {
        recommendationReason(data, candidate).forEach(text => appendDetailChip(reason, text, text.includes('быстрее') || text.includes('+') ? 'ok' : ''));
        appendDetailChip(reason, `Speedtest ${candidate.media_samples || 0}/4`, 'ok');
        appendDetailChip(reason, `Сервисы ${candidate.service_ok || 0}/${candidate.service_total || 0}`, 'ok');
        appendDetailChip(reason, 'Доступен', 'ok');
      } else {
        const diagnostics = Array.isArray(candidate.rejections) && candidate.rejections.length ? candidate.rejections.slice(0, 3) : ['Недостаточно подтверждённых данных'];
        diagnostics.forEach(text => appendDetailChip(reason, text, 'bad'));
        appendDetailChip(reason, `Speedtest ${candidate.media_samples || 0}/4`, 'bad');
        appendDetailChip(reason, `Сервисы ${candidate.service_ok || 0}/${candidate.service_total || 0}`, candidate.service_ok === candidate.service_total && candidate.service_total > 0 ? 'ok' : 'bad');
      }
      row.append(head, metrics, reason); box.appendChild(row);
    });
    const switchable = alternatives.filter(candidate => candidate.eligible).length;
    const currentPreferred = data.recommendation && data.recommendation.current ? ' Текущий VPN остаётся предпочтительным.' : '';
    const rejected = alternatives.filter(candidate => !candidate.eligible).length;
    const statusText = `Проверено профилей: ${data.profiles_scanned || 0}. Показано вариантов: ${alternatives.length}; подходит: ${switchable}${rejected ? `; не прошли проверку: ${rejected}` : ''}.${currentPreferred}`;
    setText(qs('#bestServerStatus'), statusText);
  }

  function mountBestServerUI() {
    const quick = qs('#quickActionsSection');
    if (!quick) return null;
    const countries = quick.querySelector('.quick-layout'); if (countries) countries.remove();
    const alternativesTitle = qs('.vpn-section-head h3'); if (alternativesTitle) alternativesTitle.textContent = 'Результаты проверки';
    const alternativesHint = qs('.vpn-section-head .hint'); if (alternativesHint) alternativesHint.textContent = 'Топ-3 варианта на основе реальных измерений';
    const profilesList = qs('#profilesList');
    const profileLabel = profilesList && profilesList.querySelector('label[for="profileSearch"]'); if (profileLabel) profileLabel.textContent = 'Поиск по стране или городу';
    const update = qs('#updateBtn'); if (update) { update.textContent = 'Обновить и проверить'; update.title = 'Получить свежий endpoint текущего профиля и автоматически проверить качество'; update.classList.remove('primary'); update.classList.add('secondary'); }
    const rotate = qs('#rotateBtn'); if (rotate) { rotate.textContent = 'Сменить сервер'; rotate.classList.remove('primary'); rotate.classList.add('secondary'); }
    const currentPanel = qs('.vpn-current-panel');
    const routine = update && update.closest('.action-row');
    if (routine && currentPanel && routine.parentNode !== currentPanel) currentPanel.appendChild(routine);
    const currentCheck = qs('#bestServerCheckCurrent'); if (currentCheck) { currentCheck.classList.remove('secondary'); currentCheck.classList.add('primary'); }
    if (currentPanel && !qs('#bestCurrentHealth')) {
      const health = document.createElement('div'); health.id = 'bestCurrentHealth'; health.className = 'current-health neutral'; health.textContent = 'Проверка качества ещё не выполнялась.';
      currentPanel.appendChild(health);
      const help = document.createElement('p'); help.id = 'bestCurrentHelp'; help.className = 'current-help'; help.textContent = 'FreeNet сравнивает реальный отклик через каждый VPN и глубоко проверяет лучшие варианты. Скорость — короткий тест загрузки, не скорость тарифа.';
      currentPanel.appendChild(help);
    }
    const measureNote = qs('.vpn-measure-note'); if (measureNote) measureNote.remove();
    const status = qs('#bestServerStatus'), alternativesPanel = qs('.vpn-alternatives-panel');
    if (status && alternativesPanel && status.parentNode !== alternativesPanel) alternativesPanel.appendChild(status);
    const guard = qs('#quickNetworkGuard');
    if (profilesList && !qs('#bestServerAdvanced')) {
      const manual = document.createElement('section'); manual.id = 'bestServerAdvanced';
      const title = document.createElement('h3'); title.textContent = 'Выбор сервера вручную'; manual.appendChild(title);
      profilesList.parentNode.insertBefore(manual, profilesList); manual.appendChild(profilesList);
      const exact = qs('#exactConnectRow'); if (exact) manual.appendChild(exact);
      if (guard) manual.appendChild(guard);
    }
    const manual = qs('#bestServerAdvanced');
    const exact = qs('#exactConnectRow'); if (manual && exact && exact.parentNode !== manual) manual.appendChild(exact);
    renderMetrics(qs('#bestCurrentMetrics'), currentQuality); renderCurrentHealth(currentQuality);
    try { if (typeof lastStatus !== 'undefined' && lastStatus) renderCurrentIdentity(lastStatus); } catch (_) {}
    return qs('#bestServerShell');
  }

  function setBusy(mode) {
    scanMode = mode;
    scanBusy = !!mode;
    const current = qs('#bestServerCheckCurrent'), best = qs('#bestServerRefresh');
    if (current) { current.disabled = scanBusy || applyBusy || externalBusy; current.textContent = mode === 'current' ? 'Проверяем текущий…' : 'Проверить текущий VPN'; }
    if (best) { best.disabled = scanBusy || applyBusy || externalBusy; best.textContent = mode === 'best' ? 'Подбираем…' : 'Подобрать серверы'; }
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

  function installUpdateQualityFollowup() {
    if(typeof act!=='function'||act.__freenetUpdateQualityFollowup)return;const previous=act;const wrapped=async function(action){await previous(action);if(action!=='update')return;try{if(typeof loadStatus==='function')await loadStatus();if(lastStatus&&!lastStatus.busy&&!lastStatus.updater_busy&&lastStatus.xray_online){await wait(250);await scanCurrentVPN();}}catch(_){}};wrapped.__freenetUpdateQualityFollowup=true;act=wrapped;
  }

  function installBestServerActionDelegation() {
    const root=document.documentElement;if(!root||root.dataset.freenetBestServerActions==='1')return;root.dataset.freenetBestServerActions='1';
    document.addEventListener('freenet:controls-busy',event=>{externalBusy=!!event.detail;setBusy(scanMode);});
    document.addEventListener('click',event=>{const origin=event.target;if(!origin||typeof origin.closest!=='function')return;const button=origin.closest('#bestServerCheckCurrent,#bestServerRefresh,.vpn-option-apply,.vpn-option-retry');if(!button||button.disabled)return;event.preventDefault();if(button.id==='bestServerCheckCurrent'){void scanCurrentVPN();return}if(button.id==='bestServerRefresh'||button.matches('.vpn-option-retry')){void scanBestServer();return}if(button.matches('.vpn-option-apply')){const candidate=alternatives.find(item=>item.eligible&&item.id===button.dataset.candidateId);if(candidate)void applyCandidate(candidate);}},true);
  }

  function start() {
    installRussianProfileFilter();patchCrossPlatformFlags();installBestServerActionDelegation();installUpdateQualityFollowup();mountOverviewTopbar();installOverviewStatusHook();mountBestServerUI();syncOverviewTopbar();
  }

  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',start,{once:true});else start();
})();
