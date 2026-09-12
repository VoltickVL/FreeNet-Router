(() => {
  'use strict';
  // Settings v3 is the sole Settings renderer and lifecycle owner.
  // This file owns presentation geometry only; it must not alter routing or runtime state.
  if (window.__freenetAcceptedSettingsGeometryLoaded) return;
  window.__freenetAcceptedSettingsGeometryLoaded = true;
  window.__freenetLegacyAutomationAsyncRetired = true;

  const q = (selector, root = document) => root.querySelector(selector);
  const pageIsActive = () => !!q('[data-page-view="settings"][data-settings-v3="1"].active');

  function installStyle() {
    if (q('#freenetAcceptedSettingsGeometry')) return;
    const style = document.createElement('style');
    style.id = 'freenetAcceptedSettingsGeometry';
    style.textContent = `
      /* Keep the shared Control Center shell exactly the same on Settings. */
      body.fn-settings-accepted .content{width:min(1288px,calc(100% - 36px))!important;padding-top:14px!important;padding-bottom:42px!important}
      body.fn-settings-accepted .fn3-head{margin-bottom:13px!important}
      body.fn-settings-accepted .fn3-head h1{font-size:29px!important;line-height:1.08!important}
      body.fn-settings-accepted .fn3-head p{font-size:13px!important;margin-top:5px!important}
      body.fn-settings-accepted .fn3-save{width:208px!important;min-width:208px!important;height:40px!important;min-height:40px!important;border-radius:10px!important}
      body.fn-settings-accepted .fn3-grid{grid-template-columns:640px minmax(0,1fr)!important;gap:14px!important}
      body.fn-settings-accepted .fn3-card{border-radius:12px!important}
      body.fn-settings-accepted .fn3-left>.fn3-card:first-child{min-height:518px!important}
      body.fn-settings-accepted .fn3-right>.fn3-card:first-child{min-height:293px!important}
      body.fn-settings-accepted .fn3-right>.fn3-card:nth-child(2){min-height:213px!important}
      body.fn-settings-accepted .fn3-extra{min-height:299px!important;padding:11px 6px!important}
      body.fn-settings-accepted .fn3-extra-grid{gap:10px!important}
      body.fn-settings-accepted .fn3-extra-card{min-height:229px!important}
      body.fn-settings-accepted .fn3-left>.fn3-card:first-child .fn3-icon>svg{display:none!important}
      body.fn-settings-accepted .fn3-left>.fn3-card:first-child .fn3-icon::before{content:'VPN';display:grid!important;place-items:center!important;width:30px!important;height:30px!important;border:2px solid #e8f3ff!important;border-radius:50%!important;color:#fff!important;font-size:10px!important;font-weight:800!important;letter-spacing:-.04em!important;line-height:1!important;box-sizing:border-box!important}
      body.fn-settings-accepted .fn3-auto-actions .btn svg{width:18px!important;height:18px!important}
      body.fn-settings-accepted .fn3-extra-action svg,
      body.fn-settings-accepted .fn3-backup-actions .btn svg{width:16px!important;height:16px!important}

      /* The switch is already the AUTO VPN state indicator. Avoid duplicate status copy. */
      #fn3AutoLabel,.fn3-enabled-badge{display:none!important}
      #fn3VPNState{display:none!important}

      /* Use the same CSS flags as Overview instead of platform-dependent emoji glyphs. */
      #fn3Flag.flag-icon{width:28px!important;height:19px!important;font-size:0!important;line-height:0!important;border-radius:4px!important}

      /* Current server title on Overview may use the whole current-VPN panel width. */
      .best-v4-current{position:relative!important;display:block!important;min-width:0!important}
      .best-v4-current-main{display:block!important;width:100%!important;min-width:0!important}
      .best-v4-current #bestCurrentFlag{position:absolute!important;left:0!important;top:2px!important;margin:0!important}
      .best-v4-current .best-v4-label{display:flex!important;align-items:center!important;min-height:24px!important;padding-left:45px!important;padding-right:94px!important}
      .best-v4-current .best-v4-name{display:block!important;width:100%!important;max-width:none!important;word-break:normal!important}
      .best-v4-current .best-v4-endpoint{display:block!important;width:100%!important}
      .best-v4-current .fn-current-connected{position:absolute!important;right:0!important;top:0!important;margin:0!important}

      @media(max-width:1320px){body.fn-settings-accepted .fn3-grid{grid-template-columns:minmax(0,1.04fr) minmax(0,1fr)!important}}
      @media(max-width:1120px){body.fn-settings-accepted .fn3-grid{grid-template-columns:1fr!important}}
    `;
    document.head.appendChild(style);
  }

  function codeFromFlagText(value) {
    const text = String(value || '').trim();
    if (/^[A-Za-z]{2}$/.test(text)) return text.toLowerCase();
    const chars = Array.from(text);
    if (chars.length < 2) return '';
    const base = 0x1F1E6;
    const a = chars[0].codePointAt(0);
    const b = chars[1].codePointAt(0);
    if (a < base || a > 0x1F1FF || b < base || b > 0x1F1FF) return '';
    return String.fromCharCode(97 + a - base, 97 + b - base);
  }

  function syncSettingsFlag() {
    const node = q('#fn3Flag');
    if (!node) return;
    const code = codeFromFlagText(node.textContent) || node.dataset.countryCode || '';
    if (!code) return;
    Array.from(node.classList).filter(name => name.startsWith('flag-')).forEach(name => node.classList.remove(name));
    node.classList.add('flag-icon', `flag-${code}`);
    node.dataset.countryCode = code;
    node.setAttribute('aria-label', code.toUpperCase());
    if (node.textContent) node.textContent = '';
  }

  function normalizeSettingsCopy() {
    const title = q('#fn3ControlTitle');
    if (title && title.textContent !== 'Автопроверка') title.textContent = 'Автопроверка';
  }

  function installSettingsPresentationObservers() {
    const flag = q('#fn3Flag');
    if (flag && flag.dataset.presentationObserver !== '1') {
      flag.dataset.presentationObserver = '1';
      new MutationObserver(syncSettingsFlag).observe(flag, {childList:true,characterData:true,subtree:true});
    }
    const title = q('#fn3ControlTitle');
    if (title && title.dataset.presentationObserver !== '1') {
      title.dataset.presentationObserver = '1';
      new MutationObserver(normalizeSettingsCopy).observe(title, {childList:true,characterData:true,subtree:true});
    }
    syncSettingsFlag();
    normalizeSettingsCopy();
  }

  function applyPresentation() {
    installStyle();
    document.body?.classList.toggle('fn-settings-accepted', pageIsActive());
    installSettingsPresentationObservers();
  }

  function schedule() { setTimeout(applyPresentation, 0); }
  if (document.readyState === 'complete') schedule();
  else window.addEventListener('load', schedule, {once:true});
  window.addEventListener('hashchange', schedule);
  document.addEventListener('click', event => {
    if (event.target?.closest?.('.nav-btn[data-page]')) schedule();
  });
})();
