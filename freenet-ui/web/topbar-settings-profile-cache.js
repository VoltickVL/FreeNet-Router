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
