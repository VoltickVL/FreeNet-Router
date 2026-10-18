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
    geoSuggestTimer: null,
    geoSuggestBlurTimer: null,
    geoSuggestController: null,
    geoSuggestItems: [],
    geoSuggestIndex: -1,
    rules: [],
    compiled: null,
    editing: -1,
    configLoaded: false,
    configLoading: false,
    configLoadPromise: null,
    liveStale: false,
    xrayService: null,
    xrayBusy: false,
    configTab: 'routing',
    live: {routing: {routing: {}}, policy: {policy: {}}},
    draft: {routing: '', policy: ''},
    configDirty: false,
    configValidated: false,
    liveRules: [],
    liveComplexCount: 0,
    removals: [],
    collapsed: {DIRECT:true, VPN:true, BLOCK:true}
  };

  

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
      if (ext && /geosite/i.test(ext[1])) return {kind:'geosite', value:ext[2], source:ext[1], raw:value};
      if (value.startsWith('domain:')) return {kind:'domain', value:value.slice(7), raw:value};
      if (value.startsWith('full:')) return {kind:'domain', value:value.slice(5), raw:value, exact:true};
      if (/^(regexp|keyword):/i.test(value)) return {kind:'custom', value, raw:value};
      return {kind:'domain', value, raw:value};
    }
    if (value.startsWith('geoip:')) return {kind:'geoip', value:value.slice(6), raw:value};
    const ext = value.match(/^ext:([^:]+):(.+)$/i);
    if (ext && /geoip/i.test(ext[1])) return {kind:'geoip', value:ext[2], source:ext[1], raw:value};
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

  function removalSourceKey(source) {
    return `${source.ruleIndex}\u0000${source.family}\u0000${source.raw}`;
  }

  function aggregateSelectors(items) {
    const result = [];
    const index = new Map();
    items.forEach(item => item.selectors.forEach(selector => {
      const key = `${selector.kind}\u0000${selector.source || ''}\u0000${selector.value}`;
      let target = index.get(key);
      if (!target) {
        target = Object.assign({}, selector, {sources: []});
        index.set(key, target);
        result.push(target);
      }
      target.sources.push({
        ruleIndex: item.index,
        family: ['domain','geosite'].includes(selector.kind) ? 'domain' : 'ip',
        raw: String(selector.raw || selector.value),
        kind: selector.kind,
        value: selector.value,
        action: item.action
      });
    }));
    return result;
  }

  function selectorRemovalStaged(selector) {
    const staged = new Set(state.removals.map(removalSourceKey));
    return selector.sources.length > 0 && selector.sources.every(source => staged.has(removalSourceKey(source)));
  }

  function toggleLiveSelectorRemoval(selector, action) {
    const sources = Array.isArray(selector.sources) ? selector.sources : [];
    if (!sources.length) return;
    const staged = new Set(state.removals.map(removalSourceKey));
    const undo = sources.every(source => staged.has(removalSourceKey(source)));
    if (undo) {
      const keys = new Set(sources.map(removalSourceKey));
      state.removals = state.removals.filter(item => !keys.has(removalSourceKey(item)));
    } else {
      sources.forEach(source => {
        const key = removalSourceKey(source);
        if (!staged.has(key)) {
          state.removals.push(Object.assign({}, source, {action}));
          staged.add(key);
        }
      });
    }
    signalRuleDraftChanged();
    renderLiveRules();
    renderRuleList();
    renderCompileState();
    setNotice('rv2RulesApplyResult', undo
      ? `${humanKind(selector.kind)} «${selector.value}» возвращён в live draft. На роутере ничего не изменено.`
      : `${humanKind(selector.kind)} «${selector.value}» будет удалён после проверки и явного применения. На роутере пока ничего не изменено.`,
      undo ? '' : 'warn');
  }

  function renderAggregateType(container, group, action) {
    const section = document.createElement('section'); section.className = 'rv4-type';
    const head = document.createElement('div'); head.className = 'rv4-type-head';
    const title = document.createElement('strong'); title.textContent = selectorGroupLabel(group.kind);
    const count = document.createElement('span'); count.textContent = String(group.items.length);
    head.append(title, count);

    const chips = document.createElement('div'); chips.className = 'rv4-chips';
    const limit = 10;
    group.items.forEach((selector, index) => {
      const chip = document.createElement('button');
      chip.type = 'button';
      chip.className = 'rv4-chip';
      const pending = selectorRemovalStaged(selector);
      chip.classList.toggle('pending-remove', pending);
      chip.title = pending ? 'Вернуть в черновик' : 'Удалить это правило';
      chip.setAttribute('aria-label', pending ? `Вернуть ${selector.value}` : `Удалить ${selector.value}`);
      const value = document.createElement('span'); value.textContent = String(selector.value);
      const remove = document.createElement('span'); remove.className = 'rv4-chip-remove'; remove.setAttribute('aria-hidden','true'); remove.textContent = pending ? '↶' : '×';
      chip.append(value, remove);
      chip.addEventListener('click', () => toggleLiveSelectorRemoval(selector, action));
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
    groupSelectors(selectors).forEach(group => renderAggregateType(container, group, action));
  }

  function renderLiveRules() {
    const status = qs('#rv2LiveState');
    if (!status) return;
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

    // System/transport/DNS rules remain authoritative and protected internally,
    // but are deliberately not rendered in the user-facing Rules workspace.
    state.liveRules = presented;
    state.liveComplexCount = system.length;
    status.className = 'rv2-state ok';
    status.textContent = `${visible.length} пользовательских`;

    renderActionBoard('DIRECT', byAction.DIRECT);
    renderActionBoard('VPN', byAction.VPN);
    renderActionBoard('BLOCK', byAction.BLOCK);
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
    if (!(state.kind === 'geosite' || state.kind === 'geoip')) closeGeoAutocomplete();
  }

  function setAction(action) {
    state.action = ['DIRECT','VPN','BLOCK'].includes(action) ? action : 'DIRECT';
  }

  function boardForAction(action) {
    if (action === 'VPN') return qs('#rv2VPNBoard');
    if (action === 'BLOCK') return qs('#rv2BlockBoard');
    return qs('#rv2DirectBoard');
  }

  function setBoardCollapsed(action, collapsed) {
    const normalized = ['DIRECT','VPN','BLOCK'].includes(action) ? action : 'DIRECT';
    state.collapsed[normalized] = !!collapsed;
    const board = boardForAction(normalized);
    const header = board?.querySelector('[data-board-toggle]');
    board?.classList.toggle('collapsed', !!collapsed);
    if (header) {
      header.setAttribute('aria-expanded', collapsed ? 'false' : 'true');
      header.title = collapsed ? 'Раскрыть правила' : 'Скрыть правила';
    }
  }

  function toggleBoard(action) {
    setBoardCollapsed(action, !state.collapsed[action]);
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
      closeGeoAutocomplete();
      const cancel = qs('#rv2CancelEdit'); if (cancel) cancel.hidden = true;
      const add = qs('#rv2AddRule'); if (add) add.textContent = 'Добавить';
      setNotice('rv2RuleNotice', '');
    }
  }

  function openInlineComposer(action, preserve = false) {
    setAction(action);
    setBoardCollapsed(state.action, false);
    if (!preserve) {
      state.editing = -1;
      state.kind = 'domain';
      state.family = 'domain';
      state.selectedSource = '';
      const input = qs('#rv2Value'); if (input) input.value = '';
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
    syncRuleActionButtons();
    const input = qs('#rv2Value'); if (input) setTimeout(() => input.focus(), 0);
  }

  function resetEditor() {
    closeInlineComposer(true);
  }

  function currentInputRule() {
    const value = String(qs('#rv2Value')?.value || '').trim();
    if (!value) throw new Error('Укажите сайт, группу, IP-адрес или подсеть.');
    const selector = {kind: state.kind, value};
    if ((state.kind === 'geosite' || state.kind === 'geoip') && state.selectedSource) selector.source = state.selectedSource;
    return {selector, action: state.action};
  }

  function normalizeCompiledRules(compiled) {
    if (!compiled || !Array.isArray(compiled.rules)) return [];
    return compiled.rules.map(rule => {
      const selector = {kind: String(rule.selector?.kind || ''), value: String(rule.selector?.value || '')};
      if (rule.selector?.source) selector.source = String(rule.selector.source);
      return {selector, action: String(rule.action || 'DIRECT')};
    });
  }

  async function compileCandidate(candidate, successCopy) {
    if (!candidate.length) {
      state.rules = [];
      state.compiled = null;
      renderRuleList();
      renderCompileState();
      signalRuleDraftChanged();
      if (successCopy) setNotice('rv2RuleNotice', successCopy, 'ok');
      void reconcileExternalLiveIfSafe();
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
    state.selectedSource = String(rule.selector.source || '');
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
    const dns = compiled.dns_leg ? (action === 'DIRECT' ? 'DNS: Прямой' : action === 'VPN' ? 'DNS: Раздельный' : 'DNS блокируется') : 'DNS не меняется';
    return `${actionCopy(action)} · ${dns}`;
  }

  function removalGroups() {
    const grouped = new Map();
    state.removals.forEach(item => {
      const key = `${item.action}\u0000${item.kind}\u0000${item.value}`;
      if (!grouped.has(key)) grouped.set(key, {action:item.action, kind:item.kind, value:item.value, sources:[]});
      grouped.get(key).sources.push(item);
    });
    return Array.from(grouped.values());
  }

  function hasRuleDraft() {
    return state.rules.length > 0 || state.removals.length > 0;
  }

  function undoRemovalGroup(group) {
    const keys = new Set(group.sources.map(removalSourceKey));
    state.removals = state.removals.filter(item => !keys.has(removalSourceKey(item)));
    signalRuleDraftChanged();
    renderLiveRules();
    renderRuleList();
    renderCompileState();
    setNotice('rv2RulesApplyResult', `${humanKind(group.kind)} «${group.value}» возвращён. На роутере ничего не изменено.`);
    void reconcileExternalLiveIfSafe();
  }

  function renderRuleList() {
    const list = qs('#rv2RuleList');
    const card = qs('#rv2DraftCard');
    if (!list) return;
    list.textContent = '';
    const removals = removalGroups();
    if (!state.rules.length && !removals.length) {
      if (card) card.hidden = true;
      return;
    }
    if (card) card.hidden = false;
    const draftCount = qs('#rv2DraftCount'); if (draftCount) draftCount.textContent = String(state.rules.length + removals.length);

    state.rules.forEach((rule, index) => {
      const row = document.createElement('div'); row.className = 'rv2-rule'; row.dataset.ruleIndex = String(index);
      const order = document.createElement('div'); order.className = 'rv2-order'; order.textContent = String(index + 1);
      const selector = document.createElement('div'); selector.className = 'rv2-selector';
      const strong = document.createElement('b'); strong.textContent = `${humanKind(rule.selector.kind)} · ${rule.selector.value}${rule.selector.source ? ` · ${rule.selector.source}` : ''}`;
      const meta = document.createElement('span'); meta.textContent = ruleDetails(index);
      selector.append(strong, meta);
      const action = document.createElement('div'); action.className = `rv2-rule-action ${rule.action.toLowerCase()}`; action.textContent = rule.action;
      const tools = document.createElement('div'); tools.className = 'rv2-rule-tools';
      const actions = [
        ['↑', 'Выше', () => moveRule(index, -1), index === 0],
        ['↓', 'Ниже', () => moveRule(index, 1), index === state.rules.length - 1],
        ['✎', 'Изменить', () => editRule(index), false],
        ['×', 'Убрать из черновика', () => deleteRule(index), false]
      ];
      actions.forEach(([text, title, handler, disabled]) => {
        const button = document.createElement('button'); button.type = 'button'; button.className = 'rv2-icon-btn'; button.textContent = text; button.title = title; button.disabled = disabled; button.addEventListener('click', handler); tools.appendChild(button);
      });
      row.append(order, selector, action, tools); list.appendChild(row);
    });

    removals.forEach(group => {
      const row = document.createElement('div'); row.className = 'rv2-rule removal';
      const order = document.createElement('div'); order.className = 'rv2-order'; order.textContent = '−';
      const selector = document.createElement('div'); selector.className = 'rv2-selector';
      const strong = document.createElement('b'); strong.textContent = `Удалить · ${humanKind(group.kind)} · ${group.value}`;
      const meta = document.createElement('span'); meta.textContent = `Сейчас: ${actionCopy(group.action)} · удаление только после проверки и Apply`;
      selector.append(strong, meta);
      const action = document.createElement('div'); action.className = 'rv2-rule-action block'; action.textContent = 'УДАЛИТЬ';
      const tools = document.createElement('div'); tools.className = 'rv2-rule-tools';
      const undo = document.createElement('button'); undo.type = 'button'; undo.className = 'rv2-icon-btn'; undo.textContent = '↶'; undo.title = 'Отменить удаление'; undo.addEventListener('click', () => undoRemovalGroup(group)); tools.appendChild(undo);
      row.append(order, selector, action, tools); list.appendChild(row);
    });
  }

  function syncRuleActionButtons() {
    const additionsReady = state.rules.length === 0 || !!state.compiled;
    const hasDraft = hasRuleDraft() && additionsReady;
    const apply = qs('#rv2ApplyRules');
    if (apply) apply.disabled = !hasDraft || state.liveStale;
  }

  function renderCompileState() {
    const summary = qs('#rv2CompiledSummary');
    syncRuleActionButtons();
    if (!summary) return;
    const removals = removalGroups();
    if (!state.rules.length && !removals.length) {
      summary.hidden = true;
      summary.textContent = '';
      return;
    }
    summary.hidden = false;
    const parts = [];
    if (state.rules.length) parts.push(`добавить: ${state.rules.length}`);
    if (removals.length) parts.push(`удалить: ${removals.length}`);
    summary.innerHTML = `<strong>Изменения · ${parts.join(' · ')}</strong> · нажмите «Применить».`;
  }

  function defaultGeoDataFile(kind) {
    return kind === 'geoip' ? 'geoip.dat' : 'geosite.dat';
  }

  function sourceForSuggestion(item) {
    const file = String(item?.file || '').trim();
    return file && file.toLowerCase() !== defaultGeoDataFile(state.kind).toLowerCase() ? file : '';
  }

  function geoAutocompleteBox() {
    const box = qs('#rv2GeoAutocomplete');
    if (box && box.parentElement !== document.body) document.body.appendChild(box);
    return box;
  }

  function positionGeoAutocomplete() {
    const box = qs('#rv2GeoAutocomplete');
    const input = qs('#rv2Value');
    if (!box || box.hidden || !input || !input.isConnected) return;
    const rect = input.getBoundingClientRect();
    const edge = 8;
    const gap = 6;
    const viewportWidth = Math.max(document.documentElement.clientWidth || 0, window.innerWidth || 0);
    const viewportHeight = Math.max(document.documentElement.clientHeight || 0, window.innerHeight || 0);
    const below = Math.max(0, viewportHeight - rect.bottom - edge - gap);
    const above = Math.max(0, rect.top - edge - gap);
    const openAbove = below < 260 && above > below;
    const available = Math.max(140, openAbove ? above : below);
    const maxHeight = Math.min(available, Math.floor(viewportHeight * 0.72));
    const width = Math.min(Math.max(rect.width + 12, 280), Math.max(280, viewportWidth - edge * 2));
    const left = Math.min(Math.max(edge, rect.left - 6), Math.max(edge, viewportWidth - edge - width));

    box.style.left = `${Math.round(left)}px`;
    box.style.right = 'auto';
    box.style.width = `${Math.round(width)}px`;
    box.style.maxHeight = `${Math.max(140, Math.floor(maxHeight))}px`;
    if (openAbove) {
      box.style.top = 'auto';
      box.style.bottom = `${Math.max(edge, Math.round(viewportHeight - rect.top + gap))}px`;
    } else {
      box.style.bottom = 'auto';
      box.style.top = `${Math.max(edge, Math.round(rect.bottom + gap))}px`;
    }
  }

  function hideGeoAutocomplete() {
    state.geoSuggestItems = [];
    state.geoSuggestIndex = -1;
    const box = qs('#rv2GeoAutocomplete');
    if (box) { box.hidden = true; box.textContent = ''; }
    const input = qs('#rv2Value');
    if (input) input.setAttribute('aria-expanded','false');
  }

  function closeGeoAutocomplete() {
    if (state.geoSuggestTimer) { clearTimeout(state.geoSuggestTimer); state.geoSuggestTimer = null; }
    if (state.geoSuggestBlurTimer) { clearTimeout(state.geoSuggestBlurTimer); state.geoSuggestBlurTimer = null; }
    if (state.geoSuggestController) { state.geoSuggestController.abort(); state.geoSuggestController = null; }
    hideGeoAutocomplete();
  }

  function selectGeoSuggestion(item, fromAutocomplete = true) {
    if (!item) return;
    const input = qs('#rv2Value');
    if (input) input.value = String(item.category || '');
    state.selectedSource = sourceForSuggestion(item);
    if (fromAutocomplete) closeGeoAutocomplete();
    const sourceCopy = state.selectedSource ? ` из ${state.selectedSource}` : '';
    const evidence = Array.isArray(item.evidence) && item.evidence.length ? ` · найдено по ${item.evidence.join(', ')}` : '';
    syncRuleActionButtons();
    setNotice('rv2RuleNotice', `Выбрана ${humanKind(state.kind)} категория ${item.category}${sourceCopy}${evidence}. Нажмите «Добавить».`, 'ok');
  }

  function setGeoSuggestIndex(index) {
    const box = qs('#rv2GeoAutocomplete');
    if (!box || !state.geoSuggestItems.length) return;
    const count = state.geoSuggestItems.length;
    state.geoSuggestIndex = ((index % count) + count) % count;
    qsa('.rv2-autocomplete-item', box).forEach((node,i) => {
      const active = i === state.geoSuggestIndex;
      node.classList.toggle('active', active);
      node.setAttribute('aria-selected', active ? 'true' : 'false');
      if (active) node.scrollIntoView({block:'nearest'});
    });
  }

  function renderGeoAutocomplete(body) {
    const box = geoAutocompleteBox();
    const input = qs('#rv2Value');
    if (!box || !input) return;
    box.textContent = '';
    const items = Array.isArray(body?.suggestions) ? body.suggestions.slice(0,24) : [];
    state.geoSuggestItems = items;
    state.geoSuggestIndex = items.length ? 0 : -1;

    items.forEach((item,index) => {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = `rv2-autocomplete-item${index === 0 ? ' active' : ''}`;
      button.setAttribute('role','option');
      button.setAttribute('aria-selected', index === 0 ? 'true' : 'false');
      const title = document.createElement('b');
      title.textContent = `${humanKind(state.kind)} · ${String(item.category || '')}`;
      const meta = document.createElement('span');
      const evidence = Array.isArray(item.evidence) && item.evidence.length ? ` · ${item.evidence.join(', ')}` : '';
      meta.textContent = `${String(item.file || defaultGeoDataFile(state.kind))}${evidence}`;
      button.append(title, meta);
      button.addEventListener('mousedown', event => event.preventDefault());
      button.addEventListener('click', () => selectGeoSuggestion(item, true));
      box.appendChild(button);
    });

    const warnings = Array.isArray(body?.warnings) ? body.warnings.filter(Boolean) : [];
    if (warnings.length) {
      const warn = document.createElement('div');
      warn.className = 'rv2-autocomplete-warning';
      warn.textContent = warnings.join(' · ');
      box.appendChild(warn);
    }
    box.hidden = !items.length && !warnings.length;
    input.setAttribute('aria-expanded', box.hidden ? 'false' : 'true');
    if (!box.hidden) positionGeoAutocomplete();
  }

  async function requestGeoSuggestions(query, renderInline = true) {
    if (!(state.kind === 'geosite' || state.kind === 'geoip')) return null;
    const kind = state.kind;
    const value = String(query || '').trim();
    if (value.length < 2) { if (renderInline) closeGeoAutocomplete(); return null; }
    if (state.geoSuggestController) state.geoSuggestController.abort();
    const controller = new AbortController();
    state.geoSuggestController = controller;
    try {
      const lower = value.toLowerCase();
      const explicitPrefix = lower.startsWith('geosite:') || lower.startsWith('geoip:') || lower.startsWith('ext:');
      const hostShaped = value.includes('://') || value.includes('/') || value.includes('.');
      const mode = explicitPrefix || !hostShaped ? '&mode=prefix' : '';
      const body = await api(`/api/geodata/suggest?kind=${encodeURIComponent(kind)}&q=${encodeURIComponent(value)}${mode}`, {signal:controller.signal});
      if (body.mutation !== 'NONE') throw new Error('Нарушен read-only GeoData contract.');
      const input = qs('#rv2Value');
      const currentValue = String(input?.value || '').trim();
      if (controller.signal.aborted || state.kind !== kind || currentValue !== value) return null;
      if (renderInline) {
        if (document.activeElement === input) renderGeoAutocomplete(body);
        else hideGeoAutocomplete();
        const items = Array.isArray(body?.suggestions) ? body.suggestions : [];
        const warnings = Array.isArray(body?.warnings) ? body.warnings.filter(Boolean) : [];
        if (items.length) {
          setNotice('rv2RuleNotice', `GeoData: найдено ${items.length}. Выберите категорию из подсказок.`, 'ok');
        } else if (warnings.length) {
          setNotice('rv2RuleNotice', `GeoData: ${warnings.join(' · ')}`, 'warn');
        } else {
          setNotice('rv2RuleNotice', 'GeoData: совпадений не найдено. Уточните запрос или введите категорию вручную.');
        }
      }
      return body;
    } catch (error) {
      if (controller.signal.aborted || error?.name === 'AbortError') return null;
      if (renderInline) {
        closeGeoAutocomplete();
        setNotice('rv2RuleNotice', `GeoData: ${safeError(error,'поиск недоступен')}. Текущая маршрутизация не меняется.`, 'bad');
      }
      throw error;
    } finally {
      if (state.geoSuggestController === controller) state.geoSuggestController = null;
    }
  }

  function queueGeoAutocomplete() {
    if (state.geoSuggestTimer) clearTimeout(state.geoSuggestTimer);
    if (state.geoSuggestController) {
      state.geoSuggestController.abort();
      state.geoSuggestController = null;
    }
    const input = qs('#rv2Value');
    const query = String(input?.value || '').trim();
    if (!(state.kind === 'geosite' || state.kind === 'geoip') || query.length < 2) {
      closeGeoAutocomplete();
      return;
    }
    // Never leave suggestions from the previous query visible while the new
    // request is debounced/in flight.
    hideGeoAutocomplete();
    setNotice('rv2RuleNotice', 'GeoData: ищу локальные категории…');
    state.geoSuggestTimer = setTimeout(() => {
      state.geoSuggestTimer = null;
      void requestGeoSuggestions(query, true).catch(() => {});
    }, 220);
  }

  function compactXrayVersion(raw) {
    const match = String(raw || '').match(/\bv?\d+(?:\.\d+){1,3}(?:[-+][0-9A-Za-z.-]+)?/);
    if (!match) return String(raw || '').trim() || '—';
    return match[0].startsWith('v') ? match[0] : `v${match[0]}`;
  }

  function xrayFriendlyTime(raw) {
    const date = new Date(raw);
    if (Number.isNaN(date.getTime())) return String(raw || '');
    return date.toLocaleString('ru-RU', {day:'2-digit', month:'2-digit', hour:'2-digit', minute:'2-digit'});
  }

  function renderXraySurface(result = state.xrayService) {
    if (!result) return;
    state.xrayService = result;
    const status = qs('#rv2XrayStatus');
    const version = qs('#rv2XrayVersion');
    if (status) {
      status.textContent = result.online ? 'Работает' : 'Остановлен';
      status.className = result.online ? 'ok' : 'bad';
    }
    if (version) version.textContent = compactXrayVersion(result.version);

    const start = qs('#rv2XrayStart');
    const stop = qs('#rv2XrayStop');
    const restart = qs('#rv2XrayRestart');
    const versions = qs('#rv2XrayVersions');
    const ownerReady = !!window.FreeNetXrayControl?.action;
    const blocked = !ownerReady || !!window.FreeNetXrayControl?.mutationBlocked?.();
    if (start) { start.hidden = !!result.online; start.disabled = state.xrayBusy || blocked; }
    if (stop) { stop.hidden = !result.online; stop.disabled = state.xrayBusy || blocked; }
    if (restart) { restart.hidden = !result.online; restart.disabled = state.xrayBusy || blocked; }
    if (versions) { versions.hidden = !result.online; versions.disabled = state.xrayBusy || blocked; }

    const events = qs('#rv2XrayEvents');
    if (events) {
      events.textContent = '';
      const rows = Array.isArray(result.events) ? result.events.slice(0,5) : [];
      if (!rows.length) {
        const empty = document.createElement('div');
        empty.className = 'rv2-copy';
        empty.textContent = 'Действий Xray пока нет.';
        events.appendChild(empty);
      } else rows.forEach(event => {
        const row = document.createElement('div');
        const ok = String(event?.result || '').toLowerCase() === 'success';
        row.className = `rv2-xray-event${ok ? '' : ' bad'}`;
        const at = document.createElement('span'); at.textContent = xrayFriendlyTime(event?.at);
        const outcome = document.createElement('b'); outcome.textContent = ok ? 'Успешно' : 'Ошибка';
        const message = document.createElement('span'); message.textContent = String(event?.message || '');
        row.append(at, outcome, message);
        events.appendChild(row);
      });
    }
  }

  async function loadXraySurface() {
    const notice = qs('#rv2XrayNotice');
    if (notice) { notice.className = 'rv2-notice show'; notice.textContent = 'Проверяю состояние Xray…'; }
    try {
      let result;
      if (window.FreeNetXrayControl?.refresh) result = await window.FreeNetXrayControl.refresh();
      else {
        result = await api('/api/xray/service');
        if (!result.success) throw new Error(result.error || 'Состояние Xray недоступно');
      }
      renderXraySurface(result);
      if (notice) { notice.className = 'rv2-notice'; notice.textContent = ''; }
      return true;
    } catch (error) {
      if (notice) { notice.className = 'rv2-notice show bad'; notice.textContent = safeError(error,'Состояние Xray недоступно.'); }
      return false;
    }
  }

  async function runXrayAction(action) {
    if (state.xrayBusy) return;
    const owner = window.FreeNetXrayControl;
    const notice = qs('#rv2XrayNotice');
    if (!owner?.action) {
      if (notice) { notice.className = 'rv2-notice show bad'; notice.textContent = 'Canonical Xray control ещё не готов. Никаких изменений не выполнено.'; }
      return;
    }

    state.xrayBusy = true;
    renderXraySurface();
    const labels = {start:'Запускаю Xray…',stop:'Останавливаю Xray…',restart:'Перезапускаю Xray…'};
    if (notice) { notice.className = 'rv2-notice show'; notice.textContent = labels[action] || 'Выполняю операцию Xray…'; }
    try {
      const result = await owner.action(action);
      renderXraySurface(result);
      if (notice) { notice.className = 'rv2-notice show ok'; notice.textContent = result.message || 'Операция Xray завершена.'; }
    } catch (error) {
      if (notice) { notice.className = 'rv2-notice show bad'; notice.textContent = `${safeError(error,'Состояние Xray не подтверждено.')} Сначала нажмите «Проверить состояние».`; }
    } finally {
      state.xrayBusy = false;
      renderXraySurface();
    }
  }

  document.addEventListener('freenet:xray-service-changed', event => {
    const detail = event && event.detail && typeof event.detail === 'object' ? event.detail : null;
    if (detail) renderXraySurface(detail);
  });

  function setMode(mode) {
    const selected = mode === 'xray' ? 'xray' : mode === 'config' ? 'config' : 'rules';
    qsa('.rv2-mode').forEach(button => button.classList.toggle('active', button.dataset.mode === selected));
    qs('#rv2XrayPanel').hidden = selected !== 'xray';
    qs('#rv2RulesPanel').hidden = selected !== 'rules';
    qs('#rv2ConfigPanel').hidden = selected !== 'config';

    if (selected === 'xray') {
      void loadXraySurface();
    } else if (selected === 'rules') {
      if (!hasRuleDraft()) void loadConfig(true);
      else renderLiveRules();
    } else if (!state.configLoaded) {
      void loadConfig();
    }

    document.dispatchEvent(new CustomEvent('freenet:routing-mode-changed', {detail:{mode:selected}}));
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

  async function loadConfig(force = false) {
    if (state.configLoadPromise) {
      const active = state.configLoadPromise;
      if (!force) return active;
      await active;
    }
    if (state.configLoadPromise) return loadConfig(force);

    state.configLoading = true;
    const task = (async () => {
      setConfigStatus('Загрузка…');
      setNotice('rv2ConfigNotice', '');
      renderLiveRules();
      try {
        const body = await api('/api/routing/config');
        if (body.mutation !== 'NONE') throw new Error('Нарушен read-only contract.');
        state.live.routing = normalizeLoadedSection(body.routing, 'routing', body.routing_present);
        state.live.policy = normalizeLoadedSection(body.policy, 'policy', body.policy_present);
        state.draft.routing = JSON.stringify(state.live.routing, null, 2);
        state.draft.policy = JSON.stringify(state.live.policy, null, 2);
        state.configLoaded = true;
        state.configDirty = false;
        state.configValidated = false;
        state.liveStale = false;
        showActiveEditor();
        renderLiveRules();
        const hashes = qs('#rv2ConfigHashes');
        if (hashes) hashes.textContent = `routing ${body.routing_sha256 ? String(body.routing_sha256).slice(0, 12) : 'new'} · policy ${body.policy_sha256 ? String(body.policy_sha256).slice(0, 12) : 'new'}`;
        setConfigStatus('Live загружен', 'ok');
        setNotice('rv2ConfigNotice', 'Загружены 05_routing.json и 06_policy.json. Редактирование здесь — экспертный режим; применение остаётся защищённым Xray validation и rollback.', 'ok');
        return true;
      } catch (error) {
        state.configLoaded = false;
        setConfigStatus('Недоступно', 'warn');
        renderLiveRules();
        setNotice('rv2LiveNotice', `Не удалось прочитать текущие правила: ${safeError(error, 'ошибка')}. Никаких изменений не выполнено.`, 'bad');
        setNotice('rv2ConfigNotice', `Не удалось загрузить managed sections: ${safeError(error, 'ошибка')}`, 'bad');
        return false;
      } finally {
        state.configLoading = false;
        state.configLoadPromise = null;
        renderLiveRules();
      }
    })();

    state.configLoadPromise = task;
    return task;
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
    const source = String(rule.selector?.source || '').trim();
    if (kind === 'domain') xray.domain = [`domain:${value}`];
    else if (kind === 'geosite') xray.domain = [source ? `ext:${source}:${value}` : `geosite:${value}`];
    else if (kind === 'geoip') xray.ip = [source ? `ext:${source}:${value}` : `geoip:${value}`];
    else if (kind === 'ip' || kind === 'cidr') xray.ip = [value];
    else throw new Error(`Неизвестный selector kind: ${kind}`);
    return xray;
  }

  function managedRuleFamily(rule) {
    if (Array.isArray(rule?.domain) && rule.domain.length === 1) return 'domain';
    if (Array.isArray(rule?.ip) && rule.ip.length === 1) return 'ip';
    return '';
  }

  function isSimpleMergeTarget(rule, family, outboundTag) {
    if (!rule || typeof rule !== 'object' || Array.isArray(rule)) return false;
    if (String(rule.type || '') !== 'field' || String(rule.outboundTag || '') !== String(outboundTag || '')) return false;
    const allowed = new Set(['type','outboundTag',family]);
    if (Object.keys(rule).some(key => !allowed.has(key))) return false;
    return Array.isArray(rule[family]);
  }

  function hasEarlierFamilyConflict(rules, beforeIndex, family, outboundTag) {
    for (let i = 0; i < beforeIndex; i++) {
      const rule = rules[i];
      const values = Array.isArray(rule?.[family]) ? rule[family] : [];
      if (!values.length) continue;
      if (String(rule?.outboundTag || '') !== String(outboundTag || '')) return true;
    }
    return false;
  }

  function mergeManagedRulesSafely(existingRules, managedRules) {
    const existing = existingRules.map(clone);
    const prepend = [];
    let mergedCount = 0;

    managedRules.forEach(managed => {
      const family = managedRuleFamily(managed);
      const outboundTag = String(managed?.outboundTag || '');
      const selector = family ? String(managed[family][0] || '') : '';
      if (!family || !selector) {
        prepend.push(managed);
        return;
      }

      let target = -1;
      for (let i = 0; i < existing.length; i++) {
        if (!isSimpleMergeTarget(existing[i], family, outboundTag)) continue;
        if (hasEarlierFamilyConflict(existing, i, family, outboundTag)) continue;
        target = i;
        break;
      }

      if (target < 0) {
        prepend.push(managed);
        return;
      }
      if (!existing[target][family].some(value => String(value) === selector)) existing[target][family].push(selector);
      mergedCount++;
    });

    return {rules: prepend.concat(existing), mergedCount, prependedCount: prepend.length};
  }

  async function buildRoutingDraftFromRules(openConfig = true) {
    if (!hasRuleDraft() || (state.rules.length > 0 && !state.compiled)) {
      setNotice('rv2RulesApplyResult', 'Сначала добавьте или отметьте для удаления хотя бы одно правило.', 'bad');
      return null;
    }
    if (!state.configLoaded) await loadConfig();
    if (!state.configLoaded) return null;
    try {
      const base = clone(state.live.routing);
      if (!base.routing || typeof base.routing !== 'object') base.routing = {};
      const originalExisting = Array.isArray(base.routing.rules) ? clone(base.routing.rules) : [];
      const removalsByRule = new Map();
      state.removals.forEach(removal => {
        if (!removalsByRule.has(removal.ruleIndex)) removalsByRule.set(removal.ruleIndex, []);
        removalsByRule.get(removal.ruleIndex).push(removal);
      });

      const existing = [];
      originalExisting.forEach((rule, index) => {
        const targets = removalsByRule.get(index) || [];
        if (!targets.length) {
          existing.push(rule);
          return;
        }
        const presented = presentLiveRule(rule, index);
        if (presented.complex || !['DIRECT','VPN','BLOCK'].includes(presented.action)) {
          throw new Error('Служебное или сложное правило нельзя удалить быстрым действием.');
        }
        const next = clone(rule);
        targets.forEach(target => {
          const key = target.family === 'domain' ? 'domain' : 'ip';
          const values = Array.isArray(next[key]) ? next[key] : [];
          next[key] = values.filter(value => String(value) !== String(target.raw));
          if (!next[key].length) delete next[key];
        });
        const selectorCount = (Array.isArray(next.domain) ? next.domain.length : 0) + (Array.isArray(next.ip) ? next.ip.length : 0);
        if (selectorCount > 0) existing.push(next);
      });

      const managed = state.rules.length ? (state.compiled.payload || []).map(compiledRuleToXray) : [];
      const merged = mergeManagedRulesSafely(existing, managed);
      base.routing.rules = merged.rules;
      state.draft.routing = JSON.stringify(base, null, 2);
      state.draft.policy = JSON.stringify(state.live.policy, null, 2);
      state.configDirty = true; state.configValidated = false; state.configTab = 'routing'; showActiveEditor();
      setConfigStatus('Черновик', 'warn');
      const removalCount = removalGroups().length;
      const parts = [];
      if (managed.length) parts.push(`добавить ${managed.length}`);
      if (removalCount) parts.push(`удалить ${removalCount}`);
      const copy = `Изменения: ${parts.join(', ')}. Остальные пользовательские и все служебные правила сохраняются без изменений.`;
      setNotice('rv2RulesApplyResult', copy, 'ok');
      setNotice('rv2ConfigNotice', `${copy} Это пока только черновик.`, 'ok');
      const preview = qs('#rv2RulesApplyPreview'); if (preview) preview.textContent = `${copy} FreeNet проверит candidate перед записью автоматически.`;
      if (openConfig) setMode('config');
      return {
        routing: base,
        policy: clone(state.live.policy),
        managedCount: managed.length,
        mergedCount: merged.mergedCount,
        prependedCount: merged.prependedCount,
        removedCount: removalCount,
        existingCount: originalExisting.length,
        remainingCount: existing.length
      };
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
    if (!hasRuleDraft() || (state.rules.length > 0 && !state.compiled)) {
      syncRuleActionButtons();
      setNotice('rv2RulesApplyResult', 'Сначала добавьте правило или отметьте live-chip для удаления.', 'bad');
      return;
    }
    const prepared = await buildRoutingDraftFromRules(false);
    if (!prepared) return;
    const ok = await validateConfig();
    if (ok) {
      const changes = [];
      if (prepared.managedCount) changes.push(`добавить: ${prepared.managedCount}`);
      if (prepared.removedCount) changes.push(`удалить: ${prepared.removedCount}`);
      setNotice('rv2RulesApplyResult', `Проверка пройдена · ${changes.join(' · ')}. Остальные правила сохраняются без изменений.`, 'ok');
      const preview = qs('#rv2RulesApplyPreview'); if (preview) preview.textContent = `Проверка Xray пройдена. ${changes.join(' · ')}. Остальные пользовательские и служебные правила сохраняются без изменений.`;
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
        <div class="rv2-modes"><button type="button" class="rv2-mode" data-mode="xray">Xray</button><button type="button" class="rv2-mode active" data-mode="rules">Правила</button><button type="button" class="rv2-mode" data-mode="config">Конфигурация</button></div>
      </div>
      <section id="rv2XrayPanel" class="rv2-panel" hidden>
        <div class="rv2-card">
          <div class="rv2-card-head">
            <div><h2>Управление Xray</h2><p class="rv2-copy">Сервис, версия и последние действия — отдельно от экспертной конфигурации.</p></div>
          </div>
          <div class="rv2-xray-grid">
            <div class="rv2-xray-fact"><span>Состояние</span><strong id="rv2XrayStatus">Проверяю…</strong></div>
            <div class="rv2-xray-fact"><span>Версия</span><strong id="rv2XrayVersion">—</strong></div>
          </div>
          <div class="rv2-xray-actions">
            <button id="rv2XrayStart" class="btn primary" type="button" hidden>Запустить Xray</button>
            <button id="rv2XrayRestart" class="btn secondary" type="button" hidden>Перезапустить</button>
            <button id="rv2XrayStop" class="btn secondary" type="button" hidden>Остановить</button>
            <button id="rv2XrayVersions" class="btn secondary" type="button" hidden>Обновить</button>
          </div>
          <div class="rv2-card-head" style="margin-top:16px"><div><h2>Последние действия Xray</h2></div></div>
          <div id="rv2XrayEvents" class="rv2-xray-events"></div>
          <div id="rv2XrayNotice" class="rv2-notice"></div>
        </div>
      </section>
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
            <section id="rv2DirectBoard" class="rv4-board direct collapsed">
              <div class="rv4-board-head" role="button" tabindex="0" data-board-toggle="DIRECT" aria-expanded="false" aria-controls="rv2DirectContent" aria-label="Напрямую: раскрыть или скрыть правила">
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

            <section id="rv2VPNBoard" class="rv4-board vpn collapsed">
              <div class="rv4-board-head" role="button" tabindex="0" data-board-toggle="VPN" aria-expanded="false" aria-controls="rv2VPNContent" aria-label="Через VPN: раскрыть или скрыть правила">
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

            <section id="rv2BlockBoard" class="rv4-board block collapsed">
              <div class="rv4-board-head" role="button" tabindex="0" data-board-toggle="BLOCK" aria-expanded="false" aria-controls="rv2BlockContent" aria-label="Блокировать: раскрыть или скрыть правила">
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
                <div class="rv4-field"><label for="rv2Value">Сайт или категория</label><div class="rv2-input-wrap"><input id="rv2Value" type="text" autocomplete="off" spellcheck="false" aria-autocomplete="list" aria-controls="rv2GeoAutocomplete" aria-expanded="false"><div id="rv2GeoAutocomplete" class="rv2-autocomplete" role="listbox" hidden></div></div></div>
                <button id="rv2AddRule" class="btn primary rv4-composer-submit" type="button">Добавить</button>
              </div>
              <div class="rv2-search"><button id="rv2CancelEdit" class="btn secondary" type="button" hidden>Отмена редактирования</button></div>
              <div id="rv2RuleNotice" class="rv2-notice"></div>
            </div>
          </div>

        </div>

        <div id="rv2DraftCard" class="rv2-card rv2-draft-card" hidden>
          <div class="rv2-card-head">
            <div>
              <div class="rv2-draft-head"><h2>Черновик изменений</h2><span id="rv2DraftCount" class="rv2-draft-count">0</span></div>
              <p class="rv2-copy">Изменения ещё не сохранены. FreeNet сам проверит Xray перед применением.</p>
            </div>
          </div>
          <div id="rv2RuleList" class="rv2-rule-list"></div><div id="rv2CompiledSummary" class="rv2-compiled" hidden></div>
          <div class="rv2-rule-footer">
            <div id="rv2RulesApplyPreview" class="rv2-rule-footer-copy">Текущая маршрутизация не изменена.</div>
            <div class="rv2-rule-footer-actions"><button id="rv2ApplyRules" class="btn primary" type="button" disabled>Применить</button></div>
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
    qs('#rv2XrayStart')?.addEventListener('click', () => runXrayAction('start'));
    qs('#rv2XrayStop')?.addEventListener('click', () => runXrayAction('stop'));
    qs('#rv2XrayRestart')?.addEventListener('click', () => runXrayAction('restart'));
    qs('#rv2XrayVersions')?.addEventListener('click', () => window.FreeNetXrayControl?.openVersions?.());
    qsa('.rv4-board-add').forEach(button => button.addEventListener('click', () => openInlineComposer(button.dataset.addAction || 'DIRECT')));
    qsa('[data-board-toggle]').forEach(header => {
      const toggle = event => {
        // The independent Add action must not toggle or close the board.
        if (event.target.closest?.('button,a,input,select,textarea,label')) return;
        toggleBoard(header.dataset.boardToggle || 'DIRECT');
      };
      header.addEventListener('click', toggle);
      header.addEventListener('keydown', event => {
        if (event.target !== header || !['Enter', ' '].includes(event.key)) return;
        event.preventDefault();
        toggleBoard(header.dataset.boardToggle || 'DIRECT');
      });
    });
    qs('#rv2ComposerClose')?.addEventListener('click', () => closeInlineComposer(true));
    qs('#rv2Kind')?.addEventListener('change', event => {
      state.kind = String(event.target.value || 'domain');
      state.selectedSource = '';
      closeGeoAutocomplete();
      syncKindOptions();
      queueGeoAutocomplete();
    });
    qs('#rv2AddRule')?.addEventListener('click', addOrUpdateRule);
    qs('#rv2CancelEdit')?.addEventListener('click', () => closeInlineComposer(true));
    qs('#rv2Value')?.addEventListener('input', () => {
      state.selectedSource = '';
      syncRuleActionButtons();
      queueGeoAutocomplete();
    });
    qs('#rv2Value')?.addEventListener('keydown', event => {
      const geoMode = state.kind === 'geosite' || state.kind === 'geoip';
      const box = qs('#rv2GeoAutocomplete');
      const open = geoMode && box && !box.hidden && state.geoSuggestItems.length > 0;
      if (open && (event.key === 'ArrowDown' || event.key === 'ArrowUp')) {
        event.preventDefault();
        setGeoSuggestIndex(state.geoSuggestIndex + (event.key === 'ArrowDown' ? 1 : -1));
        return;
      }
      if (open && (event.key === 'Enter' || event.key === 'Tab')) {
        event.preventDefault();
        selectGeoSuggestion(state.geoSuggestItems[state.geoSuggestIndex >= 0 ? state.geoSuggestIndex : 0], true);
        return;
      }
      if (geoMode && event.key === 'Escape') {
        event.preventDefault();
        closeGeoAutocomplete();
        setNotice('rv2RuleNotice', '');
        return;
      }
      if (event.key === 'Enter' && !geoMode) {
        event.preventDefault();
        addOrUpdateRule();
      }
    });
    qs('#rv2Value')?.addEventListener('focus', () => {
      if (state.geoSuggestBlurTimer) { clearTimeout(state.geoSuggestBlurTimer); state.geoSuggestBlurTimer = null; }
      const input = qs('#rv2Value');
      const query = String(input?.value || '').trim();
      if ((state.kind === 'geosite' || state.kind === 'geoip') && query.length >= 2 && !state.geoSuggestController) {
        queueGeoAutocomplete();
      }
    });
    qs('#rv2Value')?.addEventListener('blur', () => {
      if (state.geoSuggestBlurTimer) clearTimeout(state.geoSuggestBlurTimer);
      state.geoSuggestBlurTimer = setTimeout(() => {
        state.geoSuggestBlurTimer = null;
        // Losing focus is presentation-only. Do not cancel a read-only lookup:
        // its completion must still resolve the visible search status instead of
        // leaving Rules stuck on "ищу локальные категории…".
        hideGeoAutocomplete();
      }, 120);
    });
    qsa('.rv2-config-tab').forEach(button => button.addEventListener('click', () => switchConfigTab(button.dataset.configTab)));
    qs('#rv2FormatConfig')?.addEventListener('click', formatActiveConfig);
    qs('#rv2ValidateConfig')?.addEventListener('click', validateConfig);
    qs('#rv2ReloadConfig')?.addEventListener('click', resetConfigDraft);
    qs('#rv2ConfigEditor')?.addEventListener('input', () => { state.configDirty = true; state.configValidated = false; storeEditor(); setConfigStatus('Черновик', 'warn'); });
    window.addEventListener('resize', positionGeoAutocomplete);
    document.addEventListener('scroll', positionGeoAutocomplete, true);
  }


  async function reconcileExternalLiveIfSafe() {
    if (!state.liveStale || hasRuleDraft()) return false;
    const ok = await loadConfig(true);
    if (ok) setNotice('rv2RulesApplyResult', 'Live-маршрутизация синхронизирована с конфигурацией Xray.', 'ok');
    return ok;
  }

  async function handleExternalConfigApplied(event) {
    const detail = event && event.detail && typeof event.detail === 'object' ? event.detail : {};
    if (detail.source === 'rules') return;
    const files = Array.isArray(detail.files) ? detail.files.map(String) : [];
    if (files.length && !files.some(name => name === '05_routing' || name === '06_policy')) return;

    if (hasRuleDraft()) {
      state.liveStale = true;
      syncRuleActionButtons();
      setNotice('rv2RulesApplyResult', 'Live-конфигурация изменилась в «Конфигурация Xray». Текущий черновик правил сохранён, но применение заблокировано до отмены или повторного создания черновика.', 'bad');
      return;
    }

    state.liveStale = false;
    await loadConfig(true);
  }

  document.addEventListener('freenet:xray-config-applied', event => { void handleExternalConfigApplied(event); });

  async function prepareRulesCandidateForApply() {
    if (!hasRuleDraft() || (state.rules.length > 0 && !state.compiled)) {
      syncRuleActionButtons();
      setNotice('rv2RulesApplyResult', 'Нет готовых изменений для применения.', 'bad');
      return false;
    }
    const prepared = await buildRoutingDraftFromRules(false);
    if (!prepared) return false;
    return await validateConfig();
  }

  async function refreshRulesAfterApply(message = '') {
    state.rules = [];
    state.compiled = null;
    state.removals = [];
    state.editing = -1;
    state.selectedSource = '';
    state.liveStale = false;
    closeInlineComposer(true);
    renderRuleList();
    renderCompileState();
    await loadConfig(true);
    setMode('rules');
    setNotice('rv2RulesApplyResult', message || 'Изменения применены.', 'ok');
    document.dispatchEvent(new CustomEvent('freenet:xray-config-applied', {
      detail:{source:'rules', files:['05_routing','06_policy']}
    }));
  }

  window.FreeNetRoutingV2 = Object.assign(window.FreeNetRoutingV2 || {}, {
    prepareRulesCandidate: prepareRulesCandidateForApply,
    refreshAfterApply: refreshRulesAfterApply
  });

  function mount() {
    const page = qs('[data-page-view="network"]');
    if (!page || qs('#routingV2Workspace')) return;
    ; page.classList.add('fn-routing-v2'); page.dataset.routingV2 = '1';
    mountMarkup(page); bind(); syncKindOptions(); renderRuleList(); renderCompileState(); renderLiveRules();
    ['DIRECT','VPN','BLOCK'].forEach(action => setBoardCollapsed(action, state.collapsed[action]));
    void loadConfig();
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mount);
  else mount();
})();