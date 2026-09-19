// Post-v0.3.86 UI/runtime hardening: preserve the last known good
// Extra-profile list in the browser, keep the VPN selector usable after a
// failed refresh, and align the topbar/settings visual system. Presentation
// only: no direct VPN/DNS/routing/Xray mutation is performed here.
(() => {
  const styleID = 'freenetTopbarSettingsProfileCacheStyles';
  const cacheKey = 'freenet-extra-profiles-last-good-v1';
  const patchFlag = 'freenetProfileCacheFallbackPatch';
  const renderFlag = 'freenetCachedSelectorRenderer';

  const q = (selector, root = document) => root.querySelector(selector);

  function normalize(value) {
    return String(value || '')
      .normalize('NFKD')
      .replace(/[\u0300-\u036f]/g, '')
      .replace(/ё/g, 'е')
      .toLowerCase()
      .trim();
  }

  const aliases = {
    de: 'de germany deutschland german германия герман франкфурт frankfurt',
    nl: 'nl netherlands holland нидерланды голландия амстердам amsterdam',
    pl: 'pl poland польша варшава warsaw',
    fi: 'fi finland финляндия хельсинки helsinki',
    fr: 'fr france франция париж paris',
    gb: 'gb uk united kingdom britain england великобритания англия лондон london',
    us: 'us usa united states america сша америка new york los angeles',
    ae: 'ae uae emirates оаэ эмираты дубай dubai',
    jp: 'jp japan япония токио tokyo',
    kr: 'kr korea корея сеул seoul',
    sg: 'sg singapore сингапур',
    tr: 'tr turkey turkiye турция стамбул istanbul',
    ca: 'ca canada канада toronto montreal',
    au: 'au australia австралия sydney',
    se: 'se sweden швеция stockholm стокгольм',
    no: 'no norway норвегия oslo осло',
    dk: 'dk denmark дания copenhagen копенгаген',
    ch: 'ch switzerland швейцария zurich цюрих',
    at: 'at austria австрия vienna вена',
    cz: 'cz czechia чехия prague прага',
    sk: 'sk slovakia словакия bratislava братислава'
  };

  function endpoint(profile) {
    try {
      if (typeof formatProfileEndpoint === 'function') return formatProfileEndpoint(profile);
    } catch (_) {}
    const address = String(profile && profile.address || '');
    const host = address.includes(':') ? `[${address}]` : address;
    const port = profile && profile.port ? `:${profile.port}` : '';
    return `${host}${port}`;
  }

  function cleanLabel(value) {
    return String(value || '')
      .replace(/^[\u{1F1E6}-\u{1F1FF}]{2}\s*/u, '')
      .replace(/^[A-Za-z]{2}\s+/, '')
      .trim();
  }

  function cloneProfiles(profiles) {
    if (!Array.isArray(profiles)) return [];
    return profiles.map(profile => ({
      id: String(profile && profile.id || ''),
      name: String(profile && profile.name || profile && profile.label || 'Extra-профиль'),
      country_code: String(profile && profile.country_code || ''),
      address: String(profile && profile.address || ''),
      port: Number(profile && profile.port || 0)
    })).filter(profile => profile.id && profile.address && profile.port > 0 && profile.port <= 65535).slice(0, 100);
  }

  function readCache() {
    try {
      const parsed = JSON.parse(localStorage.getItem(cacheKey) || '{}');
      const profiles = cloneProfiles(parsed.profiles);
      if (!profiles.length) return null;
      return {profiles, ts: Number(parsed.ts || 0)};
    } catch (_) {
      return null;
    }
  }

  function writeCache(profiles) {
    const safe = cloneProfiles(profiles);
    if (!safe.length) return;
    try {
      localStorage.setItem(cacheKey, JSON.stringify({ts: Date.now(), profiles: safe}));
    } catch (_) {}
  }

  function cachedPlan(plan, cache) {
    const next = Object.assign({}, plan || {});
    next.extra_profiles = cache.profiles;
    next.profiles_error = next.profiles_error || 'используется последний успешный список Extra-профилей';
    next.profiles_stale = true;
    return next;
  }

  function setFallbackNotice(cache, reason) {
    const message = `Новый список Extra-профилей не получен. Используется последний успешный список: ${cache.profiles.length}.`;
    const profilesError = q('#profilesError');
    if (profilesError) {
      profilesError.textContent = reason ? `${message} Причина: ${reason}.` : message;
      profilesError.className = 'notice show bad';
      profilesError.hidden = false;
    }
    const subscriptionNotice = q('#subscriptionNotice');
    if (subscriptionNotice && subscriptionNotice.classList.contains('bad')) {
      subscriptionNotice.textContent = `${message} Конфигурация не изменена.`;
    }
  }

  function patchExtraProfileRenderer() {
    try {
      if (typeof renderExtraProfiles !== 'function') return;
      if (renderExtraProfiles[patchFlag]) return;
      const previous = renderExtraProfiles;
      const wrapped = function(plan) {
        const incoming = cloneProfiles(plan && plan.extra_profiles);
        if (incoming.length && !(plan && plan.profiles_error)) {
          writeCache(incoming);
          return previous(plan);
        }
        const cache = readCache();
        if (cache && cache.profiles.length && (!incoming.length || plan && plan.profiles_error)) {
          const result = previous(cachedPlan(plan, cache));
          setFallbackNotice(cache, String(plan && (plan.profiles_error || plan.error) || ''));
          return result;
        }
        return previous(plan);
      };
      wrapped[patchFlag] = true;
      renderExtraProfiles = wrapped;
    } catch (_) {}
  }

  function currentProfilesWithCache() {
    try {
      if (Array.isArray(extraProfiles) && extraProfiles.length) {
        return {profiles: extraProfiles, stale: false};
      }
    } catch (_) {}
    const cache = readCache();
    return cache ? {profiles: cache.profiles, stale: true} : {profiles: [], stale: false};
  }

  function profileCode(profile) {
    const direct = normalize(profile && profile.country_code || '');
    if (/^[a-z]{2}$/.test(direct)) return direct;
    const text = normalize([profile && profile.name, profile && profile.address, endpoint(profile)].join(' '));
    for (const [code, words] of Object.entries(aliases)) {
      if (words.split(/\s+/).some(word => word && text.includes(word))) return code;
    }
    return '';
  }

  function searchText(profile) {
    const code = profileCode(profile);
    return normalize([
      profile && profile.id,
      profile && profile.name,
      profile && profile.country_code,
      profile && profile.address,
      profile && profile.port,
      endpoint(profile),
      code,
      aliases[code] || ''
    ].join(' '));
  }

  function renderCachedProfileOptions() {
    const menu = q('#profilesMenu');
    const search = q('#profileSearch');
    const triggerText = q('#profilesTriggerText');
    if (!menu || !search) return;

    const state = currentProfilesWithCache();
    const query = normalize(search.value);
    const selectedID = (() => { try { return String(selectedProviderID || ''); } catch (_) { return ''; } })();
    const selectedName = (() => { try { return cleanLabel(selectedProviderName || ''); } catch (_) { return ''; } })();
    const filtered = state.profiles.filter(profile => !query || searchText(profile).includes(query));

    menu.replaceChildren();
    if (triggerText) triggerText.textContent = selectedName || 'Выбрать VPN-сервер';

    if (!state.profiles.length) {
      const empty = document.createElement('div');
      empty.className = 'fn-selector-empty hint';
      empty.textContent = 'Extra-профили не загружены. Откройте «Подписка» и обновите список, затем вернитесь к выбору VPN-сервера.';
      menu.appendChild(empty);
      return;
    }

    if (state.stale) {
      const stale = document.createElement('div');
      stale.className = 'fn-selector-cache-note';
      stale.textContent = `Используется последний успешный список Extra-профилей: ${state.profiles.length}.`;
      menu.appendChild(stale);
    }

    if (!filtered.length) {
      const empty = document.createElement('div');
      empty.className = 'fn-selector-empty hint';
      empty.textContent = `По запросу «${search.value.trim()}» ничего не найдено. Попробуйте Germany, DE, город или endpoint.`;
      menu.appendChild(empty);
      return;
    }

    filtered.slice(0, 100).forEach(profile => {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'profile-option';
      button.setAttribute('role', 'option');
      button.setAttribute('aria-selected', profile.id === selectedID ? 'true' : 'false');
      if (profile.id === selectedID) button.classList.add('selected');

      const main = document.createElement('span');
      main.className = 'profile-option-main';
      main.textContent = cleanLabel(profile.name) || 'VPN-сервер';
      const meta = document.createElement('span');
      meta.className = 'profile-option-meta';
      meta.textContent = endpoint(profile);
      button.append(main, meta);
      button.addEventListener('click', () => {
        if (typeof selectProviderProfile === 'function') selectProviderProfile(profile);
      });
      menu.appendChild(button);
    });
  }

  function patchProfileRenderer() {
    try {
      if (typeof renderProfileOptions !== 'function') return;
      if (renderProfileOptions[renderFlag]) return;
      const wrapped = function() { renderCachedProfileOptions(); };
      wrapped[renderFlag] = true;
      renderProfileOptions = wrapped;
      renderProfileOptions();
    } catch (_) {}
  }

  function injectStyles() {
    if (q(`#${styleID}`)) return;
    const style = document.createElement('style');
    style.id = styleID;
    style.textContent = `
      .overview-approved-top.fn-shell-summary{align-items:center!important;gap:9px!important}
      .topbar #bestServerAdvanced.fn-topbar-vpn-picker{width:230px!important;min-width:0!important;max-width:230px!important;flex:0 0 230px!important;margin-left:0!important}
      .topbar #fnVpnPickerTrigger{width:100%!important;min-width:0!important;max-width:230px!important;height:50px!important;min-height:50px!important;border-radius:11px!important;padding:6px 10px!important;display:flex!important;align-items:center!important}
      #fnVpnPickerTrigger .fn-vpn-picker-main,#fnVpnPickerTrigger strong{max-width:142px!important;overflow:hidden!important;text-overflow:ellipsis!important;white-space:nowrap!important}
      #fnVpnPickerTrigger .fn-vpn-picker-copy,#fnVpnPickerTrigger span{font-size:10px!important;line-height:1.05!important}
      #fnVpnPickerTrigger strong{font-size:13px!important;line-height:1.15!important;font-weight:800!important}
      .overview-approved-fact.fn-shell-fact,#topFreenetUpdate{height:50px!important;min-height:50px!important;border-radius:11px!important;padding:6px 11px!important;display:flex!important;align-items:center!important}
      .overview-approved-fact.fn-shell-fact{min-width:112px!important}
      .overview-approved-fact.fn-shell-fact span,.overview-approved-fact.fn-shell-fact small,#topFreenetUpdate span{font-size:10px!important;line-height:1.05!important;font-weight:760!important}
      .overview-approved-fact.fn-shell-fact strong,#topFreenetUpdate strong{font-size:13px!important;line-height:1.15!important;font-weight:800!important}
      .overview-approved-fact.fn-shell-fact .fn-shell-icon,#topFreenetUpdate .fn-shell-icon,#fnVpnPickerTrigger .fn-shell-icon{width:30px!important;height:30px!important;min-width:30px!important}
      #fnVpnPickerBody #bestServerAdvanced.fn-topbar-vpn-picker{width:100%!important;max-width:none!important;flex:auto!important}
      #profilesMenu .fn-selector-cache-note{padding:10px 12px!important;margin:0 0 8px!important;border:1px solid rgba(241,184,75,.32)!important;border-radius:10px!important;background:rgba(100,72,18,.22)!important;color:#ffd98a!important;font-size:11.5px!important;line-height:1.35!important}
      body:has([data-page-view="settings"].active) .fn3-grid{align-items:stretch!important}
      body:has([data-page-view="settings"].active) .fn3-left{display:flex!important;flex-direction:column!important;min-height:100%!important}
      body:has([data-page-view="settings"].active) .fn3-left>.fn3-card{flex:1 1 auto!important;min-height:0!important}
      body:has([data-page-view="settings"].active) .fn3-extra-grid{align-items:stretch!important}
      body:has([data-page-view="settings"].active) .fn3-extra-card{display:flex!important;flex-direction:column!important;min-height:0!important}
      body:has([data-page-view="settings"].active) .fn3-extra-action,body:has([data-page-view="settings"].active) .fn3-backup-actions .btn{height:56px!important;min-height:56px!important;display:flex!important;align-items:center!important;justify-content:center!important;text-align:center!important;line-height:1.16!important}
      body:has([data-page-view="settings"].active) .fn3-backup-actions{display:grid!important;grid-template-columns:1fr 1fr!important;gap:10px!important;margin-top:auto!important;align-items:stretch!important}
      body:has([data-page-view="settings"].active) .fn3-extra-card .fn3-extra-action{margin-top:auto!important;width:100%!important}
      @media(max-width:900px){.topbar #bestServerAdvanced.fn-topbar-vpn-picker{width:50px!important;max-width:50px!important;flex-basis:50px!important}.topbar #fnVpnPickerTrigger{width:50px!important;max-width:50px!important;padding-left:9px!important;padding-right:9px!important}.topbar #fnVpnPickerTrigger .fn-vpn-picker-main,.topbar #fnVpnPickerTrigger .fn-vpn-picker-copy{display:none!important}}
    `;
    document.head.appendChild(style);
  }

  function mount() {
    injectStyles();
    patchExtraProfileRenderer();
    patchProfileRenderer();
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mount, {once: true});
  else mount();
  requestAnimationFrame(mount);
  document.addEventListener('freenet:controls-busy', mount);
})();


// Issue #590: compact unified topbar, centered VPN selector modal,
// clickable brand/favicon, Settings spacing, and live subscription schedule.
// Presentation/state reconciliation only. Existing safe apply flows are reused.
(() => {
  'use strict';
  if (window.__freenetIssue590Polish) return;
  window.__freenetIssue590Polish = true;

  const q = (selector, root = document) => root.querySelector(selector);

  function installIssue590Styles() {
    if (q('#freenetIssue590Styles')) return;
    const style = document.createElement('style');
    style.id = 'freenetIssue590Styles';
    style.textContent = `
      .topbar.overview-approved{gap:10px!important}
      #fnVpnPickerHost{position:relative!important;flex:0 0 auto!important;min-width:0!important;max-width:none!important;margin-left:auto!important;align-self:center!important;z-index:120!important}
      #fnVpnPickerToggle,.fn-xray-topbar,.overview-approved-fact.fn-shell-fact,#topFreenetUpdate{
        height:50px!important;min-height:50px!important;box-sizing:border-box!important;
        border:1px solid #315276!important;border-radius:11px!important;
        background:linear-gradient(180deg,#0d1d30,#0a1727)!important;
        box-shadow:inset 0 1px rgba(255,255,255,.018)!important;
      }
      #fnVpnPickerToggle{width:196px!important;min-width:196px!important;max-width:196px!important;display:grid!important;grid-template-columns:26px minmax(0,1fr) 7px 14px!important;gap:8px!important;padding:6px 10px!important}
      #fnVpnPickerToggle:hover,#fnVpnPickerToggle.open,.fn-xray-topbar:hover,.overview-approved-fact.fn-shell-fact:hover,#topFreenetUpdate:hover{border-color:#5277a5!important;background:linear-gradient(180deg,#12263e,#0c1b2d)!important}
      #fnVpnPickerToggle.open{border-color:#6398ff!important;box-shadow:0 0 0 2px rgba(80,139,255,.10)!important}
      .fn-vpn-picker-icon,.fn-xray-topbar-icon,.fn-shell-fact>.fn-top-fact-icon,.fn-version-icon{display:grid!important;place-items:center!important;flex:0 0 28px!important;width:28px!important;height:28px!important;color:#67a3ff!important}
      .fn-vpn-picker-icon svg,.fn-xray-topbar-icon svg,.fn-shell-fact>.fn-top-fact-icon svg{width:23px!important;height:23px!important}
      .fn-vpn-picker-copy,.fn-shell-fact-copy,.fn-version-copy{display:flex!important;flex-direction:column!important;justify-content:center!important;gap:2px!important;min-width:0!important;text-align:left!important}
      .fn-vpn-picker-copy>span,.fn-xray-topbar>span:not(.fn-xray-topbar-icon),.fn-shell-fact-copy>span,.fn-version-copy small{font-size:10px!important;line-height:1.05!important;color:#8da4c2!important;font-weight:720!important;letter-spacing:0!important;text-transform:none!important;white-space:nowrap!important}
      .fn-vpn-picker-copy>strong,.fn-xray-topbar>strong,.fn-shell-fact-copy>strong,.fn-version-copy strong{font-size:13px!important;line-height:1.1!important;color:#f5f8ff!important;font-weight:800!important;white-space:nowrap!important}
      .fn-vpn-picker-copy>strong{overflow:hidden!important;text-overflow:ellipsis!important}
      .fn-xray-topbar{display:grid!important;grid-template-columns:28px minmax(0,1fr)!important;grid-template-rows:auto auto!important;column-gap:9px!important;align-items:center!important;min-width:112px!important;padding:6px 11px!important;text-align:left!important}
      .fn-xray-topbar-icon{grid-row:1/3!important}
      .overview-approved-fact.fn-shell-fact{min-width:116px!important;padding:6px 11px!important}
      #topFreenetUpdate{min-width:112px!important;padding:6px 11px!important}
      .overview-approved-top.fn-shell-summary{gap:9px!important}
      .sidebar>.brand{cursor:pointer!important;user-select:none!important;border-radius:10px!important}
      .sidebar>.brand:focus-visible{outline:2px solid #5b8cff!important;outline-offset:2px!important}

      #fnVpnPickerBackdrop{display:none;position:fixed;inset:0;z-index:1390;background:rgba(2,8,16,.78);backdrop-filter:blur(12px)}
      html.fn-vpn-picker-open #fnVpnPickerBackdrop{display:block!important}
      html.fn-vpn-picker-open,html.fn-vpn-picker-open body{overflow:hidden!important}
      #fnVpnPickerPopover{
        position:fixed!important;left:50%!important;top:50%!important;right:auto!important;
        transform:translate(-50%,-50%)!important;width:min(720px,calc(100vw - 34px))!important;
        max-height:min(84vh,760px)!important;overflow:auto!important;z-index:1400!important;
        border-radius:18px!important;border-color:#355273!important;
        background:linear-gradient(180deg,#11243a,#091725)!important;
        box-shadow:0 34px 110px rgba(0,0,0,.58)!important;
      }
      #fnVpnPickerPopover .fn-vpn-picker-head{padding:18px 20px 15px!important;background:rgba(15,34,55,.98)!important}
      #fnVpnPickerPopover .fn-vpn-picker-head strong{font-size:19px!important;letter-spacing:-.02em!important}
      #fnVpnPickerPopover .fn-vpn-picker-head span{font-size:11.5px!important}
      #fnVpnPickerPopover .fn-vpn-picker-body{padding:18px 20px 20px!important}
      #fnVpnPickerPopover #profilesList.profiles{grid-template-columns:minmax(0,.9fr) minmax(0,1.1fr)!important;gap:12px!important}
      #fnVpnPickerPopover #profileSearch,#fnVpnPickerPopover #profilesTrigger{height:46px!important;min-height:46px!important}
      #fnVpnPickerPopover #profilesMenu{max-height:330px!important}
      #fnVpnPickerPopover #exactConnectRow{grid-template-columns:minmax(0,1fr) 150px!important}
      #fnVpnPickerPopover #exactConnectRow .btn{min-height:44px!important}

      body.fn-settings-accepted .fn3-left>.fn3-card:first-child{display:flex!important;flex-direction:column!important}
      body.fn-settings-accepted .fn3-auto-actions{margin-top:auto!important;padding-top:24px!important}
      body.fn-settings-accepted .fn3-extra-card{display:flex!important;flex-direction:column!important}
      body.fn-settings-accepted .fn3-extra-card .fn3-extra-meta{margin-bottom:18px!important}
      body.fn-settings-accepted .fn3-extra-card .fn3-extra-action{margin-top:auto!important;height:56px!important;min-height:56px!important}
      body.fn-settings-accepted .fn3-backup-actions{margin-top:auto!important;display:grid!important;grid-template-columns:1fr 1fr!important;gap:10px!important}
      body.fn-settings-accepted .fn3-backup-actions .btn{height:56px!important;min-height:56px!important;align-items:center!important;justify-content:center!important}
      body.fn-settings-accepted .fn3-extra-grid{align-items:stretch!important}

      @media(max-width:1040px){
        #fnVpnPickerToggle{width:170px!important;min-width:170px!important;max-width:170px!important}
      }
      @media(max-width:820px){
        #fnVpnPickerToggle{width:50px!important;min-width:50px!important;max-width:50px!important;grid-template-columns:1fr!important;padding:0!important;place-items:center!important}
        #fnVpnPickerToggle .fn-vpn-picker-copy,#fnVpnPickerToggle .fn-vpn-picker-state,#fnVpnPickerToggle .fn-vpn-picker-chevron{display:none!important}
        #fnVpnPickerPopover #profilesList.profiles{grid-template-columns:1fr!important}
      }
    `;
    document.head.appendChild(style);
  }

  function installBrandNavigation() {
    const brand = q('.sidebar>.brand');
    if (!brand || brand.dataset.issue590Nav === '1') return;
    brand.dataset.issue590Nav = '1';
    brand.setAttribute('role', 'button');
    brand.setAttribute('tabindex', '0');
    brand.setAttribute('title', 'Открыть Обзор');
    const openOverview = () => {
      try {
        if (typeof setPage === 'function') setPage('overview');
        else location.hash = '#overview';
      } catch (_) { location.hash = '#overview'; }
    };
    brand.addEventListener('click', openOverview);
    brand.addEventListener('keydown', event => {
      if (event.key === 'Enter' || event.key === ' ') {
        event.preventDefault();
        openOverview();
      }
    });
  }

  function installBrandFavicon() {
    const href = "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 32 32'%3E%3Crect width='32' height='32' rx='7' fill='%2307101b'/%3E%3Cg fill='none' stroke='%2372a8ff' stroke-width='2.2' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='M16 3 29 16 16 29 3 16 16 3Z'/%3E%3Cpath d='M11 21V11l10 10V11'/%3E%3C/g%3E%3C/svg%3E";
    let icon = q('link[rel="icon"]');
    if (!icon) {
      icon = document.createElement('link');
      icon.rel = 'icon';
      document.head.appendChild(icon);
    }
    icon.type = 'image/svg+xml';
    icon.href = href;
  }

  function installVpnModal() {
    const popover = q('#fnVpnPickerPopover');
    if (!popover) return false;
    if (popover.parentNode !== document.body) document.body.appendChild(popover);
    let backdrop = q('#fnVpnPickerBackdrop');
    if (!backdrop) {
      backdrop = document.createElement('div');
      backdrop.id = 'fnVpnPickerBackdrop';
      document.body.insertBefore(backdrop, popover);
      backdrop.addEventListener('click', () => q('#fnVpnPickerClose')?.click());
    }
    if (document.documentElement.dataset.issue590Esc !== '1') {
      document.documentElement.dataset.issue590Esc = '1';
      document.addEventListener('keydown', event => {
        if (event.key === 'Escape' && document.documentElement.classList.contains('fn-vpn-picker-open')) {
          q('#fnVpnPickerClose')?.click();
        }
      });
    }
    return true;
  }

  function formatNextRun(value) {
    if (!value) return '';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return '';
    try {
      return new Intl.DateTimeFormat('ru-RU', {
        day:'2-digit', month:'2-digit', hour:'2-digit', minute:'2-digit'
      }).format(date).replace(',', '');
    } catch (_) {
      return date.toLocaleString();
    }
  }

  function intervalCopy(value) {
    const labels = { '30m':'каждые 30 минут', '1h':'раз в час', '3h':'раз в 3 часа', '6h':'раз в 6 часов', '12h':'раз в 12 часов', '24h':'раз в 24 часа' };
    return labels[String(value || '').trim()] || 'по расписанию';
  }

  function applySubscriptionSchedule(schedule) {
    const card = q('.subscription-approved .fn-sub-next');
    if (!card) return;
    const value = q('.fn-sub-value', card);
    const meta = q('.fn-sub-meta', card);
    if (!value || !meta) return;
    const enabled = !!(schedule && schedule.enabled);
    if (!enabled) {
      value.textContent = 'Вручную';
      meta.textContent = 'Автообновление списка Extra-профилей выключено';
      return;
    }
    const next = formatNextRun(schedule.next_run);
    value.textContent = next || 'Включено';
    meta.textContent = `Автообновление · ${intervalCopy(schedule.interval)}`;
  }

  async function refreshSubscriptionSchedule() {
    try {
      const response = await fetch('/api/settings-v3', {cache:'no-store'});
      if (!response.ok) return;
      const data = await response.json();
      if (!data || data.success === false) return;
      applySubscriptionSchedule(data.subscription || {});
    } catch (_) {}
  }

  function installSubscriptionScheduleSync() {
    if (document.documentElement.dataset.issue590Schedule === '1') return;
    document.documentElement.dataset.issue590Schedule = '1';
    document.addEventListener('freenet:settings-v3-updated', event => {
      applySubscriptionSchedule(event.detail && event.detail.subscription || {});
    });
    document.addEventListener('click', event => {
      if (event.target.closest?.('.nav-btn[data-page="subscription"]')) {
        queueMicrotask(refreshSubscriptionSchedule);
      }
    });
    if (q('[data-page-view="subscription"]')) refreshSubscriptionSchedule();
  }

  function mountIssue590() {
    installIssue590Styles();
    installBrandNavigation();
    installBrandFavicon();
    installVpnModal();
    installSubscriptionScheduleSync();
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mountIssue590, {once:true});
  else mountIssue590();
  requestAnimationFrame(mountIssue590);
  document.addEventListener('freenet:controls-busy', mountIssue590);
})();


// Issue #601: real-router geometry correction after v0.3.88.
// Keep the topbar as one compact right-side cluster and make the exact VPN
// selector a single-column dialog. Presentation only; safe-connect semantics
// remain owned by the existing selector/reconcile layer.
(() => {
  'use strict';
  if (window.__freenetIssue601Geometry) return;
  window.__freenetIssue601Geometry = true;

  const q = (selector, root = document) => root.querySelector(selector);

  function installIssue601Styles() {
    if (q('#freenetIssue601Styles')) return;
    const style = document.createElement('style');
    style.id = 'freenetIssue601Styles';
    style.textContent = `
      /* One right-side topbar cluster: only the VPN host consumes free space. */
      .topbar.overview-approved{justify-content:flex-start!important}
      #fnVpnPickerHost{margin-left:auto!important;flex:0 0 220px!important;width:220px!important;min-width:220px!important;max-width:220px!important}
      #fnVpnPickerToggle{width:220px!important;min-width:220px!important;max-width:220px!important}
      .overview-approved-top.fn-shell-summary{margin-left:0!important;gap:9px!important;flex:0 0 auto!important}

      /* Dialog geometry: search -> results -> selected state -> actions. */
      #fnVpnPickerPopover{
        width:min(660px,calc(100vw - 34px))!important;
        max-height:min(82vh,700px)!important;
        overflow:hidden!important;
      }
      #fnVpnPickerPopover .fn-vpn-picker-head{position:relative!important}
      #fnVpnPickerPopover .fn-vpn-picker-body{
        padding:16px 18px 18px!important;
        max-height:calc(82vh - 76px)!important;
        overflow:auto!important;
        overscroll-behavior:contain!important;
      }
      #fnVpnPickerPopover #bestServerAdvanced.fn-topbar-vpn-picker{display:block!important}
      #fnVpnPickerPopover #profilesList.profiles,
      #fnVpnPickerPopover #profilesList.profiles.show{
        display:grid!important;
        grid-template-columns:1fr!important;
        gap:10px!important;
        width:100%!important;
      }
      #fnVpnPickerPopover #profilesList .field{grid-column:1!important;width:100%!important}
      #fnVpnPickerPopover #profileSearch{width:100%!important;height:46px!important;min-height:46px!important}
      #fnVpnPickerPopover #profilesTrigger{display:none!important}
      #fnVpnPickerPopover .profile-combobox{grid-column:1!important;width:100%!important;margin:0!important}
      #fnVpnPickerPopover #profilesMenu{
        position:static!important;
        display:block;
        width:100%!important;
        max-height:270px!important;
        margin:0!important;
        overflow-y:auto!important;
        overscroll-behavior:contain!important;
        border-radius:11px!important;
        box-shadow:none!important;
      }
      #fnVpnPickerPopover #profilesMenu[hidden]{display:none}
      #fnVpnPickerPopover #profilesMenu .profile-option{min-height:50px!important}
      #fnVpnPickerPopover #profilesError{grid-column:1!important;margin:0!important}
      #fnVpnPickerPopover #selectedProfileCard.fn-selector-state{
        grid-column:1!important;
        width:100%!important;
        margin:0!important;
        min-height:68px!important;
      }
      #fnVpnPickerPopover #exactConnectRow{
        width:100%!important;
        margin:10px 0 0!important;
        grid-template-columns:minmax(0,1fr) 150px!important;
      }

      @media(max-width:1040px){
        #fnVpnPickerHost{flex-basis:190px!important;width:190px!important;min-width:190px!important;max-width:190px!important}
        #fnVpnPickerToggle{width:190px!important;min-width:190px!important;max-width:190px!important}
      }
      @media(max-width:820px){
        #fnVpnPickerHost{order:20!important;flex:1 0 100%!important;width:100%!important;min-width:0!important;max-width:none!important;margin-left:0!important}
        #fnVpnPickerToggle{width:100%!important;min-width:0!important;max-width:none!important;grid-template-columns:26px minmax(0,1fr) 7px 14px!important;padding:6px 10px!important}
        #fnVpnPickerToggle .fn-vpn-picker-copy,#fnVpnPickerToggle .fn-vpn-picker-state,#fnVpnPickerToggle .fn-vpn-picker-chevron{display:flex!important}
        #fnVpnPickerPopover .fn-vpn-picker-body{max-height:calc(88vh - 76px)!important}
        #fnVpnPickerPopover #exactConnectRow{grid-template-columns:1fr!important}
      }
    `;
    document.head.appendChild(style);
  }

  function keepDialogInsideHost() {
    const host = q('#fnVpnPickerHost');
    const popover = q('#fnVpnPickerPopover');
    if (!host || !popover) return false;
    if (popover.parentNode !== host) host.appendChild(popover);
    return true;
  }

  function openProfileResults() {
    if (!document.documentElement.classList.contains('fn-vpn-picker-open')) return;
    try { if (typeof renderProfileOptions === 'function') renderProfileOptions(); } catch (_) {}
    try { if (typeof openProfileMenu === 'function') openProfileMenu(); } catch (_) {}
  }

  function bindModalOpen() {
    const toggle = q('#fnVpnPickerToggle');
    if (!toggle || toggle.dataset.issue601Open === '1') return;
    toggle.dataset.issue601Open = '1';
    toggle.addEventListener('click', () => requestAnimationFrame(openProfileResults));
  }

  function mountIssue601() {
    installIssue601Styles();
    keepDialogInsideHost();
    bindModalOpen();
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mountIssue601, {once:true});
  else mountIssue601();
  requestAnimationFrame(mountIssue601);
  document.addEventListener('freenet:controls-busy', mountIssue601);
})();
