(() => {
  const pendingTitles = new Set(['Требуется проверка состояния', 'Связь прервалась']);
  const flagPrefix = /^[\u{1F1E6}-\u{1F1FF}]{2}\s+/u;

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
    if (window.FreeNetVPNPicker && typeof window.FreeNetVPNPicker.close === 'function') window.FreeNetVPNPicker.close();
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
    if (window.FreeNetVPNPicker && typeof window.FreeNetVPNPicker.isOpen === 'function' && window.FreeNetVPNPicker.isOpen()) return;
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

  function settingsGearSVG() {
    return '<svg viewBox="0 0 24 24" width="19" height="19" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 15.5A3.5 3.5 0 1 0 12 8a3.5 3.5 0 0 0 0 7.5Z"></path><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06A1.65 1.65 0 0 0 15 19.4a1.65 1.65 0 0 0-1 .6 1.65 1.65 0 0 0-.36 1.05V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 8.6 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.6 15a1.65 1.65 0 0 0-.6-1 1.65 1.65 0 0 0-1.05-.36H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 8.6a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.6c.38-.16.72-.39 1-.7A1.65 1.65 0 0 0 10.36 3V3a2 2 0 1 1 4 0v.09A1.65 1.65 0 0 0 15.4 4.6a1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9c.16.38.39.72.7 1 .31.28.69.44 1.1.44H21a2 2 0 1 1 0 4h-.09A1.65 1.65 0 0 0 19.4 15Z"></path></svg>';
  }

  function ensureSettingsNavStable() {
    const nav = qs('.nav');
    if (!nav) return;
    let settings = qs('.nav-btn[data-page="settings"]', nav);
    if (!settings) {
      const before = qs('.nav-btn[data-page="network"]', nav) || qs('.nav-btn[data-page="routing"]', nav);
      settings = document.createElement('button');
      settings.type = 'button';
      settings.className = 'nav-btn';
      settings.dataset.page = 'settings';
      settings.innerHTML = `<span class="nav-icon">${settingsGearSVG()}</span><span>Настройки</span>`;
      settings.addEventListener('click', () => {
        location.hash = '#settings';
        if (typeof showPage === 'function') showPage('settings');
      });
      nav.insertBefore(settings, before || null);
    }
    settings.hidden = false;
    settings.removeAttribute('aria-hidden');
    settings.style.removeProperty('display');
    const icon = qs('.nav-icon', settings);
    if (icon) icon.innerHTML = settingsGearSVG();
  }

  function stripLeadingFlagText(node) {
    if (!node || !node.childNodes || node.querySelector?.('.flag-icon')) return;
    node.childNodes.forEach(child => {
      if (child.nodeType === Node.TEXT_NODE && flagPrefix.test(child.textContent || '')) {
        child.textContent = String(child.textContent || '').replace(flagPrefix, '');
      }
    });
  }

  function normalizeCurrentVPNFlags() {
    qsa('.card, .hero, #settingsPage, [data-page-view="settings"], [data-page-view="overview"]').forEach(root => {
      if (!String(root.textContent || '').includes('Текущий VPN')) return;
      qsa('h1,h2,h3,h4,strong,b,.country-name,.profile-name,.current-vpn-name,.fn-current-vpn-title', root).forEach(stripLeadingFlagText);
    });
  }

  function mountGuards() {
    patchSubscriptionCopy();
    ensureSettingsNavStable();
    normalizeCurrentVPNFlags();
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
