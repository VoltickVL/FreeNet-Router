(() => {
  'use strict';

  const qs = (selector, root = document) => root.querySelector(selector);
  const qsa = (selector, root = document) => Array.from(root.querySelectorAll(selector));
  const clone = value => JSON.parse(JSON.stringify(value));

  const state = {
    family: 'domain',
    kind: 'domain',
    action: 'DIRECT',
    selectedSource: '',
    rules: [],
    compiled: null,
    editing: -1,
    configLoaded: false,
    configLoading: false,
    configTab: 'routing',
    live: {routing: {routing: {}}, policy: {policy: {}}},
    draft: {routing: '', policy: ''},
    configDirty: false,
    configValidated: false,
    liveRules: [],
    liveComplexCount: 0
  };

  function installStyles() {
    if (qs('#routingV2Styles')) return;
    const style = document.createElement('style');
    style.id = 'routingV2Styles';
    style.textContent = `
      [data-page-view="network"].fn-routing-v2>.card.fn-routing-v2-legacy{display:none!important}
      .rv2-workspace{display:grid;gap:14px}
      .rv2-modebar{display:flex;align-items:center;justify-content:space-between;gap:14px;flex-wrap:wrap;padding:5px 0 1px}
      .rv2-modes,.rv2-tabs,.rv2-toolbar{display:flex;align-items:center;gap:8px;flex-wrap:wrap}
      .rv2-mode,.rv2-tab,.rv2-icon-btn{appearance:none;border:1px solid #2b405e;background:#0c1726;color:#a9b7ca;border-radius:10px;cursor:pointer;font:inherit;font-weight:750}
      .rv2-mode{padding:10px 15px;font-size:13px}.rv2-tab{padding:8px 12px;font-size:12px}.rv2-icon-btn{min-width:32px;height:32px;padding:0 8px;font-size:13px}
      .rv2-mode:hover,.rv2-tab:hover,.rv2-icon-btn:hover{border-color:#4f75a4;color:#eef5ff;background:#11243d}
      .rv2-mode.active,.rv2-tab.active{border-color:#5b8cff;background:#18325a;color:#fff;box-shadow:inset 0 0 0 1px rgba(91,140,255,.10)}
      .rv2-state{display:inline-flex;align-items:center;gap:7px;padding:6px 9px;border:1px solid #314865;border-radius:999px;color:#aebed3;font-size:11.5px;font-weight:800;letter-spacing:.03em;text-transform:uppercase}
      .rv2-state::before{content:'';width:7px;height:7px;border-radius:50%;background:#8094ad}.rv2-state.ok{color:#74edb5;border-color:rgba(54,227,162,.38)}.rv2-state.ok::before{background:#36e3a2}.rv2-state.warn{color:#ffc85b;border-color:rgba(255,190,67,.38)}.rv2-state.warn::before{background:#ffbe43}
      .rv2-panel[hidden]{display:none!important}.rv2-card{border:1px solid #294360;border-radius:16px;background:linear-gradient(180deg,#0c1c2f,#0a1828);padding:16px}.rv2-card+.rv2-card{margin-top:12px}
      .rv2-card-head{display:flex;justify-content:space-between;align-items:flex-start;gap:14px;flex-wrap:wrap}.rv2-card h2{margin:0;color:#f4f7fb;font-size:17px}.rv2-copy{margin:4px 0 0;color:#91a5c0;font-size:13px;line-height:1.45}
      .rv2-builder-grid{display:grid;grid-template-columns:165px minmax(0,1fr) auto;gap:9px;margin-top:13px}.rv2-builder-grid select,.rv2-builder-grid input,.rv2-editor{width:100%;box-sizing:border-box;border:1px solid #2b405e;background:#081421;color:#eef5ff;border-radius:11px;outline:none}.rv2-builder-grid select,.rv2-builder-grid input{min-height:42px;padding:9px 11px;font:inherit;font-size:13px}.rv2-builder-grid input:focus,.rv2-builder-grid select:focus,.rv2-editor:focus{border-color:#5b8cff;box-shadow:0 0 0 2px rgba(91,140,255,.08)}
      .rv2-search{margin-top:9px;display:flex;gap:8px;align-items:center;flex-wrap:wrap}.rv2-search .btn{min-height:36px}.rv2-search-results{display:grid;gap:6px;margin-top:9px}.rv2-search-result{appearance:none;width:100%;text-align:left;border:1px solid #26384f;background:#091522;color:#eef5ff;border-radius:10px;padding:9px 11px;cursor:pointer}.rv2-search-result:hover{border-color:#5b8cff;background:#11243b}.rv2-search-result b{display:block;font-size:13px}.rv2-search-result span{display:block;margin-top:3px;color:#859bb7;font-size:12px}
      .rv2-order{display:grid;place-items:center;width:27px;height:27px;border-radius:8px;background:#12243a;color:#91b6eb;font-size:11px;font-weight:850}.rv2-selector{min-width:0}.rv2-selector b{display:block;color:#eef5ff;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.rv2-selector span{display:block;color:#8398b4;margin-top:3px}.rv2-rule-action{font-weight:850}.rv2-rule-action.direct{color:#5ce8a5}.rv2-rule-action.vpn{color:#7dabff}.rv2-rule-action.block{color:#ff858e}.rv2-rule-tools{display:flex;justify-content:flex-end;gap:5px;flex-wrap:wrap}
      .rv2-compiled{margin-top:12px;padding:11px;border:1px solid #263d58;border-radius:11px;background:#091522;color:#92a7c2;font-size:11px;line-height:1.5}.rv2-compiled strong{color:#dce8f8}.rv2-notice{margin-top:10px;display:none;padding:10px 11px;border:1px solid #315071;border-radius:11px;background:#0c2138;color:#b9cbe1;font-size:12px;white-space:pre-wrap}.rv2-notice.show{display:block}.rv2-notice.bad{border-color:rgba(255,103,115,.45);background:#321923;color:#ffd4d7}.rv2-notice.ok{border-color:rgba(54,227,162,.36);background:#0f2b24;color:#d3fae9}
      .rv2-editor-wrap{margin-top:12px}.rv2-editor{display:block;min-height:430px;resize:vertical;padding:14px 15px;font:500 12px/1.55 ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,"Liberation Mono",monospace;tab-size:2;white-space:pre}
      .rv2-config-meta{display:flex;align-items:center;justify-content:space-between;gap:10px;flex-wrap:wrap;margin-top:9px;color:#8499b6;font-size:10px}.rv2-config-meta code{color:#b7c8db}
      .rv2-danger-note{margin-top:12px;padding:10px 11px;border:1px solid rgba(255,190,67,.30);border-radius:11px;background:rgba(103,70,15,.16);color:#e7ca8a;font-size:11px;line-height:1.45}
      .rv4-board-grid{display:grid;grid-template-columns:minmax(0,1.55fr) minmax(270px,1fr) minmax(240px,.85fr);gap:12px;margin-top:15px;align-items:start}
      .rv4-board{--accent:#8aa2bf;border:1px solid #28415e;border-radius:16px;background:#081522;overflow:hidden;box-shadow:0 8px 24px rgba(0,0,0,.08)}
      .rv4-board.direct{--accent:#42df9c}.rv4-board.vpn{--accent:#6fa2ff}.rv4-board.block{--accent:#ff7f88}
      .rv4-board-head{display:flex;align-items:flex-start;justify-content:space-between;gap:12px;padding:15px 15px 13px;border-bottom:1px solid #203650;background:linear-gradient(180deg,rgba(18,40,65,.72),rgba(10,25,42,.45))}
      .rv4-board-title{min-width:0}.rv4-board-title-line{display:flex;align-items:center;gap:9px}.rv4-board-dot{width:9px;height:9px;border-radius:50%;background:var(--accent);box-shadow:0 0 0 4px color-mix(in srgb,var(--accent) 12%,transparent)}.rv4-board-title strong{font-size:17px;color:#f2f7ff}.rv4-board-sub{margin-top:4px;color:#8fa4bf;font-size:12.5px;line-height:1.35}
      .rv4-board-head-actions{display:flex;align-items:center;gap:8px;flex:0 0 auto}.rv4-board-count{min-width:32px;padding:5px 8px;border-radius:999px;background:#10243a;color:#b8cae1;text-align:center;font-size:12.5px;font-weight:850}
      .rv4-board-add{appearance:none;display:inline-flex;align-items:center;gap:6px;min-height:34px;padding:6px 10px;border:1px solid color-mix(in srgb,var(--accent) 55%,#304762);border-radius:10px;background:color-mix(in srgb,var(--accent) 10%,#0b1b2d);color:#eef6ff;font:inherit;font-size:12.5px;font-weight:800;cursor:pointer}.rv4-board-add:hover{background:color-mix(in srgb,var(--accent) 17%,#0b1b2d);border-color:var(--accent)}.rv4-board-add span{font-size:16px;line-height:1;color:var(--accent)}
      .rv4-board-body{padding:2px 15px 12px;min-height:88px}.rv4-type{padding:12px 0 11px}.rv4-type+.rv4-type{border-top:1px solid #1d3148}.rv4-type-head{display:flex;align-items:center;justify-content:space-between;gap:10px;margin-bottom:8px}.rv4-type-head strong{color:#92a9c5;font-size:12.5px;font-weight:850}.rv4-type-head span{color:#6f86a3;font-size:12px;font-weight:750}
      .rv4-chips{display:flex;gap:6px;flex-wrap:wrap}.rv4-chip{display:inline-flex;align-items:center;min-height:30px;padding:5px 9px;border:1px solid #2c4a69;border-radius:9px;background:#0e2237;color:#e0ebf8;font-size:13.5px;line-height:1.15}.rv4-chip[hidden]{display:none!important}
      .rv4-more{appearance:none;min-height:30px;padding:5px 9px;border:1px dashed #4b719b;border-radius:9px;background:#0a1c2e;color:#9fc6f5;font:inherit;font-size:12.5px;font-weight:850;cursor:pointer}.rv4-more:hover{border-style:solid;background:#122a45;color:#fff}
      .rv4-empty{display:grid;place-items:center;min-height:94px;padding:16px;text-align:center;color:#7890ad;font-size:13px;line-height:1.45}.rv4-empty b{display:block;margin-bottom:4px;color:#b9cbe0;font-size:14px}
      .rv4-composer-slot:empty{display:none}.rv4-composer-slot{padding:0 12px 12px}.rv4-composer{border:1px solid #33506f;border-radius:13px;background:#0b1c2f;padding:12px;box-shadow:0 10px 28px rgba(0,0,0,.16)}.rv4-composer[hidden]{display:none!important}
      .rv4-composer-head{display:flex;align-items:flex-start;justify-content:space-between;gap:12px;margin-bottom:10px}.rv4-composer-head strong{display:block;color:#f2f7ff;font-size:14px}.rv4-composer-head span{display:block;margin-top:3px;color:#89a0bc;font-size:12px}.rv4-composer-close{appearance:none;width:30px;height:30px;border:1px solid #314a67;border-radius:9px;background:#0a1828;color:#aabbd0;font:inherit;font-size:17px;cursor:pointer}.rv4-composer-close:hover{border-color:#5f83ad;color:#fff}
      .rv4-composer-grid{display:grid;gap:8px}.rv4-field label{display:block;margin-bottom:5px;color:#859bb7;font-size:12px;font-weight:750}.rv4-composer select,.rv4-composer input{width:100%;box-sizing:border-box;min-height:41px;padding:9px 10px;border:1px solid #2e4968;border-radius:10px;background:#071522;color:#eef5ff;outline:none;font:inherit;font-size:13px}.rv4-composer select:focus,.rv4-composer input:focus{border-color:#5b8cff;box-shadow:0 0 0 2px rgba(91,140,255,.08)}
      .rv4-composer .rv2-search{margin-top:8px}.rv4-composer .rv2-search-result{padding:9px 10px}.rv4-composer-submit{width:100%;min-height:41px;margin-top:1px}
      .rv4-system{margin-top:12px}.rv4-system .rv2-system-toggle{border-radius:12px;background:#091724}
      .rv2-system-toggle{appearance:none;width:100%;display:flex;align-items:center;justify-content:space-between;gap:12px;padding:11px 13px;border:1px solid #283e58;border-radius:12px;background:#091724;color:#a8bbd2;font:inherit;font-size:13px;font-weight:750;cursor:pointer}.rv2-system-toggle:hover{border-color:#3e5d81;background:#0d1f33}.rv2-system-toggle b{color:#dce8f8}.rv2-system-list{display:grid;margin-top:7px;border:1px solid #233951;border-radius:12px;overflow:hidden}.rv2-system-list[hidden]{display:none!important}.rv2-system-rule{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:10px;align-items:center;padding:10px 12px;background:#081522;color:#9eb1c9;font-size:13px}.rv2-system-rule+.rv2-system-rule{border-top:1px solid #1d3148}.rv2-system-route{padding:4px 8px;border:1px solid #304965;border-radius:999px;color:#aec2db;font-size:12px;font-weight:800}
      .rv2-draft-card[hidden]{display:none!important}.rv2-draft-head{display:flex;align-items:center;gap:9px}.rv2-draft-count{display:inline-grid;place-items:center;min-width:28px;height:28px;padding:0 7px;border-radius:9px;background:#18325a;color:#cfe0ff;font-size:12px;font-weight:850}.rv2-rule-footer{display:flex;align-items:flex-end;justify-content:space-between;gap:12px;flex-wrap:wrap;margin-top:12px;padding-top:12px;border-top:1px solid #223a55}.rv2-rule-footer-copy{max-width:680px;color:#8fa4bf;font-size:12px;line-height:1.45}.rv2-rule-footer-actions{display:flex;gap:8px;flex-wrap:wrap}.rv2-rule-footer-actions .btn{min-height:40px}.rv2-workspace button:disabled{opacity:.38!important;cursor:not-allowed!important;filter:saturate(.55);box-shadow:none!important}.rv2-compiled[hidden]{display:none!important}
      .rv2-rule-list{display:grid;gap:8px;margin-top:12px}.rv2-rule-empty{padding:14px;border:1px dashed #2d425d;border-radius:12px;color:#8498b4;font-size:13px;text-align:center}.rv2-rule{display:grid;grid-template-columns:34px minmax(0,1fr) 100px 155px;align-items:center;gap:10px;padding:10px 11px;border:1px solid #263d58;border-radius:12px;background:#081522}.rv2-selector b{font-size:13px}.rv2-selector span{font-size:12px}.rv2-rule-action{font-size:12px}
      .rv2-notice.warn{border-color:rgba(255,190,67,.38);background:rgba(103,70,15,.16);color:#f0cf8e}
      @media(max-width:1120px){.rv4-board-grid{grid-template-columns:1fr 1fr}.rv4-board.direct{grid-column:1/-1}.rv4-board.block{min-height:100%}}
      @media(max-width:760px){.rv4-board-grid{grid-template-columns:1fr}.rv4-board.direct{grid-column:auto}.rv2-rule{grid-template-columns:34px minmax(0,1fr)}.rv2-rule-action{grid-column:2}.rv2-rule-tools{grid-column:2;justify-content:flex-start}.rv2-system-rule{grid-template-columns:1fr}.rv2-system-route{justify-self:start}}
      @media(max-width:620px){.rv2-card{padding:13px}.rv2-mode{flex:1}.rv2-modes{width:100%}.rv2-editor{min-height:330px}.rv4-board-head{padding:13px}.rv4-board-body{padding:2px 13px 11px}.rv4-board-head-actions{gap:5px}.rv4-board-add{padding:6px 8px}.rv4-composer-slot{padding:0 10px 10px}}
    `;
    document.head.appendChild(style);
  }

  function setNotice(id, text, type = '') {
    const box = qs(`#${id}`);
    if (!box) return;
    box.textContent = text || '';
    box.className = `rv2-notice${text ? ' show' : ''}${type ? ' ' + type : ''}`;
  }

  function safeError(error, fallback) {
    return error && error.message ? error.message : fallback;
  }

  async function api(path, options = {}) {
    const response = await fetch(path, Object.assign({cache: 'no-store'}, options));
    if (response.status === 401 && typeof loadAuthStatus === 'function') await loadAuthStatus();
    let body = {};
    try { body = await response.json(); } catch (_) {}
    if (!response.ok || body.success === false) {
      throw new Error(body.error || `HTTP ${response.status}`);
    }
    return body;
  }

  function signalRuleDraftChanged() {
    document.dispatchEvent(new CustomEvent('freenet:routing-draft-changed'));
  }

  function humanKind(kind) {
    return ({domain:'Сайт', geosite:'GeoSite', ip:'IP', cidr:'Подсеть', geoip:'GeoIP'})[kind] || 'Условие';
  }

  function actionCopy(action) {
    if (action === 'DIRECT') return 'напрямую, без VPN';
    if (action === 'VPN') return 'через текущий VPN';
    if (action === 'BLOCK') return 'заблокировать';
    return 'системный маршрут';
  }

  function outboundPresentation(tag) {
    const raw = String(tag || '').trim();
    const value = raw.toLowerCase();
    if (value === 'direct' || value === 'freedom') return {action:'DIRECT', label:'DIRECT', tone:'direct'};
    if (value === 'vless-reality') return {action:'VPN', label:'VPN', tone:'vpn'};
    if (value === 'block' || value === 'blocked' || value === 'blackhole') return {action:'BLOCK', label:'BLOCK', tone:'block'};
    if (value === 'dns-out' || value === 'dns-direct' || value === 'dns-vless') return {action:'', label:'DNS', tone:'system'};
    return {action:'', label:'Системный маршрут', tone:'system'};
  }

  function humanCondition(key) {
    return ({
      inboundTag:'входящее подключение',
      network:'тип сети',
      port:'порт назначения',
      sourcePort:'исходный порт',
      protocol:'протокол',
      source:'источник',
      user:'пользователь',
      attrs:'атрибуты',
      balancerTag:'балансировщик'
    })[key] || 'дополнительное условие';
  }

  function parseLiveSelector(raw, family) {
    const value = String(raw || '').trim();
    if (!value) return null;
    if (family === 'domain') {
      if (value.startsWith('geosite:')) return {kind:'geosite', value:value.slice(8), raw:value};
      const ext = value.match(/^ext:([^:]+):(.+)$/i);
      if (ext && /geosite/i.test(ext[1])) return {kind:'geosite', value:ext[2], raw:value};
      if (value.startsWith('domain:')) return {kind:'domain', value:value.slice(7), raw:value};
      if (value.startsWith('full:')) return {kind:'domain', value:value.slice(5), raw:value, exact:true};
      if (/^(regexp|keyword):/i.test(value)) return {kind:'custom', value, raw:value};
      return {kind:'domain', value, raw:value};
    }
    if (value.startsWith('geoip:')) return {kind:'geoip', value:value.slice(6), raw:value};
    const ext = value.match(/^ext:([^:]+):(.+)$/i);
    if (ext && /geoip/i.test(ext[1])) return {kind:'geoip', value:ext[2], raw:value};
    if (value.includes('/')) return {kind:'cidr', value, raw:value};
    return {kind:'ip', value, raw:value};
  }

  function presentLiveRule(rule, index) {
    const object = rule && typeof rule === 'object' && !Array.isArray(rule) ? rule : {};
    const selectors = [];
    (Array.isArray(object.domain) ? object.domain : []).forEach(value => { const parsed = parseLiveSelector(value, 'domain'); if (parsed) selectors.push(parsed); });
    (Array.isArray(object.ip) ? object.ip : []).forEach(value => { const parsed = parseLiveSelector(value, 'ip'); if (parsed) selectors.push(parsed); });
    const knownKeys = new Set(['type','outboundTag','domain','ip']);
    const extraKeys = Object.keys(object).filter(key => !knownKeys.has(key));
    const outbound = outboundPresentation(object.outboundTag);
    const complex = object.type !== 'field' || !selectors.length || extraKeys.length > 0 || selectors.some(item => item.kind === 'custom') || !outbound.action;
    return {
      index,
      selectors,
      action: outbound.action,
      actionLabel: outbound.label,
      actionTone: outbound.tone,
      outboundTag: String(object.outboundTag || ''),
      complex,
      extraKeys,
      conditions: extraKeys.map(humanCondition)
    };
  }

  function livePresentation() {
    const rules = Array.isArray(state.live.routing?.routing?.rules) ? state.live.routing.routing.rules : [];
    return rules.map((rule, index) => presentLiveRule(rule, index));
  }

  function selectorGroupLabel(kind) {
    return ({geosite:'GeoSite', geoip:'GeoIP', domain:'Сайты', ip:'IP', cidr:'Подсети'})[kind] || 'Другое';
  }

  function groupSelectors(selectors) {
    const order = ['geosite','domain','geoip','ip','cidr'];
    const grouped = new Map();
    selectors.forEach(selector => {
      const key = selector.kind || 'custom';
      if (!grouped.has(key)) grouped.set(key, []);
      grouped.get(key).push(selector);
    });
    return order.filter(key => grouped.has(key)).map(key => ({kind:key, items:grouped.get(key)}));
  }

  function aggregateSelectors(items) {
    const result = [];
    const seen = new Set();
    items.forEach(item => item.selectors.forEach(selector => {
      const key = `${selector.kind}\u0000${selector.value}`;
      if (seen.has(key)) return;
      seen.add(key);
      result.push(selector);
    }));
    return result;
  }

  function renderAggregateType(container, group) {
    const section = document.createElement('section'); section.className = 'rv4-type';
    const head = document.createElement('div'); head.className = 'rv4-type-head';
    const title = document.createElement('strong'); title.textContent = selectorGroupLabel(group.kind);
    const count = document.createElement('span'); count.textContent = String(group.items.length);
    head.append(title, count);

    const chips = document.createElement('div'); chips.className = 'rv4-chips';
    const limit = 10;
    group.items.forEach((selector, index) => {
      const chip = document.createElement('span'); chip.className = 'rv4-chip'; chip.textContent = String(selector.value);
      if (index >= limit) chip.hidden = true;
      chips.appendChild(chip);
    });
    if (group.items.length > limit) {
      const more = document.createElement('button'); more.type = 'button'; more.className = 'rv4-more';
      more.setAttribute('aria-expanded','false'); more.textContent = `+${group.items.length - limit}`;
      more.addEventListener('click', () => {
        const expanded = more.getAttribute('aria-expanded') === 'true';
        Array.from(chips.querySelectorAll('.rv4-chip')).forEach((chip,index) => { if (index >= limit) chip.hidden = expanded; });
        more.setAttribute('aria-expanded', expanded ? 'false' : 'true');
        more.textContent = expanded ? `+${group.items.length - limit}` : 'Свернуть';
      });
      chips.appendChild(more);
    }
    section.append(head, chips); container.appendChild(section);
  }

  function renderActionBoard(action, items) {
    const map = {
      DIRECT: ['rv2DirectContent','rv2DirectCount'],
      VPN: ['rv2VPNContent','rv2VPNCount'],
      BLOCK: ['rv2BlockContent','rv2BlockCount']
    };
    const target = map[action];
    if (!target) return;
    const container = qs(`#${target[0]}`);
    const countNode = qs(`#${target[1]}`);
    if (!container || !countNode) return;
    container.textContent = '';

    const selectors = aggregateSelectors(items);
    countNode.textContent = String(selectors.length);
    if (!selectors.length) {
      const empty = document.createElement('div'); empty.className = 'rv4-empty';
      const copy = action === 'BLOCK'
        ? '<b>Ничего не блокируется</b>Добавьте сайт или категорию, если это понадобится.'
        : '<b>Правил пока нет</b>Добавьте сайт, GeoData-группу, IP или подсеть.';
      empty.innerHTML = copy;
      container.appendChild(empty);
      return;
    }
    groupSelectors(selectors).forEach(group => renderAggregateType(container, group));
  }

  function renderSystemRule(container, item) {
    const row = document.createElement('div'); row.className = 'rv2-system-rule';
    const text = document.createElement('div');
    text.textContent = item.conditions.length ? item.conditions.join(', ') : (item.selectors.length ? 'сложное условие' : 'служебное правило');
    const route = document.createElement('span'); route.className = 'rv2-system-route'; route.textContent = item.actionLabel || 'Системный маршрут';
    row.append(text, route); container.appendChild(row);
  }

  function renderLiveRules() {
    const status = qs('#rv2LiveState');
    const systemList = qs('#rv2SystemList');
    if (!status || !systemList) return;
    systemList.textContent = '';
    if (!state.configLoaded) {
      status.className = 'rv2-state'; status.textContent = state.configLoading ? 'Загрузка…' : 'Не загружено';
      return;
    }

    const presented = livePresentation();
    const visible = presented.filter(item => !item.complex && ['DIRECT','VPN','BLOCK'].includes(item.action));
    const system = presented.filter(item => !visible.includes(item));
    const byAction = {
      DIRECT: visible.filter(item => item.action === 'DIRECT'),
      VPN: visible.filter(item => item.action === 'VPN'),
      BLOCK: visible.filter(item => item.action === 'BLOCK')
    };

    state.liveRules = presented;
    state.liveComplexCount = system.length;
    status.className = 'rv2-state ok';
    status.textContent = `${visible.length} пользовательских`;

    renderActionBoard('DIRECT', byAction.DIRECT);
    renderActionBoard('VPN', byAction.VPN);
    renderActionBoard('BLOCK', byAction.BLOCK);

    const systemWrap = qs('#rv2SystemWrap');
    if (systemWrap) systemWrap.hidden = system.length === 0;
    const systemCount = qs('#rv2SystemCount'); if (systemCount) systemCount.textContent = String(system.length);
    system.forEach(item => renderSystemRule(systemList, item));
  }

  function modeMeta() {
    const placeholders = {
      domain: 'Например, example.com',
      geosite: 'Например, youtube или category-social',
      ip: 'Например, 1.1.1.1',
      cidr: 'Например, 10.20.0.0/16',
      geoip: 'Например, ru или private'
    };
    return {
      kinds: [
        ['domain', 'Сайт / домен'],
        ['geosite', 'GeoSite'],
        ['ip', 'IP-адрес'],
        ['cidr', 'Подсеть'],
        ['geoip', 'GeoIP']
      ],
      placeholder: placeholders[state.kind] || placeholders.domain
    };
  }

  function syncKindOptions() {
    const select = qs('#rv2Kind');
    const input = qs('#rv2Value');
    if (!select || !input) return;
    const meta = modeMeta();
    if (!meta.kinds.some(([value]) => value === state.kind)) state.kind = 'domain';
    state.family = ['ip','cidr','geoip'].includes(state.kind) ? 'ip' : 'domain';
    select.textContent = '';
    meta.kinds.forEach(([value, label]) => {
      const option = document.createElement('option');
      option.value = value; option.textContent = label; select.appendChild(option);
    });
    select.value = state.kind;
    input.placeholder = modeMeta().placeholder;
    const search = qs('#rv2GeoSearch');
    if (search) search.hidden = !(state.kind === 'geosite' || state.kind === 'geoip');
  }

  function setAction(action) {
    state.action = ['DIRECT','VPN','BLOCK'].includes(action) ? action : 'DIRECT';
  }

  function actionHuman(action) {
    if (action === 'VPN') return 'через VPN';
    if (action === 'BLOCK') return 'в блокировку';
    return 'напрямую';
  }

  function composerSlot(action) {
    if (action === 'VPN') return qs('#rv2VPNComposerSlot');
    if (action === 'BLOCK') return qs('#rv2BlockComposerSlot');
    return qs('#rv2DirectComposerSlot');
  }

  function closeInlineComposer(clear = true) {
    const composer = qs('#rv2InlineComposer');
    if (composer) {
      composer.hidden = true;
      const home = qs('#rv2ComposerHome'); if (home) home.appendChild(composer);
    }
    if (clear) {
      state.editing = -1;
      state.selectedSource = '';
      const input = qs('#rv2Value'); if (input) input.value = '';
      const results = qs('#rv2SearchResults'); if (results) results.textContent = '';
      const cancel = qs('#rv2CancelEdit'); if (cancel) cancel.hidden = true;
      const add = qs('#rv2AddRule'); if (add) add.textContent = 'Добавить';
      setNotice('rv2RuleNotice', '');
    }
  }

  function openInlineComposer(action, preserve = false) {
    setAction(action);
    if (!preserve) {
      state.editing = -1;
      state.kind = 'domain';
      state.family = 'domain';
      state.selectedSource = '';
      const input = qs('#rv2Value'); if (input) input.value = '';
      const results = qs('#rv2SearchResults'); if (results) results.textContent = '';
      setNotice('rv2RuleNotice', '');
    }
    syncKindOptions();

    const composer = qs('#rv2InlineComposer');
    const slot = composerSlot(state.action);
    if (!composer || !slot) return;
    slot.appendChild(composer);
    composer.hidden = false;
    composer.dataset.action = state.action;

    const title = qs('#rv2ComposerTitle');
    if (title) title.textContent = state.editing >= 0 ? `Изменить правило · ${actionHuman(state.action)}` : `Добавить ${actionHuman(state.action)}`;
    const hint = qs('#rv2ComposerHint');
    if (hint) hint.textContent = state.action === 'BLOCK' ? 'Сайт или категория будут заблокированы.' : (state.action === 'VPN' ? 'Трафик будет направлен через текущий VPN.' : 'Трафик пойдёт напрямую, минуя VPN.');
    const add = qs('#rv2AddRule'); if (add) add.textContent = state.editing >= 0 ? 'Сохранить' : 'Добавить';
    const cancel = qs('#rv2CancelEdit'); if (cancel) cancel.hidden = state.editing < 0;
    const input = qs('#rv2Value'); if (input) setTimeout(() => input.focus(), 0);
  }

  function resetEditor() {
    closeInlineComposer(true);
  }

  function currentInputRule() {
    const value = String(qs('#rv2Value')?.value || '').trim();
    if (!value) throw new Error('Укажите сайт, группу, IP-адрес или подсеть.');
    return {selector: {kind: state.kind, value}, action: state.action};
  }

  function normalizeCompiledRules(compiled) {
    if (!compiled || !Array.isArray(compiled.rules)) return [];
    return compiled.rules.map(rule => ({
      selector: {kind: String(rule.selector?.kind || ''), value: String(rule.selector?.value || '')},
      action: String(rule.action || 'DIRECT')
    }));
  }

  async function compileCandidate(candidate, successCopy) {
    if (!candidate.length) {
      state.rules = [];
      state.compiled = null;
      renderRuleList();
      renderCompileState();
      signalRuleDraftChanged();
      if (successCopy) setNotice('rv2RuleNotice', successCopy, 'ok');
      return true;
    }
    try {
      const body = await api('/api/policy/compile', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({rules: candidate})
      });
      if (body.mutation !== 'NONE') throw new Error('Нарушен read-only contract policy compiler.');
      state.compiled = body.compiled || null;
      state.rules = normalizeCompiledRules(state.compiled);
      signalRuleDraftChanged();
      renderRuleList();
      renderCompileState();
      setNotice('rv2RuleNotice', successCopy || 'Черновик проверен. Текущая маршрутизация пока не изменена.', 'ok');
      return true;
    } catch (error) {
      setNotice('rv2RuleNotice', `Правило не принято: ${safeError(error, 'ошибка compiler')}. Никакие конфиги не изменены.`, 'bad');
      return false;
    }
  }

  async function addOrUpdateRule() {
    let rule;
    try { rule = currentInputRule(); } catch (error) { setNotice('rv2RuleNotice', error.message, 'bad'); return; }
    const candidate = state.rules.map(clone);
    const wasEditing = state.editing >= 0 && state.editing < candidate.length;
    if (wasEditing) candidate[state.editing] = rule;
    else candidate.push(rule);
    const ok = await compileCandidate(candidate, wasEditing ? 'Изменение сохранено в черновике.' : 'Добавлено в черновик. На роутере пока ничего не изменено.');
    if (!ok) return;
    if (wasEditing) {
      closeInlineComposer(true);
      return;
    }
    state.editing = -1;
    state.selectedSource = '';
    const input = qs('#rv2Value'); if (input) { input.value = ''; input.focus(); }
    const results = qs('#rv2SearchResults'); if (results) results.textContent = '';
    const add = qs('#rv2AddRule'); if (add) add.textContent = 'Добавить';
  }

  async function deleteRule(index) {
    const candidate = state.rules.map(clone);
    candidate.splice(index, 1);
    await compileCandidate(candidate, 'Правило удалено из черновика. На роутере пока ничего не изменено.');
    resetEditor();
  }

  async function moveRule(index, delta) {
    const target = index + delta;
    if (target < 0 || target >= state.rules.length) return;
    const candidate = state.rules.map(clone);
    const [item] = candidate.splice(index, 1);
    candidate.splice(target, 0, item);
    await compileCandidate(candidate, 'Порядок новых правил изменён. На роутере пока ничего не изменено.');
    resetEditor();
  }

  function editRule(index) {
    const rule = state.rules[index];
    if (!rule) return;
    state.editing = index;
    state.kind = rule.selector.kind;
    state.family = ['ip','cidr','geoip'].includes(rule.selector.kind) ? 'ip' : 'domain';
    state.action = rule.action;
    openInlineComposer(rule.action, true);
    const input = qs('#rv2Value'); if (input) { input.value = rule.selector.value; input.focus(); }
    const add = qs('#rv2AddRule'); if (add) add.textContent = 'Сохранить';
    const cancel = qs('#rv2CancelEdit'); if (cancel) cancel.hidden = false;
    setNotice('rv2RuleNotice', 'Редактируется правило из черновика. Действующая маршрутизация пока не меняется.');
  }

  function ruleDetails(index) {
    const compiled = state.compiled?.rules?.[index];
    if (!compiled) return 'Ожидает проверки';
    const action = String(compiled.action || '');
    const dns = compiled.dns_leg ? (action === 'DIRECT' ? 'DNS напрямую' : action === 'VPN' ? 'DNS через VPN' : 'DNS блокируется') : 'DNS не меняется';
    return `${actionCopy(action)} · ${dns}`;
  }

  function renderRuleList() {
    const list = qs('#rv2RuleList');
    const card = qs('#rv2DraftCard');
    if (!list) return;
    list.textContent = '';
    if (!state.rules.length) {
      if (card) card.hidden = true;
      return;
    }
    if (card) card.hidden = false;
    const draftCount = qs('#rv2DraftCount'); if (draftCount) draftCount.textContent = String(state.rules.length);
    state.rules.forEach((rule, index) => {
      const row = document.createElement('div'); row.className = 'rv2-rule'; row.dataset.ruleIndex = String(index);
      const order = document.createElement('div'); order.className = 'rv2-order'; order.textContent = String(index + 1);
      const selector = document.createElement('div'); selector.className = 'rv2-selector';
      const strong = document.createElement('b'); strong.textContent = `${humanKind(rule.selector.kind)} · ${rule.selector.value}`;
      const meta = document.createElement('span'); meta.textContent = ruleDetails(index);
      selector.append(strong, meta);
      const action = document.createElement('div'); action.className = `rv2-rule-action ${rule.action.toLowerCase()}`; action.textContent = rule.action;
      const tools = document.createElement('div'); tools.className = 'rv2-rule-tools';
      const actions = [
        ['↑', 'Выше', () => moveRule(index, -1), index === 0],
        ['↓', 'Ниже', () => moveRule(index, 1), index === state.rules.length - 1],
        ['✎', 'Изменить', () => editRule(index), false],
        ['×', 'Удалить', () => deleteRule(index), false]
      ];
      actions.forEach(([text, title, handler, disabled]) => {
        const button = document.createElement('button'); button.type = 'button'; button.className = 'rv2-icon-btn'; button.textContent = text; button.title = title; button.disabled = disabled; button.addEventListener('click', handler); tools.appendChild(button);
      });
      row.append(order, selector, action, tools); list.appendChild(row);
    });
  }

  function syncRuleActionButtons() {
    const hasDraft = state.rules.length > 0 && !!state.compiled;
    const validate = qs('#rv2ValidateRules'); if (validate) validate.disabled = !hasDraft;
    if (!hasDraft) {
      const apply = qs('#rv2ApplyRules'); if (apply) apply.disabled = true;
    }
  }

  function renderCompileState() {
    const summary = qs('#rv2CompiledSummary');
    syncRuleActionButtons();
    if (!summary) return;
    if (!state.rules.length) {
      summary.hidden = true;
      summary.textContent = '';
      return;
    }
    summary.hidden = false;
    if (state.compiled) {
      summary.innerHTML = `<strong>${state.rules.length} новых правил</strong> · они будут поставлены перед существующими; порядок сверху вниз важен.`;
    } else {
      summary.textContent = 'Черновик нужно проверить перед применением.';
    }
  }

  async function searchGeo() {
    if (!(state.kind === 'geosite' || state.kind === 'geoip')) return;
    const query = String(qs('#rv2Value')?.value || '').trim();
    if (!query) { setNotice('rv2RuleNotice', 'Введите название группы или часть названия.', 'bad'); return; }
    const results = qs('#rv2SearchResults'); if (results) results.textContent = '';
    try {
      const body = await api(`/api/geodata/search?kind=${encodeURIComponent(state.kind)}&q=${encodeURIComponent(query)}`);
      if (body.mutation !== 'NONE') throw new Error('Нарушен read-only GeoData contract.');
      const warnings = Array.isArray(body.warnings) ? body.warnings.map(value => String(value || '').trim()).filter(Boolean) : [];
      let count = 0;
      (body.matches || []).forEach(match => (match.categories || []).forEach(category => {
        count++;
        const button = document.createElement('button'); button.type = 'button'; button.className = 'rv2-search-result';
        const bounded = match.truncated ? ' · показана часть совпадений' : '';
        const title = document.createElement('b'); title.textContent = `${humanKind(state.kind)} · ${String(category)}`;
        const meta = document.createElement('span'); meta.textContent = `Выбрать эту категорию${bounded}`;
        button.append(title, meta);
        button.addEventListener('click', () => {
          const input = qs('#rv2Value'); if (input) input.value = String(category);
          state.selectedSource = String(match.file || '');
          qsa('.rv2-search-result').forEach(node => node.classList.remove('active')); button.classList.add('active');
          syncRuleActionButtons();
          setNotice('rv2RuleNotice', `Выбрана категория ${category}. Нажмите «Добавить».`);
        });
        results?.appendChild(button);
      }));
      if (warnings.length) {
        setNotice('rv2RuleNotice', `${count ? `Найдено: ${count}. ` : ''}Часть источников GeoData сейчас недоступна. Доступные результаты показаны.`, 'warn');
      } else if (!count) {
        setNotice('rv2RuleNotice', 'Группы не найдены. Название можно ввести вручную — FreeNet проверит его перед применением.');
      } else {
        setNotice('rv2RuleNotice', `Найдено групп: ${count}. Выберите нужную — действующая маршрутизация пока не меняется.`, 'ok');
      }
    } catch (error) {
      setNotice('rv2RuleNotice', `Поиск GeoData сейчас недоступен. Категорию можно ввести вручную; текущая маршрутизация не меняется.`, 'bad');
    }
  }

  function setMode(mode) {
    const selected = mode === 'config' ? 'config' : 'rules';
    qsa('.rv2-mode').forEach(button => button.classList.toggle('active', button.dataset.mode === selected));
    qs('#rv2RulesPanel').hidden = selected !== 'rules';
    qs('#rv2ConfigPanel').hidden = selected !== 'config';
    if (!state.configLoaded) loadConfig();
    if (selected === 'rules') renderLiveRules();
  }

  function sectionDefault(name) {
    return name === 'routing' ? {routing: {domainStrategy: 'AsIs', rules: []}} : {policy: {}};
  }

  function normalizeLoadedSection(value, name, present) {
    if (!present || !value || typeof value !== 'object' || Array.isArray(value) || !value[name]) return sectionDefault(name);
    return value;
  }

  function setConfigStatus(text, type = '') {
    const node = qs('#rv2ConfigState'); if (!node) return;
    node.textContent = text;
    node.className = `rv2-state${type ? ' ' + type : ''}`;
  }

  function activeDraftKey() { return state.configTab === 'policy' ? 'policy' : 'routing'; }

  function storeEditor() {
    const editor = qs('#rv2ConfigEditor'); if (!editor) return;
    state.draft[activeDraftKey()] = editor.value;
  }

  function showActiveEditor() {
    const editor = qs('#rv2ConfigEditor'); if (!editor) return;
    editor.value = state.draft[activeDraftKey()] || JSON.stringify(sectionDefault(activeDraftKey()), null, 2);
    qsa('.rv2-config-tab').forEach(button => button.classList.toggle('active', button.dataset.configTab === state.configTab));
    const file = qs('#rv2ConfigFile'); if (file) file.textContent = state.configTab === 'routing' ? '05_routing.json' : '06_policy.json';
  }

  async function loadConfig() {
    if (state.configLoading) return;
    state.configLoading = true; setConfigStatus('Загрузка…'); setNotice('rv2ConfigNotice', ''); renderLiveRules();
    try {
      const body = await api('/api/routing/config');
      if (body.mutation !== 'NONE') throw new Error('Нарушен read-only contract.');
      state.live.routing = normalizeLoadedSection(body.routing, 'routing', body.routing_present);
      state.live.policy = normalizeLoadedSection(body.policy, 'policy', body.policy_present);
      state.draft.routing = JSON.stringify(state.live.routing, null, 2);
      state.draft.policy = JSON.stringify(state.live.policy, null, 2);
      state.configLoaded = true; state.configDirty = false; state.configValidated = false;
      showActiveEditor(); renderLiveRules();
      const hashes = qs('#rv2ConfigHashes');
      if (hashes) hashes.textContent = `routing ${body.routing_sha256 ? String(body.routing_sha256).slice(0, 12) : 'new'} · policy ${body.policy_sha256 ? String(body.policy_sha256).slice(0, 12) : 'new'}`;
      setConfigStatus('Live загружен', 'ok');
      setNotice('rv2ConfigNotice', 'Загружены 05_routing.json и 06_policy.json. Редактирование здесь — экспертный режим; применение остаётся защищённым Xray validation и rollback.', 'ok');
    } catch (error) {
      state.configLoaded = false; setConfigStatus('Недоступно', 'warn'); renderLiveRules();
      setNotice('rv2LiveNotice', `Не удалось прочитать текущие правила: ${safeError(error, 'ошибка')}. Никаких изменений не выполнено.`, 'bad');
      setNotice('rv2ConfigNotice', `Не удалось загрузить managed sections: ${safeError(error, 'ошибка')}`, 'bad');
    } finally { state.configLoading = false; renderLiveRules(); }
  }

  function switchConfigTab(tab) {
    storeEditor();
    state.configTab = tab === 'policy' ? 'policy' : 'routing';
    showActiveEditor();
  }

  function parseDraft(key) {
    let parsed;
    try { parsed = JSON.parse(state.draft[key]); } catch (_) { throw new Error(`${key === 'routing' ? '05_routing.json' : '06_policy.json'}: JSON syntax error`); }
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error(`${key}: верхний уровень должен быть JSON object`);
    if (!parsed[key] || typeof parsed[key] !== 'object' || Array.isArray(parsed[key])) throw new Error(`${key}: отсутствует object root \"${key}\"`);
    return parsed;
  }

  function formatActiveConfig() {
    storeEditor();
    const key = activeDraftKey();
    try {
      const parsed = parseDraft(key); state.draft[key] = JSON.stringify(parsed, null, 2); showActiveEditor();
      state.configDirty = true; state.configValidated = false; setConfigStatus('Черновик', 'warn');
      setNotice('rv2ConfigNotice', `${key === 'routing' ? '05_routing.json' : '06_policy.json'} отформатирован только в browser draft. MUTATION: NONE`, 'ok');
    } catch (error) { setNotice('rv2ConfigNotice', error.message, 'bad'); }
  }

  function compiledRuleToXray(rule) {
    const kind = String(rule.selector?.kind || ''); const value = String(rule.selector?.value || '');
    const xray = {type: 'field', outboundTag: String(rule.payload_outbound || '')};
    if (kind === 'domain') xray.domain = [`domain:${value}`];
    else if (kind === 'geosite') xray.domain = [`geosite:${value}`];
    else if (kind === 'geoip') xray.ip = [`geoip:${value}`];
    else if (kind === 'ip' || kind === 'cidr') xray.ip = [value];
    else throw new Error(`Неизвестный selector kind: ${kind}`);
    return xray;
  }

  async function buildRoutingDraftFromRules(openConfig = true) {
    if (!state.compiled || !state.rules.length) {
      setNotice('rv2RulesApplyResult', 'Сначала добавьте хотя бы одно новое правило.', 'bad');
      return null;
    }
    if (!state.configLoaded) await loadConfig();
    if (!state.configLoaded) return null;
    try {
      const base = clone(state.live.routing);
      if (!base.routing || typeof base.routing !== 'object') base.routing = {};
      const existing = Array.isArray(base.routing.rules) ? clone(base.routing.rules) : [];
      const managed = (state.compiled.payload || []).map(compiledRuleToXray);
      base.routing.rules = managed.concat(existing);
      state.draft.routing = JSON.stringify(base, null, 2);
      state.draft.policy = JSON.stringify(state.live.policy, null, 2);
      state.configDirty = true; state.configValidated = false; state.configTab = 'routing'; showActiveEditor();
      setConfigStatus('Черновик', 'warn');
      const copy = `${managed.length} новых правил будут добавлены перед ${existing.length} существующими. Существующие правила сохраняются без изменений.`;
      setNotice('rv2RulesApplyResult', copy, 'ok');
      setNotice('rv2ConfigNotice', `${copy} Это пока только черновик.`, 'ok');
      const preview = qs('#rv2RulesApplyPreview'); if (preview) preview.textContent = `${copy} Сначала выполните проверку.`;
      if (openConfig) setMode('config');
      return {routing: base, policy: clone(state.live.policy), managedCount: managed.length, existingCount: existing.length};
    } catch (error) {
      const message = safeError(error, 'Не удалось подготовить изменения');
      setNotice('rv2RulesApplyResult', message, 'bad'); setNotice('rv2ConfigNotice', message, 'bad');
      return null;
    }
  }

  async function validateConfig() {
    storeEditor();
    let routing, policy;
    try { routing = parseDraft('routing'); policy = parseDraft('policy'); }
    catch (error) { setNotice('rv2ConfigNotice', error.message, 'bad'); setConfigStatus('JSON ошибка', 'warn'); return false; }
    const button = qs('#rv2ValidateConfig'); if (button) { button.disabled = true; button.textContent = 'Проверяем…'; }
    const rulesButton = qs('#rv2ValidateRules'); if (rulesButton) { rulesButton.disabled = true; rulesButton.textContent = 'Проверяем…'; }
    setConfigStatus('Xray validation…');
    try {
      const body = await api('/api/routing/validate', {
        method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({routing, policy})
      });
      if (body.mutation !== 'NONE' || !body.xray_valid) throw new Error('Xray candidate validation contract failed');
      state.draft.routing = JSON.stringify(body.routing || routing, null, 2);
      state.draft.policy = JSON.stringify(body.policy || policy, null, 2);
      state.configValidated = true; state.configDirty = true; showActiveEditor();
      setConfigStatus('Xray валиден', 'ok');
      setNotice('rv2ConfigNotice', 'Проверка Xray пройдена. Live-конфигурация ещё не изменена.', 'ok');
      return true;
    } catch (error) {
      state.configValidated = false; setConfigStatus('Не валиден', 'warn');
      setNotice('rv2ConfigNotice', `Изменения не прошли проверку: ${safeError(error, 'ошибка Xray validation')}. Действующая конфигурация не изменена.`, 'bad');
      return false;
    } finally {
      if (button) { button.disabled = false; button.textContent = 'Проверить Xray'; }
      if (rulesButton) { rulesButton.disabled = false; rulesButton.textContent = 'Проверить изменения'; }
    }
  }

  async function validateRulesCandidate() {
    if (!state.rules.length || !state.compiled) {
      syncRuleActionButtons();
      setNotice('rv2RulesApplyResult', 'Сначала добавьте хотя бы одно правило.', 'bad');
      return;
    }
    const prepared = await buildRoutingDraftFromRules(false);
    if (!prepared) return;
    const ok = await validateConfig();
    if (ok) {
      setNotice('rv2RulesApplyResult', `Проверка пройдена. Новых правил: ${prepared.managedCount}. Текущие правила будут сохранены без изменений.`, 'ok');
      const preview = qs('#rv2RulesApplyPreview'); if (preview) preview.textContent = `Проверка Xray пройдена. Новых правил: ${prepared.managedCount}. Существующие правила сохранены без изменений: ${prepared.existingCount}. Перед записью FreeNet создаст резервную точку и автоматически откатит изменение при ошибке.`;
    } else {
      setNotice('rv2RulesApplyResult', 'Проверка не пройдена. Применение заблокировано; текущая маршрутизация не изменена.', 'bad');
    }
  }

  function resetConfigDraft() {
    if (!state.configLoaded) { loadConfig(); return; }
    state.draft.routing = JSON.stringify(state.live.routing, null, 2);
    state.draft.policy = JSON.stringify(state.live.policy, null, 2);
    state.configDirty = false; state.configValidated = false; showActiveEditor(); setConfigStatus('Live загружен', 'ok');
    setNotice('rv2ConfigNotice', 'Browser draft сброшен к последнему read-only snapshot. MUTATION: NONE', 'ok');
  }

  function mountMarkup(page) {
    const oldPreview = qs('#policyBuilderPreview', page); if (oldPreview) oldPreview.remove();
    qsa(':scope > .card', page).forEach(card => card.classList.add('fn-routing-v2-legacy'));
    const head = qs('.page-head', page);
    if (head) head.innerHTML = '<div><div class="page-kicker">ROUTING POLICY</div><h1>Маршрутизация</h1><p>Сайты и категории — сразу по направлениям. Без чтения конфигов и технических правил.</p></div>';

    const root = document.createElement('div'); root.id = 'routingV2Workspace'; root.className = 'rv2-workspace';
    root.innerHTML = `
      <div class="rv2-modebar">
        <div class="rv2-modes"><button type="button" class="rv2-mode active" data-mode="rules">Правила</button><button type="button" class="rv2-mode" data-mode="config">Конфигурация</button></div>
      </div>
      <section id="rv2RulesPanel" class="rv2-panel">
        <div class="rv2-card">
          <div class="rv2-card-head">
            <div>
              <h2>Куда идёт трафик</h2>
              <p class="rv2-copy">Всё важное на одном экране: что открывается напрямую, что идёт через VPN и что блокируется.</p>
            </div>
            <span id="rv2LiveState" class="rv2-state">Загрузка…</span>
          </div>

          <div class="rv4-board-grid">
            <section id="rv2DirectBoard" class="rv4-board direct">
              <div class="rv4-board-head">
                <div class="rv4-board-title">
                  <div class="rv4-board-title-line"><span class="rv4-board-dot"></span><strong>Напрямую</strong></div>
                  <div class="rv4-board-sub">Без VPN · через провайдера</div>
                </div>
                <div class="rv4-board-head-actions">
                  <span id="rv2DirectCount" class="rv4-board-count">0</span>
                  <button type="button" class="rv4-board-add" data-add-action="DIRECT"><span>+</span> Добавить</button>
                </div>
              </div>
              <div id="rv2DirectContent" class="rv4-board-body"></div>
              <div id="rv2DirectComposerSlot" class="rv4-composer-slot"></div>
            </section>

            <section id="rv2VPNBoard" class="rv4-board vpn">
              <div class="rv4-board-head">
                <div class="rv4-board-title">
                  <div class="rv4-board-title-line"><span class="rv4-board-dot"></span><strong>Через VPN</strong></div>
                  <div class="rv4-board-sub">Текущий VPN-профиль</div>
                </div>
                <div class="rv4-board-head-actions">
                  <span id="rv2VPNCount" class="rv4-board-count">0</span>
                  <button type="button" class="rv4-board-add" data-add-action="VPN"><span>+</span> Добавить</button>
                </div>
              </div>
              <div id="rv2VPNContent" class="rv4-board-body"></div>
              <div id="rv2VPNComposerSlot" class="rv4-composer-slot"></div>
            </section>

            <section id="rv2BlockBoard" class="rv4-board block">
              <div class="rv4-board-head">
                <div class="rv4-board-title">
                  <div class="rv4-board-title-line"><span class="rv4-board-dot"></span><strong>Блокировать</strong></div>
                  <div class="rv4-board-sub">Запретить доступ</div>
                </div>
                <div class="rv4-board-head-actions">
                  <span id="rv2BlockCount" class="rv4-board-count">0</span>
                  <button type="button" class="rv4-board-add" data-add-action="BLOCK"><span>+</span> Добавить</button>
                </div>
              </div>
              <div id="rv2BlockContent" class="rv4-board-body"></div>
              <div id="rv2BlockComposerSlot" class="rv4-composer-slot"></div>
            </section>
          </div>

          <div id="rv2ComposerHome" hidden>
            <div id="rv2InlineComposer" class="rv4-composer" hidden>
              <div class="rv4-composer-head">
                <div><strong id="rv2ComposerTitle">Добавить напрямую</strong><span id="rv2ComposerHint"></span></div>
                <button id="rv2ComposerClose" class="rv4-composer-close" type="button" aria-label="Закрыть">×</button>
              </div>
              <div class="rv4-composer-grid">
                <div class="rv4-field"><label for="rv2Kind">Что добавить</label><select id="rv2Kind" aria-label="Тип правила"></select></div>
                <div class="rv4-field"><label for="rv2Value">Сайт или категория</label><input id="rv2Value" type="text" autocomplete="off" spellcheck="false"></div>
                <button id="rv2AddRule" class="btn primary rv4-composer-submit" type="button">Добавить</button>
              </div>
              <div class="rv2-search"><button id="rv2GeoSearch" class="btn secondary" type="button" hidden>Найти в GeoData</button><button id="rv2CancelEdit" class="btn secondary" type="button" hidden>Отмена редактирования</button></div>
              <div id="rv2SearchResults" class="rv2-search-results"></div>
              <div id="rv2RuleNotice" class="rv2-notice"></div>
            </div>
          </div>

          <div id="rv2SystemWrap" class="rv4-system" hidden>
            <button id="rv2SystemToggle" class="rv2-system-toggle" type="button" aria-expanded="false">
              <span><b>Системные правила</b> · FreeNet сохраняет их без изменений</span>
              <span><span id="rv2SystemCount">0</span> · <span id="rv2SystemToggleAction">показать</span></span>
            </button>
            <div id="rv2SystemList" class="rv2-system-list" hidden></div>
          </div>
        </div>

        <div id="rv2DraftCard" class="rv2-card rv2-draft-card" hidden>
          <div class="rv2-card-head">
            <div>
              <div class="rv2-draft-head"><h2>Черновик изменений</h2><span id="rv2DraftCount" class="rv2-draft-count">0</span></div>
              <p class="rv2-copy">Новые правила ещё не применены. Сначала проверка, затем одно безопасное применение.</p>
            </div>
          </div>
          <div id="rv2RuleList" class="rv2-rule-list"></div><div id="rv2CompiledSummary" class="rv2-compiled" hidden></div>
          <div class="rv2-rule-footer">
            <div id="rv2RulesApplyPreview" class="rv2-rule-footer-copy">Текущая маршрутизация не изменена.</div>
            <div class="rv2-rule-footer-actions"><button id="rv2ValidateRules" class="btn secondary" type="button" disabled>Проверить</button><button id="rv2ApplyRules" class="btn primary" type="button" disabled>Применить</button></div>
          </div>
          <div id="rv2RulesApplyResult" class="rv2-notice"></div>
        </div>
      </section>
      <section id="rv2ConfigPanel" class="rv2-panel" hidden>
        <div class="rv2-card">
          <div class="rv2-card-head"><div><h2>Config Studio</h2><p class="rv2-copy">Экспертный режим: прямое редактирование managed 05_routing.json и 06_policy.json.</p></div><span id="rv2ConfigState" class="rv2-state">Не загружено</span></div>
          <div class="rv2-modebar" style="margin-top:10px"><div class="rv2-tabs"><button type="button" class="rv2-tab rv2-config-tab active" data-config-tab="routing">05_routing</button><button type="button" class="rv2-tab rv2-config-tab" data-config-tab="policy">06_policy</button></div><div class="rv2-toolbar"><button id="rv2FormatConfig" class="btn secondary" type="button">Формат</button><button id="rv2ValidateConfig" class="btn secondary" type="button">Проверить Xray</button><button id="rv2ReloadConfig" class="btn secondary" type="button">Сбросить draft</button></div></div>
          <div class="rv2-editor-wrap"><textarea id="rv2ConfigEditor" class="rv2-editor" spellcheck="false" aria-label="Routing Config Studio"></textarea></div>
          <div class="rv2-config-meta"><span>Файл: <code id="rv2ConfigFile">05_routing.json</code></span><span id="rv2ConfigHashes">snapshot не загружен</span></div>
          <div class="rv2-danger-note">Controlled apply добавляется отдельным safety layer: validation, snapshot, post-check и rollback обязательны.</div>
          <div id="rv2ConfigNotice" class="rv2-notice"></div>
        </div>
      </section>`;
    if (head) head.insertAdjacentElement('afterend', root); else page.prepend(root);
  }

  function bind() {
    qsa('.rv2-mode').forEach(button => button.addEventListener('click', () => setMode(button.dataset.mode)));
    qsa('.rv4-board-add').forEach(button => button.addEventListener('click', () => openInlineComposer(button.dataset.addAction || 'DIRECT')));
    qs('#rv2ComposerClose')?.addEventListener('click', () => closeInlineComposer(true));
    qs('#rv2Kind')?.addEventListener('change', event => {
      state.kind = String(event.target.value || 'domain');
      state.selectedSource = '';
      const results = qs('#rv2SearchResults'); if (results) results.textContent = '';
      syncKindOptions();
    });
    qs('#rv2AddRule')?.addEventListener('click', addOrUpdateRule);
    qs('#rv2CancelEdit')?.addEventListener('click', () => closeInlineComposer(true));
    qs('#rv2GeoSearch')?.addEventListener('click', searchGeo);
    qs('#rv2SystemToggle')?.addEventListener('click', () => {
      const list = qs('#rv2SystemList');
      const toggle = qs('#rv2SystemToggle');
      if (!list || !toggle) return;
      const expanded = toggle.getAttribute('aria-expanded') === 'true';
      list.hidden = expanded;
      toggle.setAttribute('aria-expanded', expanded ? 'false' : 'true');
      const action = qs('#rv2SystemToggleAction');
      if (action) action.textContent = expanded ? 'показать' : 'скрыть';
    });
    qs('#rv2Value')?.addEventListener('keydown', event => {
      if (event.key === 'Enter' && !(state.kind === 'geosite' || state.kind === 'geoip')) {
        event.preventDefault();
        addOrUpdateRule();
      }
    });
    qs('#rv2ValidateRules')?.addEventListener('click', validateRulesCandidate);
    qsa('.rv2-config-tab').forEach(button => button.addEventListener('click', () => switchConfigTab(button.dataset.configTab)));
    qs('#rv2FormatConfig')?.addEventListener('click', formatActiveConfig);
    qs('#rv2ValidateConfig')?.addEventListener('click', validateConfig);
    qs('#rv2ReloadConfig')?.addEventListener('click', resetConfigDraft);
    qs('#rv2ConfigEditor')?.addEventListener('input', () => { state.configDirty = true; state.configValidated = false; storeEditor(); setConfigStatus('Черновик', 'warn'); });
  }

  function mount() {
    const page = qs('[data-page-view="network"]');
    if (!page || qs('#routingV2Workspace')) return;
    installStyles(); page.classList.add('fn-routing-v2'); page.dataset.routingV2 = '1';
    mountMarkup(page); bind(); syncKindOptions(); renderRuleList(); renderCompileState(); renderLiveRules();
    void loadConfig();
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mount);
  else mount();
})();
