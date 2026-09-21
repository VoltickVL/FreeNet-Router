;(() => {
  'use strict';

  const CORE_SCRIPT = 'settings-v3-core.js';
  const STYLE_ID = 'freenetSettingsSingleSaveStyles';
  let patchQueued = false;

  function coreURL() {
    try {
      const current = document.currentScript?.src || window.location.href;
      return new URL(CORE_SCRIPT, current).href;
    } catch (_) {
      return `/${CORE_SCRIPT}`;
    }
  }

  function injectStyles() {
    if (document.getElementById(STYLE_ID)) return;
    const style = document.createElement('style');
    style.id = STYLE_ID;
    style.textContent = `
      #fn3Save.fn3-save{min-width:210px!important;min-height:42px!important;padding:0 18px!important;border-color:#4f8dff!important;background:linear-gradient(180deg,rgba(35,65,111,.96),rgba(22,45,80,.96))!important;color:#eef6ff!important;box-shadow:0 0 0 1px rgba(87,144,255,.32),0 10px 28px rgba(34,101,221,.24)!important;opacity:1!important;filter:none!important}
      #fn3Save.fn3-save svg{width:16px!important;height:16px!important;flex:0 0 16px!important}
      #fn3Save.fn3-save[disabled]{border-color:#3d638f!important;background:linear-gradient(180deg,rgba(20,43,76,.96),rgba(13,29,53,.96))!important;color:#c8d7ea!important;box-shadow:0 0 0 1px rgba(91,140,255,.18),0 8px 22px rgba(0,0,0,.18)!important;opacity:.9!important;filter:none!important}
      #fn3Save.fn3-save:not([disabled]){border-color:#82adff!important;background:linear-gradient(180deg,#4d83ff,#2f64e5)!important;color:#fff!important;box-shadow:0 0 0 1px rgba(128,174,255,.55),0 0 0 4px rgba(73,126,244,.16),0 14px 34px rgba(50,100,215,.36)!important}
      #fn3Save.fn3-save:not([disabled]):hover{transform:translateY(-1px);box-shadow:0 0 0 1px rgba(150,190,255,.65),0 0 0 5px rgba(73,126,244,.2),0 16px 38px rgba(50,100,215,.42)!important}
      .fn3-extra-save-row{display:none!important}
      @media(max-width:760px){#fn3Save.fn3-save{width:100%;min-width:0!important;margin-top:10px}}
    `;
    document.head.appendChild(style);
  }

  function patchSingleSave() {
    injectStyles();
    const footerRow = document.querySelector('.fn3-extra-save-row');
    if (footerRow) footerRow.remove();
    const duplicate = document.getElementById('fn3MaintenanceSave');
    if (duplicate) duplicate.remove();
    const save = document.getElementById('fn3Save');
    if (!save) return;
    save.classList.add('fn3-single-save');
    save.setAttribute('aria-label', 'Сохранить изменения настроек FreeNet');
    save.title = save.disabled ? 'Настройки сохранены' : 'Сохранить изменения';
  }

  function schedulePatch() {
    if (patchQueued) return;
    patchQueued = true;
    queueMicrotask(() => {
      patchQueued = false;
      patchSingleSave();
    });
  }

  function watchSettingsDOM() {
    if (window.__freenetSettingsSingleSaveWatch) return;
    window.__freenetSettingsSingleSaveWatch = true;
    document.addEventListener('freenet:settings-v3-updated', schedulePatch);
    window.addEventListener('hashchange', schedulePatch);
    if (document.body) {
      new MutationObserver(schedulePatch).observe(document.body, {childList:true, subtree:true});
    } else {
      document.addEventListener('DOMContentLoaded', watchSettingsDOM, {once:true});
    }
  }

  function loadCore() {
    if (window.__freenetSettingsV3Loaded) {
      schedulePatch();
      return;
    }
    const existing = document.querySelector('script[data-freenet-settings-v3-core="1"]');
    if (existing) {
      existing.addEventListener('load', schedulePatch, {once:true});
      return;
    }
    const script = document.createElement('script');
    script.src = coreURL();
    script.async = false;
    script.dataset.freenetSettingsV3Core = '1';
    script.onload = schedulePatch;
    script.onerror = () => console.error('Settings v3 core failed to load');
    document.head.appendChild(script);
  }

  injectStyles();
  watchSettingsDOM();
  loadCore();
  schedulePatch();
})();
