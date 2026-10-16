(() => {
  'use strict';

  const qs = (s, root = document) => root.querySelector(s);
  const qsa = (s, root = document) => Array.from(root.querySelectorAll(s));
  const LIST_TABS = new Set(['ip_exclude', 'port_exclude', 'port_proxying']);
  function setText(node, text) {
    if (node && node.textContent !== text) node.textContent = text;
  }

  function installStyles() {
    if ((document.documentElement.dataset.freenetStyleconfigstudiouxjs1 === "1")) return;
    document.documentElement.dataset.freenetStyleconfigstudiouxjs1 = "1";
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