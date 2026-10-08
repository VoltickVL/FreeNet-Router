(() => {
  'use strict';

  const qs = (s, root = document) => root.querySelector(s);
  const qsa = (s, root = document) => Array.from(root.querySelectorAll(s));
  const LIST_TABS = new Set(['ip_exclude', 'port_exclude', 'port_proxying']);
  function setText(node, text) {
    if (node && node.textContent !== text) node.textContent = text;
  }

  function installStyles() {
    if (qs('#configStudioUXStyles')) return;
    const style = document.createElement('style');
    style.id = 'configStudioUXStyles';
    style.textContent = `
      #rv2WorkspaceState,.rv2-modebar>.rv2-state,#csState,.cs-safe-note,#csApplyNote,#csMeta,#rv2ConfigPanel #rv2ApplyPreview,#rv2ConfigPanel #rv2ApplyResult,#rv2ConfigPanel #rv2ApplyConfig,.cs-notice.ok{display:none!important}
      .cs-shell>.rv2-copy{margin:0;color:#8fa4bf}
      .cs-tab-groups{gap:16px!important;align-items:flex-start!important}.cs-tabs{padding:0!important;border:0!important;border-radius:0!important;background:transparent!important}.cs-tabs-main::before,.cs-tabs-lists::before{display:flex;align-items:center;min-height:34px;padding:0 10px;margin-right:2px;border-radius:9px;font-size:10px;font-weight:900;letter-spacing:.08em;text-transform:uppercase}.cs-tabs-main::before{content:'Конфиги Xray';color:#b7d8ff;background:linear-gradient(180deg,rgba(31,99,181,.48),rgba(20,67,126,.42));border:1px solid #396fa9}.cs-tabs-lists::before{content:'Списки XKeen';color:#9fe8df;background:linear-gradient(180deg,rgba(14,111,104,.34),rgba(11,73,73,.3));border:1px solid #2d756f}.cs-tabs-main .cs-tab.active{border-color:#6aa2ff!important;background:linear-gradient(180deg,#1d5aa4,#17457f)!important}.cs-tabs-lists .cs-tab.active{border-color:#45b9ad!important;background:linear-gradient(180deg,#17665f,#104c49)!important}
      .cs-toolbar.cs-editor-footer{margin-top:10px;padding:12px 0 2px;border-top:1px solid #24415f}.cs-btn{font-size:11px!important;color:#dce8f7!important}.cs-btn-tertiary{border-color:#385471!important;background:#0b1b2d!important;color:#b9cce1!important}.cs-btn-tertiary:hover:not(:disabled){border-color:#5b7898!important;background:#10253c!important}.cs-btn-reset{border-color:#755e34!important;background:rgba(93,66,25,.22)!important;color:#f3cd84!important}.cs-btn-reset:hover:not(:disabled){border-color:#a17c38!important;background:rgba(116,79,24,.32)!important}.cs-btn.primary{min-width:118px;background:linear-gradient(180deg,#347eff,#2367e7)!important;border-color:#69a0ff!important;color:#fff!important}.cs-btn.primary:hover:not(:disabled){background:linear-gradient(180deg,#438cff,#2f72ed)!important;border-color:#81adff!important}.cs-list-view .cs-toolbar{display:none!important}.cs-list-view .cs-meta{margin-top:-3px}
      @media(max-width:760px){.cs-service{grid-template-columns:1fr}.cs-service-actions{width:100%}.cs-service-btn{flex:1}.cs-journal-row{grid-template-columns:1fr}.cs-tabs-main::before,.cs-tabs-lists::before{display:none}}
    `;
    document.head.appendChild(style);
  }

  function simplifyControls() {
    const shell = qs('.cs-shell');
    if (!shell) return false;
    setText(qs('.cs-title h2', shell), 'Конфигурация Xray');
    setText(qs(':scope > .rv2-copy', shell), 'Редактирование конфигов Xray и списков XKeen.');
    setText(qs('#csFormat'), 'Форматировать');
    setText(qs('#csReset'), 'Отменить');
    setText(qs('#csApply'), 'Применить');
    qs('#csFormat')?.classList.add('cs-btn-tertiary');
    qs('#csReset')?.classList.add('cs-btn-reset');
    qs('.cs-toolbar', shell)?.classList.add('cs-editor-footer');
    qs('#csValidate')?.remove();
    qsa('.cs-safe-note,#csMeta,#csApplyNote,#rv2ApplyPreview,#rv2ApplyResult,#rv2ApplyConfig').forEach(node => node.remove());
    // Xray has one canonical Control Surface in its own tab and topbar.
    qs('#csXray')?.remove();
    qs('#csService')?.remove();
    return true;
  }

  function syncActiveKind() {
    const shell = qs('.cs-shell'); if (!shell) return;
    const name = qs('.cs-tab.active')?.dataset.tab || '';
    const shouldList = LIST_TABS.has(name);
    if (shell.classList.contains('cs-list-view') !== shouldList) shell.classList.toggle('cs-list-view', shouldList);
  }

  function polish() {
    installStyles();
    if (!simplifyControls()) return;
    syncActiveKind();
  }

  function start() {
    polish();
    document.addEventListener('click', event => {
      if (!event.target.closest?.('.cs-tab')) return;
      setTimeout(syncActiveKind, 0);
    });
    const root = document.body || document.documentElement;
    const observer = new MutationObserver(() => polish());
    observer.observe(root, {childList:true, subtree:true});
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', start, {once:true});
  else start();
})();
