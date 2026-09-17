(() => {
  'use strict';

  const qs = (s, root = document) => root.querySelector(s);
  const JSON_TABS = new Set(['01_log', '02_dns', '03_inbounds', '04_outbounds', '05_routing', '06_policy']);
  const ROUTING_TABS = new Set(['05_routing', '06_policy']);
  const MAIN_TAB_ORDER = ['01_log', '02_dns', '03_inbounds', '04_outbounds', '05_routing', '06_policy'];
  const LIST_TAB_ORDER = ['ip_exclude', 'port_exclude', 'port_proxying'];

  const state = {
    mounted: false,
    loading: false,
    active: '',
    tabs: [],
    live: new Map(),
    draft: new Map(),
    valid: new Set(),
    errors: new Map(),
    xray: {online: false, version: ''}
  };

  function installStyles() {
    if (qs('#configStudioParityStyles')) return;
    const style = document.createElement('style');
    style.id = 'configStudioParityStyles';
    style.textContent = `
      .cs-shell{display:grid;gap:12px}.cs-head{display:flex;align-items:flex-start;justify-content:space-between;gap:14px;flex-wrap:wrap}.cs-title{display:flex;align-items:center;gap:10px;flex-wrap:wrap}.cs-title h2{margin:0;color:#f4f7fb;font-size:17px}.cs-xray{display:flex;gap:7px;align-items:center;flex-wrap:wrap}.cs-chip{display:inline-flex;align-items:center;gap:6px;border:1px solid #2c4564;background:#0a1829;border-radius:999px;padding:6px 9px;color:#aebed3;font-size:10px;font-weight:800}.cs-chip::before{content:'';width:7px;height:7px;border-radius:50%;background:#8397b0}.cs-chip.ok{border-color:rgba(54,227,162,.4);color:#72edb4}.cs-chip.ok::before{background:#36e3a2}.cs-chip.bad{border-color:rgba(255,103,115,.42);color:#ffadb4}.cs-chip.bad::before{background:#ff6773}
      .cs-tab-groups{display:flex;align-items:center;gap:10px;flex-wrap:wrap}.cs-tabs{display:flex;align-items:center;gap:7px;overflow:auto;padding:4px;scrollbar-width:thin;border-radius:12px}.cs-tabs-main{background:#091625;border:1px solid #213955}.cs-tabs-lists{background:#101a2b;border:1px solid #32425d}.cs-tab{position:relative;flex:0 0 auto;appearance:none;border:1px solid #2a405d;background:#0b1726;color:#9eafc5;border-radius:9px;padding:9px 13px;cursor:pointer;font:inherit;font-size:12px;font-weight:800}.cs-tab:hover{border-color:#5077a8;color:#eef5ff}.cs-tab.active{border-color:#5b8cff;background:#18325a;color:#fff}.cs-tabs-lists .cs-tab{background:#111d30}.cs-tabs-lists .cs-tab.active{background:#1b2943}.cs-tab .dirty{position:absolute;right:6px;top:5px;width:6px;height:6px;border-radius:50%;background:#ffc33f;box-shadow:0 0 0 2px #18325a}
      .cs-toolbar{display:flex;align-items:center;justify-content:space-between;gap:10px;flex-wrap:wrap}.cs-actions{display:flex;align-items:center;gap:8px;flex-wrap:wrap}.cs-btn{appearance:none;border:1px solid #304966;background:#0d1b2d;color:#e8f0fb;border-radius:10px;padding:9px 12px;cursor:pointer;font:inherit;font-size:12px;font-weight:800}.cs-btn:hover:not(:disabled){border-color:#5b8cff;background:#142b49}.cs-btn.primary{border-color:#4f82ff;background:linear-gradient(180deg,#347dff,#2869df);color:white}.cs-btn:disabled{opacity:.42;cursor:not-allowed}.cs-state{display:inline-flex;align-items:center;gap:7px;border:1px solid #324a68;border-radius:999px;padding:6px 9px;color:#a8b9ce;font-size:10px;font-weight:900;letter-spacing:.035em;text-transform:uppercase}.cs-state::before{content:'';width:7px;height:7px;border-radius:50%;background:#8799ae}.cs-state.live,.cs-state.valid{border-color:rgba(54,227,162,.38);color:#72edb4}.cs-state.live::before,.cs-state.valid::before{background:#36e3a2}.cs-state.draft{border-color:rgba(255,190,67,.4);color:#ffd06d}.cs-state.draft::before{background:#ffbe43}.cs-state.error{border-color:rgba(255,103,115,.44);color:#ffabb3}.cs-state.error::before{background:#ff6773}.cs-state.readonly{color:#b6c4d5}
      .cs-editor{position:relative;display:grid;grid-template-columns:auto minmax(0,1fr);min-height:470px;border:1px solid #263d5a;border-radius:12px;overflow:hidden;background:#07111f}.cs-lines{min-width:48px;padding:14px 9px 14px 7px;background:#081422;border-right:1px solid #223650;color:#536784;text-align:right;font:500 12px/1.62 ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,"Liberation Mono",monospace;user-select:none;overflow:hidden}.cs-lines span{display:block;height:1.62em}.cs-lines span.error{color:#ff5968;font-weight:900}.cs-code{position:relative;min-width:0;overflow:hidden}.cs-highlight,.cs-input{position:absolute;inset:0;margin:0;padding:14px 15px;border:0;outline:0;box-sizing:border-box;overflow:auto;white-space:pre;tab-size:2;font:500 12px/1.62 ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,"Liberation Mono",monospace}.cs-highlight{pointer-events:none;color:#dce7f7}.cs-input{resize:none;background:transparent;color:transparent;caret-color:#f6f8fc;-webkit-text-fill-color:transparent}.cs-input::selection{background:rgba(91,140,255,.34)}.cs-input:focus{box-shadow:inset 0 0 0 1px rgba(91,140,255,.58)}.cs-key{color:#86b6ff}.cs-string{color:#9de27c}.cs-number{color:#ffae69}.cs-bool{color:#c9a7ff}.cs-null{color:#ff8d9a}.cs-punct{color:#90a5c0}.cs-editor.readonly .cs-input{display:none}.cs-editor.readonly .cs-highlight{position:relative;min-height:470px}
      .cs-meta{display:flex;align-items:center;justify-content:space-between;gap:10px;flex-wrap:wrap;color:#8196b2;font-size:10px}.cs-meta code{color:#b9cae0}.cs-diagnostic{display:none;border:1px solid rgba(255,103,115,.44);border-radius:11px;background:#321923;color:#ffd6d9;padding:10px 12px;font-size:11px;line-height:1.45}.cs-diagnostic.show{display:block}.cs-diagnostic strong{color:#ff9ca6}.cs-notice{display:none;border:1px solid #315071;border-radius:11px;background:#0c2138;color:#b9cbe1;padding:10px 12px;font-size:11px;line-height:1.5;white-space:pre-wrap}.cs-notice.show{display:block}.cs-notice.ok{border-color:rgba(54,227,162,.36);background:#0f2b24;color:#d3fae9}.cs-notice.bad{border-color:rgba(255,103,115,.45);background:#321923;color:#ffd4d7}.cs-safe-note{border:1px solid rgba(91,140,255,.28);border-radius:11px;background:rgba(25,57,98,.18);color:#aebfd5;padding:10px 12px;font-size:11px;line-height:1.5}
      @media(max-width:760px){.cs-editor{min-height:380px}.cs-btn{flex:1}.cs-actions{width:100%}.cs-tab-groups{display:grid}.cs-tabs{width:100%}}
    `;
    document.head.appendChild(style);
  }

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

  function renderXray() {
    const host = qs('#csXray');
    if (!host) return;
    host.innerHTML = '';
    const status = document.createElement('span');
    status.className = `cs-chip ${state.xray.online ? 'ok' : 'bad'}`;
    status.textContent = state.xray.online ? 'Xray запущен' : 'Xray не запущен';
    const version = document.createElement('span');
    version.className = 'cs-chip';
    version.textContent = state.xray.version ? `Xray ${state.xray.version}` : 'Версия Xray не определена';
    host.append(status, version);
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
    const valid = state.valid.has(state.active);
    const format = qs('#csFormat'), validate = qs('#csValidate'), reset = qs('#csReset'), apply = qs('#csApply');
    if (format) format.disabled = !jsonEditable || listTab;
    if (validate) validate.disabled = !jsonEditable || listTab || hasError;
    if (reset) reset.disabled = !jsonEditable || !dirty;
    if (apply) {
      apply.disabled = !jsonEditable || !dirty || !valid || hasError;
      apply.textContent = ROUTING_TABS.has(state.active) ? 'Применить 05/06' : 'Применить проверенный файл';
    }
  }

  function renderReadOnly(tab) {
    const host = qs('#csBody');
    if (!host) return;
    const text = String(tab.text || '');
    host.innerHTML = `<div class="cs-editor readonly"><div id="csLines" class="cs-lines"></div><div class="cs-code"><pre id="csHighlight" class="cs-highlight"></pre></div></div>`;
    const hi = qs('#csHighlight');
    if (hi) hi.textContent = text;
    renderLineNumbers(text, 0);
  }

  function renderEditor(tab) {
    const host = qs('#csBody');
    if (!host) return;
    const text = activeText();
    host.innerHTML = `<div class="cs-editor"><div id="csLines" class="cs-lines"></div><div class="cs-code"><pre id="csHighlight" class="cs-highlight" aria-hidden="true"></pre><textarea id="csInput" class="cs-input" spellcheck="false" aria-label="${escapeHTML(tab.name)} JSON editor"></textarea></div></div><div id="csDiagnostic" class="cs-diagnostic"></div>`;
    const input = qs('#csInput');
    input.value = text;
    input.addEventListener('input', () => {
      state.draft.set(state.active, input.value);
      state.valid.delete(state.active);
      updateEditorVisuals();
    });
    input.addEventListener('scroll', syncEditorScroll);
    input.addEventListener('keydown', event => {
      if (event.key === 'Tab') {
        event.preventDefault();
        const start = input.selectionStart, end = input.selectionEnd;
        input.setRangeText('  ', start, end, 'end');
        input.dispatchEvent(new Event('input', {bubbles:true}));
      }
    });
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
    renderXray();
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

  async function reloadWorkspace(message) {
    if (state.loading) return;
    state.loading = true;
    try {
      const body = await api('/api/config-studio');
      if (body.mutation !== 'NONE') throw new Error('Нарушен read-only contract Config Studio');
      loadWorkspaceData(body);
      render();
      if (message) setNotice(message, 'ok');
    } catch (error) {
      setNotice(`Config Studio недоступен: ${error.message || 'ошибка API'}`, 'bad');
    } finally {
      state.loading = false;
    }
  }

  function formatActive() {
    const tab = activeTab();
    if (!tab || !JSON_TABS.has(tab.name)) return;
    const diagnostic = syntaxDiagnostic(activeText());
    if (diagnostic) { updateEditorVisuals(); return; }
    state.draft.set(state.active, normalizeJSONString(JSON.parse(activeText())));
    state.valid.delete(state.active);
    renderBody();
    renderTabs();
    setNotice('JSON отформатирован: 2 пробела, единый стиль FreeNet. Live config не изменён.', 'ok');
  }

  function resetActive() {
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
    if (!tab || !state.valid.has(tab.name) || !isDirty(tab.name)) return;
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
      await reloadWorkspace('Изменения применены и post-validation подтверждён. Xray Core автоматически не перезапускался.');
    } catch (error) {
      setNotice(`Apply не завершён: ${error.message || 'неизвестная ошибка'}. Blind retry не запускается. Проверьте результат/rollback перед повтором.`, 'bad');
    } finally {
      if (button) button.textContent = 'Применить';
      renderStatusAndActions();
    }
  }

  function mountMarkup(panel) {
    panel.innerHTML = `<div class="rv2-card"><div class="cs-shell"><div class="cs-head"><div class="cs-title"><h2>Config Studio</h2><span id="csState" class="cs-state">Загрузка</span></div><div id="csXray" class="cs-xray"></div></div><p class="rv2-copy">Полный Xray workspace после авторизации FreeNet: syntax highlight, dirty-state, diagnostics, validation и controlled apply.</p><div class="cs-tab-groups"><div id="csTabsMain" class="cs-tabs cs-tabs-main" aria-label="Xray config files"></div><div id="csTabsLists" class="cs-tabs cs-tabs-lists" aria-label="XKeen list files"></div></div><div class="cs-toolbar"><div class="cs-actions"><button id="csFormat" class="cs-btn" type="button">Формат</button><button id="csValidate" class="cs-btn" type="button">Проверить Xray</button><button id="csReset" class="cs-btn" type="button">Сбросить</button></div><button id="csApply" class="cs-btn primary" type="button">Применить</button></div><div id="csBody"></div><div id="csMeta" class="cs-meta"></div><div class="cs-safe-note">01–06 доступны владельцу после авторизации FreeNet. Credential-bearing значения остаются только в authenticated browser session и не должны попадать в Journal, GitHub или CI logs. Apply: validation → snapshot → atomic write → post-check → rollback/STOP.</div><div id="csNotice" class="cs-notice"></div></div></div>`;
    qs('#csFormat')?.addEventListener('click', formatActive);
    qs('#csValidate')?.addEventListener('click', validateActive);
    qs('#csReset')?.addEventListener('click', resetActive);
    qs('#csApply')?.addEventListener('click', applyActive);
  }

  function mount() {
    if (state.mounted) return;
    const panel = qs('#rv2ConfigPanel');
    if (!panel) return;
    state.mounted = true;
    installStyles();
    mountMarkup(panel);
    reloadWorkspace();
    panel.dataset.configStudioParity = '1';
  }

  function tryMount() {
    if (!state.mounted) mount();
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', tryMount, {once:true});
  else tryMount();
  const observer = new MutationObserver(() => { if (!state.mounted) tryMount(); });
  observer.observe(document.documentElement, {childList:true, subtree:true});
})();
