(() => {
  'use strict';
  if (window.__freenetAcceptedSettingsBootstrapLoaded) return;
  window.__freenetAcceptedSettingsBootstrapLoaded = true;

  const q = (selector, root = document) => root.querySelector(selector);
  const qa = (selector, root = document) => Array.from(root.querySelectorAll(selector));

  const icons = {
    settings: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1-2.8 2.8-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6V21H10v-.1a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1L4.2 17l.1-.1a1.7 1.7 0 0 0 .3-1.9A1.7 1.7 0 0 0 3 14H3v-4h.1a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9L4.2 7 7 4.2l.1.1a1.7 1.7 0 0 0 1.9.3A1.7 1.7 0 0 0 10 3h4a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.3l.1-.1L19.8 7l-.1.1a1.7 1.7 0 0 0-.3 1.9 1.7 1.7 0 0 0 1.6 1h.1v4H21a1.7 1.7 0 0 0-1.6 1Z"/></svg>',
    routing: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M4 7h12m0 0-3-3m3 3-3 3M20 17H8m0 0 3-3m-3 3 3 3"/></svg>',
    journal: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="4" y="4" width="16" height="16" rx="2"/><path d="M8 8h8M8 12h8M8 16h6"/></svg>',
    vpn: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 3 19 6v5c0 4.5-3 7.7-7 10-4-2.3-7-5.5-7-10V6l7-3Z"/><circle cx="12" cy="11" r="2.4"/></svg>'
  };

  function ensureStyles() {
    if (q('#freenetAcceptedSettingsVisualContract')) return;
    const style = document.createElement('style');
    style.id = 'freenetAcceptedSettingsVisualContract';
    style.textContent = `
      .fn3-page:not(.active){display:none!important}
      body:has([data-page-view="settings"].active) .content{width:min(1290px,calc(100% - 34px))!important;padding-top:16px!important;padding-bottom:26px!important}
      body:has([data-page-view="settings"].active) #pageTitle{display:none!important}
      .fn3-head{margin:0 0 13px!important;align-items:flex-start!important}.fn3-head h1{font-size:29px!important;line-height:1.08!important;font-weight:700!important;letter-spacing:-.035em!important}.fn3-head p{margin-top:5px!important;font-size:13px!important;color:#9cb2cc!important}.fn3-save{width:208px!important;min-width:208px!important;height:41px!important;min-height:41px!important;padding:0 14px!important;border-radius:10px!important;font-size:13px!important;gap:8px!important}.fn3-save svg{width:18px!important;height:18px!important;flex:0 0 18px!important}
      .fn3-grid{grid-template-columns:minmax(0,1.04fr) minmax(0,1fr)!important;gap:14px!important}.fn3-left,.fn3-right{gap:14px!important}.fn3-card{border-color:#25557c!important;border-radius:12px!important;background:linear-gradient(180deg,#0b2947 0%,#08223b 100%)!important;padding:13px!important;box-shadow:none!important}.fn3-card-head{align-items:center!important}.fn3-title{align-items:center!important;gap:12px!important}.fn3-icon{width:48px!important;height:48px!important;flex-basis:48px!important;border-radius:10px!important;background:linear-gradient(180deg,#1675f1,#0b56c9)!important}.fn3-icon svg{width:28px!important;height:28px!important}.fn3-card h2{font-size:20px!important;font-weight:700!important}.fn3-card h3{font-size:13px!important}.fn3-sub{font-size:11px!important;color:#9db2ca!important}.fn3-master{margin-left:auto!important}.fn3-master>#fn3AutoLabel{display:none!important}.fn3-enabled-badge{display:inline-flex!important;align-items:center!important;height:29px!important;margin-left:10px!important;padding:0 13px!important;border-radius:999px!important;background:#11875f!important;color:#e7fff5!important;font-size:12px!important;font-style:normal!important;font-weight:700!important;vertical-align:middle!important}.fn3-switch{width:58px!important;height:30px!important;flex-basis:58px!important}.fn3-switch span:after{width:24px!important;height:24px!important;top:2px!important;left:2px!important}.fn3-switch input:checked+span:after{transform:translateX(28px)!important}.fn3-info{margin-top:11px!important;padding:10px 11px!important;border-color:#2b70a9!important;background:#0a3155!important;font-size:11.5px!important}.fn3-info svg{width:19px!important;height:19px!important}.fn3-section-label{margin:12px 0 7px!important;padding-left:10px!important;border-left:3px solid #22dca2!important;font-size:13px!important}.fn3-scope-list{border-color:#2b638f!important;border-radius:9px!important}.fn3-scope{min-height:61px!important;padding:9px 13px!important}.fn3-scope.selected{background:linear-gradient(180deg,#0b4b83,#0a3e70)!important;box-shadow:inset 0 0 0 1px #2b96ff!important}.fn3-scope input{width:20px!important;height:20px!important}.fn3-scope strong{font-size:13.5px!important}.fn3-scope small{font-size:10.5px!important}.fn3-recommended{background:#0f805c!important;color:#50efb3!important}.fn3-safety{padding:10px 11px!important;border-color:#2c6ca0!important;background:#0a3155!important;font-size:10.7px!important}.fn3-auto-actions{grid-template-columns:minmax(0,1fr) minmax(0,.96fr)!important;gap:14px!important;margin-top:12px!important}.fn3-auto-actions .btn{height:55px!important;min-height:55px!important;border-radius:9px!important;font-size:15px!important;gap:9px!important}.fn3-auto-actions .btn svg{width:19px!important;height:19px!important;flex:0 0 19px!important}.fn3-control{height:55px!important;padding:8px 12px!important;border-radius:9px!important;position:relative!important}.fn3-control:after{content:'›';position:absolute;right:12px;top:50%;transform:translateY(-52%);font-size:29px;line-height:1;color:#8db7de}.fn3-control-dot{width:26px!important;height:26px!important}.fn3-control strong{font-size:13px!important}.fn3-control small{font-size:9.5px!important;padding-right:20px!important}
      .fn3-vpn-head{min-height:32px!important}.fn3-vpn-head h2{display:flex!important;align-items:center!important;gap:9px!important}.fn3-vpn-title-icon{display:grid!important;place-items:center!important;width:30px!important;height:30px!important;border-radius:8px!important;background:#115abc!important;color:#b9d8ff!important}.fn3-vpn-title-icon svg{width:18px!important;height:18px!important}.fn3-status-pill{font-size:11px!important;padding:5px 12px!important}.fn3-status-pill:after{content:' ›';font-size:15px}.fn3-profile{margin-top:9px!important;padding:10px 12px!important;border-color:#2a638f!important;border-radius:9px!important}.fn3-profile-name{font-size:18px!important}.fn3-profile-facts{margin-top:8px!important}.fn3-fact span{font-size:9.5px!important}.fn3-fact strong{font-size:12px!important}.fn3-copy svg{width:15px!important;height:15px!important}.fn3-metrics{gap:8px!important;margin-top:8px!important}.fn3-metric{min-height:60px!important;padding:8px 9px!important;border-color:#2a638f!important;border-radius:9px!important}.fn3-metric span{font-size:9px!important}.fn3-metric svg{width:14px!important;height:14px!important}.fn3-metric strong{font-size:12px!important}.fn3-health{margin-top:8px!important;padding:9px 11px!important;border-color:#0da36f!important;background:#07553f!important;font-size:10.7px!important}.fn3-health>svg{width:24px!important;height:24px!important;flex:0 0 24px!important}.fn3-health small{font-size:9.5px!important}
      .fn3-journal-head{margin-bottom:8px!important}.fn3-journal-head h2{font-size:19px!important}.fn3-link{font-size:10.5px!important}.fn3-table{font-size:9.3px!important}.fn3-table th{padding:7px 8px!important;background:#124470!important}.fn3-table td{padding:6px 8px!important}.fn3-extra{padding:8px 6px 11px!important}.fn3-extra-title{padding:0 2px!important;margin-bottom:8px!important}.fn3-extra-title h2{font-size:19px!important}.fn3-extra-grid{grid-template-columns:repeat(4,minmax(0,1fr))!important;gap:10px!important}.fn3-extra-card{padding:10px!important;border-color:#2b638f!important;border-radius:9px!important;background:#09253f!important}.fn3-extra-head{grid-template-columns:auto 1fr auto!important;gap:9px!important}.fn3-extra-icon{width:41px!important;height:41px!important;border-radius:8px!important;background:linear-gradient(180deg,#1369dc,#0d52b8)!important}.fn3-extra-icon svg{width:23px!important;height:23px!important}.fn3-extra-card h3{font-size:12px!important}.fn3-extra-card p{font-size:9.3px!important;line-height:1.35!important}.fn3-extra-card .fn3-switch{width:42px!important;height:22px!important;flex-basis:42px!important}.fn3-extra-card .fn3-switch span:after{width:16px!important;height:16px!important;top:2px!important;left:2px!important}.fn3-extra-card .fn3-switch input:checked+span:after{transform:translateX(20px)!important}.fn3-extra-row{margin-top:9px!important}.fn3-extra-row label{font-size:9.5px!important}.fn3-extra-row select{height:31px!important;font-size:10px!important;border-color:#2a638f!important}.fn3-extra-meta{font-size:9px!important;row-gap:5px!important}.fn3-extra-action{height:39px!important;min-height:39px!important;margin-top:9px!important;font-size:10.5px!important;gap:7px!important}.fn3-extra-action svg,.fn3-backup-actions .btn svg{width:16px!important;height:16px!important;flex:0 0 16px!important}.fn3-backup-actions{gap:7px!important}.fn3-backup-actions .btn{height:39px!important;min-height:39px!important;font-size:9.5px!important;padding:0 8px!important;gap:5px!important}.fn3-danger{background:#32162a!important;border-color:#b83c63!important}.fn-routing-source-hidden{display:none!important}
      @media(max-width:1120px){.fn3-grid{grid-template-columns:1fr!important}.fn3-extra-grid{grid-template-columns:1fr 1fr!important}}@media(max-width:760px){body:has([data-page-view="settings"].active) .content{width:calc(100% - 24px)!important}.fn3-head{display:block!important}.fn3-save{width:100%!important;margin-top:10px!important}.fn3-extra-grid{grid-template-columns:1fr!important}}
    `;
    document.head.appendChild(style);
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

  function ensureSettingsNav(nav) {
    let settings = q('.nav-btn[data-page="settings"]', nav);
    if (settings) return settings;
    settings = document.createElement('button');
    settings.type = 'button';
    settings.className = 'nav-btn';
    settings.dataset.page = 'settings';
    settings.innerHTML = `<span class="nav-icon">${icons.settings}</span><span>Настройки</span>`;
    settings.addEventListener('click', () => {
      try { if (typeof setPage === 'function') setPage('settings'); }
      catch (_) {}
    });
    return settings;
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
    const settingsPage = q('[data-page-view="settings"]');
    const settings = settingsPage ? ensureSettingsNav(nav) : q('.nav-btn[data-page="settings"]', nav);
    const routing = q('.nav-btn[data-page="routing"]', nav) || q('.nav-btn[data-page="network"]', nav);
    q('.nav-btn[data-page="automation"]', nav)?.remove();
    if (settings) setNav(settings, 'settings', 'Настройки', icons.settings);
    setNav(routing, 'routing', 'Маршрутизация', icons.routing);
    ['vpn','system','access'].forEach(page => q(`.nav-btn[data-page="${page}"]`, nav)?.remove());
    const journal = ensureJournal(nav);
    [overview, subscription, settings, routing, journal].filter(Boolean).forEach(node => nav.appendChild(node));
    q('.side-bottom')?.remove();

    q('[data-page-view="automation"]')?.classList.remove('active');
    const routingPage = q('[data-page-view="routing"]') || q('[data-page-view="network"]');
    if (routingPage) routingPage.dataset.pageView = 'routing';

    try {
      if (typeof pageLabels === 'object' && pageLabels) {
        pageLabels.routing = 'Маршрутизация'; pageLabels.journal = 'Журнал';
        delete pageLabels.automation; delete pageLabels.network; delete pageLabels.vpn; delete pageLabels.system; delete pageLabels.access;
      }
    } catch (_) {}
    return !!settingsPage && !!settings && !!routingPage;
  }

  function canonicalRouting() {
    const page = q('[data-page-view="routing"]');
    if (!page) return;
    const head = q('.page-head', page);
    if (head) head.innerHTML = '<div><div class="page-kicker">Routing policy</div><h1>Маршрутизация</h1><p>Правила DIRECT / VPN / BLOCK, GeoSite / GeoIP и предварительная проверка маршрутов.</p></div>';
    const networkCard = q(':scope > .card', page);
    if (networkCard) networkCard.classList.add('fn-routing-source-hidden');
  }

  function removeProviderFact() {
    const value = q('#topISPValue');
    value?.closest?.('.overview-approved-fact,.fn-shell-fact,.topbar-item,.top-stat,.top-chip')?.remove();
    qa('.topbar .overview-approved-fact,.topbar .fn-shell-fact,.topbar-item,.top-stat,.top-chip').forEach(node => {
      const text = (node.textContent || '').trim();
      if (/Провайдер|Владлинк|АльянсТелеком|Ростелеком|Подряд/.test(text)) node.remove();
    });
  }

  function decorateSettings() {
    const page = q('[data-page-view="settings"].fn3-page');
    if (!page) return false;
    const autoTitle = q('.fn3-left .fn3-title > div', page);
    const label = q('#fn3AutoLabel', page);
    if (autoTitle && label && label.parentElement !== autoTitle) {
      label.classList.add('fn3-enabled-badge');
      const h2 = q('h2', autoTitle);
      h2?.insertAdjacentElement('afterend', label);
    }
    const vpnHead = q('.fn3-vpn-head h2', page);
    if (vpnHead && !q('.fn3-vpn-title-icon', vpnHead)) {
      const icon = document.createElement('span'); icon.className = 'fn3-vpn-title-icon'; icon.innerHTML = icons.vpn; vpnHead.prepend(icon);
    }
    return true;
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
    if (event.target?.closest?.('.nav-btn[data-page="settings"],.nav-btn[data-page="routing"]')) setTimeout(() => settle(0), 0);
  });

  function settle(attempt = 0) {
    ensureStyles();
    const shell = canonicalShell();
    canonicalRouting(); removeProviderFact(); decorateSettings(); activateDirectRoute();
    if ((!shell || !q('[data-page-view="settings"].fn3-page')) && attempt < 40) setTimeout(() => settle(attempt + 1), 60);
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', () => settle(), {once:true});
  else settle();
})();
