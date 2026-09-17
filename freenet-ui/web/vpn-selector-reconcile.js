(() => {
  const pendingTitles = new Set(['Требуется проверка состояния', 'Связь прервалась']);

  function qs(selector, root = document) {
    return root.querySelector(selector);
  }

  function qsa(selector, root = document) {
    return Array.from(root.querySelectorAll(selector));
  }

  function deferMountGuards() {
    if (typeof queueMicrotask === 'function') queueMicrotask(mountGuards);
    else Promise.resolve().then(mountGuards).catch(() => {});
  }

  function pendingEndpoint() {
    const card = qs('#selectedProfileCard');
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

  function closeTopbarSelector() {
    const trigger = qs('#profilesTrigger');
    const menu = qs('#profilesMenu');
    const search = qs('#profileSearch');
    const text = qs('#profilesTriggerText');

    if (search) {
      search.value = '';
      search.blur();
      search.dispatchEvent(new Event('input', {bubbles: true}));
    }
    if (menu) {
      menu.hidden = true;
      menu.classList.remove('open', 'show', 'active');
      menu.setAttribute('aria-hidden', 'true');
    }
    if (trigger) {
      trigger.classList.remove('open', 'active');
      trigger.setAttribute('aria-expanded', 'false');
      trigger.blur();
    }
    if (text && !String(text.textContent || '').trim()) text.textContent = 'Выбрать VPN-сервер';
  }

  function finishPendingManualSwitch() {
    if (typeof selectedProviderID !== 'undefined') selectedProviderID = '';
    if (typeof selectedProviderName !== 'undefined') selectedProviderName = '';
    if (typeof providerPlanReady !== 'undefined') providerPlanReady = false;
    if (typeof providerApplied !== 'undefined') providerApplied = true;

    if (typeof renderProfileOptions === 'function') renderProfileOptions();
    if (typeof renderSelectedProfile === 'function') renderSelectedProfile(null);
    else {
      const card = qs('#selectedProfileCard');
      if (card) {
        card.textContent = '';
        card.hidden = true;
      }
    }

    const quick = qs('#quickActionsSection');
    const exactRow = qs('#exactConnectRow');
    const routine = quick && quick.querySelector('.action-row:not(#exactConnectRow)');
    if (exactRow) exactRow.hidden = true;
    if (routine) routine.hidden = false;

    const connect = qs('#exactConnectBtn');
    if (connect) {
      connect.disabled = true;
      connect.textContent = 'Выберите сервер заново';
    }

    closeTopbarSelector();
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

  function clearSelectorAfterSuccessfulStatus(status) {
    if (!status || status.xray_online !== true || status.busy || status.updater_busy) return;
    const menu = qs('#profilesMenu');
    const search = qs('#profileSearch');
    const hasOpenMenu = !!(menu && !menu.hidden && getComputedStyle(menu).display !== 'none');
    const hasSearch = !!(search && String(search.value || '').trim());
    if (!hasOpenMenu && !hasSearch) return;
    closeTopbarSelector();
  }

  function patchSubscriptionCopy() {
    qsa('.fn-sub-next .fn-sub-meta').forEach(node => {
      if (String(node.textContent || '').includes('AUTO VPN')) {
        node.textContent = 'Автообновление списка Extra-профилей пока не включено.';
      }
    });
  }

  function ensureSettingsNavStable() {
    const nav = qs('.nav');
    if (!nav) return;
    const settings = qs('.nav-btn[data-page="settings"]', nav);
    if (settings) {
      settings.hidden = false;
      settings.removeAttribute('aria-hidden');
      settings.style.removeProperty('display');
      return;
    }
    const before = qs('.nav-btn[data-page="network"]', nav) || qs('.nav-btn[data-page="routing"]', nav);
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'nav-btn';
    button.dataset.page = 'settings';
    button.innerHTML = '<span class="nav-icon">⚙</span><span>Настройки</span>';
    button.addEventListener('click', () => {
      location.hash = '#settings';
      if (typeof showPage === 'function') showPage('settings');
    });
    nav.insertBefore(button, before || null);
  }

  function mountGuards() {
    patchSubscriptionCopy();
    ensureSettingsNavStable();
  }

  if (typeof updateStatusViews === 'function') {
    const previousUpdateStatusViews = updateStatusViews;
    updateStatusViews = function(status) {
      const result = previousUpdateStatusViews.apply(this, arguments);
      reconcilePendingManualSwitch(status);
      clearSelectorAfterSuccessfulStatus(status);
      mountGuards();
      return result;
    };
  }

  document.addEventListener('click', event => {
    if (!event.target?.closest?.('#profilesTrigger, #profilesMenu')) return;
    deferMountGuards();
  }, true);
  document.addEventListener('input', event => {
    if (event.target?.matches?.('#profileSearch')) deferMountGuards();
  }, true);
  window.addEventListener('hashchange', deferMountGuards);

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mountGuards, {once: true});
  else mountGuards();
  requestAnimationFrame(mountGuards);
})();
