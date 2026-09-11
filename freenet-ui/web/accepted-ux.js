(() => {
  const qs = (selector, root = document) => root.querySelector(selector);
  const wait = ms => new Promise(resolve => setTimeout(resolve, ms));
  let updatePlan = null;
  let updatePolling = false;
  let authFetchWrapped = false;
  let subscriptionMounted = false;

  const visibilityRule = '#authSection[hidden],#controlCenter[hidden]{display:none!important}';
  if (!qs('#freenetVisibilityGuard')) {
    const visibilityGuard = document.createElement('style');
    visibilityGuard.id = 'freenetVisibilityGuard';
    visibilityGuard.textContent = visibilityRule;
    document.head.appendChild(visibilityGuard);
  }

  const shellIconPaths = {
    provider: '<ellipse cx="12" cy="6" rx="7.5" ry="3"/><path d="M4.5 6v6c0 1.7 3.4 3 7.5 3s7.5-1.3 7.5-3V6"/><path d="M4.5 12v6c0 1.7 3.4 3 7.5 3s7.5-1.3 7.5-3v-6"/>',
    dns: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c2.4 2.5 3.6 5.5 3.6 9S14.4 18.5 12 21M12 3c-2.4-2.5-3.6-5.5-3.6-9S9.6 18.5 12 21"/>',
    overview: '<path d="M4 10.5 12 4l8 6.5v8a1.5 1.5 0 0 1-1.5 1.5h-13A1.5 1.5 0 0 1 4 18.5v-8Z"/><path d="M9.5 20v-6h5v6"/>',
    subscription: '<circle cx="12" cy="12" r="8.5"/><circle cx="12" cy="12" r="4.5"/><path d="M12 3.5v2M20.5 12h-2M12 20.5v-2M3.5 12h2"/>',
    network: '<path d="M4 8h14m0 0-3-3m3 3-3 3M20 16H6m0 0 3-3m-3 3 3 3"/>',
    automation: '<path d="M5 12a7 7 0 0 1 12-4.9L19 9"/><path d="M19 5v4h-4M19 12a7 7 0 0 1-12 4.9L5 15"/><path d="M5 19v-4h4"/>',
    access: '<path d="M12 3 19 6v5c0 4.5-3 7.7-7 10-4-2.3-7-5.5-7-10V6l7-3Z"/><path d="m8.8 12.2 2 2 4.4-4.4"/>',
    vpn: '<path d="M5 8.5h14M7.5 4.5h9M7.5 12.5h9M5 16.5h14M9 20.5h6"/>',
    system: '<rect x="4" y="4" width="16" height="16" rx="3"/><path d="M8 9h8M8 13h8M8 17h5"/>',
    profiles: '<path d="m12 3 8 4-8 4-8-4 8-4Z"/><path d="m4 12 8 4 8-4M4 17l8 4 8-4"/>',
    refresh: '<path d="M20 11a8 8 0 1 0-2.3 5.7"/><path d="M20 5v6h-6"/>',
    clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
    key: '<path d="M9.5 14.5 6 18v2h2l1.5-1.5L11 20l2-2-1.5-1.5 2.2-2.2"/><circle cx="16.5" cy="8.5" r="4.5"/>',
    history: '<path d="M8 6h11M8 12h11M8 18h11"/><circle cx="4" cy="6" r="1"/><circle cx="4" cy="12" r="1"/><circle cx="4" cy="18" r="1"/>',
    info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v6M12 7h.01"/>',
    shield: '<path d="M12 3 19 6v5c0 4.5-3 7.7-7 10-4-2.3-7-5.5-7-10V6l7-3Z"/><path d="m8.8 12.2 2 2 4.4-4.4"/>'
  };

  function shellSVG(name) {
    const paths = shellIconPaths[name] || shellIconPaths.overview;
    return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths}</svg>`;
  }

  function injectStyles() {
    if (qs('#freenetAcceptedUXStyles')) return;
    const style = document.createElement('style');
    style.id = 'freenetAcceptedUXStyles';
    style.textContent = `
      :root{--fn-brand-mark:url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='black' stroke-width='1.8' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='M12 2.8 21.2 12 12 21.2 2.8 12 12 2.8Z'/%3E%3Cpath d='M8.4 15.8V8.2l7.2 7.6V8.2'/%3E%3C/svg%3E")}
      .auth-wrap{position:fixed!important;inset:0!important;max-width:none!important;margin:0!important;display:grid!important;place-items:center!important;padding:24px!important;background:radial-gradient(circle at 68% -15%,#17345c 0,#0d1d32 42%,#081523 72%,#07101b 100%)!important;backdrop-filter:none!important}
      .auth-card{width:min(440px,calc(100vw - 32px));background:linear-gradient(180deg,#112137,#0d1a2c)!important}
      .fn-remember{display:flex;align-items:center;gap:9px;margin:11px 2px 0;color:#b5c4d9;font-size:12.5px;cursor:pointer;user-select:none}
      .fn-remember input{width:16px;height:16px;accent-color:#4b85ff;cursor:pointer}
      #topXkeenLink{display:none!important}
      .topbar.overview-approved .top-actions{display:none!important}
      .overview-approved-top.fn-shell-summary{margin-left:auto!important;display:flex!important;align-items:center!important;gap:9px!important}
      .overview-approved-fact.fn-shell-fact,#topFreenetUpdate{height:50px!important;min-height:50px!important;box-sizing:border-box!important;border:1px solid #315276!important;border-radius:11px!important;background:linear-gradient(180deg,#0d1d30,#0a1727)!important;color:#f5f8ff!important;box-shadow:inset 0 1px rgba(255,255,255,.018)!important;transition:border-color .14s ease,background .14s ease,box-shadow .14s ease!important}
      .overview-approved-fact.fn-shell-fact{display:flex!important;align-items:center!important;gap:10px!important;padding:6px 12px!important;min-width:128px}
      .overview-approved-fact.fn-shell-fact:nth-child(2){min-width:166px}
      .fn-shell-fact>.fn-top-fact-icon,.fn-version-icon{display:grid!important;place-items:center!important;flex:0 0 30px!important;width:30px!important;height:30px!important;border:0!important;border-radius:0!important;background:transparent!important;color:#5ca2ff!important;box-shadow:none!important}
      .fn-shell-fact>.fn-top-fact-icon svg{display:block;width:25px!important;height:25px!important}
      .fn-shell-fact-copy,.fn-version-copy{display:flex!important;flex-direction:column!important;justify-content:center!important;gap:2px!important;min-width:0!important;line-height:1.05!important;text-align:left!important}
      .fn-shell-fact-copy>span,.fn-version-copy small{font-size:10px!important;line-height:1.05!important;color:#8da4c2!important;font-weight:720!important;letter-spacing:0!important;text-transform:none!important;white-space:nowrap!important}
      .fn-shell-fact-copy>strong,.fn-version-copy strong{font-size:13.5px!important;line-height:1.1!important;color:#f5f8ff!important;font-weight:790!important;white-space:nowrap!important}
      #topFreenetUpdate{appearance:none!important;display:flex!important;align-items:center!important;justify-content:flex-start!important;gap:10px!important;min-width:118px!important;padding:6px 12px!important;margin:0!important;text-decoration:none!important;cursor:pointer!important;font:inherit!important}
      #topFreenetUpdate:hover{border-color:#5277a5!important;background:linear-gradient(180deg,#12263e,#0c1b2d)!important}
      #topFreenetUpdate .fn-version-icon{position:relative!important;font-size:0!important}
      #topFreenetUpdate .fn-version-icon::before{content:'';display:block;width:25px;height:25px;background:currentColor;-webkit-mask:var(--fn-brand-mark) center/contain no-repeat;mask:var(--fn-brand-mark) center/contain no-repeat}
      #topFreenetUpdate.update-available{border-color:rgba(54,227,162,.68)!important;background:linear-gradient(180deg,rgba(16,63,50,.94),rgba(10,43,35,.96))!important;box-shadow:inset 0 0 0 1px rgba(54,227,162,.08),0 0 18px rgba(54,227,162,.08)!important}
      #topFreenetUpdate.update-available .fn-version-icon,#topFreenetUpdate.update-available .fn-version-copy small,#topFreenetUpdate.update-available .fn-version-copy strong{color:#63eeb1!important}
      .sidebar{padding:18px 13px!important;background:rgba(6,14,24,.97)!important;border-right-color:#233a55!important}
      .sidebar>.brand,.auth-card>.brand{display:flex!important;align-items:center!important;gap:10px!important}
      .sidebar>.brand{padding:7px 11px 23px!important;font-size:25px!important;letter-spacing:-.045em!important}
      .sidebar>.brand::before,.auth-card>.brand::before{content:'';display:block;flex:0 0 auto;width:28px;height:28px;background:#6fa7ff;-webkit-mask:var(--fn-brand-mark) center/contain no-repeat;mask:var(--fn-brand-mark) center/contain no-repeat}
      .auth-card>.brand::before{width:27px;height:27px}
      .fn-brand-wordmark{display:inline-flex!important;align-items:baseline!important;gap:0!important;line-height:.94!important;font-family:ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif!important;font-size:24px!important;font-weight:900!important;letter-spacing:-.072em!important;text-rendering:geometricPrecision;white-space:nowrap}
      .fn-brand-wordmark .fn-brand-free,.fn-brand-wordmark .fn-brand-net{display:inline-block!important}
      .fn-brand-wordmark .fn-brand-free{color:#f7f9ff!important;background:linear-gradient(180deg,#ffffff 12%,#dce7f7 100%);-webkit-background-clip:text;background-clip:text;-webkit-text-fill-color:transparent}
      .fn-brand-wordmark .fn-brand-net{margin-left:-.018em!important;color:#7baeff!important;background:linear-gradient(180deg,#9bc5ff 0,#6598f4 100%);-webkit-background-clip:text;background-clip:text;-webkit-text-fill-color:transparent}
      .auth-card>.brand .fn-brand-wordmark{font-size:23px!important}
      .nav{gap:7px!important}
      .nav-btn{min-height:48px!important;padding:0 13px!important;border:1px solid transparent!important;border-radius:12px!important;gap:12px!important;color:#9aacc4!important;font-size:14.5px!important;line-height:1.28!important;font-weight:700!important;letter-spacing:-.005em!important;transition:background .14s ease,border-color .14s ease,color .14s ease!important}
      .nav-btn:hover{background:#0d1d30!important;border-color:#203c5c!important;color:#e9f1fc!important}
      .nav-btn.active{background:linear-gradient(180deg,#17355d,#132b4b)!important;border-color:#3673b9!important;color:#f7faff!important;box-shadow:inset 0 0 0 1px rgba(92,151,255,.08),0 8px 22px rgba(0,0,0,.12)!important}
      .nav-icon{display:grid!important;place-items:center!important;flex:0 0 22px!important;width:22px!important;height:22px!important;color:#72a8ff!important;font-size:0!important}
      .nav-icon svg{display:block;width:21px;height:21px}
      .nav-btn.active .nav-icon{color:#8bb7ff!important}
      .fn-update-popover{position:fixed;z-index:1300;width:min(350px,calc(100vw - 24px));padding:15px;border:1px solid #31577d;border-radius:17px;background:linear-gradient(155deg,#102746,#0b1b31);box-shadow:0 22px 70px rgba(0,0,0,.48);color:#f5f8ff}
      .fn-update-popover[hidden]{display:none!important}.fn-update-arrow{position:absolute;top:-7px;right:29px;width:13px;height:13px;transform:rotate(45deg);background:#102746;border-left:1px solid #31577d;border-top:1px solid #31577d}
      .fn-update-head{display:flex;align-items:center;justify-content:space-between;gap:12px;position:relative}.fn-update-title{display:flex;align-items:center;gap:9px;font-size:16px;font-weight:800}.fn-update-badge{display:grid;place-items:center;width:27px;height:27px;border-radius:50%;background:#31e5a2;color:#052319;font-size:18px;font-weight:900}
      .fn-update-close{appearance:none;border:0;background:transparent;color:#aac0dc;font-size:23px;line-height:1;cursor:pointer;padding:2px 4px}.fn-update-versions{display:grid;grid-template-columns:auto 1fr;gap:7px 13px;margin-top:16px;font-size:13px}.fn-update-versions span{color:#b3c2d6}.fn-update-versions strong{color:#f7f9fd}.fn-update-versions strong.available{color:#36e3a2}
      .fn-update-copy{margin:11px 0 0;color:#c6d2e2;font-size:12.5px;line-height:1.5}.fn-update-safe{display:flex;align-items:flex-start;gap:9px;margin-top:12px;padding:10px 11px;border:1px solid rgba(54,227,162,.32);border-radius:11px;background:rgba(20,112,81,.18);color:#d7faec;font-size:11.5px;line-height:1.4}.fn-update-safe b{display:block;color:#42e7aa}.fn-update-safe-icon{font-size:18px;line-height:1}
      .fn-update-status{margin-top:11px;padding:9px 10px;border:1px solid #294969;border-radius:10px;background:#09192b;color:#b9c9dc;font-size:11.5px;line-height:1.45;white-space:pre-line}.fn-update-status.ok{border-color:rgba(54,227,162,.32);color:#c9f6e4}.fn-update-status.bad{border-color:rgba(255,92,106,.38);color:#ffd1d6}.fn-update-progress{width:100%;margin-top:10px;accent-color:#36e3a2}
      .fn-update-actions{display:grid;grid-template-columns:1fr 1fr;gap:9px;margin-top:12px}.fn-update-actions button{min-height:40px;justify-content:center;text-align:center}.fn-update-actions .fn-update-apply{background:linear-gradient(180deg,#32e5a2,#20c88a);border-color:#4cf0b2;color:#06251a}.fn-update-actions .fn-update-apply:disabled{background:#173329;border-color:#28533f;color:#6f9b88}
      .subscription-approved{--sub-line:#294866;--sub-panel:linear-gradient(180deg,rgba(12,31,52,.96),rgba(8,24,42,.97));--sub-accent:#5ca2ff}
      .subscription-approved .page-head{align-items:flex-start;margin-bottom:22px}.subscription-approved .page-head h1{font-size:31px;letter-spacing:-.04em}.subscription-approved .page-head p{font-size:13px;color:#91a8c7;margin-top:5px}.subscription-approved #subscriptionSummary{display:none!important}
      .fn-sub-stats{display:grid;grid-template-columns:1.05fr 1fr 1.05fr 1.7fr;gap:14px}.fn-sub-stat,.fn-sub-key,.fn-sub-history,.fn-sub-info{border:1px solid var(--sub-line);background:var(--sub-panel);border-radius:16px;box-shadow:0 14px 38px rgba(0,0,0,.12)}
      .fn-sub-stat{min-height:152px;padding:21px 20px;display:flex;gap:15px;align-items:flex-start}.fn-sub-stat-icon,.fn-sub-section-icon{display:grid;place-items:center;flex:0 0 auto;color:var(--sub-accent)}.fn-sub-stat-icon{width:35px;height:35px;margin-top:2px}.fn-sub-stat-icon svg{width:34px;height:34px}.fn-sub-section-icon{width:28px;height:28px}.fn-sub-section-icon svg{width:26px;height:26px}
      .fn-sub-stat-copy{min-width:0;display:flex;flex-direction:column;height:100%;flex:1}.fn-sub-label{color:#9db1cd;font-size:12px;font-weight:650}.fn-sub-value{margin-top:7px;color:#f7f9fd;font-size:23px!important;line-height:1.05!important;font-weight:820!important;letter-spacing:-.025em}.fn-sub-value.ok{color:#46e5a2!important}.fn-sub-value.small{font-size:20px!important}.fn-sub-meta{margin-top:7px;color:#9bafca;font-size:11.5px;line-height:1.45}.fn-sub-provider{margin-top:auto;padding-top:12px;color:#e8f0fb;font-size:16px;font-weight:760}.fn-sub-provider span{display:block;color:#8fa6c5;font-size:10.5px;font-weight:650;margin-bottom:3px}.fn-sub-next .btn{margin-top:14px;min-height:40px;padding:9px 13px;justify-content:center;width:100%}
      .fn-sub-key{display:grid;grid-template-columns:minmax(0,1fr) 315px;margin-top:18px;overflow:hidden}.fn-sub-key-main{padding:20px 22px}.fn-sub-key-actions{padding:20px;border-left:1px solid var(--sub-line);display:flex;flex-direction:column;gap:10px;justify-content:center}.fn-sub-heading{display:flex;align-items:center;gap:11px;margin-bottom:15px}.fn-sub-heading h2{font-size:17px;margin:0;letter-spacing:-.02em}.fn-sub-input-wrap{display:flex;align-items:center;gap:10px;padding:0 14px;border:1px solid #315478;background:#091a2d;border-radius:11px;min-height:52px}.fn-sub-input-icon{color:#6ca8ff;display:grid;place-items:center}.fn-sub-input-icon svg{width:21px;height:21px}.fn-sub-input-wrap #subscriptionInput{flex:1;min-width:0;border:0!important;background:transparent!important;padding:13px 0!important;color:#f5f8ff!important;outline:none!important;box-shadow:none!important}.fn-sub-input-wrap #subscriptionInput::placeholder{color:#6f88aa}.fn-sub-key-note{display:flex;align-items:flex-start;gap:7px;margin-top:9px;color:#91a7c5;font-size:11px;line-height:1.45}.fn-sub-key-note svg{width:15px;height:15px;flex:0 0 auto;margin-top:1px;color:#6ca8ff}.fn-sub-key-actions .btn{width:100%;min-height:48px;justify-content:center;text-align:center}.fn-sub-key-actions #saveSubscriptionBtn{background:linear-gradient(180deg,#3f83ff,#2d69df);border-color:#5b98ff;color:#fff}.fn-sub-key-actions button svg{width:19px;height:19px}.subscription-approved #subscriptionNotice{grid-column:1/-1;margin:0 20px 18px}
      .fn-sub-lower{display:grid;grid-template-columns:1.5fr .95fr;gap:18px;margin-top:18px}.fn-sub-history,.fn-sub-info{padding:20px 22px;min-height:245px}.fn-sub-table{width:100%;border-collapse:collapse;font-size:11.5px;overflow:hidden;border-radius:10px;background:#07192b}.fn-sub-table th{padding:10px 12px;text-align:left;color:#9fb5d1;background:#0c2540;font-weight:680}.fn-sub-table td{padding:10px 12px;border-top:1px solid #173552;color:#cbd8e9}.fn-sub-table td:last-child{color:#53e3a3}.fn-sub-empty{padding:28px 12px!important;text-align:center;color:#7890af!important}.fn-sub-info-grid{display:grid;grid-template-columns:1fr auto;gap:17px 18px;margin-top:22px;align-items:center}.fn-sub-info-grid dt{color:#91a8c6;font-size:12px}.fn-sub-info-grid dd{margin:0;color:#f5f8ff;font-size:13px;font-weight:760}.fn-sub-info-grid dd.ok{color:#51e3a2}.fn-sub-status-dot{display:inline-block;width:10px;height:10px;border-radius:50%;background:#4be4a2;margin-right:7px;box-shadow:0 0 12px rgba(75,228,162,.3)}
      @media(max-width:1180px){.overview-approved-fact.fn-shell-fact{min-width:116px;padding-left:9px!important;padding-right:9px!important}.overview-approved-fact.fn-shell-fact:nth-child(2){min-width:148px}.overview-approved-top.fn-shell-summary{gap:6px!important}#topFreenetUpdate{min-width:108px!important;padding-left:9px!important;padding-right:9px!important}.fn-sub-stats{grid-template-columns:repeat(2,minmax(0,1fr))}.fn-sub-key{grid-template-columns:minmax(0,1fr) 280px}}
      @media(max-width:820px){.fn-sub-key{grid-template-columns:1fr}.fn-sub-key-actions{border-left:0;border-top:1px solid var(--sub-line)}.fn-sub-lower{grid-template-columns:1fr}}
      @media(max-width:760px){.overview-approved-top.fn-shell-summary{display:none!important}.sidebar>.brand{padding-bottom:17px!important}.nav-btn{font-size:14px!important}}
      @media(max-width:600px){.fn-update-popover{right:12px!important;left:12px!important;width:auto!important}.fn-update-arrow{display:none}.fn-sub-stats{grid-template-columns:1fr}.fn-sub-stat{min-height:132px}.fn-sub-history{overflow-x:auto}.fn-sub-table{min-width:560px}}
    `;
    document.head.appendChild(style);
  }

  function decorateTopbarFact(fact) {
    if (!fact || fact.dataset.freenetShell === '1') return;
    const directChildren = Array.from(fact.children);
    const label = directChildren.find(node => node.tagName === 'SPAN' && !node.classList.contains('fn-top-fact-icon'));
    const value = directChildren.find(node => node.tagName === 'STRONG');
    if (!label || !value) return;
    fact.dataset.freenetShell = '1';
    fact.classList.add('fn-shell-fact');
    const icon = fact.querySelector(':scope > .fn-top-fact-icon');
    const copy = document.createElement('span');
    copy.className = 'fn-shell-fact-copy';
    copy.append(label, value);
    if (icon) fact.append(icon, copy);
    else fact.append(copy);
  }

  function mountBrandWordmark(brand) {
    if (!brand) return;
    brand.classList.add('fn-brand-lockup');
    if (brand.querySelector(':scope > .fn-brand-wordmark')) return;
    const wordmark = document.createElement('span');
    wordmark.className = 'fn-brand-wordmark';
    wordmark.setAttribute('aria-label', 'FreeNet');
    wordmark.innerHTML = '<span class="fn-brand-free" aria-hidden="true">Free</span><span class="fn-brand-net" aria-hidden="true">Net</span>';
    brand.replaceChildren(wordmark);
  }

  function mountShellChrome() {
    const xkeen = qs('#topXkeenLink');
    if (xkeen) {
      xkeen.hidden = true;
      xkeen.tabIndex = -1;
      xkeen.setAttribute('aria-hidden', 'true');
    }

    const summary = qs('#overviewApprovedTop');
    if (summary) {
      summary.classList.add('fn-shell-summary');
      const facts = summary.querySelectorAll('.overview-approved-fact');
      decorateTopbarFact(facts[0]);
      decorateTopbarFact(facts[1]);
      const update = qs('#topFreenetUpdate');
      if (update && update.parentNode !== summary) summary.appendChild(update);
    }

    mountBrandWordmark(qs('.sidebar>.brand'));
    mountBrandWordmark(qs('#authSection .brand'));

    document.querySelectorAll('.nav-btn[data-page]').forEach(button => {
      const icon = button.querySelector('.nav-icon');
      if (!icon || icon.dataset.freenetShell === '1') return;
      icon.dataset.freenetShell = '1';
      icon.innerHTML = shellSVG(button.dataset.page || 'overview');
    });
  }

  function mountRememberMe() {
    const card = qs('#authSection .auth-card');
    if (!card) return;
    if (!qs('#authRememberRow')) {
      const row = document.createElement('label');
      row.id = 'authRememberRow';
      row.className = 'fn-remember';
      row.innerHTML = '<input id="authRemember" type="checkbox"><span>Запомнить меня на этом устройстве</span>';
      const actions = qs('.auth-actions', card);
      if (actions) card.insertBefore(row, actions);
      else card.appendChild(row);
    }
    if (authFetchWrapped) return;
    authFetchWrapped = true;
    const previousFetch = window.fetch.bind(window);
    window.fetch = function(input, init) {
      const url = typeof input === 'string' ? input : (input && input.url) || '';
      let path = url;
      try { path = new URL(url, location.href).pathname; } catch (_) {}
      const method = String((init && init.method) || (input && input.method) || 'GET').toUpperCase();
      if (path.startsWith('/api/auth/')) {
        init = Object.assign({}, init || {}, {credentials: 'include'});
      }
      if ((path === '/api/auth/login' || path === '/api/auth/setup') && method === 'POST' && init && typeof init.body === 'string') {
        try {
          const body = JSON.parse(init.body);
          body.remember = !!qs('#authRemember')?.checked;
          init = Object.assign({}, init, {body: JSON.stringify(body)});
        } catch (_) {}
      }
      return previousFetch(input, init);
    };
  }

  const subscriptionHistoryKey = 'freenet-subscription-history-v1';

  function subscriptionHistory() {
    try {
      const value = JSON.parse(sessionStorage.getItem(subscriptionHistoryKey) || '[]');
      return Array.isArray(value) ? value.slice(0, 5) : [];
    } catch (_) { return []; }
  }

  function renderSubscriptionHistory() {
    const body = qs('#fnSubscriptionHistoryBody');
    if (!body) return;
    body.replaceChildren();
    const items = subscriptionHistory();
    if (!items.length) {
      const row = document.createElement('tr');
      const cell = document.createElement('td');
      cell.colSpan = 3;
      cell.className = 'fn-sub-empty';
      cell.textContent = 'В этой браузерной сессии действий с подпиской ещё не было.';
      row.appendChild(cell);
      body.appendChild(row);
      return;
    }
    items.forEach(item => {
      const row = document.createElement('tr');
      [item.at, item.action, item.result].forEach(value => {
        const cell = document.createElement('td');
        cell.textContent = String(value || '');
        row.appendChild(cell);
      });
      body.appendChild(row);
    });
  }

  function subscriptionTimeLabel() {
    try { return new Intl.DateTimeFormat('ru-RU', {day:'2-digit',month:'short',hour:'2-digit',minute:'2-digit'}).format(new Date()); }
    catch (_) { return new Date().toLocaleString(); }
  }

  function addSubscriptionHistory(action, result) {
    const items = subscriptionHistory();
    items.unshift({at: subscriptionTimeLabel(), action, result});
    try { sessionStorage.setItem(subscriptionHistoryKey, JSON.stringify(items.slice(0, 5))); } catch (_) {}
    renderSubscriptionHistory();
  }

  function subscriptionConfigured() {
    const text = qs('#subscriptionState')?.textContent || '';
    return /Настроена|Активна/i.test(text) && !/Не настроена/i.test(text);
  }

  function syncSubscriptionPage() {
    if (!subscriptionMounted) return;
    const state = qs('#subscriptionState');
    const count = qs('#extraCount');
    const updater = qs('#subscriptionUpdaterState');
    const input = qs('#subscriptionInput');
    const configured = subscriptionConfigured();
    state?.classList.toggle('ok', configured);
    if (input) input.placeholder = configured ? 'Ключ сохранён — вставьте новый для замены' : 'Вставьте HTTPS key-link';
    const infoState = qs('#fnSubscriptionInfoState');
    if (infoState) {
      infoState.textContent = configured ? 'Настроена' : 'Не настроена';
      infoState.classList.toggle('ok', configured);
    }
    const infoCount = qs('#fnSubscriptionInfoCount');
    if (infoCount) infoCount.textContent = count?.textContent || '—';
    const infoUpdate = qs('#fnSubscriptionInfoUpdate');
    if (infoUpdate) infoUpdate.textContent = updater?.textContent || '—';
  }

  function setSubscriptionNotice(text, tone = '') {
    const notice = qs('#subscriptionNotice');
    if (!notice) return;
    notice.textContent = text || '';
    notice.className = text ? 'notice show' + (tone ? ' ' + tone : '') : 'notice';
  }

  async function checkSubscription(label = 'Проверка подписки') {
    setSubscriptionNotice('Проверяем подписку и список Extra-профилей…');
    try {
      if (typeof window.loadNetworkPlan !== 'function') throw new Error('read-only check unavailable');
      await window.loadNetworkPlan();
      const count = qs('#extraCount')?.textContent || '—';
      const profileError = qs('#profilesError')?.textContent || '';
      if (count === '—' || profileError) {
        setSubscriptionNotice(profileError || 'Extra-профили не получены. Конфигурация не изменена.', 'bad');
        addSubscriptionHistory(label, 'Не пройдена');
      } else {
        setSubscriptionNotice(`Подписка доступна. Получено Extra-профилей: ${count}.`, 'ok');
        addSubscriptionHistory(label, `Успешно (${count})`);
        const last = qs('#fnSubscriptionLastAction');
        if (last) last.textContent = `Последняя проверка: ${subscriptionTimeLabel()}`;
      }
    } catch (_) {
      setSubscriptionNotice('Не удалось проверить подписку. Конфигурация не изменена.', 'bad');
      addSubscriptionHistory(label, 'Ошибка');
    }
    syncSubscriptionPage();
  }

  function bindSubscriptionActions(saveButton, refreshButton) {
    const checkButton = qs('#checkSubscriptionBtn');
    if (checkButton && checkButton.dataset.freenetSubscriptionBound !== '1') {
      checkButton.dataset.freenetSubscriptionBound = '1';
      checkButton.addEventListener('click', () => checkSubscription());
    }
    if (refreshButton && refreshButton.dataset.freenetSubscriptionBound !== '1') {
      refreshButton.dataset.freenetSubscriptionBound = '1';
      refreshButton.addEventListener('click', async event => {
        event.preventDefault();
        event.stopImmediatePropagation();
        if (refreshButton.disabled) return;
        setSubscriptionNotice('Обновляем список Extra-профилей…');
        try {
          if (typeof window.refreshProfiles !== 'function') return await checkSubscription('Обновление списка');
          await window.refreshProfiles();
          const count = qs('#extraCount')?.textContent || '—';
          const profileError = qs('#profilesError')?.textContent || '';
          if (count !== '—' && !profileError) {
            setSubscriptionNotice(`Список Extra-профилей обновлён: ${count}.`, 'ok');
            addSubscriptionHistory('Обновление списка', `Успешно (${count})`);
            const last = qs('#fnSubscriptionLastAction');
            if (last) last.textContent = `Последнее обновление: ${subscriptionTimeLabel()}`;
          } else {
            setSubscriptionNotice(profileError || 'Не удалось получить Extra-профили. Конфигурация не изменена.', 'bad');
            addSubscriptionHistory('Обновление списка', 'Не пройдено');
          }
        } catch (_) {
          setSubscriptionNotice('Не удалось обновить Extra-профили. Конфигурация не изменена.', 'bad');
          addSubscriptionHistory('Обновление списка', 'Ошибка');
        }
        syncSubscriptionPage();
      }, true);
    }
    if (saveButton && saveButton.dataset.freenetSubscriptionBound !== '1') {
      saveButton.dataset.freenetSubscriptionBound = '1';
      saveButton.addEventListener('click', async event => {
        event.preventDefault();
        event.stopImmediatePropagation();
        if (saveButton.disabled || typeof window.saveSubscription !== 'function') return;
        await window.saveSubscription();
        const failed = qs('#subscriptionNotice')?.classList.contains('bad');
        addSubscriptionHistory('Сохранение ключа', failed ? 'Ошибка' : 'Сохранено');
        syncSubscriptionPage();
      }, true);
    }
  }

  function mountSubscriptionPage() {
    if (subscriptionMounted) return;
    const page = qs('[data-page-view="subscription"]');
    if (!page) return;
    const summary = qs('#subscriptionSummary', page);
    const state = qs('#subscriptionState', page);
    const count = qs('#extraCount', page);
    const updater = qs('#subscriptionUpdaterState', page);
    const input = qs('#subscriptionInput', page);
    const save = qs('#saveSubscriptionBtn', page);
    const refresh = qs('#refreshProfilesBtn', page);
    const notice = qs('#subscriptionNotice', page);
    if (![summary,state,count,updater,input,save,refresh,notice].every(Boolean)) return;

    subscriptionMounted = true;
    page.classList.add('subscription-approved');
    page.innerHTML = `
      <div class="page-head"><div><h1>Подписка</h1><p>Управление ключом и списком Extra-профилей.</p></div><div id="fnSubscriptionSummaryMount"></div></div>
      <div class="fn-sub-stats">
        <section class="fn-sub-stat"><span class="fn-sub-stat-icon">${shellSVG('subscription')}</span><div class="fn-sub-stat-copy"><span class="fn-sub-label">Статус подписки</span><div id="fnSubscriptionStateMount"></div><div class="fn-sub-provider"><span>VPN-провайдер</span>BlancVPN</div></div></section>
        <section class="fn-sub-stat"><span class="fn-sub-stat-icon">${shellSVG('profiles')}</span><div class="fn-sub-stat-copy"><span class="fn-sub-label">Extra-профили</span><div id="fnSubscriptionCountMount"></div><div class="fn-sub-meta">доступно</div></div></section>
        <section class="fn-sub-stat"><span class="fn-sub-stat-icon">${shellSVG('refresh')}</span><div class="fn-sub-stat-copy"><span class="fn-sub-label">Обновление</span><div id="fnSubscriptionUpdaterMount"></div><div id="fnSubscriptionLastAction" class="fn-sub-meta">Проверка выполняется по запросу</div></div></section>
        <section class="fn-sub-stat fn-sub-next"><span class="fn-sub-stat-icon">${shellSVG('clock')}</span><div class="fn-sub-stat-copy"><span class="fn-sub-label">Следующее обновление</span><div class="fn-sub-value small">Вручную</div><div class="fn-sub-meta">AUTO VPN будет настраиваться отдельно</div><div id="fnSubscriptionRefreshMount"></div></div></section>
      </div>
      <section class="fn-sub-key">
        <div class="fn-sub-key-main"><div class="fn-sub-heading"><span class="fn-sub-section-icon">${shellSVG('key')}</span><h2>Ключ-подписка</h2></div><div class="fn-sub-input-wrap"><span class="fn-sub-input-icon">${shellSVG('key')}</span><div id="fnSubscriptionInputMount" style="display:contents"></div></div><div class="fn-sub-key-note">${shellSVG('shield')}<span>Ключ хранится только локально на роутере. Сохранённое значение никогда не выводится обратно в Control Center, API или журнал.</span></div></div>
        <div class="fn-sub-key-actions"><div id="fnSubscriptionSaveMount"></div><button id="checkSubscriptionBtn" class="btn secondary" type="button">${shellSVG('shield')}<span>Проверить подписку</span></button></div>
        <div id="fnSubscriptionNoticeMount" style="display:contents"></div>
      </section>
      <div class="fn-sub-lower">
        <section class="fn-sub-history"><div class="fn-sub-heading"><span class="fn-sub-section-icon">${shellSVG('history')}</span><div><h2>Последние обновления</h2><div class="fn-sub-meta">Фактические действия в текущей браузерной сессии</div></div></div><table class="fn-sub-table"><thead><tr><th>Дата и время</th><th>Действие</th><th>Результат</th></tr></thead><tbody id="fnSubscriptionHistoryBody"></tbody></table></section>
        <section class="fn-sub-info"><div class="fn-sub-heading"><span class="fn-sub-section-icon">${shellSVG('info')}</span><h2>Информация</h2></div><dl class="fn-sub-info-grid"><dt>VPN-провайдер</dt><dd>BlancVPN</dd><dt>Тип подписки</dt><dd>Extra-профили</dd><dt>Доступно профилей</dt><dd id="fnSubscriptionInfoCount">—</dd><dt>Статус</dt><dd id="fnSubscriptionInfoState">—</dd><dt>Обновление</dt><dd id="fnSubscriptionInfoUpdate">—</dd></dl></section>
      </div>`;

    qs('#fnSubscriptionSummaryMount', page).appendChild(summary);
    summary.hidden = true;
    state.className = 'fn-sub-value small';
    qs('#fnSubscriptionStateMount', page).appendChild(state);
    count.className = 'fn-sub-value';
    qs('#fnSubscriptionCountMount', page).appendChild(count);
    updater.className = 'fn-sub-value small';
    qs('#fnSubscriptionUpdaterMount', page).appendChild(updater);
    input.type = 'password';
    input.autocomplete = 'new-password';
    input.spellcheck = false;
    input.removeAttribute('value');
    qs('#fnSubscriptionInputMount', page).appendChild(input);
    save.className = 'btn primary';
    save.textContent = 'Сохранить изменения';
    qs('#fnSubscriptionSaveMount', page).appendChild(save);
    refresh.className = 'btn secondary';
    refresh.textContent = 'Обновить сейчас';
    qs('#fnSubscriptionRefreshMount', page).appendChild(refresh);
    qs('#fnSubscriptionNoticeMount', page).appendChild(notice);

    bindSubscriptionActions(save, refresh);
    renderSubscriptionHistory();
    syncSubscriptionPage();
    document.addEventListener('freenet:controls-busy', syncSubscriptionPage);
  }

  function ensurePopover() {
    let root = qs('#freenetUpdatePopover');
    if (root) return root;
    root = document.createElement('section');
    root.id = 'freenetUpdatePopover';
    root.className = 'fn-update-popover';
    root.hidden = true;
    root.setAttribute('role', 'dialog');
    root.setAttribute('aria-label', 'Обновление FreeNet');
    root.innerHTML = `
      <div class="fn-update-arrow"></div>
      <div class="fn-update-head"><div class="fn-update-title"><span class="fn-update-badge">↑</span><span>Обновление FreeNet</span></div><button id="fnUpdateClose" class="fn-update-close" type="button" aria-label="Закрыть">×</button></div>
      <div class="fn-update-versions"><span>Текущая версия:</span><strong id="fnUpdateCurrent">—</strong><span>Доступна новая версия:</span><strong id="fnUpdateLatest" class="available">проверяем…</strong></div>
      <p class="fn-update-copy">Проверка и обновление выполняются прямо здесь, без отдельной страницы.</p>
      <div class="fn-update-safe"><span class="fn-update-safe-icon">◆</span><span><b>Обновление безопасно</b>Backup, SHA-256, staging и проверка после перезапуска сохраняются. VPN, DNS и routing не меняются.</span></div>
      <div id="fnUpdateStatus" class="fn-update-status">Нажмите «Проверить», чтобы сверить последний опубликованный релиз.</div>
      <progress id="fnUpdateProgress" class="fn-update-progress" hidden></progress>
      <div class="fn-update-actions"><button id="fnUpdateCheck" class="btn secondary" type="button">Проверить</button><button id="fnUpdateApply" class="btn fn-update-apply" type="button" disabled>Обновить</button></div>`;
    document.body.appendChild(root);
    qs('#fnUpdateClose').addEventListener('click', () => { if (!updatePolling) root.hidden = true; });
    qs('#fnUpdateCheck').addEventListener('click', checkUpdate);
    qs('#fnUpdateApply').addEventListener('click', startUpdate);
    return root;
  }

  function positionPopover() {
    const control = qs('#topFreenetUpdate');
    const popover = ensurePopover();
    if (!control || popover.hidden) return;
    const rect = control.getBoundingClientRect();
    const width = Math.min(350, window.innerWidth - 24);
    let left = rect.right - width;
    if (left < 12) left = 12;
    if (left + width > window.innerWidth - 12) left = window.innerWidth - width - 12;
    popover.style.left = `${left}px`;
    popover.style.top = `${rect.bottom + 8}px`;
  }

  function setStatus(text, tone = '') {
    const node = qs('#fnUpdateStatus');
    if (!node) return;
    node.textContent = text || '';
    node.className = 'fn-update-status' + (tone ? ' ' + tone : '');
  }

  function setProgress(active) {
    const progress = qs('#fnUpdateProgress');
    if (!progress) return;
    progress.hidden = !active;
    if (active) progress.removeAttribute('value');
  }

  function setBusy(busy) {
    const check = qs('#fnUpdateCheck');
    const apply = qs('#fnUpdateApply');
    if (check) check.disabled = busy;
    if (apply) apply.disabled = busy || !(updatePlan && updatePlan.success && updatePlan.ready && updatePlan.update_available && updatePlan.target_tag);
  }

  function renderPlan(value) {
    updatePlan = value;
    qs('#fnUpdateCurrent').textContent = value.current_version || '—';
    const latest = qs('#fnUpdateLatest');
    latest.textContent = value.update_available ? (value.latest_version || value.target_tag || 'доступна') : 'не требуется';
    latest.className = value.update_available ? 'available' : '';
    qs('#fnUpdateApply').disabled = !(value.success && value.ready && value.update_available && value.target_tag);
    qs('#topFreenetUpdate')?.classList.toggle('update-available', !!value.update_available);
  }

  async function checkUpdate() {
    const root = ensurePopover();
    root.hidden = false;
    positionPopover();
    setBusy(true);
    setProgress(true);
    setStatus('Проверяем последний опубликованный релиз FreeNet…');
    try {
      const response = await fetch('/api/system/update/plan', {cache: 'no-store'});
      const value = await response.json();
      if (!response.ok || !value.success) throw new Error(value.error || 'Не удалось проверить обновление');
      renderPlan(value);
      setStatus(value.update_available ? `Доступно ${value.latest_version || value.target_tag}. SHA-256: ${value.manifest_verified ? 'проверен' : 'не подтверждён'}.` : 'Установлена актуальная версия FreeNet.', value.manifest_verified || !value.update_available ? 'ok' : '');
    } catch (error) {
      setStatus(error?.message || 'Не удалось проверить обновление.', 'bad');
    } finally {
      setProgress(false);
      setBusy(false);
    }
  }

  async function waitForVersion(target) {
    setStatus(`FreeNet перезапускается. Ждём подтверждения версии ${target}…`);
    for (let i = 0; i < 90; i++) {
      try {
        const response = await fetch('/versionz', {cache: 'no-store', signal: AbortSignal.timeout(5000)});
        const version = (await response.text()).trim();
        if (response.ok && version === target) {
          setStatus(`FreeNet ${target} установлен и подтверждён. Перезагружаем интерфейс…`, 'ok');
          setTimeout(() => location.reload(), 800);
          return;
        }
      } catch (_) {}
      await wait(1000);
    }
    setStatus(`Целевая версия ${target} не подтверждена. Не запускайте обновление повторно до проверки фактического состояния.`, 'bad');
  }

  async function pollUpdate(target) {
    const deadline = Date.now() + 5 * 60 * 1000;
    while (updatePolling && Date.now() < deadline) {
      try {
        const response = await fetch('/api/system/update/state', {cache: 'no-store', signal: AbortSignal.timeout(10000)});
        if (response.status === 401) {
          updatePolling = false;
          await waitForVersion(target);
          return;
        }
        if (response.ok) {
          const state = await response.json();
          const lines = [state.message || state.state || 'Обновление выполняется', state.primary_error ? `Основная ошибка: ${state.primary_error}` : '', state.rollback_state ? `Откат: ${state.rollback_state}` : ''].filter(Boolean);
          const terminalBad = state.state === 'FAILED' || state.state === 'ROLLBACK_FAILED';
          setStatus(lines.join('\n'), state.state === 'SUCCESS' ? 'ok' : terminalBad ? 'bad' : '');
          if (state.state === 'SUCCESS') {
            updatePolling = false;
            await waitForVersion(target);
            return;
          }
          if (terminalBad) {
            updatePolling = false;
            setProgress(false);
            setBusy(false);
            return;
          }
        }
      } catch (_) {
        setStatus('FreeNet кратко недоступен во время перезапуска. Ожидаем возвращения Control Center…');
      }
      await wait(1400);
    }
    updatePolling = false;
    setProgress(false);
    setBusy(false);
    setStatus('Результат обновления пока неизвестен. Не запускайте обновление повторно до проверки фактической версии.', 'bad');
  }

  async function startUpdate() {
    if (!updatePlan || !updatePlan.success || !updatePlan.ready || !updatePlan.update_available || !updatePlan.target_tag || updatePolling) return;
    updatePolling = true;
    setBusy(true);
    setProgress(true);
    setStatus(`Устанавливаем ${updatePlan.target_tag}: создаём backup, проверяем staging и применяем обновление…`);
    try {
      const response = await fetch('/api/system/update/apply', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({target_tag: updatePlan.target_tag})});
      const result = await response.json();
      if (!response.ok || !result.success) throw new Error(result.error || 'Не удалось запустить обновление');
      await pollUpdate(updatePlan.target_tag);
    } catch (error) {
      updatePolling = false;
      setProgress(false);
      setBusy(false);
      setStatus(error?.message || 'Обновление не запущено.', 'bad');
    }
  }

  function openPopover() {
    const root = ensurePopover();
    root.hidden = false;
    positionPopover();
    const versionText = qs('#topFreenetUpdate .fn-version-copy strong')?.textContent || qs('#version')?.textContent || '—';
    if (qs('#fnUpdateCurrent').textContent === '—') qs('#fnUpdateCurrent').textContent = String(versionText).replace(/^FreeNet UI\s*/i, '');
    checkUpdate();
  }

  function bindUpdateControl() {
    if (document.documentElement.dataset.freenetAcceptedUpdateBound === '1') return;
    document.documentElement.dataset.freenetAcceptedUpdateBound = '1';
    document.addEventListener('click', event => {
      const control = event.target?.closest?.('#topFreenetUpdate');
      if (!control) return;
      event.preventDefault();
      event.stopImmediatePropagation();
      openPopover();
    }, true);
    document.addEventListener('click', event => {
      const root = qs('#freenetUpdatePopover');
      if (!root || root.hidden || updatePolling) return;
      const control = event.target?.closest?.('#topFreenetUpdate');
      if (control || root.contains(event.target)) return;
      root.hidden = true;
    });
    window.addEventListener('resize', positionPopover);
    window.addEventListener('scroll', positionPopover, true);
  }

  function mount() {
    injectStyles();
    mountShellChrome();
    requestAnimationFrame(mountShellChrome);
    mountRememberMe();
    mountSubscriptionPage();
    ensurePopover();
    bindUpdateControl();
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mount, {once: true});
  else mount();
})();
