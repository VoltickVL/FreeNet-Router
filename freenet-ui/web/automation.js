(() => {
  'use strict';
  if (window.__freenetAcceptedSettingsBootstrapLoaded) return;
  window.__freenetAcceptedSettingsBootstrapLoaded = true;

  const q = (selector, root = document) => root.querySelector(selector);
  const qa = (selector, root = document) => Array.from(root.querySelectorAll(selector));

  const icons = {
    routing: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M4 7h12m0 0-3-3m3 3-3 3M20 17H8m0 0 3-3m-3 3 3 3"/></svg>',
    journal: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="4" y="4" width="16" height="16" rx="2"/><path d="M8 8h8M8 12h8M8 16h6"/></svg>'
  };

  function ensureStyles() {
    if (q('#freenetAcceptedSettingsVisualContract')) return;
    document.documentElement.dataset.freenetStyleautomationjs1 = "1";
  }

  function labelNode(button) {
    const spans = qa(':scope > span', button);
    return spans.length > 1 ? spans[spans.length - 1] : null;
  }

  function setNav(button, page, text, icon) {
    if (!button) return;
    button.dataset.page = page;
    let label = labelNode(button);
    if (!label) {
      const nodes = Array.from(button.childNodes).filter(n => n.nodeType === Node.TEXT_NODE && n.textContent.trim());
      if (nodes.length) nodes[nodes.length - 1].textContent = text;
      else { label = document.createElement('span'); button.appendChild(label); }
    }
    if (label) label.textContent = text;
    const holder = q('.nav-icon', button);
    if (holder) holder.innerHTML = icon;
  }

  function ensureJournal(nav) {
    let journal = q('.nav-btn[data-page="journal"]', nav);
    if (!journal) {
      journal = document.createElement('button');
      journal.type = 'button';
      journal.className = 'nav-btn';
      journal.dataset.page = 'journal';
      journal.innerHTML = `<span class="nav-icon">${icons.journal}</span><span>Журнал</span>`;
      journal.addEventListener('click', () => {
        try { if (typeof setPage === 'function') setPage('journal'); }
        catch (_) {}
      });
    }
    return journal;
  }

  function canonicalShell() {
    const nav = q('.sidebar .nav');
    if (!nav) return false;
    const overview = q('.nav-btn[data-page="overview"]', nav);
    const subscription = q('.nav-btn[data-page="subscription"]', nav);
    const settings = q('.nav-btn[data-page="settings"]', nav);
    const routing = q('.nav-btn[data-page="routing"]', nav) || q('.nav-btn[data-page="network"]', nav);
    setNav(routing, 'routing', 'Маршрутизация', icons.routing);
    q('.nav-btn[data-page="automation"]', nav)?.remove();
    ['vpn','system','access'].forEach(page => q(`.nav-btn[data-page="${page}"]`, nav)?.remove());
    const journal = ensureJournal(nav);
    [overview, subscription, settings, routing, journal].filter(Boolean).forEach(node => nav.appendChild(node));
    q('.side-bottom')?.remove();

    const routingPage = q('[data-page-view="routing"]') || q('[data-page-view="network"]');
    if (routingPage) routingPage.dataset.pageView = 'routing';

    try {
      if (typeof pageLabels === 'object' && pageLabels) {
        pageLabels.routing = 'Маршрутизация'; pageLabels.journal = 'Журнал';
        delete pageLabels.automation; delete pageLabels.network; delete pageLabels.vpn; delete pageLabels.system; delete pageLabels.access;
      }
    } catch (_) {}
    return !!settings && !!q('[data-page-view="settings"]') && !!routingPage;
  }

  function canonicalRouting() {
    const page = q('[data-page-view="routing"]');
    if (!page) return;
    const head = q('.page-head', page);
    if (head) head.innerHTML = '<div><div class="page-kicker">Routing policy</div><h1>Маршрутизация</h1><p>Правила DIRECT / VPN / BLOCK, GeoSite / GeoIP и предварительная проверка маршрутов.</p></div>';
    const networkCard = q(':scope > .card', page);
    if (networkCard) networkCard.classList.add('fn-routing-source-hidden');
  }

  function requestedPage() {
    const hash = location.hash.replace(/^#/, '');
    const path = location.pathname.replace(/\/+$/, '').split('/').pop();
    if (hash) return hash;
    if (path === 'settings' || path === 'routing') return path;
    return '';
  }

  function activateDirectRoute() {
    const requested = requestedPage();
    if (!['settings','routing','journal'].includes(requested)) return;
    try { if (typeof setPage === 'function') setPage(requested); } catch (_) {}
  }

  function syncButtonOwnership(busy) {
    queueMicrotask(() => {
      const page = q('[data-page-view="settings"].fn3-page'); if (!page) return;
      if (busy) { qa('button', page).forEach(b => b.disabled = true); return; }
      const save = q('#fn3Save', page); if (save) save.disabled = !(save.textContent || '').includes('Сохранить изменения');
      const check = q('#fn3Check', page); if (check) check.disabled = (check.textContent || '').includes('Проверяем');
      qa('[data-v3-action]', page).forEach(b => b.disabled = false);
      q('#fn3Copy', page)?.removeAttribute('disabled'); q('#fn3AllEvents', page)?.removeAttribute('disabled');
    });
  }

  document.addEventListener('freenet:controls-busy', e => syncButtonOwnership(!!e.detail));
  window.addEventListener('hashchange', () => setTimeout(() => settle(0), 0));
  document.addEventListener('click', event => {
    if (event.target?.closest?.('.nav-btn[data-page="routing"]')) setTimeout(() => settle(0), 0);
  });

  function settle(attempt = 0) {
    ensureStyles();
    const shell = canonicalShell();
    canonicalRouting(); activateDirectRoute();
    if (!shell && attempt < 40) setTimeout(() => settle(attempt + 1), 60);
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', () => settle(), {once:true});
  else settle();
})();