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

  async function readOperationState() {
    try {
      const response = await previousFetch('/api/operation/state', {cache: 'no-store'});
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
    for (let i = 0; i < 40; i++) {
      const state = await readOperationState();
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
      if (response.status !== 409 && response.status !== 423) return response;
      let conflict = null;
      try { conflict = await response.clone().json(); } catch (_) {}
      const current = conflict && conflict.current_operation;
      if (current && matchingFreshOperation(current, meta)) {
        const reconciled = await reconcile(meta);
        if (reconciled) return reconciled;
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
  let currentQuality = null;
  let scanBusy = false;
  let applyBusy = false;
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
    const name = s.country ? `${s.country}${s.city ? ' · ' + s.city : ''}` : 'VPN не определён';
    setText(qs('#topVpnProfile'), name);
    setText(qs('#topVpnEndpoint'), s.endpoint || '—');
    const flag = qs('#topVpnFlag');
    if (flag) flag.className = `flag-icon ${flagClass(s.country_code)}`;
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
    if (qs('#topVpnProfile') && qs('#profile')) setText(qs('#topVpnProfile'), qs('#profile').textContent);
    if (qs('#topVpnEndpoint') && qs('#endpoint')) setText(qs('#topVpnEndpoint'), qs('#endpoint').textContent);
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
    const flag = document.createElement('span');
    flag.id = 'topVpnFlag';
    flag.className = 'flag-icon flag-unknown';
    const vpn = makeChip('Текущий VPN', 'topVpnProfile', flag);
    const endpoint = document.createElement('span');
    endpoint.id = 'topVpnEndpoint';
    endpoint.className = 'overview-v4-endpoint';
    endpoint.textContent = '—';
    vpn.appendChild(endpoint);
    summary.appendChild(vpn);
    summary.appendChild(makeChip('ISP', 'topISPValue'));
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
    root.appendChild(metricPill('Скорость', metric(candidate, 'speed'), true));
    root.appendChild(metricPill('HTTP', metric(candidate, 'http')));
    root.appendChild(metricPill('TCP', metric(candidate, 'tcp')));
    root.appendChild(metricPill('Jitter', metric(candidate, 'jitter')));
  }
  function candidateName(candidate, fallback) { return candidate && candidate.name ? candidate.name : fallback; }
  function currentCandidate(data) {
    return Array.isArray(data && data.candidates) ? data.candidates.find(item => item && item.current) || data.candidates[0] || null : null;
  }

  function renderCurrentIdentity(s) {
    const name = qs('#bestCurrentName');
    const endpoint = qs('#bestCurrentEndpoint');
    if (!name || !s) return;
    setText(name, s.country ? `${s.country}${s.city ? ' · ' + s.city : ''}` : 'VPN не определён');
    setText(endpoint, s.endpoint || '—');
  }

  function renderCurrentQuality(data) {
    const candidate = currentCandidate(data);
    currentQuality = candidate;
    if (candidate) {
      setText(qs('#bestCurrentName'), candidateName(candidate, 'Текущий VPN'));
      setText(qs('#bestCurrentEndpoint'), candidate.endpoint || '—');
    }
    renderMetrics(qs('#bestCurrentMetrics'), candidate);
    setText(qs('#bestServerStatus'), data && data.message ? data.message : 'Текущий VPN проверен.');
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
    return parts.length ? `Почему рекомендуем: ${parts.join(', ')}.` : 'Почему рекомендуем: лучший подтверждённый баланс скорости, отклика и стабильности.';
  }

  function renderBestResult(data) {
    recommendation = data && data.available && data.recommendation && !isRussianProfile(data.recommendation) ? data.recommendation : null;
    const current = currentCandidate(data);
    if (current) renderCurrentQuality({message: 'Текущий VPN измерен тем же методом.', candidates: [current]});
    const box = qs('#bestServerResult');
    const apply = qs('#bestServerApply');
    if (!recommendation) {
      if (box) box.classList.remove('show');
      if (apply) apply.classList.remove('show');
      setText(qs('#bestServerStatus'), data && data.message ? data.message : 'Достоверная рекомендация недоступна.');
      return;
    }
    if (box) box.classList.add('show');
    setText(qs('#bestServerName'), recommendation.current ? 'Текущий VPN уже лучший' : candidateName(recommendation, 'Лучший VPN'));
    setText(qs('#bestServerEndpoint'), recommendation.endpoint || '—');
    setText(qs('#bestServerReason'), recommendationReason(data, recommendation));
    renderMetrics(qs('#bestServerMetrics'), recommendation);
    setText(qs('#bestServerStatus'), `${data.message || 'Рекомендация готова.'} Проверено зарубежных профилей: ${data.profiles_scanned || 0}. MUTATION NONE.`);
    if (apply) {
      apply.disabled = recommendation.current || scanBusy || applyBusy;
      apply.textContent = recommendation.current ? 'Лучший уже используется' : 'Переключиться на лучший';
      apply.classList.toggle('show', !recommendation.current);
    }
  }

  function mountBestServerUI() {
    const quick = qs('#quickActionsSection');
    if (!quick) return null;
    const title = quick.querySelector('.card-head h2');
    const hint = quick.querySelector('.card-head .hint');
    if (title) title.textContent = 'VPN';
    if (hint) hint.textContent = 'Ничего не проверяется автоматически. Текущий VPN и поиск лучшего запускаются отдельно.';
    const countries = quick.querySelector('.quick-layout');
    if (countries) countries.remove();
    const profilesList = qs('#profilesList');
    const profileLabel = profilesList && profilesList.querySelector('label[for="profileSearch"]');
    if (profileLabel) profileLabel.textContent = 'Ручной выбор Extra-профиля';

    if (!qs('#bestServerShell')) {
      const shell = document.createElement('div');
      shell.id = 'bestServerShell';
      shell.className = 'best-v4-shell';
      shell.innerHTML = `
        <div class="best-v4-current">
          <div class="best-v4-current-main"><div class="best-v4-label">Текущий VPN</div><div id="bestCurrentName" class="best-v4-name">Определяем…</div><div id="bestCurrentEndpoint" class="best-v4-endpoint">—</div></div>
          <div id="bestCurrentMetrics" class="best-v4-metrics"></div>
        </div>
        <div class="best-v4-actions">
          <button id="bestServerCheckCurrent" type="button" class="btn secondary">Проверить текущий VPN</button>
          <button id="bestServerRefresh" type="button" class="btn secondary">Найти лучший VPN</button>
        </div>
        <div id="bestServerResult" class="best-v4-result">
          <div class="best-v4-result-head"><div><div class="best-v4-label">Рекомендация FreeNet · только зарубежные серверы</div><h3 id="bestServerName">Лучший VPN</h3><div id="bestServerEndpoint" class="best-v4-endpoint">—</div></div></div>
          <div id="bestServerReason" class="best-v4-reason">Почему рекомендуем</div>
          <div id="bestServerMetrics" class="best-v4-metrics"></div>
        </div>
        <button id="bestServerApply" type="button" class="btn primary best-v4-apply" disabled>Переключиться на лучший</button>
        <div id="bestServerStatus" class="best-v4-status">Готово. Проверки запускаются только по вашему действию. MUTATION NONE.</div>`;
      const anchor = profilesList || quick.querySelector('.action-row');
      if (anchor) quick.insertBefore(shell, anchor); else quick.appendChild(shell);
      qs('#bestServerCheckCurrent').addEventListener('click', scanCurrentVPN);
      qs('#bestServerRefresh').addEventListener('click', scanBestServer);
      qs('#bestServerApply').addEventListener('click', applyBestServer);
    }

    const routine = qs('#updateBtn') && qs('#updateBtn').closest('.action-row');
    const guard = qs('#quickNetworkGuard');
    if (profilesList && !qs('#bestServerAdvanced')) {
      const details = document.createElement('details');
      details.id = 'bestServerAdvanced';
      const summary = document.createElement('summary');
      summary.textContent = 'Ручной выбор Extra-профиля и дополнительные действия';
      details.appendChild(summary);
      profilesList.parentNode.insertBefore(details, profilesList);
      details.appendChild(profilesList);
      if (routine) details.appendChild(routine);
      if (guard) details.appendChild(guard);
    }
    try { if (typeof lastStatus !== 'undefined' && lastStatus) renderCurrentIdentity(lastStatus); } catch (_) {}
    renderMetrics(qs('#bestCurrentMetrics'), currentQuality);
    return shell;
  }

  function setBusy(mode) {
    scanBusy = !!mode;
    const current = qs('#bestServerCheckCurrent');
    const best = qs('#bestServerRefresh');
    const apply = qs('#bestServerApply');
    if (current) { current.disabled = scanBusy || applyBusy; current.textContent = mode === 'current' ? 'Проверяем текущий…' : 'Проверить текущий VPN'; }
    if (best) { best.disabled = scanBusy || applyBusy; best.textContent = mode === 'best' ? 'Ищем лучший…' : 'Найти лучший VPN'; }
    if (apply) apply.disabled = scanBusy || applyBusy || !recommendation || recommendation.current;
    const status = qs('#bestServerStatus');
    if (status) status.classList.toggle('busy', !!mode);
  }

  async function scanCurrentVPN() {
    if (scanBusy || applyBusy) return;
    mountBestServerUI();
    setBusy('current');
    setText(qs('#bestServerStatus'), 'Проверяем только текущий VPN. Другие профили не сканируются. MUTATION NONE.');
    try {
      const response = await fetch('/api/vpn/current-quality', {cache: 'no-store'});
      const body = await response.json().catch(() => null);
      if (!response.ok || !body || !body.success) {
        setText(qs('#bestServerStatus'), (body && body.error) || 'Не удалось проверить текущий VPN.');
        return;
      }
      renderCurrentQuality(body);
    } catch (_) {
      setText(qs('#bestServerStatus'), 'Не удалось проверить текущий VPN: нет связи с FreeNet.');
    } finally {
      setBusy(false);
    }
  }

  async function scanBestServer() {
    if (scanBusy || applyBusy) return;
    mountBestServerUI();
    setBusy('best');
    recommendation = null;
    const apply = qs('#bestServerApply');
    if (apply) apply.classList.remove('show');
    setText(qs('#bestServerStatus'), 'Сравниваем зарубежные Extra-профили. Российские серверы исключены. MUTATION NONE.');
    try {
      const response = await fetch('/api/vpn/best-foreign', {cache: 'no-store'});
      const body = await response.json().catch(() => null);
      if (!response.ok || !body || !body.success) {
        renderBestResult(null);
        setText(qs('#bestServerStatus'), (body && body.error) || 'Поиск лучшего VPN не завершён.');
        return;
      }
      renderBestResult(body);
    } catch (_) {
      renderBestResult(null);
      setText(qs('#bestServerStatus'), 'Поиск лучшего VPN не завершён: нет связи с FreeNet.');
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
          if (status && status.xray_online && status.endpoint === expected) return status;
        }
      } catch (_) {}
      await wait(850);
    }
    return null;
  }

  async function applyBestServer() {
    if (applyBusy || scanBusy || !recommendation || recommendation.current || !recommendation.id || isRussianProfile(recommendation)) return;
    applyBusy = true;
    setBusy(false);
    const apply = qs('#bestServerApply');
    if (apply) { apply.disabled = true; apply.textContent = 'Переключаем…'; }
    setText(qs('#bestServerStatus'), 'Применяем выбранный зарубежный профиль транзакционно и подтверждаем фактический endpoint.');
    const expectedEndpoint = recommendation.endpoint;
    try {
      const response = await fetch('/api/network-profile/apply', {
        method: 'POST', headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({operation: 'provider', profile_id: recommendation.id, confirm: true})
      });
      const body = await response.json().catch(() => null);
      if (!response.ok || !body || !body.success) {
        setText(qs('#bestServerStatus'), (body && (body.primary_error || body.error)) || 'VPN не переключён.');
        return;
      }
      const status = await waitForEndpoint(expectedEndpoint);
      if (!status) {
        setText(qs('#bestServerStatus'), 'Apply завершён, но endpoint ещё не подтверждён. Blind retry не выполняется.');
        return;
      }
      recommendation = null;
      currentQuality = null;
      renderOverviewTopbarFromStatus(status);
      if (qs('#bestServerResult')) qs('#bestServerResult').classList.remove('show');
      if (apply) apply.classList.remove('show');
      renderMetrics(qs('#bestCurrentMetrics'), null);
      setText(qs('#bestServerStatus'), 'VPN переключён и endpoint подтверждён. Для новых метрик нажмите «Проверить текущий VPN».');
    } catch (_) {
      setText(qs('#bestServerStatus'), 'Связь прервалась во время переключения. Operation Coordinator reconciles результат; второй mutation не запускается.');
    } finally {
      applyBusy = false;
      setBusy(false);
    }
  }

  function installBestServerActionDelegation() {
    const root = document.documentElement;
    if (!root || root.dataset.freenetBestServerActions === '1') return;
    root.dataset.freenetBestServerActions = '1';
    document.addEventListener('click', event => {
      const origin = event.target;
      if (!origin || typeof origin.closest !== 'function') return;
      const button = origin.closest('#bestServerCheckCurrent, #bestServerRefresh, #bestServerApply');
      if (!button || button.disabled) return;
      event.preventDefault();
      if (button.id === 'bestServerCheckCurrent') { void scanCurrentVPN(); return; }
      if (button.id === 'bestServerRefresh') { void scanBestServer(); return; }
      if (button.id === 'bestServerApply') void applyBestServer();
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