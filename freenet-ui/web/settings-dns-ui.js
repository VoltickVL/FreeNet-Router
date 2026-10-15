;(() => {
  'use strict';
  if (window.__freenetSettingsDNSUILoaded) return;
  window.__freenetSettingsDNSUILoaded = true;

  const q = (s, r = document) => r.querySelector(s);
  const state = {data:null, baseline:'', dirty:false, applying:false, saveWrapped:false, flagBusy:false};

  const escapeHTML = value => String(value || '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const provider = (id, options) => (options || []).find(x => x.id === id) || null;
  const formKey = () => JSON.stringify({mode:q('input[name="fnDnsMode"]:checked')?.value || 'firmware',direct:q('#fnDnsDirect')?.value || 'yandex-doh',vpn:q('#fnDnsVPN')?.value || 'google-doh'});

  function injectStyle() {
    if (q('#freenetSettingsDNSUIStyle')) return;
    document.documentElement.dataset.freenetStylesettingsdnsuijs1 = "1";
  }

  function markup() {
    return `<section id="fn3DnsCard" class="fn3-card fn3-dns-card fn3-section-collapsed">
      <div class="fn3-dns-head" role="button" tabindex="0" aria-expanded="false" aria-label="Режим DNS: скрыть или раскрыть"><div class="fn3-dns-title"><span class="fn3-dns-icon" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="6" cy="6" r="2"/><circle cx="18" cy="6" r="2"/><circle cx="12" cy="18" r="2"/><path d="M7.7 7.2 11 16M16.3 7.2 13 16M8 6h8"/></svg></span><div><h2>Режим DNS</h2><p>Выберите, где FreeNet должен разрешать DNS-запросы.</p></div></div><span id="fn3DnsState" class="fn3-dns-state"><i></i><span>Определяем…</span></span></div>
      <div class="fn3-dns-layout">
        <div class="fn3-dns-modes">
          <label class="fn3-dns-mode" data-dns-mode="firmware"><input type="radio" name="fnDnsMode" value="firmware"><span class="fn3-dns-mode-dot"></span><span>Прямой</span></label>
          <label class="fn3-dns-mode" data-dns-mode="xkeen"><input type="radio" name="fnDnsMode" value="xkeen"><span class="fn3-dns-mode-dot"></span><span>Раздельный</span></label>
        </div>
        <div class="fn3-dns-resolvers">
          <article class="fn3-dns-resolver direct"><div class="fn3-dns-resolver-top"><span class="fn3-dns-resolver-label"><span class="fn3-dns-provider-icon">D</span>DIRECT DNS</span><span class="fn3-dns-path">DIRECT</span></div><select id="fnDnsDirect" aria-label="DIRECT DNS provider"></select><span id="fnDnsDirectEndpoint" class="fn3-dns-endpoint">—</span></article>
          <article class="fn3-dns-resolver vpn"><div class="fn3-dns-resolver-top"><span class="fn3-dns-resolver-label"><span class="fn3-dns-provider-icon">V</span>VPN DNS</span><span class="fn3-dns-path">VPN</span></div><select id="fnDnsVPN" aria-label="VPN DNS provider"></select><span id="fnDnsVPNEndpoint" class="fn3-dns-endpoint">—</span></article>
          <div class="fn3-dns-note">По умолчанию FreeNet использует <b>Яндекс DoH</b> для DIRECT и <b>Google DoH</b> для VPN. В режиме «Раздельный» VPN DNS следует через защищённый VPN path.</div>
          <div id="fn3DnsWarning" class="fn3-dns-warning"></div>
          <div id="fn3DnsRepairRow" class="fn3-dns-repair" hidden><button id="fn3DnsRepair" type="button">Восстановить DIRECT</button></div>
        </div>
      </div>
    </section>`;
  }

  function optionHTML(options, selected) {
    return (options || []).map(item => `<option value="${escapeHTML(item.id)}"${item.id===selected?' selected':''}>${escapeHTML(item.label)}</option>`).join('');
  }

  function renderMode() {
    const selected = q('input[name="fnDnsMode"]:checked')?.value || 'firmware';
    document.querySelectorAll('[data-dns-mode]').forEach(node => node.classList.toggle('selected', node.dataset.dnsMode === selected));
    const split = q('[data-dns-mode="xkeen"]');
    const splitInput = split?.querySelector('input');
    const supported = state.data?.split_supported !== false;
    if (split) split.classList.toggle('disabled', !supported);
    if (splitInput) splitInput.disabled = !supported;
  }

  function renderEndpoint() {
    const direct = provider(q('#fnDnsDirect')?.value, state.data?.direct_options);
    const vpn = provider(q('#fnDnsVPN')?.value, state.data?.vpn_options);
    if (q('#fnDnsDirectEndpoint')) q('#fnDnsDirectEndpoint').textContent = direct?.endpoint || '—';
    if (q('#fnDnsVPNEndpoint')) q('#fnDnsVPNEndpoint').textContent = vpn?.endpoint || '—';
  }

  function renderState() {
    const chip = q('#fn3DnsState');
    if (!chip || !state.data) return;
    const mode = state.data.active_mode === 'xkeen' ? 'Раздельный' : 'Прямой';
    const healthy = state.data.active_mode !== 'xkeen' || ((state.data.runtime_state === 'accepted' || state.data.runtime_state === 'legacy') && state.data.direct_egress_state === 'accepted');
    chip.classList.toggle('ok', healthy);
    q('span', chip).textContent = mode;
    const warning = q('#fn3DnsWarning');
    if (warning) warning.textContent = state.data.warning || '';
    renderRepairAction();
  }

  function renderRepairAction() {
    const row = q('#fn3DnsRepairRow'), btn = q('#fn3DnsRepair');
    if (!row || !btn) return;
    const needed = state.data?.repair_required === true;
    row.hidden = !needed;
    btn.disabled = !needed || state.applying || state.data?.apply_supported === false;
    btn.textContent = state.applying ? 'Восстанавливаем DIRECT…' : 'Восстановить DIRECT';
  }

  function syncSaveButton() {
    const btn = q('#fn3Save');
    if (!btn || !state.dirty) return;
    btn.disabled = false;
    const label = q('span', btn);
    if (label) label.textContent = 'Сохранить изменения';
  }

  function markDirty() {
    state.dirty = formKey() !== state.baseline;
    renderMode(); renderEndpoint(); syncSaveButton();
  }

  async function fetchJSON(url, init) {
    const response = await fetch(url, init);
    let data = {};
    try { data = await response.json(); } catch (_) {}
    if (!response.ok || data.success === false) {
      const parts = [data.error || `HTTP ${response.status}`];
      if (data.primary_error) parts.push(`Основная ошибка: ${data.primary_error}`);
      if (data.rollback_state) parts.push(`Откат: ${data.rollback_state}`);
      throw new Error(parts.join(' · '));
    }
    return data;
  }

  async function loadControl() {
    try {
      const data = await fetchJSON('/api/settings-v3/dns/control', {cache:'no-store'});
      state.data = data;
      const mode = data.mode === 'xkeen' ? 'xkeen' : 'firmware';
      const modeInput = q(`input[name="fnDnsMode"][value="${mode}"]`); if (modeInput) modeInput.checked = true;
      const direct = q('#fnDnsDirect'), vpn = q('#fnDnsVPN');
      if (direct) { direct.innerHTML = optionHTML(data.direct_options, data.direct_provider || 'yandex-doh'); direct.value = data.direct_provider || 'yandex-doh'; }
      if (vpn) { vpn.innerHTML = optionHTML(data.vpn_options, data.vpn_provider || 'google-doh'); vpn.value = data.vpn_provider || 'google-doh'; }
      renderMode(); renderEndpoint(); renderState();
      state.baseline = formKey(); state.dirty = false;
    } catch (err) {
      const warning = q('#fn3DnsWarning'); if (warning) warning.textContent = `DNS state недоступен: ${err.message}`;
    }
  }

  async function applyDNS(force = false) {
    if ((!state.dirty && !force) || state.applying) return true;
    if (state.data?.apply_supported === false) {
      alert(state.data.warning || 'Изменение DNS сейчас заблокировано до подтверждения фактического состояния.');
      return false;
    }
    const mode = q('input[name="fnDnsMode"]:checked')?.value || 'firmware';
    const direct = q('#fnDnsDirect')?.value || 'yandex-doh';
    const vpn = q('#fnDnsVPN')?.value || 'google-doh';
    state.applying = true;
    const btn = q('#fn3Save'), label = btn && q('span', btn);
    if (!force) {
      if (btn) btn.disabled = true;
      if (label) label.textContent = 'Применяем DNS…';
    }
    renderRepairAction();
    try {
      const data = await fetchJSON('/api/settings-v3/dns/control', {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({mode,direct_provider:direct,vpn_provider:vpn,confirm:true})});
      state.data = data;
      await loadControl();
      return true;
    } catch (err) {
      alert(`DNS не применён: ${err.message}`);
      if (!force) {
        state.dirty = true;
        syncSaveButton();
      }
      return false;
    } finally {
      state.applying = false;
      renderRepairAction();
    }
  }

  function wrapSave() {
    if (state.saveWrapped) return;
    const btn = q('#fn3Save');
    if (!btn || typeof btn.onclick !== 'function') return;
    const original = btn.onclick;
    btn.onclick = async event => {
      if (!state.dirty) return original.call(btn, event);
      await original.call(btn, event);
      const ok = await applyDNS();
      if (!ok) return;
      const label = q('span', btn);
      if (label && label.textContent === 'Сохранено') btn.disabled = true;
    };
    state.saveWrapped = true;
  }

  function bind() {
    document.querySelectorAll('input[name="fnDnsMode"]').forEach(input => input.addEventListener('change', markDirty));
    q('#fnDnsDirect')?.addEventListener('change', markDirty);
    q('#fnDnsVPN')?.addEventListener('change', markDirty);
    q('#fn3DnsRepair')?.addEventListener('click', async () => { await applyDNS(true); });
  }

  async function enforceCurrentVPNFlag() {
    if (state.flagBusy) return;
    const node = q('#fn3Flag');
    const host = q('#controlCenter');
    if (!node || !host) return;
    state.flagBusy = true;
    try {
      const status = await fetch('/api/status', {cache:'no-store'}).then(r => r.ok ? r.json() : null).catch(() => null);
      let code = String(status?.country_code || node.dataset.countryCode || node.getAttribute('aria-label') || '').trim().toLowerCase();
      if (!/^[a-z]{2}$/.test(code)) {
        const text = String(node.textContent || '').trim();
        if (/^[a-z]{2}$/i.test(text)) code = text.toLowerCase();
      }
      if (!/^[a-z]{2}$/.test(code)) return;
      node.className = `fn3-flag flag-icon flag-${code} fn3-force-flag`;
      node.dataset.countryCode = code;
      node.setAttribute('aria-label', code.toUpperCase());
      node.textContent = '';
      const probe = document.createElement('span');
      probe.className = `flag-icon flag-${code}`;
      probe.style.cssText = 'position:absolute!important;left:-10000px!important;width:30px!important;height:20px!important;';
      host.appendChild(probe);
      const background = getComputedStyle(probe).backgroundImage;
      probe.remove();
      if (background && background !== 'none') node.style.setProperty('background-image', background, 'important');
    } finally {
      state.flagBusy = false;
    }
  }

  function installFlagGuard() {
    const node = q('#fn3Flag');
    if (!node || node.dataset.dnsFlagGuard === '1') return;
    node.dataset.dnsFlagGuard = '1';
    new MutationObserver(() => { if (!state.flagBusy && String(node.textContent || '').trim()) void enforceCurrentVPNFlag(); }).observe(node, {childList:true,characterData:true,subtree:true});
    void enforceCurrentVPNFlag();
  }

  function mount() {
    injectStyle();
    const page = q('[data-page-view="settings"][data-settings-v3="1"]');
    if (!page) return false;
    if (!q('#fn3DnsCard', page)) {
      const grid = q('.fn3-grid', page);
      if (!grid) return false;
      grid.insertAdjacentHTML('beforebegin', markup());
      bind();
      void loadControl();
    }
    wrapSave(); installFlagGuard();
    if (page.classList.contains('active')) void enforceCurrentVPNFlag();
    return true;
  }

  let tries = 0;
  const timer = setInterval(() => { tries++; if (mount() || tries > 80) clearInterval(timer); }, 150);
  const observer = new MutationObserver(() => mount());
  const start = () => { if (document.body) observer.observe(document.body, {childList:true,subtree:true}); mount(); };
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', start, {once:true}); else start();
  window.addEventListener('hashchange', () => { mount(); if (location.hash.slice(1)==='settings') { void loadControl(); void enforceCurrentVPNFlag(); } });
})();