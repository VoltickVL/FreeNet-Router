;(() => {
  'use strict';
  if (window.__freenetRoutingApplyUILoaded) return;
  window.__freenetRoutingApplyUILoaded = true;

  const q = (selector, root = document) => root.querySelector(selector);
  const originalFetch = window.fetch.bind(window);
  let validatedCandidate = null;
  let liveBaseline = null;
  let stopLatched = false;

  function routingPage() {
    return q('[data-page-view="routing"]') || q('[data-page-view="network"]');
  }

  function setResult(text, tone = '') {
    ['#rv2ApplyResult','#rv2RulesApplyResult'].forEach(selector => {
      const node = q(selector);
      if (!node) return;
      node.textContent = text || '';
      node.className = `rv2-notice${text ? ' show' : ''}${tone ? ' ' + tone : ''}`;
    });
  }

  function setPreview(text) {
    const node = q('#rv2ApplyPreview');
    if (node) node.textContent = text || '';
  }

  function setRulesPreview(text) {
    const node = q('#rv2RulesApplyPreview');
    if (node) node.textContent = text || '';
  }

  function applyButtons() {
    return ['#rv2ApplyConfig','#rv2ApplyRules'].map(selector => q(selector)).filter(Boolean);
  }

  function canonicalHead(page) {
    if (!page || !q('#routingV2Workspace', page)) return;
    page.classList.add('fn-routing-v2');
    const head = q('.page-head', page);
    if (head && !q('[data-routing-v2-head="1"]', head)) {
      head.innerHTML = '<div data-routing-v2-head="1"><div class="page-kicker">ROUTING POLICY</div><h1>Маршрутизация</h1><p>Понятные правила для сайтов и GeoData-групп. Экспертная конфигурация остаётся во вкладке «Конфигурация».</p></div>';
    }
  }

  function installCompatibilityStyle() {
    if (q('#freenetRoutingV2CanonicalCompat')) return;
    const style = document.createElement('style');
    style.id = 'freenetRoutingV2CanonicalCompat';
    style.textContent = `
      [data-page-view="routing"].fn-routing-v2>.card.fn-routing-v2-legacy{display:none!important}
      [data-page-view="routing"].fn-routing-v2>#policyBuilderPreview{display:none!important}
      .rv2-apply-preview{margin-top:10px;padding:10px 11px;border:1px solid #294866;border-radius:11px;background:#081522;color:#9eb2ca;font-size:11px;line-height:1.5;white-space:pre-wrap}
      .rv2-apply-stop{border-color:#9b3b51!important;background:#351725!important;color:#ffd0d8!important}
    `;
    document.head.appendChild(style);
  }

  function invalidateCandidate(message = '') {
    validatedCandidate = null;
    applyButtons().forEach(button => { button.disabled = true; });
    if (message && !stopLatched) setResult(message);
  }

  function jsonEqual(a, b) {
    try { return JSON.stringify(a) === JSON.stringify(b); }
    catch (_) { return false; }
  }

  function renderDelta(candidate) {
    const baseline = liveBaseline || {};
    const routingChanged = !jsonEqual(candidate?.routing, baseline.routing);
    const policyChanged = !jsonEqual(candidate?.policy, baseline.policy);
    const lines = [
      `05_routing.json: ${routingChanged ? 'будет изменён' : 'без изменений'}`,
      `06_policy.json: ${policyChanged ? 'будет изменён' : 'без изменений'}`,
      'Порядок: validation → snapshot → atomic apply → post-check → rollback при failure.'
    ];
    setPreview(lines.join('\n'));
    const beforeRules = Array.isArray(baseline?.routing?.routing?.rules) ? baseline.routing.routing.rules.length : 0;
    const afterRules = Array.isArray(candidate?.routing?.routing?.rules) ? candidate.routing.routing.rules.length : beforeRules;
    if (routingChanged) {
      const added = Math.max(0, afterRules - beforeRules);
      setRulesPreview(`${added ? `${added} новых правил готовы к применению. ` : ''}Существующие правила сохранены. FreeNet создаст резервную точку, проверит результат и выполнит откат при ошибке.`);
    } else if (policyChanged) {
      setRulesPreview('Изменения проверены. Перед применением FreeNet создаст резервную точку и проверит результат.');
    } else {
      setRulesPreview('Проверка пройдена, но фактических изменений относительно текущей конфигурации нет.');
    }
    return routingChanged || policyChanged;
  }

  async function loadBaseline() {
    try {
      const response = await originalFetch('/api/routing/config', {cache:'no-store'});
      const body = await response.json();
      if (!response.ok || !body.success || body.mutation !== 'NONE') return;
      liveBaseline = {routing: body.routing, policy: body.policy};
      const routingHash = body.routing_sha256 ? String(body.routing_sha256).slice(0, 12) : 'new';
      const policyHash = body.policy_sha256 ? String(body.policy_sha256).slice(0, 12) : 'new';
      setPreview(`Live snapshot: 05_routing ${routingHash} · 06_policy ${policyHash}\nЧтобы применить изменения, сначала выполните «Проверить Xray».`);
      setRulesPreview('Добавьте правило и нажмите «Проверить изменения». До применения текущая маршрутизация не изменится.');
    } catch (_) {}
  }

  function installValidationCapture() {
    if (window.fetch.__freenetRoutingValidationCapture) return;
    const wrapped = async function(input, init) {
      const url = typeof input === 'string' ? input : (input && input.url) || '';
      const method = String((init && init.method) || (input && input.method) || 'GET').toUpperCase();
      const isValidation = url === '/api/routing/validate' && method === 'POST' && init && typeof init.body === 'string';
      let requestCandidate = null;
      if (isValidation) {
        try { requestCandidate = JSON.parse(init.body); } catch (_) {}
        invalidateCandidate();
      }
      const response = await originalFetch(input, init);
      if (isValidation) {
        try {
          const body = await response.clone().json();
          if (response.ok && body.success && body.mutation === 'NONE' && body.xray_valid === true && requestCandidate) {
            const candidate = {
              routing: body.routing || requestCandidate.routing,
              policy: body.policy || requestCandidate.policy
            };
            const changed = renderDelta(candidate);
            if (changed && !stopLatched) {
              validatedCandidate = candidate;
              applyButtons().forEach(button => { button.disabled = false; });
              setResult('Проверка Xray пройдена. Изменения готовы к применению.', 'ok');
            } else if (!changed) {
              setResult('Проверка пройдена, но изменений относительно текущей конфигурации нет.', 'ok');
            }
          }
        } catch (_) {}
      }
      return response;
    };
    wrapped.__freenetRoutingValidationCapture = true;
    window.fetch = wrapped;
  }

  function describeApply(body, fallback = '') {
    const mutation = String(body?.mutation || (body?.success ? 'APPLIED' : 'FAILED'));
    const rollback = String(body?.rollback || 'UNKNOWN');
    const result = String(body?.result || body?.error || fallback || 'Операция завершена.');
    return `Результат: ${mutation}\nОткат: ${rollback}\n${result}`;
  }

  function isStop(body) {
    const mutation = String(body?.mutation || '');
    const rollback = String(body?.rollback || '');
    return mutation === 'STOP' || rollback === 'FAILED' || rollback === 'UNKNOWN';
  }

  async function applyValidatedCandidate() {
    if (stopLatched) {
      setResult('STOP: предыдущий откат не подтверждён. Новое изменение заблокировано до проверки фактического состояния.', 'bad');
      return;
    }
    if (!validatedCandidate) {
      setResult('Сначала нажмите «Проверить изменения».', 'bad');
      return;
    }
    if (!window.confirm('Применить проверенные правила?\n\nFreeNet создаст резервную точку, применит изменения, проверит результат и автоматически откатится при ошибке.')) return;

    const candidate = validatedCandidate;
    validatedCandidate = null;
    const buttons = applyButtons();
    buttons.forEach(button => { button.disabled = true; button.dataset.previousText = button.textContent; button.textContent = 'Применяем…'; });
    setResult('Создаём резервную точку и применяем проверенные правила…');

    try {
      const response = await originalFetch('/api/routing/apply', {
        method:'POST',
        headers:{'Content-Type':'application/json'},
        body:JSON.stringify(candidate)
      });
      let body = {};
      try { body = await response.json(); } catch (_) {}
      if (isStop(body)) {
        stopLatched = true;
        setResult(`${describeApply(body)}\nSTOP: дальнейшие routing mutation запрещены до проверки фактического состояния.`, 'bad');
        q('#rv2ApplyResult')?.classList.add('rv2-apply-stop'); q('#rv2RulesApplyResult')?.classList.add('rv2-apply-stop');
        return;
      }
      if (!response.ok || !body.success || !body.applied) {
        setResult(describeApply(body, `HTTP ${response.status}`), 'bad');
        return;
      }
      const message = `${describeApply(body)}\nLive state будет перечитан после обновления страницы.`;
      try { sessionStorage.setItem('freenet-routing-last-result', message); } catch (_) {}
      location.reload();
    } catch (_) {
      stopLatched = true;
      setResult('Связь прервалась, поэтому результат применения и отката не подтверждён. STOP: не повторяйте mutation/изменение до проверки фактического состояния.', 'bad');
      q('#rv2ApplyResult')?.classList.add('rv2-apply-stop'); q('#rv2RulesApplyResult')?.classList.add('rv2-apply-stop');
    } finally {
      buttons.forEach(button => { button.textContent = button.dataset.previousText || (button.id === 'rv2ApplyRules' ? 'Применить' : 'Применить проверенный candidate'); delete button.dataset.previousText; });
    }
  }

  function enhanceWorkspace() {
    const page = routingPage();
    const workspace = page && q('#routingV2Workspace', page);
    if (!workspace) return false;
    installCompatibilityStyle();
    canonicalHead(page);
    q('#policyBuilderPreview', page)?.remove();

    const toolbar = q('.rv2-toolbar', workspace);
    if (toolbar && !q('#rv2ApplyConfig', toolbar)) {
      const apply = document.createElement('button');
      apply.id = 'rv2ApplyConfig';
      apply.type = 'button';
      apply.className = 'btn primary';
      apply.disabled = true;
      apply.textContent = 'Применить проверенный candidate';
      apply.addEventListener('click', applyValidatedCandidate);
      toolbar.appendChild(apply);
    }

    const card = q('#rv2ConfigPanel .rv2-card', workspace);
    if (card && !q('#rv2ApplyPreview', card)) {
      const preview = document.createElement('div');
      preview.id = 'rv2ApplyPreview';
      preview.className = 'rv2-apply-preview';
      const result = document.createElement('div');
      result.id = 'rv2ApplyResult';
      result.className = 'rv2-notice';
      card.append(preview, result);
    }

    const danger = q('.rv2-danger-note', workspace);
    if (danger) danger.innerHTML = '<b>Controlled apply включён.</b> Применить можно только candidate, который только что прошёл Xray validation. FreeNet создаёт snapshot, выполняет post-check и rollback при failure. ROLLBACK FAILED/UNKNOWN = STOP.';

    installValidationCapture();
    const rulesApply = q('#rv2ApplyRules', workspace);
    if (rulesApply && rulesApply.dataset.applyBound !== '1') {
      rulesApply.dataset.applyBound = '1';
      rulesApply.addEventListener('click', applyValidatedCandidate);
    }
    if (workspace.dataset.ruleDraftListener !== '1') {
      workspace.dataset.ruleDraftListener = '1';
      document.addEventListener('freenet:routing-draft-changed', () => invalidateCandidate('Черновик правил изменён. Выполните проверку ещё раз.'));
    }
    if (!workspace.dataset.applyBound) {
      workspace.dataset.applyBound = '1';
      workspace.addEventListener('input', event => {
        if (event.target?.matches?.('#rv2ConfigEditor')) invalidateCandidate('Конфигурация изменена. Перед применением нужна новая проверка Xray.');
      }, true);
      workspace.addEventListener('click', event => {
        if (event.target?.closest?.('#rv2FormatConfig,#rv2ReloadConfig,#rv2BuildConfig,.rv2-rule-tools')) {
          invalidateCandidate('Черновик изменён. Перед применением нужна новая проверка.');
        }
      }, true);
    }

    try {
      const previous = sessionStorage.getItem('freenet-routing-last-result');
      if (previous) {
        sessionStorage.removeItem('freenet-routing-last-result');
        setResult(previous, 'ok');
      }
    } catch (_) {}
    void loadBaseline();
    return true;
  }

  function showMountFailure() {
    const page = routingPage();
    if (!page || q('#routingV2MountFailure', page)) return;
    q('#policyBuilderPreview', page)?.remove();
    const failure = document.createElement('div');
    failure.id = 'routingV2MountFailure';
    failure.className = 'card';
    failure.innerHTML = '<div class="notice show bad">Routing v2 UI не загрузился. Live routing не изменён. Применение заблокировано.</div>';
    const head = q('.page-head', page);
    if (head) head.insertAdjacentElement('afterend', failure);
    else page.prepend(failure);
  }

  function settle() {
    installCompatibilityStyle();
    const page = routingPage();
    if (!page) return;
    if (!enhanceWorkspace()) showMountFailure();
    canonicalHead(page);
  }

  function boot() {
    settle();
    requestAnimationFrame(() => { settle(); requestAnimationFrame(settle); });
    window.addEventListener('hashchange', () => requestAnimationFrame(settle));
    const page = routingPage();
    const head = page && q('.page-head', page);
    if (head && typeof MutationObserver === 'function') {
      new MutationObserver(() => {
        if (!q('[data-routing-v2-head="1"]', head)) canonicalHead(page);
      }).observe(head, {childList:true,subtree:true,characterData:true});
    }
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', boot, {once:true});
  else boot();
})();
