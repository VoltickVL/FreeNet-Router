(() => {
  const qs = (s, root = document) => root.querySelector(s);
  let plan = null;
  let polling = false;
  let activeModalConfirm = null;

  function mountTypographyReadability() {
    if (qs('#freenetTypographyReadability')) return;
    const style = document.createElement('style');
    style.id = 'freenetTypographyReadability';
    style.textContent = `
      :root{--fn-text-body:14px;--fn-text-secondary:12.5px;--fn-text-small:11px}
      [hidden]{display:none!important}
      body{font-size:var(--fn-text-body)}
      .nav-btn{font-size:14px;line-height:1.35}
      .top-title{font-size:15px}
      .top-status{font-size:13px;line-height:1.4}
      .mini-link,.mini-btn{font-size:12px}
      .page-head h1{font-size:28px}
      .page-head p{font-size:14px;line-height:1.5}
      .page-kicker{font-size:11.5px}
      .card h2{font-size:15.5px}
      .card-title-lg{font-size:21px!important}
      .summary-state{font-size:12px;line-height:1.4}
      .eyebrow{font-size:11px}
      .country p{font-size:14px}
      .endpoint strong{font-size:14.5px}
      .health b{font-size:12.5px}
      .health span{font-size:11.5px;line-height:1.4}
      .btn{font-size:13.5px;line-height:1.3}
      .btn .sub{font-size:11px;line-height:1.35}
      .field label{font-size:11px}
      .field input,.field select{font-size:13.5px}
      .hint{font-size:12.5px;line-height:1.55}
      .notice{font-size:12.5px;line-height:1.55}
      .details summary{font-size:12px}
      .profile-trigger{font-size:13.5px}
      .profile-option{min-height:54px;align-items:center}
      .profile-option-main{font-size:13.5px;line-height:1.35}
      .profile-option-endpoint{font-size:12.5px;line-height:1.35;margin-top:3px;color:#9fb4d2}
      .selected-profile{font-size:12.5px;line-height:1.5}
      .selected-profile strong{font-size:13.5px}
      .selected-endpoint{font-size:13px;color:#b9c9df}
      .status-pill b{font-size:12px}
      .status-pill span{font-size:11.5px;line-height:1.4}
      .setting-card h3{font-size:13.5px}
      .setting-card p{font-size:12px;line-height:1.55}
      .coming{font-size:10.5px}
      .setup-banner{font-size:12.5px;line-height:1.5}
      .auth-wrap{position:fixed!important;inset:0;z-index:1000;display:grid;place-items:center;padding:24px;background:rgba(3,9,17,.82);backdrop-filter:blur(18px)}
      .auth-card{width:min(440px,calc(100vw - 32px));box-shadow:0 32px 100px rgba(0,0,0,.55)}
      .auth-card p{font-size:13px;line-height:1.55}
      .footer{font-size:11.5px}
      @media(max-width:600px){
        .page-head h1{font-size:25px}
        .nav-btn{font-size:13.5px}
        .hint,.notice{font-size:12px}
      }`;
    document.head.appendChild(style);
  }

  function mountDashboardStability() {
    const quick = qs('#quickActionsSection');
    const profiles = qs('#profilesList');
    const selected = qs('#selectedProfileCard');
    if (!quick || !profiles) return;

    if (!qs('#freenetDashboardStability')) {
      const style = document.createElement('style');
      style.id = 'freenetDashboardStability';
      style.textContent = `
        #profilesList.profiles{display:block}
        #profilesList{min-height:178px}
        #selectedProfileCard{min-height:78px}
        @media(min-width:981px){
          #quickActionsSection{min-height:472px}
        }
        @media(max-width:980px){
          #profilesList{min-height:0}
          #selectedProfileCard{min-height:0}
          #quickActionsSection{min-height:0}
        }`;
      document.head.appendChild(style);
    }

    profiles.classList.add('show');
    if (selected && !selected.querySelector('strong')) {
      selected.textContent = '';
      const title = document.createElement('strong');
      const endpoint = document.createElement('span');
      const note = document.createElement('span');
      title.textContent = 'Загружаем Extra-профили…';
      endpoint.className = 'selected-endpoint';
      endpoint.textContent = '—';
      note.className = 'selected-note';
      note.textContent = 'Текущий VPN и список профилей появятся здесь без изменения размеров Dashboard.';
      selected.appendChild(title);
      selected.appendChild(endpoint);
      selected.appendChild(note);
    }
  }

  function mountModalLayer() {
    if (qs('#fnModalRoot')) return;
    const style = document.createElement('style');
    style.id = 'fnModalStyles';
    style.textContent = `
      .fn-modal-root{position:fixed;inset:0;z-index:1200;display:grid;place-items:center;padding:22px}
      .fn-modal-root[hidden]{display:none!important}
      .fn-modal-backdrop{position:absolute;inset:0;background:rgba(2,7,14,.76);backdrop-filter:blur(14px)}
      .fn-modal{position:relative;width:min(620px,calc(100vw - 32px));max-height:min(82vh,760px);overflow:auto;border:1px solid #355074;border-radius:20px;background:linear-gradient(180deg,#132239,#0c1727);box-shadow:0 34px 110px rgba(0,0,0,.58);padding:22px}
      .fn-modal-head{display:flex;align-items:flex-start;justify-content:space-between;gap:16px}
      .fn-modal-kicker{color:var(--accent2);font-size:11.5px;font-weight:800;letter-spacing:.12em;text-transform:uppercase}
      .fn-modal h2{margin:5px 0 0;font-size:24px;letter-spacing:-.03em}
      .fn-modal-close{appearance:none;border:1px solid var(--line);background:#0b1523;color:#cbd7e8;border-radius:10px;width:36px;height:36px;font-size:20px;cursor:pointer}
      .fn-modal-body{margin-top:16px;color:#c4d1e4;font-size:13.5px;line-height:1.62;white-space:pre-line}
      .fn-modal-meta{margin-top:14px;padding:13px 14px;border:1px solid #243955;border-radius:13px;background:#09131f;color:#aebed4;font-size:12.5px;line-height:1.6;white-space:pre-line}
      .fn-modal-actions{display:grid;grid-template-columns:1fr 1fr;gap:10px;margin-top:18px}
      .fn-modal-actions.one{grid-template-columns:1fr}
      .fn-modal-status{display:none;margin-top:14px;padding:12px 14px;border-radius:12px;border:1px solid #2b405e;background:#0a1624;font-size:13px;line-height:1.55;white-space:pre-line}
      .fn-modal-status.show{display:block}.fn-modal-status.ok{border-color:rgba(73,218,146,.38);color:#c9f7dc}.fn-modal-status.bad{border-color:rgba(255,112,112,.4);color:#ffd0d0}
      @media(max-width:600px){.fn-modal{padding:17px}.fn-modal h2{font-size:21px}.fn-modal-actions{grid-template-columns:1fr}}
    `;
    document.head.appendChild(style);

    const root = document.createElement('div');
    root.id = 'fnModalRoot';
    root.className = 'fn-modal-root';
    root.hidden = true;
    root.innerHTML = `
      <div class="fn-modal-backdrop"></div>
      <section class="fn-modal" role="dialog" aria-modal="true" aria-labelledby="fnModalTitle">
        <div class="fn-modal-head">
          <div><div id="fnModalKicker" class="fn-modal-kicker">FreeNet</div><h2 id="fnModalTitle">Действие</h2></div>
          <button id="fnModalClose" class="fn-modal-close" type="button" aria-label="Закрыть">×</button>
        </div>
        <div id="fnModalBody" class="fn-modal-body"></div>
        <div id="fnModalMeta" class="fn-modal-meta" hidden></div>
        <div id="fnModalStatus" class="fn-modal-status"></div>
        <div id="fnModalActions" class="fn-modal-actions">
          <button id="fnModalCancel" class="btn secondary" type="button">Отмена</button>
          <button id="fnModalConfirm" class="btn primary" type="button">Применить</button>
        </div>
      </section>`;
    document.body.appendChild(root);
    qs('#fnModalClose').addEventListener('click', closeModal);
    qs('#fnModalCancel').addEventListener('click', closeModal);
    qs('.fn-modal-backdrop').addEventListener('click', closeModal);
    qs('#fnModalConfirm').addEventListener('click', async () => {
      if (!activeModalConfirm) return;
      const fn = activeModalConfirm;
      activeModalConfirm = null;
      qs('#fnModalConfirm').disabled = true;
      qs('#fnModalCancel').disabled = true;
      qs('#fnModalClose').disabled = true;
      await fn();
    });
  }

  function openModal({kicker = 'FreeNet', title, body = '', meta = '', confirmText = 'Применить', cancelText = 'Отмена', onConfirm = null, closable = true}) {
    mountModalLayer();
    const root = qs('#fnModalRoot');
    qs('#fnModalKicker').textContent = kicker;
    qs('#fnModalTitle').textContent = title || 'FreeNet';
    qs('#fnModalBody').textContent = body;
    const metaNode = qs('#fnModalMeta');
    metaNode.textContent = meta;
    metaNode.hidden = !meta;
    const status = qs('#fnModalStatus');
    status.textContent = '';
    status.className = 'fn-modal-status';
    qs('#fnModalConfirm').textContent = confirmText;
    qs('#fnModalCancel').textContent = cancelText;
    qs('#fnModalConfirm').hidden = !onConfirm;
    qs('#fnModalCancel').hidden = !onConfirm;
    qs('#fnModalClose').hidden = !closable;
    qs('#fnModalConfirm').disabled = false;
    qs('#fnModalCancel').disabled = false;
    qs('#fnModalClose').disabled = false;
    qs('#fnModalActions').className = 'fn-modal-actions' + (onConfirm ? '' : ' one');
    activeModalConfirm = onConfirm;
    root.hidden = false;
  }

  function modalStatus(text, type = '') {
    const n = qs('#fnModalStatus');
    if (!n) return;
    n.textContent = text || '';
    n.className = text ? 'fn-modal-status show' + (type ? ' ' + type : '') : 'fn-modal-status';
  }

  function modalProgress(title, text) {
    qs('#fnModalTitle').textContent = title;
    qs('#fnModalBody').textContent = text;
    qs('#fnModalConfirm').hidden = true;
    qs('#fnModalCancel').hidden = true;
    qs('#fnModalClose').hidden = true;
    qs('#fnModalActions').className = 'fn-modal-actions one';
  }

  function modalResult(title, text, type = 'ok') {
    qs('#fnModalTitle').textContent = title;
    qs('#fnModalBody').textContent = text;
    modalStatus('', '');
    qs('#fnModalConfirm').hidden = true;
    qs('#fnModalCancel').hidden = true;
    qs('#fnModalClose').hidden = false;
    qs('#fnModalClose').disabled = false;
    qs('#fnModalActions').className = 'fn-modal-actions one';
    if (type) modalStatus(type === 'ok' ? 'Операция подтверждена фактическим состоянием FreeNet.' : 'Проверьте фактическое состояние перед повторной операцией.', type);
  }

  function closeModal() {
    const root = qs('#fnModalRoot');
    if (!root || root.hidden) return;
    activeModalConfirm = null;
    root.hidden = true;
  }

  function profileCountryCode(p) {
    const direct = String((p && p.country_code) || '').trim().toLowerCase();
    if (/^[a-z]{2}$/.test(direct)) return direct;
    const name = String((p && p.name) || '').trim();
    const m = name.match(/^([A-Za-z]{2})\b/);
    return m ? m[1].toLowerCase() : '';
  }

  function mountOverviewVPNFlow() {
    const vpnNav = qs('.nav-btn[data-page="vpn"]');
    if (vpnNav) vpnNav.remove();
    const vpnPage = qs('[data-page-view="vpn"]');
    if (vpnPage) vpnPage.remove();
    if (typeof pageLabels === 'object') delete pageLabels.vpn;
    if (location.hash === '#vpn') setPage('overview');

    renderProfileOptions = function() {
      const menu = el('profilesMenu'), triggerText = el('profilesTriggerText'), query = el('profileSearch').value.trim().toLowerCase();
      while (menu.firstChild) menu.removeChild(menu.firstChild);
      const filtered = extraProfiles.filter(p => {
        const hay = ((p.name || '') + ' ' + formatProfileEndpoint(p)).toLowerCase();
        return !query || hay.includes(query);
      });
      triggerText.textContent = selectedProviderName || (extraProfiles.length ? 'Выбрать конкретный Extra-профиль' : 'Профили не загружены');
      if (!filtered.length) {
        const empty = document.createElement('div');
        empty.className = 'hint';
        empty.textContent = extraProfiles.length ? 'По вашему поиску профилей нет.' : 'Сначала обновите Extra-профили.';
        menu.appendChild(empty);
        return;
      }
      filtered.forEach(p => {
        const option = document.createElement('button');
        option.type = 'button';
        option.className = 'profile-option';
        option.setAttribute('role', 'option');
        option.setAttribute('aria-selected', p.id === selectedProviderID ? 'true' : 'false');
        option.dataset.profileId = p.id || '';
        const marker = makeCountryMarker(profileCountryCode(p));
        const text = document.createElement('span');
        const main = document.createElement('span');
        const endpoint = document.createElement('span');
        main.className = 'profile-option-main';
        endpoint.className = 'profile-option-endpoint';
        main.textContent = p.name || 'Extra-профиль';
        endpoint.textContent = formatProfileEndpoint(p);
        text.appendChild(main);
        text.appendChild(endpoint);
        option.appendChild(marker);
        option.appendChild(text);
        option.addEventListener('click', () => selectProviderProfile(p));
        menu.appendChild(option);
      });
    };

    selectProviderProfile = async function(p) {
      selectedProviderID = p.id || '';
      selectedProviderName = p.name || 'Extra-профиль';
      providerPlanReady = false;
      providerApplied = false;
      resetSetupFinalizePlan();
      hideBox('providerNotice');
      closeProfileMenu();
      renderProfileOptions();
      renderSelectedProfile(p);
      await loadNetworkPlan(selectedProviderID);
      const pp = lastNetworkPlan && lastNetworkPlan.provider_plan;
      if (!providerPlanReady || !pp) {
        openModal({
          kicker: 'VPN',
          title: 'Профиль не готов к применению',
          body: selectedProviderName,
          meta: pp && pp.error ? pp.error : 'FreeNet не подтвердил read-only проверку кандидата.',
          closable: true
        });
        return;
      }
      openModal({
        kicker: 'VPN · точный Extra-профиль',
        title: selectedProviderName,
        body: 'Кандидат проверен без изменений runtime. После подтверждения FreeNet применит профиль транзакционно, перезапустит VPN при необходимости и перечитает фактическое состояние без перехода на отдельную страницу.',
        meta: `Endpoint: ${pp.endpoint || formatProfileEndpoint(p)}\nИзменится: ${pp.expected_delta || 'точный VPN-профиль'}\nНе изменится: ISP, DNS и routing policy`,
        confirmText: 'Применить профиль',
        cancelText: 'Отмена',
        onConfirm: () => applyExactProfileFromOverview(p, pp)
      });
    };
  }

  async function waitForVPNState(expectedEndpoint, expectedCode) {
    let s = null;
    for (let i = 0; i < 28; i++) {
      s = await loadStatus();
      if (s) {
        const endpointOK = !expectedEndpoint || s.endpoint === expectedEndpoint;
        const countryOK = !expectedCode || s.country_code === expectedCode;
        if (endpointOK && countryOK && s.xray_online) return s;
      }
      await new Promise(resolve => setTimeout(resolve, 850));
    }
    return s;
  }

  async function applyExactProfileFromOverview(p, pp) {
    if (!selectedProviderID || providerApplying) return;
    const expectedEndpoint = pp.endpoint || formatProfileEndpoint(p);
    const expectedCode = profileCountryCode(p);
    providerApplying = true;
    buttonsBusy(true);
    modalProgress('Применяем VPN-профиль…', `${p.name || 'Extra-профиль'}\n\nFreeNet выполняет controlled apply и ждёт фактического live-state. Страницу не закрывайте.`);
    try {
      const r = await fetch('/api/network-profile/apply', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({operation: 'provider', profile_id: selectedProviderID, confirm: true})
      });
      if (r.status === 401) {
        closeModal();
        await loadAuthStatus();
        return;
      }
      const j = await r.json();
      if (!r.ok || !j.success) {
        const parts = [j.error || 'VPN-профиль не применён'];
        if (j.primary_error) parts.push('Основная ошибка: ' + j.primary_error);
        if (j.rollback_state) parts.push('Откат: ' + j.rollback_state);
        modalResult('VPN-профиль не применён', parts.join('\n'), 'bad');
        return;
      }

      const s = await waitForVPNState(expectedEndpoint, expectedCode);
      const accepted = !!(s && s.endpoint === expectedEndpoint && (!expectedCode || s.country_code === expectedCode) && s.xray_online);
      if (!accepted) {
        modalResult('Применение завершено, live-state не подтверждён', `Ожидали: ${expectedEndpoint}${expectedCode ? ` · ${expectedCode.toUpperCase()}` : ''}\nФактически: ${(s && s.endpoint) || 'нет данных'} · ${(s && s.country_code) || 'не определено'}\n\nНе повторяйте операцию вслепую.`, 'bad');
        return;
      }

      providerApplied = true;
      selectedProviderID = '';
      selectedProviderName = '';
      providerPlanReady = false;
      renderProfileOptions();
      renderSelectedProfile(null);
      await loadNetworkPlan('');
      modalResult('VPN переключён', `${s.country || p.name || 'Профиль'}${s.city ? ' · ' + s.city : ''}\n${s.endpoint}\n\nОбзор уже обновлён по фактическому состоянию FreeNet.`, 'ok');
    } catch (_) {
      modalResult('Связь прервалась во время переключения', 'FreeNet мог кратко перезапустить VPN. Не повторяйте действие вслепую; дождитесь фактического статуса.', 'bad');
    } finally {
      providerApplying = false;
      buttonsBusy(!!(lastStatus && (lastStatus.busy || lastStatus.updater_busy)));
    }
  }

  function stateText(state) {
    const labels = {
      IDLE: 'Готово к проверке',
      CHECKING: 'Проверяем релиз и SHA-256',
      SNAPSHOT: 'Создаём резервную копию',
      UPDATING: 'Устанавливаем обновление',
      RECONNECTING: 'FreeNet перезапускается и проверяет результат',
      SUCCESS: 'Обновление успешно установлено',
      FAILED: 'Обновление отменено, предыдущее состояние восстановлено',
      ROLLBACK_FAILED: 'Откат не подтверждён — дальнейшие изменения остановлены',
      BUSY: 'Обновление уже выполняется'
    };
    return labels[state] || state || 'Неизвестное состояние';
  }

  function planItems(text) {
    return String(text || '')
      .split(';')
      .map(v => v.trim())
      .filter(v => v && !/^NONE$/i.test(v));
  }

  function bulletSection(title, items) {
    if (!items || !items.length) return '';
    return `${title}\n${items.map(v => `• ${v}`).join('\n')}`;
  }

  function updatePlanText(p) {
    const changed = planItems(p.expected_delta);
    const unchanged = planItems(p.expected_no_delta);
    return [
      p.update_available ? `Версия: ${p.current_version} → ${p.latest_version}` : 'Установлена актуальная версия.',
      bulletSection('Что изменится:', changed),
      bulletSection('Что останется без изменений:', unchanged),
      p.manifest_verified ? 'Проверка целостности: SHA-256 подтверждён.' : 'Проверка целостности: не подтверждена.'
    ].filter(Boolean).join('\n\n');
  }

  function uniqueLines(lines) {
    const seen = new Set();
    return lines.filter(Boolean).filter(line => {
      const key = String(line).trim();
      if (!key || seen.has(key)) return false;
      seen.add(key);
      return true;
    });
  }

  function mountUpdate() {
    const grid = qs('[data-page-view="system"] .grid-equal');
    if (!grid || grid.children.length < 2) return;
    const card = grid.children[1];
    card.innerHTML = `
      <div class="card-head">
        <h2>Обновление FreeNet</h2>
        <div id="webUpdateSummary" class="summary-state">Готово к проверке</div>
      </div>
      <div class="status-strip" style="margin-bottom:10px">
        <div class="status-pill"><b>Установлено</b><span id="webUpdateCurrent">—</span></div>
        <div class="status-pill"><b>Доступно</b><span id="webUpdateLatest">—</span></div>
        <div class="status-pill"><b>SHA-256</b><span id="webUpdateManifest">—</span></div>
      </div>
      <p class="hint" style="margin-top:0">FreeNet проверит точный GitHub Release, создаст резервную копию, проверит SHA-256 и подготовленные файлы, обновит только компоненты FreeNet, перезапустит Control Center и подтвердит фактическую версию. XKeen/Xray, подписка, ISP, DNS и routing этим действием не изменяются.</p>
      <div class="action-row">
        <button id="webUpdateCheckBtn" class="btn secondary" type="button">Проверить обновление</button>
        <button id="webUpdateApplyBtn" class="btn primary" type="button" disabled>Обновить</button>
      </div>
      <details class="details" id="webUpdateDetails"><summary>Что изменится</summary><div id="webUpdatePlan" class="notice"></div></details>
      <div id="webUpdateNotice" class="notice"></div>`;

    qs('#webUpdateCheckBtn').addEventListener('click', checkUpdate);
    qs('#webUpdateApplyBtn').addEventListener('click', openUpdateConfirmModal);
    loadState();
  }

  function setUpdateSummary(text, type = '') {
    const n = qs('#webUpdateSummary');
    if (!n) return;
    n.textContent = text;
    n.className = 'summary-state' + (type ? ' ' + type : '');
  }

  function updateNotice(text, type = '') {
    const n = qs('#webUpdateNotice');
    if (!n) return;
    n.textContent = text || '';
    n.className = text ? 'notice show' + (type ? ' ' + type : '') : 'notice';
  }

  function renderPlan(p) {
    plan = p;
    qs('#webUpdateCurrent').textContent = p.current_version || '—';
    qs('#webUpdateLatest').textContent = p.latest_version || '—';
    qs('#webUpdateManifest').textContent = p.manifest_verified ? 'проверен' : 'нет';
    const box = qs('#webUpdatePlan');
    box.textContent = updatePlanText(p);
    box.className = 'notice show';
    const apply = qs('#webUpdateApplyBtn');
    apply.disabled = !(p.success && p.ready && p.update_available && p.target_tag);
    apply.textContent = p.update_available && p.target_tag ? `Обновить до ${p.target_tag}` : 'Обновить';
    setUpdateSummary(p.update_available ? `Доступно ${p.latest_version}` : 'Актуальная версия', p.update_available ? '' : 'ok');
  }

  function openUpdateConfirmModal() {
    if (!plan || !plan.update_available || !plan.target_tag) return;
    openModal({
      kicker: 'Обновление FreeNet',
      title: `${plan.current_version || 'текущая версия'} → ${plan.latest_version || plan.target_tag}`,
      body: 'Перед установкой FreeNet создаст резервную копию и проверит целостность релиза. После установки Control Center кратко перезапустится и сам проверит результат.',
      meta: updatePlanText(plan),
      confirmText: `Установить ${plan.target_tag}`,
      onConfirm: startUpdate
    });
  }

  async function checkUpdate() {
    const btn = qs('#webUpdateCheckBtn');
    btn.disabled = true;
    qs('#webUpdateApplyBtn').disabled = true;
    updateNotice('Проверяем последний опубликованный релиз FreeNet…');
    setUpdateSummary('Проверяем…');
    try {
      const r = await fetch('/api/system/update/plan', {cache: 'no-store'});
      const p = await r.json();
      if (!r.ok || !p.success) throw new Error(p.error || 'Не удалось проверить обновление');
      renderPlan(p);
      updateNotice(p.update_available ? `Обновление ${p.target_tag} готово к установке после вашего подтверждения.` : 'Установлена актуальная версия FreeNet.', 'ok');
      if (p.update_available) openUpdateConfirmModal();
      else openModal({kicker: 'Обновление FreeNet', title: 'Установлена актуальная версия', body: `${p.current_version || 'FreeNet'} уже является последним опубликованным релизом.`, meta: p.manifest_verified ? 'SHA-256: проверен.' : '', closable: true});
    } catch (e) {
      setUpdateSummary('Проверка не удалась', 'bad');
      updateNotice(e.message || 'Ошибка проверки обновления', 'bad');
      openModal({kicker: 'Обновление FreeNet', title: 'Не удалось проверить обновление', body: e.message || 'Ошибка проверки обновления', closable: true});
    } finally {
      btn.disabled = false;
    }
  }

  async function startUpdate() {
    if (!plan || !plan.update_available || !plan.target_tag) return;
    qs('#webUpdateCheckBtn').disabled = true;
    qs('#webUpdateApplyBtn').disabled = true;
    setUpdateSummary('Запускаем обновление…');
    updateNotice('Запускаем безопасное обновление. FreeNet кратко перезапустится.');
    modalProgress(`Устанавливаем ${plan.target_tag}…`, 'Создаём резервную копию, проверяем подготовленные файлы и устанавливаем обновление. Краткая потеря связи или 502 во время перезапуска ожидаема и сама по себе не считается ошибкой.');
    try {
      const r = await fetch('/api/system/update/apply', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({target_tag: plan.target_tag})
      });
      const j = await r.json();
      if (!r.ok || !j.success) throw new Error(j.error || 'Не удалось запустить обновление');
      polling = true;
      pollState(plan.target_tag);
    } catch (e) {
      setUpdateSummary('Не запущено', 'bad');
      updateNotice(e.message || 'Ошибка запуска обновления', 'bad');
      modalResult('Обновление не запущено', e.message || 'Ошибка запуска обновления', 'bad');
      qs('#webUpdateCheckBtn').disabled = false;
      qs('#webUpdateApplyBtn').disabled = false;
    }
  }

  async function loadState() {
    try {
      const r = await fetch('/api/system/update/state', {cache: 'no-store'});
      if (!r.ok) return;
      const s = await r.json();
      if (s.state && s.state !== 'IDLE') renderState(s);
    } catch (_) {}
  }

  function renderState(s) {
    const terminalGood = s.state === 'SUCCESS';
    const terminalBad = s.state === 'FAILED' || s.state === 'ROLLBACK_FAILED';
    setUpdateSummary(stateText(s.state), terminalGood ? 'ok' : terminalBad ? 'bad' : '');
    const lines = uniqueLines([
      stateText(s.state),
      s.message || '',
      s.primary_error ? `Основная ошибка: ${s.primary_error}` : '',
      s.rollback_state ? `Откат: ${s.rollback_state}` : ''
    ]);
    updateNotice(lines.join('\n'), terminalGood ? 'ok' : terminalBad ? 'bad' : '');
    if (!qs('#fnModalRoot')?.hidden) modalStatus(lines.join('\n'), terminalGood ? 'ok' : terminalBad ? 'bad' : '');
  }

  async function pollState(target) {
    if (!polling) return;
    try {
      const r = await fetch('/api/system/update/state', {cache: 'no-store'});
      if (r.status === 401) {
        setUpdateSummary('FreeNet перезапущен — требуется вход');
        updateNotice('Control Center вернулся после перезапуска. Авторизуйтесь; проверка результата продолжится автоматически.');
        closeModal();
        if (typeof loadAuthStatus === 'function') {
          try { await loadAuthStatus(); } catch (_) {}
        }
        setTimeout(() => pollState(target), 1400);
        return;
      }
      if (r.ok) {
        const state = await r.json();
        renderState(state);
        if (state.state === 'SUCCESS') {
          polling = false;
          await waitForVersion(target);
          return;
        }
        if (state.state === 'FAILED' || state.state === 'ROLLBACK_FAILED') {
          polling = false;
          const parts = uniqueLines([
            state.message || stateText(state.state),
            state.primary_error ? `Основная ошибка: ${state.primary_error}` : '',
            state.rollback_state ? `Откат: ${state.rollback_state}` : ''
          ]);
          openModal({
            kicker: 'Обновление FreeNet',
            title: 'Обновление не применено',
            body: parts.join('\n'),
            meta: state.rollback_state === 'SUCCESS' ? 'Предыдущее состояние восстановлено. Повторно запускать обновление до исправления причины не нужно.' : 'Состояние требует проверки перед следующей операцией.',
            closable: true
          });
          qs('#webUpdateCheckBtn').disabled = false;
          return;
        }
      }
    } catch (_) {
      setUpdateSummary('FreeNet перезапускается…');
      modalStatus('Перезапускаем FreeNet и ждём возвращения Control Center…');
    }
    setTimeout(() => pollState(target), 1400);
  }

  async function waitForVersion(target) {
    modalProgress('Подтверждаем новую версию…', `Control Center уже сообщил об успешном обновлении. Проверяем, что браузер видит именно ${target}.`);
    for (let i = 0; i < 90; i++) {
      try {
        const r = await fetch('/versionz', {cache: 'no-store'});
        const v = (await r.text()).trim();
        if (r.ok && v === target) {
          modalResult('Обновление установлено', `FreeNet ${target} запущен и подтверждён. Интерфейс обновится автоматически.`, 'ok');
          updateNotice(`FreeNet ${target} установлен и принят. Обновляем интерфейс…`, 'ok');
          setTimeout(() => location.reload(), 700);
          return;
        }
      } catch (_) {}
      await new Promise(resolve => setTimeout(resolve, 1000));
    }
    modalResult('Не удалось подтвердить новую версию', `FreeNet сообщил об успешном обновлении, но браузер не подтвердил ${target} за ограниченное время. Не запускайте обновление повторно до проверки фактического состояния.`, 'bad');
    updateNotice(`Браузер не подтвердил ${target}. Проверьте фактическое состояние перед повторной попыткой.`, 'bad');
  }

  function mountNetworkDraftFlow() {
    const page = qs('[data-page-view="network"]');
    const isp = qs('#ispSelect');
    const dns = qs('#dnsModeSelect');
    const oldSave = qs('#saveNetworkBtn');
    const oldPlan = qs('#planNetworkBtn');
    const oldApply = qs('#applyNetworkBtn');
    if (!page || !isp || !dns || !oldPlan || !oldApply) return;

    const intro = qs('.page-head p', page);
    if (intro) intro.textContent = 'Выберите интернет-провайдера и DNS. Сначала FreeNet проверит изменения без записи настроек, затем применит их одной транзакцией.';
    if (oldSave) oldSave.hidden = true;
    const firmwareOption = dns.querySelector('option[value="firmware"]');
    if (firmwareOption) firmwareOption.textContent = 'DNS напрямую через роутер';
    dnsLabels.firmware = 'DNS напрямую через роутер';

    const planButton = oldPlan.cloneNode(true);
    planButton.textContent = 'Проверить изменения';
    oldPlan.replaceWith(planButton);
    const applyButton = oldApply.cloneNode(true);
    applyButton.textContent = 'Применить';
    oldApply.replaceWith(applyButton);

    const draftParams = (profileID = '') => {
      const q = new URLSearchParams();
      q.set('isp', isp.value);
      q.set('dns_mode', dns.value);
      if (profileID) q.set('provider_profile_id', profileID);
      return q.toString();
    };

    renderNetworkControls = function(serverBusy = false) {
      const state = networkState();
      const save = qs('#saveNetworkBtn');
      const planBtn = qs('#planNetworkBtn');
      const applyBtn = qs('#applyNetworkBtn');
      if (save) save.hidden = true;
      if (planBtn) {
        planBtn.hidden = false;
        planBtn.disabled = serverBusy || networkChecking || networkApplying;
      }
      if (applyBtn) {
        applyBtn.hidden = state !== 'changes';
        applyBtn.disabled = serverBusy || state !== 'changes' || networkApplying;
      }
      if (state === 'active') setSummary('networkSummary', 'Выбранный профиль уже активен', 'ok');
      else if (state === 'changes') setSummary('networkSummary', 'Изменения проверены — можно применить');
      else if (state === 'blocked' || state === 'error') setSummary('networkSummary', 'Применение заблокировано', 'bad');
      else if (state === 'dirty') setSummary('networkSummary', 'Изменения ещё не проверены');
      else if (state === 'checking') setSummary('networkSummary', 'Проверяем изменения…');
      else setSummary('networkSummary', 'Проверьте выбранные настройки');
    };

    loadNetworkPlan = async function(profileID = selectedProviderID) {
      if (!authAuthenticated) return;
      networkChecking = true;
      networkPlanReady = false;
      networkPlanError = false;
      lastNetworkPlan = null;
      providerPlanReady = false;
      showBox('networkPlan', 'Проверяем выбранные настройки без сохранения и без изменений runtime…');
      buttonsBusy(!!(lastStatus && (lastStatus.busy || lastStatus.updater_busy)));
      try {
        const r = await fetch('/api/network-profile/plan?' + draftParams(profileID), {cache: 'no-store'});
        if (r.status === 401) {
          await loadAuthStatus();
          return;
        }
        const j = await r.json();
        if (!r.ok || !j.success) {
          networkPlanError = true;
          showBox('networkPlan', j.error || 'Не удалось проверить изменения', 'bad');
          renderExtraProfiles(j);
          if (profileID) showBox('providerPlan', 'Не удалось проверить выбранный VPN-профиль', 'bad');
          return;
        }
        networkDirty = false;
        lastNetworkPlan = j;
        networkPlanReady = !!(j.supported && !j.active);
        showBox('networkPlan', formatNetworkPlan(j), j.supported ? 'ok' : 'bad');
        renderExtraProfiles(j);
        if (profileID) {
          const pp = j.provider_plan;
          providerPlanReady = !!(pp && pp.success && pp.candidate_xray_valid && pp.mutation === 'NONE' && !pp.error);
          showBox('providerPlan', formatProviderPlan(pp), providerPlanReady ? 'ok' : 'bad');
          setSummary('providerSummary', providerPlanReady ? 'VPN-кандидат проверен' : 'VPN-кандидат заблокирован', providerPlanReady ? 'ok' : 'bad');
        } else {
          hideBox('providerPlan');
        }
      } catch (_) {
        networkPlanError = true;
        showBox('networkPlan', 'Не удалось проверить изменения: нет связи с FreeNet', 'bad');
        renderExtraProfiles(null);
        if (profileID) showBox('providerPlan', 'Нет связи при проверке VPN-профиля', 'bad');
      } finally {
        networkChecking = false;
        renderNetworkControls(!!(lastStatus && (lastStatus.busy || lastStatus.updater_busy)));
        buttonsBusy(!!(lastStatus && (lastStatus.busy || lastStatus.updater_busy)));
      }
    };

    async function applyDraft() {
      if (networkDirty || !networkPlanReady || networkApplying) return;
      const delta = (lastNetworkPlan && lastNetworkPlan.expected_delta) || 'выбранный сетевой профиль';
      openModal({
        kicker: 'Сеть · ISP / DNS',
        title: 'Применить проверенные настройки?',
        body: 'FreeNet сначала сделает резервную копию. Активный ISP/DNS будет сохранён только после успешной проверки результата.',
        meta: `Изменится: ${delta}`,
        confirmText: 'Применить',
        onConfirm: async () => {
          networkApplying = true;
          buttonsBusy(true);
          modalProgress('Применяем сетевые настройки…', 'Выполняем transactional apply и post-apply acceptance.');
          showBox('networkNotice', 'Применяем и проверяем сетевые настройки…');
          try {
            const r = await fetch('/api/network-profile/apply', {
              method: 'POST',
              headers: {'Content-Type': 'application/json'},
              body: JSON.stringify({operation: 'network', isp: isp.value, dns_mode: dns.value, confirm: true})
            });
            if (r.status === 401) {
              closeModal();
              await loadAuthStatus();
              return;
            }
            const j = await r.json();
            if (!r.ok || !j.success) {
              const parts = [j.error || 'Сетевые настройки не применены'];
              if (j.primary_error) parts.push('Основная ошибка: ' + j.primary_error);
              if (j.rollback_state) parts.push('Откат: ' + j.rollback_state);
              if (j.rollback_state === 'FAILED/UNKNOWN') parts.push('Дальнейшие изменения остановлены до проверки фактического состояния.');
              showBox('networkNotice', parts.join('\n'), 'bad');
              modalResult('Сетевые настройки не применены', parts.join('\n'), 'bad');
              return;
            }
            networkDirty = false;
            networkPlanReady = false;
            showBox('networkNotice', j.message || 'Сетевые настройки применены и проверены.', 'ok');
            const s = await loadStatus();
            if (s) {
              isp.value = s.isp || j.isp || isp.value;
              dns.value = s.dns_mode || j.dns_mode || dns.value;
            }
            await loadNetworkPlan(selectedProviderID);
            modalResult('Сетевые настройки применены', j.message || 'Профиль применён и проверен.', 'ok');
          } catch (_) {
            networkPlanError = true;
            showBox('networkNotice', 'Связь с FreeNet прервалась во время применения. Не повторяйте операцию вслепую; сначала проверьте фактический статус.', 'bad');
            modalResult('Связь прервалась', 'Не повторяйте операцию вслепую; сначала проверьте фактический статус.', 'bad');
          } finally {
            networkApplying = false;
            buttonsBusy(!!(lastStatus && (lastStatus.busy || lastStatus.updater_busy)));
          }
        }
      });
    }

    const markDraft = () => {
      networkDirty = true;
      networkPlanReady = false;
      networkPlanError = false;
      lastNetworkPlan = null;
      hideBox('networkNotice');
      hideBox('networkPlan');
      resetSetupFinalizePlan();
      renderNetworkControls(!!(lastStatus && (lastStatus.busy || lastStatus.updater_busy)));
    };

    planButton.addEventListener('click', () => loadNetworkPlan(selectedProviderID));
    applyButton.addEventListener('click', applyDraft);
    isp.addEventListener('change', markDraft);
    dns.addEventListener('change', markDraft);
    renderNetworkControls(false);
  }

  function mount() {
    mountTypographyReadability();
    mountDashboardStability();
    mountModalLayer();
    mountOverviewVPNFlow();
    mountUpdate();
    mountNetworkDraftFlow();
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mount);
  else mount();
})();
