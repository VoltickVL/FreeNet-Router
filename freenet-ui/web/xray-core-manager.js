(() => {
  'use strict';

  const qs = (s, root = document) => root.querySelector(s);
  let catalog = null;
  let selectedVersion = '';
  let applying = false;
  let topbarVersionLoading = false;
  let topbarVersionAttempts = 0;

  function installStyles() {
    if (qs('#xrayCoreManagerStyles')) return;
    const style = document.createElement('style');
    style.id = 'xrayCoreManagerStyles';
    style.textContent = `
      .cs-version{cursor:pointer!important}.cs-version:hover{border-color:#5b8cff!important;background:#183253!important;color:#fff!important}
      .fn-xray-topbar{appearance:none;display:grid;grid-template-columns:30px minmax(0,1fr);gap:11px;align-items:center;text-align:left;width:148px;height:60px;padding:9px 14px;border:1px solid #315276;border-radius:11px;background:linear-gradient(180deg,#0d1d30,#0a1727);color:#f3f7ff;font:inherit;cursor:pointer}
      .fn-xray-topbar:hover,.fn-xray-topbar[aria-expanded="true"]{border-color:#6597d9;background:#10243c}.fn-xray-topbar:focus-visible{outline:2px solid #80adff;outline-offset:2px}
      .fn-xray-topbar-icon{display:grid;place-items:center;width:30px;height:30px;color:#72a8ff}.fn-xray-topbar-icon svg{width:26px;height:26px}
      .fn-xray-chip{display:flex;flex-direction:column;justify-content:center;gap:3px;min-width:0}.fn-xray-chip small{font-weight:750;color:#8da4c2;font-size:11px;line-height:1.05}.fn-xray-chip strong{font-size:14px;line-height:1.15;font-weight:750;color:#f3f7ff;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
      .xcm-root{position:fixed;inset:0;z-index:2700;display:block;pointer-events:none}.xcm-root[hidden]{display:none!important}.xcm-root.xcm-open{pointer-events:none}
      .xcm-backdrop{display:none!important}
      .xcm-modal{position:fixed;pointer-events:auto;box-sizing:border-box;display:flex;flex-direction:column;width:500px;max-width:calc(100vw - 24px);max-height:min(78vh,650px);overflow:hidden;border:1px solid #355473;border-radius:12px;background:#0c1c2e;box-shadow:0 18px 46px rgba(0,0,0,.52);color:#eef4ff;font-family:Inter,ui-sans-serif,system-ui,sans-serif}
      .xcm-modal *{box-sizing:border-box}.xcm-head{display:flex;align-items:flex-start;justify-content:space-between;gap:12px;padding:15px 16px;border-bottom:1px solid #28415b;flex:none}
      .xcm-kicker{color:#8da4c2;font-size:9px;font-weight:750;line-height:1.1}.xcm-head h3{margin:3px 0 0;color:#f4f8ff;font-size:17px;line-height:1.3;font-weight:750}.xcm-subtitle{margin-top:3px;color:#8198b5;font-size:9px;line-height:1.35}
      .xcm-close{appearance:none;display:grid;place-items:center;flex:none;width:30px;height:30px;border:1px solid #365674;border-radius:8px;background:#11273e;color:#b3c5da;cursor:pointer;font-size:20px}.xcm-close:hover{border-color:#6597d9;color:#fff}.xcm-close:focus-visible,.xcm-btn:focus-visible,.xcm-release:focus-visible{outline:2px solid #80adff;outline-offset:2px}
      #xcmBody{min-height:0;overflow:auto;padding:0 16px 12px;overscroll-behavior:contain}.xcm-summary{position:sticky;top:0;z-index:2;margin:0 -16px 12px;padding:12px 16px;border-bottom:1px solid #28415b;background:#0c1c2ef2;color:#92a8c3;font-size:11px;line-height:1.45;backdrop-filter:blur(10px)}.xcm-summary b{color:#eef5ff}
      .xcm-current{display:flex;align-items:center;gap:10px;margin:10px 0;padding:10px 11px;border:1px solid #284863;border-radius:10px;background:#0b2238}.xcm-current-icon{display:grid;place-items:center;width:34px;height:34px;flex:none;border-radius:9px;background:#12345a;color:#72a8ff}.xcm-current-copy{display:grid;gap:2px;min-width:0}.xcm-current-copy small{color:#8da4c2;font-size:9px}.xcm-current-copy strong{font-size:14px;color:#f4f8ff}.xcm-current-copy span{font-size:10px;color:#90a8c5}
      .xcm-search{width:100%;height:38px;margin:0 0 9px;padding:0 11px;border:1px solid #315276;border-radius:9px;background:#081827;color:#eef5ff;font:inherit;font-size:11px;outline:none}.xcm-search:focus{border-color:#6597d9;box-shadow:0 0 0 2px rgba(101,151,217,.12)}.xcm-list{display:grid;gap:6px;margin-top:0}.xcm-release{appearance:none;width:100%;display:grid;grid-template-columns:minmax(105px,.65fr) minmax(0,1.8fr);gap:10px;text-align:left;border:1px solid #263f5c;border-radius:10px;background:#081827;color:#dbe7f6;padding:10px 11px;cursor:pointer}.xcm-release:hover:not(:disabled),.xcm-release.selected{border-color:#4f82bf;background:#15304d}.xcm-release.selected{box-shadow:inset 3px 0 #5b9cff}.xcm-release:disabled{cursor:not-allowed;opacity:.48}
      .xcm-release-main{display:flex;align-items:center;gap:6px;flex-wrap:wrap}.xcm-release-version{font-size:12px;font-weight:800;color:#f5f8fd}.xcm-badge{display:inline-flex;align-items:center;padding:2px 6px;border-radius:999px;background:#163552;color:#a9caff;font-size:8px;font-weight:800}.xcm-badge.current{background:rgba(52,221,159,.14);color:#65e3aa}.xcm-badge.latest{background:rgba(81,137,255,.18);color:#8bb4ff}.xcm-badge.preview{background:rgba(255,190,74,.13);color:#ffd17a}.xcm-release-date{margin-top:5px;color:#839ab7;font-size:9px}.xcm-release-desc{color:#9db0c7;font-size:10px;line-height:1.4}
      .xcm-empty{padding:16px;border:1px dashed #2c4664;border-radius:11px;color:#8da3bf;text-align:center;font-size:11px}.xcm-error,.xcm-confirm,.xcm-progress,.xcm-result{margin-top:14px;padding:12px 13px;border-radius:11px;font-size:11px;line-height:1.5}.xcm-error{border:1px solid rgba(255,101,112,.42);background:rgba(83,21,31,.28);color:#ffc8cd}.xcm-confirm,.xcm-progress{border:1px solid #315071;background:#091a2a;color:#c8d7e9}.xcm-confirm strong{color:#fff}.xcm-progress::before{content:'';display:inline-block;width:9px;height:9px;margin-right:8px;border:2px solid #6b93d0;border-top-color:transparent;border-radius:50%;animation:xcm-spin .8s linear infinite}@keyframes xcm-spin{to{transform:rotate(360deg)}}.xcm-result.ok{border:1px solid rgba(58,220,158,.4);background:rgba(18,83,60,.28);color:#c9f5df}.xcm-result.bad{border:1px solid rgba(255,104,115,.42);background:rgba(83,21,31,.28);color:#ffd0d4}.xcm-result.stop{border-color:#ff6571;color:#fff0f1}
      .xcm-actions{flex:none;display:flex;justify-content:flex-end;gap:8px;padding:10px 12px;border-top:1px solid #28415b;background:#0b1b2d}.xcm-btn{appearance:none;min-height:44px;border:1px solid #365473;border-radius:9px;background:#10233a;color:#eaf2fb;padding:8px 13px;font:inherit;font-size:11px;font-weight:800;cursor:pointer}.xcm-btn:hover:not(:disabled){border-color:#6593ff}.xcm-btn.primary{min-width:210px;border-color:#5d8dff;background:linear-gradient(180deg,#347eff,#2367e7);color:#fff}.xcm-btn:disabled{opacity:.42;cursor:not-allowed}
      @media(max-width:760px){.fn-xray-topbar{width:50px;min-width:50px;padding:0;place-items:center}.fn-xray-chip{display:none}.xcm-modal{width:calc(100vw - 24px)!important;max-width:none;max-height:calc(100vh - 24px)}.xcm-release{grid-template-columns:1fr}.xcm-actions{display:grid;grid-template-columns:1fr}.xcm-btn,.xcm-btn.primary{width:100%;min-width:0}}
      @media(max-height:580px) and (min-width:761px){.xcm-modal{max-height:calc(100vh - 16px)}.xcm-head{padding-top:9px;padding-bottom:9px}.xcm-current{margin:7px 0;padding:8px 10px}.xcm-release{padding-top:7px;padding-bottom:7px}.xcm-release-desc{display:none}}
    `;
    document.head.appendChild(style);
  }

  function rootMarkup() {
    const root = document.createElement('div');
    root.id = 'xrayCoreManager';
    root.className = 'xcm-root';
    root.hidden = true;
    root.innerHTML = `
      <div class="xcm-backdrop"></div>
      <section class="xcm-modal" role="dialog" aria-modal="false" aria-labelledby="xcmTitle" tabindex="-1">
        <div class="xcm-head"><div><div class="xcm-kicker">Xray</div><h3 id="xcmTitle">Версия Xray Core</h3><div class="xcm-subtitle">Выбор версии, затем проверка и установка</div></div><button id="xcmClose" class="xcm-close" type="button" aria-label="Закрыть">×</button></div>
        <div id="xcmBody"></div>
        <div id="xcmActions" class="xcm-actions"></div>
      </section>`;
    document.body.appendChild(root);
    qs('#xcmClose', root).addEventListener('click', closeManager);
    return root;
  }

  function ensureRoot() { return qs('#xrayCoreManager') || rootMarkup(); }

  function topbarIcon() {
    return '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 3 20 8v8l-8 5-8-5V8l8-5Z"/><path d="m8.5 9.5 7 5m0-5-7 5"/></svg>';
  }

  function syncTopbarVersion(value) {
    const node = qs('#xrayTopbarVersion');
    if (!node) return;
    node.textContent = compactVersion(value) || String(value || '').trim() || 'версия…';
  }

  function mountTopbarChip() {
    const topbar = qs('.topbar.overview-approved') || qs('.topbar');
    if (!topbar) return null;
    let chip = qs('#xrayTopbarChip');
    if (!chip) {
      chip = document.createElement('button');
      chip.id = 'xrayTopbarChip';
      chip.type = 'button';
      chip.className = 'fn-xray-topbar';
      chip.title = 'Версия и обновление Xray Core';
      chip.setAttribute('aria-haspopup', 'dialog');
      chip.setAttribute('aria-controls', 'xrayCoreManager');
      chip.setAttribute('aria-expanded', 'false');
      chip.innerHTML = `<span class="fn-xray-topbar-icon">${topbarIcon()}</span><span class="fn-xray-chip"><small>Xray</small><strong id="xrayTopbarVersion">версия…</strong></span>`;
      chip.addEventListener('click', event => { event.preventDefault(); openManager(); });
    }
    const facts = qs('#overviewApprovedTop');
    if (facts && chip.parentNode !== facts) facts.insertBefore(chip, facts.firstChild);
    else if (!facts && chip.parentNode !== topbar) {
      const actions = qs('.top-actions');
      topbar.insertBefore(chip, actions || null);
    }
    return chip;
  }

  async function refreshTopbarVersion() {
    if (topbarVersionLoading) return;
    const chip = mountTopbarChip();
    if (!chip) return;
    topbarVersionLoading = true;
    topbarVersionAttempts += 1;
    try {
      const response = await fetch('/api/xray/service', {cache:'no-store'});
      if (response.status === 401) {
        if (topbarVersionAttempts < 4) setTimeout(refreshTopbarVersion, 900);
        return;
      }
      const result = await response.json();
      if (response.ok && result && result.success) syncTopbarVersion(result.version || result.current_version);
    } catch (_) {
      if (topbarVersionAttempts < 3) setTimeout(refreshTopbarVersion, 1200);
    } finally {
      topbarVersionLoading = false;
    }
  }

  function body() { return qs('#xcmBody', ensureRoot()); }
  function actions() { return qs('#xcmActions', ensureRoot()); }

  function clearNode(node) { while (node.firstChild) node.removeChild(node.firstChild); }
  function addButton(parent, text, className, handler, disabled = false) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = `xcm-btn${className ? ' ' + className : ''}`;
    button.textContent = text;
    button.disabled = !!disabled;
    button.addEventListener('click', handler);
    parent.appendChild(button);
    return button;
  }

  function compactVersion(raw) {
    const match = String(raw || '').match(/\bv?\d+(?:\.\d+){1,3}(?:[-+][0-9A-Za-z.-]+)?/);
    if (!match) return '';
    return match[0].startsWith('v') ? match[0] : `v${match[0]}`;
  }

  function formatDate(raw) {
    const date = new Date(raw);
    if (Number.isNaN(date.getTime())) return '';
    return date.toLocaleDateString('ru-RU', {day:'2-digit', month:'2-digit', year:'numeric'});
  }

  let focusBeforeOpen = null;

  function positionManager() {
    const modal = qs('#xrayCoreManager .xcm-modal');
    const chip = qs('#xrayTopbarChip');
    if (!modal || !chip || qs('#xrayCoreManager')?.hidden) return;
    const vw = window.visualViewport?.width || window.innerWidth;
    const vh = window.visualViewport?.height || window.innerHeight;
    const rect = chip.getBoundingClientRect();
    const width = Math.min(500, vw - 24);
    modal.style.width = width + 'px';
    const left = Math.max(12, Math.min(vw - width - 12, rect.right - width));
    modal.style.left = left + 'px';
    const desiredTop = rect.bottom + 8;
    const modalHeight = Math.min(modal.scrollHeight || 650, Math.max(220, vh - 24));
    const top = Math.max(12, Math.min(desiredTop, vh - modalHeight - 12));
    modal.style.top = top + 'px';
  }

  function closeManager() {
    if (applying) return;
    const root = qs('#xrayCoreManager');
    if (!root || root.hidden) return;
    root.hidden = true;
    root.classList.remove('xcm-open');
    qs('#xrayTopbarChip')?.setAttribute('aria-expanded', 'false');
    if (focusBeforeOpen?.isConnected) focusBeforeOpen.focus({preventScroll:true});
    focusBeforeOpen = null;
  }

  function actionLabel(release) {
    if (!catalog || !release || release.current) return 'Установлено';
    const currentIndex = catalog.releases.findIndex(item => item.current);
    const targetIndex = catalog.releases.findIndex(item => item.version === release.version);
    if (currentIndex >= 0 && targetIndex >= 0) {
      if (targetIndex < currentIndex) return `Обновить до ${release.version}`;
      if (targetIndex > currentIndex) return `Откатить до ${release.version}`;
    }
    if (release.latest) return `Обновить до ${release.version}`;
    return `Установить ${release.version}`;
  }

  function renderCatalog() {
    const target = body();
    const footer = actions();
    clearNode(target); clearNode(footer);
    const summary = document.createElement('div');
    summary.className = 'xcm-summary';
    const current = catalog.current_version || 'не определена';
    const latest = catalog.latest_version || 'не определена';
    syncTopbarVersion(catalog.current_version || '');
    summary.innerHTML = `Установлена <b>${current}</b> · Доступна <b>${latest}</b>`;
    target.appendChild(summary);

    const search = document.createElement('input');
    search.type = 'search';
    search.className = 'xcm-search';
    search.autocomplete = 'off';
    search.placeholder = 'Поиск версии Xray';
    search.setAttribute('aria-label', 'Поиск версии Xray');
    target.appendChild(search);

    const list = document.createElement('div'); list.className = 'xcm-list'; target.appendChild(list);
    const releases = Array.isArray(catalog.releases) ? catalog.releases : [];
    if (!releases.length) {
      const empty = document.createElement('div'); empty.className = 'xcm-empty'; empty.textContent = 'Доступные версии Xray не найдены.'; list.appendChild(empty);
    }
    releases.forEach(release => {
      const option = document.createElement('button');
      option.type = 'button';
      option.className = `xcm-release${release.version === selectedVersion ? ' selected' : ''}`;
      option.disabled = !release.asset?.available;
      option.dataset.version = release.version || '';
      const left = document.createElement('div');
      const main = document.createElement('div'); main.className = 'xcm-release-main';
      const version = document.createElement('span'); version.className = 'xcm-release-version'; version.textContent = release.version || '—'; main.appendChild(version);
      if (release.current) { const badge = document.createElement('span'); badge.className = 'xcm-badge current'; badge.textContent = 'Текущая'; main.appendChild(badge); }
      if (release.latest) { const badge = document.createElement('span'); badge.className = 'xcm-badge latest'; badge.textContent = 'Последняя'; main.appendChild(badge); }
      if (release.prerelease) { const badge = document.createElement('span'); badge.className = 'xcm-badge preview'; badge.textContent = 'Предрелиз'; main.appendChild(badge); }
      left.appendChild(main);
      const date = document.createElement('div'); date.className = 'xcm-release-date'; date.textContent = formatDate(release.published_at); left.appendChild(date);
      const desc = document.createElement('div'); desc.className = 'xcm-release-desc'; desc.textContent = release.asset?.available ? (release.description || 'Официальная версия Xray.') : 'Нет подходящего файла для этого роутера.';
      option.append(left, desc);
      option.addEventListener('click', () => { selectedVersion = release.version; renderCatalog(); });
      list.appendChild(option);
    });
    search.addEventListener('input', () => {
      const query = search.value.trim().toLowerCase();
      list.querySelectorAll('.xcm-release').forEach(option => {
        option.hidden = !!query && !option.textContent.toLowerCase().includes(query);
      });
    });

    addButton(footer, 'Закрыть', '', closeManager);
    const selected = releases.find(item => item.version === selectedVersion);
    if (selected && !selected.current && selected.asset?.available) addButton(footer, actionLabel(selected), 'primary', () => applyRelease(selected));
  }

  async function applyRelease(release) {
    if (applying) return;
    applying = true;
    const target = body(); const footer = actions(); clearNode(target); clearNode(footer);
    const progress = document.createElement('div'); progress.className = 'xcm-progress'; progress.textContent = `Устанавливаем Xray ${release.version}…`; target.appendChild(progress);
    try {
      const response = await fetch('/api/xray/core/apply', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({target_version:release.version}), cache:'no-store'});
      let result = {}; try { result = await response.json(); } catch (_) {}
      clearNode(target);
      const box = document.createElement('div');
      if (response.ok && result.success) {
        box.className = 'xcm-result ok';
        box.textContent = result.message || `Xray ${release.version} установлен.`;
        const chip = qs('#csServiceVersion'); if (chip) chip.textContent = `${result.current_version || release.version} ▾`;
        syncTopbarVersion(result.current_version || release.version);
        target.appendChild(box);
        addButton(footer, 'Готово', 'primary', () => location.reload());
      } else {
        const rollbackFailed = result.rollback === 'FAILED';
        box.className = `xcm-result bad${rollbackFailed ? ' stop' : ''}`;
        const prefix = rollbackFailed ? 'Операция остановлена. Автоматический откат не подтверждён. ' : result.rollback === 'SUCCESS' ? 'Изменение отменено, предыдущая версия восстановлена. ' : '';
        box.textContent = prefix + (result.error || 'Не удалось изменить версию Xray.');
        target.appendChild(box);
        addButton(footer, 'Закрыть', '', closeManager);
        addButton(footer, 'Обновить состояние', 'primary', () => location.reload());
      }
    } catch (_) {
      clearNode(target);
      const box = document.createElement('div'); box.className = 'xcm-result bad'; box.textContent = 'Связь с FreeNet прервалась во время операции. Не повторяйте установку вслепую: сначала обновите состояние.'; target.appendChild(box);
      addButton(footer, 'Обновить состояние', 'primary', () => location.reload());
    } finally {
      applying = false;
    }
  }

  async function openManager() {
    installStyles();
    const root = ensureRoot();
    if (!root.hidden) return;
    focusBeforeOpen = document.activeElement;
    root.hidden = false;
    root.classList.add('xcm-open');
    qs('#xrayTopbarChip')?.setAttribute('aria-expanded', 'true');
    positionManager();
    requestAnimationFrame(() => qs('.xcm-modal', root)?.focus({preventScroll:true}));
    selectedVersion = '';
    const target = body(); const footer = actions(); clearNode(target); clearNode(footer);
    const progress = document.createElement('div'); progress.className = 'xcm-progress'; progress.textContent = 'Получаю список официальных версий Xray…'; target.appendChild(progress);
    try {
      const response = await fetch('/api/xray/core/catalog', {cache:'no-store'});
      const result = await response.json();
      if (!response.ok || !result.success) throw new Error(result.error || 'Каталог Xray недоступен');
      catalog = result;
      selectedVersion = result.latest_version && result.latest_version !== result.current_version
        ? result.latest_version
        : (result.current_version || '');
      renderCatalog();
    } catch (error) {
      clearNode(target); clearNode(footer);
      const box = document.createElement('div'); box.className = 'xcm-error'; box.textContent = error.message || 'Каталог Xray недоступен.'; target.appendChild(box);
      addButton(footer, 'Закрыть', '', closeManager);
    }
  }

  installStyles();
  document.addEventListener('click', event => {
    const chip = event.target.closest?.('#csServiceVersion');
    if (!chip) return;
    event.preventDefault();
    openManager();
  });
  const bootTopbar = () => {
    mountTopbarChip();
    refreshTopbarVersion();
    setTimeout(mountTopbarChip, 250);
    setTimeout(mountTopbarChip, 900);
  };
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', bootTopbar, {once:true});
  else bootTopbar();
  window.addEventListener('hashchange', mountTopbarChip);
  window.addEventListener('resize', positionManager);
  window.visualViewport?.addEventListener('resize', positionManager);
  document.addEventListener('click', event => {
    const root = qs('#xrayCoreManager');
    if (!root || root.hidden || applying) return;
    if (event.target.closest?.('#xrayTopbarChip') || event.target.closest?.('#xrayCoreManager .xcm-modal')) return;
    closeManager();
  }, true);
  document.addEventListener('keydown', event => {
    if (event.key !== 'Escape' || qs('#xrayCoreManager')?.hidden || applying) return;
    event.preventDefault();
    closeManager();
  });
})();
