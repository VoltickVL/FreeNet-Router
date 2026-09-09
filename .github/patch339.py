from pathlib import Path

p = Path('freenet-ui/web/operation-coordinator.js')
s = p.read_text()

old = """      if (url === '/api/network-profile/apply' && body && body.operation === 'provider' && typeof body.profile_id === 'string' && body.profile_id) {
        return {kind: 'provider', target: body.profile_id, startedAt: Date.now()};
      }
"""
new = old + """      if (url === '/api/vpn/current-refresh' && body && body.confirm === true) {
        return {kind: 'refresh', target: 'current', startedAt: Date.now()};
      }
"""
assert old in s, 'mutationMeta provider block not found'
s = s.replace(old, new, 1)

old = """      return jsonResponse(200, {
        success: true, applied: true, operation: 'provider', profile_id: meta.target,
        operation_id: op.id, rollback_state: 'NOT_NEEDED', message: op.message || 'VPN-профиль применён и проверен'
      });
"""
new = """      if (meta.kind === 'refresh') {
        return jsonResponse(200, {success: true, outcome: 'applied', applied: true, mutation: 'APPLIED',
          operation_id: op.id, rollback_state: 'NOT_NEEDED', message: op.message || 'Свежий endpoint применён и проверен'});
      }
""" + old
assert old in s, 'terminal provider block not found'
s = s.replace(old, new, 1)

old = "const deadline = Date.now() + 120000;"
assert old in s, 'reconcile deadline not found'
s = s.replace(old, "const deadline = Date.now() + (meta.kind === 'refresh' ? 240000 : 120000);", 1)

old = "const retry = document.createElement('button'); retry.type = 'button'; retry.className = 'btn secondary vpn-option-retry'; retry.textContent = 'Проверить снова'; actions.appendChild(retry);"
new = "const retry = document.createElement('button'); retry.type = 'button'; retry.className = 'btn secondary vpn-option-retry'; retry.dataset.candidateId = candidate.id; retry.textContent = 'Проверить снова'; actions.appendChild(retry);"
assert old in s, 'retry render block not found'
s = s.replace(old, new, 1)

marker = "  function installUpdateQualityFollowup() {\n"
assert marker in s, 'update followup marker not found'
funcs = r'''  async function retryCandidate(candidate, button) {
    if (scanBusy || applyBusy || externalBusy || !candidate || !candidate.id) return;
    const original = button && button.textContent;
    try {
      setBusy('retry');
      if (button) { button.disabled = true; button.textContent = 'Проверяем…'; }
      setText(qs('#bestServerStatus'), `Повторно проверяем только ${profileDisplayName(candidate, 'этот сервер')}… Текущий VPN не изменяется.`);
      const response = await fetch(`/api/vpn/best-candidate?id=${encodeURIComponent(candidate.id)}`, {cache:'no-store', signal:AbortSignal.timeout(50000)});
      const body = await response.json().catch(()=>null);
      if (!response.ok || !body || body.success !== true || !Array.isArray(body.candidates)) {
        setText(qs('#bestServerStatus'), (body && (body.message || body.error)) || 'Повторная проверка не завершена. Текущий VPN не изменён.');
        return;
      }
      const updated = body.candidates.find(item => item && item.id === candidate.id);
      if (!updated) {
        setText(qs('#bestServerStatus'), 'Повторная проверка не вернула выбранный сервер. Текущий VPN не изменён.');
        return;
      }
      alternatives = alternatives.map(item => item.id === candidate.id ? updated : item);
      const candidates = currentQuality ? [Object.assign({}, currentQuality, {current:true}), ...alternatives] : alternatives.slice();
      renderBestResult(Object.assign({}, body, {candidates, profiles_scanned: 1, profiles_total: 1}));
      setText(qs('#bestServerStatus'), body.message || (updated.eligible ? 'Сервер прошёл повторную проверку.' : 'Сервер снова не прошёл все проверки.'));
    } catch (_) {
      setText(qs('#bestServerStatus'), 'Повторная проверка прервалась. Текущий VPN не изменён.');
    } finally {
      if (button && button.isConnected) { button.disabled = false; if (original) button.textContent = original; }
      setBusy(false);
    }
  }

  async function refreshCurrentVPN() {
    if (scanBusy || applyBusy || externalBusy) return;
    const update = qs('#updateBtn');
    applyBusy = true;
    setBusy(false);
    if (update) { update.disabled = true; setButtonLabel(update, 'Проверяем обновление…', 'refresh'); }
    setText(qs('#bestServerStatus'), 'Получаем свежий endpoint и сравниваем его с текущим VPN до переключения…');
    try {
      const response = await fetch('/api/vpn/current-refresh', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({confirm:true})});
      const body = await response.json().catch(()=>null);
      if (!response.ok || !body || body.success !== true) {
        const detail = body && (body.primary_error || body.error || body.message);
        setText(qs('#bestServerStatus'), detail || 'Безопасное обновление не завершено. Не повторяйте операцию до проверки состояния.');
        return;
      }
      if (body.current) renderCurrentQuality({scanned_at:new Date().toISOString(), candidates:[Object.assign({}, body.current, {current:true})]});
      if (body.outcome === 'applied') {
        const expected = body.candidate && body.candidate.endpoint;
        const status = expected ? await waitForEndpoint(expected) : null;
        if (!status) {
          setText(qs('#bestServerStatus'), 'Свежий endpoint применён, но live-state ещё не подтверждён в браузере. Не повторяйте операцию.');
          return;
        }
        currentQuality = body.candidate ? Object.assign({}, body.candidate, {current:true}) : null;
        renderOverviewTopbarFromStatus(status);
        if (currentQuality) renderCurrentQuality({scanned_at:new Date().toISOString(), candidates:[currentQuality]});
        clearAlternatives('Текущий endpoint обновлён. Для нового сравнения подберите серверы снова.');
      }
      setText(qs('#bestServerStatus'), body.message || ({no_new:'Нового endpoint нет. Текущий VPN сохранён.',current_better:'Текущий VPN лучше. Переключение не выполнялось.',check_failed:'Свежий endpoint не прошёл проверку. Текущий VPN сохранён.',applied:'Свежий endpoint применён и проверен.'}[body.outcome] || 'Проверка завершена.'));
    } catch (_) {
      setText(qs('#bestServerStatus'), 'Связь прервалась во время безопасного обновления. Проверьте фактическое состояние перед повтором.');
    } finally {
      applyBusy = false;
      setBusy(false);
      if (update) { update.disabled = externalBusy; setButtonLabel(update, 'Обновить и проверить', 'refresh'); }
    }
  }

'''
s = s.replace(marker, funcs + marker, 1)

old = "if(typeof act!=='function'||act.__freenetUpdateQualityFollowup)return;const previous=act;const wrapped=async function(action){await previous(action);if(action!=='update')return;try{if(typeof loadStatus==='function')await loadStatus();if(lastStatus&&!lastStatus.busy&&!lastStatus.updater_busy&&lastStatus.xray_online){await wait(250);await scanCurrentVPN();}}catch(_){}};wrapped.__freenetUpdateQualityFollowup=true;act=wrapped;"
new = "if(typeof act!=='function'||act.__freenetUpdateQualityFollowup)return;const previous=act;const wrapped=async function(action){if(action==='update')return refreshCurrentVPN();return previous(action);};wrapped.__freenetUpdateQualityFollowup=true;act=wrapped;"
assert old in s, 'legacy update wrapper not found'
s = s.replace(old, new, 1)

old = "if(button.id==='bestServerCheckCurrent'){void scanCurrentVPN();return}if(button.id==='bestServerRefresh'||button.matches('.vpn-option-retry')){void scanBestServer();return}if(button.matches('.vpn-option-apply')){const candidate=alternatives.find(item=>item.eligible&&item.id===button.dataset.candidateId);if(candidate)void applyCandidate(candidate);}"
new = "if(button.id==='bestServerCheckCurrent'){void scanCurrentVPN();return}if(button.id==='bestServerRefresh'){void scanBestServer();return}if(button.matches('.vpn-option-retry')){const candidate=alternatives.find(item=>item.id===button.dataset.candidateId);if(candidate)void retryCandidate(candidate,button);return}if(button.matches('.vpn-option-apply')){const candidate=alternatives.find(item=>item.eligible&&item.id===button.dataset.candidateId);if(candidate)void applyCandidate(candidate);}"
assert old in s, 'legacy retry delegation not found'
s = s.replace(old, new, 1)
p.write_text(s)

p = Path('freenet-ui/best_server_targeted.go')
s = p.read_text()
old = 'applyCtx, cancel := context.WithTimeout(ctx, a.cfg.Timeout)'
assert old in s, 'request-scoped apply context not found'
s = s.replace(old, 'applyCtx, cancel := context.WithTimeout(context.Background(), a.cfg.Timeout)', 1)
p.write_text(s)

Path('freenet-ui/best_server_targeted_test.go').write_text(r'''package main

import (
    "regexp"
    "strings"
    "testing"
)

func TestBestServerFreshCandidateForCurrentPrefersExactLocation(t *testing.T) {
    candidates := []bestServerInternalCandidate{
        {Profile: subscriptionProfile{ID:"other", Name:"PL Warsaw Backup", Address:"198.51.100.2", Port:443}},
        {Profile: subscriptionProfile{ID:"exact", Name:"PL Warsaw Main", Address:"198.51.100.3", Port:443}},
    }
    got, ok := bestServerFreshCandidateForCurrent(candidates, regexp.MustCompile(`^PL Warsaw`), "PL Warsaw Main", "198.51.100.1:443")
    if !ok || got.Profile.ID != "exact" { t.Fatalf("expected exact fresh location, got %#v ok=%v", got.Profile, ok) }
}

func TestBestServerFreshCandidateForCurrentRejectsSameEndpoint(t *testing.T) {
    candidates := []bestServerInternalCandidate{{Profile: subscriptionProfile{ID:"same", Name:"PL Warsaw Main", Address:"198.51.100.1", Port:443}}}
    if _, ok := bestServerFreshCandidateForCurrent(candidates, regexp.MustCompile(`^PL Warsaw`), "PL Warsaw Main", "198.51.100.1:443"); ok { t.Fatal("same endpoint must not be treated as fresh") }
}

func TestBestServerTargetedUIContract(t *testing.T) {
    data, err := webFS.ReadFile("web/operation-coordinator.js")
    if err != nil { t.Fatal(err) }
    src := string(data)
    for _, want := range []string{"/api/vpn/best-candidate?id=", "/api/vpn/current-refresh", "retry.dataset.candidateId = candidate.id", "if(action==='update')return refreshCurrentVPN()"} {
        if !strings.Contains(src, want) { t.Fatalf("missing targeted UI contract %q", want) }
    }
    if strings.Contains(src, "button.id==='bestServerRefresh'||button.matches('.vpn-option-retry')") { t.Fatal("retry must not share full-scan path") }
}
''')

Path('.github/workflows/patch-339.yml').unlink(missing_ok=True)
Path('.github/patch339.py').unlink(missing_ok=True)
