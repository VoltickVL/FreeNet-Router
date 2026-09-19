// VPN picker v2: isolated presentation over the existing exact-connect engine.
// No apply endpoint, credentials, system emoji or second mutation implementation.
(() => {
  'use strict';
  if (window.__freenetVPNPickerV2Mounted) return;
  window.__freenetVPNPickerV2Mounted = true;
  const q = (s, root = document) => root.querySelector(s);
  const text = (node, value) => { if (node && node.textContent !== value) node.textContent = value; };
  const L = {
    choose:'Выбор VPN-сервера', current:'Текущий VPN', connected:'Подключено',
    offline:'Не подключён', unknown:'Статус неизвестен', connect:'Подключиться', reset:'Сбросить', close:'Закрыть',
    search:'Страна, город или адрес сервера', hint:'Сначала проверка, затем подключение',
    idle:'Выберите сервер из списка', unchanged:'Текущий VPN не меняется до нажатия «Подключиться».',
    checking:'Проверяем сервер…', ready:'Проверка пройдена', applying:'Подключаем и проверяем соединение…',
    failed:'Проверка не пройдена', empty:'Список серверов недоступен. Проверьте подписку.',
    noMatch:'Ничего не найдено. Измените запрос.', stale:'Показан последний успешный список.',
    busy:'Другая операция VPN ещё выполняется.', blocked:'Результат нужно подтвердить. Повтор заблокирован.'
  };
  const countries = (() => { try { return new Intl.DisplayNames(['ru'], {type:'region'}); } catch (_) { return null; } })();
  const english = (() => { try { return new Intl.DisplayNames(['en'], {type:'region'}); } catch (_) { return null; } })();
  const clean = value => String(value || '').replace(/^[\u{1F1E6}-\u{1F1FF}]{2}\s*/u, '').replace(/^[A-Za-z]{2}[\s,:]+/, '').replace(/[,\s]+Extra$/i, '').trim();
  const normalize = value => String(value || '').normalize('NFKD').replace(/[\u0300-\u036f]/g,'').replace(/ё/g,'е').toLowerCase().trim();
  function codeOf(profile) {
    const direct = String(profile?.country_code || '').trim().toLowerCase();
    if (/^[a-z]{2}$/.test(direct)) return direct;
    const match = String(profile?.name || profile?.profile_label || '').trim().match(/^([a-z]{2})[\s,:]/i);
    return match ? match[1].toLowerCase() : '';
  }
  function countryName(code, fallback = '') {
    if (/^[a-z]{2}$/.test(code)) return countries?.of(code.toUpperCase()) || fallback || code.toUpperCase();
    return fallback || L.unknown;
  }
  function endpoint(p) {
    if (p?.endpoint) return String(p.endpoint);
    const addr = String(p?.address || '');
    return addr ? (addr.includes(':') ? '[' + addr + ']' : addr) + (p.port ? ':' + p.port : '') : '';
  }
  function runtime() { try { return typeof lastStatus !== 'undefined' ? lastStatus : null; } catch (_) { return null; } }
  function engine() {
    const card = q('#selectedProfileCard'), button = q('#exactConnectBtn');
    let id = '', ready = false, applying = false;
    try { id = String(selectedProviderID || ''); ready = !!providerPlanReady; applying = !!providerApplying; } catch (_) {}
    return {id, ready, applying, button, card, note:card?.querySelector('.selected-note')?.textContent || '', title:card?.querySelector('strong')?.textContent || ''};
  }
  let sourceKey = '', rows = [], selected = null, choosing = false, error = '', sent = false;
  let host, toggle, panel, search, list, footer, statusText, detail, connect, reset, currentName, currentCopy, currentFlag, badge;
  let paintQueued = false, listKey = '', timer = null, observedCard = null, observedButton = null;
  const atlas = new Map();
  function readAtlas() {
    // Reuse the EXACT integrated freenetCanonicalAllFlags SVG data, including
    // emblems. The original stylesheet is scoped to #controlCenter; the body
    // panel receives the same URL inline, rather than falling back to OS glyphs.
    if (atlas.size) return;
    const sheet = q('#freenetCanonicalAllFlags')?.sheet;
    if (!sheet) return;
    for (const rule of Array.from(sheet.cssRules || [])) {
      const code = rule.selectorText?.match(/\.flag-([a-z]{2})(?:\b)/)?.[1];
      const image = rule.style?.backgroundImage;
      if (code && image?.includes('data:image/svg+xml')) atlas.set(code, image);
    }
  }
  function setFlag(node, code) {
    readAtlas();
    const key = atlas.has(code) ? code : '';
    if (node.dataset.country === key) return;
    node.dataset.country = key;
    node.className = 'flag-icon fnv2-flag' + (key ? ' flag-' + key : ' flag-unknown');
    node.dataset.flagSource = key ? 'canonical' : 'unknown';
    node.style.setProperty('background-image', atlas.get(key) || 'none', 'important');
  }
  function profileSource() {
    let profiles = [], stale = false;
    try { profiles = Array.isArray(extraProfiles) ? extraProfiles : []; stale = !!lastNetworkPlan?.profiles_stale; } catch (_) {}
    if (!profiles.length) {
      try { const cache = JSON.parse(localStorage.getItem('freenet-extra-profiles-last-good-v1') || '{}'); profiles = Array.isArray(cache.profiles) ? cache.profiles : []; stale = profiles.length > 0; } catch (_) {}
    }
    const safe = profiles.filter(p => p && typeof p.id === 'string' && p.id && codeOf(p) !== 'ru').slice(0,100).map(p => ({id:p.id,name:String(p.name || p.label || ''),country_code:codeOf(p),endpoint:endpoint(p),address:String(p.address || ''),port:Number(p.port || 0)}));
    const key = JSON.stringify([safe, stale]);
    if (key !== sourceKey) { sourceKey = key; rows = safe; listKey = ''; }
    return {stale};
  }
  function currentIdentity(s) {
    const labelCode = codeOf({profile_label:s?.profile_label});
    const match = rows.find(p => s?.endpoint && p.endpoint === s.endpoint);
    let code = codeOf(s) || labelCode || codeOf(match);
    // Overview is a fallback only for the SAME confirmed endpoint, never for a
    // candidate being measured or a selected-but-not-connected server.
    if (!code && s?.endpoint && q('#bestCurrentEndpoint')?.textContent.trim() === s.endpoint) {
      code = q('#bestCurrentFlag')?.className.match(/(?:^|\s)flag-([a-z]{2})(?:\s|$)/)?.[1] || '';
    }
    return {code, country:countryName(code, s?.country), label:clean(s?.profile_label || match?.name || '')};
  }
  function mountStyles() {
    if (q('#freenetVPNPickerV2Styles')) return;
    const style = document.createElement('style'); style.id = 'freenetVPNPickerV2Styles';
    style.textContent = `
      html body #bestServerAdvanced,html body #exactConnectRow,html body #profilesList{display:none!important}
      #fnVpnPickerV2Host{flex:0 0 auto;min-width:0;margin:0}
      #fnVpnPickerV2Toggle{appearance:none;display:grid;grid-template-columns:22px minmax(0,1fr);gap:9px;align-items:center;text-align:left;width:157px;height:50px;padding:6px 11px;border:1px solid #315276;border-radius:11px;background:linear-gradient(180deg,#0d1d30,#0a1727);color:#f3f7ff;font:inherit;cursor:pointer}
      #fnVpnPickerV2Toggle:hover,#fnVpnPickerV2Toggle[aria-expanded=true]{border-color:#6597d9;background:#10243c}
      #fnVpnPickerV2Toggle svg{width:22px;height:22px;color:#72a8ff}
      #fnVpnPickerV2Toggle .fnv2-chip{display:grid;gap:3px;min-width:0}
      #fnVpnPickerV2Toggle small{font-weight:750;color:#8da4c2;font-size:9px;line-height:1.1}
      #fnVpnPickerV2Toggle .fnv2-chip-value{display:flex;align-items:center;gap:6px;min-width:0;font-size:12px;line-height:1.25;font-weight:750}
      #fnVpnPickerV2Country{white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
      .fnv2-flag{position:relative;display:inline-block;flex:0 0 auto;width:27px;height:18px;border:1px solid #ffffff24;border-radius:3px;background-color:#24344c!important;background-size:100% 100%!important;background-repeat:no-repeat!important;overflow:hidden;box-shadow:none!important}
      .fnv2-flag::before,.fnv2-flag::after{display:none!important;content:none!important}
      #fnVpnPickerV2Toggle .fnv2-flag{width:20px;height:14px}
      #fnVpnPickerV2Panel{position:fixed;z-index:2600;box-sizing:border-box;display:flex;flex-direction:column;gap:0;width:540px;max-width:calc(100vw - 24px);max-height:var(--fnv2-space,680px);margin:0;padding:0;color:#eef4ff;background:#0c1c2e;border:1px solid #355473;border-radius:16px;box-shadow:0 24px 70px #0009;font-family:Inter,ui-sans-serif,system-ui,sans-serif;overflow:hidden}
      #fnVpnPickerV2Panel[hidden]{display:none!important}
      #fnVpnPickerV2Panel *{box-sizing:border-box}
      #fnVpnPickerV2Panel button,#fnVpnPickerV2Panel input{font:inherit}
      #fnVpnPickerV2Panel button:focus-visible,#fnVpnPickerV2Toggle:focus-visible{outline:2px solid #80adff;outline-offset:2px}
      #fnVpnPickerV2Panel .fnv2-head{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:15px 16px;border-bottom:1px solid #28415b;flex:none}
      #fnVpnPickerV2Panel h2{font-size:17px;line-height:1.3;margin:0;font-weight:750}
      #fnVpnPickerV2Panel .fnv2-subtitle{font-size:11px;line-height:1.4;color:#92a8c3;margin:3px 0 0}
      #fnVpnPickerV2Close{appearance:none;flex:none;width:30px;height:30px;border:1px solid #365674;border-radius:8px;background:#11273e;color:#b3c5da;cursor:pointer;font-size:20px!important}
      #fnVpnPickerV2Panel .fnv2-current{flex:none;display:flex;align-items:center;gap:10px;padding:12px 16px}
      #fnVpnPickerV2Panel .fnv2-current>.fnv2-flag{width:34px;height:23px}
      #fnVpnPickerV2Panel .fnv2-current-copy{display:grid;gap:2px;min-width:0;flex:1}
      #fnVpnPickerV2Panel .fnv2-current-copy small{font-size:10px;color:#8ea6c1}
      #fnVpnPickerV2Panel .fnv2-current-copy strong{font-size:13px;line-height:1.4}
      #fnVpnPickerV2Panel .fnv2-current-copy span{font-size:11px;color:#90a7c2;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
      #fnVpnPickerV2State{font-size:10px;line-height:1.5;padding:3px 7px;border:1px solid #35516e;border-radius:999px;color:#a6b8cd;white-space:nowrap}
      #fnVpnPickerV2State[data-online=true]{color:#87e5b6;border-color:#23835f;background:#103b32}
      #fnVpnPickerV2Panel .fnv2-search-wrap{position:relative;padding:0 12px 9px;flex:none}
      #fnVpnPickerV2Search{width:100%;height:41px;border:1px solid #355473;border-radius:10px;background:#081727;color:#f2f7ff;padding:0 12px;outline:none;font-size:12px!important}
      #fnVpnPickerV2Search:focus{border-color:#79a8f7;box-shadow:0 0 0 2px #4b7bc426}
      #fnVpnPickerV2Results{margin:0 12px;padding:4px;min-height:70px;max-height:280px;flex:1 1 280px;overflow-x:hidden;overflow-y:auto;overscroll-behavior:contain;border:1px solid #243f59;border-radius:10px;background:#081727}
      #fnVpnPickerV2Results .fnv2-option{appearance:none;width:100%;min-height:48px;display:flex;align-items:center;gap:10px;padding:8px 10px;border:1px solid transparent;border-radius:8px;background:none;color:#eef4ff;text-align:left;cursor:pointer}
      #fnVpnPickerV2Results .fnv2-option:hover,#fnVpnPickerV2Results .fnv2-option[aria-selected=true]{background:#132d49;border-color:#345b87}
      #fnVpnPickerV2Results .fnv2-option:disabled{opacity:.55;cursor:wait}
      #fnVpnPickerV2Results .fnv2-option-copy{display:grid;gap:3px;min-width:0;flex:1}
      #fnVpnPickerV2Results .fnv2-option-copy strong{font-size:12px;line-height:1.35;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
      #fnVpnPickerV2Results .fnv2-option-copy small{font-size:10px;line-height:1.2;color:#92a8c3;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
      #fnVpnPickerV2Results .fnv2-empty{font-size:12px;line-height:1.6;color:#a3b7d1;padding:17px 12px}
      #fnVpnPickerV2Stale{margin:5px 16px 0;font-size:10px;color:#dcc28b;flex:none}
      #fnVpnPickerV2Stale[hidden]{display:none}
      #fnVpnPickerV2Footer{flex:none;padding:11px 12px 12px;display:grid;gap:9px;border-top:1px solid #20364c;margin-top:10px;background:#0c1c2e}
      #fnVpnPickerV2Footer .fnv2-validation{min-height:53px;display:grid;gap:4px;padding:8px 10px;border-left:3px solid #4b6382;border-radius:6px;background:#0a1726}
      #fnVpnPickerV2Footer[data-state=ready] .fnv2-validation,#fnVpnPickerV2Footer[data-state=success] .fnv2-validation{border-left-color:#43d795;background:#0e302d}
      #fnVpnPickerV2Footer[data-state=error] .fnv2-validation{border-left-color:#f08089;background:#2e202e}
      #fnVpnPickerV2Footer[data-state=checking] .fnv2-validation,#fnVpnPickerV2Footer[data-state=applying] .fnv2-validation{border-left-color:#72a8ff}
      #fnVpnPickerV2Status{font-size:12px;line-height:1.5;font-weight:700;overflow-wrap:anywhere}
      #fnVpnPickerV2Detail{font-size:11px;line-height:1.5;color:#a1b4cc;overflow-wrap:anywhere}
      #fnVpnPickerV2Footer .fnv2-actions{display:grid;grid-template-columns:minmax(0,1fr) 115px;gap:8px}
      #fnVpnPickerV2Footer button{height:41px;appearance:none;border:1px solid #3b638c;border-radius:9px;color:#edf5ff;background:#12283f;cursor:pointer;font-size:12px;font-weight:750}
      #fnVpnPickerV2Connect{background:linear-gradient(180deg,#347eff,#2465dc)!important;border-color:#6d9ee9!important}
      #fnVpnPickerV2Footer button:disabled{opacity:.45;cursor:not-allowed}
      @media(max-width:760px){#fnVpnPickerV2Panel{left:12px!important;right:12px!important;bottom:12px!important;top:auto!important;width:auto;max-height:calc(100dvh - 24px)}#fnVpnPickerV2Toggle{width:146px}#fnVpnPickerV2Results{max-height:32dvh}#fnVpnPickerV2Panel .fnv2-subtitle{max-width:280px}}
      @media(max-height:580px){#fnVpnPickerV2Panel .fnv2-current{padding-top:6px;padding-bottom:6px}#fnVpnPickerV2Panel .fnv2-current-copy span{display:none}#fnVpnPickerV2Results{min-height:55px}#fnVpnPickerV2Footer{padding-top:6px;gap:5px;margin-top:5px}}
    `;
    document.head.appendChild(style);
  }
  function mount() {
    if (host) return;
    const summary = q('#overviewApprovedTop');
    if (!summary) return;
    mountStyles();
    host = document.createElement('div'); host.id = 'fnVpnPickerV2Host';
    host.innerHTML = '<button id="fnVpnPickerV2Toggle" type="button" aria-haspopup="dialog" aria-controls="fnVpnPickerV2Panel" aria-expanded="false"><svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c5 5 5 13 0 18-5-5-5-13 0-18Z"/></svg><span class="fnv2-chip"><small>VPN</small><span class="fnv2-chip-value"><span id="fnVpnPickerV2Flag" class="fnv2-flag" aria-hidden="true"></span><span id="fnVpnPickerV2Country"></span></span></span></button>';
    summary.appendChild(host); toggle = q('button',host);
    panel = document.createElement('section'); panel.id = 'fnVpnPickerV2Panel'; panel.hidden = true;
    panel.setAttribute('role','dialog'); panel.setAttribute('aria-labelledby','fnVpnPickerV2Title');
    panel.innerHTML = '<header class="fnv2-head"><div><h2 id="fnVpnPickerV2Title"></h2><p class="fnv2-subtitle"></p></div><button id="fnVpnPickerV2Close" type="button">&times;</button></header><div class="fnv2-current"><span id="fnVpnPickerV2CurrentFlag" class="fnv2-flag" aria-hidden="true"></span><div class="fnv2-current-copy"><small></small><strong></strong><span></span></div><span id="fnVpnPickerV2State"></span></div><div class="fnv2-search-wrap"><input id="fnVpnPickerV2Search" type="search" autocomplete="off" spellcheck="false"></div><div id="fnVpnPickerV2Results" role="listbox"></div><p id="fnVpnPickerV2Stale" hidden></p><footer id="fnVpnPickerV2Footer"><div class="fnv2-validation" role="status" aria-live="polite"><strong id="fnVpnPickerV2Status"></strong><span id="fnVpnPickerV2Detail"></span></div><div class="fnv2-actions"><button id="fnVpnPickerV2Connect" type="button" disabled></button><button id="fnVpnPickerV2Reset" type="button" disabled></button></div></footer>';
    document.body.appendChild(panel);
    text(q('h2',panel),L.choose); text(q('.fnv2-subtitle',panel),L.hint);
    text(q('.fnv2-current-copy small',panel),L.current);
    q('#fnVpnPickerV2Close').setAttribute('aria-label',L.close);
    search = q('input',panel); search.placeholder = L.search; search.setAttribute('aria-label',L.search);
    list = q('#fnVpnPickerV2Results'); list.setAttribute('aria-label',L.choose);
    footer = q('footer',panel); statusText = q('#fnVpnPickerV2Status'); detail = q('#fnVpnPickerV2Detail');
    connect = q('#fnVpnPickerV2Connect'); reset = q('#fnVpnPickerV2Reset'); text(connect,L.connect); text(reset,L.reset);
    currentName = q('.fnv2-current-copy strong',panel); currentCopy = q('.fnv2-current-copy span',panel); currentFlag = q('#fnVpnPickerV2CurrentFlag'); badge = q('#fnVpnPickerV2State');
    toggle.addEventListener('click',() => panel.hidden ? open() : close(true));
    q('#fnVpnPickerV2Close').addEventListener('click',() => close(true));
    search.addEventListener('input',() => { listKey=''; paint(); });
    list.addEventListener('keydown',event => {
      if (!['ArrowDown','ArrowUp','Home','End'].includes(event.key)) return;
      const options = Array.from(list.querySelectorAll('button:not(:disabled)'));
      if (!options.length) return;
      const index = options.indexOf(document.activeElement);
      const next = event.key==='Home'?0:event.key==='End'?options.length-1:(index+(event.key==='ArrowDown'?1:-1)+options.length)%options.length;
      event.preventDefault(); options[next].focus();
    });
    search.addEventListener('keydown',event => { if (event.key==='ArrowDown') { event.preventDefault(); list.querySelector('button:not(:disabled)')?.focus(); } });
    connect.addEventListener('click',() => {
      const e = engine();
      if (!canConnect(e)) return;
      sent = true; error=''; connect.disabled = true;
      e.button.click(); schedulePaint();
    });
    reset.addEventListener('click',() => {
      if (choosing || engine().applying) return;
      q('#exactCancelBtn')?.click(); selected=null; sent=false; error=''; listKey=''; paint();
    });
  }
  function orderTopbar() {
    const summary = q('#overviewApprovedTop'), xray = q('.fn-xray-topbar'), freenet = q('#topFreenetUpdate');
    const dns = summary && Array.from(summary.querySelectorAll('.overview-approved-fact')).find(n => /DNS/i.test(n.textContent || ''));
    if (!summary || !host || !xray || !dns || !freenet) return;
    const nodes = [xray,host,dns,freenet];
    if (nodes.some((n,i)=>summary.children[i]!==n)) nodes.forEach(n=>summary.appendChild(n));
    summary.dataset.vpnOrder = 'xray-vpn-dns-freenet';
  }
  function busy() { const s=runtime(); return choosing || engine().applying || !!(s?.busy || s?.updater_busy); }
  function uncertain(e) { return /ROLLBACK_FAILED|ROLLBACK_UNKNOWN|FAILED_UNKNOWN|\bUNKNOWN\b/.test(e.note + ' ' + e.title); }
  function canConnect(e=engine()) { return !!(selected && selected.id===e.id && e.ready && e.card?.classList.contains('is-ready') && e.button && !e.button.disabled && !sent && !busy() && !uncertain(e)); }
  async function choose(profile) {
    if (busy() || uncertain(engine()) || typeof selectProviderProfile !== 'function') return;
    selected=profile; choosing=true; sent=false; error=''; listKey=''; paint();
    try { await selectProviderProfile(profile); } catch (_) { error=L.failed; }
    finally { choosing=false; paint(); }
  }
  function renderList() {
    const disabled=busy() || uncertain(engine());
    const key=JSON.stringify([sourceKey,search.value,selected?.id,disabled]);
    if (key===listKey) return;
    listKey=key;
    const query=normalize(search.value);
    const filtered=rows.filter(p=> {
      const code=p.country_code;
      if (/^[a-z]{2}$/.test(query)) return code===query;
      return !query || normalize([p.name,p.endpoint,countryName(code),code,english?.of(code.toUpperCase() || 'ZZ') || ''].join(' ')).includes(query);
    });
    const fragment=document.createDocumentFragment();
    for (const p of filtered) {
      const button=document.createElement('button'); button.type='button'; button.className='fnv2-option'; button.dataset.profileId=p.id;
      button.setAttribute('role','option'); button.setAttribute('aria-selected',String(selected?.id===p.id)); button.disabled=disabled;
      const flag=document.createElement('span'); flag.setAttribute('aria-hidden','true'); setFlag(flag,p.country_code);
      const copy=document.createElement('span'); copy.className='fnv2-option-copy';
      const title=document.createElement('strong'); title.textContent=clean(p.name) || countryName(p.country_code);
      const meta=document.createElement('small'); meta.textContent=p.endpoint;
      copy.append(title,meta); button.append(flag,copy); button.addEventListener('click',()=>choose(p)); fragment.appendChild(button);
    }
    if (!filtered.length) { const empty=document.createElement('div'); empty.className='fnv2-empty'; empty.textContent=rows.length?L.noMatch:L.empty; fragment.appendChild(empty); }
    const focused=document.activeElement?.dataset?.profileId;
    list.replaceChildren(fragment);
    if (focused) Array.from(list.querySelectorAll('button')).find(n=>n.dataset.profileId===focused)?.focus({preventScroll:true});
  }
  function observeEngine() {
    const e=engine();
    if (e.card===observedCard && e.button===observedButton) return;
    observer.disconnect(); observedCard=e.card; observedButton=e.button;
    if (e.card) observer.observe(e.card,{childList:true,subtree:true,attributes:true,attributeFilter:['class','hidden']});
    if (e.button) observer.observe(e.button,{attributes:true,attributeFilter:['disabled']});
  }
  const observer=new MutationObserver(()=>schedulePaint());
  function paint() {
    if (document.hidden) return;
    mount(); if (!host) return;
    orderTopbar(); observeEngine();
    const {stale}=profileSource(), s=runtime(), id=currentIdentity(s), e=engine();
    const online=s?.xray_online===true;
    const name=s?.xray_online===false?L.offline:id.country;
    text(q('#fnVpnPickerV2Country'),name); toggle.setAttribute('aria-label','VPN: '+name);
    setFlag(q('#fnVpnPickerV2Flag'),online?id.code:''); setFlag(currentFlag,online?id.code:'');
    text(currentName,id.country); text(currentCopy,id.label); text(badge,online?L.connected:s?.xray_online===false?L.offline:L.unknown); badge.dataset.online=String(online);
    let state='idle', title=L.idle, note=L.unchanged;
    if (selected) {
      if (choosing || e.card?.classList.contains('is-checking')) {state='checking';title=L.checking;note=clean(selected.name);}
      else if (e.applying) {state='applying';title=L.applying;note=clean(selected.name);}
      else if (sent && !e.id && online && s.endpoint===selected.endpoint) {state='success';title=L.connected;note=clean(selected.name);}
      else if (error || e.card?.classList.contains('is-error')) {state='error';title=uncertain(e)?L.blocked:L.failed;note=error || e.note || e.title;}
      else if (canConnect(e)) {state='ready';title=L.ready;note=clean(selected.name);}
      else if (sent) {state='applying';title=L.applying;note=e.note || L.blocked;}
    } else if (busy()) {title=L.busy;}
    footer.dataset.state=state; text(statusText,title); text(detail,note);
    connect.disabled=!canConnect(e); reset.disabled=!selected || busy() || uncertain(e);
    text(q('#fnVpnPickerV2Stale'),L.stale); q('#fnVpnPickerV2Stale').hidden=!stale;
    if (!panel.hidden) { renderList(); positionPanel(); }
  }
  function schedulePaint() { if (paintQueued) return; paintQueued=true; requestAnimationFrame(()=>{paintQueued=false;paint();}); }
  function positionPanel() {
    if (!panel || panel.hidden) return;
    const vw=window.visualViewport?.width || window.innerWidth, vh=window.visualViewport?.height || window.innerHeight;
    if (vw<=760) { panel.style.setProperty('--fnv2-space',Math.max(260,vh-24)+'px'); return; }
    const r=toggle.getBoundingClientRect(), width=Math.min(540,vw-24);
    panel.style.left=Math.max(12,Math.min(vw-width-12,r.right-width))+'px';
    const below=vh-r.bottom-22, above=r.top-22;
    const top=below>=340 || below>=above ? r.bottom+10 : 12;
    panel.style.top=top+'px'; panel.style.bottom='auto';
    panel.style.setProperty('--fnv2-space',Math.max(240,vh-top-12)+'px');
  }
  function open() { panel.hidden=false; toggle.setAttribute('aria-expanded','true'); listKey=''; paint(); search.focus({preventScroll:true}); }
  function close(restore=false) { if (!panel) return; panel.hidden=true; toggle.setAttribute('aria-expanded','false'); if (restore) toggle.focus({preventScroll:true}); }
  function boot() {
    paint(); timer=setInterval(schedulePaint,1000); // Reads shared state only; no extra requests.
    document.addEventListener('visibilitychange',schedulePaint);
    document.addEventListener('freenet:controls-busy',schedulePaint);
    document.addEventListener('freenet:status-updated',schedulePaint);
    document.addEventListener('freenet:settings-v3-updated',schedulePaint);
    document.addEventListener('pointerdown',e=>{if(panel && !panel.hidden && !panel.contains(e.target) && !host.contains(e.target))close();},true);
    document.addEventListener('keydown',e=>{if(e.key==='Escape' && panel && !panel.hidden){e.preventDefault();close(true);}});
    window.addEventListener('resize',positionPanel); window.addEventListener('scroll',positionPanel,true);
    window.visualViewport?.addEventListener('resize',positionPanel);
    window.addEventListener('pagehide',()=>{clearInterval(timer);observer.disconnect();},{once:true});
    window.addEventListener('pageshow',e=>{if(e.persisted){clearInterval(timer);timer=setInterval(schedulePaint,1000);observedCard=null;observedButton=null;schedulePaint();}});
  }
  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',boot,{once:true});else boot();
})();
