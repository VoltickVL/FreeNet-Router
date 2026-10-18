(() => {
  const qs = (s, root = document) => root.querySelector(s);
  let plan = null;
  let polling = false;
  let activeModalConfirm = null;
  let updateProgressTimer = null;
  let updateProgressStarted = 0;
  let versionCatalog = null;
  let versionChannel = 'stable';
  const betaTag = tag => /^v\d+\.\d+\.\d+-beta\.([1-9]|[1-9][0-9])$/.test(String(tag || ''));
  let versionTargetPlan = null;
  let versionSelectionToken = 0;
  let modalFocusBeforeOpen = null;

  function stopUpdateProgress() {
    clearInterval(updateProgressTimer);
    updateProgressTimer = null;
    updateProgressStarted = 0;
    qs('#fnUpdateProgress')?.remove();
  }

  

  function mountDashboardStability() {
    const quick = qs('#quickActionsSection');
    const profiles = qs('#profilesList');
    const selected = qs('#selectedProfileCard');
    if (!quick || !profiles) return;

    if (!(document.documentElement.dataset.freenetStyleselfupdatejs2 === "1")) {
      document.documentElement.dataset.freenetStyleselfupdatejs2 = "1";
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
    document.documentElement.dataset.freenetStyleselfupdatejs3 = "1";

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

  function positionVersionPicker() {
    const root = qs('#fnModalRoot');
    const modal = qs('#fnModalRoot .fn-modal');
    const control = qs('#topFreenetUpdate');
    if (!root || root.hidden || !root.classList.contains('fn-version-picker-mode') || !modal || !control) return;
    const vw = window.visualViewport?.width || window.innerWidth;
    const vh = window.visualViewport?.height || window.innerHeight;
    const rect = control.getBoundingClientRect();
    const width = Math.min(540, vw - 24);
    modal.style.width = width + 'px';
    modal.style.left = Math.max(12, Math.min(vw - width - 12, rect.right - width)) + 'px';
    const desiredTop = rect.bottom + 8;
    const modalHeight = Math.min(modal.scrollHeight || 620, Math.max(220, vh - 24));
    modal.style.top = Math.max(12, Math.min(desiredTop, vh - modalHeight - 12)) + 'px';
  }

  function openModal({kicker = 'FreeNet', title, body = '', meta = '', confirmText = 'Применить', cancelText = 'Отмена', onConfirm = null, closable = true, anchored = false}) {
    mountModalLayer();
    const root = qs('#fnModalRoot');
    if (root.hidden) modalFocusBeforeOpen = document.activeElement;
    // The anchored release picker writes inline left/top/width. Those offsets
    // MUST NOT survive when the Beta confirmation becomes a centered modal.
    // Otherwise CSS position:relative applies them a second time and pushes
    // the confirmation off the right edge of the viewport.
    const modal = qs('.fn-modal', root);
    if (!anchored && modal) {
      for (const property of ['left', 'top', 'width']) modal.style.removeProperty(property);
    }
    root.classList.toggle('fn-version-picker-mode', !!anchored);
    modal?.setAttribute('aria-modal', anchored ? 'false' : 'true');
    qs('#topFreenetUpdate')?.setAttribute('aria-expanded', anchored ? 'true' : 'false');
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
    if (anchored) requestAnimationFrame(positionVersionPicker);
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
    const anchored = root.classList.contains('fn-version-picker-mode');
    activeModalConfirm = null;
    root.hidden = true;
    root.classList.remove('fn-version-picker-mode');
    if (anchored) qs('#topFreenetUpdate')?.setAttribute('aria-expanded', 'false');
    if (anchored && modalFocusBeforeOpen?.isConnected) modalFocusBeforeOpen.focus({preventScroll:true});
    modalFocusBeforeOpen = null;
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
        meta: `Endpoint: ${pp.endpoint || formatProfileEndpoint(p)}\nИзменится: ${pp.expected_delta || 'точный VPN-профиль'}\nНе изменится: DNS и routing policy`,
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
      body: 'Версия проверена и готова к установке. FreeNet создаст резервную копию, обновится и автоматически проверит результат.',
      meta: 'Ваши настройки VPN, DNS и маршрутизации сохраняются.',
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

  async function startUpdateRecovery(targetTag = '') {
    // The normal no-argument Recovery still targets latest Stable. A selected
    // exact Beta uses this path only after the user's explicit confirmation.
    const exact = typeof targetTag === 'string' ? targetTag.trim() : '';
    if (exact && !betaTag(exact)) {
      updateNotice('Неверная Beta-версия для Recovery', 'bad');
      return;
    }
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
      const recoveryRequest = {method: 'POST', headers: {'Content-Type': 'application/json'}};
      if (exact) recoveryRequest.body = JSON.stringify({target_tag: exact, beta_acknowledged: true});
      const r = await fetch('/api/system/update/recover', recoveryRequest);
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
      else openModal({kicker: 'Обновление FreeNet', title: 'Установлена актуальная версия', body: `${p.current_version || 'FreeNet'} уже является последним опубликованным релизом.`, meta: 'Проверка завершена.', closable: true});
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
    updateNotice(isDowngrade ? 'Возвращаем выбранную версию. FreeNet кратко перезапустится.' : 'Устанавливаем обновление. FreeNet кратко перезапустится.');
    modalProgress(`Устанавливаем ${plan.target_tag}…`, 'Подготавливаем обновление и создаём резервную копию. После перезапуска FreeNet автоматически проверит результат.');
    showInstallingReleaseNotes();
    try {
      const r = await fetch('/api/system/update/apply', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({target_tag: plan.target_tag, ...(betaTag(plan.target_tag) ? {beta_acknowledged: true} : {})})
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

  async function targetVersionIsLive(target) {
    const expected = String(target || '').trim();
    if (!expected) return false;
    try {
      const response = await fetch('/versionz', {cache: 'no-store', signal: AbortSignal.timeout(5000)});
      if (!response.ok) return false;
      return (await response.text()).trim() === expected;
    } catch (_) {
      return false;
    }
  }

  async function reloadIfTargetVersionIsLive(target) {
    if (!(await targetVersionIsLive(target))) return false;
    polling = false;
    setUpdateSummary('Новая версия уже запущена');
    updateNotice('Сервер уже работает на целевой версии. Перезагружаем Control Center, чтобы убрать старый интерфейс.');
    modalStatus('Целевая версия уже запущена. Обновляем интерфейс без повторного запуска update.', '');
    setTimeout(() => location.reload(), 250);
    return true;
  }

  async function pollState(target) {
    if (!polling) return;
    if (updateProgressStarted && Date.now() - updateProgressStarted > 300000) {
      if (await reloadIfTargetVersionIsLive(target)) return;
      if (Date.now() - updateProgressStarted > 600000) {
        polling = false;
        modalResult('Результат обновления пока неизвестен', 'FreeNet не подтвердил целевую версию за десять минут. Не запускайте обновление повторно до проверки фактической версии и состояния.', 'bad');
        return;
      }
      setUpdateSummary('Обновление выполняется дольше обычного…');
      modalStatus('Продолжаем следить за текущей операцией. Повторный запуск обновления не выполняется.', '');
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
    showInstallingReleaseNotes();
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
    const dns = qs('#dnsModeSelect');
    const oldSave = qs('#saveNetworkBtn');
    const oldPlan = qs('#planNetworkBtn');
    const oldApply = qs('#applyNetworkBtn');
    if (!page || !dns || !oldPlan || !oldApply) return;

    const intro = qs('.page-head p', page);
    if (intro) intro.textContent = 'Выберите режим DNS. Сначала FreeNet проверит изменения без записи настроек, затем применит их одной транзакцией.';
    if (oldSave) oldSave.hidden = true;
    const firmwareOption = dns.querySelector('option[value="firmware"]');
    if (firmwareOption) firmwareOption.textContent = 'Прямой';
    dnsLabels.firmware = 'Прямой';

    const planButton = oldPlan.cloneNode(true);
    planButton.textContent = 'Проверить изменения';
    oldPlan.replaceWith(planButton);
    const applyButton = oldApply.cloneNode(true);
    applyButton.textContent = 'Применить';
    oldApply.replaceWith(applyButton);

    const draftParams = (profileID = '') => {
      const q = new URLSearchParams();
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
        kicker: 'Сеть · DNS',
        title: 'Применить проверенные настройки?',
        body: 'FreeNet сначала сделает резервную копию. Активный DNS-профиль будет сохранён только после успешной проверки результата.',
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
              body: JSON.stringify({operation: 'network', dns_mode: dns.value, confirm: true})
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
    // Semantic content exists before asynchronous version requests complete.
    // Unknown is explicit; never create a blank version control during login/remount.
    control.innerHTML = '<span class="fn-version-icon" aria-hidden="true">◈</span><span class="fn-version-copy"><small>FreeNet</small><strong>—</strong></span>';
    control.type = 'button';
    control.className = 'mini-link fn-version-control';
    control.setAttribute('aria-haspopup', 'dialog');
    control.setAttribute('aria-controls', 'fnModalRoot');
    control.setAttribute('aria-expanded', 'false');
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
    control.setAttribute('aria-label', updateAvailable ? `FreeNet v${current}. Доступно обновление ${latestVersion || ''}` : `FreeNet v${current}. Управление версиями`);
    control.setAttribute('aria-expanded', String(!!(qs('#fnModalRoot') && !qs('#fnModalRoot').hidden && qs('#fnModalRoot').classList.contains('fn-version-picker-mode'))));
    const icon = document.createElement('span');
    icon.className = 'fn-version-icon';
    icon.textContent = updateAvailable ? '↑' : '◈';
    const copy = document.createElement('span');
    copy.className = 'fn-version-copy';
    const label = document.createElement('small');
    label.textContent = 'FreeNet';
    const value = document.createElement('strong');
    value.textContent = `v${current}`;
    copy.append(label, value);
    control.append(icon, copy);
  }

  function stateApplyingVersion() {
    return !!updateProgressStarted || !!(plan && ['CHECKING','SNAPSHOT','UPDATING','RECONNECTING','BUSY'].includes(String(plan.state || '').toUpperCase()));
  }

  function versionActionLabel(p) {
    if (!p || !p.target_tag) return 'Установить версию';
    if (p.direction === 'downgrade') return `Откатить до ${p.target_tag}`;
    if (p.direction === 'upgrade') return `Обновить до ${p.target_tag}`;
    return `Установлено ${p.target_tag}`;
  }

  function readableReleaseNotes(release) {
    const raw = String(release?.release_notes || '').trim();
    if (!raw) return '';
    return raw.split(/\r?\n/).map(line => line.trim())
      .filter(line => line && !/^#{1,3}\s*(?:What's Changed|Что изменилось)\s*$/i.test(line) && !/^\*\*Full Changelog\*\*/i.test(line))
      .map(line => line.replace(/^[-*]\s+/, '• ').replace(/\s+by\s+@[\w-]+\s+in\s+https:\/\/github\.com\/\S+$/i, '').replace(/\[([^\]]+)\]\(https?:\/\/[^)]+\)/g, '$1'))
      .slice(0, 12).join('\n').slice(0, 2400);
  }

  function appendVersionReleaseNotes(root, release) {
    // Missing GitHub Release body is not a changelog. Keep the compact selector
    // unchanged for legacy releases rather than adding a fake information card.
    const description = readableReleaseNotes(release);
    if (!root || !description) return;
    const notes = document.createElement('div');
    notes.className = 'fn-version-release-notes';
    const title = document.createElement('strong');
    title.textContent = `Что изменилось в ${release?.version || 'релизе'}`;
    const content = document.createElement('p');
    content.textContent = description;
    notes.append(title, content);
    root.appendChild(notes);
  }

  function showInstallingReleaseNotes() {
    const target = String(plan?.target_tag || '').trim();
    if (!target) return;
    const release = versionCatalog?.releases?.find(item => item.version === target);
    const notes = release || {version: target, release_notes: plan?.release_notes || ''};
    appendVersionReleaseNotes(qs('#fnModalBody'), notes);
  }

  async function selectVersionTarget(release, detail) {
    const token = ++versionSelectionToken;
    versionTargetPlan = null;
    detail.className = 'fn-version-detail checking';
    detail.textContent = `Проверяем версию ${release.version}…`;
    appendVersionReleaseNotes(detail, release);
    requestAnimationFrame(positionVersionPicker);
    try {
      const mode = release.prerelease ? '&channel=beta' : '';
      const r = await fetch(`/api/system/update/plan?target=${encodeURIComponent(release.version)}${mode}`, {cache:'no-store'});
      const p = await r.json().catch(() => ({}));
      if (token !== versionSelectionToken) return;
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
        ? 'Эта версия уже установлена.'
        : 'Версия проверена и готова к установке.';
      detail.append(title, text);
      appendVersionReleaseNotes(detail, release);
      if (p.update_available) {
        const action = document.createElement('button');
        action.type = 'button';
        action.className = 'btn primary fn-version-apply';
        action.textContent = versionActionLabel(p);
        action.addEventListener('click', async () => {
          if (stateApplyingVersion()) return;
          plan = p;
          if (release.prerelease) {
            let protectedRecovery = false;
            try {
              const statusResponse = await fetch('/api/status', {cache: 'no-store', signal: AbortSignal.timeout(6000)});
              if (statusResponse.ok) protectedRecovery = (await statusResponse.json()).mutation_blocked === true;
            } catch (_) {}
            if (protectedRecovery) {
              openModal({
                kicker: 'FreeNet · аварийный Recovery',
                title: `Установить ${p.target_tag} без изменения VPN?`,
                body: 'Предыдущая VPN-транзакция незавершена. Обычное обновление заблокировано. Recovery проверит точный Beta-релиз и SHA-256, сохранит резервную копию FreeNet и установит только компоненты Control Center.',
                meta: 'Исходный VPN checkpoint, Xray, XKeen, DNS и routing сохраняются. После установки используйте ручной выбор сервера для подтверждённого восстановления VPN. При ошибке обновления FreeNet попытается вернуть прежние файлы; неопределённый rollback остановит дальнейшие изменения.',
                confirmText: `Да, Recovery ${p.target_tag}`,
                cancelText: 'Ничего не менять',
                onConfirm: () => startUpdateRecovery(p.target_tag)
              });
              return;
            }
            openModal({
              kicker: 'FreeNet · BETA',
              title: `Установить экспериментальную ${p.target_tag}?`,
              body: 'Это предварительная версия. Для неё не подтверждены все аппаратные сценарии Keenetic/XKeen. Рабочий VPN может потребовать отката.',
              meta: 'Установка только по вашему явному выбору. FreeNet сначала сверит exact release, SHA-256, сохранит snapshot и при ошибке попытается восстановить прежнюю версию.',
              confirmText: `Да, установить BETA ${p.target_tag}`,
              cancelText: 'Остаться на текущей версии',
              onConfirm: startUpdate
            });
            return;
          }
          await startUpdate();
        });
        detail.appendChild(action);
      }
      requestAnimationFrame(positionVersionPicker);
    } catch (e) {
      if (token !== versionSelectionToken) return;
      detail.className = 'fn-version-detail bad';
      detail.textContent = `Эту версию сейчас нельзя установить: ${e.message || 'проверка не пройдена'}.`;
      appendVersionReleaseNotes(detail, release);
      requestAnimationFrame(positionVersionPicker);
    }
  }

  function renderVersionManager(catalog) {
    const body = qs('#fnModalBody');
    body.textContent = '';
    body.className = 'fn-modal-body fn-version-manager-body';

    const summary = document.createElement('div');
    summary.className = 'fn-version-summary';
    summary.innerHTML = '<span>Текущая версия</span><strong></strong><span>Последняя версия</span><strong></strong>';
    const strongs = summary.querySelectorAll('strong');
    strongs[0].textContent = catalog.current_version || '—';
    strongs[1].textContent = catalog.latest_version || '—';

    const degraded = catalog.degraded && catalog.warning ? document.createElement('div') : null;
    if (degraded) {
      degraded.className = 'fn-version-warning';
      degraded.textContent = catalog.warning;
    }

    const search = document.createElement('input');
    search.id = 'fnVersionSearch';
    search.className = 'fn-version-search';
    search.type = 'search';
    search.autocomplete = 'off';
    search.placeholder = 'Найти версию FreeNet';
    search.setAttribute('aria-label', 'Поиск версии FreeNet');

    const list = document.createElement('div');
    list.id = 'fnVersionList';
    list.className = 'fn-version-list';
    const detail = document.createElement('div');
    detail.id = 'fnVersionDetail';
    detail.className = 'fn-version-detail';
    detail.textContent = 'Проверяем доступное обновление…';

    const channel = catalog.channel === 'beta' ? 'beta' : 'stable';
    const channelChoice = document.createElement('div');
    channelChoice.className = 'fn-version-channels';
    channelChoice.setAttribute('role', 'group');
    channelChoice.setAttribute('aria-label', 'Канал обновлений FreeNet');
    for (const option of [{id:'stable',label:'Stable'}, {id:'beta',label:'Beta · тестовая'}]) {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'fn-version-channel' + (option.id === channel ? ' selected' : '');
      button.textContent = option.label;
      button.setAttribute('aria-pressed', String(option.id === channel));
      button.addEventListener('click', async () => {
        if (option.id === channel) return;
        detail.textContent = option.id === 'beta'
          ? 'Открываем тестовые версии только по вашему выбору…'
          : 'Показываем только стабильные версии…';
        try {
          const url = option.id === 'beta'
            ? '/api/system/update/releases?channel=beta&fresh=1'
            : '/api/system/update/releases?fresh=1';
          const response = await fetch(url, {cache:'no-store', signal: AbortSignal.timeout(12000)});
          const next = await response.json().catch(() => ({}));
          if (!response.ok || !next.success || !Array.isArray(next.releases)) {
            throw new Error(next.error || 'Каталог канала недоступен');
          }
          versionChannel = option.id;
          versionCatalog = next;
          ++versionSelectionToken;
          renderVersionManager(next);
        } catch (e) {
          detail.className = 'fn-version-detail bad';
          const reason = e.name === 'TypeError'
            ? 'нет связи с FreeNet — проверьте подключение и повторите открытие списка версий'
            : (e.name === 'TimeoutError' ? 'превышено время ожидания FreeNet' : (e.message || 'нет данных'));
          detail.textContent = 'Канал не переключён: ' + reason + '. Текущая версия не меняется.';
        }
      });
      channelChoice.appendChild(button);
    }

    const releases = Array.isArray(catalog.releases) ? catalog.releases : [];
    // If running Beta, do not automatically propose an older stable release
    // as an "upgrade"; returning to Stable requires choosing a version.
    const latest = channel === 'stable' && betaTag(catalog.current_version)
      ? null : (releases.find(item => item.latest && !item.current) || null);
    const draw = () => {
      const q = search.value.trim().toLowerCase();
      list.textContent = '';
      const filtered = releases.filter(item => !q || String(item.version || '').toLowerCase().includes(q));
      const visible = filtered.slice(0, 5);
      if (!visible.length) {
        const empty = document.createElement('div');
        empty.className = 'fn-version-empty';
        empty.textContent = 'Версии по этому фильтру не найдены.';
        list.appendChild(empty);
        return;
      }
      visible.forEach(item => {
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
      requestAnimationFrame(positionVersionPicker);
    };
    search.addEventListener('input', draw);
    body.append(summary, channelChoice);
    if (channel === 'beta') {
      const warning = document.createElement('p');
      warning.className = 'fn-version-beta-warning';
      warning.textContent = 'Beta — предварительные сборки. Не устанавливаются автоматически и не влияют на Stable. Выберите конкретную Beta только для испытаний.';
      body.appendChild(warning);
    }
    if (degraded) body.append(degraded);
    body.append(search, list, detail);
    draw();
    if (latest) {
      const latestButton = list.querySelector(`.fn-version-release[data-version="${CSS.escape(latest.version || '')}"]`);
      latestButton?.classList.add('selected');
      void selectVersionTarget(latest, detail);
    } else {
      detail.className = 'fn-version-detail ready';
      detail.textContent = channel === 'stable' && betaTag(catalog.current_version)
        ? 'Выберите Stable для добровольного возврата или перейдите на вкладку Beta.'
        : 'Установлена актуальная версия FreeNet.';
    }
  }

  async function openTopbarUpdateModal() {
    const root = qs('#fnModalRoot');
    if (root && !root.hidden && root.classList.contains('fn-version-picker-mode')) {
      closeModal();
      return;
    }
    openModal({
      kicker: 'FreeNet',
      title: 'Версия FreeNet',
      body: 'Проверяем доступную версию…',
      closable: true,
      anchored: true
    });
    try {
      const r = await fetch('/api/system/update/releases?fresh=1', {cache:'no-store'});
      const catalog = await r.json().catch(() => ({}));
      if (!r.ok || !catalog.success || !Array.isArray(catalog.releases)) throw new Error(catalog.error || 'Каталог релизов недоступен');
      versionCatalog = catalog;
      versionChannel = 'stable';
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
      const r = await fetch('/api/system/update/releases', {cache: 'no-store', signal: AbortSignal.timeout(15000)});
      if (!r.ok) return;
      const catalog = await r.json();
      if (!catalog.success) return;
      const current = catalog.current_version || '';
      const latest = catalog.latest_version || '';
      renderTopbarVersion(current, !betaTag(current) && !!latest && latest !== current, latest);
    } catch (_) {}
  }

  function mountChromeUpdateUX() {
    if (!(document.documentElement.dataset.freenetStyleselfupdatejs4 === "1")) {
      document.documentElement.dataset.freenetStyleselfupdatejs4 = "1";
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
    if (document.documentElement.dataset.freenetVersionPickerEvents !== '1') {
      document.documentElement.dataset.freenetVersionPickerEvents = '1';
      document.addEventListener('click', event => {
        const root = qs('#fnModalRoot');
        if (!root || root.hidden || !root.classList.contains('fn-version-picker-mode')) return;
        if (qs('#fnModalClose')?.hidden || qs('#fnModalClose')?.disabled) return;
        if (event.target.closest?.('#topFreenetUpdate') || event.target.closest?.('#fnModalRoot .fn-modal')) return;
        closeModal();
      }, true);
      document.addEventListener('keydown', event => {
        const root = qs('#fnModalRoot');
        if (event.key !== 'Escape' || !root || root.hidden || !root.classList.contains('fn-version-picker-mode')) return;
        if (qs('#fnModalClose')?.hidden || qs('#fnModalClose')?.disabled) return;
        event.preventDefault();
        closeModal();
      });
      window.addEventListener('resize', positionVersionPicker);
      window.visualViewport?.addEventListener('resize', positionVersionPicker);
    }
    refreshTopbarUpdateState();
    setInterval(refreshTopbarUpdateState, 10 * 60 * 1000);
  }

  function mount() {
    ;
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