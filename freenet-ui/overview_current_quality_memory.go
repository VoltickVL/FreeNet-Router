package main

const overviewCurrentQualityMemoryScript = `<script id="freenetOverviewCurrentQualityMemory">
(() => {
  const q = (selector, root = document) => root.querySelector(selector);
  const wait = ms => new Promise(resolve => setTimeout(resolve, ms));
  let hydratedEndpoint = '';
  let seedEndpoint = '';
  let seedRunning = false;

  const iconPath = {
    speed: '<path d="M12 3v12m0 0 5-5m-5 5-5-5"/><path d="M5 21h14"/>',
    http: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
    tcp: '<circle cx="12" cy="5" r="2"/><circle cx="5" cy="16" r="2"/><circle cx="19" cy="16" r="2"/><path d="M10.8 6.7 6.2 14M13.2 6.7l4.6 7.3M7 16h10"/>',
    jitter: '<path d="M3 13h3l2-6 3 11 3-13 2 8h5"/>'
  };

  function overviewActive() {
    const page = q('.page[data-page-view="overview"]');
    return !!(page && page.classList.contains('active')) || location.hash === '' || location.hash === '#overview';
  }

  function metricValue(candidate, key) {
    if (!candidate) return '—';
    if (key === 'speed') {
      const speed = Number(candidate.download_mbps || 0);
      return speed > 0 ? speed.toFixed(speed >= 100 ? 0 : 1) + ' Мбит/с' : '—';
    }
    if (key === 'http') return Number(candidate.application_rtt_ms || 0) > 0 ? candidate.application_rtt_ms + ' мс' : '—';
    if (key === 'tcp') return Number(candidate.tcp_rtt_ms || 0) > 0 ? candidate.tcp_rtt_ms + ' мс' : '—';
    if (key === 'jitter') return Number.isFinite(Number(candidate.jitter_ms)) ? candidate.jitter_ms + ' мс' : '—';
    return '—';
  }

  function metricPill(label, candidate, key) {
    const node = document.createElement('div');
    node.className = 'best-v4-pill' + (key === 'speed' && candidate && candidate.eligible ? ' speed' : '');
    node.dataset.metric = key;
    const icon = document.createElement('span');
    icon.className = 'fn-icon metric-icon';
    icon.setAttribute('aria-hidden', 'true');
    icon.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round">' + iconPath[key] + '</svg>';
    const copy = document.createElement('div');
    copy.className = 'metric-copy';
    const name = document.createElement('span');
    name.className = 'metric-label';
    name.textContent = label;
    const line = document.createElement('div');
    line.className = 'metric-value-line';
    const number = document.createElement('b');
    number.textContent = metricValue(candidate, key);
    line.appendChild(number);
    copy.append(name, line);
    node.append(icon, copy);
    return node;
  }

  function renderMetrics(candidate) {
    const root = q('#bestCurrentMetrics');
    if (!root) return;
    root.textContent = '';
    root.append(
      metricPill('Скорость VPN', candidate, 'speed'),
      metricPill('Отклик сайтов', candidate, 'http'),
      metricPill('Связь с сервером', candidate, 'tcp'),
      metricPill('Стабильность', candidate, 'jitter')
    );
  }

  function ageText(scannedAt) {
    const at = Date.parse(scannedAt || '');
    if (!Number.isFinite(at)) return 'время неизвестно';
    const age = Math.max(0, Date.now() - at);
    if (age < 60 * 1000) return 'только что';
    if (age < 60 * 60 * 1000) return Math.max(1, Math.round(age / 60000)) + ' мин назад';
    if (age < 24 * 60 * 60 * 1000) return Math.max(1, Math.round(age / 3600000)) + ' ч назад';
    return new Date(at).toLocaleString('ru-RU', {day:'2-digit', month:'2-digit', hour:'2-digit', minute:'2-digit'});
  }

  function currentCandidate(data) {
    return Array.isArray(data && data.candidates) ? data.candidates.find(item => item && item.current) || null : null;
  }

  function renderQuality(data) {
    const candidate = currentCandidate(data);
    if (!candidate || !candidate.endpoint) return false;
    const liveEndpoint = String((q('#bestCurrentEndpoint') || {}).textContent || '').trim();
    if (liveEndpoint && liveEndpoint !== '—' && liveEndpoint !== candidate.endpoint) return false;
    renderMetrics(candidate);
    hydratedEndpoint = candidate.endpoint;
    const quality = q('#bestCurrentQuality');
    if (quality) quality.textContent = 'Последний замер: ' + ageText(data.scanned_at);
    const health = q('#bestCurrentHealth');
    if (health) {
      health.className = candidate.eligible ? 'current-health' : 'current-health neutral';
      health.textContent = candidate.eligible
        ? 'Текущий VPN работает стабильно.\nПоказан последний подтверждённый замер.'
        : 'Показан последний подтверждённый замер.\nДля свежей оценки можно запустить проверку вручную.';
    }
    document.dispatchEvent(new CustomEvent('freenet:current-quality-display', {
      detail: {candidate: Object.assign({}, candidate, {current:true}), scanned_at: data.scanned_at || ''}
    }));
    return true;
  }

  function clearWrongIdentity() {
    if (!hydratedEndpoint) return;
    const liveEndpoint = String((q('#bestCurrentEndpoint') || {}).textContent || '').trim();
    if (!liveEndpoint || liveEndpoint === '—' || liveEndpoint === hydratedEndpoint) return;
    hydratedEndpoint = '';
    renderMetrics(null);
    const quality = q('#bestCurrentQuality');
    if (quality) quality.textContent = 'Качество ещё не проверено';
    const health = q('#bestCurrentHealth');
    if (health) {
      health.className = 'current-health neutral';
      health.textContent = 'Для этого VPN подтверждённого замера ещё нет.';
    }
  }

  async function seedMissingMeasurement() {
    if (seedRunning || !overviewActive()) return;
    let status = null;
    try {
      const response = await fetch('/api/status', {cache:'no-store', signal:AbortSignal.timeout(7000)});
      if (response.ok) status = await response.json();
    } catch (_) {}
    if (!status || status.busy || status.updater_busy || !status.xray_online || !status.endpoint) return;
    if (seedEndpoint === status.endpoint) return;
    seedEndpoint = status.endpoint;
    seedRunning = true;
    const id = crypto.randomUUID();
    const started = Date.now();
    try {
      let response = await fetch('/api/vpn/current-quality?job=start&id=' + encodeURIComponent(id), {cache:'no-store', signal:AbortSignal.timeout(10000)});
      if (response.status === 409 || response.status === 423) return;
      while (response.status === 202 && Date.now() - started < 80000) {
        const job = await response.json();
        if (job.state === 'completed' && job.result) {
          renderQuality(job.result);
          return;
        }
        if (job.state === 'failed') return;
        await wait(1200);
        response = await fetch('/api/vpn/current-quality?job=status&id=' + encodeURIComponent(id), {cache:'no-store', signal:AbortSignal.timeout(10000)});
      }
    } catch (_) {
      // A silent seed failure leaves the normal manual current-VPN check available.
    } finally {
      seedRunning = false;
    }
  }

  async function hydrate() {
    if (!overviewActive()) return;
    try {
      const response = await fetch('/api/vpn/current-quality?job=cache', {cache:'no-store', signal:AbortSignal.timeout(7000)});
      if (!response.ok) return;
      const data = await response.json();
      if (!renderQuality(data)) void seedMissingMeasurement();
    } catch (_) {
      // Do not turn a first-paint cache read into repeated background traffic.
    }
  }

  function install() {
    const endpoint = q('#bestCurrentEndpoint');
    if (endpoint) new MutationObserver(clearWrongIdentity).observe(endpoint, {childList:true, characterData:true,subtree:true});
    const overview = q('.page[data-page-view="overview"]');
    if (overview) new MutationObserver(() => {
      if (overview.classList.contains('active')) setTimeout(hydrate, 80);
    }).observe(overview, {attributes:true, attributeFilter:['class']});
    window.addEventListener('hashchange', () => setTimeout(hydrate, 80));
    setTimeout(hydrate, 120);
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', install, {once:true});
  else install();
})();
</script>`
