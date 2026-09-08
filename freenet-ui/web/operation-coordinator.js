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
  function isRussianProfile(profile) {
    const code = String(profile && profile.country_code || '').trim().toLowerCase();
    const name = String(profile && profile.name || '').trim().toLowerCase();
    return code === 'ru' || name.includes('russia') || name.includes('росси');
  }

  function installRussianProfileFilter() {
    if (typeof renderExtraProfiles !== 'function' || renderExtraProfiles.__freenetForeignOnly) return;
    const previous = renderExtraProfiles;
    const wrapped = function(plan) {
      if (plan && Array.isArray(plan.extra_profiles)) {
        plan = Object.assign({}, plan, {extra_profiles: plan.extra_profiles.filter(profile => !isRussianProfile(profile))});
      }
      return previous(plan);
    };
    wrapped.__freenetForeignOnly = true;
    renderExtraProfiles = wrapped;
    try {
      if (Array.isArray(extraProfiles)) {
        extraProfiles = extraProfiles.filter(profile => !isRussianProfile(profile));
        if (typeof renderProfileOptions === 'function') renderProfileOptions();
      }
    } catch (_) {}
  }

  function injectStyle() {
    if (qs('#FreeNetOverviewV4')) return;
    const style = document.createElement('style');
    style.id = 'FreeNetOverviewV4';
    style.textContent = `
      :root{--muted:#b5c1d2;--line:#2c405c}
      body{font-size:15px}.content{width:min(1180px,calc(100% - 42px));padding-top:28px}.page-head{margin-bottom:18px}.page-head h1{font-size:30px}.page-head p{font-size:14px;line-height:1.5;color:#b7c3d4}
      .card{border-color:#2a3d57;padding:20px;border-radius:18px;background:linear-gradient(165deg,rgba(17,30,48,.98),rgba(9,18,31,.98))}.card h2{font-size:18px}.hint{font-size:13px;line-height:1.5;color:#aebbd0}.btn{font-size:14px;min-height:44px}
      .topbar.overview-v4-topbar{height:82px;padding:0 26px;display:grid;grid-template-columns:auto minmax(520px,1fr) auto;gap:16px;align-items:center;background:rgba(7,15,26,.95)}
      .overview-v4-top{justify-self:center;width:min(820px,100%);display:grid;grid-template-columns:minmax(250px,1.5fr) minmax(130px,.65fr) minmax(150px,.8fr);gap:8px;min-width:0}
      .overview-v4-chip{min-width:0;padding:9px 12px;border:1px solid #304560;border-radius:12px;background:#0e1b2c}.overview-v4-chip-label{display:block;color:#8099b9;font-size:10px;font-weight:850;letter-spacing:.08em;text-transform:uppercase;margin-bottom:3px}.overview-v4-chip-value{display:flex;align-items:center;gap:8px;font-size:14px;font-weight:800}.overview-v4-chip-value strong{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.overview-v4-endpoint{display:block;margin-top:2px;color:#91a3ba;font-size:11px;font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
      .overview-v4-health{padding:9px 11px!important;font-size:12px!important}.overview-hero-source{display:none!important}.overview-compact-grid{grid-template-columns:minmax(0,1fr)!important}.page[data-page-view="overview"] > .grid-equal{display:none!important}
      #quickActionsSection{overflow:visible}#quickActionsSection .card-head{margin-bottom:14px}.best-v4-shell{display:grid;gap:12px}.best-v4-current{display:flex;align-items:center;justify-content:space-between;gap:18px;padding:14px 16px;border:1px solid #2b415d;border-radius:14px;background:#0a1727}.best-v4-current-main{min-width:0}.best-v4-label{color:#8099ba;font-size:10px;font-weight:850;letter-spacing:.09em;text-transform:uppercase}.best-v4-name{margin-top:4px;font-size:17px;font-weight:830;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.best-v4-endpoint{margin-top:3px;color:#91a4bb;font-size:11px;font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace}.best-v4-metrics{display:flex;align-items:center;justify-content:flex-end;gap:6px;flex-wrap:wrap}.best-v4-pill{min-width:82px;padding:8px 9px;border:1px solid #263a54;border-radius:10px;background:#0d1c2d}.best-v4-pill span{display:block;color:#8195ae;font-size:9px;text-transform:uppercase;letter-spacing:.05em}.best-v4-pill b{display:block;margin-top:3px;font-size:13px;white-space:nowrap}.best-v4-pill.speed b{color:#8cebb6}
      .best-v4-actions{display:grid;grid-template-columns:1fr 1fr;gap:9px}.best-v4-actions .btn{justify-content:center;text-align:center}.best-v4-actions #bestServerRefresh{background:linear-gradient(180deg,#315fba,#264b92);border-color:#4d73bb}.best-v4-apply{display:none!important}.best-v4-apply.show{display:flex!important}
      .best-v4-result{display:none;padding:14px 16px;border:1px solid #3b5e91;border-radius:14px;background:linear-gradient(160deg,rgba(26,52,91,.68),rgba(10,24,41,.9))}.best-v4-result.show{display:block}.best-v4-result-head{display:flex;align-items:flex-start;justify-content:space-between;gap:12px}.best-v4-result h3{margin:3px 0 0;font-size:18px}.best-v4-reason{margin-top:8px;color:#b9c7d9;font-size:13px;line-height:1.5}.best-v4-result .best-v4-metrics{justify-content:flex-start;margin-top:10px}.best-v4-status{color:#8ea2bc;font-size:12px;line-height:1.45}.best-v4-status.busy{color:#c8d8ee}
      #profilesList{margin-top:12px}#bestServerAdvanced{margin-top:10px;border-top:1px solid #23364e;padding-top:10px}#bestServerAdvanced summary{cursor:pointer;color:#9fb0c6;font-size:12px}
      @media(max-width:1050px){.topbar.overview-v4-topbar{grid-template-columns:auto minmax(360px,1fr) auto}.overview-v4-top{grid-template-columns:1fr 1fr}.overview-v4-chip:first-child{grid-column:1/-1}.overview-v4-endpoint{display:none}.best-v4-current{align-items:flex-start;flex-direction:column}.best-v4-metrics{justify-content:flex-start}}
      @media(max-width:680px){.overview-v4-top{display:none}.topbar.overview-v4-topbar{grid-template-columns:auto 1fr}.top-actions{justify-self:end}.content{width:calc(100% - 20px)}.best-v4-actions{grid-template-columns:1fr}.best-v4-pill{min-width:72px}}
      .topbar.overview-v4-topbar{height:76px;grid-template-columns:auto minmax(0,1fr) auto}
      .overview-v4-top{display:flex;justify-self:end;justify-content:flex-end;gap:24px;width:auto}
      .overview-v4-chip{padding:0;border:0;background:none}.overview-v4-chip-label{font-size:11px;letter-spacing:0;text-transform:none}.overview-v4-chip-value{font-size:13px}
      #quickActionsSection{min-height:0;background:#101c2c;padding:26px;border-radius:20px;box-shadow:none}
      #quickActionsSection .card-head{margin-bottom:24px}#quickActionsSection .card-head .hint{margin-top:5px}
      #quickActionState{display:none}.best-v4-shell{gap:20px}.best-v4-current{background:none;border:0;padding:0;align-items:center;justify-content:flex-start;gap:16px}
      #bestCurrentFlag{width:44px;height:30px;border-radius:6px;flex-shrink:0}.best-v4-label{font-size:12px;letter-spacing:0;text-transform:none;color:#aebbd0}.best-v4-name{font-size:26px;font-weight:700;white-space:normal;overflow-wrap:anywhere;line-height:1.25;margin-top:5px}
      .best-quality{color:#aebbd0;font-size:14px;line-height:1.5;margin-top:7px}.best-v4-actions{display:flex;flex-wrap:wrap;gap:12px}.best-v4-actions .btn{padding:12px 18px;min-height:46px;border-radius:10px}
      .best-v4-actions #bestServerRefresh{background:#3267d6;border-color:#5083ea}.best-v4-actions .btn:focus-visible,#bestServerAdvanced summary:focus-visible,.best-details summary:focus-visible{outline:2px solid #9ec2ff;outline-offset:4px}
      .best-v4-status{min-height:22px;font-size:14px;line-height:1.6;color:#b9c7d9}.best-v4-status:empty{display:none}.best-v4-status.busy:before{content:'';display:inline-block;width:8px;height:8px;border-radius:50%;background:#7aa8ff;margin-right:9px}
      .best-details{border-top:1px solid #29384d;padding-top:14px}.best-details summary,#bestServerAdvanced summary{font-size:13px;color:#aebbd0;cursor:pointer;padding:4px 0}.best-details[open] summary{margin-bottom:12px}.best-details p{font-size:12px;line-height:1.5;color:#aebbd0;margin:12px 0 0}
      .best-v4-metrics{justify-content:flex-start;gap:24px}.best-v4-pill{padding:0;border:0;background:none;min-width:80px}.best-v4-pill span{font-size:11px;letter-spacing:0;text-transform:none;color:#aebbd0}.best-v4-pill b{font-size:17px;font-weight:600}.best-v4-endpoint{overflow-wrap:anywhere}
      .best-v4-result{border:0;border-left:3px solid #6b9aff;border-radius:0 12px 12px 0;background:#15263e;padding:20px}.best-v4-result h3{font-size:21px}.best-v4-result .best-details{margin-top:14px}.best-v4-result-head{display:block}
      #bestServerAdvanced{margin-top:18px;padding-top:14px}#bestServerApply.show{width:fit-content;padding:12px 20px}
      @media(max-width:1050px){.topbar.overview-v4-topbar{grid-template-columns:auto minmax(0,1fr) auto}.overview-v4-top{display:flex}.best-v4-current{flex-direction:row}.overview-v4-chip:first-child{grid-column:auto}}
      @media(max-width:760px){.overview-v4-top{display:none}.topbar.overview-v4-topbar{grid-template-columns:auto 1fr;padding:0 16px}.content{width:calc(100% - 28px)}#quickActionsSection{padding:20px}.best-v4-name{font-size:23px}.best-v4-actions{display:grid;grid-template-columns:1fr}.best-v4-actions .btn{width:100%}.best-v4-metrics{gap:18px}.best-v4-result{padding:16px}}

      /* Overview: visible comparison and manual controls, with no nested disclosures. */
      .page[data-page-view="overview"] .page-head{margin-bottom:14px;align-items:center}
      .page[data-page-view="overview"] .page-kicker,.page[data-page-view="overview"] .page-head p{display:none}
      .content{padding-top:18px}.topbar.overview-v4-topbar{height:64px}
      #quickActionsSection{padding:20px}#quickActionsSection>.card-head{display:none}
      .best-v4-shell{grid-template-columns:minmax(240px,.8fr) minmax(0,1.7fr);gap:16px 24px}
      .vpn-current-panel{min-width:0;padding-right:22px;border-right:1px solid #29384d}
      .vpn-current-panel .best-v4-current{gap:10px;align-items:flex-start}
      #bestCurrentFlag{width:28px;height:20px;margin-top:3px}
      .best-v4-name{font-size:20px;line-height:1.35;margin-top:5px}
      .vpn-current-panel>.best-v4-endpoint{margin:10px 0 20px}
      .best-v4-metrics{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:10px}
      .best-v4-pill{min-width:0}.best-v4-pill span{font-size:11px;line-height:1.25}.best-v4-pill b{font-size:15px}
      .vpn-current-panel .best-v4-metrics{grid-template-columns:repeat(2,minmax(0,1fr));gap:18px 12px}
      .vpn-current-panel .best-v4-pill b{font-size:19px}.best-quality{font-size:12px;margin:16px 0 10px}
      .vpn-current-panel>.btn{width:100%;justify-content:center}
      .vpn-alternatives-panel{min-width:0}.vpn-section-head{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:14px}
      .vpn-section-head h3,#bestServerAdvanced h3{margin:0 0 4px;font-size:16px}.vpn-section-head .hint{font-size:12px}
      .vpn-empty{padding:38px 24px;border:1px dashed #34465e;border-radius:12px;color:#aebbd0;font-size:14px;line-height:1.6}
      .best-v4-result,.best-v4-result.show{padding:0;border:0;background:none;border-radius:0}
      .best-v4-result.show{display:grid;gap:10px}.vpn-option{padding:12px 14px;border:1px solid #2d425e;background:#122238;border-radius:12px}
      .vpn-option-head{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:10px}
      .vpn-option-head h4{font-size:14px;line-height:1.3;margin:0;overflow-wrap:anywhere}
      .vpn-option .btn{min-height:34px;padding:7px 12px;font-size:12px;flex-shrink:0}
      .vpn-option .best-v4-metrics{margin:0}.best-v4-reason{font-size:11px;line-height:1.4;margin-top:8px}
      .best-v4-status{grid-column:1/-1;font-size:13px;min-height:0;line-height:1.4}
      .vpn-measure-note{grid-column:1/-1;margin:0;font-size:11px;color:#91a4bb;line-height:1.4}
      #bestServerAdvanced{margin-top:16px;padding-top:14px;display:grid;grid-template-columns:minmax(0,1fr) auto;gap:8px 16px}
      #bestServerAdvanced h3{grid-column:1/-1}#bestServerAdvanced #profilesList.profiles{min-height:0;margin-top:0!important;display:grid;grid-template-columns:minmax(140px,.7fr) minmax(190px,1fr);gap:10px;align-items:end}
      #profilesList .profile-combobox{margin-top:0!important}#profilesList .field{padding:0;border:0;background:none}#profilesList .field label{font-size:11px;margin-bottom:4px}
      #profilesList input,#profilesTrigger{min-height:40px}#profilesList #selectedProfileCard{min-height:0;grid-column:1/-1;margin-top:0;padding:0;border:0;background:none;font-size:11px}
      #profilesList .selected-profile strong,#profilesList .selected-endpoint{display:inline;margin-right:8px}#profilesList .selected-note{margin-top:3px}
      #bestServerAdvanced .action-row{align-self:center;margin:0;display:flex;gap:8px}#bestServerAdvanced .action-row[hidden]{display:none}
      #bestServerAdvanced .btn{font-size:12px;padding:8px 12px;min-height:40px}#quickNetworkGuard{display:none}
      #bestServerAdvanced[inert]{opacity:.55}.footer{margin-top:10px}
      @media(max-width:1150px){.best-v4-shell{grid-template-columns:minmax(210px,.8fr) minmax(0,1.5fr);gap:14px}.vpn-current-panel{padding-right:14px}#bestServerAdvanced{grid-template-columns:1fr}#bestServerAdvanced .action-row{justify-content:flex-end}}
      @media(max-width:760px){.best-v4-shell{grid-template-columns:1fr}.vpn-current-panel{border-right:0;border-bottom:1px solid #29384d;padding:0 0 16px}.vpn-current-panel .best-v4-metrics{grid-template-columns:repeat(4,minmax(0,1fr));gap:8px}.vpn-current-panel .best-v4-pill b{font-size:14px}.vpn-current-panel>.best-v4-endpoint{margin-bottom:12px}.vpn-section-head{align-items:flex-start}.vpn-section-head .btn{padding:9px;font-size:12px;min-height:40px}.best-v4-pill b{font-size:13px}.best-v4-pill span{font-size:10px}.vpn-option{padding:12px}.vpn-option-head{gap:8px}.vpn-option-head h4{font-size:13px}#profilesList{grid-template-columns:1fr}.best-v4-name{font-size:20px}#bestServerAdvanced .action-row{flex-wrap:wrap}.vpn-empty{padding:24px 16px}}
    `;
    document.head.appendChild(style);
  }

  function dnsLabel(s) { return s && s.dns_mode === 'xkeen' ? 'XKeen/Xray DNS' : 'DNS напрямую'; }
  function healthState(s) {
    if (!s) return {healthy: false, label: 'Проверяем состояние'};
    if (!s.xray_online) return {healthy: false, label: 'VPN не работает'};
    if (s.dns_mode === 'xkeen' && !s.dns_out_present) return {healthy: false, label: 'DNS требует внимания'};
    return {healthy: true, label: s.dns_mode === 'xkeen' ? 'VPN + DNS OK' : 'VPN OK · DNS напрямую'};
  }
  function flagClass(code) {
    const value = String(code || '').trim().toLowerCase();
    return /^[a-z]{2}$/.test(value) ? `flag-${value}` : 'flag-unknown';
  }

  function renderOverviewTopbarFromStatus(s) {
    if (!s) return;
    setText(qs('#topISPValue'), s.isp_label || s.isp || 'Не определён');
    setText(qs('#topDNSValue'), dnsLabel(s));
    const state = healthState(s);
    const dot = qs('#topDot');
    if (dot) dot.className = 'dot ' + (state.healthy ? 'ok' : 'bad');
    setText(qs('#topStatus'), state.label);
    renderCurrentIdentity(s);
  }

  function makeChip(label, id, extra) {
    const chip = document.createElement('div');
    chip.className = 'overview-v4-chip';
    const l = document.createElement('span');
    l.className = 'overview-v4-chip-label';
    l.textContent = label;
    const row = document.createElement('div');
    row.className = 'overview-v4-chip-value';
    if (extra) row.appendChild(extra);
    const value = document.createElement('strong');
    value.id = id;
    value.textContent = 'Определяем…';
    row.appendChild(value);
    chip.appendChild(l);
    chip.appendChild(row);
    return chip;
  }

  function syncOverviewTopbar() {
    try {
      if (typeof lastStatus !== 'undefined' && lastStatus) {
        renderOverviewTopbarFromStatus(lastStatus);
        return;
      }
    } catch (_) {}
    const selectors = ['#profile', '#endpoint', '#overviewISP', '#overviewDNS'];
    const nodes = selectors.map(selector => qs(selector)).filter(Boolean);
    if (!nodes.length) return;
    if (qs('#topISPValue') && qs('#overviewISP')) setText(qs('#topISPValue'), qs('#overviewISP').textContent);
    if (qs('#topDNSValue') && qs('#overviewDNS')) setText(qs('#topDNSValue'), qs('#overviewDNS').textContent);
  }

  function installOverviewTopbarStatusHook() {
    if (typeof updateStatusViews !== 'function' || updateStatusViews.__freenetOverviewTopbarHook) return;
    const previous = updateStatusViews;
    const wrapped = function(s) {
      const result = previous.apply(this, arguments);
      queueMicrotask(() => renderOverviewTopbarFromStatus(s));
      return result;
    };
    wrapped.__freenetOverviewTopbarHook = true;
    updateStatusViews = wrapped;
  }

  function mountOverviewTopbar() {
    injectStyle();
    const topbar = qs('.topbar');
    const topActions = qs('.top-actions');
    const overview = qs('.page[data-page-view="overview"]');
    const grid = overview && overview.querySelector('.grid-2');
    const hero = overview && overview.querySelector('.hero');
    if (!topbar || !topActions || !grid || !hero) return;
    topbar.classList.add('overview-v4-topbar');
    grid.classList.add('overview-compact-grid');
    hero.classList.add('overview-hero-source');
    const status = qs('.top-status');
    if (status) status.classList.add('overview-v4-health');
    const old = qs('#topVpnSummary');
    if (old) old.remove();
    const summary = document.createElement('div');
    summary.id = 'topVpnSummary';
    summary.className = 'overview-v4-top';
    summary.appendChild(makeChip('Провайдер', 'topISPValue'));
    summary.appendChild(makeChip('DNS', 'topDNSValue'));
    topbar.insertBefore(summary, topActions);
    const watched = ['#profile', '#endpoint', '#overviewISP', '#overviewDNS'].map(selector => qs(selector)).filter(Boolean);
    if (watched.length) {
      const observer = new MutationObserver(syncOverviewTopbar);
      watched.forEach(node => observer.observe(node, {subtree: true, childList: true, characterData: true, attributes: true}));
    }
    syncOverviewTopbar();
    if (!topbarSyncTimer) topbarSyncTimer = setInterval(syncOverviewTopbar, 2500);
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

  function metricPill(label, value, speed) {
    const node = document.createElement('div');
    node.className = 'best-v4-pill' + (speed ? ' speed' : '');
    const l = document.createElement('span'); l.textContent = label;
    const v = document.createElement('b'); v.textContent = value;
    node.appendChild(l); node.appendChild(v);
    return node;
  }

  function renderMetrics(root, candidate) {
    if (!root) return;
    root.textContent = '';
    root.appendChild(metricPill('Тест загрузки', metric(candidate, 'speed'), true));
    root.appendChild(metricPill('HTTP-отклик', metric(candidate, 'http')));
    root.appendChild(metricPill('TCP', metric(candidate, 'tcp')));
    root.appendChild(metricPill('Колебание задержки', metric(candidate, 'jitter')));
  }
  function candidateName(candidate, fallback) { return candidate && candidate.name ? candidate.name : fallback; }
  function currentCandidate(data) {
    return Array.isArray(data && data.candidates) ? data.candidates.find(item => item && item.current) || null : null;
  }

  function renderCurrentIdentity(s) {
    const name = qs('#bestCurrentName');
    const endpoint = qs('#bestCurrentEndpoint');
    if (!name || !s) return;
    const label = s.profile_label || (s.country ? `${s.country}${s.city ? ' · ' + s.city : ''}` : 'Профиль не определён');
    setText(name, label);
    setText(endpoint, s.endpoint || '—');
    const flag = qs('#bestCurrentFlag');
    if (flag) {
      flag.className = `flag-icon ${flagClass(s.country_code)}`;
      flag.hidden = !s.country_code;
    }
    if (!applyBusy && currentQuality && currentQuality.endpoint !== s.endpoint) {
      currentQuality = null;
      clearAlternatives('Текущий сервер изменился. Подберите варианты заново.');
      renderMetrics(qs('#bestCurrentMetrics'), null);
      setText(qs('#bestCurrentQuality'), 'Качество ещё не проверено');
      qs('#bestServerResult')?.classList.remove('show');
      qs('#bestServerApply')?.classList.remove('show');
    }
  }

  function renderCurrentQuality(data) {
    const candidate = currentCandidate(data);
    currentQuality = candidate;
    if (candidate) {
      setText(qs('#bestCurrentName'), candidateName(candidate, 'Текущий VPN'));
      setText(qs('#bestCurrentEndpoint'), candidate.endpoint || '—');
    }
    renderMetrics(qs('#bestCurrentMetrics'), candidate);
    const measured = candidate && Number(candidate.download_mbps) > 0;
    const stamp = new Date(data && data.scanned_at || Date.now());
    const time = Number.isNaN(stamp.getTime()) ? '' : stamp.toLocaleTimeString('ru-RU', {hour: '2-digit', minute: '2-digit'});
    setText(qs('#bestCurrentQuality'), candidate ? `${measured ? 'Тест загрузки: ' + metric(candidate, 'speed') : 'Скорость не измерена'}${time ? ' · ' + time : ''}` : 'Недостаточно данных для оценки');
    setText(qs('#bestServerStatus'), candidate ? 'Проверка текущего VPN завершена.' : 'Текущий профиль не удалось определить. Другие серверы не проверялись.');
  }

  function recommendationReason(data, best) {
    if (!best) return 'Недостаточно подтверждённых данных для безопасной рекомендации.';
    const current = currentCandidate(data);
    if (best.current) return 'Текущий VPN уже показывает лучший подтверждённый результат среди проверенных зарубежных профилей.';
    const parts = [];
    const bs = Number(best.download_mbps || 0), cs = Number(current && current.download_mbps || 0);
    const bh = Number(best.application_rtt_ms || 0), ch = Number(current && current.application_rtt_ms || 0);
    if (bs > 0 && cs > 0 && bs > cs) parts.push(`скорость выше примерно на ${(bs - cs).toFixed(1)} Мбит/с`);
    if (bh > 0 && ch > 0 && bh < ch) parts.push(`HTTP-отклик быстрее на ${ch - bh} мс`);
    return parts.length ? `Почему рекомендуем: ${parts.join(', ')}.` : 'Скорость и работа VPN подтверждены проверкой.';
  }

  function clearAlternatives(message) {
    alternatives = [];
    recommendation = null;
    const box = qs('#bestServerResult');
    if (box) { box.textContent = ''; box.classList.remove('show'); }
    const empty = qs('#bestServerEmpty');
    if (empty) { empty.hidden = false; empty.textContent = message || 'Подберите серверы, чтобы увидеть проверенные варианты.'; }
  }

  function renderBestResult(data) {
    clearAlternatives();
    const current = currentCandidate(data);
    if (current) renderCurrentQuality({scanned_at: data.scanned_at, candidates: [current]});
    // Candidates arrive in the engine's deterministic quality order. Use the
    // same evidence threshold as its recommendation, and distinct endpoints.
    const seen = new Set([data && data.current_endpoint, current && current.endpoint].filter(Boolean));
    alternatives = (Array.isArray(data && data.candidates) ? data.candidates : []).filter(candidate => {
      if (!candidate || candidate.current || isRussianProfile(candidate) || !candidate.id || !candidate.endpoint ||
          !candidate.available || !(candidate.download_mbps > 0) || !(candidate.media_samples >= 4) || seen.has(candidate.endpoint)) return false;
      seen.add(candidate.endpoint);
      return true;
    }).slice(0, 3);
    const box = qs('#bestServerResult');
    if (!alternatives.length) {
      clearAlternatives('Подходящих замен с подтверждённой скоростью не найдено. Текущий VPN сохранён.');
      setText(qs('#bestServerStatus'), data && data.message || 'Достоверные варианты для замены недоступны.');
      return;
    }
    qs('#bestServerEmpty').hidden = true;
    box.classList.add('show');
    alternatives.forEach((candidate, index) => {
      const row = document.createElement('article'); row.className = 'vpn-option';
      const head = document.createElement('div'); head.className = 'vpn-option-head';
      const name = document.createElement('h4'); name.textContent = candidateName(candidate, 'VPN');
      if (index === 0) name.id = 'bestServerName';
      const button = document.createElement('button'); button.type = 'button';
      button.className = 'btn secondary vpn-option-apply'; button.dataset.candidateId = candidate.id;
      if (index === 0) button.id = 'bestServerApply';
      button.textContent = 'Переключиться'; button.setAttribute('aria-label', 'Переключиться: ' + name.textContent);
      head.append(name, button);
      const metrics = document.createElement('div'); metrics.className = 'best-v4-metrics'; renderMetrics(metrics, candidate);
      const reason = document.createElement('div'); reason.className = 'best-v4-reason';
      if (index === 0) reason.id = 'bestServerReason';
      reason.textContent = recommendationReason(data, candidate);
      row.append(head, metrics, reason); box.appendChild(row);
    });
    setText(qs('#bestServerStatus'), `Проверено профилей: ${data.profiles_scanned || 0}. Вариантов для замены: ${alternatives.length}.${data.recommendation && data.recommendation.current ? ' Текущий VPN имеет лучший общий результат.' : ''}`);
  }

  function mountBestServerUI() {
    const quick = qs('#quickActionsSection');
    if (!quick) return null;
    const title = quick.querySelector('.card-head h2');
    const hint = quick.querySelector('.card-head .hint');
    if (title) title.textContent = 'VPN';
    if (hint) hint.textContent = 'Соединение и выбор сервера';
    const countries = quick.querySelector('.quick-layout');
    if (countries) countries.remove();
    const profilesList = qs('#profilesList');
    const profileLabel = profilesList && profilesList.querySelector('label[for="profileSearch"]');
    if (profileLabel) profileLabel.textContent = 'Поиск по стране или городу';

    const routine = qs('#updateBtn') && qs('#updateBtn').closest('.action-row');
    const guard = qs('#quickNetworkGuard');
    if (profilesList && !qs('#bestServerAdvanced')) {
      const details = document.createElement('section');
      details.id = 'bestServerAdvanced';
      const summary = document.createElement('h3');
      summary.textContent = 'Выбор сервера вручную';
      details.appendChild(summary);
      profilesList.parentNode.insertBefore(details, profilesList);
      details.appendChild(profilesList);
      const exact = qs('#exactConnectRow');
      if (exact) details.appendChild(exact);
      if (routine) details.appendChild(routine);
      if (guard) details.appendChild(guard);
    }
    try { if (typeof lastStatus !== 'undefined' && lastStatus) renderCurrentIdentity(lastStatus); } catch (_) {}
    renderMetrics(qs('#bestCurrentMetrics'), currentQuality);
    return qs('#bestServerShell');
  }

  function setBusy(mode) {
    scanMode = mode;
    scanBusy = !!mode;
    const current = qs('#bestServerCheckCurrent');
    const best = qs('#bestServerRefresh');
    const apply = qs('#bestServerApply');
    if (current) { current.disabled = scanBusy || applyBusy || externalBusy; current.textContent = mode === 'current' ? 'Проверяем текущий…' : 'Проверить текущий VPN'; }
    if (best) { best.disabled = scanBusy || applyBusy || externalBusy; best.textContent = mode === 'best' ? 'Подбираем…' : 'Подобрать серверы'; }
    document.querySelectorAll('.vpn-option-apply').forEach(button => { button.disabled = scanBusy || applyBusy || externalBusy || !alternatives.some(candidate => candidate.id === button.dataset.candidateId); });
    const manual = qs('#bestServerAdvanced');
    if (manual) manual.inert = scanBusy || applyBusy || externalBusy;
    const status = qs('#bestServerStatus');
    if (status) status.classList.toggle('busy', !!mode || applyBusy);
    qs('#bestServerShell')?.setAttribute('aria-busy', String(scanBusy || applyBusy));
  }

  async function scanCurrentVPN() {
    if (scanBusy || applyBusy || externalBusy) return;
    try {
      mountBestServerUI();
      setBusy('current');
      setText(qs('#bestServerStatus'), 'Проверяем текущий VPN… Это может занять до минуты.');
      const response = await fetch('/api/vpn/current-quality', {cache: 'no-store', signal: AbortSignal.timeout(60000)});
      const body = await response.json().catch(() => null);
      if (!response.ok || !body || !body.success) {
        setText(qs('#bestServerStatus'), (body && body.error) || 'Не удалось проверить текущий VPN.');
        return;
      }
      renderCurrentQuality(body);
    } catch (_) {
      setText(qs('#bestServerStatus'), 'Не удалось завершить проверку текущего VPN. Проверьте связь с FreeNet.');
    } finally {
      setBusy(false);
    }
  }

  async function scanBestServer() {
    if (scanBusy || applyBusy || externalBusy) return;
    try {
      mountBestServerUI();
      setBusy('best');
      clearAlternatives('Подбираем варианты и измеряем качество…');
      const apply = qs('#bestServerApply');
      if (apply) apply.classList.remove('show');
      qs('#bestServerResult')?.classList.remove('show');
      setText(qs('#bestServerStatus'), 'Сравниваем зарубежные серверы… Текущий VPN продолжает работать.');
      const response = await fetch('/api/vpn/best-foreign', {cache: 'no-store', signal: AbortSignal.timeout(180000)});
      const body = await response.json().catch(() => null);
      if (!response.ok || !body || !body.success) {
        renderBestResult(null);
        setText(qs('#bestServerStatus'), (body && body.error) || 'Поиск лучшего VPN не завершён.');
        return;
      }
      renderBestResult(body);
    } catch (_) {
      renderBestResult(null);
      setText(qs('#bestServerStatus'), 'Не удалось завершить поиск. Проверьте связь с FreeNet.');
    } finally {
      setBusy(false);
    }
  }

  async function waitForEndpoint(expected) {
    for (let i = 0; i < 35; i++) {
      try {
        const response = await fetch('/api/status', {cache: 'no-store'});
        if (response.ok) {
          const status = await response.json();
          if (status && !status.busy && !status.updater_busy && status.xray_online && status.endpoint === expected) return status;
        }
      } catch (_) {}
      await wait(850);
    }
    return null;
  }

  async function applyBestServer() {
    if (applyBusy || scanBusy || externalBusy || !recommendation || recommendation.current || !recommendation.id || isRussianProfile(recommendation)) return;
    applyBusy = true;
    setBusy(false);
    const apply = Array.from(document.querySelectorAll('.vpn-option-apply')).find(button => button.dataset.candidateId === recommendation.id);
    if (apply) { apply.disabled = true; apply.textContent = 'Переключаем…'; }
    setText(qs('#bestServerStatus'), 'Переключаем VPN и проверяем соединение…');
    const expectedEndpoint = recommendation.endpoint;
    try {
      const response = await fetch('/api/network-profile/apply', {
        method: 'POST', headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({operation: 'provider', profile_id: recommendation.id, confirm: true})
      });
      const body = await response.json().catch(() => null);
      if (!response.ok || !body || !body.success) {
        const detail = body && (body.primary_error || body.error);
        setText(qs('#bestServerStatus'), detail || 'Результат переключения не подтверждён. Не повторяйте операцию.');
        if (!body || body.result_unknown) {
          recommendation = null;
          if (apply) apply.classList.remove('show');
        }
        return;
      }
      const status = await waitForEndpoint(expectedEndpoint);
      if (!status) {
        setText(qs('#bestServerStatus'), 'Переключение ещё не подтверждено. Проверьте состояние системы перед повторной попыткой.');
        return;
      }
      recommendation = null;
      currentQuality = null;
      renderOverviewTopbarFromStatus(status);
      if (qs('#bestServerResult')) qs('#bestServerResult').classList.remove('show');
      if (apply) apply.classList.remove('show');
      renderMetrics(qs('#bestCurrentMetrics'), null);
      setText(qs('#bestCurrentQuality'), 'Качество ещё не проверено');
      setText(qs('#bestServerStatus'), 'VPN переключён. Соединение проверено.');
    } catch (_) {
      recommendation = null;
      if (apply) apply.classList.remove('show');
      setText(qs('#bestServerStatus'), 'Связь прервалась во время переключения. Результат не подтверждён — проверьте состояние системы перед повторной попыткой.');
    } finally {
      clearAlternatives('Результаты сброшены после переключения. Для нового сравнения подберите серверы снова.');
      applyBusy = false;
      setBusy(false);
    }
  }

  function installBestServerActionDelegation() {
    const root = document.documentElement;
    if (!root || root.dataset.freenetBestServerActions === '1') return;
    root.dataset.freenetBestServerActions = '1';
    document.addEventListener('freenet:controls-busy', event => {
      externalBusy = !!event.detail;
      setBusy(scanMode);
    });
    document.addEventListener('click', event => {
      const origin = event.target;
      if (!origin || typeof origin.closest !== 'function') return;
      const button = origin.closest('#bestServerCheckCurrent, #bestServerRefresh, #bestServerApply, .vpn-option-apply');
      if (!button || button.disabled) return;
      event.preventDefault();
      if (button.id === 'bestServerCheckCurrent') { void scanCurrentVPN(); return; }
      if (button.id === 'bestServerRefresh') { void scanBestServer(); return; }
      if (button.matches('.vpn-option-apply')) {
        if (scanBusy || applyBusy || externalBusy) return;
        recommendation = alternatives.find(candidate => candidate.id === button.dataset.candidateId) || null;
        void applyBestServer();
      }
    }, true);
  }

  function start() {
    installRussianProfileFilter();
    installBestServerActionDelegation();
    mountOverviewTopbar();
    installOverviewTopbarStatusHook();
    syncOverviewTopbar();
    mountBestServerUI();
    // Intentionally no Best Server scan here. Heavy VPN quality probes are user-triggered only.
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', start, {once: true});
  else start();
})();
