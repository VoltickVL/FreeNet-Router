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
      .rv2-modes,.rv2-tabs,.rv2-actions,.rv2-toolbar{display:flex;align-items:center;gap:8px;flex-wrap:wrap}
      .rv2-mode,.rv2-tab,.rv2-action,.rv2-icon-btn{appearance:none;border:1px solid #2b405e;background:#0c1726;color:#a9b7ca;border-radius:10px;cursor:pointer;font:inherit;font-weight:750}
      .rv2-mode{padding:10px 15px;font-size:13px}.rv2-tab{padding:8px 12px;font-size:12px}.rv2-action{padding:9px 13px;font-size:12px}.rv2-icon-btn{min-width:32px;height:32px;padding:0 8px;font-size:13px}
      .rv2-mode:hover,.rv2-tab:hover,.rv2-action:hover,.rv2-icon-btn:hover{border-color:#4f75a4;color:#eef5ff;background:#11243d}
      .rv2-mode.active,.rv2-tab.active{border-color:#5b8cff;background:#18325a;color:#fff;box-shadow:inset 0 0 0 1px rgba(91,140,255,.10)}
      .rv2-action[data-action="DIRECT"].active{border-color:#49da92;background:#123729;color:#ecfff6}.rv2-action[data-action="VPN"].active{border-color:#5b8cff;background:#18325a;color:#fff}.rv2-action[data-action="BLOCK"].active{border-color:#ff7070;background:#3a1d27;color:#fff0f2}
      .rv2-state{display:inline-flex;align-items:center;gap:7px;padding:6px 9px;border:1px solid #314865;border-radius:999px;color:#aebed3;font-size:10px;font-weight:800;letter-spacing:.04em;text-transform:uppercase}
      .rv2-state::before{content:'';width:7px;height:7px;border-radius:50%;background:#8094ad}.rv2-state.ok{color:#74edb5;border-color:rgba(54,227,162,.38)}.rv2-state.ok::before{background:#36e3a2}.rv2-state.warn{color:#ffc85b;border-color:rgba(255,190,67,.38)}.rv2-state.warn::before{background:#ffbe43}
      .rv2-panel[hidden]{display:none!important}.rv2-card{border:1px solid #294360;border-radius:16px;background:linear-gradient(180deg,#0c1c2f,#0a1828);padding:16px}.rv2-card+.rv2-card{margin-top:12px}
      .rv2-card-head{display:flex;justify-content:space-between;align-items:flex-start;gap:14px;flex-wrap:wrap}.rv2-card h2{margin:0;color:#f4f7fb;font-size:17px}.rv2-copy{margin:4px 0 0;color:#91a5c0;font-size:12px;line-height:1.45}
      .rv2-builder-grid{display:grid;grid-template-columns:165px minmax(0,1fr) auto;gap:9px;margin-top:13px}.rv2-builder-grid select,.rv2-builder-grid input,.rv2-editor{width:100%;box-sizing:border-box;border:1px solid #2b405e;background:#081421;color:#eef5ff;border-radius:11px;outline:none}.rv2-builder-grid select,.rv2-builder-grid input{min-height:42px;padding:9px 11px;font:inherit;font-size:13px}.rv2-builder-grid input:focus,.rv2-builder-grid select:focus,.rv2-editor:focus{border-color:#5b8cff;box-shadow:0 0 0 2px rgba(91,140,255,.08)}
      .rv2-search{margin-top:9px;display:flex;gap:8px;align-items:center;flex-wrap:wrap}.rv2-search .btn{min-height:36px}.rv2-search-results{display:grid;gap:6px;margin-top:9px}.rv2-search-result{appearance:none;width:100%;text-align:left;border:1px solid #26384f;background:#091522;color:#eef5ff;border-radius:10px;padding:9px 11px;cursor:pointer}.rv2-search-result:hover{border-color:#5b8cff;background:#11243b}.rv2-search-result b{display:block;font-size:12px}.rv2-search-result span{display:block;margin-top:3px;color:#859bb7;font-size:10px}
      .rv2-rule-list{display:grid;gap:8px;margin-top:12px}.rv2-rule-empty{padding:14px;border:1px dashed #2d425d;border-radius:12px;color:#8498b4;font-size:12px;text-align:center}.rv2-rule{display:grid;grid-template-columns:34px minmax(0,1fr) 120px 155px;align-items:center;gap:10px;padding:10px 11px;border:1px solid #263d58;border-radius:12px;background:#081522}.rv2-order{display:grid;place-items:center;width:27px;height:27px;border-radius:8px;background:#12243a;color:#91b6eb;font-size:11px;font-weight:850}.rv2-selector{min-width:0}.rv2-selector b{display:block;color:#eef5ff;font-size:12px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.rv2-selector span{display:block;color:#8398b4;font-size:10px;margin-top:3px}.rv2-rule-action{font-size:11px;font-weight:850}.rv2-rule-action.direct{color:#5ce8a5}.rv2-rule-action.vpn{color:#7dabff}.rv2-rule-action.block{color:#ff858e}.rv2-rule-tools{display:flex;justify-content:flex-end;gap:5px;flex-wrap:wrap}
      .rv2-compiled{margin-top:12px;padding:11px;border:1px solid #263d58;border-radius:11px;background:#091522;color:#92a7c2;font-size:11px;line-height:1.5}.rv2-compiled strong{color:#dce8f8}.rv2-notice{margin-top:10px;display:none;padding:10px 11px;border:1px solid #315071;border-radius:11px;background:#0c2138;color:#b9cbe1;font-size:11px;white-space:pre-wrap}.rv2-notice.show{display:block}.rv2-notice.bad{border-color:rgba(255,103,115,.45);background:#321923;color:#ffd4d7}.rv2-notice.ok{border-color:rgba(54,227,162,.36);background:#0f2b24;color:#d3fae9}
      .rv2-editor-wrap{margin-top:12px}.rv2-editor{display:block;min-height:430px;resize:vertical;padding:14px 15px;font:500 12px/1.55 ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,"Liberation Mono",monospace;tab-size:2;white-space:pre}
      .rv2-config-meta{display:flex;align-items:center;justify-content:space-between;gap:10px;flex-wrap:wrap;margin-top:9px;color:#8499b6;font-size:10px}.rv2-config-meta code{color:#b7c8db}
      .rv2-danger-note{margin-top:12px;padding:10px 11px;border:1px solid rgba(255,190,67,.30);border-radius:11px;background:rgba(103,70,15,.16);color:#e7ca8a;font-size:11px;line-height:1.45}
      .rv2-guide{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:9px;margin-top:12px}
      .rv2-guide-item{padding:11px 12px;border:1px solid #29425f;border-radius:12px;background:#091725}.rv2-guide-item strong{display:block;font-size:12px}.rv2-guide-item span{display:block;margin-top:4px;color:#8fa4bf;font-size:10.5px;line-height:1.4}.rv2-guide-item.direct strong{color:#5ce8a5}.rv2-guide-item.vpn strong{color:#7dabff}.rv2-guide-item.block strong{color:#ff858e}
      .rv2-live-list{display:grid;gap:7px;margin-top:12px}.rv2-live-rule{display:grid;grid-template-columns:34px minmax(0,1fr) auto;align-items:center;gap:10px;padding:10px 11px;border:1px solid #263d58;border-radius:12px;background:#081522}.rv2-live-main{min-width:0}.rv2-live-title{display:flex;align-items:center;gap:7px;flex-wrap:wrap}.rv2-live-title strong{font-size:12px;color:#eef5ff}.rv2-live-meta{margin-top:6px;color:#8398b4;font-size:10px;line-height:1.4}.rv2-selector-chips{display:flex;gap:5px;flex-wrap:wrap}.rv2-selector-chip{display:inline-flex;align-items:center;max-width:100%;padding:3px 7px;border:1px solid #2c4665;border-radius:999px;background:#0d2136;color:#b7cae3;font-size:9.5px}.rv2-selector-chip b{color:#e7f0fb;margin-right:4px}.rv2-selector-chip[hidden]{display:none!important}.rv2-selector-more{appearance:none;padding:3px 8px;border:1px dashed #41658e;border-radius:999px;background:transparent;color:#8fb8eb;font:inherit;font-size:9.5px;font-weight:800;cursor:pointer}.rv2-selector-more:hover{border-style:solid;background:#10243a;color:#dcecff}.rv2-live-lock{display:inline-flex;padding:2px 6px;border-radius:999px;border:1px solid rgba(255,190,67,.35);color:#f0c66d;font-size:8px;font-weight:800}
      .rv2-action-pill{display:inline-flex;align-items:center;justify-content:center;min-width:72px;padding:6px 9px;border-radius:999px;border:1px solid #314865;font-size:10px;font-weight:850}.rv2-action-pill.direct{color:#5ce8a5;border-color:rgba(73,218,146,.38);background:rgba(18,55,41,.58)}.rv2-action-pill.vpn{color:#8ab2ff;border-color:rgba(91,140,255,.45);background:rgba(24,50,90,.58)}.rv2-action-pill.block{color:#ff9aa1;border-color:rgba(255,112,112,.42);background:rgba(58,29,39,.58)}.rv2-action-pill.system{color:#c4d1e2;background:#0c1d30}.rv2-live-details{margin-top:6px}.rv2-live-details button{appearance:none;border:0;background:transparent;padding:0;color:#7897bb;font:inherit;font-size:9.5px;cursor:pointer}.rv2-live-details button:hover{color:#b9d4f5}.rv2-live-tech{margin-top:5px;padding:6px 8px;border:1px solid #243b55;border-radius:8px;background:#07131f;color:#7f95b1;font:500 9px/1.45 ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace}.rv2-live-tech[hidden]{display:none!important}
      .rv2-action{display:flex;flex-direction:column;align-items:flex-start;gap:2px;min-width:112px}.rv2-action small{font-size:8.5px;font-weight:650;color:#7f94b0}.rv2-action.active small{color:inherit;opacity:.76}
      .rv2-rule-footer{display:flex;align-items:flex-end;justify-content:space-between;gap:12px;flex-wrap:wrap;margin-top:12px;padding-top:12px;border-top:1px solid #223a55}.rv2-rule-footer-copy{max-width:680px;color:#8fa4bf;font-size:10.5px;line-height:1.45}.rv2-rule-footer-actions{display:flex;gap:8px;flex-wrap:wrap}.rv2-rule-footer-actions .btn{min-height:40px}.rv2-workspace button:disabled{opacity:.38!important;cursor:not-allowed!important;filter:saturate(.55);box-shadow:none!important}.rv2-compiled[hidden]{display:none!important}
      .rv2-notice.warn{border-color:rgba(255,190,67,.38);background:rgba(103,70,15,.16);color:#f0cf8e}
      @media(max-width:900px){.rv2-guide{grid-template-columns:1fr}.rv2-live-rule{grid-template-columns:34px minmax(0,1fr)}.rv2-action-pill{grid-column:2;justify-self:start}}
      @media(max-width:900px){.rv2-builder-grid{grid-template-columns:1fr}.rv2-rule{grid-template-columns:34px minmax(0,1fr);}.rv2-rule-action{grid-column:2}.rv2-rule-tools{grid-column:2;justify-content:flex-start}}
      @media(max-width:620px){.rv2-card{padding:13px}.rv2-mode{flex:1}.rv2-modes{width:100%}.rv2-editor{min-height:330px}}
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

  function renderLiveRules() {
    const list = qs('#rv2LiveRuleList');
    const status = qs('#rv2LiveState');
    if (!list || !status) return;
    list.textContent = '';
    if (!state.configLoaded) {
      status.className = 'rv2-state'; status.textContent = state.configLoading ? 'Загрузка…' : 'Не загружено';
      const empty = document.createElement('div'); empty.className = 'rv2-rule-empty'; empty.textContent = 'Получаем текущие правила с роутера…'; list.appendChild(empty);
      return;
    }
    const presented = livePresentation();
    state.liveRules = presented; state.liveComplexCount = presented.filter(item => item.complex).length;
    status.className = 'rv2-state ok'; status.textContent = `${presented.length} активных`;
    if (!presented.length) {
      const empty = document.createElement('div'); empty.className = 'rv2-rule-empty'; empty.textContent = 'Активных правил маршрутизации пока нет.'; list.appendChild(empty); return;
    }
    presented.forEach(item => {
      const row = document.createElement('div'); row.className = 'rv2-live-rule'; row.dataset.liveRuleIndex = String(item.index);
      const order = document.createElement('div'); order.className = 'rv2-order'; order.textContent = String(item.index + 1);
      const main = document.createElement('div'); main.className = 'rv2-live-main';

      if (item.complex) {
        const title = document.createElement('div'); title.className = 'rv2-live-title';
        const strong = document.createElement('strong'); strong.textContent = 'Системное правило';
        const lock = document.createElement('span'); lock.className = 'rv2-live-lock'; lock.textContent = 'защищено';
        title.append(strong, lock); main.appendChild(title);
      }

      if (item.selectors.length) {
        const chips = document.createElement('div'); chips.className = 'rv2-selector-chips';
        const limit = 6;
        item.selectors.forEach((selector, selectorIndex) => {
          const chip = document.createElement('span'); chip.className = 'rv2-selector-chip';
          if (selectorIndex >= limit) chip.hidden = true;
          const label = selector.kind === 'custom' ? 'Xray' : humanKind(selector.kind);
          const kind = document.createElement('b'); kind.textContent = label;
          chip.append(kind, document.createTextNode(String(selector.value))); chips.appendChild(chip);
        });
        if (item.selectors.length > limit) {
          const more = document.createElement('button'); more.type = 'button'; more.className = 'rv2-selector-more';
          more.setAttribute('aria-expanded', 'false'); more.textContent = `Ещё ${item.selectors.length - limit}`;
          more.addEventListener('click', () => {
            const expanded = more.getAttribute('aria-expanded') === 'true';
            qsa('.rv2-selector-chip', chips).forEach((chip, chipIndex) => { if (chipIndex >= limit) chip.hidden = expanded; });
            more.setAttribute('aria-expanded', expanded ? 'false' : 'true');
            more.textContent = expanded ? `Ещё ${item.selectors.length - limit}` : 'Свернуть';
          });
          chips.appendChild(more);
        }
        main.appendChild(chips);
      }

      if (item.complex) {
        const meta = document.createElement('div'); meta.className = 'rv2-live-meta';
        const conditionCopy = item.conditions.length ? `Условия: ${item.conditions.join(', ')}. ` : '';
        meta.textContent = `${conditionCopy}FreeNet сохранит это правило без изменений.`;
        main.appendChild(meta);

        const details = document.createElement('div'); details.className = 'rv2-live-details';
        const toggle = document.createElement('button'); toggle.type = 'button'; toggle.textContent = 'Технические детали';
        toggle.setAttribute('aria-expanded', 'false');
        const tech = document.createElement('div'); tech.className = 'rv2-live-tech'; tech.hidden = true;
        const rawOutbound = item.outboundTag || '—';
        const rawConditions = item.extraKeys.length ? item.extraKeys.join(', ') : '—';
        tech.textContent = `outbound: ${rawOutbound} · conditions: ${rawConditions}`;
        toggle.addEventListener('click', () => {
          const expanded = toggle.getAttribute('aria-expanded') === 'true';
          tech.hidden = expanded; toggle.setAttribute('aria-expanded', expanded ? 'false' : 'true');
          toggle.textContent = expanded ? 'Технические детали' : 'Скрыть детали';
        });
        details.append(toggle, tech); main.appendChild(details);
      }

      const action = document.createElement('div');
      action.className = `rv2-action-pill ${item.actionTone || 'system'}`;
      action.textContent = item.actionLabel || 'Системный маршрут';
      row.append(order, main, action); list.appendChild(row);
    });
    const notice = qs('#rv2LiveNotice');
    if (notice) {
      if (state.liveComplexCount) setNotice('rv2LiveNotice', `${state.liveComplexCount} системных правил защищены от случайного редактирования. При добавлении новых правил FreeNet сохранит их без изменений.`, 'warn');
      else setNotice('rv2LiveNotice', 'Порядок сверху вниз является приоритетом: срабатывает первое подходящее правило.', 'ok');
    }
  }

  function modeMeta() {
    if (state.family === 'ip') {
      return {
        kinds: [
          ['ip', 'IP-адрес'], ['cidr', 'Подсеть CIDR'], ['geoip', 'Группа GeoIP']
        ],
        placeholder: state.kind === 'geoip' ? 'Например, ru или private' : (state.kind === 'cidr' ? 'Например, 10.20.0.0/16' : 'Например, 1.1.1.1')
      };
    }
    return {
      kinds: [['domain', 'Сайт / домен'], ['geosite', 'Группа GeoSite']],
      placeholder: state.kind === 'geosite' ? 'Например, youtube' : 'Например, example.com'
    };
  }

  function syncKindOptions() {
    const select = qs('#rv2Kind');
    const input = qs('#rv2Value');
    if (!select || !input) return;
    const meta = modeMeta();
    if (!meta.kinds.some(([value]) => value === state.kind)) state.kind = meta.kinds[0][0];
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

  function setFamily(family) {
    state.family = family === 'ip' ? 'ip' : 'domain';
    state.kind = state.family === 'ip' ? 'ip' : 'domain';
    state.selectedSource = '';
    qsa('.rv2-family').forEach(button => button.classList.toggle('active', button.dataset.family === state.family));
    const input = qs('#rv2Value'); if (input) input.value = '';
    const results = qs('#rv2SearchResults'); if (results) results.textContent = '';
    syncKindOptions();
  }

  function setAction(action) {
    state.action = ['DIRECT', 'VPN', 'BLOCK'].includes(action) ? action : 'DIRECT';
    qsa('.rv2-action').forEach(button => button.classList.toggle('active', button.dataset.action === state.action));
  }

  function resetEditor() {
    state.editing = -1;
    state.selectedSource = '';
    const input = qs('#rv2Value'); if (input) input.value = '';
    const add = qs('#rv2AddRule'); if (add) add.textContent = 'Добавить правило';
    const cancel = qs('#rv2CancelEdit'); if (cancel) cancel.hidden = true;
    const results = qs('#rv2SearchResults'); if (results) results.textContent = '';
    setNotice('rv2RuleNotice', '');
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
    if (state.editing >= 0 && state.editing < candidate.length) candidate[state.editing] = rule;
    else candidate.push(rule);
    const ok = await compileCandidate(candidate, state.editing >= 0 ? 'Правило обновлено в черновике. На роутере пока ничего не изменено.' : 'Правило добавлено в черновик. На роутере пока ничего не изменено.');
    if (ok) resetEditor();
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
    state.family = ['ip', 'cidr', 'geoip'].includes(rule.selector.kind) ? 'ip' : 'domain';
    state.kind = rule.selector.kind;
    state.action = rule.action;
    qsa('.rv2-family').forEach(button => button.classList.toggle('active', button.dataset.family === state.family));
    syncKindOptions(); setAction(state.action);
    const input = qs('#rv2Value'); if (input) { input.value = rule.selector.value; input.focus(); }
    const add = qs('#rv2AddRule'); if (add) add.textContent = 'Обновить правило';
    const cancel = qs('#rv2CancelEdit'); if (cancel) cancel.hidden = false;
    setNotice('rv2RuleNotice', `Редактируется новое правило #${index + 1}. Действующая маршрутизация не меняется до применения.`);
  }

  function ruleDetails(index) {
    const compiled = state.compiled?.rules?.[index];
    if (!compiled) return 'Ожидает проверки';
    const action = String(compiled.action || '');
    const dns = compiled.dns_leg ? (action === 'DIRECT' ? 'DNS напрямую' : action === 'VPN' ? 'DNS через VPN' : 'DNS блокируется') : 'DNS не меняется';
    return `${actionCopy(action)} · ${dns}`;
  }

  function renderRuleList() {
    const list = qs('#rv2RuleList'); if (!list) return;
    list.textContent = '';
    if (!state.rules.length) {
      const empty = document.createElement('div'); empty.className = 'rv2-rule-empty';
      empty.textContent = 'Изменений пока нет. Добавьте сайт, GeoData-группу, IP или подсеть.';
      list.appendChild(empty); return;
    }
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
    const json = qs('#rv2BuildConfig'); if (json) json.disabled = !hasDraft;
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
        const bounded = match.truncated ? ' · результат ограничен безопасным лимитом' : '';
        const title = document.createElement('b'); title.textContent = `${state.kind}:${String(category)}`;
        const meta = document.createElement('span'); meta.textContent = `${String(match.file || 'geodata')} · выбрать группу${bounded}`;
        button.append(title, meta);
        button.addEventListener('click', () => {
          const input = qs('#rv2Value'); if (input) input.value = String(category);
          state.selectedSource = String(match.file || '');
          qsa('.rv2-search-result').forEach(node => node.classList.remove('active')); button.classList.add('active');
          syncRuleActionButtons();
          setNotice('rv2RuleNotice', `Выбрана группа ${category}. Нажмите «Добавить правило», чтобы поместить её в черновик.`);
        });
        results?.appendChild(button);
      }));
      if (warnings.length) {
        setNotice('rv2RuleNotice', `${count ? `Найдено групп: ${count}. ` : ''}Часть GeoData недоступна: ${warnings.join(' · ')}. Действующая маршрутизация не менялась.`, 'warn');
      } else if (!count) {
        setNotice('rv2RuleNotice', 'Группы не найдены. Название можно ввести вручную — FreeNet проверит его перед применением.');
      } else {
        setNotice('rv2RuleNotice', `Найдено групп: ${count}. Выберите нужную — действующая маршрутизация пока не меняется.`, 'ok');
      }
    } catch (error) {
      setNotice('rv2RuleNotice', `Поиск geodata сейчас недоступен: ${safeError(error, 'ошибка')}. Category можно ввести вручную; live config не меняется.`, 'bad');
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
      setNotice('rv2RuleNotice', 'Сначала добавьте хотя бы одно новое правило.', 'bad');
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
      setNotice('rv2RuleNotice', copy, 'ok');
      setNotice('rv2ConfigNotice', `${copy} Это пока только черновик.`, 'ok');
      const preview = qs('#rv2RulesApplyPreview'); if (preview) preview.textContent = `${copy} Сначала выполните проверку Xray.`;
      if (openConfig) setMode('config');
      return {routing: base, policy: clone(state.live.policy), managedCount: managed.length, existingCount: existing.length};
    } catch (error) {
      const message = safeError(error, 'Не удалось подготовить изменения');
      setNotice('rv2RuleNotice', message, 'bad'); setNotice('rv2ConfigNotice', message, 'bad');
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
      setNotice('rv2RuleNotice', 'Сначала добавьте хотя бы одно правило.', 'bad');
      return;
    }
    const prepared = await buildRoutingDraftFromRules(false);
    if (!prepared) return;
    const ok = await validateConfig();
    if (ok) {
      setNotice('rv2RuleNotice', `Проверка пройдена. Новых правил: ${prepared.managedCount}. Существующие правила: ${prepared.existingCount}, все будут сохранены без изменений.`, 'ok');
      const preview = qs('#rv2RulesApplyPreview'); if (preview) preview.textContent = `Проверка Xray пройдена. Новых правил: ${prepared.managedCount}. Существующие правила сохранены без изменений: ${prepared.existingCount}. Перед записью FreeNet создаст резервную точку и автоматически откатит изменение при ошибке.`;
    } else {
      setNotice('rv2RuleNotice', 'Проверка не пройдена. Применение заблокировано; действующая маршрутизация не изменена.', 'bad');
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
    if (head) head.innerHTML = '<div><div class="page-kicker">ROUTING POLICY</div><h1>Маршрутизация</h1><p>Понятные правила для сайтов и GeoData-групп. Экспертная конфигурация остаётся во вкладке «Конфигурация».</p></div>';

    const root = document.createElement('div'); root.id = 'routingV2Workspace'; root.className = 'rv2-workspace';
    root.innerHTML = `
      <div class="rv2-modebar">
        <div class="rv2-modes"><button type="button" class="rv2-mode active" data-mode="rules">Правила</button><button type="button" class="rv2-mode" data-mode="config">Конфигурация</button></div>
        <span class="rv2-state ok">Безопасный режим</span>
      </div>
      <section id="rv2RulesPanel" class="rv2-panel">
        <div class="rv2-card">
          <div class="rv2-card-head"><div><h2>Как работает маршрутизация</h2><p class="rv2-copy">Правила проверяются сверху вниз. Срабатывает первое подходящее правило.</p></div></div>
          <div class="rv2-guide">
            <div class="rv2-guide-item direct"><strong>DIRECT</strong><span>Открывать напрямую через провайдера, минуя VPN.</span></div>
            <div class="rv2-guide-item vpn"><strong>VPN</strong><span>Отправлять трафик через текущий VPN-профиль.</span></div>
            <div class="rv2-guide-item block"><strong>BLOCK</strong><span>Блокировать обращения к сайту, группе или адресу.</span></div>
          </div>
        </div>
        <div class="rv2-card">
          <div class="rv2-card-head"><div><h2>Сейчас действует</h2><p class="rv2-copy">Фактический порядок правил на этом роутере.</p></div><span id="rv2LiveState" class="rv2-state">Загрузка…</span></div>
          <div id="rv2LiveRuleList" class="rv2-live-list"></div>
          <div id="rv2LiveNotice" class="rv2-notice"></div>
        </div>
        <div class="rv2-card">
          <div class="rv2-card-head"><div><h2>Добавить правило</h2><p class="rv2-copy">Выберите сайт, GeoData-группу, IP или подсеть и укажите, что с ней делать.</p></div></div>
          <div class="rv2-tabs" style="margin-top:13px"><button type="button" class="rv2-tab rv2-family active" data-family="domain">Сайты и GeoSite</button><button type="button" class="rv2-tab rv2-family" data-family="ip">IP и GeoIP</button></div>
          <div class="rv2-builder-grid"><select id="rv2Kind" aria-label="Тип правила"></select><input id="rv2Value" type="text" autocomplete="off" spellcheck="false"><button id="rv2AddRule" class="btn primary" type="button">Добавить правило</button></div>
          <div class="rv2-search"><button id="rv2GeoSearch" class="btn secondary" type="button" hidden>Найти группу в GeoData</button><button id="rv2CancelEdit" class="btn secondary" type="button" hidden>Отмена редактирования</button></div>
          <div id="rv2SearchResults" class="rv2-search-results"></div>
          <div style="margin-top:13px"><div class="eyebrow">Куда направить</div><div class="rv2-actions" style="margin-top:7px">
            <button type="button" class="rv2-action active" data-action="DIRECT"><span>DIRECT</span><small>мимо VPN</small></button>
            <button type="button" class="rv2-action" data-action="VPN"><span>VPN</span><small>через VPN</small></button>
            <button type="button" class="rv2-action" data-action="BLOCK"><span>BLOCK</span><small>заблокировать</small></button>
          </div></div>
          <div id="rv2RuleNotice" class="rv2-notice"></div>
        </div>
        <div class="rv2-card">
          <div class="rv2-card-head"><div><h2>Изменения перед применением</h2><p class="rv2-copy">Новые правила будут добавлены перед существующими. Их можно менять местами, редактировать или удалить.</p></div><button id="rv2BuildConfig" class="btn secondary" type="button" disabled>Посмотреть JSON</button></div>
          <div id="rv2RuleList" class="rv2-rule-list"></div><div id="rv2CompiledSummary" class="rv2-compiled" hidden></div>
          <div class="rv2-rule-footer">
            <div id="rv2RulesApplyPreview" class="rv2-rule-footer-copy">Добавьте правило. До применения действующая маршрутизация не изменится.</div>
            <div class="rv2-rule-footer-actions"><button id="rv2ValidateRules" class="btn secondary" type="button" disabled>Проверить изменения</button><button id="rv2ApplyRules" class="btn primary" type="button" disabled>Применить</button></div>
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
    qsa('.rv2-family').forEach(button => button.addEventListener('click', () => { setFamily(button.dataset.family); syncRuleActionButtons(); }));
    qsa('.rv2-action').forEach(button => button.addEventListener('click', () => { setAction(button.dataset.action); syncRuleActionButtons(); }));
    qs('#rv2Kind')?.addEventListener('change', event => { state.kind = String(event.target.value || 'domain'); state.selectedSource = ''; syncKindOptions(); syncRuleActionButtons(); });
    qs('#rv2AddRule')?.addEventListener('click', addOrUpdateRule);
    qs('#rv2CancelEdit')?.addEventListener('click', resetEditor);
    qs('#rv2GeoSearch')?.addEventListener('click', searchGeo);
    qs('#rv2Value')?.addEventListener('input', syncRuleActionButtons);
    qs('#rv2Value')?.addEventListener('keydown', event => { if (event.key === 'Enter' && !(state.kind === 'geosite' || state.kind === 'geoip')) { event.preventDefault(); addOrUpdateRule(); } });
    qs('#rv2BuildConfig')?.addEventListener('click', () => buildRoutingDraftFromRules(true));
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
