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
    configValidated: false
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

  function modeMeta() {
    if (state.family === 'ip') {
      return {
        kinds: [
          ['ip', 'IP'], ['cidr', 'CIDR'], ['geoip', 'GeoIP']
        ],
        placeholder: state.kind === 'geoip' ? 'Например, ru или private' : (state.kind === 'cidr' ? 'Например, 10.20.0.0/16' : 'Например, 1.1.1.1')
      };
    }
    return {
      kinds: [['domain', 'Домен'], ['geosite', 'GeoSite']],
      placeholder: state.kind === 'geosite' ? 'Например, youtube' : 'Например, plati.market'
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
    if (!value) throw new Error('Введите selector.');
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
      renderRuleList();
      renderCompileState();
      setNotice('rv2RuleNotice', successCopy || 'Candidate проверен серверным compiler. MUTATION: NONE', 'ok');
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
    const ok = await compileCandidate(candidate, state.editing >= 0 ? 'Правило обновлено в candidate draft. MUTATION: NONE' : 'Правило добавлено в candidate draft. MUTATION: NONE');
    if (ok) resetEditor();
  }

  async function deleteRule(index) {
    const candidate = state.rules.map(clone);
    candidate.splice(index, 1);
    await compileCandidate(candidate, 'Правило удалено из candidate draft. MUTATION: NONE');
    resetEditor();
  }

  async function moveRule(index, delta) {
    const target = index + delta;
    if (target < 0 || target >= state.rules.length) return;
    const candidate = state.rules.map(clone);
    const [item] = candidate.splice(index, 1);
    candidate.splice(target, 0, item);
    await compileCandidate(candidate, 'Порядок first-match обновлён. MUTATION: NONE');
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
    setNotice('rv2RuleNotice', `Редактируется правило #${index + 1}. Live config не меняется.`);
  }

  function ruleDetails(index) {
    const compiled = state.compiled?.rules?.[index];
    if (!compiled) return 'candidate ещё не скомпилирован';
    const dns = compiled.dns_leg ? ` · DNS: ${compiled.dns_leg}` : ' · DNS: —';
    return `payload: ${compiled.payload_outbound || '—'}${dns}`;
  }

  function renderRuleList() {
    const list = qs('#rv2RuleList'); if (!list) return;
    list.textContent = '';
    if (!state.rules.length) {
      const empty = document.createElement('div'); empty.className = 'rv2-rule-empty';
      empty.textContent = 'Candidate пуст. Добавьте первое правило — live routing останется без изменений.';
      list.appendChild(empty); return;
    }
    state.rules.forEach((rule, index) => {
      const row = document.createElement('div'); row.className = 'rv2-rule'; row.dataset.ruleIndex = String(index);
      const order = document.createElement('div'); order.className = 'rv2-order'; order.textContent = String(index + 1);
      const selector = document.createElement('div'); selector.className = 'rv2-selector';
      const strong = document.createElement('b'); strong.textContent = `${rule.selector.kind}:${rule.selector.value}`;
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

  function renderCompileState() {
    const stateNode = qs('#rv2CompileState'); const summary = qs('#rv2CompiledSummary');
    if (!stateNode || !summary) return;
    if (!state.rules.length) {
      stateNode.className = 'rv2-state'; stateNode.textContent = 'Draft пуст';
      summary.innerHTML = '<strong>Server compiler</strong> будет вызван при добавлении первого правила. MUTATION: NONE.'; return;
    }
    if (state.compiled) {
      stateNode.className = 'rv2-state ok'; stateNode.textContent = 'Candidate валиден';
      const payload = Array.isArray(state.compiled.payload) ? state.compiled.payload.length : 0;
      const dns = Array.isArray(state.compiled.dns) ? state.compiled.dns.length : 0;
      summary.innerHTML = `<strong>${state.rules.length} правил</strong> · payload: ${payload} · Split DNS legs: ${dns} · порядок first-match сохранён · MUTATION: NONE`;
    } else {
      stateNode.className = 'rv2-state warn'; stateNode.textContent = 'Не проверен';
      summary.textContent = 'Candidate требует server compile.';
    }
  }

  async function searchGeo() {
    if (!(state.kind === 'geosite' || state.kind === 'geoip')) return;
    const query = String(qs('#rv2Value')?.value || '').trim();
    if (!query) { setNotice('rv2RuleNotice', 'Введите category или часть названия для поиска.', 'bad'); return; }
    const results = qs('#rv2SearchResults'); if (results) results.textContent = '';
    try {
      const body = await api(`/api/geodata/search?kind=${encodeURIComponent(state.kind)}&q=${encodeURIComponent(query)}`);
      let count = 0;
      (body.matches || []).forEach(match => (match.categories || []).forEach(category => {
        count++;
        const button = document.createElement('button'); button.type = 'button'; button.className = 'rv2-search-result';
        button.innerHTML = `<b>${state.kind}:${String(category)}</b><span>${String(match.file || 'geodata')} · выбрать category</span>`;
        button.addEventListener('click', () => {
          const input = qs('#rv2Value'); if (input) input.value = String(category);
          state.selectedSource = String(match.file || '');
          qsa('.rv2-search-result').forEach(node => node.classList.remove('active')); button.classList.add('active');
          setNotice('rv2RuleNotice', `Выбрана category ${category}. Добавление в candidate произойдёт только по кнопке. MUTATION: NONE`);
        });
        results?.appendChild(button);
      }));
      if (!count) setNotice('rv2RuleNotice', 'Совпадений нет. Category можно ввести вручную; server compiler всё равно проверит selector.');
    } catch (error) {
      setNotice('rv2RuleNotice', `Поиск geodata сейчас недоступен: ${safeError(error, 'ошибка')}. Category можно ввести вручную; live config не меняется.`, 'bad');
    }
  }

  function setMode(mode) {
    const selected = mode === 'config' ? 'config' : 'rules';
    qsa('.rv2-mode').forEach(button => button.classList.toggle('active', button.dataset.mode === selected));
    qs('#rv2RulesPanel').hidden = selected !== 'rules';
    qs('#rv2ConfigPanel').hidden = selected !== 'config';
    if (selected === 'config' && !state.configLoaded) loadConfig();
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
    state.configLoading = true; setConfigStatus('Загрузка…'); setNotice('rv2ConfigNotice', '');
    try {
      const body = await api('/api/routing/config');
      if (body.mutation !== 'NONE') throw new Error('Нарушен read-only contract.');
      state.live.routing = normalizeLoadedSection(body.routing, 'routing', body.routing_present);
      state.live.policy = normalizeLoadedSection(body.policy, 'policy', body.policy_present);
      state.draft.routing = JSON.stringify(state.live.routing, null, 2);
      state.draft.policy = JSON.stringify(state.live.policy, null, 2);
      state.configLoaded = true; state.configDirty = false; state.configValidated = false;
      showActiveEditor();
      const hashes = qs('#rv2ConfigHashes');
      if (hashes) hashes.textContent = `routing ${body.routing_sha256 ? String(body.routing_sha256).slice(0, 12) : 'new'} · policy ${body.policy_sha256 ? String(body.policy_sha256).slice(0, 12) : 'new'}`;
      setConfigStatus('Live загружен', 'ok');
      setNotice('rv2ConfigNotice', 'Загружены только 05_routing.json и 06_policy.json. 04_outbounds и credentials не выдаются API. MUTATION: NONE', 'ok');
    } catch (error) {
      setConfigStatus('Недоступно', 'warn');
      setNotice('rv2ConfigNotice', `Не удалось загрузить managed sections: ${safeError(error, 'ошибка')}`, 'bad');
    } finally { state.configLoading = false; }
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

  async function buildRoutingDraftFromRules() {
    if (!state.compiled || !state.rules.length) { setNotice('rv2ConfigNotice', 'Сначала добавьте и проверьте хотя бы одно правило.', 'bad'); return; }
    if (!state.configLoaded) await loadConfig();
    if (!state.configLoaded) return;
    try {
      const base = clone(state.live.routing);
      if (!base.routing || typeof base.routing !== 'object') base.routing = {};
      const existing = Array.isArray(base.routing.rules) ? base.routing.rules : [];
      const managed = (state.compiled.payload || []).map(compiledRuleToXray);
      base.routing.rules = managed.concat(existing);
      state.draft.routing = JSON.stringify(base, null, 2);
      state.configDirty = true; state.configValidated = false; state.configTab = 'routing'; showActiveEditor(); setMode('config');
      setConfigStatus('Черновик', 'warn');
      setNotice('rv2ConfigNotice', `Собран candidate 05_routing.json: ${managed.length} FreeNet rules поставлены перед ${existing.length} существующими правилами, чтобы сохранить first-match. Это только browser draft; live config не изменён.`, 'ok');
    } catch (error) { setNotice('rv2ConfigNotice', safeError(error, 'Не удалось собрать candidate'), 'bad'); }
  }

  async function validateConfig() {
    storeEditor();
    let routing, policy;
    try { routing = parseDraft('routing'); policy = parseDraft('policy'); }
    catch (error) { setNotice('rv2ConfigNotice', error.message, 'bad'); setConfigStatus('JSON ошибка', 'warn'); return; }
    const button = qs('#rv2ValidateConfig'); if (button) { button.disabled = true; button.textContent = 'Проверяем…'; }
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
      setNotice('rv2ConfigNotice', 'Candidate прошёл `xray run -test -confdir` во временной копии текущего config dir. Live 05/06 не изменены. MUTATION: NONE', 'ok');
    } catch (error) {
      state.configValidated = false; setConfigStatus('Не валиден', 'warn');
      setNotice('rv2ConfigNotice', `Candidate не прошёл проверку: ${safeError(error, 'ошибка Xray validation')}. Live config не изменён.`, 'bad');
    } finally { if (button) { button.disabled = false; button.textContent = 'Проверить Xray'; } }
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
    if (head) head.innerHTML = '<div><div class="page-kicker">ROUTING POLICY</div><h1>Маршрутизация</h1><p>Правила DIRECT / VPN / BLOCK и экспертный Config Studio с безопасной Xray validation.</p></div>';

    const root = document.createElement('div'); root.id = 'routingV2Workspace'; root.className = 'rv2-workspace';
    root.innerHTML = `
      <div class="rv2-modebar">
        <div class="rv2-modes"><button type="button" class="rv2-mode active" data-mode="rules">Правила</button><button type="button" class="rv2-mode" data-mode="config">Конфигурация</button></div>
        <span class="rv2-state ok">Draft · MUTATION: NONE</span>
      </div>
      <section id="rv2RulesPanel" class="rv2-panel">
        <div class="rv2-card">
          <div class="rv2-card-head"><div><h2>Конструктор правил</h2><p class="rv2-copy">Ordered first-match policy. Правила проверяются серверным compiler до попадания в candidate.</p></div><span id="rv2CompileState" class="rv2-state">Draft пуст</span></div>
          <div class="rv2-tabs" style="margin-top:13px"><button type="button" class="rv2-tab rv2-family active" data-family="domain">Домен / GeoSite</button><button type="button" class="rv2-tab rv2-family" data-family="ip">IP / GeoIP</button></div>
          <div class="rv2-builder-grid"><select id="rv2Kind" aria-label="Тип selector"></select><input id="rv2Value" type="text" autocomplete="off" spellcheck="false"><button id="rv2AddRule" class="btn primary" type="button">Добавить правило</button></div>
          <div class="rv2-search"><button id="rv2GeoSearch" class="btn secondary" type="button" hidden>Найти в GeoData</button><button id="rv2CancelEdit" class="btn secondary" type="button" hidden>Отмена редактирования</button></div>
          <div id="rv2SearchResults" class="rv2-search-results"></div>
          <div style="margin-top:13px"><div class="eyebrow">Действие</div><div class="rv2-actions" style="margin-top:7px"><button type="button" class="rv2-action active" data-action="DIRECT">DIRECT</button><button type="button" class="rv2-action" data-action="VPN">VPN</button><button type="button" class="rv2-action" data-action="BLOCK">BLOCK</button></div></div>
          <div id="rv2RuleNotice" class="rv2-notice"></div>
        </div>
        <div class="rv2-card">
          <div class="rv2-card-head"><div><h2>Candidate rules</h2><p class="rv2-copy">Порядок сверху вниз является first-match order.</p></div><button id="rv2BuildConfig" class="btn secondary" type="button">Собрать 05_routing draft</button></div>
          <div id="rv2RuleList" class="rv2-rule-list"></div><div id="rv2CompiledSummary" class="rv2-compiled"></div>
        </div>
      </section>
      <section id="rv2ConfigPanel" class="rv2-panel" hidden>
        <div class="rv2-card">
          <div class="rv2-card-head"><div><h2>Config Studio</h2><p class="rv2-copy">Редактирование только managed non-secret sections. По образцу XKeen UI, но с FreeNet candidate validation.</p></div><span id="rv2ConfigState" class="rv2-state">Не загружено</span></div>
          <div class="rv2-modebar" style="margin-top:10px"><div class="rv2-tabs"><button type="button" class="rv2-tab rv2-config-tab active" data-config-tab="routing">05_routing</button><button type="button" class="rv2-tab rv2-config-tab" data-config-tab="policy">06_policy</button></div><div class="rv2-toolbar"><button id="rv2FormatConfig" class="btn secondary" type="button">Формат</button><button id="rv2ValidateConfig" class="btn secondary" type="button">Проверить Xray</button><button id="rv2ReloadConfig" class="btn secondary" type="button">Сбросить draft</button></div></div>
          <div class="rv2-editor-wrap"><textarea id="rv2ConfigEditor" class="rv2-editor" spellcheck="false" aria-label="Routing Config Studio"></textarea></div>
          <div class="rv2-config-meta"><span>Файл: <code id="rv2ConfigFile">05_routing.json</code></span><span id="rv2ConfigHashes">snapshot не загружен</span></div>
          <div class="rv2-danger-note">В этом цикле <b>нет Save/Apply</b>: editor меняет только browser candidate. `04_outbounds.json`, subscription URL, VLESS UUID и Reality credentials не выдаются этому API вообще.</div>
          <div id="rv2ConfigNotice" class="rv2-notice"></div>
        </div>
      </section>`;
    if (head) head.insertAdjacentElement('afterend', root); else page.prepend(root);
  }

  function bind() {
    qsa('.rv2-mode').forEach(button => button.addEventListener('click', () => setMode(button.dataset.mode)));
    qsa('.rv2-family').forEach(button => button.addEventListener('click', () => setFamily(button.dataset.family)));
    qsa('.rv2-action').forEach(button => button.addEventListener('click', () => setAction(button.dataset.action)));
    qs('#rv2Kind')?.addEventListener('change', event => { state.kind = String(event.target.value || 'domain'); state.selectedSource = ''; syncKindOptions(); });
    qs('#rv2AddRule')?.addEventListener('click', addOrUpdateRule);
    qs('#rv2CancelEdit')?.addEventListener('click', resetEditor);
    qs('#rv2GeoSearch')?.addEventListener('click', searchGeo);
    qs('#rv2Value')?.addEventListener('keydown', event => { if (event.key === 'Enter' && !(state.kind === 'geosite' || state.kind === 'geoip')) { event.preventDefault(); addOrUpdateRule(); } });
    qs('#rv2BuildConfig')?.addEventListener('click', buildRoutingDraftFromRules);
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
    mountMarkup(page); bind(); syncKindOptions(); renderRuleList(); renderCompileState();
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mount);
  else mount();
})();
