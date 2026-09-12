(() => {
  'use strict';

  const q = (selector, root = document) => root.querySelector(selector);
  const qa = (selector, root = document) => Array.from(root.querySelectorAll(selector));
  let latestAutomationPayload = null;
  let latestStatusPayload = null;

  function dnsLabel(mode) {
    return mode === 'xkeen' ? 'Раздельный DNS' : 'DNS через роутер';
  }

  function normalizeLegacyDNSLabels() {
    try {
      if (typeof dnsLabels !== 'object' || !dnsLabels) return;
      dnsLabels.auto = 'DNS через роутер';
      dnsLabels.firmware = 'DNS через роутер';
      dnsLabels.xkeen = 'Раздельный DNS';
      dnsLabels.custom = 'DNS через роутер';
    } catch (_) {}
  }

  function formatTime(value) {
    if (!value) return '—';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return '—';
    try {
      return new Intl.DateTimeFormat('ru-RU', {
        day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit'
      }).format(date);
    } catch (_) {
      return date.toLocaleString();
    }
  }

  function removeLegacyFooter() {
    qa('.footer').forEach(node => node.remove());
  }

  function syncTopbarDNS(mode) {
    const text = dnsLabel(mode);
    const overview = q('#overviewDNS');
    if (overview) overview.textContent = text;

    qa('.fn-shell-fact-copy').forEach(copy => {
      const label = q('span', copy);
      const value = q('strong', copy);
      if (label && value && label.textContent.trim().toUpperCase() === 'DNS') {
        value.textContent = text;
      }
    });

    qa('.overview-approved-fact.fn-shell-fact').forEach(fact => {
      const label = q('.fn-shell-fact-copy span', fact);
      const value = q('.fn-shell-fact-copy strong', fact);
      if (label && value && label.textContent.trim().toUpperCase() === 'DNS') {
        value.textContent = text;
      }
    });

    const legacyState = q('#dnsState');
    if (legacyState) legacyState.textContent = text;
    const quickGuard = q('#quickNetworkGuard');
    if (quickGuard) quickGuard.textContent = `VPN-действия не меняют ISP и DNS. Текущий DNS-режим: ${text}.`;
  }

  function syncCountryButton() {
    const button = q('#fnCountriesBtn');
    if (!button) return;
    const mode = q('input[name="fnAutoMode"]:checked')?.value || 'endpoint';
    const scope = q('input[name="fnCountryScope"]:checked')?.value || 'region';
    button.disabled = mode !== 'best' || scope !== 'allowlist';
    if (button.disabled) {
      const popover = q('#fnCountryPopover');
      if (popover) popover.hidden = true;
    }
  }

  function renderCurrentQuality(auto) {
    if (!auto) return;

    const checkedAt = auto.current_quality_checked_at || '';
    const lastCheck = q('#fnLastRun');
    if (lastCheck) lastCheck.textContent = formatTime(checkedAt);
    if (!auto.current_quality_known) return;

    const latency = q('#fnLatency');
    const speed = q('#fnSpeed');
    const jitter = q('#fnJitter');
    if (latency) latency.textContent = auto.current_latency_ms ? `${auto.current_latency_ms} мс` : '—';
    if (speed) speed.textContent = auto.current_download_mbps ? `${Math.round(auto.current_download_mbps)} Мбит/с` : '—';
    if (jitter) jitter.textContent = auto.current_jitter_ms ? `${auto.current_jitter_ms} мс` : '—';

    const health = q('#fnHealthBanner');
    if (!health || !auto.current_eligible) return;
    if (auto.current_quality_fresh) {
      health.className = 'fn-health-banner';
      health.innerHTML = '<span>Текущий VPN подтверждён последней проверкой и работает стабильно.</span>';
      return;
    }
    health.className = 'fn-health-banner neutral';
    health.innerHTML = `<span>Показаны последние подтверждённые метрики${checkedAt ? ` от ${formatTime(checkedAt)}` : ''}. Для актуальной оценки используйте «Проверить текущий VPN» на Обзоре.</span>`;
  }

  function reconcileRuntimePresentation() {
    removeLegacyFooter();
    normalizeLegacyDNSLabels();
    if (latestAutomationPayload) {
      renderCurrentQuality(latestAutomationPayload);
      syncCountryButton();
    }
    if (latestStatusPayload) syncTopbarDNS(latestStatusPayload.dns_mode);
  }

  function scheduleRuntimePresentation() {
    setTimeout(reconcileRuntimePresentation, 0);
  }

  function requestPath(input) {
    try {
      const raw = typeof input === 'string' ? input : input?.url;
      if (!raw) return '';
      return new URL(raw, location.href).pathname;
    } catch (_) {
      return '';
    }
  }

  function installFetchTap() {
    if (window.__freenetWorkAcceptanceFetchWrapped) return;
    window.__freenetWorkAcceptanceFetchWrapped = true;
    const originalFetch = window.fetch.bind(window);
    window.fetch = async (...args) => {
      const response = await originalFetch(...args);
      const path = requestPath(args[0]);
      if (response.ok && (path === '/api/automation' || path === '/api/status')) {
        try {
          const payload = await response.clone().json();
          if (path === '/api/automation') latestAutomationPayload = payload;
          if (path === '/api/status') latestStatusPayload = payload;
          scheduleRuntimePresentation();
        } catch (_) {}
      }
      return response;
    };
  }

  function installSettingsStateGuard() {
    document.addEventListener('change', event => {
      const target = event.target;
      if (!(target instanceof HTMLInputElement)) return;
      if (target.name === 'fnAutoMode' || target.name === 'fnCountryScope') {
        syncCountryButton();
      }
    });
  }

  removeLegacyFooter();
  normalizeLegacyDNSLabels();
  installFetchTap();
  installSettingsStateGuard();
})();
