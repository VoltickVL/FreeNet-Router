package main

// First-paint helper only hydrates current-quality data. It never renders
// metrics/health itself: Operation Coordinator is the single visual owner.
const overviewCurrentQualityMemoryScript = `<script id="freenetOverviewCurrentQualityMemory">
(() => {
  const wait = ms => new Promise(resolve => setTimeout(resolve, ms));
  let seedEndpoint = '';
  let seedRunning = false;

  function overviewActive() {
    const page = document.querySelector('.page[data-page-view="overview"]');
    return !!(page && page.classList.contains('active')) || location.hash === '' || location.hash === '#overview';
  }

  function currentCandidate(data) {
    return Array.isArray(data && data.candidates) ? data.candidates.find(item => item && item.current) || null : null;
  }

  function publish(data, status = null) {
    const candidate = currentCandidate(data);
    if (!candidate || !candidate.endpoint) return false;
    const liveEndpoint = String(status && status.endpoint || '').trim();
    if (!liveEndpoint || liveEndpoint !== String(candidate.endpoint).trim()) return false;
    const detail = {
      candidate: Object.assign({}, candidate, {current:true}),
      scanned_at: data && data.scanned_at || '',
      status: status ? {endpoint:liveEndpoint, xray_online:status.xray_online} : null
    };
    window.__freenetCurrentQualityHydration = detail;
    document.dispatchEvent(new CustomEvent('freenet:current-quality-display', {detail}));
    return true;
  }

  async function readStatus() {
    try {
      const response = await fetch('/api/status', {cache:'no-store', signal:AbortSignal.timeout(7000)});
      if (response.ok) return await response.json();
    } catch (_) {}
    return null;
  }

  async function seedMissingMeasurement(status) {
    if (seedRunning || !overviewActive() || !status || status.busy || status.updater_busy || !status.xray_online || !status.endpoint) return;
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
          publish(job.result, status);
          return;
        }
        if (job.state === 'failed') return;
        await wait(1200);
        response = await fetch('/api/vpn/current-quality?job=status&id=' + encodeURIComponent(id), {cache:'no-store', signal:AbortSignal.timeout(10000)});
      }
    } catch (_) {
      // Silent seed failure leaves the explicit current-VPN check available.
    } finally {
      seedRunning = false;
    }
  }

  async function hydrate() {
    if (!overviewActive()) return;
    const status = await readStatus();
    if (!status || !status.endpoint) return;
    try {
      const response = await fetch('/api/vpn/current-quality?job=cache', {cache:'no-store', signal:AbortSignal.timeout(7000)});
      if (response.ok) {
        const data = await response.json();
        if (publish(data, status)) return;
      }
    } catch (_) {}
    void seedMissingMeasurement(status);
  }

  function clearStaleHydration() {
    const pending = window.__freenetCurrentQualityHydration;
    if (!pending || !pending.candidate) return;
    const current = String(window.__freenetLastStatusEndpoint || '').trim();
    if (current && current !== String(pending.candidate.endpoint || '').trim()) {
      window.__freenetCurrentQualityHydration = null;
    }
  }

  function install() {
    document.addEventListener('freenet:status-updated', event => {
      const endpoint = String(event && event.detail && event.detail.endpoint || '').trim();
      if (endpoint) window.__freenetLastStatusEndpoint = endpoint;
      clearStaleHydration();
    });
    const overview = document.querySelector('.page[data-page-view="overview"]');
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
