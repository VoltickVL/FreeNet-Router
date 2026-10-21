package main

// First-paint helper only hydrates current-quality data. It never renders
// metrics/health itself: Operation Coordinator is the single visual owner.
const overviewCurrentQualityMemoryScript = `<script id="freenetOverviewCurrentQualityMemory">
(() => {
  const wait = ms => new Promise(resolve => setTimeout(resolve, ms));
  const seededIdentities = new Set();
  let seedRunning = false;
  let hydrateRunning = false;
  let latestStatus = null;
  let statusRevision = 0;
  let identityGeneration = 0;
  let measurementRevision = 0;

  function identity(value) {
    const result = String(value || '').trim().toLowerCase();
    return /^[a-f0-9]{64}$/.test(result) ? result : '';
  }

  function statusKey(status) {
    return status ? identity(status.current_identity) || String(status.endpoint || '').trim() : '';
  }

  function overviewActive() {
    const page = document.querySelector('.page[data-page-view="overview"]');
    return !!(page && page.classList.contains('active')) || location.hash === '' || location.hash === '#overview';
  }

  function currentCandidate(data) {
    return Array.isArray(data && data.candidates) ? data.candidates.find(item => item && item.current) || null : null;
  }

  function matchesCurrent(data, status) {
    const candidate = currentCandidate(data);
    if (!candidate || !candidate.endpoint) return false;
    const liveEndpoint = String(status && status.endpoint || '').trim();
    if (!liveEndpoint || liveEndpoint !== String(candidate.endpoint).trim()) return false;
    const liveIdentity = identity(status && status.current_identity);
    return !liveIdentity || identity(data && data.current_identity) === liveIdentity;
  }

  function hasCurrentSpeed(data) {
    const candidate = currentCandidate(data);
    return !!(candidate && candidate.throughput_source === 'strict_aggregate' && Number.isFinite(candidate.download_mbps) && candidate.download_mbps > 0);
  }

  function rememberStatus(status) {
    if (!status) return;
    if (latestStatus && (statusKey(latestStatus) !== statusKey(status) || latestStatus.endpoint !== status.endpoint)) identityGeneration++;
    latestStatus = status;
    statusRevision++;
    window.__freenetLastStatusEndpoint = String(status.endpoint || '').trim();
    window.__freenetLastStatusIdentity = identity(status.current_identity);
    clearStaleHydration();
  }

  function publish(data, status = null, generation = identityGeneration) {
    status = latestStatus || status;
    if (generation !== identityGeneration || !matchesCurrent(data, status)) return false;
    const candidate = currentCandidate(data);
    const liveEndpoint = String(status.endpoint || '').trim();
    const detail = {
      candidate: Object.assign({}, candidate, {current:true}),
      scanned_at: data && data.scanned_at || '',
      current_identity: identity(data && data.current_identity),
      display_throughput: data && data.display_throughput ? Object.assign({}, data.display_throughput) : null,
      status: {endpoint:liveEndpoint, current_identity:identity(status.current_identity), xray_online:status.xray_online}
    };
    window.__freenetCurrentQualityHydration = detail;
    document.dispatchEvent(new CustomEvent('freenet:current-quality-display', {detail}));
    return true;
  }

  async function readStatus() {
    const revision = statusRevision;
    try {
      const response = await fetch('/api/status', {cache:'no-store', signal:AbortSignal.timeout(7000)});
      if (response.ok) {
        const status = await response.json();
        if (revision === statusRevision) rememberStatus(status);
        return latestStatus;
      }
    } catch (_) {}
    return null;
  }

  async function seedMissingMeasurement(status) {
    status = latestStatus || status;
    if (seedRunning || !overviewActive() || !status || status.busy || status.updater_busy || status.mutation_blocked || !status.xray_online || !status.endpoint) return;
    if (document.querySelector('#bestServerShell')?.getAttribute('aria-busy') === 'true' || document.querySelector('#controlCenter')?.inert) return;
    const key = statusKey(status);
    if (!key || seededIdentities.has(key)) return;
    const id = typeof window.freenetQualityJobID === 'function' ? window.freenetQualityJobID() : '';
    if (!/^[a-zA-Z0-9_-]{16,64}$/.test(id)) return;
    seededIdentities.add(key);
    seedRunning = true;
    const generation = identityGeneration;
    const refresh = running => document.dispatchEvent(new CustomEvent('freenet:current-quality-refresh', {detail:{running, endpoint:status.endpoint, current_identity:identity(status.current_identity)}}));
    const started = Date.now();
    refresh(true);
    try {
      let response = await fetch('/api/vpn/current-quality?job=start&id=' + encodeURIComponent(id) + '&automatic=1', {cache:'no-store', signal:AbortSignal.timeout(10000)});
      while (Date.now() - started < 80000) {
        if (response.status === 200) {
          const data = await response.json();
          if (data && data.success) publish(data, status, generation);
          return;
        }
        if (response.status !== 202) return;
        const job = await response.json();
        if (job.id !== id || job.mode !== 'current') return;
        if (job.state === 'completed' && job.result) {
          publish(job.result, status, generation);
          return;
        }
        if (job.state !== 'running') return;
        await wait(1200);
        response = await fetch('/api/vpn/current-quality?job=status&id=' + encodeURIComponent(id), {cache:'no-store', signal:AbortSignal.timeout(10000)});
      }
    } catch (_) {
      // Silent seed failure leaves the explicit current-VPN check available.
    } finally {
      seedRunning = false;
      refresh(false);
    }
  }

  async function hydrate() {
    if (hydrateRunning || !overviewActive()) return;
    hydrateRunning = true;
    try {
      const status = await readStatus();
      if (!status || !status.endpoint) return;
      const generation = identityGeneration, measured = measurementRevision;
      try {
        const response = await fetch('/api/vpn/current-quality?job=cache', {cache:'no-store', signal:AbortSignal.timeout(7000)});
        if (generation !== identityGeneration || measured !== measurementRevision) return;
        if (response.ok) {
          const data = await response.json();
          if (publish(data, status, generation) && hasCurrentSpeed(data)) return;
        }
      } catch (_) {}
      if (generation === identityGeneration && measured === measurementRevision) void seedMissingMeasurement(status);
    } finally {
      hydrateRunning = false;
    }
  }

  function clearStaleHydration() {
    const pending = window.__freenetCurrentQualityHydration;
    if (!pending || !pending.candidate) return;
    if (latestStatus && !matchesCurrent({candidates:[pending.candidate], current_identity:pending.current_identity}, latestStatus)) {
      window.__freenetCurrentQualityHydration = null;
    }
  }

  function install() {
    document.addEventListener('freenet:status-updated', event => {
      rememberStatus(event && event.detail);
    });
    document.addEventListener('freenet:current-quality-measured', async event => {
      const data = event && event.detail;
      measurementRevision++;
      if (!data || hasCurrentSpeed(data)) return;
      const generation = identityGeneration;
      const status = await readStatus();
      if (generation === identityGeneration && matchesCurrent(data, status)) void seedMissingMeasurement(status);
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
