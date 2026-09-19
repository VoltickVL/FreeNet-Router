// Runtime hotfix for the topbar VPN selector: compact modal presentation,
// bilingual profile search and explicit empty/error states. The patch is
// presentation-only and delegates selection to the existing safe exact-connect
// flow.
(() => {
  const styleID = 'freenetVPNSelectorModalSearchStyles';
  const patchFlag = 'freenetVpnSelectorHotfix';

  const q = (selector, root = document) => root.querySelector(selector);
  const qa = (selector, root = document) => Array.from(root.querySelectorAll(selector));

  const countryAliases = {
    de: 'de deu germany deutschland german германия герман немец франкфурт frankfurt',
    nl: 'nl nld netherlands holland dutch нидерланды голландия голланд амстердам amsterdam',
    pl: 'pl pol poland polish польша польский варшава warsaw',
    fi: 'fi fin finland finnish финляндия финский хельсинки helsinki',
    fr: 'fr fra france french франция француз париж paris',
    gb: 'gb uk united kingdom britain england london великобритания англия британ лондон',
    us: 'us usa united states america американ сша америка new york los angeles',
    es: 'es esp spain spanish испания испан мадрид madrid',
    it: 'it ita italy italian италия итальян милан rome milan',
    se: 'se swe sweden swedish швеция швед stockholm стокгольм',
    no: 'no nor norway norwegian норвегия норвеж oslo осло',
    dk: 'dk dnk denmark danish дания датский copenhagen копенгаген',
    ch: 'ch che switzerland swiss швейцария цюрих zurich',
    at: 'at aut austria austrian австрия вена vienna',
    cz: 'cz cze czechia czech прага чехия prague',
    sk: 'sk svk slovakia slovak словакия братислава bratislava',
    rs: 'rs srb serbia serbian сербия белград belgrade',
    si: 'si svn slovenia slovenian словения любляна ljubljana',
    is: 'is isl iceland island исландия reykjavik рейкьявик',
    lu: 'lu lux luxembourg люксембург',
    kz: 'kz kaz kazakhstan казахстан алматы astana almaty',
    tr: 'tr tur turkey turkiye турция стамбул istanbul',
    ae: 'ae are uae emirates dubai оаэ эмираты дубай',
    kr: 'kr kor korea south korea корея сеул seoul',
    jp: 'jp jpn japan japanese япония токио tokyo',
    sg: 'sg sgp singapore сингапур',
    hk: 'hk hkg hong kong гонконг',
    ca: 'ca can canada канада toronto toronto montreal',
    br: 'br bra brazil бразилия сан-паулу sao paulo',
    ar: 'ar arg argentina аргентина буэнос buenos aires',
    mx: 'mx mex mexico мексика mexico city',
    au: 'au aus australia австралия sydney сидней',
    my: 'my mys malaysia малайзия kuala lumpur',
    za: 'za zaf south africa юар африка cape town',
    ng: 'ng nga nigeria нигерия lagos лагос',
    pe: 'pe per peru перу lima лима'
  };

  function normalize(value) {
    return String(value || '')
      .normalize('NFKD')
      .replace(/[\u0300-\u036f]/g, '')
      .replace(/ё/g, 'е')
      .toLowerCase()
      .trim();
  }

  function endpoint(profile) {
    try {
      if (typeof formatProfileEndpoint === 'function') return formatProfileEndpoint(profile);
    } catch (_) {}
    const address = String(profile?.address || '');
    const host = address.includes(':') ? `[${address}]` : address;
    const port = profile?.port ? `:${profile.port}` : '';
    return `${host}${port}`;
  }

  function cleanLabel(value) {
    return String(value || '')
      .replace(/^[\u{1F1E6}-\u{1F1FF}]{2}\s*/u, '')
      .replace(/^[A-Za-z]{2}\s+/, '')
      .trim();
  }

  function profileCode(profile) {
    const direct = normalize(profile?.country_code || profile?.code || '');
    if (/^[a-z]{2}$/.test(direct)) return direct;
    const hay = normalize([
      profile?.id,
      profile?.name,
      profile?.label,
      profile?.country,
      profile?.city,
      profile?.address
    ].join(' '));
    for (const [code, aliases] of Object.entries(countryAliases)) {
      if (` ${hay} `.includes(` ${code} `)) return code;
      if (aliases.split(/\s+/).some(alias => alias && hay.includes(alias))) return code;
    }
    const dashed = hay.match(/(?:^|[^a-z])([a-z]{2})(?:[-_. ]|$)/);
    return dashed ? dashed[1] : '';
  }

  function profileSearchText(profile) {
    const code = profileCode(profile);
    return normalize([
      profile?.id,
      profile?.name,
      profile?.label,
      profile?.country,
      profile?.country_code,
      profile?.city,
      profile?.address,
      profile?.port,
      endpoint(profile),
      code,
      countryAliases[code] || ''
    ].join(' '));
  }

  function currentProfiles() {
    try {
      return Array.isArray(extraProfiles) ? extraProfiles : [];
    } catch (_) {
      return [];
    }
  }

  function currentSelectedID() {
    try { return String(selectedProviderID || ''); } catch (_) { return ''; }
  }

  function currentSelectedName() {
    try { return cleanLabel(selectedProviderName || ''); } catch (_) { return ''; }
  }

  function showMenu() {
    const menu = q('#profilesMenu');
    const trigger = q('#profilesTrigger');
    if (menu) {
      menu.hidden = false;
      menu.removeAttribute('aria-hidden');
      menu.classList.add('open');
    }
    if (trigger) {
      trigger.setAttribute('aria-expanded', 'true');
      trigger.classList.add('open');
    }
  }

  function hideMenu() {
    if (typeof closeProfileMenu === 'function') {
      closeProfileMenu();
      return;
    }
    const menu = q('#profilesMenu');
    const trigger = q('#profilesTrigger');
    if (menu) menu.hidden = true;
    if (trigger) trigger.setAttribute('aria-expanded', 'false');
  }

  function appendEmpty(menu, text) {
    const box = document.createElement('div');
    box.className = 'fn-selector-empty hint';
    box.textContent = text;
    menu.appendChild(box);
  }

  function optionTitle(profile) {
    return cleanLabel(profile?.name || profile?.label || endpoint(profile) || 'VPN-сервер');
  }

  function renderProfileOptionsHotfix() {
    const menu = q('#profilesMenu');
    const triggerText = q('#profilesTriggerText');
    const search = q('#profileSearch');
    if (!menu || !search) return;

    const query = normalize(search.value);
    const source = currentProfiles();
    const selectedID = currentSelectedID();
    const selectedName = currentSelectedName();
    const filtered = source.filter(profile => !query || profileSearchText(profile).includes(query));

    menu.replaceChildren();
    if (triggerText) {
      triggerText.textContent = selectedName || (source.length ? 'Выбрать VPN-сервер' : 'Профили не загружены');
    }

    if (!source.length) {
      appendEmpty(menu, 'Extra-профили не загружены. Откройте «Подписка» и обновите список, затем вернитесь к выбору VPN-сервера.');
      return;
    }
    if (!filtered.length) {
      appendEmpty(menu, `По запросу «${search.value.trim()}» ничего не найдено. Попробуйте Germany, DE, город или endpoint.`);
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
      main.textContent = optionTitle(profile);
      const meta = document.createElement('span');
      meta.className = 'profile-option-meta';
      meta.textContent = endpoint(profile);
      button.append(main, meta);

      button.addEventListener('click', () => {
        if (typeof selectProviderProfile === 'function') selectProviderProfile(profile);
        hideMenu();
      });
      menu.appendChild(button);
    });

    if (filtered.length > 100) {
      appendEmpty(menu, `Показаны первые 100 из ${filtered.length}. Уточните страну, город или endpoint.`);
    }
  }

  function patchRenderer() {
    try {
      if (typeof renderProfileOptions !== 'function') return;
      if (renderProfileOptions[patchFlag]) return;
      const patched = function() {
        renderProfileOptionsHotfix();
      };
      patched[patchFlag] = true;
      renderProfileOptions = patched;
      renderProfileOptions();
    } catch (_) {}
  }

  function injectStyles() {
    if (q(`#${styleID}`)) return;
    const style = document.createElement('style');
    style.id = styleID;
    style.textContent = `
      #fnVpnPickerTrigger{flex:0 0 auto!important;width:min(244px,28vw)!important;min-width:188px!important;max-width:244px!important;align-self:center!important}
      #fnVpnPickerTrigger .fn-vpn-picker-main,#fnVpnPickerTrigger strong{max-width:150px!important;overflow:hidden!important;text-overflow:ellipsis!important;white-space:nowrap!important}
      #bestServerAdvanced.fn-topbar-vpn-picker{flex:0 1 auto!important;min-width:0!important;max-width:none!important;margin-left:0!important}
      #fnVpnPickerBody #bestServerAdvanced.fn-topbar-vpn-picker{display:block!important;width:100%!important}
      #fnVpnPickerBody #profilesList.profiles{display:grid!important;grid-template-columns:1fr!important;gap:10px!important;margin-top:0!important}
      #fnVpnPickerBody #profileSearch{width:100%!important}
      #fnVpnPickerBody #profilesMenu.profile-menu{position:relative!important;left:auto!important;right:auto!important;top:auto!important;width:100%!important;max-height:min(330px,42vh)!important;margin-top:8px!important;overflow:auto!important;z-index:1400!important}
      #profilesMenu .profile-option{width:100%!important;text-align:left!important;display:flex!important;flex-direction:column!important;align-items:flex-start!important;gap:3px!important}
      #profilesMenu .profile-option-main{font-weight:760!important;color:#f5f8ff!important}
      #profilesMenu .profile-option-meta{font-size:11px!important;color:#8fa6c5!important;overflow:hidden!important;text-overflow:ellipsis!important;white-space:nowrap!important;max-width:100%!important}
      #profilesMenu .fn-selector-empty{padding:13px!important;border:1px solid rgba(92,162,255,.24)!important;border-radius:11px!important;background:rgba(8,24,42,.72)!important;color:#b9c9df!important;line-height:1.45!important}
      @media(max-width:900px){#fnVpnPickerTrigger{width:50px!important;min-width:50px!important;max-width:50px!important;padding-left:9px!important;padding-right:9px!important}#fnVpnPickerTrigger .fn-vpn-picker-main,#fnVpnPickerTrigger .fn-vpn-picker-copy{display:none!important}}
    `;
    document.head.appendChild(style);
  }

  function bindSearchAndTrigger() {
    const search = q('#profileSearch');
    if (search && search.dataset.freenetVpnSearchHotfix !== '1') {
      search.dataset.freenetVpnSearchHotfix = '1';
      search.addEventListener('input', () => {
        patchRenderer();
        renderProfileOptionsHotfix();
        showMenu();
      }, true);
      search.addEventListener('focus', () => {
        patchRenderer();
        renderProfileOptionsHotfix();
        showMenu();
      }, true);
    }

    const profileTrigger = q('#profilesTrigger');
    if (profileTrigger && profileTrigger.dataset.freenetVpnTriggerHotfix !== '1') {
      profileTrigger.dataset.freenetVpnTriggerHotfix = '1';
      profileTrigger.addEventListener('click', () => {
        patchRenderer();
        renderProfileOptionsHotfix();
        if (q('#profilesMenu')?.hidden) showMenu();
      }, true);
    }

    const topbarTrigger = q('#fnVpnPickerTrigger');
    if (topbarTrigger && topbarTrigger.dataset.freenetVpnTopbarHotfix !== '1') {
      topbarTrigger.dataset.freenetVpnTopbarHotfix = '1';
      topbarTrigger.addEventListener('click', () => {
        requestAnimationFrame(() => {
          patchRenderer();
          renderProfileOptionsHotfix();
          const field = q('#profileSearch');
          if (field) field.focus({preventScroll:true});
          showMenu();
        });
      }, true);
    }
  }

  function mount() {
    injectStyles();
    patchRenderer();
    bindSearchAndTrigger();
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mount, {once:true});
  else mount();
  requestAnimationFrame(mount);
  document.addEventListener('freenet:controls-busy', mount);
})();
