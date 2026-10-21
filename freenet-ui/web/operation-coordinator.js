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
        const profileID = body.profile_id.trim();
        const token = typeof body.selection_token === 'string' ? body.selection_token.trim() : '';
        let target = token ? profileID + '@' + token : profileID;
        if (body.manual_override === true) {
          target += '@manual:' + (typeof body.expected_endpoint === 'string' ? body.expected_endpoint : '');
        }
        return {kind: 'provider', target, profileID, startedAt: Date.now()};
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
        success: true, applied: true, operation: 'provider', profile_id: meta.profileID || meta.target,
        operation_id: op.id, rollback_state: 'NOT_NEEDED', message: op.message || 'VPN-профиль применён и проверен'
      });
    }
    if (op.state === 'failed' && op.result === 'FAIL') {
      // Operation state has no structured apply/rollback receipt after a lost response.
      return jsonResponse(502, {
        success: false,
        result_unknown: meta.kind === 'provider' ? true : undefined,
        operation: meta.kind === 'provider' ? 'provider' : undefined,
        profile_id: meta.kind === 'provider' ? (meta.profileID || meta.target) : undefined,
        action: meta.kind === 'quick' ? meta.target : undefined,
        operation_id: op.id,
        error: op.error || 'VPN-операция завершилась ошибкой'
      });
    }
    return null;
  }

  async function reconcile(meta) {
    const deadline = Date.now() + (meta.kind === 'provider' ? 360000 : meta.kind === 'refresh' ? 540000 : 120000);
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
  let selectionToken = '';
  let currentQuality = null;
  let currentCheckedAt = '';
  let currentResultIdentity = '';
  let currentResultSource = '';
  let currentDisplayThroughput = null;
  let currentStatus = null;
  let currentGeneration = 0;
  let automaticRefreshKey = '';
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

  

  function dnsLabel(status) { return status && status.dns_mode === 'xkeen' ? 'Раздельный' : 'Прямой'; }
  function healthState(status) {
    if (!status) return {healthy: false, label: 'Проверяем состояние'};
    if (!status.xray_online) return {healthy: false, label: 'VPN не работает'};
    if (status.dns_mode === 'xkeen' && !status.dns_out_present) return {healthy: false, label: 'DNS требует внимания'};
    return {healthy: true, label: status.dns_mode === 'xkeen' ? 'VPN + DNS OK' : 'VPN OK · DNS: Прямой'};
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
    ;
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
      summary.innerHTML = '<div class="overview-approved-fact"><span>DNS</span><strong id="topDNSValue">—</strong></div>';
      topbar.insertBefore(summary, actions);
    }
    syncOverviewTopbar();
    if (!topbarSyncTimer) topbarSyncTimer = setInterval(syncOverviewTopbar, 2500);
  }

  function renderOverviewTopbarFromStatus(status) {
    if (!status) return;
    setText(qs('#topDNSValue'), dnsLabel(status));
    const state = healthState(status);
    const dot = qs('#topDot');
    if (dot) dot.className = 'dot ' + (state.healthy ? 'ok' : 'bad');
    setText(qs('#topStatus'), state.label);
    renderCurrentIdentity(status);
    renderCurrentHealth(currentQuality, status);
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
      document.dispatchEvent(new CustomEvent('freenet:status-updated', {detail:status}));
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
      return Number.isFinite(n) && n > 0 ? `${n.toFixed(n >= 100 ? 0 : 1)} Мбит/с` : '—';
    }
    if (key === 'http') return candidate.application_rtt_ms ? `${candidate.application_rtt_ms} мс` : '—';
    if (key === 'vpn') return candidate.vpn_rtt_ms ? `${candidate.vpn_rtt_ms} мс` : '—';
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

  function metricPill(label, value, key, trustedSpeed, delta = null, hint = '') {
    const node = document.createElement('div');
    node.className = 'best-v4-pill' + (key === 'speed' && trustedSpeed ? ' speed' : '');
    node.dataset.metric = key;
    node.appendChild(makeIcon(key, 'metric-icon'));
    const copy = document.createElement('div'); copy.className = 'metric-copy';
    const name = document.createElement('span'); name.className = 'metric-label'; name.textContent = label;
    if (hint) { node.title = hint; name.title = hint; }
    const valueLine = document.createElement('div'); valueLine.className = 'metric-value-line';
    const number = document.createElement('b'); number.textContent = value; valueLine.appendChild(number);
    if (delta && delta.text) {
      const note = document.createElement('span'); note.className = `metric-delta${delta.tone ? ' ' + delta.tone : ''}`; note.textContent = delta.text; valueLine.appendChild(note);
    }
    copy.append(name, valueLine); node.appendChild(copy);
    return node;
  }

  function renderMetrics(root, candidate, baseline = null, displayThroughput = undefined) {
    if (!root) return;
    root.textContent = '';
    const strictSpeed = Number(candidate?.download_mbps || 0) > 0 && candidate?.throughput_source === 'strict_aggregate';
    const speed = displayThroughput === undefined ? candidate : displayThroughput;
    root.appendChild(metricPill('Скорость VPN', metric(speed, 'speed'), 'speed', strictSpeed && candidate?.eligible === true && !displayThroughput?.historical, null, 'Измеренная скорость подключения через VPN.'));
    root.appendChild(metricPill('Отклик сайтов', metric(candidate, 'http'), 'http', false, httpDelta(candidate, baseline), 'Медианный HTTPS-отклик реальных сайтов через VPN. Чем меньше, тем отзывчивее веб и приложения.'));
    root.appendChild(metricPill('VPN-пинг', metric(candidate, 'vpn'), 'tcp', false, null, 'Базовая задержка рабочего VPN-тракта: фиксированный HTTPS RTT через VPN без DNS. Это не ICMP-пинг непосредственно до VPN-сервера.'));
    root.appendChild(metricPill('Стабильность', metric(candidate, 'jitter'), 'jitter', false, null, 'Разброс серии измерений отклика (джиттер/spread). Чем меньше, тем ровнее соединение.'));
  }

  function currentCandidate(data) {
    return Array.isArray(data && data.candidates) ? data.candidates.find(item => item && item.current) || null : null;
  }

  function qualityIdentity(value) {
    const identity = String(value || '').trim().toLowerCase();
    return /^[a-f0-9]{64}$/.test(identity) ? identity : '';
  }

  function qualityStamp(value) {
    if (typeof value !== 'string' || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(value)) return null;
    const calendar = new Date(value.slice(0, 19) + 'Z');
    if (!Number.isFinite(calendar.getTime()) || calendar.toISOString().slice(0, 19) !== value.slice(0, 19)) return null;
    const ms = Date.parse(value);
    return Number.isFinite(ms) ? {value, ms} : null;
  }

  function currentStatusKey(status = currentStatus) {
    return status ? qualityIdentity(status.current_identity) || String(status.endpoint || '').trim() : '';
  }

  function resetCurrentQuality() {
    currentQuality = null;
    currentCheckedAt = '';
    currentResultIdentity = '';
    currentResultSource = '';
    currentDisplayThroughput = null;
    automaticRefreshKey = '';
  }

  function renderCurrentIdentity(status) {
    if (!status) return;
    const nextStatus = {endpoint:String(status.endpoint || '').trim(), current_identity:qualityIdentity(status.current_identity)};
    const changed = currentStatus && (currentStatus.endpoint !== nextStatus.endpoint || currentStatusKey() !== currentStatusKey(nextStatus));
    currentStatus = nextStatus;
    if (changed) currentGeneration++;
    setText(qs('#bestCurrentName'), statusDisplayName(status));
    setText(qs('#bestCurrentEndpoint'), status.endpoint || '—');
    const flag = qs('#bestCurrentFlag');
    if (flag) {
      const currentCode = String(status.country_code || '').trim().toLowerCase() || profileCode({name: status.profile_label || ''});
      flag.className = `flag-icon ${flagClass(currentCode)}`;
      flag.hidden = !currentCode;
    }
    if (changed || (currentQuality && (currentQuality.endpoint !== nextStatus.endpoint || (nextStatus.current_identity && currentResultIdentity !== nextStatus.current_identity)))) {
      resetCurrentQuality();
      clearAlternatives('Текущий сервер изменился. Подберите варианты заново.');
      renderMetrics(qs('#bestCurrentMetrics'), null);
      renderCurrentHealth(null, status);
      setBusy(scanMode);
    }
  }

  function latencyOnlyWarning(candidate) {
    const reasons = Array.isArray(candidate && candidate.rejections) ? candidate.rejections.map(value => String(value || '').trim()).filter(Boolean) : [];
    return !!(candidate && candidate.tested && candidate.available && candidate.eligible !== true &&
      reasons.length === 1 && /^Отклик сайтов выше \d+ мс$/i.test(reasons[0]));
  }

  function liveVPNOnline(status = null) {
    if (status && typeof status.xray_online === 'boolean') return status.xray_online;
    try { if (typeof lastStatus !== 'undefined' && lastStatus && typeof lastStatus.xray_online === 'boolean') return lastStatus.xray_online; } catch (_) {}
    return null;
  }

  function renderCurrentHealth(candidate, status = null) {
    const box = qs('#bestCurrentHealth');
    if (!box) return;
    if (liveVPNOnline(status) === false) {
      box.className = 'current-health offline';
      box.textContent = 'VPN сейчас не подключен.';
      return;
    }
    if (!candidate) {
      box.className = 'current-health neutral';
      box.textContent = 'Проверка подключения ещё не выполнялась.';
      return;
    }
    if (candidate.tested && candidate.available === false) {
      box.className = 'current-health offline';
      box.textContent = 'Интернет через VPN недоступен.';
    } else if (candidate.available === true) {
      box.className = 'current-health';
      box.textContent = 'VPN работает.';
    } else {
      box.className = 'current-health neutral';
      box.textContent = 'Ожидаем проверку подключения.';
    }
  }

  function displayThroughput(data, candidate, stamp) {
    const display = data && data.display_throughput;
    if (display && display.throughput_source === 'strict_aggregate' && typeof display.historical === 'boolean' &&
        Number.isFinite(display.download_mbps) && display.download_mbps > 0 && qualityStamp(display.measured_at)) {
      return {download_mbps:display.download_mbps, measured_at:display.measured_at, historical:display.historical, throughput_source:'strict_aggregate'};
    }
    // Older responses can still display their own measured value and timestamp.
    if (candidate && candidate.throughput_source === 'strict_aggregate' && Number.isFinite(candidate.download_mbps) && candidate.download_mbps > 0 && stamp) {
      return {download_mbps:candidate.download_mbps, measured_at:stamp.value, historical:false, throughput_source:'strict_aggregate'};
    }
    return null;
  }

  function renderCurrentQualityCaption() {
    const node = qs('#bestCurrentQuality');
    if (!node) return;
    const formatTime = (value, withDate = false) => {
      const stamp = qualityStamp(value);
      return stamp ? new Date(stamp.ms).toLocaleString('ru-RU', {hour:'2-digit', minute:'2-digit', ...(withDate ? {day:'2-digit', month:'2-digit'} : {})}) : '';
    };
    node.dataset.checkedAt = currentCheckedAt;
    node.dataset.speedMeasuredAt = currentDisplayThroughput ? currentDisplayThroughput.measured_at : '';
    const updating = scanMode === 'current' || (automaticRefreshKey && automaticRefreshKey === currentStatusKey());
    const parts = [updating ? 'Обновляем…' : currentQuality ? 'Последняя проверка: ' + (formatTime(currentCheckedAt) || 'время неизвестно') : 'Скорость ещё не измерена'];
    if (currentDisplayThroughput) {
      parts.push(metric(currentDisplayThroughput, 'speed'));
      if (updating || currentDisplayThroughput.historical || currentDisplayThroughput.measured_at !== currentCheckedAt) {
        parts.push('замер ' + formatTime(currentDisplayThroughput.measured_at, true));
      }
    }
    setText(node, parts.join(' · '));
  }

  function currentResultMatches(data, candidate, requestContext = null, source = 'manual') {
    const endpoint = String(candidate && candidate.endpoint || '').trim();
    const liveEndpoint = currentStatus ? currentStatus.endpoint : String(qs('#bestCurrentEndpoint')?.textContent || '').trim();
    if (!endpoint || (liveEndpoint && liveEndpoint !== '—' && endpoint !== liveEndpoint)) return false;
    const identity = qualityIdentity(data && data.current_identity);
    if (source !== 'comparison' && currentStatus?.current_identity && identity !== currentStatus.current_identity) return false;
    if (requestContext && (requestContext.generation !== currentGeneration || (requestContext.endpoint && requestContext.endpoint !== endpoint) || (requestContext.identity && requestContext.identity !== identity))) return false;
    return true;
  }

  function renderCurrentQuality(data, source = 'manual', requestContext = null) {
    const candidate = currentCandidate(data);
    if (candidate) {
      if (!currentResultMatches(data, candidate, requestContext, source)) return false;
      const stamp = source === 'comparison' ? null : qualityStamp(data && data.scanned_at);
      const previous = qualityStamp(currentCheckedAt);
      if (source === 'cache' && (scanMode === 'current' || applyBusy || !stamp || (previous && stamp.ms <= previous.ms) || (!previous && currentResultSource === 'manual'))) return false;
      if (source === 'comparison' && currentQuality) return false;
      if (source === 'manual' && previous && (!stamp || stamp.ms < previous.ms)) return false;
      currentQuality = Object.assign({}, candidate, {current:true});
      currentResultIdentity = qualityIdentity(data && data.current_identity) || (source === 'comparison' ? currentStatus?.current_identity || '' : '');
      currentResultSource = source;
      currentCheckedAt = stamp ? stamp.value : '';
      currentDisplayThroughput = displayThroughput(data, candidate, stamp);
    } else if (requestContext && requestContext.generation !== currentGeneration) {
      return false;
    }
    const shown = currentQuality;
    if (candidate) {
      setText(qs('#bestCurrentName'), profileDisplayName(candidate, 'Текущий VPN'));
      setText(qs('#bestCurrentEndpoint'), candidate.endpoint || '—');
      const flag = qs('#bestCurrentFlag');
      if (flag) {
        flag.className = `flag-icon ${flagClass(profileCode(candidate))}`;
        flag.hidden = !profileCode(candidate);
      }
    }
    renderMetrics(qs('#bestCurrentMetrics'), shown, null, currentDisplayThroughput);
    renderCurrentHealth(shown);
    renderCurrentQualityCaption();
    if (shown) {
      setText(qs('#bestServerStatus'), candidate ? 'Проверка текущего VPN завершена.' : 'Проверка текущего VPN завершена. Показан последний подтверждённый замер.');
    } else {
      setText(qs('#bestServerStatus'), 'Текущий профиль не удалось определить. Другие серверы не проверялись.');
    }
    return !!candidate;
  }

  function comparableSpeed(candidate) {
    if (!candidate || candidate.throughput_source !== 'strict_aggregate') return 0;
    return Number(candidate.download_mbps || 0);
  }

  function recommendationReason(data, candidate) {
    if (!candidate) return [];
    const current = currentCandidate(data) || currentQuality;
    const parts = [];
    const speed = comparableSpeed(candidate), currentSpeed = comparableSpeed(current);
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
    if (tone === 'bad' || tone === 'warn') chip.appendChild(makeIcon('alert', 'chip-icon'));
    const label = document.createElement('span'); label.textContent = String(text).replace(/[.]$/, ''); chip.appendChild(label);
    root.appendChild(chip);
  }

  function friendlyRejection(text) {
    const value = String(text || '').trim();
    const lower = value.toLowerCase();
    if (lower.includes('timeout') || lower.includes('тайма')) return 'Таймауты при замере';
    if (lower.includes('стабил') || lower.includes('jitter') || lower.includes('колеб')) return 'Нестабильное соединение';
    if ((lower.includes('speedtest') || lower.includes('throughput')) && lower.includes('0/')) return 'Скорость не измерена';
    if (lower.includes('скорость') && lower.includes('не измер')) return 'Скорость не измерена';
    return value.length > 42 ? value.slice(0, 39) + '…' : value;
  }

  function clearAlternatives(message) {
    alternatives = [];
    recommendation = null;
    selectionToken = '';
    const box = qs('#bestServerResult');
    if (box) { box.textContent = ''; box.classList.remove('show'); }
    const empty = qs('#bestServerEmpty');
    if (empty) { empty.hidden = false; empty.textContent = message || 'Подберите серверы, чтобы увидеть проверенные варианты.'; }
    const status = qs('#bestServerStatus'); if (status) status.classList.remove('summary');
  }

  function comparisonCandidates(data, current) {
    const seen = new Set();
    const pool = (Array.isArray(data && data.candidates) ? data.candidates : []).filter(candidate => {
      if (!candidate || !candidate.tested || candidate.current || isRussianProfile(candidate) || !candidate.id || !candidate.endpoint || seen.has(candidate.id)) return false;
      if (!candidate.available && !candidate.reachable) return false;
      seen.add(candidate.id);
      return true;
    });
    // Final Best Server cards are recommendations, not a diagnostic dump.
    // Never fill a missing Top-3 slot with a rejected/near-miss profile.
    return pool.filter(candidate => candidate.eligible === true).slice(0, 3);
  }

  function sameCandidate(a, b) {
    if (!a || !b) return false;
    if (a.id && b.id) return a.id === b.id;
    return !!(a.endpoint && b.endpoint && a.endpoint === b.endpoint);
  }

  function stateForCandidate(data, candidate) {
    if (!candidate.eligible && latencyOnlyWarning(candidate)) return {kind: 'warning', label: 'Высокий отклик', icon: 'alert'};
    if (!candidate.eligible) return {kind: 'rejected', label: 'Не прошёл проверку', icon: 'alert'};
    if (data && data.recommendation && !data.recommendation.current && sameCandidate(data.recommendation, candidate)) {
      return {kind: 'best', label: 'Лучший из альтернатив', icon: 'trophy'};
    }
    return {kind: 'comparison', label: 'Для сравнения', icon: 'compare'};
  }

  function renderSummaryStatus(data, states) {
    const status = qs('#bestServerStatus');
    if (!status) return;
    const best = states.filter(state => state.kind === 'best').length;
    const comparisons = states.filter(state => state.kind === 'comparison').length;
    const warnings = states.filter(state => state.kind === 'warning').length;
    const rejected = states.filter(state => state.kind === 'rejected').length;
    status.textContent = ''; status.classList.add('summary');
    status.appendChild(makeIcon('info', 'status-icon'));
    const copy = document.createElement('div'); copy.className = 'summary-copy';
    const main = document.createElement('div'); main.className = 'summary-main';
    const pieces = [];
    if (best) pieces.push(`${best} лучший`);
    if (comparisons) pieces.push(`${comparisons} для сравнения`);
    if (warnings) pieces.push(`${warnings} выше целевого отклика`);
    if (rejected) pieces.push(`${rejected} не прошёл проверку`);
    const deepTotal = Number(data.deep_total || 0);
    const deepChecked = Number(data.deep_checked || 0);
    const eligibleTarget = Number(data.eligible_target || 3);
    const eligibleFound = Number(data.eligible_found || states.length);
    main.textContent = deepTotal > 0
      ? `Из ${data.profiles_scanned || 0} профилей в shortlist попало ${deepTotal}. Глубоко проверено ${deepChecked} из ${deepTotal}. Найдено ${eligibleFound} из ${eligibleTarget} подходящих альтернатив: ${pieces.join(', ') || 'нет подходящих'}.`
      : `Проверено профилей: ${data.profiles_scanned || 0}. Показано ${states.length} вариант${states.length === 1 ? '' : states.length < 5 ? 'а' : 'ов'}: ${pieces.join(', ') || 'нет подходящих'}.`;
    copy.appendChild(main);
    if (data.recommendation && data.recommendation.current) {
      const sub = document.createElement('div'); sub.className = 'summary-sub'; sub.textContent = 'Текущий VPN остаётся предпочтительным.'; copy.appendChild(sub);
    }
    status.appendChild(copy);
  }

  function renderBestResult(data, currentContext = null) {
    clearAlternatives();
    selectionToken = String(data && data.selection_token || '').trim();
    const current = currentCandidate(data);
    if (current) renderCurrentQuality({candidates:[current]}, 'comparison', currentContext);
    const baseline = current || currentQuality;
    alternatives = comparisonCandidates(data, current);
    const box = qs('#bestServerResult');
    if (!alternatives.length) {
      clearAlternatives('Проверенных подходящих VPN сейчас нет. Текущий VPN сохранён.');
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
        appendDetailChip(reason, `Скорость ${candidate.media_samples || 0}/4`, 'ok');
        appendDetailChip(reason, `Сервисы ${candidate.service_ok || 0}/${candidate.service_total || 0}`, 'ok');
        appendDetailChip(reason, 'Доступен', 'ok');
      } else if (state.kind === 'warning') {
        appendDetailChip(reason, `Скорость ${candidate.media_samples || 0}/4`, 'ok');
        appendDetailChip(reason, `Сервисы ${candidate.service_ok || 0}/${candidate.service_total || 0}`, candidate.service_ok === candidate.service_total && candidate.service_total > 0 ? 'ok' : 'bad');
        const diagnostics = Array.isArray(candidate.rejections) && candidate.rejections.length ? candidate.rejections.slice(0, 2) : ['Отклик выше целевого порога AUTO VPN'];
        diagnostics.forEach(text => appendDetailChip(reason, friendlyRejection(text), 'warn'));
        const originals = document.createElement('span'); originals.className = 'fn-sr-only rejection-originals'; originals.textContent = (candidate.rejections || []).join(' · '); reason.appendChild(originals);
      } else {
        appendDetailChip(reason, `Скорость ${candidate.media_samples || 0}/4`, 'bad');
        appendDetailChip(reason, `Сервисы ${candidate.service_ok || 0}/${candidate.service_total || 0}`, candidate.service_ok === candidate.service_total && candidate.service_total > 0 ? 'ok' : 'bad');
        const diagnostics = Array.isArray(candidate.rejections) && candidate.rejections.length ? candidate.rejections.slice(0, 2) : ['Недостаточно подтверждённых данных'];
        diagnostics.forEach(text => appendDetailChip(reason, friendlyRejection(text), 'bad'));
        const originals = document.createElement('span'); originals.className = 'fn-sr-only rejection-originals'; originals.textContent = (candidate.rejections || []).join(' · '); reason.appendChild(originals);
      }
      row.append(head, metrics, reason);
      if (!candidate.eligible) {
        const note = document.createElement('p'); note.className = 'vpn-rejected-note';
        note.textContent = state.kind === 'warning'
          ? 'Сервер доступен, но его отклик выше целевого порога AUTO VPN. Автоматически FreeNet на него не переключится.'
          : 'Сервер не прошёл все проверки и не рекомендуется для переключения.';
        row.appendChild(note);
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
    const alternativesHint = qs('.vpn-section-head .hint'); if (alternativesHint) alternativesHint.textContent = 'Топ-3 альтернативы на основе реальных измерений';
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
      const help = document.createElement('p'); help.id = 'bestCurrentHelp'; help.className = 'current-help'; help.textContent = 'Скорость обновляется при проверке подключения.';
      currentPanel.appendChild(help);
    }
    const measureNote = qs('.vpn-measure-note'); if (measureNote) measureNote.remove();
    const status = qs('#bestServerStatus'), alternativesPanel = qs('.vpn-alternatives-panel');
    if (status && alternativesPanel && status.parentNode !== alternativesPanel) alternativesPanel.appendChild(status);
    const guard = qs('#quickNetworkGuard');
    let manual = qs('#bestServerAdvanced');
    if (profilesList && !manual) {
      manual = document.createElement('section');
      manual.id = 'bestServerAdvanced';
      qs('#bestServerShell')?.appendChild(manual);
    }
    if (manual) manual.classList.remove('fn-topbar-vpn-picker');
    if (manual && profilesList && profilesList.parentNode !== manual) manual.appendChild(profilesList);
    const exact = qs('#exactConnectRow'); if (manual && exact && exact.parentNode !== manual) manual.appendChild(exact);
    if (manual && guard && guard.parentNode !== manual) manual.appendChild(guard);
    const searchField = profilesList && profilesList.querySelector('.field');
    if (searchField && !searchField.querySelector('.manual-search-icon')) searchField.prepend(makeIcon('search', 'manual-search-icon'));
    const currentMetrics = qs('#bestCurrentMetrics');
    if (currentQuality || !currentMetrics || currentMetrics.children.length === 0) {
      renderMetrics(currentMetrics, currentQuality, null, currentDisplayThroughput);
      renderCurrentHealth(currentQuality);
    }
    try { if (typeof lastStatus !== 'undefined' && lastStatus) renderCurrentIdentity(lastStatus); } catch (_) {}
    return qs('#bestServerShell');
  }

  function setBusy(mode) {
    scanMode = mode;
    scanBusy = !!mode;
    const busy = scanBusy || applyBusy || !!automaticRefreshKey;
    const blocked = busy || externalBusy;
    const current = qs('#bestServerCheckCurrent'), best = qs('#bestServerRefresh');
    if (current) { current.disabled = blocked; current.textContent = mode === 'current' ? 'Проверяем текущий…' : 'Проверить текущий VPN'; }
    if (best) { best.disabled = blocked; setButtonLabel(best, mode === 'best' ? 'Подбираем…' : 'Подобрать серверы', 'search'); }
    document.querySelectorAll('.vpn-option-apply,.vpn-option-retry').forEach(button => { button.disabled = blocked; });
    const manual = qs('#bestServerAdvanced'); if (manual) manual.inert = blocked;
    const maintenance = qs('.vpn-current-panel>.action-row'); if (maintenance) maintenance.inert = blocked;
    const status = qs('#bestServerStatus'); if (status) status.classList.toggle('busy', busy);
    qs('#bestServerShell')?.setAttribute('aria-busy', String(busy));
    renderCurrentQualityCaption();
  }

  function qualityJobID() {
    const cryptoAPI = globalThis.crypto;
    if (cryptoAPI && typeof cryptoAPI.randomUUID === 'function') {
      try { return cryptoAPI.randomUUID(); } catch (_) {}
    }
    const bytes = new Uint8Array(16);
    if (cryptoAPI && typeof cryptoAPI.getRandomValues === 'function') {
      cryptoAPI.getRandomValues(bytes);
    } else {
      for (let i = 0; i < bytes.length; i++) bytes[i] = Math.floor(Math.random() * 256);
    }
    return 'q_' + Array.from(bytes, value => value.toString(16).padStart(2, '0')).join('');
  }
  window.freenetQualityJobID = qualityJobID;

  async function requestQuality(path, mode) {
    const id = qualityJobID(), started = Date.now();
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
        stage.textContent=job.stage==='quality'?`Глубоко проверяем кандидатов · проверено ${job.completed} из ${job.total} · цель 3 подходящих`:job.stage==='preflight'?`Сравниваем реальный отклик через VPN · завершено ${job.completed} из ${job.total}`:job.stage==='tcp'?'Проверяем доступность серверов…':'Получаем профили подписки…';
        if(job.stage==='preflight'&&job.total>0){progress.max=job.total;progress.value=job.completed}else progress.removeAttribute('value');
        if(Date.now()-started>370000)throw new DOMException('Quality job timeout','TimeoutError');await wait(1000);response=await readState();
      }
      return response;
    } finally {clearInterval(ticker);panel.remove();if(controls)controls.inert=wasInert;if(focused?.isConnected)focused.focus();}
  }

  async function scanCurrentVPN() {
    if (scanBusy || applyBusy || externalBusy || automaticRefreshKey) return;
    syncOverviewTopbar();
    const requestContext = {generation:currentGeneration, endpoint:currentStatus?.endpoint || '', identity:currentStatus?.current_identity || ''};
    let measured = null;
    try {
      mountBestServerUI(); setBusy('current'); setText(qs('#bestServerStatus'),'Проверяем текущий VPN… Это может занять до минуты.');
      const response=await requestQuality('/api/vpn/current-quality','current');const body=await response.json().catch(()=>null);
      if(!response.ok||!body||!body.success){setText(qs('#bestServerStatus'),(body&&body.error)||'Не удалось проверить текущий VPN.');return;}
      syncOverviewTopbar();
      if (renderCurrentQuality(body, 'manual', requestContext)) measured = body;
      else if (currentCandidate(body)) setText(qs('#bestServerStatus'), requestContext.generation !== currentGeneration ? 'Текущий VPN изменился. Можно проверить новое подключение.' : 'Показан последний замер текущего VPN.');
    } catch (_) {setText(qs('#bestServerStatus'),'Не удалось завершить проверку текущего VPN. Проверьте связь с FreeNet.');} finally {setBusy(false);}
    if (measured) document.dispatchEvent(new CustomEvent('freenet:current-quality-measured', {detail:measured}));
  }

  async function scanBestServer() {
    if (scanBusy || applyBusy || externalBusy || automaticRefreshKey) return;
    syncOverviewTopbar();
    const currentContext = {generation:currentGeneration, endpoint:currentStatus?.endpoint || ''};
    try {
      mountBestServerUI();setBusy('best');clearAlternatives('Сравниваем реальный отклик и измеряем качество…');setText(qs('#bestServerStatus'),'Сравниваем зарубежные серверы… Текущий VPN продолжает работать.');
      const response=await requestQuality('/api/vpn/best-foreign','best');const body=await response.json().catch(()=>null);
      if(!response.ok||!body||body.success!==true||!Array.isArray(body.candidates)){
        clearAlternatives('Подбор не завершён. Наличие подходящих замен пока неизвестно.');
        const detail=body&&typeof body.error==='string'?body.error:response.status===504?'Шлюз не дождался ответа FreeNet.':response.status===502?'Шлюз не получил корректный ответ FreeNet.':response.status===401?'Требуется войти в FreeNet заново.':response.ok?'Ответ FreeNet не содержит результатов проверки.':'Сервер вернул ошибку.';
        setText(qs('#bestServerStatus'),`Подбор не завершён (HTTP ${response.status}). ${detail}`);return;
      }
      syncOverviewTopbar();
      renderBestResult(body, currentContext);if(body.partial)setText(qs('#bestServerStatus'),'Проверка завершена в пределах лимита времени. Показаны только измеренные варианты; часть кандидатов не проверена.');
    } catch(error){clearAlternatives('Подбор не завершён. Наличие подходящих замен пока неизвестно.');setText(qs('#bestServerStatus'),error&&error.name==='TimeoutError'?'Подбор не завершён: превышено безопасное время ожидания ответа.':'Подбор не завершён: связь с FreeNet прервалась.');} finally {setBusy(false);}
  }

  async function waitForAppliedProvider(candidate, expectedEndpoint='') {
    const expectedName=String(candidate&&candidate.name||'').trim();
    for(let i=0;i<35;i++){
      try{
        const response=await fetch('/api/status',{cache:'no-store'});
        if(response.ok){
          const status=await response.json();
          if(status&&!status.busy&&!status.updater_busy&&status.xray_online){
            const statusName=String(status.profile_label||status.profile||'').trim();
            const endpointOK=!!expectedEndpoint&&status.endpoint===expectedEndpoint;
            const nameOK=!expectedEndpoint&&!!expectedName&&(statusName===expectedName||profileDisplayName({name:statusName},statusName)===profileDisplayName(candidate,expectedName));
            if(endpointOK||nameOK||(!expectedEndpoint&&!expectedName))return status;
          }
        }
      }catch(_){}
      await wait(850);
    }
    return null;
  }

  async function applyCandidate(candidate) {
    if(applyBusy||scanBusy||externalBusy||automaticRefreshKey||!candidate||candidate.current||!candidate.id||!candidate.eligible||isRussianProfile(candidate))return;
    if(!/^[a-f0-9]{32}$/.test(selectionToken)){
      setText(qs('#bestServerStatus'),'Результат подбора больше нельзя безопасно применить. Подберите серверы заново.');
      clearAlternatives('Результаты подбора устарели. Для переключения подберите серверы снова.');
      return;
    }
    recommendation=candidate;applyBusy=true;setBusy(false);
    const apply=Array.from(document.querySelectorAll('.vpn-option-apply')).find(button=>button.dataset.candidateId===candidate.id);if(apply){apply.disabled=true;apply.textContent='Переключаем…';}
    setText(qs('#bestServerStatus'),'Переключаем VPN и проверяем соединение…');
    try{
      const response=await fetch('/api/network-profile/apply',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({operation:'provider',profile_id:candidate.id,selection_token:selectionToken,confirm:true})});const body=await response.json().catch(()=>null);
      if(!response.ok||!body||!body.success){const detail=body&&(body.primary_error||body.error);setText(qs('#bestServerStatus'),detail||'Результат переключения не подтверждён. Не повторяйте операцию.');if(!body||body.result_unknown)recommendation=null;return;}
      const appliedEndpoint=String(body?.provider_plan?.endpoint||'').trim();
      const status=await waitForAppliedProvider(candidate,appliedEndpoint);if(!status){setText(qs('#bestServerStatus'),'Переключение ещё не подтверждено. Проверьте состояние системы перед повторной попыткой.');return;}
      recommendation=null;resetCurrentQuality();renderOverviewTopbarFromStatus(status);renderMetrics(qs('#bestCurrentMetrics'),null);renderCurrentHealth(null);renderCurrentQualityCaption();setText(qs('#bestServerStatus'),'VPN переключён. Соединение проверено.');
    }catch(_){recommendation=null;setText(qs('#bestServerStatus'),'Связь прервалась во время переключения. Результат не подтверждён — проверьте состояние системы перед повторной попыткой.');}finally{clearAlternatives('Результаты подбора израсходованы. Для нового переключения подберите серверы снова.');applyBusy=false;setBusy(false);}
  }

  async function retryCandidate(candidate, button) {
    if (scanBusy || applyBusy || externalBusy || automaticRefreshKey || !candidate || !candidate.id) return;
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
    if (scanBusy || applyBusy || externalBusy || automaticRefreshKey) return;
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
      if (body.current) renderCurrentQuality({candidates:[Object.assign({}, body.current, {current:true})]}, 'comparison');
      if (body.outcome === 'applied') {
        const expected = body.candidate && body.candidate.endpoint;
        const status = expected ? await waitForEndpoint(expected) : null;
        if (!status) {
          setText(qs('#bestServerStatus'), 'Свежий endpoint применён, но live-state ещё не подтверждён в браузере. Не повторяйте операцию.');
          return;
        }
        resetCurrentQuality();
        renderOverviewTopbarFromStatus(status);
        if (body.candidate) renderCurrentQuality({candidates:[Object.assign({}, body.candidate, {current:true})]}, 'comparison');
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

  function installCurrentQualityMemoryBridge() {
    const acceptHydration = detail => {
      const candidate = detail && detail.candidate;
      if (!candidate || candidate.current !== true || !candidate.endpoint) return;
      if (!currentStatus && detail.status) renderOverviewTopbarFromStatus(detail.status);
      renderCurrentQuality({scanned_at:detail.scanned_at || '', candidates:[candidate], current_identity:detail.current_identity, display_throughput:detail.display_throughput}, 'cache');
    };
    document.addEventListener('freenet:current-quality-display', event => acceptHydration(event && event.detail));
    document.addEventListener('freenet:status-updated', event => {
      if (event && event.detail) renderOverviewTopbarFromStatus(event.detail);
    });
    document.addEventListener('freenet:current-quality-refresh', event => {
      const detail = event && event.detail;
      if (!detail || !currentResultMatches(detail, {endpoint:detail.endpoint})) return;
      automaticRefreshKey = detail.running ? currentStatusKey() : '';
      setBusy(scanMode);
    });
    if (window.__freenetCurrentQualityHydration) queueMicrotask(() => acceptHydration(window.__freenetCurrentQualityHydration));
  }

  function start() {
    installRussianProfileFilter();patchCrossPlatformFlags();installBestServerActionDelegation();installUpdateQualityFollowup();installCurrentQualityMemoryBridge();mountOverviewTopbar();installOverviewStatusHook();mountBestServerUI();syncOverviewTopbar();
  }

  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',start,{once:true});else start();
})();

// Runtime polish pass: visual-only. Best Server selection/apply semantics above are unchanged.
(() => {
  const q = (selector, root = document) => root.querySelector(selector);
  let scheduled = false;

  function polishBestAlternative() {
    const rows = Array.from(document.querySelectorAll('#bestServerResult .vpn-option'));
    rows.forEach(row => row.classList.remove('fn-best-alternative'));
    if (!rows.length || rows.some(row => row.classList.contains('vpn-best'))) return;
    const first = rows.find(row => !row.classList.contains('vpn-rejected') && row.querySelector('.vpn-option-apply'));
    if (!first) return;
    first.classList.add('fn-best-alternative');
    const badge = first.querySelector('.vpn-state-badge');
    if (badge && badge.textContent.trim() === 'Для сравнения') badge.textContent = 'Лучший из альтернатив';
  }

  function fitCurrentMetrics() {
    document.querySelectorAll('.vpn-current-panel .best-v4-pill b').forEach(node => {
      node.classList.toggle('fn-long-value', node.textContent.trim().length >= 9);
    });
  }

  function run() {
    scheduled = false;
    polishBestAlternative();
    fitCurrentMetrics();
  }

  function schedule() {
    if (scheduled) return;
    scheduled = true;
    requestAnimationFrame(run);
  }

  function install() {
    if (q('#FreeNetFinalRuntimePolish')) return;
    document.documentElement.dataset.freenetStyleoperationcoordinatorjs2 = "1";
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
    const applicationFailed = !!q('#bestCurrentHealth')?.classList.contains('offline');
    online = online && !applicationFailed;
    badge.classList.toggle('offline', !online);
    badge.textContent = online ? '● Подключен' : applicationFailed ? '● VPN не работает' : '● Нет соединения';
  }

  function incompleteCard(row) {
    if (!row?.classList.contains('vpn-rejected')) return false;
    const text = row.textContent || '';
    const services = text.match(/Сервисы\s+(\d+)\/(\d+)/i);
    const servicesOK = !!(services && Number(services[2]) > 0 && Number(services[1]) === Number(services[2]));
    const speedIncomplete = /Скорость не измерена|(?:Speedtest|Скорость) не заверш|(?:Speedtest|Скорость)\s+[0-2]\/4/i.test(text);
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
    document.documentElement.dataset.freenetStyleoperationcoordinatorjs3 = "1";
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
    if ((document.documentElement.dataset.freenetStyleoperationcoordinatorjs4 === "1")) return;
    document.documentElement.dataset.freenetStyleoperationcoordinatorjs4 = "1";
  }
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', installV037Scale, {once:true}); else installV037Scale();
})();


// Issue #363: safe v0.3.8 visual polish without a DOM observer.
// This block is deliberately event-driven: static CSS + the current-refresh request lifecycle.
// Do not replace this with a whole-document MutationObserver.
(() => {
  const q = (selector, root = document) => root.querySelector(selector);
  const root = document.documentElement;

  function ensureRefreshResult() {
    let result = q('#fnRefreshResult');
    if (result) return result;
    const update = q('#updateBtn');
    const row = update?.closest('.action-row');
    const panel = q('.vpn-current-panel');
    if (!row || !panel || row.parentNode !== panel) return null;
    result = document.createElement('div');
    result.id = 'fnRefreshResult';
    result.setAttribute('role', 'status');
    result.setAttribute('aria-live', 'polite');
    row.insertAdjacentElement('afterend', result);
    return result;
  }

  function showRefreshResult(text, tone = '') {
    const result = ensureRefreshResult();
    if (!result) return;
    result.textContent = String(text || '').trim();
    result.className = tone ? `fn-refresh-result-${tone}` : '';
    root.classList.toggle('fn-current-refresh-visible', !!result.textContent);
  }

  function clearRefreshResult() {
    const result = q('#fnRefreshResult');
    if (result) { result.textContent = ''; result.className = ''; }
    root.classList.remove('fn-current-refresh-visible');
  }

  function refreshOutcomeMessage(body, response) {
    if (!body || typeof body !== 'object') {
      return response && response.ok
        ? 'Обновление endpoint завершено. Проверьте фактическое состояние VPN.'
        : 'Обновление endpoint не завершено. Текущий VPN не изменяйте до проверки состояния.';
    }
    if (body.message) return body.message;
    if (!response.ok || body.success !== true) return body.primary_error || body.error || 'Обновление endpoint не завершено.';
    return ({
      no_new: 'Нового endpoint для текущей локации нет. Текущий VPN сохранён.',
      current_better: 'Текущий VPN лучше. Переключение не выполнялось.',
      check_failed: 'Новый endpoint не прошёл проверку. Текущий VPN сохранён.',
      applied: 'Новый endpoint применён и проверен.'
    })[body.outcome] || 'Проверка endpoint завершена.';
  }

  function install() {
    if (q('#FreeNetV038SafePolish')) return;
    document.documentElement.dataset.freenetStyleoperationcoordinatorjs5 = "1";
    ensureRefreshResult();

    document.addEventListener('click', event => {
      const target = event.target?.closest?.('#updateBtn,#bestServerCheckCurrent,#bestServerRefresh,.vpn-option-apply,.vpn-option-retry');
      if (!target) return;
      if (target.id === 'updateBtn') showRefreshResult('Получаем новый endpoint и сравниваем его с текущим VPN…');
      else clearRefreshResult();
    }, true);

    const safeBaseFetch = window.fetch.bind(window);
    window.fetch = async function(input, init) {
      const url = typeof input === 'string' ? input : (input && input.url) || '';
      const method = String((init && init.method) || (input && input.method) || 'GET').toUpperCase();
      const currentRefresh = method === 'POST' && url.split('?')[0] === '/api/vpn/current-refresh';
      if (!currentRefresh) return safeBaseFetch(input, init);
      try {
        const response = await safeBaseFetch(input, init);
        let body = null;
        try { body = await response.clone().json(); } catch (_) {}
        const message = refreshOutcomeMessage(body, response);
        const ok = response.ok && body && body.success === true;
        showRefreshResult(message, ok ? 'ok' : 'bad');
        return response;
      } catch (error) {
        showRefreshResult('Связь прервалась во время обновления endpoint. Проверьте фактическое состояние перед повтором.', 'bad');
        throw error;
      }
    };
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', install, {once:true});
  else install();
})();


// Issue #559: final visual hierarchy for Overview after v0.3.78 runtime acceptance.
// Presentation-only. VPN quality, apply, recovery and operation semantics are unchanged.
(() => {
  const q = (selector, root = document) => root.querySelector(selector);

  function install() {
    if (q('#FreeNetIssue559VisualPolish')) return;
    document.documentElement.dataset.freenetStyleoperationcoordinatorjs6 = "1";
    // The current identity renderer owns the flag, including label fallback.
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', install, {once:true});
  else install();
})();


// Issue #568: final Overview hierarchy polish.
// Presentation-only: current-VPN identity, quality and action semantics are unchanged.
(() => {
  const q = (selector, root = document) => root.querySelector(selector);
  function install() {
    if (q('#FreeNetIssue568OverviewPolish')) return;
    document.documentElement.dataset.freenetStyleoperationcoordinatorjs7 = "1";
  }
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', install, {once:true});
  else install();
})();
