package main

const overviewCurrentQualityMemoryScript = `<script id="freenetOverviewCurrentQualityMemory">
(() => {
  const q = (selector, root = document) => root.querySelector(selector);
  const wait = ms => new Promise(resolve => setTimeout(resolve, ms));
  let hydratedEndpoint = '';
  let seedEndpoint = '';
  let seedRunning = false;

  function overviewActive() {
    const page = q('.page[data-page-view="overview"]');
    return !!(page && page.classList.contains('active')) || location.hash === '' || location.hash === '#overview';
  }

  function currentCandidate(data) {
    return Array.isArray(data && data.candidates) ? data.candidates.find(item => item && item.current) || null : null;
  }

  function compactStatus(status) {
    if (!status) return null;
    return {
      xray_online: status.xray_online,
      endpoint: String(status.endpoint || '')
    };
  }

  function publishQuality(data, status = null) {
    const candidate = currentCandidate(data);
    if (!candidate || !candidate.endpoint) return false;
    const liveEndpoint = String((q('#bestCurrentEndpoint') || {}).textContent || '').trim();
    if (liveEndpoint && liveEndpoint !== '—' && liveEndpoint !== candidate.endpoint) return false;
    hydratedEndpoint = candidate.endpoint;
    document.dispatchEvent(new CustomEvent('freenet:current-quality-display', {
      detail: {
        candidate: Object.assign({}, candidate, {current:true}),
        scanned_at: data.scanned_at || '',
        status: compactStatus(status)
      }
    }));
    return true;
  }

  function invalidateWrongIdentity() {
    if (!hydratedEndpoint) return;
    const liveEndpoint = String((q('#bestCurrentEndpoint') || {}).textContent || '').trim();
    if (!liveEndpoint || liveEndpoint === '—' || liveEndpoint === hydratedEndpoint) return;
    hydratedEndpoint = '';
    document.dispatchEvent(new CustomEvent('freenet:current-quality-display', {
      detail: {invalidate:true}
    }));
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
    const id = typeof window.freenetQualityJobID === 'function' ? window.freenetQualityJobID() : '';
    if (!/^[a-zA-Z0-9_-]{16,64}$/.test(id)) {
      seedRunning = false;
      return;
    }
    const started = Date.now();
    try {
      let response = await fetch('/api/vpn/current-quality?job=start&id=' + encodeURIComponent(id), {cache:'no-store', signal:AbortSignal.timeout(10000)});
      if (response.status === 409 || response.status === 423) return;
      while (response.status === 202 && Date.now() - started < 80000) {
        const job = await response.json();
        if (job.state === 'completed' && job.result) {
          publishQuality(job.result, status);
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
      let status = null;
      try {
        const statusResponse = await fetch('/api/status', {cache:'no-store', signal:AbortSignal.timeout(7000)});
        if (statusResponse.ok) status = await statusResponse.json();
      } catch (_) {}
      const response = await fetch('/api/vpn/current-quality?job=cache', {cache:'no-store', signal:AbortSignal.timeout(7000)});
      if (!response.ok) return;
      const data = await response.json();
      if (!publishQuality(data, status)) void seedMissingMeasurement();
    } catch (_) {
      // Do not turn a first-paint cache read into repeated background traffic.
    }
  }

  function install() {
    const endpoint = q('#bestCurrentEndpoint');
    if (endpoint) new MutationObserver(invalidateWrongIdentity).observe(endpoint, {childList:true, characterData:true,subtree:true});
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
