(() => {
  'use strict';

  const qs = (s, root = document) => root.querySelector(s);
  const JSON_TABS = new Set(['01_log', '02_dns', '03_inbounds', '04_outbounds', '05_routing', '06_policy']);
  const ROUTING_TABS = new Set(['05_routing', '06_policy']);
  const MAIN_TAB_ORDER = ['01_log', '02_dns', '03_inbounds', '04_outbounds', '05_routing', '06_policy'];
  const LIST_TAB_ORDER = ['ip_exclude', 'port_exclude', 'port_proxying'];

  const state = {
    mounted: false,
    externalBusy: false,
    applying: false,
    loading: false,
    loadPromise: null,
    active: '',
    tabs: [],
    live: new Map(),
    draft: new Map(),
    valid: new Set(),
    errors: new Map(),
    stale: false,
    draftRevision: 0,
    geoSuggestTimer: null,
    geoSuggestController: null,
    geoSuggestItems: [],
    geoSuggestIndex: -1,
    geoSuggestToken: null,
    xray: {online: false, version: ''},
    editorHeight: 0
  };
  let editorResizeObserver = null;
  try {
    const savedHeight = Number(sessionStorage.getItem('freenet.configStudio.editorHeight') || 0);
    if (savedHeight >= 340 && savedHeight <= 920) state.editorHeight = savedHeight;
  } catch (_) {}

  

  function escapeHTML(text) {
    return String(text).replace(/[&<>]/g, ch => ({'&':'&amp;','<':'&lt;','>':'&gt;'}[ch]));
  }

  function highlightJSON(text) {
    const src = String(text || '');
    let out = '';
    let i = 0;
    while (i < src.length) {
      const ch = src[i];
      if (ch === '"') {
        let j = i + 1;
        let escaped = false;
        while (j < src.length) {
          const c = src[j];
          if (!escaped && c === '"') { j += 1; break; }
          if (!escaped && c === '\\') escaped = true; else escaped = false;
          j += 1;
        }
        const token = src.slice(i, j);
        let k = j;
        while (/\s/.test(src[k] || '')) k += 1;
        const cls = src[k] === ':' ? 'cs-key' : 'cs-string';
        out += `<span class="${cls}">${escapeHTML(token)}</span>`;
        i = j;
        continue;
      }
      const rest = src.slice(i);
      const number = rest.match(/^-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?/);
      if (number) { out += `<span class="cs-number">${number[0]}</span>`; i += number[0].length; continue; }
      const keyword = rest.match(/^(true|false|null)\b/);
      if (keyword) { out += `<span class="${keyword[1] === 'null' ? 'cs-null' : 'cs-bool'}">${keyword[1]}</span>`; i += keyword[1].length; continue; }
      if ('{}[],:'.includes(ch)) { out += `<span class="cs-punct">${ch}</span>`; i += 1; continue; }
      out += escapeHTML(ch);
      i += 1;
    }
    return out + (src.endsWith('\n') ? '' : '\n');
  }

  function syntaxDiagnostic(text) {
    try { JSON.parse(text); return null; } catch (error) {
      const message = String(error && error.message || 'Некорректный JSON');
      let line = 0, column = 0, position = -1;
      const pos = message.match(/position\s+(\d+)/i);
      if (pos) position = Number(pos[1]);
      const lc = message.match(/line\s+(\d+).*column\s+(\d+)/i);
      if (lc) { line = Number(lc[1]); column = Number(lc[2]); }
      if (position >= 0 && !line) {
        const before = text.slice(0, position);
        line = before.split('\n').length;
        const at = before.lastIndexOf('\n');
        column = position - at;
      }
      return {message, line, column, position};
    }
  }

  function normalizeJSONString(value) {
    return JSON.stringify(value, null, 2) + '\n';
  }

  async function api(path, options = {}) {
    const response = await fetch(path, Object.assign({cache: 'no-store'}, options));
    let body = {};
    try { body = await response.json(); } catch (_) {}
    if (!response.ok || body.success === false) throw new Error(body.error || `HTTP ${response.status}`);
    return body;
  }

  function tabByName(name) { return state.tabs.find(tab => tab.name === name); }
  function activeTab() { return tabByName(state.active); }
  function activeFile() { return state.active + '.json'; }
  function activeText() { return String(state.draft.get(state.active) || ''); }
  function isDirty(name) { return state.draft.has(name) && state.draft.get(name) !== state.live.get(name); }

  function hasDirtyDrafts() {
    return state.tabs.some(tab => JSON_TABS.has(tab.name) && isDirty(tab.name));
  }

  function statusFor(tab) {
    if (!tab) return ['Не загружено', ''];
    if (tab.kind === 'list') return ['READ ONLY', 'readonly'];
    if (state.errors.has(tab.name)) return ['ERROR', 'error'];
    if (state.valid.has(tab.name)) return ['XRAY VALID', 'valid'];
    if (isDirty(tab.name)) return ['DRAFT', 'draft'];
    return ['LIVE', 'live'];
  }

  function setNotice(text, type = '') {
    const box = qs('#csNotice');
    if (!box) return;
    box.textContent = text || '';
    box.className = `cs-notice${text ? ' show' : ''}${type ? ' ' + type : ''}`;
  }

  function makeTabButton(tab) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = `cs-tab${tab.name === state.active ? ' active' : ''}`;
    button.dataset.tab = tab.name;
    button.textContent = tab.name;
    if (isDirty(tab.name)) {
      const dot = document.createElement('span');
      dot.className = 'dirty';
      dot.setAttribute('aria-label', 'Есть несохранённые изменения');
      button.appendChild(dot);
    }
    button.addEventListener('click', () => {
      state.active = tab.name;
      state.valid.delete(tab.name);
      render();
    });
    return button;
  }

  function renderTabGroup(hostSelector, order, includeAbsent) {
    const host = qs(hostSelector);
    if (!host) return;
    host.textContent = '';
    order.forEach(name => {
      const tab = tabByName(name);
      if (!tab || (!includeAbsent && !tab.present)) return;
      host.appendChild(makeTabButton(tab));
    });
  }

  function renderTabs() {
    renderTabGroup('#csTabsMain', MAIN_TAB_ORDER, true);
    renderTabGroup('#csTabsLists', LIST_TAB_ORDER, false);
    const lists = qs('#csTabsLists');
    if (lists) lists.hidden = !LIST_TAB_ORDER.some(name => tabByName(name)?.present);
  }

  function syncEditorScroll() {
    const input = qs('#csInput'), highlight = qs('#csHighlight'), lines = qs('#csLines');
    if (!input || !highlight || !lines) return;
    highlight.scrollTop = input.scrollTop;
    highlight.scrollLeft = input.scrollLeft;
    lines.scrollTop = input.scrollTop;
  }

  function renderLineNumbers(text, errorLine) {
    const host = qs('#csLines');
    if (!host) return;
    const count = Math.max(1, String(text).split('\n').length);
    host.innerHTML = Array.from({length: count}, (_, i) => `<span${errorLine === i + 1 ? ' class="error"' : ''}>${i + 1}</span>`).join('');
  }

  function updateEditorVisuals() {
    const input = qs('#csInput'), highlight = qs('#csHighlight');
    if (!input || !highlight) return;
    const text = input.value;
    highlight.innerHTML = highlightJSON(text);
    const diagnostic = syntaxDiagnostic(text);
    if (diagnostic) state.errors.set(state.active, diagnostic); else state.errors.delete(state.active);
    renderLineNumbers(text, diagnostic && diagnostic.line);
    const diag = qs('#csDiagnostic');
    if (diag) {
      if (diagnostic) {
        const where = diagnostic.line ? `Строка ${diagnostic.line}${diagnostic.column ? ` · колонка ${diagnostic.column}` : ''}` : 'Позиция ошибки не определена';
        diag.innerHTML = `<strong>Ошибка JSON · ${where}</strong><br>${escapeHTML(diagnostic.message)}`;
        diag.classList.add('show');
      } else {
        diag.textContent = '';
        diag.classList.remove('show');
      }
    }
    syncEditorScroll();
    renderStatusAndActions();
    renderTabs();
  }

  function renderStatusAndActions() {
    const tab = activeTab();
    const [label, cls] = statusFor(tab);
    const chip = qs('#csState');
    if (chip) { chip.textContent = label; chip.className = `cs-state ${cls}`; }
    const jsonEditable = tab && JSON_TABS.has(tab.name);
    const listTab = tab && tab.kind === 'list';
    const hasError = state.errors.has(state.active);
    const dirty = isDirty(state.active);
    const blocked = state.externalBusy || state.applying;
    const input = qs('#csInput');
    if (input) input.readOnly = blocked;
    document.querySelectorAll('.cs-shell .cs-tab').forEach(button => { button.disabled = blocked; });
    const format = qs('#csFormat'), reset = qs('#csReset'), apply = qs('#csApply');
    if (format) format.disabled = !jsonEditable || listTab || blocked;
    if (reset) reset.disabled = !jsonEditable || !dirty || blocked;
    if (apply) {
      apply.disabled = !jsonEditable || !dirty || hasError || state.stale || blocked;
      apply.textContent = 'Применить';
    }
  }

  function bindEditorResize(editor) {
    if (!editor) return;
    if (editorResizeObserver) {
      editorResizeObserver.disconnect();
      editorResizeObserver = null;
    }
    if (state.editorHeight >= 340 && state.editorHeight <= 920) editor.style.height = state.editorHeight + 'px';
    if (typeof ResizeObserver !== 'function') return;
    editorResizeObserver = new ResizeObserver(entries => {
      const height = Math.round(entries[0]?.contentRect?.height || 0);
      if (height < 340 || height > 920 || Math.abs(height - state.editorHeight) < 2) return;
      state.editorHeight = height;
      try { sessionStorage.setItem('freenet.configStudio.editorHeight', String(height)); } catch (_) {}
    });
    editorResizeObserver.observe(editor);
  }

  function closeGeoAutocomplete() {
    if (state.geoSuggestTimer) { clearTimeout(state.geoSuggestTimer); state.geoSuggestTimer = null; }
    if (state.geoSuggestController) { state.geoSuggestController.abort(); state.geoSuggestController = null; }
    state.geoSuggestItems = [];
    state.geoSuggestIndex = -1;
    state.geoSuggestToken = null;
    const box = qs('#csGeoAutocomplete');
    if (box) { box.hidden = true; box.textContent = ''; }
  }

  function unescapedQuoteBefore(text, from) {
    for (let i = from; i >= 0; i--) {
      if (text[i] !== '"') continue;
      let slashes = 0;
      for (let j = i - 1; j >= 0 && text[j] === '\\'; j--) slashes++;
      if (slashes % 2 === 0) return i;
    }
    return -1;
  }

  function unescapedQuoteAfter(text, from) {
    for (let i = from; i < text.length; i++) {
      if (text[i] !== '"') continue;
      let slashes = 0;
      for (let j = i - 1; j >= 0 && text[j] === '\\'; j--) slashes++;
      if (slashes % 2 === 0) return i;
    }
    return -1;
  }

  function geoEditorToken(input) {
    if (state.active !== '05_routing' || !input) return null;
    const text = input.value;
    const cursor = input.selectionStart;
    const startQuote = unescapedQuoteBefore(text, cursor - 1);
    if (startQuote < 0) return null;
    const foundEndQuote = unescapedQuoteAfter(text, cursor);
    const endQuote = foundEndQuote >= cursor ? foundEndQuote : cursor;
    const beforeCursor = text.slice(startQuote + 1, cursor);
    const fullValue = text.slice(startQuote + 1, endQuote);

    let match = beforeCursor.match(/^(geosite|geoip):([a-z0-9._@:-]{1,128})$/i);
    if (match) {
      const kind = match[1].toLowerCase();
      return {
        kind,
        file: '',
        explicitFile: false,
        prefix: match[2],
        typedPrefix: kind + ':',
        start: startQuote + 1,
        end: endQuote,
        fullValue
      };
    }

    match = beforeCursor.match(/^ext:([^:"\\/]+\.dat):([a-z0-9._@:-]{1,128})$/i);
    if (!match) return null;
    const file = match[1];
    const lowerFile = file.toLowerCase();
    const kind = lowerFile.includes('geoip') ? 'geoip' : lowerFile.includes('geosite') ? 'geosite' : '';
    if (!kind) return null;
    return {
      kind,
      file,
      explicitFile: true,
      prefix: match[2],
      typedPrefix: 'ext:' + file + ':',
      start: startQuote + 1,
      end: endQuote,
      fullValue
    };
  }

  function setGeoSuggestIndex(index) {
    const box = qs('#csGeoAutocomplete');
    if (!box || !state.geoSuggestItems.length) return;
    const count = state.geoSuggestItems.length;
    state.geoSuggestIndex = ((index % count) + count) % count;
    Array.from(box.querySelectorAll('.cs-geo-item')).forEach((node,i) => {
      const active = i === state.geoSuggestIndex;
      node.classList.toggle('active', active);
      node.setAttribute('aria-selected', active ? 'true' : 'false');
      if (active) node.scrollIntoView({block:'nearest'});
    });
  }

  function geoEditorSuggestionValue(item, token) {
    if (!item || !token) return '';
    if (token.explicitFile) return token.typedPrefix + String(item.category || '');
    return String(item.selector || (token.typedPrefix + String(item.category || '')));
  }

  function chooseGeoEditorSuggestion(item) {
    const input = qs('#csInput');
    const token = state.geoSuggestToken;
    if (!input || !token || !item) return;
    const value = geoEditorSuggestionValue(item, token);
    input.setRangeText(value, token.start, token.end, 'end');
    input.dispatchEvent(new Event('input', {bubbles:true}));
    closeGeoAutocomplete();
    input.focus();
  }

  function renderGeoEditorSuggestions(body, token) {
    const box = qs('#csGeoAutocomplete');
    if (!box) return;
    box.textContent = '';
    const items = Array.isArray(body?.suggestions) ? body.suggestions.slice(0,10) : [];
    state.geoSuggestItems = items;
    state.geoSuggestIndex = items.length ? 0 : -1;
    state.geoSuggestToken = token;
    items.forEach((item,index) => {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'cs-geo-item' + (index === 0 ? ' active' : '');
      button.setAttribute('role','option');
      button.setAttribute('aria-selected', index === 0 ? 'true' : 'false');
      const title = document.createElement('b');
      title.textContent = geoEditorSuggestionValue(item, token);
      const meta = document.createElement('span');
      meta.textContent = String(item.file || token.file);
      button.append(title, meta);
      button.addEventListener('mousedown', event => event.preventDefault());
      button.addEventListener('click', () => chooseGeoEditorSuggestion(item));
      box.appendChild(button);
    });
    const warnings = Array.isArray(body?.warnings) ? body.warnings.filter(Boolean) : [];
    if (warnings.length) {
      const warn = document.createElement('div');
      warn.className = 'cs-geo-warning';
      warn.textContent = warnings.join(' · ');
      box.appendChild(warn);
    }
    if (!items.length && !warnings.length) {
      const empty = document.createElement('div');
      empty.className = 'cs-geo-status';
      empty.textContent = 'GeoData: совпадений не найдено.';
      box.appendChild(empty);
    }
    box.hidden = false;
  }

  function renderGeoEditorStatus(text, bad = false) {
    const box = qs('#csGeoAutocomplete');
    if (!box) return;
    box.textContent = '';
    state.geoSuggestItems = [];
    state.geoSuggestIndex = -1;
    const status = document.createElement('div');
    status.className = 'cs-geo-status' + (bad ? ' bad' : '');
    status.textContent = text;
    box.appendChild(status);
    box.hidden = false;
  }

  async function requestGeoEditorSuggestions(input, token) {
    if (!token || token.prefix.length < 1) { closeGeoAutocomplete(); return; }
    if (state.geoSuggestController) state.geoSuggestController.abort();
    const controller = new AbortController();
    state.geoSuggestController = controller;
    const q = token.typedPrefix + token.prefix;
    let url = '/api/geodata/suggest?kind=' + encodeURIComponent(token.kind) + '&q=' + encodeURIComponent(q) + '&mode=prefix';
    if (token.explicitFile && token.file) url += '&file=' + encodeURIComponent(token.file);
    try {
      const body = await api(url, {signal:controller.signal});
      if (body.mutation !== 'NONE' || controller.signal.aborted) return;
      renderGeoEditorSuggestions(body, token);
    } catch (error) {
      if (controller.signal.aborted || error?.name === 'AbortError') return;
      renderGeoEditorStatus('GeoData: ' + (error?.message || 'поиск недоступен'), true);
    } finally {
      if (state.geoSuggestController === controller) state.geoSuggestController = null;
    }
  }

  function queueGeoEditorAutocomplete(input) {
    if (state.geoSuggestTimer) clearTimeout(state.geoSuggestTimer);
    if (state.geoSuggestController) {
      state.geoSuggestController.abort();
      state.geoSuggestController = null;
    }
    const token = geoEditorToken(input);
    if (!token) { closeGeoAutocomplete(); return; }
    renderGeoEditorStatus('GeoData: ищу локальные категории…');
    state.geoSuggestTimer = setTimeout(() => {
      state.geoSuggestTimer = null;
      void requestGeoEditorSuggestions(input, token);
    }, 180);
  }

  function renderReadOnly(tab) {
    closeGeoAutocomplete();
    const host = qs('#csBody');
    if (!host) return;
    const text = String(tab.text || '');
    host.innerHTML = `<div class="cs-editor readonly"><div id="csLines" class="cs-lines"></div><div class="cs-code"><pre id="csHighlight" class="cs-highlight"></pre></div></div>`;
    const hi = qs('#csHighlight');
    if (hi) hi.textContent = text;
    renderLineNumbers(text, 0);
    bindEditorResize(qs('.cs-editor', host));
  }

  function renderEditor(tab) {
    closeGeoAutocomplete();
    const host = qs('#csBody');
    if (!host) return;
    const text = activeText();
    host.innerHTML = `<div class="cs-editor"><div id="csLines" class="cs-lines"></div><div class="cs-code"><pre id="csHighlight" class="cs-highlight" aria-hidden="true"></pre><textarea id="csInput" class="cs-input" spellcheck="false" aria-label="${escapeHTML(tab.name)} JSON editor" aria-autocomplete="list" aria-controls="csGeoAutocomplete"></textarea><div id="csGeoAutocomplete" class="cs-geo-autocomplete" role="listbox" hidden></div></div></div><div id="csDiagnostic" class="cs-diagnostic"></div>`;
    const input = qs('#csInput');
    input.value = text;
    input.addEventListener('input', () => {
      state.draft.set(state.active, input.value);
      state.draftRevision++;
      state.valid.delete(state.active);
      updateEditorVisuals();
      queueGeoEditorAutocomplete(input);
    });
    input.addEventListener('scroll', syncEditorScroll);
    input.addEventListener('keydown', event => {
      const box = qs('#csGeoAutocomplete');
      const open = box && !box.hidden && state.geoSuggestItems.length > 0;
      if (open && (event.key === 'ArrowDown' || event.key === 'ArrowUp')) {
        event.preventDefault();
        setGeoSuggestIndex(state.geoSuggestIndex + (event.key === 'ArrowDown' ? 1 : -1));
        return;
      }
      if (open && (event.key === 'Enter' || event.key === 'Tab')) {
        event.preventDefault();
        chooseGeoEditorSuggestion(state.geoSuggestItems[state.geoSuggestIndex >= 0 ? state.geoSuggestIndex : 0]);
        return;
      }
      if (open && event.key === 'Escape') {
        event.preventDefault();
        closeGeoAutocomplete();
        return;
      }
      if (event.key === 'Tab') {
        event.preventDefault();
        const start = input.selectionStart, end = input.selectionEnd;
        input.setRangeText('  ', start, end, 'end');
        input.dispatchEvent(new Event('input', {bubbles:true}));
      }
    });
    input.addEventListener('click', () => queueGeoEditorAutocomplete(input));
    input.addEventListener('keyup', event => {
      if (['ArrowLeft','ArrowRight','Home','End'].includes(event.key)) queueGeoEditorAutocomplete(input);
    });
    input.addEventListener('blur', () => setTimeout(() => closeGeoAutocomplete(), 120));
    bindEditorResize(qs('.cs-editor', host));
    updateEditorVisuals();
  }

  function renderBody() {
    const tab = activeTab();
    if (!tab) return;
    if (tab.kind === 'list') renderReadOnly(tab);
    else renderEditor(tab);
    const meta = qs('#csMeta');
    if (meta) meta.innerHTML = `<span>Файл: <code>${escapeHTML(tab.name + (tab.kind === 'json' ? '.json' : '.lst'))}</code></span><span>${tab.present ? `live · ${escapeHTML(tab.sha256 || 'hash —')}` : 'файл отсутствует'}</span>`;
    renderStatusAndActions();
  }

  function render() {
    renderTabs();
    renderBody();
  }

  function loadWorkspaceData(body) {
    state.tabs = Array.isArray(body.tabs) ? body.tabs : [];
    state.xray = body.xray || {online:false, version:''};
    state.live.clear();
    state.draft.clear();
    state.valid.clear();
    state.errors.clear();
    state.tabs.forEach(tab => {
      if (JSON_TABS.has(tab.name) && tab.content && typeof tab.content === 'object') {
        const text = normalizeJSONString(tab.content);
        state.live.set(tab.name, text);
        state.draft.set(tab.name, text);
      } else if (tab.kind === 'list') {
        const text = String(tab.text || '');
        state.live.set(tab.name, text);
        state.draft.set(tab.name, text);
      }
    });
    const preferred = MAIN_TAB_ORDER.find(name => state.tabs.some(tab => tab.name === name && tab.present));
    state.active = preferred || (state.tabs[0] && state.tabs[0].name) || '';
  }

  async function reloadWorkspace(message, force = false, discardDraft = false) {
    if (state.loadPromise) {
      const active = state.loadPromise;
      if (!force) return active;
      await active;
    }
    if (state.loadPromise) return reloadWorkspace(message, force, discardDraft);

    state.loading = true;
    const previousActive = state.active;
    const revisionAtStart = state.draftRevision;
    const task = (async () => {
      try {
        const body = await api('/api/config-studio');
        if (body.mutation !== 'NONE') throw new Error('Нарушен read-only contract Config Studio');
        // A background response must never clear a newer draft or STOP marker.
        // Explicit Reset or verified Apply alone authorizes replacing a stale draft.
        if (state.draftRevision !== revisionAtStart || (!discardDraft && (state.stale || hasDirtyDrafts()))) return false;
        loadWorkspaceData(body);
        if (previousActive && tabByName(previousActive)) state.active = previousActive;
        state.stale = false;
        render();
        if (message) setNotice(message, 'ok');
        return true;
      } catch (error) {
        setNotice(`Config Studio недоступен: ${error.message || 'ошибка API'}`, 'bad');
        return false;
      } finally {
        state.loading = false;
        state.loadPromise = null;
      }
    })();

    state.loadPromise = task;
    return task;
  }

  function formatActive() {
    if (state.externalBusy || state.applying) return;
    const tab = activeTab();
    if (!tab || !JSON_TABS.has(tab.name)) return;
    const diagnostic = syntaxDiagnostic(activeText());
    if (diagnostic) { updateEditorVisuals(); return; }
    state.draft.set(state.active, normalizeJSONString(JSON.parse(activeText())));
    state.draftRevision++;
    state.valid.delete(state.active);
    renderBody();
    renderTabs();
    setNotice('JSON отформатирован: 2 пробела, единый стиль FreeNet. Live config не изменён.', 'ok');
  }

  function resetActive() {
    if (state.externalBusy || state.applying) return;
    if (state.stale) {
      void reloadWorkspace('Черновик отменён. Загружена актуальная live-конфигурация.', true, true);
      return;
    }
    if (!state.live.has(state.active)) return;
    state.draft.set(state.active, state.live.get(state.active));
    state.valid.delete(state.active);
    state.errors.delete(state.active);
    render();
    setNotice('Черновик сброшен к authoritative live snapshot.', 'ok');
  }

  function routingPayload() {
    const routing = JSON.parse(String(state.draft.get('05_routing') || '{}'));
    const policy = JSON.parse(String(state.draft.get('06_policy') || '{}'));
    return {routing, policy};
  }

  async function validateActive() {
    const tab = activeTab();
    if (!tab || !JSON_TABS.has(tab.name) || state.errors.has(tab.name)) return;
    const button = qs('#csValidate');
    if (button) { button.disabled = true; button.textContent = 'Проверяю…'; }
    try {
      if (ROUTING_TABS.has(tab.name)) {
        const body = await api('/api/routing/validate', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(routingPayload())});
        if (body.mutation !== 'NONE' || !body.xray_valid) throw new Error('Routing validation contract failed');
        state.valid.add('05_routing');
        state.valid.add('06_policy');
      } else {
        const body = await api('/api/config-studio/validate', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({file:activeFile(), content:JSON.parse(activeText())})});
        if (body.mutation !== 'NONE' || !body.xray_valid) throw new Error('Config Studio validation contract failed');
        state.valid.add(tab.name);
      }
      renderStatusAndActions();
      renderTabs();
      setNotice('Candidate прошёл `xray run -test -confdir`. Live config не изменён. MUTATION: NONE', 'ok');
    } catch (error) {
      state.valid.delete(tab.name);
      renderStatusAndActions();
      setNotice(`Xray validation: ${error.message || 'candidate не валиден'}`, 'bad');
    } finally {
      if (button) button.textContent = 'Проверить Xray';
      renderStatusAndActions();
    }
  }

  async function applyActive() {
    const tab = activeTab();
    if (!tab || !isDirty(tab.name) || state.errors.has(tab.name) || state.stale || state.externalBusy || state.applying) return;
    state.applying = true;
    renderStatusAndActions();
    const appliedTab = tab.name;
    const button = qs('#csApply');
    if (button) { button.disabled = true; button.textContent = 'Применяю…'; }
    try {
      let body;
      if (ROUTING_TABS.has(tab.name)) {
        body = await api('/api/routing/apply', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(routingPayload())});
      } else {
        body = await api('/api/config-studio/apply', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({file:activeFile(), content:JSON.parse(activeText())})});
      }
      if (body.mutation !== 'APPLIED') throw new Error(body.error || `mutation ${body.mutation || 'UNKNOWN'}`);
      const appliedMessage = body.core_restart
        ? 'Изменения применены, post-validation подтверждён, Xray Core перезапущен безопасным core-only path.'
        : 'Изменения сохранены и post-validation подтверждён. Xray был остановлен и остался остановлен; новый конфиг загрузится при следующем запуске.';
      await reloadWorkspace(appliedMessage, true, true);
      document.dispatchEvent(new CustomEvent('freenet:xray-config-applied', {
        detail:{
          source:'config-studio',
          files:ROUTING_TABS.has(appliedTab) ? ['05_routing','06_policy'] : [appliedTab]
        }
      }));
    } catch (error) {
      setNotice(`Apply не завершён: ${error.message || 'неизвестная ошибка'}. Blind retry не запускается. Проверьте результат/rollback перед повтором.`, 'bad');
    } finally {
      state.applying = false;
      if (button) button.textContent = 'Применить';
      renderStatusAndActions();
    }
  }

  function mountMarkup(panel) {
    panel.innerHTML = `<div class="rv2-card"><div class="cs-shell"><div class="cs-head"><div class="cs-title"><h2>Config Studio</h2><span id="csState" class="cs-state">Загрузка</span></div></div><p class="rv2-copy">Полный Xray workspace после авторизации FreeNet: syntax highlight, dirty-state, diagnostics, validation и controlled apply.</p><div class="cs-tab-groups"><div id="csTabsMain" class="cs-tabs cs-tabs-main" aria-label="Xray config files"></div><div id="csTabsLists" class="cs-tabs cs-tabs-lists" aria-label="XKeen list files"></div></div><div id="csBody"></div><div class="cs-toolbar cs-editor-footer"><div class="cs-actions"><button id="csFormat" class="cs-btn cs-btn-tertiary" type="button">Форматировать</button><button id="csReset" class="cs-btn cs-btn-reset" type="button">Отменить</button></div><button id="csApply" class="cs-btn primary" type="button">Применить</button></div><div id="csMeta" class="cs-meta"></div><div class="cs-safe-note">01–06 доступны владельцу после авторизации FreeNet. Credential-bearing значения остаются только в authenticated browser session и не должны попадать в Journal, GitHub или CI logs. Apply: validation → snapshot → atomic write → post-check → rollback/STOP.</div><div id="csNotice" class="cs-notice"></div></div></div>`;
    qs('#csFormat')?.addEventListener('click', formatActive);
    qs('#csReset')?.addEventListener('click', resetActive);
    qs('#csApply')?.addEventListener('click', applyActive);
  }

  async function handleExternalConfigApplied(event) {
    const detail = event && event.detail && typeof event.detail === 'object' ? event.detail : {};
    if (detail.source === 'config-studio') return;

    if (hasDirtyDrafts()) {
      state.stale = true;
      state.draftRevision++;
      state.valid.clear();
      renderStatusAndActions();
      setNotice('Live-конфигурация изменилась в «Правила». Локальный черновик сохранён, но применение заблокировано. Нажмите «Отменить», чтобы загрузить актуальный live state.', 'bad');
      return;
    }

    await reloadWorkspace('', true);
  }

  async function handleRoutingModeChanged(event) {
    const mode = String(event?.detail?.mode || '');
    if (mode !== 'config' || hasDirtyDrafts() || state.stale) return;
    await reloadWorkspace('', true);
  }

  // The editor owns its disabled state; background polling cannot clear STALE.
  document.addEventListener('freenet:controls-busy', event => {
    state.externalBusy = !!event.detail;
    renderStatusAndActions();
  });

  document.addEventListener('freenet:xray-config-applied', event => { void handleExternalConfigApplied(event); });
  document.addEventListener('freenet:routing-mode-changed', event => { void handleRoutingModeChanged(event); });

  function mount() {
    if (state.mounted) return;
    const panel = qs('#rv2ConfigPanel');
    if (!panel) return;
    state.mounted = true;
    ;
    mountMarkup(panel);
    reloadWorkspace();
    panel.dataset.configStudioParity = '1';
  }

  window.FreeNetConfigStudio = Object.assign(window.FreeNetConfigStudio || {}, {
    reloadWorkspace,
    hasDirtyDrafts
  });

  function tryMount() {
    if (!state.mounted) mount();
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', tryMount, {once:true});
  else tryMount();
  const observer = new MutationObserver(() => { if (!state.mounted) tryMount(); });
  observer.observe(document.documentElement, {childList:true, subtree:true});
})();