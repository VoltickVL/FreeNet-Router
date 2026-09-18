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
      .fn-xray-topbar{appearance:none;display:grid;grid-template-columns:25px auto;grid-template-rows:auto auto;column-gap:8px;align-items:center;min-width:112px;min-height:45px;padding:7px 11px;border:1px solid #294b70;border-radius:11px;background:linear-gradient(180deg,#0d2135,#0a1929);color:#eaf2fb;text-align:left;cursor:pointer;font:inherit}
      .fn-xray-topbar:hover{border-color:#4d7aa6;background:linear-gradient(180deg,#12304e,#0d2238)}.fn-xray-topbar:focus-visible{outline:0;border-color:#69a0ff;box-shadow:0 0 0 3px rgba(74,139,255,.15)}
      .fn-xray-topbar-icon{grid-row:1/3;display:grid;place-items:center;width:22px;height:22px;color:#76aaff}.fn-xray-topbar-icon svg{width:22px;height:22px}.fn-xray-topbar>span:not(.fn-xray-topbar-icon){font-size:9px;line-height:1.05;color:#829ab9}.fn-xray-topbar>strong{font-size:11px;line-height:1.15;color:#f2f7ff;white-space:nowrap}
      @media(max-width:1180px){.fn-xray-topbar{min-width:102px;padding-left:9px;padding-right:9px}}
      .xcm-root{position:fixed;inset:0;z-index:1450;display:grid;place-items:center;padding:22px}.xcm-root[hidden]{display:none!important}.xcm-backdrop{position:absolute;inset:0;background:rgba(2,8,16,.78);backdrop-filter:blur(14px)}
      .xcm-modal{position:relative;width:min(760px,calc(100vw - 32px));max-height:min(86vh,820px);overflow:auto;border:1px solid #355273;border-radius:18px;background:linear-gradient(180deg,#11243a,#091725);box-shadow:0 34px 110px rgba(0,0,0,.58);padding:20px}.xcm-head{display:flex;align-items:flex-start;justify-content:space-between;gap:18px}.xcm-kicker{color:#70a1ff;font-size:10px;font-weight:900;letter-spacing:.1em;text-transform:uppercase}.xcm-head h3{margin:4px 0 0;color:#f4f8ff;font-size:22px;letter-spacing:-.02em}.xcm-close{appearance:none;width:36px;height:36px;border:1px solid #314a69;border-radius:10px;background:#0a1827;color:#dce8f7;font-size:20px;cursor:pointer}.xcm-summary{margin-top:14px;padding:11px 13px;border:1px solid #28425f;border-radius:12px;background:#091827;color:#aabdd5;font-size:12px;line-height:1.5}.xcm-summary b{color:#eef5ff}
      .xcm-list{display:grid;gap:8px;margin-top:14px}.xcm-release{appearance:none;width:100%;display:grid;grid-template-columns:minmax(105px,.55fr) minmax(0,2fr);gap:12px;text-align:left;border:1px solid #263f5c;border-radius:12px;background:#081625;color:#dbe7f6;padding:12px;cursor:pointer}.xcm-release:hover:not(:disabled),.xcm-release.selected{border-color:#5b8cff;background:#102844}.xcm-release:disabled{cursor:not-allowed;opacity:.48}.xcm-release-main{display:flex;align-items:center;gap:7px;flex-wrap:wrap}.xcm-release-version{font-size:13px;font-weight:900;color:#f5f8fd}.xcm-badge{display:inline-flex;align-items:center;padding:3px 6px;border-radius:999px;background:#163552;color:#a9caff;font-size:8px;font-weight:900;letter-spacing:.04em;text-transform:uppercase}.xcm-badge.current{background:rgba(52,221,159,.14);color:#65e3aa}.xcm-badge.latest{background:rgba(81,137,255,.18);color:#8bb4ff}.xcm-badge.preview{background:rgba(255,190,74,.13);color:#ffd17a}.xcm-release-date{margin-top:6px;color:#839ab7;font-size:10px}.xcm-release-desc{color:#a9bad0;font-size:11px;line-height:1.5}.xcm-empty{padding:18px;border:1px dashed #2c4664;border-radius:12px;color:#8da3bf;text-align:center;font-size:12px}
      .xcm-error{margin-top:14px;padding:11px 13px;border:1px solid rgba(255,101,112,.42);border-radius:11px;background:rgba(83,21,31,.28);color:#ffc8cd;font-size:12px;line-height:1.5}.xcm-confirm{margin-top:14px;padding:14px;border:1px solid #315071;border-radius:12px;background:#091a2a;color:#c8d7e9}.xcm-confirm strong{color:#fff}.xcm-progress{margin-top:14px;padding:15px;border:1px solid #315071;border-radius:12px;background:#091a2a;color:#c6d5e7;font-size:12px;line-height:1.6}.xcm-progress::before{content:'';display:inline-block;width:9px;height:9px;margin-right:8px;border:2px solid #6b93d0;border-top-color:transparent;border-radius:50%;animation:xcm-spin .8s linear infinite}@keyframes xcm-spin{to{transform:rotate(360deg)}}
      .xcm-result{margin-top:14px;padding:14px;border-radius:12px;font-size:12px;line-height:1.55}.xcm-result.ok{border:1px solid rgba(58,220,158,.4);background:rgba(18,83,60,.28);color:#c9f5df}.xcm-result.bad{border:1px solid rgba(255,104,115,.42);background:rgba(83,21,31,.28);color:#ffd0d4}.xcm-result.stop{border-color:#ff6571;color:#fff0f1}.xcm-actions{display:flex;justify-content:flex-end;gap:9px;margin-top:16px}.xcm-btn{appearance:none;border:1px solid #365473;border-radius:10px;background:#10233a;color:#eaf2fb;padding:9px 13px;font:inherit;font-size:11px;font-weight:850;cursor:pointer}.xcm-btn:hover:not(:disabled){border-color:#6593ff}.xcm-btn.primary{border-color:#5d8dff;background:#3974ee;color:#fff}.xcm-btn:disabled{opacity:.42;cursor:not-allowed}
      @media(max-width:620px){.xcm-modal{padding:16px}.xcm-release{grid-template-columns:1fr}.xcm-actions{display:grid;grid-template-columns:1fr}.xcm-btn{width:100%}}
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
      <section class="xcm-modal" role="dialog" aria-modal="true" aria-labelledby="xcmTitle">
        <div class="xcm-head"><div><div class="xcm-kicker">Xray Core Manager</div><h3 id="xcmTitle">Версия Xray</h3></div><button id="xcmClose" class="xcm-close" type="button" aria-label="Закрыть">×</button></div>
        <div id="xcmBody"></div>
        <div id="xcmActions" class="xcm-actions"></div>
      </section>`;
    document.body.appendChild(root);
    qs('#xcmClose', root).addEventListener('click', closeManager);
    qs('.xcm-backdrop', root).addEventListener('click', closeManager);
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
      chip.innerHTML = `<span class="fn-xray-topbar-icon">${topbarIcon()}</span><span>Xray</span><strong id="xrayTopbarVersion">версия…</strong>`;
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
      const response = await fetch('/api/xray/core/catalog', {cache:'no-store'});
      if (response.status === 401) {
        if (topbarVersionAttempts < 4) setTimeout(refreshTopbarVersion, 900);
        return;
      }
      const result = await response.json();
      if (response.ok && result && result.success) syncTopbarVersion(result.current_version);
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

  function closeManager() {
    if (applying) return;
    const root = qs('#xrayCoreManager');
    if (root) root.hidden = true;
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
    summary.innerHTML = `Текущая версия: <b>${current}</b> · Последняя стабильная: <b>${latest}</b><br>Предрелизы отмечены отдельно. Платформа: ${catalog.architecture || '—'}. Выбор версии сам по себе ничего не изменяет.`;
    target.appendChild(summary);

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
      const desc = document.createElement('div'); desc.className = 'xcm-release-desc'; desc.textContent = release.description || (release.asset?.available ? 'Официальный релиз XTLS/Xray-core.' : 'Нет проверенного файла для архитектуры этого роутера.');
      option.append(left, desc);
      option.addEventListener('click', () => { selectedVersion = release.version; renderCatalog(); });
      list.appendChild(option);
    });

    addButton(footer, 'Закрыть', '', closeManager);
    const selected = releases.find(item => item.version === selectedVersion);
    if (selected && !selected.current && selected.asset?.available) addButton(footer, actionLabel(selected), 'primary', () => renderConfirmation(selected));
  }

  function renderConfirmation(release) {
    const target = body(); const footer = actions(); clearNode(target); clearNode(footer);
    const box = document.createElement('div'); box.className = 'xcm-confirm';
    const previous = catalog.current_version || 'текущая версия';
    box.innerHTML = `<strong>${actionLabel(release)}</strong><br><br>${previous} → ${release.version}<br><br>FreeNet сначала проверит текущий конфиг и скачанный Xray, сверит SHA-256, создаст резервную копию, затем выполнит один контролируемый перезапуск. При неуспешном post-check предыдущая версия будет восстановлена автоматически.`;
    target.appendChild(box);
    addButton(footer, 'Назад', '', renderCatalog);
    addButton(footer, actionLabel(release), 'primary', () => applyRelease(release));
  }

  async function applyRelease(release) {
    if (applying) return;
    applying = true;
    const target = body(); const footer = actions(); clearNode(target); clearNode(footer);
    const progress = document.createElement('div'); progress.className = 'xcm-progress'; progress.textContent = `Устанавливаю ${release.version}. Не закрывайте страницу и не выключайте роутер.`; target.appendChild(progress);
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
    const root = ensureRoot(); root.hidden = false;
    selectedVersion = '';
    const target = body(); const footer = actions(); clearNode(target); clearNode(footer);
    const progress = document.createElement('div'); progress.className = 'xcm-progress'; progress.textContent = 'Получаю список официальных версий Xray…'; target.appendChild(progress);
    try {
      const response = await fetch('/api/xray/core/catalog', {cache:'no-store'});
      const result = await response.json();
      if (!response.ok || !result.success) throw new Error(result.error || 'Каталог Xray недоступен');
      catalog = result;
      selectedVersion = result.current_version || '';
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
})();
