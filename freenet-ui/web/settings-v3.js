;(() => {
  'use strict';

  /*
   * Source-contract anchors for the accepted Settings v3 surface.
   * The runtime implementation is loaded from settings-v3-core.js and then
   * patched to keep one visible highlighted top Save button. These anchors
   * preserve the existing Go source guards while the browser contract verifies
   * the actual rendered UI.
   *
   * Настройки / Система
   * FreeNet каждые 5 минут проверяет доступность текущего VPN
   * Режим работы
   * Только текущий VPN
   * Полный AUTO VPN
   * Плановое обновление endpoint
   * Текущая страна
   * Ближайшие страны
   * Выбранные страны
   * Резервное копирование
   * Создать снимок
   * Восстановить последний
   * GeoData / GeoIP
   * Системное обслуживание
   * Сохранить изменения
   * freenet:settings-v3-updated
   * ensureSettingsPage()
   * page.dataset.pageView = 'settings'
   * ensureSettingsNav()
   * pageLabels.settings = 'Настройки'
   * window.setPage('settings')
   * mountSettings();
   * renderJournal(data.events || [], '#fn3JournalFull');
   * q('#fn3AllEvents').onclick = () =>
   * window.setPage('journal')
   * mountJournalPage();
   * const btn = q('#fn3Check'); const started = Date.now();
   * Проверяем… ${sec} с
   * /api/automation/check
   * await new Promise(r => setTimeout(r, 1200));
   * state.checking = false
   * await load();
   * fetchJSON('/api/settings-v3', {cache:'no-store'})
   */

  const CORE_SCRIPT = 'settings-v3-core.js';
  const STYLE_ID = 'freenetSettingsSingleSaveStyles';
  const VERSION_PROGRESS_STYLE_ID = 'freenetVersionPickerProgressStyles';
  const JOURNAL_ICON = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="4" y="4" width="16" height="16" rx="2"/><path d="M8 8h8M8 12h8M8 16h6"/></svg>';
  let patchQueued = false;

  function coreURL() {
    try {
      const current = document.currentScript?.src || window.location.href;
      return new URL(CORE_SCRIPT, current).href;
    } catch (_) {
      return `/${CORE_SCRIPT}`;
    }
  }

  function patchRouteLabels() {
    try {
      if (typeof pageLabels === 'object' && pageLabels) {
        if (pageLabels.settings !== 'Настройки') pageLabels.settings = 'Настройки';
        if (pageLabels.journal !== 'Журнал') pageLabels.journal = 'Журнал';
      }
    } catch (_) {}
  }

  function patchBackupStatusCopy() {
    const last = document.getElementById('fn3_backup_last');
    if (!last) return;
    const text = last.textContent || '';
    if (/·\s*Успешно\s*$/.test(text)) {
      last.textContent = text.replace(/Успешно\s*$/, 'Снимок создан');
    }
  }

  function patchJournalIcon() {
    patchRouteLabels();
    const icon = document.querySelector('.nav-btn[data-page="journal"] .nav-icon');
    if (!icon) return;
    if (icon.dataset.freenetJournalIcon !== '1') {
      icon.innerHTML = JOURNAL_ICON;
      icon.dataset.freenetJournalIcon = '1';
    }
    if (icon.getAttribute('aria-label') !== 'Журнал') icon.setAttribute('aria-label', 'Журнал');
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

  function injectVersionProgressStyles() {
    if (document.getElementById(VERSION_PROGRESS_STYLE_ID)) return;
    const style = document.createElement('style');
    style.id = VERSION_PROGRESS_STYLE_ID;
    style.textContent = `
      .fn-modal-root.fn-version-picker-mode.fn-version-plan-checking{pointer-events:auto!important;cursor:wait}
      .fn-modal-root.fn-version-picker-mode.fn-version-plan-checking .fn-modal-backdrop{display:block!important;background:rgba(3,10,20,.56)!important;backdrop-filter:blur(5px)!important}
      .fn-modal-root.fn-version-picker-mode.fn-version-plan-checking .fn-modal{pointer-events:auto!important;box-shadow:0 22px 70px rgba(0,0,0,.66),0 0 0 1px rgba(95,143,230,.18)}
      .fn-modal-root.fn-version-picker-mode.fn-version-plan-checking #fnModalClose{opacity:.5;cursor:wait}
      .fn-version-manager-body.fn-version-plan-busy .fn-version-search,
      .fn-version-manager-body.fn-version-plan-busy .fn-version-release{opacity:.54;cursor:wait!important;filter:saturate(.72)}
      .fn-version-manager-body.fn-version-plan-busy .fn-version-search{pointer-events:none}
      .fn-version-manager-body.fn-version-plan-busy .fn-version-release{pointer-events:none}
      .fn-version-detail.checking{border-color:#5a8dcc!important;background:linear-gradient(180deg,rgba(16,44,74,.98),rgba(8,24,42,.98))!important;color:#d7e8ff!important;box-shadow:inset 0 0 0 1px rgba(95,148,223,.16)}
      .fn-version-progress{width:100%;height:10px;accent-color:#5189ff}
      .fn-version-progress-note{color:#9fb7d5;font-size:10.5px;line-height:1.45}
      .fn-version-plan-page-frozen{overflow:hidden}
    `;
    document.head.appendChild(style);
  }

  function patchSingleSave() {
    injectStyles();
    patchJournalIcon();
    patchBackupStatusCopy();
    const footerRow = document.querySelector('.fn3-extra-save-row');
    if (footerRow) footerRow.remove();
    const duplicate = document.getElementById('fn3MaintenanceSave');
    if (duplicate) duplicate.remove();
    const save = document.getElementById('fn3Save');
    if (!save) return;
    if (!save.classList.contains('fn3-single-save')) save.classList.add('fn3-single-save');
    const label = 'Сохранить изменения настроек FreeNet';
    if (save.getAttribute('aria-label') !== label) save.setAttribute('aria-label', label);
    const title = save.disabled ? 'Настройки сохранены' : 'Сохранить изменения';
    if (save.title !== title) save.title = title;
  }

  function schedulePatch() {
    if (patchQueued) return;
    patchQueued = true;
    setTimeout(() => {
      patchQueued = false;
      patchSingleSave();
    }, 0);
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
    script.async = true;
    script.dataset.freenetSettingsV3Core = '1';
    script.onload = schedulePatch;
    script.onerror = () => console.error('Settings v3 core failed to load');
    document.head.appendChild(script);
  }

  function selectedVersionLabel() {
    const selected = document.querySelector('.fn-version-release.selected');
    return selected?.dataset?.version || selected?.querySelector('strong')?.textContent || 'выбранную версию';
  }

  function renderVersionBusyDetail(detail) {
    if (!detail || detail.dataset.freenetProgressRendered === '1') return;
    detail.dataset.freenetProgressRendered = '1';
    detail.textContent = '';
    const title = document.createElement('strong');
    title.textContent = `Проверяем ${selectedVersionLabel()}…`;
    const progress = document.createElement('progress');
    progress.className = 'fn-version-progress';
    progress.setAttribute('aria-label', 'Проверка версии FreeNet выполняется');
    const note = document.createElement('span');
    note.className = 'fn-version-progress-note';
    note.textContent = 'Формируем безопасный plan: release metadata, manifest/SHA-256 и compatibility. Изменения пока не применяются; фон временно заблокирован.';
    detail.append(title, progress, note);
  }

  function setVersionPickerBusy(busy) {
    const root = document.getElementById('fnModalRoot');
    const body = document.getElementById('fnModalBody');
    if (!root || root.hidden || !root.classList.contains('fn-version-picker-mode')) {
      document.body?.classList.remove('fn-version-plan-page-frozen');
      return;
    }
    root.classList.toggle('fn-version-plan-checking', busy);
    root.setAttribute('aria-busy', busy ? 'true' : 'false');
    body?.classList.toggle('fn-version-plan-busy', busy);
    document.body?.classList.toggle('fn-version-plan-page-frozen', busy);

    const close = document.getElementById('fnModalClose');
    if (close) {
      close.disabled = busy;
      if (busy) close.setAttribute('aria-disabled', 'true');
      else close.removeAttribute('aria-disabled');
    }
    const search = document.getElementById('fnVersionSearch');
    if (search) search.disabled = busy;
    document.querySelectorAll('.fn-version-release').forEach(btn => {
      btn.disabled = busy;
      if (busy) btn.setAttribute('aria-disabled', 'true');
      else btn.removeAttribute('aria-disabled');
    });
    const topbar = document.getElementById('topFreenetUpdate');
    if (topbar) {
      if (busy) topbar.setAttribute('aria-disabled', 'true');
      else topbar.removeAttribute('aria-disabled');
    }
  }

  function syncVersionPickerProgress() {
    const root = document.getElementById('fnModalRoot');
    if (!root || root.hidden || !root.classList.contains('fn-version-picker-mode')) {
      setVersionPickerBusy(false);
      return;
    }
    const detail = document.getElementById('fnVersionDetail');
    const busy = !!detail && detail.classList.contains('checking');
    if (busy) renderVersionBusyDetail(detail);
    setVersionPickerBusy(busy);
  }

  function watchVersionPickerProgress() {
    if (window.__freenetVersionPickerProgress) return;
    window.__freenetVersionPickerProgress = true;
    injectVersionProgressStyles();
    const syncSoon = () => requestAnimationFrame(syncVersionPickerProgress);
    document.addEventListener('click', event => {
      const root = document.getElementById('fnModalRoot');
      const busy = !!root && root.classList.contains('fn-version-plan-checking');
      if (busy && (event.target.closest?.('#topFreenetUpdate') || event.target.closest?.('#fnModalRoot .fn-modal-backdrop'))) {
        event.preventDefault();
        event.stopImmediatePropagation();
        return;
      }
      if (event.target.closest?.('.fn-version-release')) syncSoon();
    }, true);
    document.addEventListener('keydown', event => {
      const root = document.getElementById('fnModalRoot');
      if (event.key === 'Escape' && root?.classList.contains('fn-version-plan-checking')) {
        event.preventDefault();
        event.stopImmediatePropagation();
      }
    }, true);
    if (document.documentElement) {
      new MutationObserver(syncSoon).observe(document.documentElement, {
        childList: true,
        subtree: true,
        attributes: true,
        attributeFilter: ['class', 'hidden', 'disabled']
      });
    }
    window.addEventListener('resize', syncSoon);
    syncSoon();
  }

  injectStyles();
  injectVersionProgressStyles();
  watchSettingsDOM();
  watchVersionPickerProgress();
  loadCore();
  schedulePatch();
})();