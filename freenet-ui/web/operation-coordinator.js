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
    return new Response(JSON.stringify(body), {
      status,
      headers: {'Content-Type': 'application/json; charset=utf-8'}
    });
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
        return jsonResponse(200, {
          success: true,
          action: meta.target,
          operation_id: op.id,
          message: op.message || 'VPN-действие выполнено'
        });
      }
      return jsonResponse(200, {
        success: true,
        applied: true,
        operation: 'provider',
        profile_id: meta.target,
        operation_id: op.id,
        rollback_state: 'NOT_NEEDED',
        message: op.message || 'VPN-профиль применён и проверен'
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
      try {
        conflict = await response.clone().json();
      } catch (_) {}
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
  let lastQualityData = null;
  let scanBusy = false;
  let applyBusy = false;
  let authRetries = 0;
  let topbarSyncTimer = null;

  function setText(node, value) {
    if (node) node.textContent = value || '';
  }

  function injectOverviewV3Style() {
    if (qs('#FreeNetOverviewV3')) return;
    const style = document.createElement('style');
    style.id = 'FreeNetOverviewV3';
    style.textContent = `
      :root{--muted:#b6c3d5;--muted2:#8fa1b9;--line:#2c405c;--line2:#45628a}
      body{font-size:15px;letter-spacing:.002em}
      .nav-btn{font-size:15px;padding:12px 13px}.nav-icon{font-size:16px}
      .content{width:min(1240px,calc(100% - 48px));padding-top:32px}
      .page-head{margin-bottom:22px}.page-head h1{font-size:31px}.page-head p{font-size:15px;line-height:1.55;color:#b9c6d8}.page-kicker{font-size:12px}
      .card{border-color:#2c405c;padding:22px;border-radius:20px;background:linear-gradient(160deg,rgba(18,32,52,.98),rgba(10,20,34,.98));box-shadow:0 22px 60px rgba(0,0,0,.28),inset 0 1px 0 rgba(255,255,255,.035)}
      .card h2{font-size:18px}.card-head{margin-bottom:16px}.hint{font-size:14px;line-height:1.55;color:#b6c3d5}.summary-state{font-size:13px}.btn{font-size:14.5px;padding:13px 15px}.field label{font-size:12px}.field input,.field select{font-size:14px;padding:12px}.selected-profile{font-size:14px}.selected-profile strong{font-size:16px}.selected-profile .selected-note{font-size:13px;line-height:1.55}.profile-option-main{font-size:14px}.profile-option-endpoint{font-size:12px}.details summary{font-size:13px}.notice{font-size:13.5px}.footer{font-size:12px}
      .topbar.overview-v3-topbar{height:88px;padding:0 30px;display:grid;grid-template-columns:auto minmax(620px,1fr) auto;gap:18px;align-items:center;background:rgba(7,15,26,.94);border-bottom:1px solid rgba(66,91,126,.62)}
      .overview-v3-top{justify-self:center;width:min(900px,100%);display:grid;grid-template-columns:minmax(280px,1.6fr) minmax(145px,.7fr) minmax(170px,.8fr);gap:10px;min-width:0}
      .overview-v3-chip{min-width:0;padding:10px 13px;border:1px solid #314866;border-radius:14px;background:linear-gradient(180deg,rgba(22,39,62,.95),rgba(12,24,40,.95));box-shadow:inset 0 1px 0 rgba(255,255,255,.045)}
      .overview-v3-chip-label{display:block;color:#91a9c9;font-size:11px;font-weight:800;letter-spacing:.08em;text-transform:uppercase;margin-bottom:4px}
      .overview-v3-chip-value{display:flex;align-items:center;gap:9px;color:#f7f9fd;font-size:15px;font-weight:800;min-width:0}
      .overview-v3-chip-value strong{white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.overview-v3-endpoint{display:block;margin-top:3px;color:#a8b8cc;font-size:12px;font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
      .overview-v3-health{padding:10px 13px!important;font-size:13px!important;color:#d5e0ec!important;border-color:#314866!important;background:rgba(16,31,49,.94)!important}.overview-v3-health .dot{width:9px;height:9px}
      .overview-hero-source{display:none!important}.overview-compact-grid{grid-template-columns:minmax(0,1fr)!important}.overview-compact-grid #quickActionsSection{width:100%;max-width:none}
      .page[data-page-view="overview"] > .grid-equal{display:none!important}
      #quickActionsSection{overflow:hidden;position:relative}#quickActionsSection:before{content:'';position:absolute;inset:-120px auto auto -80px;width:300px;height:300px;background:radial-gradient(circle,rgba(91,140,255,.12),transparent 67%);pointer-events:none}
      .best-v3-panel{position:relative;margin-top:6px;padding:20px;border:1px solid #334d70;border-radius:18px;background:linear-gradient(145deg,rgba(17,33,55,.98),rgba(8,19,33,.98));box-shadow:inset 0 1px 0 rgba(255,255,255,.04)}
      .best-v3-kicker{font-size:12px;text-transform:uppercase;letter-spacing:.1em;font-weight:850;color:#80a8ff}.best-v3-title{margin-top:7px;font-size:23px;font-weight:850;letter-spacing:-.025em;color:#fff}.best-v3-endpoint{margin-top:5px;font-size:13px;color:#aebdd0;font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace}
      .best-v3-explain{margin-top:14px;padding:13px 15px;border-left:3px solid #5b8cff;border-radius:0 12px 12px 0;background:rgba(91,140,255,.075)}.best-v3-explain b{display:block;font-size:14px;margin-bottom:4px}.best-v3-explain span{display:block;color:#c1cddd;font-size:13.5px;line-height:1.5}
      .best-v3-compare{display:grid;grid-template-columns:1fr 1fr;gap:12px;margin-top:15px}.best-v3-candidate{padding:15px;border:1px solid #2b4262;border-radius:15px;background:#0a1727}.best-v3-candidate.recommended{border-color:#4f7ed0;background:linear-gradient(160deg,#102546,#0a1727)}.best-v3-candidate.current{border-color:#31566a}
      .best-v3-candidate-head{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:10px}.best-v3-candidate-head b{font-size:15px}.best-v3-badge{font-size:10px;font-weight:850;letter-spacing:.07em;text-transform:uppercase;padding:4px 7px;border:1px solid #38577e;border-radius:999px;color:#a9c5f5}
      .best-v3-metrics{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:8px}.best-v3-metric{padding:10px;border:1px solid #223752;border-radius:11px;background:rgba(7,17,29,.72)}.best-v3-metric span{display:block;color:#91a4bd;font-size:11px;margin-bottom:4px}.best-v3-metric strong{display:block;color:#f8fbff;font-size:16px;white-space:nowrap}.best-v3-metric.speed strong{color:#93f1bb}
      .best-v3-actions{grid-template-columns:1.35fr 1fr 1fr!important;gap:10px!important;margin-top:14px!important}.best-v3-actions .btn{min-height:48px;justify-content:center;text-align:center}
      #bestServerStats{font-size:13px;margin-top:12px;color:#aebdd0}.best-current-focus{box-shadow:0 0 0 2px rgba(73,218,146,.4),0 0 28px rgba(73,218,146,.08)}
      @media(max-width:1200px){.topbar.overview-v3-topbar{grid-template-columns:auto minmax(420px,1fr) auto}.overview-v3-top{grid-template-columns:minmax(230px,1.4fr) 130px 150px}.overview-v3-endpoint{display:none}}
      @media(max-width:900px){.topbar.overview-v3-topbar{height:auto;min-height:86px;grid-template-columns:auto 1fr auto;padding:9px 16px}.overview-v3-top{grid-template-columns:1fr 1fr}.overview-v3-chip:first-child{grid-column:1/-1}.best-v3-compare{grid-template-columns:1fr}.best-v3-metrics{grid-template-columns:repeat(2,1fr)}}
      @media(max-width:600px){.overview-v3-top{display:none}.topbar.overview-v3-topbar{grid-template-columns:auto 1fr}.top-actions{justify-self:end}.best-v3-actions{grid-template-columns:1fr!important}.best-v3-title{font-size:20px}.content{width:calc(100% - 20px)}}
    `;
    document.head.appendChild(style);
  }

  function dnsLabel(s) {
    return s && s.dns_mode === 'xkeen' ? 'XKeen/Xray DNS' : 'DNS напрямую';
  }

  function healthStateFromStatus(s) {
    if (!s) return {healthy: false, label: 'Проверяем состояние'};
    const xrayHealthy = !!s.xray_online;
    const xrayDNS = s.dns_mode === 'xkeen';
    const dnsHealthy = xrayDNS ? !!s.dns_out_present : true;
    if (!xrayHealthy) return {healthy: false, label: 'VPN не работает'};
    if (!dnsHealthy) return {healthy: false, label: 'DNS требует внимания'};
    return {healthy: true, label: xrayDNS ? 'VPN + DNS OK' : 'VPN OK · DNS напрямую'};
  }

  function flagClass(code) {
    const value = String(code || '').trim().toLowerCase();
    return /^[a-z]{2}$/.test(value) ? `flag-${value}` : 'flag-unknown';
  }

  function renderOverviewTopbarFromStatus(s) {
    if (!s) return;
    const profile = s.country ? `${s.country}${s.city ? ' · ' + s.city : ''}` : 'VPN не определён';
    const vpnName = qs('#topVpnProfile');
    const endpoint = qs('#topVpnEndpoint');
    const flag = qs('#topVpnFlag');
    const isp = qs('#topISPValue');
    const dns = qs('#topDNSValue');
    if (vpnName) vpnName.textContent = profile;
    if (endpoint) endpoint.textContent = s.endpoint || '—';
    if (flag) flag.className = `flag-icon ${flagClass(s.country_code)}`;
    if (isp) isp.textContent = s.isp_label || s.isp || 'Не определён';
    if (dns) dns.textContent = dnsLabel(s);
    const health = healthStateFromStatus(s);
    const dot = qs('#topDot');
    const status = qs('#topStatus');
    if (dot) dot.className = 'dot ' + (health.healthy ? 'ok' : 'bad');
    if (status) status.textContent = health.label;
  }

  function syncOverviewTopbar() {
    try {
      if (typeof lastStatus !== 'undefined' && lastStatus) {
        renderOverviewTopbarFromStatus(lastStatus);
        return;
      }
    } catch (_) {}
    const profile = qs('#profile');
    const endpoint = qs('#endpoint');
    const topProfile = qs('#topVpnProfile');
    const topEndpoint = qs('#topVpnEndpoint');
    if (topProfile && profile) topProfile.textContent = profile.textContent || 'VPN не определён';
    if (topEndpoint && endpoint) topEndpoint.textContent = endpoint.textContent || '—';
    const isp = qs('#overviewISP');
    const dns = qs('#overviewDNS');
    if (qs('#topISPValue') && isp) qs('#topISPValue').textContent = isp.textContent || 'Не определён';
    if (qs('#topDNSValue') && dns) qs('#topDNSValue').textContent = dns.textContent || 'Не определён';
  }

  function installOverviewTopbarStatusHook() {
    if (typeof updateStatusViews !== 'function' || updateStatusViews.__freenetOverviewTopbarHook) return;
    const previousUpdateStatusViews = updateStatusViews;
    const wrapped = function(s) {
      const result = previousUpdateStatusViews.apply(this, arguments);
      queueMicrotask(() => renderOverviewTopbarFromStatus(s));
      return result;
    };
    wrapped.__freenetOverviewTopbarHook = true;
    updateStatusViews = wrapped;
  }

  function makeTopChip(label, id, extra) {
    const chip = document.createElement('div');
    chip.className = 'overview-v3-chip';
    const l = document.createElement('span');
    l.className = 'overview-v3-chip-label';
    l.textContent = label;
    const value = document.createElement('div');
    value.className = 'overview-v3-chip-value';
    if (extra) value.appendChild(extra);
    const strong = document.createElement('strong');
    strong.id = id;
    strong.textContent = 'Определяем…';
    value.appendChild(strong);
    chip.appendChild(l);
    chip.appendChild(value);
    return {chip, strong};
  }

  function mountOverviewTopbar() {
    injectOverviewV3Style();
    const topbar = qs('.topbar');
    const topActions = qs('.top-actions');
    const overview = qs('.page[data-page-view="overview"]');
    const grid = overview && overview.querySelector('.grid-2');
    const hero = overview && overview.querySelector('.hero');
    if (!topbar || !topActions || !grid || !hero) return;
    topbar.classList.add('overview-v3-topbar');
    grid.classList.add('overview-compact-grid');
    hero.classList.add('overview-hero-source');
    const oldSummary = qs('#topVpnSummary');
    if (oldSummary) oldSummary.remove();
    const topStatus = qs('.top-status');
    if (topStatus) topStatus.classList.add('overview-v3-health');

    if (!qs('#topVpnSummary')) {
      const summary = document.createElement('div');
      summary.id = 'topVpnSummary';
      summary.className = 'overview-v3-top';
      summary.setAttribute('aria-label', 'Текущий VPN, ISP и DNS');
      const flag = document.createElement('span');
      flag.id = 'topVpnFlag';
      flag.className = 'flag-icon flag-unknown';
      const vpn = makeTopChip('Текущий VPN', 'topVpnProfile', flag);
      const endpoint = document.createElement('span');
      endpoint.id = 'topVpnEndpoint';
      endpoint.className = 'overview-v3-endpoint';
      endpoint.textContent = '—';
      vpn.chip.appendChild(endpoint);
      summary.appendChild(vpn.chip);
      summary.appendChild(makeTopChip('ISP', 'topISPValue').chip);
      summary.appendChild(makeTopChip('DNS', 'topDNSValue').chip);
      topbar.insertBefore(summary, topActions);
    }

    const watched = ['#profile', '#endpoint', '#overviewISP', '#overviewDNS'].map(selector => qs(selector)).filter(Boolean);
    if (watched.length) {
      const observer = new MutationObserver(syncOverviewTopbar);
      watched.forEach(node => observer.observe(node, {subtree: true, childList: true, characterData: true, attributes: true}));
    }
    syncOverviewTopbar();
    if (!topbarSyncTimer) topbarSyncTimer = setInterval(syncOverviewTopbar, 2500);
  }

  function confidenceLabel(value) {
    if (value === 'high') return 'Высокая';
    if (value === 'medium') return 'Средняя';
    return 'Не определена';
  }

  function metricValue(candidate, kind) {
    if (!candidate) return '—';
    if (kind === 'speed') {
      const n = Number(candidate.download_mbps || 0);
      return n > 0 ? `${n.toFixed(n >= 100 ? 0 : 1)} Мбит/с` : '—';
    }
    if (kind === 'http') return candidate.application_rtt_ms ? `${candidate.application_rtt_ms} мс` : '—';
    if (kind === 'tcp') return candidate.tcp_rtt_ms ? `${candidate.tcp_rtt_ms} мс` : '—';
    if (kind === 'jitter') return Number.isFinite(Number(candidate.jitter_ms)) ? `${candidate.jitter_ms} мс` : '—';
    return '—';
  }

  function candidateName(candidate, fallback) {
    return candidate && candidate.name ? candidate.name : fallback;
  }

  function currentCandidate(data) {
    return Array.isArray(data && data.candidates) ? data.candidates.find(item => item && item.current) || null : null;
  }

  function metricTile(label, value, kind) {
    const box = document.createElement('div');
    box.className = 'best-v3-metric' + (kind === 'speed' ? ' speed' : '');
    const l = document.createElement('span');
    l.textContent = label;
    const v = document.createElement('strong');
    v.textContent = value;
    box.appendChild(l);
    box.appendChild(v);
    return box;
  }

  function renderCandidateColumn(node, candidate, role) {
    if (!node) return;
    node.textContent = '';
    node.className = 'best-v3-candidate ' + role;
    const head = document.createElement('div');
    head.className = 'best-v3-candidate-head';
    const name = document.createElement('b');
    name.textContent = candidateName(candidate, role === 'current' ? 'Текущий VPN' : 'Рекомендация');
    const badge = document.createElement('span');
    badge.className = 'best-v3-badge';
    badge.textContent = role === 'current' ? 'Текущий' : 'Рекомендуем';
    head.appendChild(name);
    head.appendChild(badge);
    const metrics = document.createElement('div');
    metrics.className = 'best-v3-metrics';
    metrics.appendChild(metricTile('Скорость', metricValue(candidate, 'speed'), 'speed'));
    metrics.appendChild(metricTile('HTTP-отклик', metricValue(candidate, 'http'), 'http'));
    metrics.appendChild(metricTile('TCP', metricValue(candidate, 'tcp'), 'tcp'));
    metrics.appendChild(metricTile('Jitter', metricValue(candidate, 'jitter'), 'jitter'));
    node.appendChild(head);
    node.appendChild(metrics);
    if (candidate && candidate.endpoint) {
      const ep = document.createElement('div');
      ep.className = 'best-v3-endpoint';
      ep.style.marginTop = '9px';
      ep.textContent = candidate.endpoint;
      node.appendChild(ep);
    }
  }

  function recommendationReason(data) {
    const best = data && data.recommendation;
    const current = currentCandidate(data);
    if (!best) return 'Недостаточно стабильных измерений для безопасной рекомендации.';
    if (best.current) return 'Текущий VPN уже показывает лучший подтверждённый баланс скорости, отклика и стабильности среди проверенных кандидатов.';
    const bestSpeed = Number(best.download_mbps || 0);
    const currentSpeed = Number(current && current.download_mbps || 0);
    const bestHTTP = Number(best.application_rtt_ms || 0);
    const currentHTTP = Number(current && current.application_rtt_ms || 0);
    const parts = [];
    if (bestSpeed > 0 && currentSpeed > 0 && bestSpeed > currentSpeed) parts.push(`скорость выше примерно на ${(bestSpeed - currentSpeed).toFixed(1)} Мбит/с`);
    if (bestHTTP > 0 && currentHTTP > 0 && bestHTTP < currentHTTP) parts.push(`HTTP-отклик быстрее на ${currentHTTP - bestHTTP} мс`);
    if (!parts.length) parts.push('лучший суммарный score по скорости, отклику и стабильности');
    return `FreeNet рекомендует этот профиль: ${parts.join(', ')}. Текущий VPN не меняется до нажатия кнопки переключения.`;
  }

  function mountBestServerUI() {
    mountOverviewTopbar();
    const quick = qs('#quickActionsSection');
    if (!quick) return null;
    const head = quick.querySelector('.card-head');
    const title = head && head.querySelector('h2');
    const hint = head && head.querySelector('.hint');
    if (title) title.textContent = 'Лучший VPN';
    if (hint) hint.textContent = 'FreeNet сравнивает текущий VPN и кандидатов одинаковыми замерами скорости, HTTP, TCP и jitter.';
    const countries = quick.querySelector('.quick-layout');
    if (countries) countries.remove();
    const profilesList = qs('#profilesList');
    const profileLabel = profilesList && profilesList.querySelector('label[for="profileSearch"]');
    if (profileLabel) profileLabel.textContent = 'Ручной выбор Extra-профиля';

    let card = qs('#bestServerCard');
    if (!card) {
      card = document.createElement('div');
      card.id = 'bestServerCard';
      card.className = 'best-v3-panel';
      card.innerHTML = `
        <div class="best-v3-kicker">Рекомендация FreeNet</div>
        <div id="bestServerName" class="best-v3-title">Проверяем доступные VPN…</div>
        <div id="bestServerEndpoint" class="best-v3-endpoint">Live VPN не переключается</div>
        <div class="best-v3-explain"><b>Почему рекомендуем</b><span id="bestServerReason">Собираем одинаковые метрики для текущего VPN и кандидатов.</span></div>
        <div class="best-v3-compare"><div id="bestCurrentColumn" class="best-v3-candidate current"></div><div id="bestRecommendedColumn" class="best-v3-candidate recommended"></div></div>
        <div id="bestServerMetrics" class="hint" style="margin-top:12px">Scan: MUTATION NONE.</div>`;
      const actions = document.createElement('div');
      actions.id = 'bestServerActions';
      actions.className = 'action-row best-v3-actions';
      const apply = document.createElement('button');
      apply.id = 'bestServerApply';
      apply.type = 'button';
      apply.className = 'btn primary';
      apply.disabled = true;
      apply.textContent = 'Переключиться на лучший';
      const current = document.createElement('button');
      current.id = 'bestServerCheckCurrent';
      current.type = 'button';
      current.className = 'btn secondary';
      current.textContent = 'Проверить текущий VPN';
      const refresh = document.createElement('button');
      refresh.id = 'bestServerRefresh';
      refresh.type = 'button';
      refresh.className = 'btn secondary';
      refresh.textContent = 'Проверить всё заново';
      actions.appendChild(apply);
      actions.appendChild(current);
      actions.appendChild(refresh);
      const stats = document.createElement('div');
      stats.id = 'bestServerStats';
      stats.className = 'hint';
      stats.textContent = 'Проверка read-only: VPN, ISP, DNS и routing не изменяются.';
      const anchor = profilesList || quick.querySelector('.action-row');
      if (anchor) {
        quick.insertBefore(card, anchor);
        quick.insertBefore(actions, anchor);
        quick.insertBefore(stats, anchor);
      } else {
        quick.appendChild(card);
        quick.appendChild(actions);
        quick.appendChild(stats);
      }
      apply.addEventListener('click', applyBestServer);
      refresh.addEventListener('click', () => scanBestServer(true, false));
      current.addEventListener('click', () => scanBestServer(true, true));
    }

    const routine = qs('#updateBtn') && qs('#updateBtn').closest('.action-row');
    const guard = qs('#quickNetworkGuard');
    if (routine && !qs('#bestServerAdvanced')) {
      const details = document.createElement('details');
      details.id = 'bestServerAdvanced';
      details.style.marginTop = '12px';
      const summary = document.createElement('summary');
      summary.className = 'hint';
      summary.style.cursor = 'pointer';
      summary.textContent = 'Дополнительные VPN-действия и ручной выбор';
      routine.parentNode.insertBefore(details, routine);
      details.appendChild(summary);
      details.appendChild(routine);
      if (guard) details.appendChild(guard);
    }
    return card;
  }

  function setBusy(busy) {
    scanBusy = busy;
    const refresh = qs('#bestServerRefresh');
    const current = qs('#bestServerCheckCurrent');
    const apply = qs('#bestServerApply');
    if (refresh) {
      refresh.disabled = busy || applyBusy;
      refresh.textContent = busy ? 'Проверяем…' : 'Проверить всё заново';
    }
    if (current) {
      current.disabled = busy || applyBusy;
      current.textContent = busy ? 'Проверяем…' : 'Проверить текущий VPN';
    }
    if (apply) apply.disabled = busy || applyBusy || !recommendation || recommendation.current;
  }

  function renderBestServer(data, focusCurrent) {
    lastQualityData = data && data.success ? data : null;
    recommendation = data && data.available ? data.recommendation : null;
    const name = qs('#bestServerName');
    const endpoint = qs('#bestServerEndpoint');
    const reason = qs('#bestServerReason');
    const metrics = qs('#bestServerMetrics');
    const stats = qs('#bestServerStats');
    const apply = qs('#bestServerApply');
    const current = currentCandidate(data);

    if (!data || !data.success) {
      setText(name, 'Рекомендация сейчас недоступна');
      setText(endpoint, 'Текущий VPN не изменён.');
      setText(reason, 'Не удалось получить достаточно достоверных измерений. Можно повторить read-only проверку позже.');
      renderCandidateColumn(qs('#bestCurrentColumn'), null, 'current');
      renderCandidateColumn(qs('#bestRecommendedColumn'), null, 'recommended');
      setText(metrics, 'Scan: MUTATION NONE.');
      if (stats) stats.textContent = 'Автоматического переключения или blind retry нет.';
      if (apply) apply.disabled = true;
      return;
    }

    if (!recommendation) {
      setText(name, 'Нет достоверно лучшего профиля');
      setText(endpoint, 'Текущий VPN сохранён без изменений.');
      setText(reason, recommendationReason(data));
      renderCandidateColumn(qs('#bestCurrentColumn'), current, 'current');
      renderCandidateColumn(qs('#bestRecommendedColumn'), null, 'recommended');
      setText(metrics, 'Scan: MUTATION NONE.');
      if (stats) stats.textContent = `Проверено ${data.profiles_scanned || 0} из ${data.profiles_total || 0} профилей.`;
      if (apply) apply.disabled = true;
      return;
    }

    setText(name, recommendation.current ? 'Текущий VPN уже лучший' : `Лучший VPN: ${candidateName(recommendation, 'Extra-профиль')}`);
    setText(endpoint, recommendation.endpoint || 'endpoint не указан');
    setText(reason, recommendationReason(data));
    renderCandidateColumn(qs('#bestCurrentColumn'), current, 'current');
    renderCandidateColumn(qs('#bestRecommendedColumn'), recommendation, 'recommended');
    setText(metrics, `Уверенность: ${confidenceLabel(recommendation.confidence)} · Scan: MUTATION NONE.`);
    const deep = Array.isArray(data.candidates) ? data.candidates.filter(candidate => candidate && candidate.available).length : 0;
    if (stats) stats.textContent = `${data.message || 'Рекомендация готова.'} Проверено профилей: ${data.profiles_scanned || 0}/${data.profiles_total || 0}; глубокая VPN-проверка: ${deep}.`;
    if (apply) {
      apply.disabled = recommendation.current || scanBusy || applyBusy;
      apply.textContent = recommendation.current ? 'Уже используется лучший' : 'Переключиться на лучший';
    }
    const currentColumn = qs('#bestCurrentColumn');
    if (currentColumn) currentColumn.classList.toggle('best-current-focus', !!focusCurrent);
  }

  async function scanBestServer(force, focusCurrent) {
    if (scanBusy || applyBusy) return;
    mountBestServerUI();
    setBusy(true);
    setText(qs('#bestServerName'), focusCurrent ? 'Проверяем текущий VPN…' : 'Ищем лучший VPN…');
    setText(qs('#bestServerEndpoint'), 'Измеряем скорость, HTTP-отклик, TCP и jitter через реальный временный Xray path.');
    setText(qs('#bestServerReason'), 'Проверка read-only. Текущий VPN не переключается.');
    setText(qs('#bestServerMetrics'), 'Scan: MUTATION NONE.');
    try {
      const response = await fetch('/api/vpn/best' + (force ? '?refresh=1' : ''), {cache: 'no-store'});
      if (response.status === 401) {
        if (authRetries++ < 20) setTimeout(() => scanBestServer(false, focusCurrent), 3000);
        return;
      }
      const body = await response.json().catch(() => null);
      if (!response.ok || !body || !body.success) {
        renderBestServer(null, focusCurrent);
        return;
      }
      authRetries = 0;
      renderBestServer(body, focusCurrent);
    } catch (_) {
      renderBestServer(null, focusCurrent);
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
    if (applyBusy || scanBusy || !recommendation || recommendation.current || !recommendation.id) return;
    applyBusy = true;
    setBusy(false);
    const apply = qs('#bestServerApply');
    if (apply) {
      apply.disabled = true;
      apply.textContent = 'Переключаем…';
    }
    setText(qs('#bestServerName'), 'Переключаемся на рекомендованный VPN…');
    setText(qs('#bestServerReason'), 'Используется transactional provider apply; после операции FreeNet подтверждает фактический endpoint.');
    const expectedEndpoint = recommendation.endpoint;
    try {
      const response = await fetch('/api/network-profile/apply', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({operation: 'provider', profile_id: recommendation.id, confirm: true})
      });
      const body = await response.json().catch(() => null);
      if (!response.ok || !body || !body.success) {
        setText(qs('#bestServerName'), 'VPN не переключён');
        setText(qs('#bestServerReason'), (body && (body.primary_error || body.error)) || 'Операция завершилась ошибкой; автоматического повтора нет.');
        return;
      }
      const status = await waitForEndpoint(expectedEndpoint);
      if (!status) {
        setText(qs('#bestServerName'), 'Требуется проверка фактического состояния');
        setText(qs('#bestServerReason'), 'Apply завершён, но endpoint ещё не подтверждён. Blind retry не выполняется.');
        return;
      }
      recommendation = null;
      lastQualityData = null;
      renderOverviewTopbarFromStatus(status);
      await scanBestServer(true, false);
    } catch (_) {
      setText(qs('#bestServerName'), 'Связь прервалась во время переключения');
      setText(qs('#bestServerReason'), 'Operation Coordinator сначала reconciles фактический результат; второй mutation автоматически не запускается.');
    } finally {
      applyBusy = false;
      setBusy(false);
    }
  }

  function start() {
    mountOverviewTopbar();
    installOverviewTopbarStatusHook();
    syncOverviewTopbar();
    if (!mountBestServerUI()) return;
    setTimeout(() => scanBestServer(false, false), 900);
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', start, {once: true});
  else start();
})();
