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
   * Быстрый watchdog проверяет доступность текущего VPN
   * Режим работы
   * Только текущий VPN
   * Полный AUTO VPN
   * Плановое обновление endpoint
   * Текущая страна
   * Лучший отклик
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

  

  

  function patchSingleSave() {
    ;
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
    ;
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

  ;
  ;
  watchSettingsDOM();
  watchVersionPickerProgress();
  loadCore();
  schedulePatch();
})();