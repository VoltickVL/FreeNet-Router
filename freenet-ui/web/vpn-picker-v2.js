// Issue #612: canonical VPN picker v2.
// One visual owner for topbar VPN state, anchored picker geometry and profile list.
// Existing exact-connect plan/apply/rollback semantics remain untouched.
(() => {
  'use strict';
  if (window.__freenetVPNPickerV2Mounted) return;
  window.__freenetVPNPickerV2Mounted = true;

  const q = (s, root = document) => root.querySelector(s);
  const qa = (s, root = document) => Array.from(root.querySelectorAll(s));
  const countryNames = {
    ae:'ОАЭ', ar:'Аргентина', at:'Австрия', au:'Австралия', be:'Бельгия',
    bg:'Болгария', br:'Бразилия', ca:'Канада', ch:'Швейцария', co:'Колумбия',
    cz:'Чехия', de:'Германия', dk:'Дания', fi:'Финляндия', fr:'Франция',
    gb:'Великобритания', gr:'Греция', hk:'Гонконг', hr:'Хорватия', hu:'Венгрия',
    ie:'Ирландия', il:'Израиль', is:'Исландия', it:'Италия', jp:'Япония',
    kr:'Южная Корея', kz:'Казахстан', lt:'Литва', lu:'Люксембург', mx:'Мексика',
    my:'Малайзия', ng:'Нигерия', nl:'Нидерланды', no:'Норвегия', pe:'Перу',
    pl:'Польша', pt:'Португалия', ro:'Румыния', rs:'Сербия', se:'Швеция',
    sg:'Сингапур', si:'Словения', sk:'Словакия', tr:'Турция', ua:'Украина',
    us:'США', za:'ЮАР'
  };

  const legacyPickerStyleIDs = [
    'FreeNetIssue562SelectorPopover',
    'freenetVPNSelectorModalSearchStyles',
    'freenetIssue590Styles',
    'freenetIssue601Styles',
    'freenetIssue604Styles',
    'freenetIssue609VpnPolishStyles'
  ];

  function clean(value) {
    return String(value || '').replace(/^[\u{1F1E6}-\u{1F1FF}]{2}\s*/u, '').replace(/^[A-Za-z]{2}\s+/, '').trim();
  }
  function normalize(value) {
    return clean(value).normalize('NFKD').replace(/[\u0300-\u036f]/g, '').replace(/ё/g, 'е').toLowerCase().trim();
  }
  function profileCode(profile) {
    const direct = String(profile && (profile.country_code || profile.code) || '').trim().toLowerCase();
    if (/^[a-z]{2}$/.test(direct)) return direct;
    const raw = String(profile && (profile.name || profile.label) || '').trim();
    const ascii = raw.match(/^([A-Za-z]{2})\s+/);
    if (ascii) return ascii[1].toLowerCase();
    return '';
  }
  function endpoint(profile) {
    if (!profile) return '';
    if (profile.endpoint) return String(profile.endpoint);
    const address = String(profile.address || '');
    const host = address.includes(':') ? '[' + address + ']' : address;
    return address ? host + (profile.port ? ':' + profile.port : '') : '';
  }
  function profileCountryName(profile, code) {
    const explicit = String(profile && profile.country || '').trim();
    if (explicit) return explicit;
    const parts = clean(profile && (profile.name || profile.label) || '').split(',').map(v => v.trim()).filter(Boolean);
    if (parts.length >= 2) {
      const candidate = parts[parts.length - 2];
      if (candidate && candidate.toLowerCase() !== 'extra') return candidate;
    }
    return countryNames[code] || (code ? code.toUpperCase() : 'VPN');
  }
  function profiles() {
    try { return Array.isArray(extraProfiles) ? extraProfiles : []; } catch (_) { return []; }
  }
  function selectedID() {
    try { return String(selectedProviderID || ''); } catch (_) { return ''; }
  }
  function flagNode(code, extraClass) {
    const span = document.createElement('span');
    span.className = 'flag-icon ' + (extraClass || '') + ' ' + (code ? 'flag-' + code : 'flag-unknown');
    span.setAttribute('aria-hidden', 'true');
    return span;
  }
  function inferFromOverview() {
    const flag = q('#bestCurrentFlag');
    const name = q('#bestCurrentName');
    let code = '';
    if (flag) {
      const match = String(flag.className || '').match(/(?:^|\s)flag-([a-z]{2})(?:\s|$)/);
      if (match) code = match[1];
    }
    const raw = clean(name && name.textContent || '');
    const parts = raw.split(',').map(v => v.trim()).filter(Boolean);
    const country = parts.length >= 2 ? parts[parts.length - 2] : '';
    return {code, country};
  }
  function matchCurrentProfile(status) {
    const list = profiles();
    const targetEndpoint = String(status && status.endpoint || '');
    const targetLabel = normalize(status && status.profile_label || '');
    return list.find(p => targetEndpoint && endpoint(p) === targetEndpoint) ||
      list.find(p => targetLabel && normalize(p.name || p.label || '') === targetLabel) || null;
  }
  function currentIdentity(status) {
    const s = status || {};
    const matched = matchCurrentProfile(s);
    const overview = inferFromOverview();
    let code = String(s.country_code || '').trim().toLowerCase();
    if (!/^[a-z]{2}$/.test(code)) code = profileCode(matched) || overview.code || profileCode({name:s.profile_label || ''});
    let country = String(s.country || '').trim();
    if (!country && matched) country = profileCountryName(matched, code);
    if (!country) country = overview.country || countryNames[code] || (code ? code.toUpperCase() : 'VPN');
    const profile = clean(s.profile_label || (matched && matched.name) || q('#bestCurrentName')?.textContent || 'Текущий VPN');
    return {code, country, profile};
  }

  function removeLegacyPickerStyles() {
    legacyPickerStyleIDs.forEach(id => q('#' + id)?.remove());
    q('#fnVpnPickerBackdrop')?.remove();
  }

  function installStyles() {
    if (q('#freenetVPNPickerV2Styles')) return;
    const style = document.createElement('style');
    style.id = 'freenetVPNPickerV2Styles';
    style.textContent = `
      html body #controlCenter #fnVpnPickerHost{position:relative!important;display:block!important;flex:0 0 auto!important;margin:0!important;min-width:0!important}
      html body #controlCenter #fnVpnPickerToggle{appearance:none!important;width:154px!important;height:50px!important;min-width:154px!important;max-width:154px!important;padding:6px 10px!important;border:1px solid #315276!important;border-radius:11px!important;background:linear-gradient(180deg,#0d1d30,#0a1727)!important;color:#eef5ff!important;display:grid!important;grid-template-columns:24px minmax(0,1fr)!important;align-items:center!important;gap:8px!important;text-align:left!important;cursor:pointer!important;box-shadow:inset 0 1px rgba(255,255,255,.018)!important}
      html body #controlCenter #fnVpnPickerToggle:hover,html body #controlCenter #fnVpnPickerToggle[aria-expanded="true"]{border-color:#5d91d1!important;background:linear-gradient(180deg,#112743,#0d2036)!important}
      #fnVpnPickerToggle .fnv2-globe{display:grid;place-items:center;width:22px;height:22px;color:#72a8ff}
      #fnVpnPickerToggle .fnv2-globe svg{width:22px;height:22px}
      #fnVpnPickerToggle .fnv2-chip-copy{display:grid;gap:3px;min-width:0}
      #fnVpnPickerToggle .fnv2-chip-copy small{font-size:9px;line-height:1;color:#8097b6;font-weight:800}
      #fnVpnPickerToggle .fnv2-chip-line{display:flex;align-items:center;gap:6px;min-width:0;font-size:12px;line-height:1.1;font-weight:800;color:#f7f9ff}
      #fnVpnPickerToggle .fnv2-chip-line .flag-icon{width:20px;height:14px;flex:0 0 20px;border-radius:3px}
      #fnVpnPickerToggle #fnVpnPickerCountryName{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}

      #fnVpnPickerPopover{position:fixed!important;z-index:2600!important;width:min(620px,calc(100vw - 24px))!important;max-height:min(680px,calc(100vh - 24px))!important;overflow:hidden!important;border:1px solid #31577b!important;border-radius:16px!important;background:linear-gradient(180deg,#0d2034,#091827)!important;box-shadow:0 28px 80px rgba(0,0,0,.55)!important;padding:0!important;transform:none!important}
      #fnVpnPickerPopover[hidden]{display:none!important}
      #fnVpnPickerPopover .fnv2-head{display:flex;align-items:flex-start;justify-content:space-between;gap:14px;padding:15px 16px 13px;border-bottom:1px solid #27415e;background:#0d2034}
      #fnVpnPickerPopover .fnv2-head strong{display:block;font-size:15px;color:#f7f9ff}
      #fnVpnPickerPopover .fnv2-head span{display:block;margin-top:3px;font-size:10px;color:#8299b7}
      #fnVpnPickerPopover .fnv2-close{appearance:none;width:31px;height:31px;border:1px solid #31577b;border-radius:9px;background:#10253c;color:#9cb2cd;font-size:18px;line-height:1;cursor:pointer}
      #fnVpnPickerPopover .fnv2-body{display:grid;gap:10px;padding:12px;min-width:0;overflow:hidden}
      #fnVpnPickerPopover .fnv2-current{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:10px;padding:10px 11px;border:1px solid #274864;border-radius:12px;background:#0a1a2b}
      #fnVpnPickerPopover .fnv2-current>.flag-icon{width:34px;height:23px;border-radius:5px}
      #fnVpnPickerPopover .fnv2-current-copy{display:grid;gap:2px;min-width:0}
      #fnVpnPickerPopover .fnv2-current-copy small{font-size:9px;color:#7890ad;font-weight:800;text-transform:uppercase;letter-spacing:.06em}
      #fnVpnPickerPopover .fnv2-current-copy strong{font-size:13px;color:#f5f8ff;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
      #fnVpnPickerPopover .fnv2-current-copy span{font-size:10px;color:#8ba0bb;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
      #fnVpnPickerPopover .fnv2-current-state{padding:4px 8px;border:1px solid rgba(73,218,146,.28);border-radius:999px;background:rgba(73,218,146,.08);color:#80e8ad;font-size:9px;font-weight:800}

      #fnVpnPickerPopover #bestServerAdvanced{display:block!important;width:100%!important;max-width:none!important;min-width:0!important;margin:0!important;padding:0!important;background:transparent!important;border:0!important;box-shadow:none!important}
      #fnVpnPickerPopover #bestServerAdvanced>h3,#fnVpnPickerPopover #bestServerAdvanced:before{display:none!important}
      #fnVpnPickerPopover #profilesList{display:grid!important;grid-template-columns:1fr!important;gap:8px!important;width:100%!important;min-width:0!important;margin:0!important;padding:0!important}
      #fnVpnPickerPopover #profilesList>.field{display:block!important;margin:0!important;padding:0!important;border:0!important;background:transparent!important}
      #fnVpnPickerPopover #profilesList>.field>label,#fnVpnPickerPopover #profilesTrigger{display:none!important}
      #fnVpnPickerPopover #profileSearch{display:block!important;width:100%!important;height:42px!important;min-height:42px!important;margin:0!important;padding:0 13px!important;border:1px solid #315273!important;border-radius:11px!important;background:#0a1b2d!important;color:#edf4ff!important;font-size:12px!important;outline:none!important}
      #fnVpnPickerPopover #profileSearch:focus{border-color:#5b8cff!important;box-shadow:0 0 0 3px rgba(91,140,255,.12)!important}
      #fnVpnPickerPopover #profilesMenu{position:static!important;display:block!important;width:100%!important;max-width:none!important;max-height:min(330px,38vh)!important;overflow-y:auto!important;overflow-x:hidden!important;margin:0!important;padding:4px!important;border:1px solid #27445f!important;border-radius:12px!important;background:#071522!important;box-shadow:none!important;overscroll-behavior:contain!important}
      #fnVpnPickerPopover #profilesMenu .profile-option{appearance:none!important;width:100%!important;min-width:0!important;display:grid!important;grid-template-columns:auto minmax(0,1fr)!important;align-items:center!important;gap:10px!important;padding:9px 10px!important;border:1px solid transparent!important;border-radius:9px!important;background:transparent!important;color:#eef5ff!important;text-align:left!important;cursor:pointer!important}
      #fnVpnPickerPopover #profilesMenu .profile-option:hover,#fnVpnPickerPopover #profilesMenu .profile-option[aria-selected="true"]{border-color:#31577b!important;background:#10263d!important}
      #fnVpnPickerPopover #profilesMenu .profile-option>.flag-icon{width:28px;height:19px;border-radius:4px}
      #fnVpnPickerPopover .fnv2-profile-copy{display:grid;gap:2px;min-width:0}
      #fnVpnPickerPopover .profile-option-main{font-size:11.5px!important;font-weight:800!important;color:#f4f7fb!important;overflow:hidden!important;text-overflow:ellipsis!important;white-space:nowrap!important}
      #fnVpnPickerPopover .profile-option-endpoint{font-size:9.5px!important;color:#8298b4!important;font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace!important;overflow:hidden!important;text-overflow:ellipsis!important;white-space:nowrap!important}
      #fnVpnPickerPopover .fnv2-empty{padding:14px 12px;color:#91a5bf;font-size:11px;line-height:1.45;text-align:center}

      #fnVpnPickerPopover #selectedProfileCard,#fnVpnPickerPopover #selectedProfileCard.fn-selector-state{width:100%!important;min-width:0!important;min-height:0!important;margin:0!important;padding:9px 11px 9px 14px!important;border-radius:11px!important;box-shadow:none!important}
      #fnVpnPickerPopover #exactConnectRow{display:grid!important;grid-template-columns:minmax(0,1fr) 150px!important;gap:8px!important;width:100%!important;margin:0!important}
      #fnVpnPickerPopover #exactConnectRow[hidden]{display:none!important}
      #fnVpnPickerPopover #exactConnectRow .btn{width:100%!important;min-width:0!important;min-height:42px!important;border-radius:10px!important}
      #fnVpnPickerPopover #exactConnectBtn{background:linear-gradient(180deg,#347eff,#2367e7)!important;border-color:#69a0ff!important}
      #fnVpnPickerPopover #quickNetworkGuard,#fnVpnPickerPopover .quick-network-guard{margin:0!important}

      @media(max-width:760px){
        html body #controlCenter #fnVpnPickerToggle{width:132px!important;min-width:132px!important;max-width:132px!important}
        #fnVpnPickerPopover{left:12px!important;right:12px!important;bottom:12px!important;top:auto!important;width:auto!important;max-height:calc(100vh - 24px)!important;border-radius:16px!important}
        #fnVpnPickerPopover .fnv2-body{max-height:calc(100vh - 90px)!important;overflow-y:auto!important}
        #fnVpnPickerPopover #profilesMenu{max-height:34vh!important}
        #fnVpnPickerPopover #exactConnectRow{grid-template-columns:1fr!important}
      }
    `;
    document.head.appendChild(style);
  }

  function dnsFact(summary) {
    if (!summary) return null;
    return qa('.overview-approved-fact', summary).find(node => /DNS/i.test(node.textContent || '')) || null;
  }
  function orderTopbar(host) {
    const summary = q('.overview-approved-top.fn-shell-summary') || q('#overviewApprovedTop');
    const xray = q('.fn-xray-topbar');
    const dns = dnsFact(summary);
    const freenet = q('#topFreenetUpdate');
    if (!summary || !host || !xray || !dns || !freenet) return false;
    [xray, host, dns, freenet].forEach(node => summary.appendChild(node));
    summary.dataset.vpnOrder = 'xray-vpn-dns-freenet';
    return true;
  }
  function icon() {
    return '<span class="fnv2-globe" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c2.3 2.5 3.5 5.5 3.5 9S14.3 18.5 12 21M12 3C9.7 5.5 8.5 8.5 8.5 12S9.7 18.5 12 21"/></svg></span>';
  }

  let host, toggle, panel, currentFlag, currentCountry, currentProfile;
  function buildShell() {
    q('#fnVpnPickerPopover')?.remove();
    q('#fnVpnPickerHost')?.remove();

    host = document.createElement('div');
    host.id = 'fnVpnPickerHost';
    host.className = 'fn-vpn-picker-host fnv2-host';
    host.innerHTML = '<button id="fnVpnPickerToggle" type="button" aria-haspopup="dialog" aria-expanded="false">' +
      icon() +
      '<span class="fnv2-chip-copy"><small>VPN</small><span class="fnv2-chip-line"><span id="fnVpnPickerCountryFlag" class="flag-icon flag-unknown" aria-hidden="true"></span><span id="fnVpnPickerCountryName">VPN</span></span></span>' +
      '</button>';

    panel = document.createElement('section');
    panel.id = 'fnVpnPickerPopover';
    panel.setAttribute('role','dialog');
    panel.setAttribute('aria-label','Выбор VPN-сервера');
    panel.hidden = true;
    panel.innerHTML = '<header class="fnv2-head"><div><strong>VPN-сервер</strong><span>Выберите Extra-профиль — FreeNet проверит его до подключения</span></div><button id="fnVpnPickerClose" class="fnv2-close" type="button" aria-label="Закрыть">×</button></header>' +
      '<div class="fnv2-body"><div class="fnv2-current"><span id="fnv2CurrentFlag" class="flag-icon flag-unknown" aria-hidden="true"></span><div class="fnv2-current-copy"><small>Сейчас подключено</small><strong id="fnv2CurrentCountry">VPN</strong><span id="fnv2CurrentProfile">Текущий профиль</span></div><span class="fnv2-current-state">Подключено</span></div><div id="fnv2Controls"></div></div>';
    document.body.appendChild(panel);

    const summary = q('.overview-approved-top.fn-shell-summary') || q('#overviewApprovedTop');
    if (summary) summary.appendChild(host);
    toggle = q('#fnVpnPickerToggle', host);
    currentFlag = q('#fnv2CurrentFlag', panel);
    currentCountry = q('#fnv2CurrentCountry', panel);
    currentProfile = q('#fnv2CurrentProfile', panel);

    toggle.addEventListener('click', event => {
      event.stopPropagation();
      panel.hidden ? open() : close();
    });
    q('#fnVpnPickerClose', panel).addEventListener('click', close);
    return orderTopbar(host);
  }

  function moveControls() {
    const controls = q('#fnv2Controls', panel);
    const manual = q('#bestServerAdvanced');
    if (!controls || !manual) return false;
    if (manual.parentNode !== controls) controls.appendChild(manual);
    manual.classList.add('fnv2-picker-content');
    const exact = q('#exactConnectRow');
    if (exact && exact.parentNode !== manual) manual.appendChild(exact);
    const guard = q('#quickNetworkGuard');
    if (guard && guard.parentNode !== manual) manual.appendChild(guard);
    return true;
  }

  function renderProfiles() {
    const menu = q('#profilesMenu');
    const search = q('#profileSearch');
    if (!menu || !search) return;
    const query = normalize(search.value);
    const list = profiles();
    const selected = selectedID();
    const filtered = list.filter(profile => {
      if (!query) return true;
      const code = profileCode(profile);
      const hay = normalize([
        profile.id, profile.name, profile.label, profile.country, profile.city,
        profile.country_code, endpoint(profile), countryNames[code] || ''
      ].join(' '));
      return hay.includes(query);
    });
    menu.replaceChildren();
    if (!list.length) {
      const empty = document.createElement('div');
      empty.className = 'fnv2-empty';
      empty.textContent = 'Список Extra-профилей пока не загружен. FreeNet попробует получить его автоматически.';
      menu.appendChild(empty);
      return;
    }
    if (!filtered.length) {
      const empty = document.createElement('div');
      empty.className = 'fnv2-empty';
      empty.textContent = 'Ничего не найдено. Введите страну, город или endpoint.';
      menu.appendChild(empty);
      return;
    }
    filtered.slice(0,100).forEach(profile => {
      const code = profileCode(profile);
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'profile-option';
      button.dataset.profileId = String(profile.id || '');
      button.setAttribute('role','option');
      button.setAttribute('aria-selected', String(String(profile.id || '') === selected));
      const copy = document.createElement('span');
      copy.className = 'fnv2-profile-copy';
      const main = document.createElement('span');
      main.className = 'profile-option-main';
      main.textContent = clean(profile.name || profile.label || profileCountryName(profile, code));
      const ep = document.createElement('span');
      ep.className = 'profile-option-endpoint';
      ep.textContent = endpoint(profile) || '—';
      copy.append(main, ep);
      button.append(flagNode(code), copy);
      button.addEventListener('click', () => {
        if (typeof selectProviderProfile === 'function') selectProviderProfile(profile);
        requestAnimationFrame(() => {
          moveControls();
          renderProfiles();
        });
      });
      menu.appendChild(button);
    });
  }

  function installRenderer() {
    try {
      renderProfileOptions = renderProfiles;
      if (typeof openProfileMenu === 'function') {
        const openLegacy = openProfileMenu;
        openProfileMenu = function() {
          const menu = q('#profilesMenu');
          if (menu) { menu.hidden = false; menu.removeAttribute('aria-hidden'); }
          renderProfiles();
          return menu || openLegacy();
        };
      }
    } catch (_) {}
    const search = q('#profileSearch');
    if (search && !search.dataset.fnv2Bound) {
      search.dataset.fnv2Bound = '1';
      search.addEventListener('input', renderProfiles);
    }
  }

  function positionPanel() {
    if (!panel || panel.hidden || !toggle || window.innerWidth <= 760) return;
    const r = toggle.getBoundingClientRect();
    const width = Math.min(620, window.innerWidth - 24);
    panel.style.width = width + 'px';
    const left = Math.max(12, Math.min(window.innerWidth - width - 12, r.right - width));
    panel.style.left = left + 'px';
    panel.style.right = 'auto';
    panel.style.bottom = 'auto';
    panel.style.top = (r.bottom + 10) + 'px';
    requestAnimationFrame(() => {
      const h = panel.getBoundingClientRect().height;
      if (r.bottom + 10 + h > window.innerHeight - 12) {
        panel.style.top = Math.max(12, r.top - h - 10) + 'px';
      }
    });
  }

  async function syncCurrent(status) {
    let live = status || null;
    if (!live) {
      try { live = typeof lastStatus !== 'undefined' ? lastStatus : null; } catch (_) {}
    }
    if (!live) {
      try {
        const response = await fetch('/api/status', {cache:'no-store'});
        if (response.ok) live = await response.json();
      } catch (_) {}
    }
    const id = currentIdentity(live || {});
    const chipFlag = q('#fnVpnPickerCountryFlag', host);
    const chipName = q('#fnVpnPickerCountryName', host);
    [chipFlag, currentFlag].forEach(flag => {
      if (!flag) return;
      flag.className = 'flag-icon ' + (id.code ? 'flag-' + id.code : 'flag-unknown');
      flag.hidden = false;
    });
    if (chipName) chipName.textContent = id.country;
    if (currentCountry) currentCountry.textContent = id.country;
    if (currentProfile) currentProfile.textContent = id.profile || 'Текущий VPN';
    if (toggle) toggle.setAttribute('aria-label','VPN · ' + id.country);
  }

  function open() {
    if (!panel) return;
    panel.hidden = false;
    toggle.setAttribute('aria-expanded','true');
    moveControls();
    installRenderer();
    renderProfiles();
    syncCurrent();
    positionPanel();
    const search = q('#profileSearch');
    if (search) requestAnimationFrame(() => search.focus({preventScroll:true}));
  }
  function close() {
    if (!panel) return;
    panel.hidden = true;
    toggle?.setAttribute('aria-expanded','false');
  }

  function reconcile() {
    removeLegacyPickerStyles();
    installStyles();
    if (!host || !document.documentElement.contains(host)) buildShell();
    orderTopbar(host);
    moveControls();
    installRenderer();
    renderProfiles();
    syncCurrent();
  }

  function boot() {
    removeLegacyPickerStyles();
    installStyles();
    buildShell();
    reconcile();
    document.addEventListener('freenet:status-updated', event => syncCurrent(event.detail));
    document.addEventListener('freenet:controls-busy', () => { moveControls(); syncCurrent(); });
    document.addEventListener('freenet:settings-v3-updated', () => { orderTopbar(host); syncCurrent(); });
    document.addEventListener('click', event => {
      if (!panel || panel.hidden) return;
      if (panel.contains(event.target) || host.contains(event.target)) return;
      close();
    }, true);
    document.addEventListener('keydown', event => {
      if (event.key === 'Escape' && panel && !panel.hidden) { close(); toggle?.focus(); }
    });
    window.addEventListener('resize', positionPanel);
    window.addEventListener('scroll', positionPanel, true);

    let frames = 0;
    const settle = () => {
      reconcile();
      frames += 1;
      if (frames < 24) requestAnimationFrame(settle);
    };
    requestAnimationFrame(settle);

    const observer = new MutationObserver(() => {
      if (q('#exactConnectRow') && moveControls()) observer.disconnect();
    });
    observer.observe(document.body, {childList:true, subtree:true});
    setTimeout(() => observer.disconnect(), 5000);
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', boot, {once:true});
  else boot();
})();
