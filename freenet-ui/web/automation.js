(() => {
  'use strict';
  if (window.__freenetSettingsV3BootstrapLoaded) return;
  window.__freenetSettingsV3BootstrapLoaded = true;

  const q = (selector, root = document) => root.querySelector(selector);
  const qa = (selector, root = document) => Array.from(root.querySelectorAll(selector));

  const settingsIcon = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1-2.8 2.8-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6V21H10v-.1a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1L4.2 17l.1-.1a1.7 1.7 0 0 0 .3-1.9A1.7 1.7 0 0 0 3 14H3v-4h.1a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9L4.2 7 7 4.2l.1.1a1.7 1.7 0 0 0 1.9.3A1.7 1.7 0 0 0 10 3h4a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.3l.1-.1L19.8 7l-.1.1a1.7 1.7 0 0 0-.3 1.9 1.7 1.7 0 0 0 1.6 1h.1v4H21a1.7 1.7 0 0 0-1.6 1Z"/></svg>';
  const routingIcon = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M4 7h12m0 0-3-3m3 3-3 3M20 17H8m0 0 3-3m-3 3 3 3"/></svg>';

  function ensureVisibilityGuard() {
    if (q('#freenetSettingsV3VisibilityGuard')) return;
    const style = document.createElement('style');
    style.id = 'freenetSettingsV3VisibilityGuard';
    style.textContent = '.fn3-page:not(.active){display:none!important}';
    document.head.appendChild(style);
  }

  function setNavLabel(button, text, icon) {
    if (!button) return;
    const label = button.querySelector(':scope > span:last-child');
    if (label) label.textContent = text;
    const iconNode = button.querySelector('.nav-icon');
    if (iconNode && icon) iconNode.innerHTML = icon;
  }

  function retireLegacyShell() {
    const nav = q('.sidebar .nav');
    if (!nav) return false;

    const settings = q('.nav-btn[data-page="settings"]', nav) || q('.nav-btn[data-page="automation"]', nav);
    if (settings) {
      settings.dataset.page = 'settings';
      setNavLabel(settings, 'Настройки', settingsIcon);
    }

    const routing = q('.nav-btn[data-page="routing"]', nav) || q('.nav-btn[data-page="network"]', nav);
    if (routing) {
      routing.dataset.page = 'routing';
      setNavLabel(routing, 'Маршрутизация', routingIcon);
    }

    ['vpn', 'system', 'access'].forEach(page => q(`.nav-btn[data-page="${page}"]`, nav)?.remove());

    const overview = q('.nav-btn[data-page="overview"]', nav);
    const subscription = q('.nav-btn[data-page="subscription"]', nav);
    [overview, subscription, settings, routing].filter(Boolean).forEach(node => nav.appendChild(node));
    q('.side-bottom')?.remove();

    const settingsPage = q('[data-page-view="settings"]') || q('[data-page-view="automation"]');
    if (settingsPage) settingsPage.dataset.pageView = 'settings';
    const routingPage = q('[data-page-view="routing"]') || q('[data-page-view="network"]');
    if (routingPage) routingPage.dataset.pageView = 'routing';

    try {
      if (typeof pageLabels === 'object' && pageLabels) {
        pageLabels.settings = 'Настройки';
        pageLabels.routing = 'Маршрутизация';
        pageLabels.journal = 'Журнал';
        delete pageLabels.automation;
        delete pageLabels.network;
        delete pageLabels.vpn;
        delete pageLabels.system;
        delete pageLabels.access;
      }
    } catch (_) {}

    return !!settingsPage;
  }

  function removeProviderFact() {
    const value = q('#topISPValue');
    const fact = value?.closest?.('.overview-approved-fact,.fn-shell-fact,.topbar-item,.top-stat,.top-chip');
    if (fact) {
      fact.remove();
      return true;
    }
    for (const node of qa('.topbar .overview-approved-fact,.topbar .fn-shell-fact,.topbar-item,.top-stat,.top-chip')) {
      const text = (node.textContent || '').trim();
      if (/Провайдер|Владлинк|АльянсТелеком|Ростелеком|Подряд/.test(text)) {
        node.remove();
        return true;
      }
    }
    return false;
  }

  function settingsRequested() {
    const hash = location.hash.replace(/^#/, '');
    const path = location.pathname.replace(/\/+$/, '').split('/').pop();
    return hash === 'settings' || path === 'settings' || !!q('[data-page-view="settings"].active');
  }

  function activateDirectSettingsRoute() {
    if (!settingsRequested()) return;
    try {
      if (typeof setPage === 'function') setPage('settings');
    } catch (_) {}
  }

  function syncV3ButtonOwnership(busy) {
    queueMicrotask(() => {
      const page = q('[data-page-view="settings"].fn3-page');
      if (!page) return;
      const buttons = qa('button', page);
      if (busy) {
        buttons.forEach(button => { button.disabled = true; });
        return;
      }
      const save = q('#fn3Save', page);
      if (save) {
        const text = (save.textContent || '').trim();
        save.disabled = !text.includes('Сохранить изменения');
      }
      const check = q('#fn3Check', page);
      if (check) check.disabled = (check.textContent || '').includes('Проверяем');
      qa('[data-v3-action]', page).forEach(button => { button.disabled = false; });
      const copy = q('#fn3Copy', page);
      if (copy) copy.disabled = false;
      const allEvents = q('#fn3AllEvents', page);
      if (allEvents) allEvents.disabled = false;
    });
  }

  document.addEventListener('freenet:controls-busy', event => {
    syncV3ButtonOwnership(!!event.detail);
  });

  document.addEventListener('click', event => {
    const button = event.target?.closest?.('.nav-btn[data-page="settings"]');
    if (!button) return;
    setTimeout(() => {
      activateDirectSettingsRoute();
      window.dispatchEvent(new Event('hashchange'));
    }, 0);
  });

  function settle(attempt = 0) {
    ensureVisibilityGuard();
    const ready = retireLegacyShell();
    removeProviderFact();
    activateDirectSettingsRoute();
    if (!ready && attempt < 30) setTimeout(() => settle(attempt + 1), 75);
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => settle(), {once: true});
  } else {
    settle();
  }
})();
