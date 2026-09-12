(() => {
  'use strict';

  const q = (selector, root = document) => root.querySelector(selector);
  const qa = (selector, root = document) => Array.from(root.querySelectorAll(selector));
  const wait = ms => new Promise(resolve => setTimeout(resolve, ms));

  let mounted = false;
  let loading = false;
  let saving = false;
  let checking = false;
  let lastAutomation = null;
  let lastStatus = null;
  let baseline = null;
  let dirty = false;
  let countriesCache = [];

  const iconPaths = {
    settings: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1-2.8 2.8-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6V21H10v-.1a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1L4.2 17l.1-.1a1.7 1.7 0 0 0 .3-1.9A1.7 1.7 0 0 0 3 14H3v-4h.1a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9L4.2 7 7 4.2l.1.1a1.7 1.7 0 0 0 1.9.3A1.7 1.7 0 0 0 10 3h4a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.3l.1-.1L19.8 7l-.1.1a1.7 1.7 0 0 0-.3 1.9 1.7 1.7 0 0 0 1.6 1h.1v4H21a1.7 1.7 0 0 0-1.6 1Z"/>',
    routing: '<path d="M4 7h12m0 0-3-3m3 3-3 3M20 17H8m0 0 3-3m-3 3 3 3"/>',
    system: '<rect x="4" y="4" width="16" height="16" rx="3"/><path d="M8 9h8M8 13h8M8 17h5"/>',
    globe: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c2.5 2.6 3.7 5.6 3.7 9S14.5 18.4 12 21M12 3C9.5 5.6 8.3 8.6 8.3 12S9.5 18.4 12 21"/>',
    refresh: '<path d="M20 11a8 8 0 1 0-2.3 5.7"/><path d="M20 5v6h-6"/>',
    clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
    speed: '<path d="M5 17a8 8 0 1 1 14 0"/><path d="m12 14 4-4"/><path d="M5 17h14"/>',
    signal: '<path d="M5 19v-3M9 19v-6M13 19v-9M17 19V7M21 19V4"/>',
    shield: '<path d="M12 3 19 6v5c0 4.5-3 7.7-7 10-4-2.3-7-5.5-7-10V6l7-3Z"/><path d="m8.8 12.2 2 2 4.4-4.4"/>',
    link: '<path d="M10 13a5 5 0 0 0 7.1.1l2-2a5 5 0 0 0-7.1-7.1l-1.1 1.1"/><path d="M14 11a5 5 0 0 0-7.1-.1l-2 2A5 5 0 0 0 12 20l1.1-1.1"/>',
    copy: '<rect x="8" y="8" width="11" height="11" rx="2"/><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2"/>',
    play: '<path d="m9 7 8 5-8 5V7Z"/>',
    save: '<path d="M5 4h11l3 3v13H5V4Z"/><path d="M8 4v6h7V4M8 20v-6h8v6"/>',
    subscription: '<circle cx="12" cy="12" r="8.5"/><circle cx="12" cy="12" r="4.5"/><path d="M12 3.5v2M20.5 12h-2M12 20.5v-2M3.5 12h2"/>',
    box: '<path d="m12 3 8 4.5-8 4.5-8-4.5L12 3Z"/><path d="m4 7.5 8 4.5 8-4.5V17l-8 4-8-4V7.5Z"/>',
    info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v6M12 7h.01"/>',
    check: '<circle cx="12" cy="12" r="9"/><path d="m8 12 2.6 2.6L16.5 9"/>'
  };

  function svg(name, cls = '') {
    const paths = iconPaths[name] || iconPaths.info;
    return `<svg class="${cls}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths}</svg>`;
  }

  function injectStyles() {
    if (q('#freenetSettingsV2Styles')) return;
    const style = document.createElement('style');
    style.id = 'freenetSettingsV2Styles';
    style.textContent = `
      body:has([data-page-view="settings"].active) .content{width:min(1420px,calc(100% - 52px))!important;padding-top:22px!important}
      body:has([data-page-view="settings"].active) #pageTitle{display:none!important}
      .side-bottom{display:none!important}
      .fn-settings-page .page-head{align-items:flex-start;margin-bottom:14px!important}.fn-settings-page .page-head h1{font-size:31px!important;letter-spacing:-.04em!important}.fn-settings-page .page-head p{font-size:13px!important;color:var(--muted)!important;margin-top:5px!important}
      .fn-settings-layout{display:grid;grid-template-columns:minmax(0,1.08fr) minmax(0,1fr);gap:14px;align-items:start}
      .fn-settings-left,.fn-settings-right{display:grid;gap:14px;min-width:0}
      .fn-settings-card{border:1px solid var(--line);border-radius:16px;background:linear-gradient(180deg,rgba(14,32,54,.97),rgba(9,24,42,.98));box-shadow:0 12px 34px rgba(0,0,0,.13);padding:16px;min-width:0}
      .fn-settings-card h2,.fn-settings-card h3{margin:0;color:var(--text);letter-spacing:-.02em}.fn-settings-card h2{font-size:18px}.fn-settings-card h3{font-size:14px}.fn-settings-sub{margin-top:3px;color:var(--muted);font-size:11px;line-height:1.45}
      .fn-card-head{display:flex;align-items:flex-start;justify-content:space-between;gap:12px;margin-bottom:13px}.fn-card-title{display:flex;align-items:flex-start;gap:11px}.fn-card-icon{display:grid;place-items:center;flex:0 0 34px;width:34px;height:34px;border-radius:10px;background:linear-gradient(180deg,#194f96,#143e77);color:#78b0ff}.fn-card-icon svg{width:22px;height:22px}
      .fn-internet-grid{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:12px}.fn-field label{display:block;color:#9db0ca;font-size:11px;font-weight:700;margin:0 0 6px}.fn-field select,.fn-field input{width:100%;height:40px;padding:0 12px;border:1px solid #315173;border-radius:9px;background:#0b1b2e;color:#f6f9ff;outline:none}.fn-field select:focus,.fn-field input:focus{border-color:#4d8cff}.fn-info-line{display:flex;align-items:flex-start;gap:9px;margin-top:11px;padding:10px 11px;border:1px solid #2c5684;border-radius:10px;background:rgba(24,74,132,.24);color:#bed0e6;font-size:11px;line-height:1.45}.fn-info-line svg{width:18px;height:18px;flex:0 0 18px;color:#68a7ff}
      .fn-master{display:flex;align-items:center;gap:9px;white-space:nowrap;color:#e7effa;font-size:12px;font-weight:750}.fn-switch{position:relative;display:inline-block;width:43px;height:23px;flex:0 0 43px}.fn-switch input{position:absolute;opacity:0;pointer-events:none}.fn-switch span{position:absolute;inset:0;border-radius:999px;background:#415066;border:1px solid #5b6b80;transition:.16s ease}.fn-switch span::after{content:'';position:absolute;top:2px;left:2px;width:17px;height:17px;border-radius:50%;background:#eaf1fa;transition:.16s ease;box-shadow:0 2px 5px rgba(0,0,0,.3)}.fn-switch input:checked+span{background:#21c98a;border-color:#35e7a4}.fn-switch input:checked+span::after{transform:translateX(20px);background:white}.fn-switch input:disabled+span{opacity:.45}
      .fn-mode-grid{display:grid;grid-template-columns:1fr 1fr;gap:10px;margin-top:10px}.fn-mode{display:grid;grid-template-columns:auto 1fr;gap:10px;min-height:78px;padding:11px;border:1px solid #315173;border-radius:11px;background:#0b1b2e;cursor:pointer}.fn-mode.selected{border-color:#3388f1;box-shadow:inset 0 0 0 1px rgba(51,136,241,.26);background:linear-gradient(180deg,#0d2744,#0a2038)}.fn-mode input{width:18px;height:18px;margin:1px 0 0;accent-color:#37d9a0}.fn-mode strong{display:block;font-size:13px}.fn-mode span{display:block;color:#9cafc8;font-size:10.5px;line-height:1.4;margin-top:4px}
      .fn-auto-options{display:grid;grid-template-columns:.9fr 1.1fr 1.05fr;gap:12px;margin-top:11px}.fn-option-panel{min-width:0}.fn-option-label{color:#9db0ca;font-size:10.5px;margin-bottom:7px}.fn-choice-list{display:grid;gap:6px}.fn-choice{display:flex;align-items:flex-start;gap:7px;color:#cbd7e7;font-size:10.5px;line-height:1.35}.fn-choice input{margin:1px 0 0;accent-color:#36dca1}.fn-best-controls.disabled{opacity:.42;pointer-events:none}.fn-country-button{width:100%;margin-top:7px;min-height:34px!important;padding:7px 10px!important;justify-content:center!important;font-size:11px!important}.fn-toggle-list{display:grid;gap:9px;margin-top:12px}.fn-toggle-row{display:flex;align-items:center;gap:9px;color:#c5d3e4;font-size:11px}.fn-toggle-row .fn-switch{transform:scale(.82);transform-origin:left center;margin-right:-5px}
      .fn-settings-actions{display:grid;grid-template-columns:1fr 1fr;gap:10px;margin-top:12px}.fn-settings-actions .btn{min-height:43px!important;justify-content:center!important}.fn-settings-actions svg{width:18px;height:18px}.fn-settings-note{min-height:0;margin-top:10px;padding:9px 10px;border:1px solid #294866;border-radius:9px;background:#091a2c;color:#a9bad0;font-size:10.5px;line-height:1.4}.fn-settings-note:empty{display:none}.fn-settings-note.ok{border-color:rgba(54,227,162,.35);color:#bff4da}.fn-settings-note.bad{border-color:rgba(255,112,112,.4);color:#ffd1d1}
      .fn-watch-head{display:flex;align-items:center;justify-content:space-between;gap:10px}.fn-watch-state{display:inline-flex;align-items:center;gap:6px;padding:5px 10px;border-radius:999px;border:1px solid #255a4b;background:#0d3b31;color:#58e4ad;font-size:10px;font-weight:800}.fn-watch-state.off{border-color:#3a4e66;background:#172437;color:#a9bad0}.fn-profile-box{margin-top:12px;padding:13px;border:1px solid #294866;border-radius:11px;background:#0a192a}.fn-profile-main{display:flex;align-items:center;gap:11px}.fn-profile-name{font-size:18px;font-weight:800;letter-spacing:-.02em}.fn-profile-grid{display:grid;grid-template-columns:1fr 1fr;gap:10px;margin-top:12px}.fn-profile-fact{padding-left:10px;border-left:1px solid #294866}.fn-profile-fact span{display:block;color:#8fa4c0;font-size:9.5px}.fn-profile-fact strong{display:block;margin-top:4px;font-size:12px}.fn-endpoint-row{display:flex;align-items:center;gap:7px}.fn-icon-btn{appearance:none;border:0;background:transparent;color:#68a7ff;display:grid;place-items:center;padding:3px;cursor:pointer}.fn-icon-btn svg{width:17px;height:17px}
      .fn-quality-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:7px;margin-top:10px}.fn-quality{min-height:58px;padding:9px;border:1px solid #294866;border-radius:10px;background:#0a192a}.fn-quality span{display:flex;align-items:center;gap:5px;color:#8fa4c0;font-size:9px}.fn-quality span svg{width:15px;height:15px;color:#66a8ff}.fn-quality strong{display:block;margin-top:4px;font-size:12px}.fn-quality strong.ok{color:#4be0a5}.fn-health-banner{display:flex;align-items:center;gap:9px;margin-top:10px;padding:10px 12px;border:1px solid rgba(41,218,151,.48);border-radius:10px;background:rgba(17,104,77,.30);color:#66e9b4;font-size:11px;font-weight:650}.fn-health-banner.neutral{border-color:#34506f;background:#102337;color:#afc1d7}.fn-health-banner svg{width:19px;height:19px;flex:0 0 19px}
      .fn-journal-head{display:flex;align-items:center;justify-content:space-between;gap:10px;margin-bottom:11px}.fn-journal-table{width:100%;border-collapse:collapse;overflow:hidden;border:1px solid #294866;border-radius:9px;font-size:10px}.fn-journal-table th{padding:7px 8px;background:#123252;color:#9fb5d0;text-align:left;font-weight:650}.fn-journal-table td{padding:7px 8px;border-top:1px solid #294866;color:#d2dceb;vertical-align:top}.fn-journal-result{display:inline-flex;align-items:center;gap:6px;color:#63e7ad}.fn-journal-result.neutral{color:#b5c5d8}.fn-result-dot{width:7px;height:7px;border-radius:50%;background:currentColor}.fn-empty-row{text-align:center!important;color:#8195af!important;padding:18px!important}
      .fn-extra{grid-column:1/-1}.fn-extra-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:10px}.fn-extra-card{display:grid;grid-template-columns:auto 1fr auto;gap:9px;padding:11px;border:1px solid #315173;border-radius:11px;background:#0b1b2e;min-height:118px}.fn-extra-icon{display:grid;place-items:center;width:33px;height:33px;border-radius:9px;background:#154b8e;color:#75adff}.fn-extra-icon svg{width:21px;height:21px}.fn-extra-copy strong{font-size:12px}.fn-extra-copy p{margin:3px 0 0;color:#91a5bf;font-size:9.5px;line-height:1.35}.fn-extra-meta{grid-column:1/-1;display:grid;grid-template-columns:auto 1fr;gap:7px;align-items:center;margin-top:1px;padding-top:8px;border-top:1px solid #243f5d;color:#9db0c9;font-size:9.5px}.fn-extra-meta b{color:#e5edf8;font-weight:700}
      .fn-country-pop{position:fixed;z-index:1500;width:min(440px,calc(100vw - 30px));max-height:min(560px,calc(100vh - 40px));overflow:auto;padding:14px;border:1px solid #35608a;border-radius:14px;background:#0b1d32;box-shadow:0 22px 70px rgba(0,0,0,.5)}.fn-country-pop[hidden]{display:none!important}.fn-country-head{display:flex;align-items:center;justify-content:space-between;gap:10px}.fn-country-list{display:grid;grid-template-columns:1fr 1fr;gap:6px;margin-top:12px}.fn-country-item{display:flex;align-items:center;gap:8px;padding:8px;border:1px solid #294866;border-radius:9px;background:#0a192a;color:#d8e2ef;font-size:11px}.fn-country-item input{accent-color:#36dca1}.fn-country-close{appearance:none;border:0;background:transparent;color:#b4c3d6;font-size:22px;cursor:pointer}.fn-country-actions{display:flex;justify-content:flex-end;margin-top:12px}.fn-country-actions .btn{min-height:36px!important;padding:8px 13px!important;justify-content:center!important}
      .fn-routing-source-hidden{display:none!important}.fn-system-access{margin-top:14px}
      @media(max-width:1120px){.fn-settings-layout{grid-template-columns:1fr}.fn-extra{grid-column:auto}.fn-auto-options{grid-template-columns:1fr 1fr}.fn-extra-grid{grid-template-columns:1fr 1fr}}
      @media(max-width:760px){body:has([data-page-view="settings"].active) .content{width:calc(100% - 28px)!important}.fn-internet-grid,.fn-mode-grid,.fn-auto-options,.fn-settings-actions,.fn-profile-grid{grid-template-columns:1fr}.fn-quality-grid{grid-template-columns:1fr 1fr}.fn-extra-grid{grid-template-columns:1fr}.fn-country-list{grid-template-columns:1fr}}
    `;
    document.head.appendChild(style);
  }

  function cleanProfile(value) {
    let text = String(value || '').trim();
    text = text.replace(/^[\u{1F1E6}-\u{1F1FF}]{2}\s*/u, '');
    text = text.replace(/^[A-Z]{2}\s+/, '');
    return text || 'Текущий VPN';
  }

  function dnsLabel(mode) {
    return mode === 'xkeen' ? 'Раздельный DNS' : 'DNS через роутер';
  }

  function formatTime(value) {
    if (!value) return '—';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return '—';
    try { return new Intl.DateTimeFormat('ru-RU', {day:'2-digit',month:'2-digit',hour:'2-digit',minute:'2-digit'}).format(date); }
    catch (_) { return date.toLocaleString(); }
  }

  function resultLabel(value) {
    const labels = {
      updated:'Endpoint обновлён', same:'Без изменений', switched:'VPN переключён', success:'Успешно',
      candidate:'Найден кандидат', cooldown:'Ожидание', uncertain:'Неоднозначно', failed:'Ошибка', disabled:'Выключено'
    };
    return labels[String(value || '').toLowerCase()] || (value || 'Нет данных');
  }

  function countryMarker(code) {
    const safe = String(code || '').toLowerCase();
    if (/^[a-z]{2}$/.test(safe)) return `<span class="flag-icon flag-${safe}" aria-hidden="true"></span>`;
    return '<span class="country-code-badge">VPN</span>';
  }

  function rewireNavigation() {
    try {
      pageLabels.settings = 'Настройки';
      pageLabels.routing = 'Маршрутизация';
    } catch (_) {}

    const nav = q('.sidebar .nav');
    if (!nav) return;
    const overview = q('.nav-btn[data-page="overview"]', nav);
    const subscription = q('.nav-btn[data-page="subscription"]', nav);
    const network = q('.nav-btn[data-page="network"]', nav) || q('.nav-btn[data-page="routing"]', nav);
    const automation = q('.nav-btn[data-page="automation"]', nav) || q('.nav-btn[data-page="settings"]', nav);
    const system = q('.nav-btn[data-page="system"]', nav);
    const vpn = q('.nav-btn[data-page="vpn"]', nav);
    const access = q('.nav-btn[data-page="access"]', nav);

    if (automation) {
      automation.dataset.page = 'settings';
      automation.lastChild.textContent = 'Настройки';
      const icon = q('.nav-icon', automation);
      if (icon) { icon.innerHTML = svg('settings'); icon.dataset.freenetShell = '1'; }
    }
    if (network) {
      network.dataset.page = 'routing';
      network.lastChild.textContent = 'Маршрутизация';
      const icon = q('.nav-icon', network);
      if (icon) { icon.innerHTML = svg('routing'); icon.dataset.freenetShell = '1'; }
    }
    if (system) {
      const icon = q('.nav-icon', system);
      if (icon) { icon.innerHTML = svg('system'); icon.dataset.freenetShell = '1'; }
    }
    if (vpn) vpn.remove();
    if (access) access.remove();
    [overview, subscription, automation, network, system].filter(Boolean).forEach(node => nav.appendChild(node));
    q('.side-bottom')?.remove();

    const settingsPage = q('[data-page-view="automation"]') || q('[data-page-view="settings"]');
    if (settingsPage) settingsPage.dataset.pageView = 'settings';
    const routingPage = q('[data-page-view="network"]') || q('[data-page-view="routing"]');
    if (routingPage) routingPage.dataset.pageView = 'routing';

    const hash = location.hash.slice(1);
    const aliases = {automation:'settings', network:'routing', access:'system', vpn:'overview'};
    if (aliases[hash]) {
      history.replaceState(null, '', '#' + aliases[hash]);
      try { setPage(aliases[hash]); } catch (_) {}
    }
  }

  function mountRoutingPage() {
    const page = q('[data-page-view="routing"]');
    if (!page) return;
    const head = q('.page-head', page);
    if (head) head.innerHTML = '<div><div class="page-kicker">Policy</div><h1>Маршрутизация</h1><p>Правила DIRECT / VPN / BLOCK, GeoSite / GeoIP и предварительная проверка маршрутов.</p></div>';
    const firstCard = q(':scope > .card', page);
    if (firstCard) firstCard.classList.add('fn-routing-source-hidden');
  }

  function mergeAccessIntoSystem() {
    const system = q('[data-page-view="system"]');
    const access = q('[data-page-view="access"]');
    if (!system || !access || q('#fnSystemAccess', system)) return;
    const wrap = document.createElement('section');
    wrap.id = 'fnSystemAccess';
    wrap.className = 'fn-system-access';
    const accessHead = q('.page-head', access);
    if (accessHead) accessHead.remove();
    const children = Array.from(access.children);
    if (children.length) {
      const title = document.createElement('div');
      title.className = 'page-head';
      title.innerHTML = '<div><div class="page-kicker">Security</div><h1 style="font-size:20px">Доступ и безопасность</h1><p>Сессии Control Center и безопасный доступ к FreeNet.</p></div>';
      wrap.appendChild(title);
      children.forEach(child => wrap.appendChild(child));
      system.appendChild(wrap);
    }
    access.hidden = true;
    const p = q('.page-head p', system);
    if (p) p.textContent = 'FreeNet, XKeen/Xray, обновления, recovery, диагностика и доступ.';
  }

  function settingsMarkup() {
    return `
      <div class="page-head"><div><h1>Настройки</h1><p>Автоматизация VPN, DNS и параметры обслуживания FreeNet.</p></div></div>
      <div class="fn-settings-layout">
        <div class="fn-settings-left">
          <section class="fn-settings-card" id="fnInternetDNS">
            <div class="fn-card-head"><div class="fn-card-title"><span class="fn-card-icon">${svg('globe')}</span><div><h2>Интернет и DNS</h2><div class="fn-settings-sub">Провайдер и понятный режим DNS без технических названий core.</div></div></div></div>
            <div class="fn-internet-grid">
              <div class="fn-field"><label for="fnISP">Интернет-провайдер</label><select id="fnISP"><option value="vladlink">Владлинк</option><option value="alliancetelecom">АльянсТелеком</option><option value="rostelecom">Ростелеком</option><option value="podryad">Подряд</option><option value="custom">Свой</option></select></div>
              <div class="fn-field"><label for="fnDNS">Режим DNS</label><select id="fnDNS"><option value="firmware">DNS через роутер</option><option value="xkeen">Раздельный DNS</option></select></div>
            </div>
            <div class="fn-info-line">${svg('info')}<span id="fnDNSHint">DNS через роутер использует текущие resolver-настройки Keenetic/Netcraze. Раздельный DNS разводит DIRECT и VPN по независимым DNS-путям.</span></div>
          </section>

          <section class="fn-settings-card" id="fnAutoVPN">
            <div class="fn-card-head"><div class="fn-card-title"><span class="fn-card-icon">${svg('refresh')}</span><div><h2>AUTO VPN</h2><div class="fn-settings-sub">Автоматический контроль VPN с fail-closed решениями и rollback.</div></div></div><label class="fn-master"><span class="fn-switch"><input id="fnAutoEnabled" type="checkbox"><span></span></span><span id="fnAutoEnabledLabel">Выключено</span></label></div>
            <div class="fn-info-line">${svg('info')}<span>Выберите: FreeNet обновляет только endpoint текущего logical-профиля или может переключать страну, профиль и endpoint только на полностью подтверждённый Eligible VPN.</span></div>
            <div class="fn-mode-grid">
              <label class="fn-mode" data-mode-card="endpoint"><input type="radio" name="fnAutoMode" value="endpoint"><span><strong>Только endpoint</strong><span>Меняется только endpoint. Страна и logical-профиль не меняются.</span></span></label>
              <label class="fn-mode" data-mode-card="best"><input type="radio" name="fnAutoMode" value="best"><span><strong>Лучший VPN автоматически</strong><span>Может менять страну, профиль и endpoint после полного Best Server validation.</span></span></label>
            </div>
            <div class="fn-auto-options">
              <div class="fn-option-panel fn-field"><label for="fnAutoInterval">Интервал проверки</label><select id="fnAutoInterval"><option value="30m">30 минут</option><option value="1h">1 час</option><option value="3h">3 часа</option><option value="6h">6 часов</option><option value="manual">Вручную</option></select><div class="fn-toggle-list"><label class="fn-toggle-row"><span class="fn-switch"><input id="fnAutoApply" type="checkbox"><span></span></span>Автоматически применять подтверждённое решение</label></div></div>
              <div class="fn-option-panel fn-best-controls" id="fnBestPolicy"><div class="fn-option-label">Политика смены (для «Лучший VPN»)</div><div class="fn-choice-list"><label class="fn-choice"><input type="radio" name="fnAutoPolicy" value="degraded">Только если текущий VPN подтверждённо ухудшился</label><label class="fn-choice"><input type="radio" name="fnAutoPolicy" value="better">Если найден заметно лучший (hysteresis ≥10%)</label></div><div class="fn-settings-sub" style="margin-top:8px">После смены действует 6-часовой cooldown: никаких прыжков между близкими серверами.</div></div>
              <div class="fn-option-panel fn-best-controls" id="fnBestCountries"><div class="fn-option-label">Разрешённые страны (для «Лучший VPN»)</div><div class="fn-choice-list"><label class="fn-choice"><input type="radio" name="fnCountryScope" value="current">Только текущая страна</label><label class="fn-choice"><input type="radio" name="fnCountryScope" value="region">Страны текущего региона</label><label class="fn-choice"><input type="radio" name="fnCountryScope" value="allowlist">Свой список стран</label></div><button id="fnCountriesBtn" type="button" class="btn secondary fn-country-button">Выбрать страны <span id="fnCountriesCount"></span></button></div>
            </div>
            <div class="fn-settings-actions"><button id="fnCheckNow" class="btn primary" type="button">${svg('play')}<span>Проверить сейчас</span></button><button id="fnSaveSettings" class="btn secondary" type="button">${svg('save')}<span>Сохранить настройки</span></button></div>
            <div id="fnSettingsNotice" class="fn-settings-note"></div>
          </section>
        </div>

        <div class="fn-settings-right">
          <section class="fn-settings-card" id="fnVPNWatch">
            <div class="fn-watch-head"><h2>Текущий VPN под наблюдением</h2><span id="fnWatchState" class="fn-watch-state off">Автоматика выключена</span></div>
            <div class="fn-profile-box"><div class="fn-profile-main"><span id="fnCurrentFlag">${countryMarker('')}</span><div class="fn-profile-name" id="fnCurrentProfile">Определяем…</div></div><div class="fn-profile-grid"><div class="fn-profile-fact"><span>Профиль</span><strong id="fnCurrentProfileSmall">—</strong></div><div class="fn-profile-fact"><span>Endpoint</span><div class="fn-endpoint-row"><strong id="fnCurrentEndpoint">—</strong><button id="fnCopyEndpoint" class="fn-icon-btn" type="button" title="Копировать endpoint">${svg('copy')}</button></div></div></div></div>
            <div class="fn-quality-grid"><div class="fn-quality"><span>${svg('clock')}Последняя проверка</span><strong id="fnLastRun">—</strong></div><div class="fn-quality"><span>${svg('signal')}Отклик</span><strong id="fnLatency">—</strong></div><div class="fn-quality"><span>${svg('speed')}Скорость</span><strong id="fnSpeed">—</strong></div><div class="fn-quality"><span>${svg('shield')}Стабильность</span><strong id="fnJitter">—</strong></div></div>
            <div id="fnHealthBanner" class="fn-health-banner neutral">${svg('check')}<span>Качество текущего VPN ещё не подтверждено отдельной проверкой.</span></div>
          </section>

          <section class="fn-settings-card" id="fnAutomationJournal">
            <div class="fn-journal-head"><h2>Журнал автоматических операций</h2><span class="fn-settings-sub">Последние события</span></div>
            <table class="fn-journal-table"><thead><tr><th>Время</th><th>Тип автоматизации</th><th>Результат</th><th>Комментарий</th></tr></thead><tbody id="fnJournalBody"></tbody></table>
          </section>
        </div>

        <section class="fn-settings-card fn-extra">
          <div class="fn-card-head"><div><h2>Дополнительные автоматизации</h2><div class="fn-settings-sub">Обновление данных и компонентов FreeNet.</div></div></div>
          <div class="fn-extra-grid">
            <article class="fn-extra-card"><span class="fn-extra-icon">${svg('subscription')}</span><div class="fn-extra-copy"><strong>Обновление подписки</strong><p>Список Extra-профилей обновляется штатным механизмом. Секретная key-link никогда не попадает в UI или журнал.</p></div><span class="fn-switch"><input type="checkbox" disabled><span></span></span><div class="fn-extra-meta"><span>Режим</span><b>Вручную</b></div></article>
            <article class="fn-extra-card"><span class="fn-extra-icon">${svg('globe')}</span><div class="fn-extra-copy"><strong>GeoData / GeoIP</strong><p>Плановое обновление geosite.dat / geoip.dat штатным XKeen updater без raw shell surface.</p></div><span class="fn-switch"><input id="fnGeoDataEnabled" type="checkbox"><span></span></span><div class="fn-extra-meta"><span>Интервал</span><b id="fnGeoDataSchedule">—</b></div></article>
            <article class="fn-extra-card"><span class="fn-extra-icon">${svg('box')}</span><div class="fn-extra-copy"><strong>Обновление FreeNet</strong><p>Проверка версии доступна в topbar. Автоматическая установка без подтверждения отключена.</p></div><span class="fn-switch"><input type="checkbox" disabled><span></span></span><div class="fn-extra-meta"><span>Режим</span><b>Только проверка / вручную</b></div></article>
          </div>
        </section>
      </div>
      <div id="fnCountryPopover" class="fn-country-pop" hidden></div>
    `;
  }

  function mountSettingsPage() {
    const page = q('[data-page-view="settings"]');
    if (!page || page.dataset.settingsV2 === '1') return;
    page.dataset.settingsV2 = '1';
    page.className = 'page fn-settings-page';
    page.innerHTML = settingsMarkup();

    q('#fnAutoEnabled')?.addEventListener('change', markDirty);
    q('#fnAutoInterval')?.addEventListener('change', markDirty);
    q('#fnAutoApply')?.addEventListener('change', markDirty);
    q('#fnISP')?.addEventListener('change', markDirty);
    q('#fnDNS')?.addEventListener('change', () => { markDirty(); updateDNSHint(); });
    q('#fnGeoDataEnabled')?.addEventListener('change', markDirty);
    qa('input[name="fnAutoMode"]').forEach(node => node.addEventListener('change', () => { markDirty(); syncModeUI(); }));
    qa('input[name="fnAutoPolicy"]').forEach(node => node.addEventListener('change', markDirty));
    qa('input[name="fnCountryScope"]').forEach(node => node.addEventListener('change', () => { markDirty(); syncCountryUI(); }));
    q('#fnCountriesBtn')?.addEventListener('click', openCountries);
    q('#fnSaveSettings')?.addEventListener('click', saveSettings);
    q('#fnCheckNow')?.addEventListener('click', checkNow);
    q('#fnCopyEndpoint')?.addEventListener('click', copyEndpoint);
  }

  function updateDNSHint() {
    const mode = q('#fnDNS')?.value;
    const hint = q('#fnDNSHint');
    if (!hint) return;
    hint.textContent = mode === 'xkeen'
      ? 'Раздельный DNS: DIRECT и VPN используют независимые DNS-пути; VPN DNS привязан к защищённому VPN-path и routing policy.'
      : 'DNS через роутер: запросы обслуживаются Keenetic/Netcraze по его текущим resolver-настройкам. FreeNet не подменяет их скрытым режимом.';
  }

  function markDirty() {
    dirty = true;
    renderDirtyState();
  }

  function renderDirtyState() {
    const note = q('#fnSettingsNotice');
    if (!note || saving || checking) return;
    if (dirty) {
      note.className = 'fn-settings-note';
      note.textContent = 'Есть несохранённые изменения. «Проверить сейчас» использует только уже сохранённую AUTO VPN policy.';
    } else if (!note.classList.contains('bad') && !note.classList.contains('ok')) {
      note.textContent = '';
    }
  }

  function setNotice(text, tone = '') {
    const node = q('#fnSettingsNotice');
    if (!node) return;
    node.textContent = text || '';
    node.className = 'fn-settings-note' + (tone ? ' ' + tone : '');
  }

  function setBusy(value, message = '') {
    saving = value && message.includes('Сохраня');
    checking = value && !saving;
    ['#fnSaveSettings','#fnCheckNow'].forEach(selector => { const node = q(selector); if (node) node.disabled = value; });
    if (value && message) setNotice(message);
  }

  function selectedValue(name, fallback) {
    return q(`input[name="${name}"]:checked`)?.value || fallback;
  }

  function currentForm() {
    return {
      isp: q('#fnISP')?.value || lastStatus?.isp || 'vladlink',
      dns: q('#fnDNS')?.value === 'xkeen' ? 'xkeen' : 'firmware',
      enabled: !!q('#fnAutoEnabled')?.checked,
      interval: q('#fnAutoInterval')?.value || 'manual',
      mode: selectedValue('fnAutoMode', 'endpoint'),
      policy: selectedValue('fnAutoPolicy', 'degraded'),
      country_scope: selectedValue('fnCountryScope', 'region'),
      countries: countriesCache.slice(),
      auto_apply: !!q('#fnAutoApply')?.checked,
      geodata_enabled: !!q('#fnGeoDataEnabled')?.checked
    };
  }

  function syncModeUI() {
    const mode = selectedValue('fnAutoMode', 'endpoint');
    qa('[data-mode-card]').forEach(card => card.classList.toggle('selected', card.dataset.modeCard === mode));
    qa('.fn-best-controls').forEach(node => node.classList.toggle('disabled', mode !== 'best'));
  }

  function syncCountryUI() {
    const scope = selectedValue('fnCountryScope', 'region');
    const button = q('#fnCountriesBtn');
    if (button) button.disabled = scope !== 'allowlist';
    const count = q('#fnCountriesCount');
    if (count) count.textContent = countriesCache.length ? `(${countriesCache.length})` : '';
  }

  function applySnapshot(auto, status) {
    if (!auto || !status) return;
    lastAutomation = auto;
    lastStatus = status;
    const settings = auto.settings || {};
    q('#fnISP').value = ['vladlink','alliancetelecom','rostelecom','podryad','custom'].includes(status.isp) ? status.isp : 'custom';
    q('#fnDNS').value = status.dns_mode === 'xkeen' ? 'xkeen' : 'firmware';
    q('#fnAutoEnabled').checked = !!settings.enabled;
    q('#fnAutoInterval').value = settings.interval || 'manual';
    q('#fnAutoApply').checked = settings.auto_apply !== false;
    q('#fnGeoDataEnabled').checked = !!auto.geodata_auto;
    q('#fnGeoDataSchedule').textContent = auto.geodata_schedule || '—';
    countriesCache = Array.isArray(settings.countries) ? settings.countries.slice() : [];
    const mode = settings.mode === 'best' ? 'best' : 'endpoint';
    const policy = settings.policy === 'better' ? 'better' : 'degraded';
    const scope = ['current','region','allowlist'].includes(settings.country_scope) ? settings.country_scope : 'region';
    const modeInput = q(`input[name="fnAutoMode"][value="${mode}"]`); if (modeInput) modeInput.checked = true;
    const policyInput = q(`input[name="fnAutoPolicy"][value="${policy}"]`); if (policyInput) policyInput.checked = true;
    const scopeInput = q(`input[name="fnCountryScope"][value="${scope}"]`); if (scopeInput) scopeInput.checked = true;
    q('#fnAutoEnabledLabel').textContent = settings.enabled ? 'Включено' : 'Выключено';
    syncModeUI(); syncCountryUI(); updateDNSHint(); renderCurrent(auto, status); renderJournal(auto.events || []); syncTopbarDNS(status.dns_mode);
    baseline = currentForm(); dirty = false; renderDirtyState();
  }

  function renderCurrent(auto, status) {
    const profile = cleanProfile(auto.current_profile || status.profile_label || [status.city,status.country,'Extra'].filter(Boolean).join(', '));
    q('#fnCurrentProfile').textContent = profile;
    q('#fnCurrentProfileSmall').textContent = profile;
    q('#fnCurrentEndpoint').textContent = auto.current_endpoint || status.endpoint || '—';
    const flag = q('#fnCurrentFlag'); if (flag) flag.innerHTML = countryMarker(auto.country_code || status.country_code);
    const watch = q('#fnWatchState');
    watch.textContent = auto.settings?.enabled ? 'Под контролем' : 'Автоматика выключена';
    watch.classList.toggle('off', !auto.settings?.enabled);
    q('#fnLastRun').textContent = formatTime(auto.last_run);
    q('#fnLatency').textContent = auto.current_quality_known && auto.current_latency_ms ? `${auto.current_latency_ms} мс` : '—';
    q('#fnSpeed').textContent = auto.current_quality_known && auto.current_download_mbps ? `${Math.round(auto.current_download_mbps)} Мбит/с` : '—';
    q('#fnJitter').textContent = auto.current_quality_known && auto.current_jitter_ms ? `${auto.current_jitter_ms} мс` : '—';
    const health = q('#fnHealthBanner');
    if (auto.current_quality_known && auto.current_eligible) {
      health.className = 'fn-health-banner';
      health.innerHTML = `${svg('check')}<span>Текущий VPN подтверждён и работает стабильно.${auto.next_run ? ` Следующая проверка: ${formatTime(auto.next_run)}.` : ''}</span>`;
    } else if (auto.last_reason) {
      health.className = 'fn-health-banner neutral';
      health.innerHTML = `${svg('info')}<span>${auto.last_reason}</span>`;
    } else {
      health.className = 'fn-health-banner neutral';
      health.innerHTML = `${svg('info')}<span>Качество текущего VPN ещё не подтверждено отдельной проверкой.</span>`;
    }
  }

  function renderJournal(events) {
    const body = q('#fnJournalBody'); if (!body) return;
    body.replaceChildren();
    if (!events.length) {
      const row = document.createElement('tr'); const cell = document.createElement('td'); cell.colSpan = 4; cell.className = 'fn-empty-row'; cell.textContent = 'Автоматических операций пока не было.'; row.appendChild(cell); body.appendChild(row); return;
    }
    events.slice(0, 8).forEach(event => {
      const row = document.createElement('tr');
      const cells = [formatTime(event.at), event.kind || 'AUTO VPN', resultLabel(event.result), event.message || '—'];
      cells.forEach((value, index) => { const cell = document.createElement('td'); if (index === 2) { const span = document.createElement('span'); const good = /success|updated|switched|same/i.test(event.result || ''); span.className = 'fn-journal-result' + (good ? '' : ' neutral'); span.innerHTML = '<i class="fn-result-dot"></i>'; span.append(document.createTextNode(value)); cell.appendChild(span); } else cell.textContent = value; row.appendChild(cell); });
      body.appendChild(row);
    });
  }

  function syncTopbarDNS(mode) {
    qa('.fn-shell-fact-copy').forEach(copy => {
      const label = q('span', copy); const value = q('strong', copy);
      if (label && value && label.textContent.trim().toUpperCase() === 'DNS') value.textContent = dnsLabel(mode);
    });
  }

  async function loadAll(silent = false) {
    if (loading) return;
    loading = true;
    try {
      const [autoResponse, statusResponse] = await Promise.all([fetch('/api/automation', {cache:'no-store'}), fetch('/api/status', {cache:'no-store'})]);
      if (!autoResponse.ok || !statusResponse.ok) throw new Error('Не удалось загрузить Settings state');
      const auto = await autoResponse.json(); const status = await statusResponse.json();
      if (!auto.success) throw new Error(auto.error || 'Automation state unavailable');
      if (!dirty || !silent) applySnapshot(auto, status); else { lastAutomation = auto; lastStatus = status; renderCurrent(auto,status); renderJournal(auto.events || []); syncTopbarDNS(status.dns_mode); }
    } catch (error) {
      if (!silent) setNotice(error?.message || 'Не удалось загрузить настройки.', 'bad');
    } finally { loading = false; }
  }

  async function planNetwork(form) {
    const url = `/api/network-profile/plan?isp=${encodeURIComponent(form.isp)}&dns_mode=${encodeURIComponent(form.dns)}`;
    const response = await fetch(url, {cache:'no-store'}); const value = await response.json();
    if (!response.ok || !value.success) throw new Error(value.error || value.reason || 'Сетевой план недоступен');
    if (!value.supported || value.mutation !== 'NONE') throw new Error(value.reason || 'Сетевой план не прошёл безопасную read-only проверку');
    return value;
  }

  async function applyNetwork(form) {
    const response = await fetch('/api/network-profile/apply', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({operation:'network',isp:form.isp,dns_mode:form.dns,confirm:true})});
    const value = await response.json();
    if (!response.ok || !value.success) {
      const error = new Error(value.primary_error || value.error || 'Сетевые настройки не применены');
      error.rollback = value.rollback_state || '';
      throw error;
    }
    return value;
  }

  async function saveAutomation(form) {
    const response = await fetch('/api/automation', {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'save',enabled:form.enabled,interval:form.interval,mode:form.mode,policy:form.policy,country_scope:form.country_scope,countries:form.countries,auto_apply:form.auto_apply,geodata_enabled:form.geodata_enabled})});
    const value = await response.json();
    if (!response.ok || !value.success) throw new Error(value.error || 'AUTO VPN settings not saved');
    return value;
  }

  async function rollbackNetwork(original) {
    try { const plan = await planNetwork(original); if (!plan.active) await applyNetwork(original); return 'SUCCESS'; }
    catch (_) { return 'FAILED_OR_UNKNOWN'; }
  }

  async function saveSettings() {
    if (saving || checking) return;
    const form = currentForm();
    if (form.mode === 'best' && form.country_scope === 'allowlist' && !form.countries.length) { setNotice('Для своего списка стран выберите минимум одну страну.', 'bad'); return; }
    saving = true; setBusy(true, 'Сохраняем настройки: read-only plan → controlled apply → acceptance…');
    const originalNetwork = {isp:lastStatus?.isp || baseline?.isp || form.isp, dns:lastStatus?.dns_mode === 'xkeen' ? 'xkeen' : 'firmware'};
    let networkChanged = false;
    try {
      const plan = await planNetwork(form);
      if (!plan.active) { await applyNetwork(form); networkChanged = true; }
      try { await saveAutomation(form); }
      catch (error) {
        if (networkChanged) {
          const rollback = await rollbackNetwork(originalNetwork);
          if (rollback !== 'SUCCESS') throw new Error(`${error.message}. ROLLBACK FAILED/UNKNOWN — дальнейшие изменения остановлены.`);
          throw new Error(`${error.message}. Сетевые настройки возвращены в исходное состояние.`);
        }
        throw error;
      }
      dirty = false; await loadAll(false); setNotice('Настройки сохранены и подтверждены.', 'ok');
    } catch (error) {
      setNotice(error?.message || 'Настройки не сохранены.', 'bad');
    } finally { saving = false; checking = false; ['#fnSaveSettings','#fnCheckNow'].forEach(selector => { const node=q(selector); if(node) node.disabled=false; }); }
  }

  async function checkNow() {
    if (saving || checking) return;
    if (dirty) { setNotice('Сначала сохраните изменения. Проверка использует только подтверждённую policy.', 'bad'); return; }
    checking = true; setBusy(true, 'Проверяем текущую AUTO VPN policy. Возможная mutation допускается только после полной validation…');
    try {
      const response = await fetch('/api/automation', {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'check'})});
      const value = await response.json();
      if (!response.ok || !value.success) throw new Error(value.error || value.last_reason || 'Проверка AUTO VPN не завершена');
      lastAutomation = value; renderCurrent(value,lastStatus || {}); renderJournal(value.events || []); setNotice(value.last_reason || 'Проверка завершена.', 'ok');
    } catch (error) { setNotice(error?.message || 'Проверка AUTO VPN не завершена.', 'bad'); }
    finally { checking=false; saving=false; ['#fnSaveSettings','#fnCheckNow'].forEach(selector=>{const node=q(selector);if(node)node.disabled=false;}); }
  }

  async function loadCountries() {
    if (loadCountries.loaded) return loadCountries.values || [];
    try {
      const status = lastStatus || {};
      const isp = status.isp || q('#fnISP')?.value || 'vladlink';
      const dns = status.dns_mode === 'xkeen' ? 'xkeen' : 'firmware';
      const response = await fetch(`/api/network-profile/plan?isp=${encodeURIComponent(isp)}&dns_mode=${encodeURIComponent(dns)}`, {cache:'no-store'});
      const value = await response.json();
      const map = new Map();
      (value.extra_profiles || []).forEach(profile => {
        const code = String(profile.country_code || '').toLowerCase();
        if (!/^[a-z]{2}$/.test(code) || code === 'ru') return;
        const name = cleanProfile(profile.name || code.toUpperCase()).replace(/,?\s*Extra\s*$/i,'').trim();
        if (!map.has(code)) map.set(code, {code,name:name || code.toUpperCase()});
      });
      loadCountries.values = Array.from(map.values()).sort((a,b)=>a.name.localeCompare(b.name,'ru')); loadCountries.loaded=true; return loadCountries.values;
    } catch (_) { return []; }
  }

  async function openCountries() {
    if (selectedValue('fnCountryScope','region') !== 'allowlist') return;
    const pop = q('#fnCountryPopover'); if (!pop) return;
    pop.hidden = false; pop.innerHTML = '<div class="fn-country-head"><strong>Разрешённые страны</strong><button type="button" class="fn-country-close" aria-label="Закрыть">×</button></div><div class="fn-settings-sub">Показываются только зарубежные base Extra страны из вашей подписки.</div><div class="fn-settings-sub" style="margin-top:12px">Загружаем список…</div>';
    const rect = q('#fnCountriesBtn').getBoundingClientRect(); pop.style.left = Math.max(15,Math.min(window.innerWidth-pop.offsetWidth-15,rect.right-pop.offsetWidth))+'px'; pop.style.top = Math.max(15,Math.min(window.innerHeight-pop.offsetHeight-15,rect.bottom+8))+'px';
    const values = await loadCountries();
    pop.innerHTML = '<div class="fn-country-head"><strong>Разрешённые страны</strong><button type="button" class="fn-country-close" aria-label="Закрыть">×</button></div><div class="fn-settings-sub">Только зарубежные base Extra из текущей подписки.</div><div class="fn-country-list"></div><div class="fn-country-actions"><button type="button" class="btn primary" id="fnCountryDone">Готово</button></div>';
    const list = q('.fn-country-list',pop);
    if (!values.length) { list.innerHTML='<div class="fn-settings-sub">Список стран сейчас недоступен. Ничего не изменено.</div>'; }
    values.forEach(item => { const label=document.createElement('label');label.className='fn-country-item';const input=document.createElement('input');input.type='checkbox';input.value=item.code;input.checked=countriesCache.includes(item.code);const text=document.createElement('span');text.textContent=item.name;label.append(input,text);list.appendChild(label); });
    q('.fn-country-close',pop)?.addEventListener('click',()=>{pop.hidden=true});
    q('#fnCountryDone',pop)?.addEventListener('click',()=>{countriesCache=qa('.fn-country-item input:checked',pop).map(n=>n.value).sort();pop.hidden=true;markDirty();syncCountryUI();});
  }

  async function copyEndpoint() {
    const value = q('#fnCurrentEndpoint')?.textContent?.trim(); if (!value || value === '—') return;
    try { await navigator.clipboard.writeText(value); setNotice('Endpoint скопирован.', 'ok'); setTimeout(()=>{if(!dirty)setNotice('')},1200); } catch (_) {}
  }

  function mount() {
    if (mounted) return; mounted = true;
    injectStyles(); rewireNavigation(); mountRoutingPage(); mergeAccessIntoSystem(); mountSettingsPage();
    const hash = location.hash.slice(1); if (hash === 'settings') { try { setPage('settings'); } catch (_) {} }
    loadAll(false);
    setInterval(() => {
      if (!saving && !checking) {
        if (q('[data-page-view="settings"]')?.classList.contains('active')) loadAll(true);
        else if (lastStatus) syncTopbarDNS(lastStatus.dns_mode);
      }
    }, 15000);
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mount, {once:true});
  else mount();
})();
