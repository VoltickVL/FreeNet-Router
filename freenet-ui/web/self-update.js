(() => {
  const qs = (s, root = document) => root.querySelector(s);
  let plan = null;
  let polling = false;
  let activeModalConfirm = null;
  let updateProgressTimer = null;
  let updateProgressStarted = 0;
  let versionCatalog = null;
  let versionTargetPlan = null;

  function stopUpdateProgress() {
    clearInterval(updateProgressTimer);
    updateProgressTimer = null;
    updateProgressStarted = 0;
    qs('#fnUpdateProgress')?.remove();
  }

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
      .fn-modal-meta{margin-top:14px;padding:13px 14px;border:1px solid #243955;border-radius:13px;background:#09131f;color:#aebed4;font-size:12.5px;line-height:1.55;white-space:pre-line}
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
    if (!updateProgressStarted) updateProgressStarted = Date.now();
    if (!qs('#fnUpdateProgress')) {
      const box = document.createElement('div'); box.id = 'fnUpdateProgress';
      const bar = document.createElement('progress');
      bar.setAttribute('aria-label', 'Обновление выполняется');
      bar.style.cssText = 'width:100%;accent-color:#5189ff';
      const timer = document.createElement('p');
      const tick = () => { timer.textContent = `Прошло ${Math.floor((Date.now()-updateProgressStarted)/1000)} с · не выключайте роутер`; };
      tick(); box.append(bar, timer); qs('#fnModalBody').after(box);
      updateProgressTimer = setInterval(tick, 1000);
    }
    qs('#fnModalTitle').textContent = title;
    qs('#fnModalBody').textContent = text;
    qs('#fnModalConfirm').hidden = true;
    qs('#fnModalCancel').hidden = true;
    qs('#fnModalClose').hidden = true;
    qs('#fnModalActions').className = 'fn-modal-actions one';
  }

  function modalResult(title, text, type = 'ok') {
    stopUpdateProgress();
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
        if (endpointOK && countryOK && s.xray_online && s.dns_out_present) return s;
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
      const accepted = !!(s && s.endpoint === expectedEndpoint && (!expectedCode || s.country_code === expectedCode) && s.xray_online && s.dns_out_present);
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
      <p class="hint" style="margin-top:0">FreeNet проверит точный GitHub Release, создаст резервную копию, проверит SHA-256 и staging, обновит только файлы FreeNet, перезапустит Control Center и подтвердит фактическую версию. XKeen/Xray, подписка и сетевые настройки этим действием не изменяются.</p>
      <div class="action-row">
        <button id="webUpdateCheckBtn" class="btn secondary" type="button">Проверить обновление</button>
        <button id="webUpdateRecoverBtn" class="btn secondary" type="button" hidden>Восстановить обновление</button>
        <button id="webUpdateApplyBtn" class="btn primary" type="button" disabled>Обновить</button>
      </div>
      <details class="details" id="webUpdateDetails"><summary>Что изменится</summary><div id="webUpdatePlan" class="notice"></div></details>
      <div id="webUpdateNotice" class="notice"></div>`;

    qs('#webUpdateCheckBtn').addEventListener('click', checkUpdate);
    qs('#webUpdateRecoverBtn').addEventListener('click', openUpdaterRecoveryModal);
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
    const recover = qs('#webUpdateRecoverBtn');
    if (recover) recover.hidden = true;
    qs('#webUpdateCurrent').textContent = p.current_version || '—';
    qs('#webUpdateLatest').textContent = p.latest_version || '—';
    qs('#webUpdateManifest').textContent = p.manifest_verified ? 'проверен' : 'нет';
    const text = [
      p.update_available ? `Доступно обновление ${p.current_version} → ${p.latest_version}` : 'Установлена актуальная версия.',
      p.expected_delta ? `Изменится: ${p.expected_delta}` : '',
      p.expected_no_delta ? `Не изменится: ${p.expected_no_delta}` : ''
    ].filter(Boolean).join('\n');
    const box = qs('#webUpdatePlan');
    box.textContent = text;
    box.className = 'notice show';
    const apply = qs('#webUpdateApplyBtn');
    apply.disabled = !(p.success && p.ready && p.update_available && p.target_tag);
    apply.textContent = p.update_available && p.target_tag ? `Обновить до ${p.target_tag}` : 'Обновить';
    setUpdateSummary(p.update_available ? `Доступно ${p.latest_version}` : 'Актуальная версия', p.update_available ? '' : 'ok');
    renderTopbarVersion(p.current_version, !!p.update_available, p.latest_version);
  }

  function openUpdateConfirmModal() {
    if (!plan || !plan.update_available || !plan.target_tag) return;
    openModal({
      kicker: 'Обновление FreeNet',
      title: `${plan.current_version || 'текущая версия'} → ${plan.latest_version || plan.target_tag}`,
      body: 'FreeNet обновит только собственные файлы. Перед изменением будет создан backup, релиз и SHA-256 будут проверены, затем Control Center кратко перезапустится и автоматически подтвердит целевую версию.',
      meta: `SHA-256: ${plan.manifest_verified ? 'проверен' : 'не подтверждён'}\nИзменится: ${plan.expected_delta || 'файлы FreeNet'}\nНе изменится: ${plan.expected_no_delta || 'XKeen/Xray, подписка, ISP/DNS/routing'}`,
      confirmText: `Установить ${plan.target_tag}`,
      onConfirm: startUpdate
    });
  }

  function openUpdaterRecoveryModal() {
    openModal({
      kicker: 'FreeNet · восстановление updater',
      title: 'Восстановить механизм обновления?',
      body: 'FreeNet независимо от установленного updater скачает только metadata релиза, SHA256SUMS и новый self_update.sh. Новый helper будет запущен только после SHA-256 проверки и собственного read-only plan.',
      meta: 'VPN, DNS, routing, Xray и подписка на этапе восстановления updater не изменяются. Live updater, неизвестный rollback или неподтверждённая SHA-проверка остановят операцию.',
      confirmText: 'Восстановить и обновить',
      cancelText: 'Отмена',
      onConfirm: startUpdateRecovery
    });
  }

  async function startUpdateRecovery() {
    const check = qs('#webUpdateCheckBtn');
    const recover = qs('#webUpdateRecoverBtn');
    const apply = qs('#webUpdateApplyBtn');
    if (check) check.disabled = true;
    if (recover) recover.disabled = true;
    if (apply) apply.disabled = true;
    setUpdateSummary('Восстанавливаем updater…');
    updateNotice('Проверяем release manifest и новый updater независимо от установленного helper.');
    modalProgress('Восстанавливаем механизм обновления…', 'FreeNet проверяет exact release и SHA-256. До подтверждения нового updater persistent mutation не выполняется.');
    try {
      const r = await fetch('/api/system/update/recover', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'}
      });
      const j = await r.json();
      if (!r.ok || !j.success) throw new Error(j.error || 'Не удалось восстановить updater');
      const target = String(j.operation_id || '').trim();
      setUpdateSummary('Updater восстановлен — обновляем…');
      updateNotice(j.message || 'Проверенный updater запущен.');
      polling = true;
      pollState(target);
    } catch (e) {
      setUpdateSummary('Восстановление остановлено', 'bad');
      updateNotice(e.message || 'Не удалось восстановить updater', 'bad');
      modalResult('Updater не восстановлен', e.message || 'Операция остановлена до изменений.', 'bad');
      if (check) check.disabled = false;
      if (recover) {
        recover.hidden = false;
        recover.disabled = false;
      }
    }
  }

  async function checkUpdate() {
    const btn = qs('#webUpdateCheckBtn');
    plan = null;
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
      else openModal({kicker: 'Обновление FreeNet', title: 'Установлена актуальная версия', body: `${p.current_version || 'FreeNet'} уже является последним опубликованным релизом.`, meta: p.manifest_verified ? 'SHA-256 manifest проверен.' : '', closable: true});
    } catch (e) {
      setUpdateSummary('Проверка не удалась', 'bad');
      updateNotice(e.message || 'Ошибка проверки обновления', 'bad');
      const recover = qs('#webUpdateRecoverBtn');
      if (recover) recover.hidden = false;
      openModal({
        kicker: 'Обновление FreeNet',
        title: 'Штатный updater не подтвердил обновление',
        body: e.message || 'Ошибка проверки обновления',
        meta: 'Можно запустить независимое восстановление updater прямо из браузера. FreeNet сначала проверит SHA-256 нового helper и только затем передаст ему обновление.',
        confirmText: 'Восстановить updater',
        cancelText: 'Закрыть',
        onConfirm: startUpdateRecovery
      });
    } finally {
      btn.disabled = false;
      qs('#webUpdateApplyBtn').disabled = !(plan && plan.success && plan.ready && plan.update_available && plan.target_tag);
    }
  }

  async function startUpdate() {
    if (!plan || !plan.update_available || !plan.target_tag) return;
    const checkBtn = qs('#webUpdateCheckBtn');
    const applyBtn = qs('#webUpdateApplyBtn');
    if (checkBtn) checkBtn.disabled = true;
    if (applyBtn) applyBtn.disabled = true;
    const isDowngrade = plan.direction === 'downgrade';
    setUpdateSummary(isDowngrade ? 'Запускаем откат версии…' : 'Запускаем обновление…');
    updateNotice(isDowngrade ? 'Запускаем безопасный downgrade. FreeNet кратко перезапустится.' : 'Запускаем безопасное обновление. FreeNet кратко перезапустится.');
    modalProgress(`Устанавливаем ${plan.target_tag}…`, 'Создаём backup, проверяем exact staging и применяем выбранную версию. Краткая потеря связи/502 во время перезапуска ожидаема и сама по себе не считается ошибкой.');
    try {
      const r = await fetch('/api/system/update/apply', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({target_tag: plan.target_tag})
      });
      const j = await r.json();
      if (!r.ok || !j.success) throw new Error(j.error || 'Не удалось запустить обновление');
      const activeTarget = String(j.operation_id || plan.target_tag || '').trim();
      if (j.message && /уже выполняется/i.test(j.message)) {
        setUpdateSummary('Обновление уже выполняется');
        updateNotice('Подключаемся к текущей операции обновления. Повторный запуск не выполняется.');
        modalStatus('Обновление уже выполняется. Показываем его фактический прогресс.', '');
      }
      polling = true;
      pollState(activeTarget);
    } catch (e) {
      setUpdateSummary('Не запущено', 'bad');
      updateNotice(e.message || 'Ошибка запуска обновления', 'bad');
      modalResult('Обновление не запущено', e.message || 'Ошибка запуска обновления', 'bad');
      if (checkBtn) checkBtn.disabled = false;
      if (applyBtn) applyBtn.disabled = false;
    }
  }

  async function loadState() {
    try {
      const r = await fetch('/api/system/update/state', {cache: 'no-store'});
      if (!r.ok) return;
      const s = await r.json();
      const activeState = ['CHECKING','SNAPSHOT','UPDATING','RECONNECTING','BUSY'].includes(String(s.state || ''));
      if (s.state === 'ROLLBACK_FAILED') {
        renderState(s);
        return;
      }
      if (s.update_lock_held || activeState) {
        renderState(s);
        if (!polling) {
          polling = true;
          pollState(String(s.target_version || '').trim());
        }
      }
    } catch (_) {}
  }

  function renderState(s) {
    const terminalGood = s.state === 'SUCCESS';
    const terminalBad = s.state === 'FAILED' || s.state === 'ROLLBACK_FAILED';
    setUpdateSummary(stateText(s.state), terminalGood ? 'ok' : terminalBad ? 'bad' : '');
    const lines = [stateText(s.state), s.message || '', s.primary_error ? `Основная ошибка: ${s.primary_error}` : '', s.rollback_state ? `Откат: ${s.rollback_state}` : ''].filter(Boolean);
    updateNotice(lines.join('\n'), terminalGood ? 'ok' : terminalBad ? 'bad' : '');
    if (!qs('#fnModalRoot')?.hidden) modalStatus(lines.join('\n'), terminalGood ? 'ok' : terminalBad ? 'bad' : '');
  }

  async function pollState(target) {
    if (!polling) return;
    if (updateProgressStarted && Date.now() - updateProgressStarted > 300000) {
      polling = false;
      modalResult('Результат обновления пока неизвестен', 'Ожидание ограничено пятью минутами. Не запускайте обновление повторно до проверки фактической версии и состояния FreeNet.', 'bad');
      return;
    }
    try {
      const r = await fetch('/api/system/update/state', {cache: 'no-store', signal: AbortSignal.timeout(10000)});
      if (r.status === 401) {
        setUpdateSummary('FreeNet перезапущен — восстанавливаем интерфейс…');
        modalStatus('Новая версия запущена. Сессия авторизации будет восстановлена через экран входа.', '');
        polling = false;
        await waitForVersion(target);
        return;
      }
      if (r.ok) {
        const state = await r.json();
        const stateTarget = String(state.target_version || '').trim();
        const requestedTarget = String(target || '').trim();
        const targetMismatch = !!(requestedTarget && stateTarget && requestedTarget !== stateTarget);
        const staleTerminalWhileRunning = !!state.update_lock_held && (state.state === 'FAILED' || state.state === 'SUCCESS');

        if (targetMismatch || staleTerminalWhileRunning) {
          setUpdateSummary('Обновление выполняется…');
          updateNotice(
            targetMismatch
              ? 'Ждём состояние текущей операции обновления. Предыдущий результат больше не используется.'
              : 'Операция ещё выполняется. Игнорируем устаревший terminal state до подтверждения текущего запуска.'
          );
          modalStatus('Ждём фактическое состояние текущего обновления…', '');
        } else {
          renderState(state);
          if (state.state === 'SUCCESS') {
            polling = false;
            await waitForVersion(stateTarget || requestedTarget);
            return;
          }
          if (state.state === 'ROLLBACK_FAILED') {
            polling = false;
            modalResult('Обновление остановлено', [stateText(state.state), state.message || '', state.primary_error || '', state.rollback_state ? 'Откат: ' + state.rollback_state : ''].filter(Boolean).join('\n'), 'bad');
            qs('#webUpdateCheckBtn').disabled = false;
            return;
          }
          if (state.state === 'FAILED') {
            polling = false;
            modalResult('Обновление не принято', [stateText(state.state), state.message || '', state.primary_error || '', state.rollback_state ? 'Откат: ' + state.rollback_state : ''].filter(Boolean).join('\n'), 'bad');
            qs('#webUpdateCheckBtn').disabled = false;
            return;
          }
        }
      }
    } catch (_) {
      setUpdateSummary('FreeNet перезапускается…');
      modalStatus('Перезапускаем FreeNet и ждём возвращения Control Center…');
    }
    setTimeout(() => pollState(target), 1400);
  }

  async function waitForVersion(target) {
    modalProgress(`Перезапускаем FreeNet…`, `Ждём, когда Control Center вернётся на версии ${target}. Краткий 502 во время этого этапа ожидаем.`);
    for (let i = 0; i < 90; i++) {
      try {
        const r = await fetch('/versionz', {cache: 'no-store', signal: AbortSignal.timeout(5000)});
        const v = (await r.text()).trim();
        if (r.ok && v === target) {
          modalResult('Обновление установлено', `FreeNet ${target} запущен и подтверждён. Интерфейс сейчас обновится автоматически.`, 'ok');
          updateNotice(`FreeNet ${target} установлен и принят. Перезагружаем интерфейс…`, 'ok');
          setTimeout(() => location.reload(), 900);
          return;
        }
      } catch (_) {}
      await new Promise(resolve => setTimeout(resolve, 1000));
    }
    modalResult('Не удалось подтвердить возвращение интерфейса', `Целевая версия ${target} не была подтверждена браузером за ограниченное время. Не запускайте обновление повторно до проверки фактического состояния.`, 'bad');
    updateNotice(`Обновление завершалось, но браузер не подтвердил ${target}. Проверьте фактическое состояние перед повторной попыткой.`, 'bad');
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
              headers: {'Content-Type':'application/json'},
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
    applyNetworkProfile = applyDraft;
    applyButton.addEventListener('click', applyDraft);
    isp.addEventListener('change', markDraft);
    dns.addEventListener('change', markDraft);
    renderNetworkControls(false);
  }

  function ensureTopbarUpdateControl() {
    let control = qs('#topFreenetUpdate');
    if (control) return control;
    const actions = qs('.top-actions');
    if (!actions) return null;
    control = document.createElement('button');
    control.id = 'topFreenetUpdate';
    control.type = 'button';
    control.className = 'mini-link fn-version-control';
    const xkeen = qs('#topXkeenLink');
    if (xkeen && xkeen.parentNode === actions) actions.insertBefore(control, xkeen);
    else actions.appendChild(control);
    return control;
  }

  function renderTopbarVersion(currentVersion, updateAvailable = false, latestVersion = '') {
    const control = ensureTopbarUpdateControl();
    if (!control) return;
    const current = String(currentVersion || '').replace(/^v/i, '') || '—';
    control.textContent = '';
    control.className = 'mini-link fn-version-control' + (updateAvailable ? ' update-available' : '');
    control.setAttribute('aria-label', updateAvailable ? `FreeNet v${current}. Доступно обновление ${latestVersion || ''}` : `FreeNet v${current}. Проверить обновление`);
    const icon = document.createElement('span');
    icon.className = 'fn-version-icon';
    icon.textContent = updateAvailable ? '↑' : '◈';
    const copy = document.createElement('span');
    copy.className = 'fn-version-copy';
    const label = document.createElement('small');
    label.textContent = updateAvailable ? 'Обновление' : 'FreeNet';
    const value = document.createElement('strong');
    value.textContent = `v${current}`;
    copy.append(label, value);
    control.append(icon, copy);
  }

  function versionActionLabel(p) {
    if (!p || !p.target_tag) return 'Установить версию';
    if (p.direction === 'downgrade') return `Откатить до ${p.target_tag}`;
    if (p.direction === 'upgrade') return `Обновить до ${p.target_tag}`;
    return `Установлено ${p.target_tag}`;
  }

  function openVersionTargetConfirmation(p) {
    if (!p || !p.success || !p.ready || !p.update_available || !p.manifest_verified) return;
    const label = versionActionLabel(p);
    const downgrade = p.direction === 'downgrade';
    openModal({
      kicker: 'FreeNet · версии',
      title: label,
      body: `${p.current_version || 'текущая версия'} → ${p.target_tag}\n\nFreeNet установит только exact release после проверки SHA-256 и staging. Перед изменением будет создан snapshot FreeNet-owned файлов. После перезапуска FreeNet подтвердит выбранную версию и неизменность Xray-конфигурации.`,
      meta: downgrade
        ? 'Это downgrade. При любой ошибке применяется автоматический rollback. ROLLBACK FAILED/UNKNOWN блокирует дальнейшие изменения.'
        : 'Это upgrade. При любой ошибке применяется автоматический rollback. Пользовательские VPN/DNS/routing настройки не входят в expected delta.',
      confirmText: label,
      cancelText: 'Отмена',
      onConfirm: async () => {
        plan = p;
        await startUpdate();
      }
    });
  }

  async function selectVersionTarget(release, detail) {
    versionTargetPlan = null;
    detail.className = 'fn-version-detail checking';
    detail.textContent = `Проверяем exact release ${release.version}: manifest, SHA-256 и обязательные assets…`;
    try {
      const r = await fetch(`/api/system/update/plan?target=${encodeURIComponent(release.version)}`, {cache:'no-store'});
      const p = await r.json().catch(() => ({}));
      if (!r.ok || !p.success || !p.ready || !p.manifest_verified) {
        throw new Error(p.error || 'Этот релиз не прошёл compatibility plan');
      }
      if (p.target_tag !== release.version) throw new Error('FreeNet вернул другой target release');
      versionTargetPlan = p;
      detail.className = 'fn-version-detail ready';
      detail.textContent = '';
      const title = document.createElement('strong');
      title.textContent = p.direction === 'same' ? `${p.target_tag} уже установлена` : versionActionLabel(p);
      const text = document.createElement('span');
      text.textContent = p.direction === 'same'
        ? 'Текущая версия. Повторная установка не выполняется.'
        : `Manifest SHA-256 подтверждён. ${p.expected_no_delta || 'VPN/DNS/routing и Xray-конфигурация не должны измениться.'}`;
      detail.append(title, text);
      if (p.update_available) {
        const action = document.createElement('button');
        action.type = 'button';
        action.className = 'btn primary fn-version-apply';
        action.textContent = versionActionLabel(p);
        action.addEventListener('click', () => openVersionTargetConfirmation(p));
        detail.appendChild(action);
      }
    } catch (e) {
      detail.className = 'fn-version-detail bad';
      detail.textContent = `Эту версию нельзя применить безопасно: ${e.message || 'compatibility plan не подтверждён'}.`;
    }
  }

  function renderVersionManager(catalog) {
    const body = qs('#fnModalBody');
    body.textContent = '';
    body.className = 'fn-modal-body fn-version-manager-body';

    const summary = document.createElement('div');
    summary.className = 'fn-version-summary';
    summary.innerHTML = '<span>Текущая</span><strong></strong><span>Последняя стабильная</span><strong></strong>';
    const strongs = summary.querySelectorAll('strong');
    strongs[0].textContent = catalog.current_version || '—';
    strongs[1].textContent = catalog.latest_version || '—';

    const search = document.createElement('input');
    search.id = 'fnVersionSearch';
    search.className = 'fn-version-search';
    search.type = 'search';
    search.autocomplete = 'off';
    search.placeholder = 'Поиск версии, например v0.3.94';
    search.setAttribute('aria-label', 'Поиск версии FreeNet');

    const list = document.createElement('div');
    list.id = 'fnVersionList';
    list.className = 'fn-version-list';
    const detail = document.createElement('div');
    detail.id = 'fnVersionDetail';
    detail.className = 'fn-version-detail';
    detail.textContent = 'Выберите версию. Сам выбор ничего не меняет.';

    const releases = Array.isArray(catalog.releases) ? catalog.releases : [];
    const draw = () => {
      const q = search.value.trim().toLowerCase();
      list.textContent = '';
      const filtered = releases.filter(item => !q || String(item.version || '').toLowerCase().includes(q));
      if (!filtered.length) {
        const empty = document.createElement('div');
        empty.className = 'fn-version-empty';
        empty.textContent = 'Версии по этому фильтру не найдены.';
        list.appendChild(empty);
        return;
      }
      filtered.forEach(item => {
        const button = document.createElement('button');
        button.type = 'button';
        button.className = 'fn-version-release';
        button.dataset.version = item.version || '';
        if (item.current) button.classList.add('current');
        const main = document.createElement('span');
        main.className = 'fn-version-release-main';
        const v = document.createElement('strong');
        v.textContent = item.version || '—';
        main.appendChild(v);
        if (item.current) {
          const badge = document.createElement('em'); badge.textContent = 'Текущая'; badge.className = 'current'; main.appendChild(badge);
        }
        if (item.latest) {
          const badge = document.createElement('em'); badge.textContent = 'Последняя'; badge.className = 'latest'; main.appendChild(badge);
        }
        const date = document.createElement('small');
        date.textContent = item.published_at ? new Date(item.published_at).toLocaleDateString('ru-RU') : '';
        button.append(main, date);
        button.addEventListener('click', () => {
          list.querySelectorAll('.fn-version-release.selected').forEach(n => n.classList.remove('selected'));
          button.classList.add('selected');
          selectVersionTarget(item, detail);
        });
        list.appendChild(button);
      });
    };
    search.addEventListener('input', draw);
    body.append(summary, search, list, detail);
    draw();
  }

  async function openTopbarUpdateModal() {
    openModal({
      kicker: 'FreeNet · версии',
      title: 'Версии FreeNet',
      body: 'Загружаем опубликованные стабильные релизы…',
      closable: true
    });
    try {
      const r = await fetch('/api/system/update/releases', {cache:'no-store'});
      const catalog = await r.json().catch(() => ({}));
      if (!r.ok || !catalog.success || !Array.isArray(catalog.releases)) throw new Error(catalog.error || 'Каталог релизов недоступен');
      versionCatalog = catalog;
      renderVersionManager(catalog);
    } catch (e) {
      qs('#fnModalBody').className = 'fn-modal-body';
      qs('#fnModalBody').textContent = e.message || 'Не удалось загрузить каталог версий.';
      modalStatus('Никаких изменений не выполнено.', 'bad');
    }
  }

  async function refreshTopbarUpdateState() {
    try {
      const vr = await fetch('/versionz', {cache: 'no-store'});
      if (vr.ok) renderTopbarVersion((await vr.text()).trim());
    } catch (_) {}
    try {
      if (typeof authAuthenticated !== 'undefined' && !authAuthenticated) return;
      const r = await fetch('/api/system/update/plan', {cache: 'no-store'});
      if (!r.ok) return;
      const p = await r.json();
      if (!p.success) return;
      plan = p;
      if (qs('#webUpdateCurrent')) renderPlan(p);
      else renderTopbarVersion(p.current_version, !!p.update_available, p.latest_version);
    } catch (_) {}
  }

  function mountChromeUpdateUX() {
    if (!qs('#freenetChromeUpdateUX')) {
      const style = document.createElement('style');
      style.id = 'freenetChromeUpdateUX';
      style.textContent = `
        .side-bottom{display:none!important}
        .nav-btn[data-page="system"]{display:none!important}
        .fn-version-control{appearance:none;min-width:112px;min-height:46px;padding:6px 11px;display:flex;align-items:center;gap:9px;border:1px solid var(--line);border-radius:10px;background:#0b1523;color:#d6e3f5!important;text-decoration:none!important;cursor:pointer;font:inherit}
        .fn-version-control:hover{border-color:#456489;background:#122239;color:#fff!important}
        .fn-version-icon{display:grid;place-items:center;width:25px;height:25px;border-radius:8px;border:1px solid #315077;background:#0b1b2d;color:#82adff;font-size:14px;font-weight:900}
        .fn-version-copy{display:flex;flex-direction:column;line-height:1.08;text-align:left}
        .fn-version-copy small{font-size:9px;color:#899bb4;font-weight:700}
        .fn-version-copy strong{font-size:12.5px;color:#f4f7fb;margin-top:2px}
        .fn-version-control.update-available{border-color:rgba(73,218,146,.62);background:linear-gradient(180deg,rgba(18,70,55,.92),rgba(12,48,39,.92));box-shadow:inset 0 0 0 1px rgba(73,218,146,.09),0 0 18px rgba(73,218,146,.08)}
        .fn-version-control.update-available .fn-version-icon{border-color:rgba(73,218,146,.58);background:rgba(18,86,62,.65);color:#65f0ad}
        .fn-version-control.update-available .fn-version-copy small,.fn-version-control.update-available .fn-version-copy strong{color:#c9f7dc}
        .fn-version-manager-body{white-space:normal!important}.fn-version-summary{display:grid;grid-template-columns:auto 1fr;gap:5px 12px;padding:11px 12px;border:1px solid #29445f;border-radius:11px;background:#0a1929;font-size:11px}.fn-version-summary span{color:#8fa4bf}.fn-version-summary strong{color:#f2f7ff}
        .fn-version-search{width:100%;margin-top:12px;padding:10px 11px;border:1px solid #315070;border-radius:10px;background:#081624;color:#eef5ff;font:inherit;font-size:12px;outline:none}.fn-version-search:focus{border-color:#6094df;box-shadow:0 0 0 2px rgba(96,148,223,.12)}
        .fn-version-list{display:grid;gap:6px;max-height:300px;overflow:auto;margin-top:10px;padding-right:2px}.fn-version-release{appearance:none;display:flex;align-items:center;justify-content:space-between;gap:12px;width:100%;padding:9px 11px;border:1px solid #29445f;border-radius:9px;background:#0a1929;color:#dbe8f8;text-align:left;cursor:pointer}.fn-version-release:hover,.fn-version-release.selected{border-color:#5a8dcc;background:#12304d}.fn-version-release.current{box-shadow:inset 3px 0 #39d79a}.fn-version-release-main{display:flex;align-items:center;gap:7px}.fn-version-release-main strong{font-size:12.5px}.fn-version-release em{padding:2px 6px;border-radius:999px;background:#173652;color:#a9caff;font-size:8px;font-style:normal;font-weight:800}.fn-version-release em.current{background:rgba(52,221,159,.14);color:#65e3aa}.fn-version-release em.latest{background:rgba(81,137,255,.18);color:#8bb4ff}.fn-version-release small{color:#8198b5;font-size:9px}
        .fn-version-detail{display:grid;gap:8px;margin-top:12px;padding:12px;border:1px solid #29445f;border-radius:11px;background:#091827;color:#9fb2ca;font-size:11.5px;line-height:1.5}.fn-version-detail strong{color:#f2f7ff;font-size:13px}.fn-version-detail.ready{border-color:#35658e}.fn-version-detail.bad{border-color:rgba(255,104,115,.45);color:#ffd0d4}.fn-version-detail.checking{color:#c4d7ee}.fn-version-apply{margin-top:4px;width:100%}.fn-version-empty{padding:14px;text-align:center;color:#839ab8;border:1px dashed #29445f;border-radius:9px;font-size:11px}
        @media(max-width:760px){.fn-version-control{min-width:46px}.fn-version-copy small{display:none}.fn-version-list{max-height:250px}}
      `;
      document.head.appendChild(style);
    }
    if (typeof pageLabels === 'object') delete pageLabels.system;
    if (location.hash === '#system') setPage('overview');
    const control = ensureTopbarUpdateControl();
    if (control) control.dataset.freenetVersionManager = '1';
    if (control && control.dataset.freenetUpdateBound !== '1') {
      control.dataset.freenetUpdateBound = '1';
      control.addEventListener('click', event => {
        event.preventDefault();
        openTopbarUpdateModal();
      });
    }
    refreshTopbarUpdateState();
    setInterval(refreshTopbarUpdateState, 10 * 60 * 1000);
  }

  function mount() {
    mountTypographyReadability();
    mountDashboardStability();
    mountModalLayer();
    mountOverviewVPNFlow();
    mountUpdate();
    mountChromeUpdateUX();
    mountNetworkDraftFlow();
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mount);
  else mount();
})();