from pathlib import Path

root = Path(__file__).resolve().parents[1]
js_path = root / "freenet-ui" / "web" / "operation-coordinator.js"
test_path = root / "tests" / "test_vpn_ui.cjs"
go_test_path = root / "freenet-ui" / "v038_safe_polish_test.go"

marker = "// Issue #363: safe v0.3.8 visual polish without a DOM observer."
js = js_path.read_text()
if marker not in js:
    js += r'''

// Issue #363: safe v0.3.8 visual polish without a DOM observer.
// This block is deliberately event-driven: static CSS + the current-refresh request lifecycle.
// Do not replace this with a whole-document MutationObserver.
(() => {
  const q = (selector, root = document) => root.querySelector(selector);
  const root = document.documentElement;

  function ensureRefreshResult() {
    let result = q('#fnRefreshResult');
    if (result) return result;
    const update = q('#updateBtn');
    const row = update?.closest('.action-row');
    const panel = q('.vpn-current-panel');
    if (!row || !panel || row.parentNode !== panel) return null;
    result = document.createElement('div');
    result.id = 'fnRefreshResult';
    result.setAttribute('role', 'status');
    result.setAttribute('aria-live', 'polite');
    row.insertAdjacentElement('afterend', result);
    return result;
  }

  function showRefreshResult(text, tone = '') {
    const result = ensureRefreshResult();
    if (!result) return;
    result.textContent = String(text || '').trim();
    result.className = tone ? `fn-refresh-result-${tone}` : '';
    root.classList.toggle('fn-current-refresh-visible', !!result.textContent);
  }

  function clearRefreshResult() {
    const result = q('#fnRefreshResult');
    if (result) { result.textContent = ''; result.className = ''; }
    root.classList.remove('fn-current-refresh-visible');
  }

  function refreshOutcomeMessage(body, response) {
    if (!body || typeof body !== 'object') {
      return response && response.ok
        ? 'Обновление endpoint завершено. Проверьте фактическое состояние VPN.'
        : 'Обновление endpoint не завершено. Текущий VPN не изменяйте до проверки состояния.';
    }
    if (body.message) return body.message;
    if (!response.ok || body.success !== true) return body.primary_error || body.error || 'Обновление endpoint не завершено.';
    return ({
      no_new: 'Нового endpoint для текущей локации нет. Текущий VPN сохранён.',
      current_better: 'Текущий VPN лучше. Переключение не выполнялось.',
      check_failed: 'Новый endpoint не прошёл проверку. Текущий VPN сохранён.',
      applied: 'Новый endpoint применён и проверен.'
    })[body.outcome] || 'Проверка endpoint завершена.';
  }

  function install() {
    if (q('#FreeNetV038SafePolish')) return;
    const style = document.createElement('style');
    style.id = 'FreeNetV038SafePolish';
    style.textContent = `
      @media(min-width:981px){:root{--sidebar:238px}.sidebar{width:238px!important;padding:22px 14px!important}.sidebar .brand{font-size:24px!important;margin:4px 14px 20px!important;padding:0!important}.sidebar .nav{gap:6px!important}.nav-btn{min-height:48px!important;padding:0 16px!important;border-radius:12px!important;font-size:14px!important;gap:12px!important;align-items:center!important}.nav-btn .nav-icon{width:20px!important;height:20px!important;flex:0 0 20px!important;font-size:17px!important}.nav-btn.active{background:linear-gradient(180deg,#17335a,#112947)!important;box-shadow:inset 0 0 0 1px #3b68a6!important}}
      .fn-current-connected{font-size:11px!important;min-height:30px!important;padding:5px 10px!important}
      .fn-endpoint-refresh{min-height:52px!important;padding:10px 16px!important;justify-content:center!important;text-align:center!important;gap:10px!important;background:linear-gradient(180deg,#18304d,#12263e)!important;border-color:#41678f!important}
      .fn-endpoint-copy{justify-items:center!important;text-align:center!important;gap:3px!important}.fn-endpoint-copy strong{font-size:14px!important;line-height:1.15!important}.fn-endpoint-copy small{font-size:11px!important;line-height:1.2!important}.fn-endpoint-icon{width:22px!important;height:22px!important;color:#8ab9ff!important}
      .current-health{justify-content:center!important;align-items:center!important;text-align:center!important;font-size:14px!important;line-height:1.45!important;padding:13px 14px!important;white-space:pre-line!important}.current-health:before{flex:0 0 25px!important;width:25px!important;height:25px!important}.current-help{font-size:13px!important;line-height:1.55!important;color:#a7bbd2!important;margin-top:12px!important}
      #fnRefreshResult{margin:10px 0 0;padding:10px 12px;border:1px solid #326da4;border-radius:11px;background:linear-gradient(90deg,#0d3155,#0a2746);color:#c9def6;font-size:12.5px;line-height:1.45;text-align:center}#fnRefreshResult:empty{display:none}.fn-refresh-result-ok{border-color:rgba(52,226,160,.55)!important;color:#bdf9df!important;background:linear-gradient(90deg,rgba(15,112,80,.28),rgba(13,70,60,.16))!important}.fn-refresh-result-bad{border-color:rgba(255,112,112,.5)!important;color:#ffd0d0!important;background:linear-gradient(90deg,rgba(105,30,42,.32),rgba(67,24,34,.18))!important}.fn-current-refresh-visible #bestServerStatus{display:none!important}
      #bestServerAdvanced.fn-topbar-vpn-picker #profileSearch{height:46px!important;min-height:46px!important;padding:0 16px 0 44px!important;border-radius:12px!important;font-size:13px!important;background:linear-gradient(180deg,#0d2136,#0a1b2d)!important;border-color:#3a6088!important;box-shadow:inset 0 1px rgba(255,255,255,.025)!important}#bestServerAdvanced.fn-topbar-vpn-picker #profileSearch:focus{border-color:#5e9cff!important;box-shadow:0 0 0 3px rgba(74,139,255,.14)!important;outline:none!important}#bestServerAdvanced.fn-topbar-vpn-picker .manual-search-icon{left:14px!important;width:18px!important;height:18px!important;color:#79a8e8!important}
      @media(max-width:820px){.nav-btn{min-height:44px!important}.current-help{font-size:12px!important}#fnRefreshResult{font-size:12px!important}}
    `;
    document.head.appendChild(style);
    ensureRefreshResult();

    document.addEventListener('click', event => {
      const target = event.target?.closest?.('#updateBtn,#bestServerCheckCurrent,#bestServerRefresh,.vpn-option-apply,.vpn-option-retry');
      if (!target) return;
      if (target.id === 'updateBtn') showRefreshResult('Получаем новый endpoint и сравниваем его с текущим VPN…');
      else clearRefreshResult();
    }, true);

    const previousFetch = window.fetch.bind(window);
    window.fetch = async function(input, init) {
      const url = typeof input === 'string' ? input : (input && input.url) || '';
      const method = String((init && init.method) || (input && input.method) || 'GET').toUpperCase();
      const currentRefresh = method === 'POST' && url.split('?')[0] === '/api/vpn/current-refresh';
      if (!currentRefresh) return previousFetch(input, init);
      try {
        const response = await previousFetch(input, init);
        let body = null;
        try { body = await response.clone().json(); } catch (_) {}
        const message = refreshOutcomeMessage(body, response);
        const ok = response.ok && body && body.success === true;
        showRefreshResult(message, ok ? 'ok' : 'bad');
        return response;
      } catch (error) {
        showRefreshResult('Связь прервалась во время обновления endpoint. Проверьте фактическое состояние перед повтором.', 'bad');
        throw error;
      }
    };
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', install, {once:true});
  else install();
})();
'''
    js_path.write_text(js)

test = test_path.read_text()
needle = "    await page.waitForTimeout(150);\n"
liveness = r'''    // v0.3.8 regression guard: any ordinary DOM mutation must leave the browser event loop alive.
    await page.evaluate(() => {
      window.__freenetLiveness = 0;
      const probe = document.createElement('div');
      probe.id = 'fn-liveness-probe';
      document.body.appendChild(probe);
      for (let i = 0; i < 40; i++) {
        probe.textContent = `mutation-${i}`;
        probe.classList.toggle('tick', i % 2 === 0);
      }
      setTimeout(() => { window.__freenetLiveness = 1; probe.remove(); }, 60);
    });
    await page.waitForFunction(() => window.__freenetLiveness === 1, null, {timeout: 1500});
'''
if "__freenetLiveness" not in test:
    if needle not in test:
        raise SystemExit("browser insertion point not found")
    test = test.replace(needle, needle + liveness, 1)
    test_path.write_text(test)

go_test_path.write_text(r'''package main

import (
    "os"
    "strings"
    "testing"
)

func TestSafeV038PolishHasNoSelfMutatingObserver(t *testing.T) {
    data, err := os.ReadFile("web/operation-coordinator.js")
    if err != nil { t.Fatal(err) }
    src := string(data)
    marker := "// Issue #363: safe v0.3.8 visual polish without a DOM observer."
    pos := strings.Index(src, marker)
    if pos < 0 { t.Fatal("safe v0.3.8 polish marker missing") }
    safe := src[pos:]
    if strings.Contains(safe, "MutationObserver") && !strings.Contains(safe, "Do not replace this with a whole-document MutationObserver") {
        t.Fatal("safe v0.3.8 block unexpectedly contains MutationObserver code")
    }
    bad := "new MutationObserver(run).observe(document.documentElement, {subtree:true, childList:true, characterData:true})"
    if strings.Contains(src, bad) { t.Fatal("v0.3.8 self-mutating observer regression detected") }
    for _, needle := range []string{
        "FreeNetV038SafePolish",
        "fn-current-refresh-visible",
        "white-space:pre-line",
        "min-height:52px",
        "height:46px",
        "font-size:14px",
    } {
        if !strings.Contains(safe, needle) { t.Fatalf("safe polish contract missing %q", needle) }
    }
}
''')
