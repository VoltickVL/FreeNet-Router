from pathlib import Path

path = Path('freenet-ui/web/operation-coordinator.js')
text = path.read_text(encoding='utf-8')
marker = 'Issue #353: v0.3.8 final UI polish'
if marker in text:
    raise SystemExit(0)
append = r'''

// Issue #353: v0.3.8 final UI polish
(() => {
  const q = (s, r = document) => r.querySelector(s);
  const css = document.createElement('style');
  css.id = 'FreeNetV038Polish';
  css.textContent = `
    .sidebar{width:238px!important;padding:22px 14px!important}
    .sidebar .brand{font-size:24px!important;margin:4px 14px 20px!important}
    .nav-btn{min-height:48px!important;padding:0 16px!important;border-radius:12px!important;font-size:14px!important;gap:12px!important;align-items:center!important}
    .nav-btn svg,.nav-btn .nav-icon{width:20px!important;height:20px!important;flex:0 0 20px!important}
    .nav-btn.active{background:linear-gradient(180deg,#17335a,#112947)!important;border-color:#3b68a6!important}
    .sidebar nav{gap:6px!important}
    .fn-current-connected{font-size:11px!important;min-height:30px!important;padding:5px 10px!important}
    .fn-endpoint-refresh{min-height:52px!important;padding:10px 16px!important;justify-content:center!important;text-align:center!important;gap:10px!important;background:linear-gradient(180deg,#18304d,#12263e)!important;border-color:#41678f!important}
    .fn-endpoint-copy{justify-items:center!important;text-align:center!important;gap:3px!important}
    .fn-endpoint-copy strong{font-size:14px!important;line-height:1.15!important}
    .fn-endpoint-copy small{font-size:11px!important;line-height:1.2!important}
    .fn-endpoint-icon{width:22px!important;height:22px!important;color:#8ab9ff!important}
    .current-health{justify-content:center!important;align-items:center!important;text-align:center!important;font-size:14px!important;line-height:1.45!important;padding:13px 14px!important}
    .current-health:before{flex:0 0 25px!important;width:25px!important;height:25px!important}
    .current-help{font-size:13px!important;line-height:1.55!important;color:#a7bbd2!important;margin-top:12px!important}
    #fnRefreshResult{margin-top:10px;padding:10px 12px;border:1px solid #326da4;border-radius:11px;background:linear-gradient(90deg,#0d3155,#0a2746);color:#c9def6;font-size:12.5px;line-height:1.45;text-align:center}
    #fnRefreshResult:empty{display:none}
    #bestServerStatus.fn-refresh-shadow{display:none!important}
    #bestServerAdvanced.fn-topbar-vpn-picker #profileSearch{height:46px!important;min-height:46px!important;padding:0 16px 0 44px!important;border-radius:12px!important;font-size:13px!important;background:linear-gradient(180deg,#0d2136,#0a1b2d)!important;border-color:#3a6088!important;box-shadow:inset 0 1px rgba(255,255,255,.025)!important}
    #bestServerAdvanced.fn-topbar-vpn-picker #profileSearch:focus{border-color:#5e9cff!important;box-shadow:0 0 0 3px rgba(74,139,255,.14)!important;outline:none!important}
    #bestServerAdvanced.fn-topbar-vpn-picker .manual-search-icon{left:14px!important;width:18px!important;height:18px!important;color:#79a8e8!important}
    @media(max-width:1180px){.sidebar{width:214px!important}.nav-btn{font-size:13px!important;padding:0 13px!important}}
    @media(max-width:820px){.sidebar{width:auto!important;padding:12px!important}.nav-btn{min-height:44px!important}.current-help{font-size:12px!important}}
  `;
  document.head.appendChild(css);

  function normalizeHealth() {
    const box = q('#bestCurrentHealth');
    if (!box) return;
    const text = (box.textContent || '').trim();
    if (text === 'Текущий VPN работает стабильно. Скорость и отклик в норме.') {
      box.innerHTML = '<span>Текущий VPN работает стабильно.<br><span>Скорость и отклик в норме.</span></span>';
    }
  }

  function mountRefreshResult() {
    const panel = q('.vpn-current-panel');
    if (!panel) return null;
    let result = q('#fnRefreshResult');
    if (!result) {
      result = document.createElement('div');
      result.id = 'fnRefreshResult';
      const help = q('#bestCurrentHelp');
      if (help && help.parentNode === panel) panel.insertBefore(result, help);
      else panel.appendChild(result);
    }
    return result;
  }

  const refreshPattern = /(endpoint|обновлен|обновлён|свежий|Свежий|Текущий VPN лучше|Безопасное обновление|безопасного обновления|Нового endpoint|Получаем свежий)/i;
  function syncRefreshResult() {
    normalizeHealth();
    const status = q('#bestServerStatus');
    const result = mountRefreshResult();
    if (!status || !result) return;
    const text = (status.textContent || '').trim();
    if (text && refreshPattern.test(text)) {
      result.textContent = text;
      status.classList.add('fn-refresh-shadow');
    } else {
      status.classList.remove('fn-refresh-shadow');
    }
  }

  function polishEndpointButton() {
    const button = q('#updateBtn');
    if (!button) return;
    button.classList.add('fn-endpoint-refresh');
    const copy = q('.fn-endpoint-copy', button);
    if (copy) {
      const strong = q('strong', copy); if (strong) strong.textContent = 'Обновить endpoint';
      const small = q('small', copy); if (small) small.textContent = 'Новый IP в этой же локации';
    }
  }

  const run = () => { polishEndpointButton(); normalizeHealth(); mountRefreshResult(); syncRefreshResult(); };
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', run, {once:true}); else run();
  new MutationObserver(run).observe(document.documentElement, {subtree:true, childList:true, characterData:true});
})();
'''
path.write_text(text + append, encoding='utf-8')
Path('.github/scripts/apply_v038.py').unlink(missing_ok=True)
Path('.github/workflows/v038-codemod.yml').unlink(missing_ok=True)
