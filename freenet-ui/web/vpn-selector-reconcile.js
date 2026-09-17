(() => {
  const pendingTitles = new Set(['Требуется проверка состояния', 'Связь прервалась']);

  function pendingEndpoint() {
    const card = document.querySelector('#selectedProfileCard');
    if (!card) return '';
    const title = String(card.querySelector('strong')?.textContent || '').trim();
    if (!pendingTitles.has(title)) return '';
    const endpoint = String(card.querySelector('.selected-endpoint')?.textContent || '').trim();
    return endpoint && endpoint !== '—' ? endpoint : '';
  }

  function hideStaleNotice(id) {
    const box = document.getElementById(id);
    if (!box) return;
    const text = String(box.textContent || '');
    if (
      text.includes('Фактическое состояние VPN после подключения не подтверждено') ||
      text.includes('Связь прервалась во время переключения') ||
      id === 'providerNotice'
    ) {
      if (typeof hideBox === 'function') hideBox(id);
      else box.hidden = true;
    }
  }

  function finishPendingManualSwitch() {
    if (typeof selectedProviderID !== 'undefined') selectedProviderID = '';
    if (typeof selectedProviderName !== 'undefined') selectedProviderName = '';
    if (typeof providerPlanReady !== 'undefined') providerPlanReady = false;
    if (typeof providerApplied !== 'undefined') providerApplied = true;

    if (typeof renderProfileOptions === 'function') renderProfileOptions();
    if (typeof renderSelectedProfile === 'function') renderSelectedProfile(null);
    else {
      const card = document.querySelector('#selectedProfileCard');
      if (card) {
        card.textContent = '';
        card.hidden = true;
      }
    }

    const quick = document.querySelector('#quickActionsSection');
    const exactRow = document.querySelector('#exactConnectRow');
    const routine = quick && quick.querySelector('.action-row:not(#exactConnectRow)');
    if (exactRow) exactRow.hidden = true;
    if (routine) routine.hidden = false;

    const connect = document.querySelector('#exactConnectBtn');
    if (connect) {
      connect.disabled = true;
      connect.textContent = 'Выберите сервер заново';
    }

    if (typeof closeProfileMenu === 'function') closeProfileMenu();
    hideStaleNotice('providerNotice');
    hideStaleNotice('notice');
  }

  function reconcilePendingManualSwitch(status) {
    const expectedEndpoint = pendingEndpoint();
    if (!expectedEndpoint || !status) return false;
    const accepted = status.endpoint === expectedEndpoint && !status.busy && !status.updater_busy && status.xray_online === true;
    if (!accepted) return false;
    finishPendingManualSwitch();
    return true;
  }

  if (typeof updateStatusViews !== 'function') return;
  const previousUpdateStatusViews = updateStatusViews;
  updateStatusViews = function(status) {
    const result = previousUpdateStatusViews.apply(this, arguments);
    reconcilePendingManualSwitch(status);
    return result;
  };
})();
