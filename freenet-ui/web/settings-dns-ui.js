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
    const style = document.createElement('style');
    style.id = 'freenetSettingsDNSUIStyle';
    style.textContent = `
      body.fn-settings-accepted .fn3-dns-card{grid-column:1/-1;margin-bottom:14px;padding:15px 17px!important;border-color:#28577e!important;background:linear-gradient(180deg,rgba(10,35,60,.99),rgba(7,25,44,.99))!important}
      .fn3-dns-head{display:flex;align-items:flex-start;justify-content:space-between;gap:14px;margin-bottom:12px}.fn3-dns-title{display:flex;align-items:center;gap:11px}.fn3-dns-icon{display:grid;place-items:center;width:36px;height:36px;flex:0 0 36px;border-radius:10px;background:linear-gradient(180deg,#1763d0,#114692);color:#c7deff;font-weight:900;font-size:15px}.fn3-dns-title h2{margin:0;font-size:19px}.fn3-dns-title p{margin:3px 0 0;color:#8fa6c0;font-size:12px}.fn3-dns-state{display:flex;align-items:center;gap:7px;padding:5px 10px;border:1px solid #28577e;border-radius:999px;background:#071b30;color:#a9bed6;font-size:11px;white-space:nowrap}.fn3-dns-state.ok{border-color:#18785c;color:#54e4aa;background:#073a2f}.fn3-dns-state i{width:7px;height:7px;border-radius:50%;background:currentColor}
      .fn3-dns-layout{display:grid;grid-template-columns:minmax(280px,.72fr) minmax(0,1.55fr);gap:12px}.fn3-dns-modes{display:grid;grid-template-columns:1fr 1fr;gap:8px;padding:5px;border:1px solid #294f70;border-radius:12px;background:#07192a;align-content:start}.fn3-dns-mode{position:relative;display:flex;align-items:center;justify-content:center;gap:8px;min-height:50px;padding:9px 12px;border:1px solid transparent;border-radius:9px;color:#a9bed5;font-size:12px;font-weight:800;cursor:pointer;text-align:center}.fn3-dns-mode input{position:absolute;opacity:0;pointer-events:none}.fn3-dns-mode.selected{border-color:#2f8cf8;background:linear-gradient(180deg,#0f3a67,#0b2c50);color:#f3f8ff;box-shadow:inset 0 0 0 1px rgba(70,157,255,.18)}.fn3-dns-mode.disabled{opacity:.45;cursor:not-allowed}.fn3-dns-mode-dot{width:16px;height:16px;border:2px solid #62788f;border-radius:50%;box-sizing:border-box}.fn3-dns-mode.selected .fn3-dns-mode-dot{border:5px solid #38d9a0;background:#fff}
      .fn3-dns-resolvers{display:grid;grid-template-columns:1fr 1fr;gap:10px}.fn3-dns-resolver{position:relative;border:1px solid #2b5579;border-radius:11px;background:linear-gradient(180deg,#09243e,#071b30);padding:11px 12px;min-width:0}.fn3-dns-resolver.direct{box-shadow:inset 3px 0 0 #f2cf39}.fn3-dns-resolver.vpn{box-shadow:inset 3px 0 0 #4b9cff}.fn3-dns-resolver-top{display:flex;align-items:center;justify-content:space-between;gap:10px}.fn3-dns-resolver-label{display:flex;align-items:center;gap:8px;color:#f0f6ff;font-size:12px;font-weight:850}.fn3-dns-provider-icon{display:grid;place-items:center;width:24px;height:24px;border-radius:7px;background:#102f4d;color:#dbeaff;font-size:12px;font-weight:900}.fn3-dns-resolver select{width:100%;height:34px;margin-top:8px;border:1px solid #315b80;border-radius:8px;background:#081a2c;color:#eef6ff;padding:0 30px 0 10px;font-size:12px}.fn3-dns-endpoint{display:block;margin-top:7px;color:#7996b5;font-size:10.5px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.fn3-dns-path{font-size:9.5px;padding:3px 6px;border-radius:999px;background:#0d3155;color:#8fc3ff;white-space:nowrap}.fn3-dns-note{grid-column:1/-1;margin-top:0;padding:8px 10px;border:1px solid #244b6d;border-radius:9px;background:rgba(15,54,91,.35);color:#a9bfd6;font-size:11px;line-height:1.4}.fn3-dns-warning{grid-column:1/-1;color:#f2c56c;font-size:10.5px;min-height:0}.fn3-dns-warning:empty{display:none}
      #fn3Flag.fn3-force-flag{display:inline-block!important;width:28px!important;height:19px!important;min-width:28px!important;background-repeat:no-repeat!important;background-position:center!important;background-size:100% 100%!important;color:transparent!important;font-size:0!important;line-height:0!important;border-radius:4px!important;overflow:hidden!important}
      @media(max-width:1120px){.fn3-dns-layout{grid-template-columns:1fr}.fn3-dns-resolvers{grid-template-columns:1fr 1fr}}
      @media(max-width:720px){.fn3-dns-resolvers,.fn3-dns-modes{grid-template-columns:1fr}}
    `;
    document.head.appendChild(style);
  }

  function markup() {
    return `<section id="fn3DnsCard" class="fn3-card fn3-dns-card">
      <div class="fn3-dns-head"><div class="fn3-dns-title"><span class="fn3-dns-icon">DNS</span><div><h2>Режим DNS</h2><p>Выберите, где FreeNet должен разрешать DNS-запросы.</p></div></div><span id="fn3DnsState" class="fn3-dns-state"><i></i><span>Определяем…</span></span></div>
      <div class="fn3-dns-layout">
        <div class="fn3-dns-modes">
          <label class="fn3-dns-mode" data-dns-mode="firmware"><input type="radio" name="fnDnsMode" value="firmware"><span class="fn3-dns-mode-dot"></span><span>DNS через роутер</span></label>
          <label class="fn3-dns-mode" data-dns-mode="xkeen"><input type="radio" name="fnDnsMode" value="xkeen"><span class="fn3-dns-mode-dot"></span><span>Раздельный DNS</span></label>
        </div>
        <div class="fn3-dns-resolvers">
          <article class="fn3-dns-resolver direct"><div class="fn3-dns-resolver-top"><span class="fn3-dns-resolver-label"><span class="fn3-dns-provider-icon">D</span>DIRECT DNS</span><span class="fn3-dns-path">DIRECT</span></div><select id="fnDnsDirect" aria-label="DIRECT DNS provider"></select><span id="fnDnsDirectEndpoint" class="fn3-dns-endpoint">—</span></article>
          <article class="fn3-dns-resolver vpn"><div class="fn3-dns-resolver-top"><span class="fn3-dns-resolver-label"><span class="fn3-dns-provider-icon">V</span>VPN DNS</span><span class="fn3-dns-path">VPN</span></div><select id="fnDnsVPN" aria-label="VPN DNS provider"></select><span id="fnDnsVPNEndpoint" class="fn3-dns-endpoint">—</span></article>
          <div class="fn3-dns-note">По умолчанию FreeNet использует <b>Яндекс DoH</b> для DIRECT и <b>Google DoH</b> для VPN. В режиме «Раздельный DNS» VPN DNS следует через защищённый VPN path.</div>
          <div id="fn3DnsWarning" class="fn3-dns-warning"></div>
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
    const mode = state.data.active_mode === 'xkeen' ? 'Раздельный DNS' : 'DNS через роутер';
    const healthy = state.data.active_mode !== 'xkeen' || state.data.runtime_state === 'accepted' || state.data.runtime_state === 'legacy';
    chip.classList.toggle('ok', healthy);
    q('span', chip).textContent = mode;
    const warning = q('#fn3DnsWarning');
    if (warning) warning.textContent = state.data.warning || '';
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

  async function applyDNS() {
    if (!state.dirty || state.applying) return true;
    if (state.data?.apply_supported === false) {
      alert(state.data.warning || 'Изменение DNS сейчас заблокировано до подтверждения фактического состояния.');
      return false;
    }
    const mode = q('input[name="fnDnsMode"]:checked')?.value || 'firmware';
    const direct = q('#fnDnsDirect')?.value || 'yandex-doh';
    const vpn = q('#fnDnsVPN')?.value || 'google-doh';
    state.applying = true;
    const btn = q('#fn3Save'), label = btn && q('span', btn);
    if (btn) btn.disabled = true;
    if (label) label.textContent = 'Применяем DNS…';
    try {
      const data = await fetchJSON('/api/settings-v3/dns/control', {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({mode,direct_provider:direct,vpn_provider:vpn,confirm:true})});
      state.data = data;
      await loadControl();
      return true;
    } catch (err) {
      alert(`DNS не применён: ${err.message}`);
      state.dirty = true;
      syncSaveButton();
      return false;
    } finally {
      state.applying = false;
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
