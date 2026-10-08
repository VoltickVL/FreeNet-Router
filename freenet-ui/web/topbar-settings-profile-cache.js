// Post-v0.3.86 UI/runtime hardening: preserve the last known good
// Extra-profile list in the browser, keep the VPN selector usable after a
// failed refresh, and align the topbar/settings visual system. Presentation
// only: no direct VPN/DNS/routing/Xray mutation is performed here.
(() => {
  const styleID = 'freenetTopbarSettingsProfileCacheStyles';
  const cacheKey = 'freenet-extra-profiles-last-good-v1';
  const patchFlag = 'freenetProfileCacheFallbackPatch';
  const renderFlag = 'freenetCachedSelectorRenderer';
  let catalogStale = false;

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
          catalogStale = false;
          writeCache(incoming);
          return previous(plan);
        }
        const cache = readCache();
        if (cache && cache.profiles.length && (!incoming.length || plan && plan.profiles_error)) {
          catalogStale = true;
          const result = previous(cachedPlan(plan, cache));
          setFallbackNotice(cache, String(plan && (plan.profiles_error || plan.error) || ''));
          return result;
        }
        catalogStale = false;
        return previous(plan);
      };
      wrapped[patchFlag] = true;
      renderExtraProfiles = wrapped;
      window.freenetPublishMeasuredProfileCatalog = function(profiles) {
        const incoming = cloneProfiles(profiles);
        if (!incoming.length) return false;
        catalogStale = false;
        writeCache(incoming);
        previous({extra_profiles: incoming});
        return true;
      };
      window.freenetProfileCatalogState = function() {
        const state = currentProfilesWithCache();
        return {profiles: cloneProfiles(state.profiles), stale: !!state.stale};
      };
    } catch (_) {}
  }

  function currentProfilesWithCache() {
    try {
      if (Array.isArray(extraProfiles) && extraProfiles.length) {
        return {profiles: extraProfiles, stale: catalogStale};
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
      button.dataset.profileId = profile.id;
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





      .overview-approved-fact.fn-shell-fact,#topFreenetUpdate{height:50px!important;min-height:50px!important;border-radius:11px!important;padding:6px 11px!important;display:flex!important;align-items:center!important}
      .overview-approved-fact.fn-shell-fact{min-width:112px!important}
      .overview-approved-fact.fn-shell-fact span,.overview-approved-fact.fn-shell-fact small,#topFreenetUpdate span{font-size:9px!important;line-height:1.05!important;font-weight:700!important}
      .overview-approved-fact.fn-shell-fact strong,#topFreenetUpdate strong{font-size:12px!important;line-height:1.25!important;font-weight:750!important}


      #profilesMenu .fn-selector-cache-note{padding:10px 12px!important;margin:0 0 8px!important;border:1px solid rgba(241,184,75,.32)!important;border-radius:10px!important;background:rgba(100,72,18,.22)!important;color:#ffd98a!important;font-size:11.5px!important;line-height:1.35!important}
      /* Settings cards must size to their content, also after folding.
         The former 100% / flex-grow rules left large empty collapsed panels. */
      body:has([data-page-view="settings"].active) .fn3-grid{align-items:start!important}
      body:has([data-page-view="settings"].active) .fn3-left{display:grid!important;grid-auto-rows:max-content!important;align-items:start!important;min-height:0!important}
      body:has([data-page-view="settings"].active) .fn3-left>.fn3-card{flex:none!important;min-height:0!important;height:auto!important}
      body:has([data-page-view="settings"].active) .fn3-extra-grid{align-items:start!important}
      body:has([data-page-view="settings"].active) .fn3-section-collapsed{height:auto!important;min-height:0!important;flex:none!important;align-self:start!important}
      body:has([data-page-view="settings"].active) .fn3-section-collapsed>.fn3-extra-title{margin-bottom:0!important}
      body:has([data-page-view="settings"].active) .fn3-extra-card{display:flex!important;flex-direction:column!important;min-height:0!important}
      body:has([data-page-view="settings"].active) .fn3-extra-action,body:has([data-page-view="settings"].active) .fn3-backup-actions .btn{height:56px!important;min-height:56px!important;display:flex!important;align-items:center!important;justify-content:center!important;text-align:center!important;line-height:1.16!important}
      body:has([data-page-view="settings"].active) .fn3-backup-actions{display:grid!important;grid-template-columns:1fr 1fr!important;gap:10px!important;margin-top:auto!important;align-items:stretch!important}
      body:has([data-page-view="settings"].active) .fn3-extra-card .fn3-extra-action{margin-top:auto!important;width:100%!important}
      @media(max-width:900px){}
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
      .topbar.overview-approved{gap:12px!important;height:82px!important;min-height:82px!important}
      .fn-xray-topbar,.overview-approved-fact.fn-shell-fact,#topFreenetUpdate{
        height:60px!important;min-height:60px!important;box-sizing:border-box!important;
        border:1px solid #315276!important;border-radius:11px!important;
        background:linear-gradient(180deg,#0d1d30,#0a1727)!important;
        box-shadow:inset 0 1px rgba(255,255,255,.018)!important;
      }
      .fn-xray-topbar:hover,.overview-approved-fact.fn-shell-fact:hover,#topFreenetUpdate:hover{border-color:#5277a5!important;background:linear-gradient(180deg,#12263e,#0c1b2d)!important}
      .fn-xray-topbar-icon,.fn-shell-fact>.fn-top-fact-icon,.fn-version-icon{display:grid!important;place-items:center!important;flex:0 0 30px!important;width:30px!important;height:30px!important;color:#67a3ff!important}
      .fn-xray-topbar-icon svg,.fn-shell-fact>.fn-top-fact-icon svg{width:23px!important;height:23px!important}
      .fn-shell-fact-copy,.fn-version-copy{display:flex!important;flex-direction:column!important;justify-content:center!important;gap:2px!important;min-width:0!important;text-align:left!important}
      /* Current VPN sets the typography contract: country is 12px/750.
         Xray, DNS and FreeNet must not overpower the VPN value. */
      .fn-xray-chip small,.fn-shell-fact-copy>span,.fn-version-copy small{font-size:9px!important;line-height:1.1!important;color:#8da4c2!important;font-weight:750!important;letter-spacing:0!important;text-transform:none!important;white-space:nowrap!important}
      .fn-xray-chip strong,.fn-shell-fact-copy>strong,.fn-version-copy strong{font-size:12px!important;line-height:1.25!important;color:#eef4ff!important;font-weight:750!important;white-space:nowrap!important}
      #xrayTopbarChip #xrayTopbarVersion,#overviewApprovedTop .fn-shell-fact-copy>strong,#topFreenetUpdate .fn-version-copy strong{font-size:12px!important;line-height:1.25!important;font-weight:750!important;letter-spacing:0!important}
      #xrayTopbarChip .fn-xray-chip small,#overviewApprovedTop .fn-shell-fact-copy>span,#topFreenetUpdate .fn-version-copy small{font-size:9px!important;line-height:1.1!important;font-weight:750!important}
      .fn-xray-topbar{display:grid!important;grid-template-columns:30px minmax(0,1fr)!important;grid-template-rows:1fr!important;column-gap:11px!important;align-items:center!important;min-width:148px!important;width:148px!important;padding:9px 14px!important;text-align:left!important}
      .fn-xray-topbar-icon{grid-row:auto!important}
      .overview-approved-fact.fn-shell-fact{min-width:166px!important;max-width:180px!important;padding:9px 14px!important}
      #topFreenetUpdate{min-width:148px!important;padding:9px 14px!important}
      .overview-approved-top.fn-shell-summary{gap:9px!important}
      .sidebar>.brand{cursor:pointer!important;user-select:none!important;border-radius:10px!important}
      .sidebar>.brand:focus-visible{outline:2px solid #5b8cff!important;outline-offset:2px!important}

      body.fn-settings-accepted .fn3-left>.fn3-card:first-child{display:flex!important;flex-direction:column!important}
      body.fn-settings-accepted .fn3-auto-actions{margin-top:auto!important;padding-top:24px!important}
      body.fn-settings-accepted .fn3-extra-card{display:flex!important;flex-direction:column!important}
      body.fn-settings-accepted .fn3-extra-card .fn3-extra-meta{margin-bottom:18px!important}
      body.fn-settings-accepted .fn3-extra-card .fn3-extra-action{margin-top:auto!important;height:56px!important;min-height:56px!important}
      body.fn-settings-accepted .fn3-backup-actions{margin-top:auto!important;display:grid!important;grid-template-columns:1fr 1fr!important;gap:10px!important}
      body.fn-settings-accepted .fn3-backup-actions .btn{height:56px!important;min-height:56px!important;align-items:center!important;justify-content:center!important}
      body.fn-settings-accepted .fn3-extra-grid{align-items:stretch!important}

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
      meta.textContent = 'Автопроверка списка Extra-профилей выключена';
    } else {
      const next = formatNextRun(schedule.next_run);
      value.textContent = next || 'Включено';
      meta.textContent = `Автопроверка · ${intervalCopy(schedule.interval)}`;
    }
    const result = String(schedule && schedule.result || '');
    const updaterState = q('#subscriptionUpdaterState');
    if (updaterState) {
      updaterState.textContent = result === 'success' ? 'Актуально' : result === 'failed' ? 'Ошибка' : 'Ожидает проверки';
      updaterState.classList.toggle('ok', result === 'success');
      updaterState.classList.toggle('bad', result === 'failed');
    }
    const lastNode = q('#fnSubscriptionLastAction');
    if (lastNode) {
      const strong = q('strong', lastNode);
      if (strong) {
        const last = formatNextRun(schedule && schedule.last_run);
        const resultText = result === 'success' ? ' · Успешно' : result === 'failed' ? ' · Ошибка' : '';
        strong.textContent = last ? last + resultText : 'Нет данных';
      }
    }
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
    installSubscriptionScheduleSync();
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mountIssue590, {once:true});
  else mountIssue590();
  requestAnimationFrame(mountIssue590);
  document.addEventListener('freenet:controls-busy', mountIssue590);
})();
