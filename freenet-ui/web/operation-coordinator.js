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
  let scanBusy = false;
  let applyBusy = false;
  let authRetries = 0;
  let topbarSyncTimer = null;

  function setText(node, value) {
    if (node) node.textContent = value || '';
  }

  function injectOverviewCompactStyle() {
    if (qs('#overviewCompactStyle')) return;
    const style = document.createElement('style');
    style.id = 'overviewCompactStyle';
    style.textContent = `
      .topbar.compact-vpn-topbar{height:72px;display:grid;grid-template-columns:auto minmax(260px,1fr) auto;gap:20px;align-items:center}
      .top-vpn-summary{justify-self:center;min-width:0;max-width:560px;width:100%;display:flex;align-items:center;gap:10px;padding:7px 12px;border:1px solid rgba(51,73,103,.72);border-radius:12px;background:rgba(13,25,40,.72);box-shadow:inset 0 1px 0 rgba(255,255,255,.025)}
      .top-vpn-flag{width:28px;height:19px;border-radius:4px;box-shadow:none}
      .top-vpn-copy{display:flex;align-items:baseline;gap:10px;min-width:0;flex:1}
      .top-vpn-profile{font-size:12px;font-weight:800;color:var(--text);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
      .top-vpn-endpoint{font-size:10px;color:var(--muted);font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;white-space:nowrap}
      .top-health-state{padding:7px 10px;border:1px solid rgba(51,73,103,.72);border-radius:999px;background:rgba(13,25,40,.72);color:var(--muted);font-weight:700;letter-spacing:.01em}
      .top-health-state .dot{width:7px;height:7px;box-shadow:none}
      .top-health-state .dot.ok{box-shadow:none}
      .overview-compact-grid{grid-template-columns:minmax(0,1fr)!important}
      .overview-compact-grid #quickActionsSection{width:100%;max-width:none}
      .overview-hero-source{display:none!important}
      @media(max-width:980px){.topbar.compact-vpn-topbar{grid-template-columns:auto minmax(0,1fr) auto;gap:10px}.top-vpn-summary{justify-self:stretch}.top-vpn-endpoint{display:none}}
      @media(max-width:700px){.top-vpn-copy{display:block}.top-vpn-profile{display:block}.top-health-state #topStatus{display:none}.top-health-state{padding:8px}.top-vpn-summary{padding:7px 9px}}
    `;
    document.head.appendChild(style);
  }

  function syncOverviewTopbar() {
    const profile = qs('#profile');
    const endpoint = qs('#endpoint');
    const sourceFlag = qs('#heroFlag');
    const topProfile = qs('#topVpnProfile');
    const topEndpoint = qs('#topVpnEndpoint');
    const topFlag = qs('#topVpnFlag');
    if (topProfile && profile) topProfile.textContent = profile.textContent || 'VPN не определён';
    if (topEndpoint && endpoint) topEndpoint.textContent = endpoint.textContent || '—';
    if (topFlag && sourceFlag) {
      const flagClass = Array.from(sourceFlag.classList).find(name => name.startsWith('flag-') && name !== 'flag-icon' && name !== 'flag-hero') || 'flag-unknown';
      topFlag.className = `flag-icon top-vpn-flag ${flagClass}`;
      topFlag.setAttribute('aria-label', profile && profile.textContent ? profile.textContent : 'VPN');
    }

    const xray = (qs('#xrayState') && qs('#xrayState').textContent || '').toLowerCase();
    const dns = (qs('#dnsState') && qs('#dnsState').textContent || '').toLowerCase();
    const healthy = xray.includes('работает') && dns.includes('защищ');
    const dot = qs('#topDot');
    const status = qs('#topStatus');
    if (dot) dot.className = 'dot ' + (healthy ? 'ok' : 'bad');
    if (status) status.textContent = healthy ? 'VPN + DNS OK' : 'Система требует внимания';
  }

  function mountOverviewTopbar() {
    injectOverviewCompactStyle();
    const topbar = qs('.topbar');
    const topActions = qs('.top-actions');
    const overview = qs('.page[data-page-view="overview"]');
    const grid = overview && overview.querySelector('.grid-2');
    const hero = overview && overview.querySelector('.hero');
    if (!topbar || !topActions || !grid || !hero) return;

    topbar.classList.add('compact-vpn-topbar');
    grid.classList.add('overview-compact-grid');
    hero.classList.add('overview-hero-source');

    const topStatus = qs('.top-status');
    if (topStatus) topStatus.classList.add('top-health-state');

    if (!qs('#topVpnSummary')) {
      const summary = document.createElement('div');
      summary.id = 'topVpnSummary';
      summary.className = 'top-vpn-summary';
      summary.setAttribute('aria-label', 'Текущий VPN');

      const flag = document.createElement('span');
      flag.id = 'topVpnFlag';
      flag.className = 'flag-icon top-vpn-flag flag-unknown';
      flag.setAttribute('role', 'img');
      flag.setAttribute('aria-label', 'VPN');

      const copy = document.createElement('span');
      copy.className = 'top-vpn-copy';
      const profile = document.createElement('strong');
      profile.id = 'topVpnProfile';
      profile.className = 'top-vpn-profile';
      profile.textContent = 'Определяем VPN…';
      const endpoint = document.createElement('span');
      endpoint.id = 'topVpnEndpoint';
      endpoint.className = 'top-vpn-endpoint';
      endpoint.textContent = '—';
      copy.appendChild(profile);
      copy.appendChild(endpoint);
      summary.appendChild(flag);
      summary.appendChild(copy);
      topbar.insertBefore(summary, topActions);
    }

    const watched = ['#profile', '#endpoint', '#heroFlag', '#xrayState', '#dnsState'].map(qs).filter(Boolean);
    if (watched.length) {
      const observer = new MutationObserver(syncOverviewTopbar);
      watched.forEach(node => observer.observe(node, {subtree: true, childList: true, characterData: true, attributes: true}));
    }
    syncOverviewTopbar();
    if (!topbarSyncTimer) topbarSyncTimer = setInterval(syncOverviewTopbar, 2500);
  }

  function mountBestServerUI() {
    mountOverviewTopbar();
    const quick = qs('#quickActionsSection');
    if (!quick) return null;

    const head = quick.querySelector('.card-head');
    const title = head && head.querySelector('h2');
    const hint = head && head.querySelector('.hint');
    if (title) title.textContent = 'Лучший VPN';
    if (hint) hint.textContent = 'FreeNet сравнивает отклик, стабильность и ограниченную скорость доступных Extra-профилей.';

    const countries = quick.querySelector('.quick-layout');
    if (countries) countries.remove();

    const profilesList = qs('#profilesList');
    const profileLabel = profilesList && profilesList.querySelector('label[for="profileSearch"]');
    if (profileLabel) profileLabel.textContent = 'Ручной выбор Extra-профиля';

    let card = qs('#bestServerCard');
    if (!card) {
      card = document.createElement('div');
      card.id = 'bestServerCard';
      card.className = 'selected-profile';
      card.style.marginTop = '4px';

      const name = document.createElement('strong');
      name.id = 'bestServerName';
      name.textContent = 'Ищем лучший VPN…';
      const endpoint = document.createElement('span');
      endpoint.id = 'bestServerEndpoint';
      endpoint.className = 'selected-endpoint';
      endpoint.textContent = 'Проверяем доступные Extra-профили без переключения текущего VPN.';
      const metrics = document.createElement('span');
      metrics.id = 'bestServerMetrics';
      metrics.className = 'selected-note';
      metrics.textContent = 'MUTATION: NONE';
      card.appendChild(name);
      card.appendChild(endpoint);
      card.appendChild(metrics);

      const actions = document.createElement('div');
      actions.id = 'bestServerActions';
      actions.className = 'action-row';
      actions.style.marginTop = '10px';
      const apply = document.createElement('button');
      apply.id = 'bestServerApply';
      apply.type = 'button';
      apply.className = 'btn primary';
      apply.disabled = true;
      apply.textContent = 'Переключиться на лучший';
      const refresh = document.createElement('button');
      refresh.id = 'bestServerRefresh';
      refresh.type = 'button';
      refresh.className = 'btn secondary';
      refresh.textContent = 'Проверить заново';
      actions.appendChild(apply);
      actions.appendChild(refresh);

      const stats = document.createElement('div');
      stats.id = 'bestServerStats';
      stats.className = 'hint';
      stats.textContent = 'Read-only оценка: live VPN, ISP, DNS и routing не изменяются.';

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
      refresh.addEventListener('click', () => scanBestServer(true));
    }

    const routine = qs('#updateBtn') && qs('#updateBtn').closest('.action-row');
    const guard = qs('#quickNetworkGuard');
    if (routine && !qs('#bestServerAdvanced')) {
      const details = document.createElement('details');
      details.id = 'bestServerAdvanced';
      details.style.marginTop = '10px';
      const summary = document.createElement('summary');
      summary.className = 'hint';
      summary.style.cursor = 'pointer';
      summary.textContent = 'Дополнительные VPN-действия';
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
    const apply = qs('#bestServerApply');
    if (refresh) {
      refresh.disabled = busy || applyBusy;
      refresh.textContent = busy ? 'Проверяем…' : 'Проверить заново';
    }
    if (apply) apply.disabled = busy || applyBusy || !recommendation || recommendation.current;
  }

  function confidenceLabel(value) {
    if (value === 'high') return 'высокая';
    if (value === 'medium') return 'средняя';
    return 'не определена';
  }

  function formatMbps(value) {
    const number = Number(value || 0);
    if (!Number.isFinite(number) || number <= 0) return 'скорость —';
    return `скорость ${number.toFixed(number >= 100 ? 0 : 1)} Мбит/с`;
  }

  function renderBestServer(data) {
    recommendation = data && data.available ? data.recommendation : null;
    const name = qs('#bestServerName');
    const endpoint = qs('#bestServerEndpoint');
    const metrics = qs('#bestServerMetrics');
    const stats = qs('#bestServerStats');
    const apply = qs('#bestServerApply');

    if (!data || !data.success) {
      setText(name, 'Рекомендация недоступна');
      setText(endpoint, 'FreeNet не получил достоверный результат. Текущий VPN не изменён.');
      setText(metrics, 'Повторного переключения или догадки нет.');
      if (stats) stats.textContent = 'Можно запустить read-only проверку заново.';
      if (apply) apply.disabled = true;
      return;
    }

    const scanned = Number(data.profiles_scanned || 0);
    const total = Number(data.profiles_total || scanned);
    const deep = Array.isArray(data.candidates) ? data.candidates.filter(candidate => candidate && candidate.available).length : 0;
    if (!recommendation) {
      setText(name, 'Нет достоверно лучшего профиля');
      setText(endpoint, 'Кандидаты не прошли полную VPN quality-проверку.');
      setText(metrics, 'Текущий VPN сохранён без изменений. MUTATION: NONE.');
      if (stats) stats.textContent = `Проверено профилей: ${scanned} из ${total}.`;
      if (apply) apply.disabled = true;
      return;
    }

    const prefix = recommendation.current ? 'Текущий VPN уже лучший: ' : 'Рекомендуем: ';
    setText(name, prefix + (recommendation.name || 'Extra-профиль'));
    setText(endpoint, recommendation.endpoint || 'endpoint не указан');
    const http = recommendation.application_rtt_ms ? `HTTP-отклик ${recommendation.application_rtt_ms} мс` : 'HTTP-отклик —';
    const tcp = recommendation.tcp_rtt_ms ? `TCP ${recommendation.tcp_rtt_ms} мс` : 'TCP —';
    const jitter = Number.isFinite(Number(recommendation.jitter_ms)) ? `jitter ${recommendation.jitter_ms} мс` : 'jitter —';
    const speed = formatMbps(recommendation.download_mbps);
    setText(metrics, `${speed} · ${http} · ${tcp} · ${jitter} · уверенность ${confidenceLabel(recommendation.confidence)}`);
    if (stats) {
      stats.textContent = `${data.message || 'Рекомендация готова.'} TCP-проверка: ${scanned}/${total}; глубокая VPN-проверка: ${deep}. Scan: MUTATION NONE.`;
    }
    if (apply) {
      apply.disabled = recommendation.current || scanBusy || applyBusy;
      apply.textContent = recommendation.current ? 'Уже используется лучший' : 'Переключиться на лучший';
    }
  }

  async function scanBestServer(force) {
    if (scanBusy || applyBusy) return;
    mountBestServerUI();
    setBusy(true);
    setText(qs('#bestServerName'), 'Ищем лучший VPN…');
    setText(qs('#bestServerEndpoint'), 'Для каждого endpoint делаем несколько TCP-замеров; shortlist проверяем реальным Xray, HTTP и ограниченной загрузкой.');
    setText(qs('#bestServerMetrics'), 'Live VPN не переключается. MUTATION: NONE.');
    try {
      const response = await fetch('/api/vpn/best' + (force ? '?refresh=1' : ''), {cache: 'no-store'});
      if (response.status === 401) {
        if (authRetries++ < 20) setTimeout(() => scanBestServer(false), 3000);
        return;
      }
      const body = await response.json().catch(() => null);
      if (!response.ok || !body || !body.success) {
        renderBestServer(null);
        return;
      }
      authRetries = 0;
      renderBestServer(body);
    } catch (_) {
      renderBestServer(null);
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
    const refresh = qs('#bestServerRefresh');
    if (apply) {
      apply.disabled = true;
      apply.textContent = 'Переключаем…';
    }
    if (refresh) refresh.disabled = true;
    setText(qs('#bestServerName'), 'Переключаемся на рекомендованный VPN…');
    setText(qs('#bestServerMetrics'), 'Используется transactional provider apply; после операции проверяем фактический endpoint.');

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
        setText(qs('#bestServerEndpoint'), expectedEndpoint);
        setText(qs('#bestServerMetrics'), (body && (body.primary_error || body.error)) || 'Операция завершилась ошибкой; автоматического повтора нет.');
        return;
      }

      const status = await waitForEndpoint(expectedEndpoint);
      if (!status) {
        setText(qs('#bestServerName'), 'Требуется проверка фактического состояния');
        setText(qs('#bestServerEndpoint'), expectedEndpoint);
        setText(qs('#bestServerMetrics'), 'Apply завершён, но endpoint ещё не подтверждён. Blind retry не выполняется.');
        return;
      }
      setText(qs('#bestServerName'), 'Подключено: ' + (status.country || recommendation.name || 'VPN') + (status.city ? ' · ' + status.city : ''));
      setText(qs('#bestServerEndpoint'), status.endpoint || expectedEndpoint);
      setText(qs('#bestServerMetrics'), 'Фактический endpoint подтверждён. Обновляем рекомендацию…');
      recommendation = null;
      syncOverviewTopbar();
      await scanBestServer(true);
    } catch (_) {
      setText(qs('#bestServerName'), 'Связь прервалась во время переключения');
      setText(qs('#bestServerEndpoint'), expectedEndpoint);
      setText(qs('#bestServerMetrics'), 'Operation Coordinator сначала reconciles фактический результат; второй mutation автоматически не запускается.');
    } finally {
      applyBusy = false;
      if (refresh) refresh.disabled = false;
      if (apply) {
        apply.textContent = recommendation && recommendation.current ? 'Уже используется лучший' : 'Переключиться на лучший';
        apply.disabled = !recommendation || recommendation.current || scanBusy;
      }
    }
  }

  function start() {
    mountOverviewTopbar();
    if (!mountBestServerUI()) return;
    setTimeout(() => scanBestServer(false), 900);
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', start, {once: true});
  } else {
    start();
  }
})();
