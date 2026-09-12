(() => {
  'use strict';
  // Legacy async Settings renderer is retired. This file only applies the
  // approved visual geometry after Settings v3 has mounted, so presentation
  // cannot participate in routing or page lifecycle.
  if (window.__freenetAcceptedSettingsGeometryLoaded) return;
  window.__freenetAcceptedSettingsGeometryLoaded = true;
  window.__freenetLegacyAutomationAsyncRetired = true;

  const q = (selector, root = document) => root.querySelector(selector);

  function installStyle() {
    if (q('#freenetAcceptedSettingsGeometry')) return;
    const style = document.createElement('style');
    style.id = 'freenetAcceptedSettingsGeometry';
    style.textContent = `
      body:has([data-page-view="settings"].active) .app{grid-template-columns:212px minmax(0,1fr)!important}
      body:has([data-page-view="settings"].active) .sidebar{padding:14px 9px 18px!important}
      body:has([data-page-view="settings"].active) .sidebar>.brand{padding:6px 14px 24px!important;font-size:25px!important}
      body:has([data-page-view="settings"].active) .sidebar>.brand::before{width:28px!important;height:28px!important;background:none!important;-webkit-mask:none!important;mask:none!important;background-image:url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 32 32'%3E%3Cpath fill='%232e8cff' d='M16 1 23 8 16 15 9 8zM8 9l7 7-7 7-7-7zM24 9l7 7-7 7-7-7zM16 17l7 7-7 7-7-7z'/%3E%3C/svg%3E")!important;background-size:contain!important;background-position:center!important;background-repeat:no-repeat!important}
      body:has([data-page-view="settings"].active) .fn-brand-free{background:none!important;-webkit-text-fill-color:#eaf3ff!important;color:#eaf3ff!important}
      body:has([data-page-view="settings"].active) .fn-brand-net{background:none!important;-webkit-text-fill-color:#78adff!important;color:#78adff!important}
      body:has([data-page-view="settings"].active) .nav{gap:5px!important}
      body:has([data-page-view="settings"].active) .nav-btn{min-height:43px!important;padding:0 13px!important;border-radius:10px!important;font-size:14px!important;gap:11px!important}
      body:has([data-page-view="settings"].active) .nav-icon{width:22px!important;height:22px!important;flex-basis:22px!important}
      body:has([data-page-view="settings"].active) .nav-icon svg{width:20px!important;height:20px!important}
      body:has([data-page-view="settings"].active) .content{width:min(1288px,calc(100% - 36px))!important;padding-top:14px!important;padding-bottom:42px!important}
      body:has([data-page-view="settings"].active) .fn3-head{margin-bottom:13px!important}
      body:has([data-page-view="settings"].active) .fn3-head h1{font-size:29px!important;line-height:1.08!important}
      body:has([data-page-view="settings"].active) .fn3-head p{font-size:13px!important;margin-top:5px!important}
      body:has([data-page-view="settings"].active) .fn3-save{width:208px!important;min-width:208px!important;height:40px!important;min-height:40px!important;border-radius:10px!important}
      body:has([data-page-view="settings"].active) .fn3-grid{grid-template-columns:640px minmax(0,1fr)!important;gap:14px!important}
      body:has([data-page-view="settings"].active) .fn3-card{border-radius:12px!important}
      body:has([data-page-view="settings"].active) .fn3-left>.fn3-card:first-child{min-height:518px!important}
      body:has([data-page-view="settings"].active) .fn3-right>.fn3-card:first-child{min-height:293px!important}
      body:has([data-page-view="settings"].active) .fn3-right>.fn3-card:nth-child(2){min-height:213px!important}
      body:has([data-page-view="settings"].active) .fn3-extra{min-height:299px!important;padding:11px 6px!important}
      body:has([data-page-view="settings"].active) .fn3-extra-grid{gap:10px!important}
      body:has([data-page-view="settings"].active) .fn3-extra-card{min-height:229px!important}
      body:has([data-page-view="settings"].active) .fn3-vpn-glyph{display:grid!important;place-items:center!important;width:30px!important;height:30px!important;border:2px solid #e8f3ff!important;border-radius:50%!important;color:#fff!important;font-size:10px!important;font-weight:800!important;letter-spacing:-.04em!important;line-height:1!important}
      body:has([data-page-view="settings"].active) .fn3-auto-actions .btn svg{width:18px!important;height:18px!important}
      body:has([data-page-view="settings"].active) .fn3-extra-action svg,
      body:has([data-page-view="settings"].active) .fn3-backup-actions .btn svg{width:16px!important;height:16px!important}
      @media(max-width:1320px){body:has([data-page-view="settings"].active) .fn3-grid{grid-template-columns:minmax(0,1.04fr) minmax(0,1fr)!important}}
      @media(max-width:1120px){body:has([data-page-view="settings"].active) .app{grid-template-columns:var(--sidebar) minmax(0,1fr)!important}body:has([data-page-view="settings"].active) .fn3-grid{grid-template-columns:1fr!important}}
    `;
    document.head.appendChild(style);
  }

  function tuneMarkup() {
    const page = q('[data-page-view="settings"][data-settings-v3="1"]');
    if (!page) return false;
    const autoIcon = q('.fn3-left .fn3-card:first-child .fn3-icon', page);
    if (autoIcon && autoIcon.dataset.acceptedGlyph !== '1') {
      autoIcon.dataset.acceptedGlyph = '1';
      autoIcon.innerHTML = '<span class="fn3-vpn-glyph">VPN</span>';
    }
    return true;
  }

  function applyAfterMount(attempt = 0) {
    if (!tuneMarkup()) {
      if (attempt < 100) setTimeout(() => applyAfterMount(attempt + 1), 50);
      return;
    }
    installStyle();
  }

  function schedule() { setTimeout(() => applyAfterMount(), 0); }
  if (document.readyState === 'complete') schedule();
  else window.addEventListener('load', schedule, {once:true});
  window.addEventListener('hashchange', schedule);
})();
