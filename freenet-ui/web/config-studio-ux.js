(() => {
  'use strict';

  const qs = (s, root = document) => root.querySelector(s);
  const qsa = (s, root = document) => Array.from(root.querySelectorAll(s));
  const LIST_TABS = new Set(['ip_exclude', 'port_exclude', 'port_proxying']);
  let busy = false;
  let service = null;

  function setText(node, text) {
    if (node && node.textContent !== text) node.textContent = text;
  }

  function compactVersion(raw) {
    const match = String(raw || '').match(/\bv?\d+(?:\.\d+){1,3}(?:[-+][0-9A-Za-z.-]+)?/);
    if (!match) return '';
    return match[0].startsWith('v') ? match[0] : `v${match[0]}`;
  }

  function installStyles() {
    if (qs('#configStudioUXStyles')) return;
    const style = document.createElement('style');
    style.id = 'configStudioUXStyles';
    style.textContent = `
      #rv2WorkspaceState,.rv2-modebar>.rv2-state,#csState,.cs-safe-note,#csApplyNote,#csMeta,#rv2ConfigPanel #rv2ApplyPreview,#rv2ConfigPanel #rv2ApplyResult,#rv2ConfigPanel #rv2ApplyConfig,.cs-notice.ok{display:none!important}
      .cs-shell>.rv2-copy{margin:0;color:#8fa4bf}
      .cs-service{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:12px;align-items:center;padding:0 0 12px;border:0;border-bottom:1px solid #24415f;border-radius:0;background:transparent}
      .cs-service-main{display:flex;align-items:center;gap:9px;min-width:0;flex-wrap:wrap}.cs-service-title{font-size:13px;font-weight:850;color:#f2f6fc}.cs-service-status{display:inline-flex;align-items:center;gap:6px;color:#8ea4c0;font-size:11px;font-weight:750}.cs-service-status::before{content:'';width:8px;height:8px;border-radius:50%;background:#7d91aa}.cs-service-status.ok{color:#67e6aa}.cs-service-status.ok::before{background:#36e3a2}.cs-service-status.bad{color:#ff929d}.cs-service-status.bad::before{background:#ff6773}
      .cs-version{appearance:none;border:1px solid #365a7d;background:#10243b;color:#d9e7f6;border-radius:9px;padding:7px 10px;font:inherit;font-size:11px;font-weight:800;cursor:pointer}.cs-version:hover{border-color:#6594cb;background:#173352;color:#fff}.cs-service-actions{display:flex;gap:8px;align-items:center;flex-wrap:wrap}.cs-service-btn{appearance:none;border:1px solid #365a7d;background:#10243b;color:#d9e7f6;border-radius:9px;padding:8px 11px;font:inherit;font-size:11px;font-weight:800;cursor:pointer}.cs-service-btn:hover:not(:disabled){border-color:#6594cb;background:#173352}.cs-service-btn:disabled{opacity:.45;cursor:not-allowed}
      .cs-journal{display:none;grid-column:1/-1;border-top:1px solid #203952;padding-top:10px}.cs-journal.show{display:block}.cs-journal-head{display:flex;align-items:center;justify-content:space-between;gap:10px;margin-bottom:7px}.cs-journal-title{font-size:11px;font-weight:850;color:#dce8f8}.cs-journal-list{display:grid;gap:6px}.cs-journal-row{display:grid;grid-template-columns:130px 82px minmax(0,1fr);gap:9px;align-items:start;padding:7px 9px;border-radius:9px;background:#07131f;color:#9fb2ca;font-size:10px}.cs-journal-row b{color:#dce8f8;font-weight:800}.cs-journal-row.ok b{color:#68e8ad}.cs-journal-row.bad b{color:#ff929c}.cs-journal-empty{color:#8398b4;font-size:11px;padding:5px 0}
      .cs-tab-groups{gap:16px!important;align-items:flex-start!important}.cs-tabs{padding:0!important;border:0!important;border-radius:0!important;background:transparent!important}.cs-tabs-main::before,.cs-tabs-lists::before{display:flex;align-items:center;min-height:34px;padding:0 10px;margin-right:2px;border-radius:9px;font-size:10px;font-weight:900;letter-spacing:.08em;text-transform:uppercase}.cs-tabs-main::before{content:'Конфиги Xray';color:#b7d8ff;background:linear-gradient(180deg,rgba(31,99,181,.48),rgba(20,67,126,.42));border:1px solid #396fa9}.cs-tabs-lists::before{content:'Списки XKeen';color:#9fe8df;background:linear-gradient(180deg,rgba(14,111,104,.34),rgba(11,73,73,.3));border:1px solid #2d756f}.cs-tabs-main .cs-tab.active{border-color:#6aa2ff!important;background:linear-gradient(180deg,#1d5aa4,#17457f)!important}.cs-tabs-lists .cs-tab.active{border-color:#45b9ad!important;background:linear-gradient(180deg,#17665f,#104c49)!important}
      .cs-toolbar.cs-editor-footer{margin-top:10px;padding:12px 0 2px;border-top:1px solid #24415f}.cs-btn{font-size:11px!important;color:#dce8f7!important}.cs-btn-tertiary{border-color:#385471!important;background:#0b1b2d!important;color:#b9cce1!important}.cs-btn-tertiary:hover:not(:disabled){border-color:#5b7898!important;background:#10253c!important}.cs-btn-reset{border-color:#755e34!important;background:rgba(93,66,25,.22)!important;color:#f3cd84!important}.cs-btn-reset:hover:not(:disabled){border-color:#a17c38!important;background:rgba(116,79,24,.32)!important}.cs-btn.primary{min-width:118px;background:linear-gradient(180deg,#347eff,#2367e7)!important;border-color:#69a0ff!important;color:#fff!important}.cs-btn.primary:hover:not(:disabled){background:linear-gradient(180deg,#438cff,#2f72ed)!important;border-color:#81adff!important}.cs-list-view .cs-toolbar{display:none!important}.cs-list-view .cs-meta{margin-top:-3px}
      @media(max-width:760px){.cs-service{grid-template-columns:1fr}.cs-service-actions{width:100%}.cs-service-btn{flex:1}.cs-journal-row{grid-template-columns:1fr}.cs-tabs-main::before,.cs-tabs-lists::before{display:none}}
    `;
    document.head.appendChild(style);
  }

  function friendlyTime(raw) {
    const d = new Date(raw);
    if (Number.isNaN(d.getTime())) return String(raw || '');
    return d.toLocaleString('ru-RU', {day:'2-digit', month:'2-digit', hour:'2-digit', minute:'2-digit'});
  }

  function serviceMarkup() {
    const node = document.createElement('div');
    node.id = 'csService';
    node.className = 'cs-service';
    node.innerHTML = `
      <div class="cs-service-main"><span class="cs-service-title">Xray</span><span id="csServiceStatus" class="cs-service-status">Проверяю…</span><button id="csServiceVersion" type="button" class="cs-version" title="Выбрать версию Xray">Версия…</button></div>
      <div class="cs-service-actions"><button id="csRestartXray" type="button" class="cs-service-btn">Перезапустить</button><button id="csToggleJournal" type="button" class="cs-service-btn">Журнал</button></div>
      <div id="csServiceJournal" class="cs-journal"><div class="cs-journal-head"><span class="cs-journal-title">Последние действия Xray</span><button id="csOpenFullJournal" type="button" class="cs-service-btn">Все события</button></div><div id="csServiceJournalList" class="cs-journal-list"></div></div>`;
    return node;
  }

  function setNotice(text, type = '') {
    const box = qs('#csNotice');
    if (!box) return;
    setText(box, text || '');
    const nextClass = `cs-notice${text ? ' show' : ''}${type ? ' ' + type : ''}`;
    if (box.className !== nextClass) box.className = nextClass;
  }

  function renderService(body) {
    service = body || service || {};
    const status = qs('#csServiceStatus');
    const version = qs('#csServiceVersion');
    const restart = qs('#csRestartXray');
    if (status) {
      setText(status, service.online ? 'Работает' : 'Остановлен');
      const nextClass = `cs-service-status ${service.online ? 'ok' : 'bad'}`;
      if (status.className !== nextClass) status.className = nextClass;
    }
    if (version) {
      const shortVersion = compactVersion(service.version);
      setText(version, shortVersion ? `${shortVersion} ▾` : 'Версия неизвестна');
      version.disabled = !shortVersion || busy;
    }
    if (restart && restart.disabled !== busy) restart.disabled = busy;
    const list = qs('#csServiceJournalList');
    if (list) {
      list.textContent = '';
      const events = Array.isArray(service.events) ? service.events.slice(0, 5) : [];
      if (!events.length) {
        const empty = document.createElement('div'); empty.className = 'cs-journal-empty'; empty.textContent = 'Действий Xray пока нет.'; list.appendChild(empty);
      } else events.forEach(event => {
        const row = document.createElement('div');
        const ok = String(event.result || '').toLowerCase() === 'success';
        row.className = `cs-journal-row ${ok ? 'ok' : 'bad'}`;
        const time = document.createElement('span'); time.textContent = friendlyTime(event.at);
        const result = document.createElement('b'); result.textContent = ok ? 'Успешно' : 'Ошибка';
        const message = document.createElement('span'); message.textContent = String(event.message || '');
        row.append(time, result, message); list.appendChild(row);
      });
    }
  }

  async function loadService() {
    try {
      const response = await fetch('/api/xray/service', {cache:'no-store'});
      const body = await response.json();
      if (!response.ok || !body.success) throw new Error(body.error || `HTTP ${response.status}`);
      renderService(body);
    } catch (_) {
      renderService({online:false,version:'',events:[]});
    }
  }

  async function restartXray() {
    if (busy) return;
    busy = true; renderService(service || {});
    const button = qs('#csRestartXray'); setText(button, 'Перезапускаю…');
    setNotice('Проверяю конфигурацию и перезапускаю Xray…');
    try {
      const response = await fetch('/api/xray/service', {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'restart'}),cache:'no-store'});
      let body = {}; try { body = await response.json(); } catch (_) {}
      if (!response.ok || !body.success) throw new Error(body.error || 'Не удалось перезапустить Xray');
      renderService(body);
      setNotice(body.message || 'Xray перезапущен.', 'ok');
    } catch (error) {
      setNotice(error.message || 'Не удалось перезапустить Xray.', 'bad');
      await loadService();
    } finally {
      busy = false;
      setText(button, 'Перезапустить');
      renderService(service || {});
    }
  }

  function openFullJournal() {
    const button = qs('[data-page="access"]');
    if (button) { button.click(); return; }
    location.hash = '#journal';
  }

  function simplifyControls() {
    const shell = qs('.cs-shell');
    if (!shell) return false;
    setText(qs('.cs-title h2', shell), 'Конфигурация Xray');
    setText(qs(':scope > .rv2-copy', shell), 'Редактирование конфигов Xray и списков XKeen.');
    setText(qs('#csFormat'), 'Форматировать');
    setText(qs('#csReset'), 'Отменить');
    setText(qs('#csApply'), 'Сохранить');
    qs('#csValidate')?.remove();
    qsa('.cs-safe-note,#csMeta,#csApplyNote,#rv2ApplyPreview,#rv2ApplyResult,#rv2ApplyConfig').forEach(node => node.remove());
    const xray = qs('#csXray'); if (xray) xray.remove();
    if (!qs('#csService', shell)) {
      const groups = qs('.cs-tab-groups', shell) || qs('#csTabsMain', shell);
      if (groups) groups.parentNode.insertBefore(serviceMarkup(), groups);
      else shell.prepend(serviceMarkup());
      qs('#csRestartXray')?.addEventListener('click', restartXray);
      qs('#csToggleJournal')?.addEventListener('click', () => qs('#csServiceJournal')?.classList.toggle('show'));
      qs('#csOpenFullJournal')?.addEventListener('click', openFullJournal);
      loadService();
    }
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
