from pathlib import Path


def replace(path, old, new):
    p = Path(path)
    s = p.read_text(encoding='utf-8')
    if old not in s:
        raise SystemExit(f'missing expected text in {path}: {old!r}')
    p.write_text(s.replace(old, new, 1), encoding='utf-8')

replace('freenet-ui/best_server_quality.go', 'bestServerQualityCandidateTimeout     = 26 * time.Second', 'bestServerQualityCandidateTimeout     = 55 * time.Second')
replace('freenet-ui/best_server_quality.go', 'bestServerQualityScanTimeout          = 150 * time.Second', 'bestServerQualityScanTimeout          = 420 * time.Second')
replace('freenet-ui/best_server_ux.go', 'bestServerCurrentScanTimeout  = 45 * time.Second', 'bestServerCurrentScanTimeout  = 75 * time.Second')
replace('freenet-ui/best_server_targeted.go', 'bestServerTargetedRetryTimeout = 42 * time.Second', 'bestServerTargetedRetryTimeout = 75 * time.Second')
replace('freenet-ui/best_server_speedtest.go', 'bestServerSpeedtestListTimeout  = 4 * time.Second', 'bestServerSpeedtestListTimeout  = 5 * time.Second')
replace('freenet-ui/best_server_speedtest.go', 'bestServerSpeedtestRunTimeout   = 6 * time.Second', 'bestServerSpeedtestRunTimeout   = 8 * time.Second')
replace('freenet-ui/best_server_speedtest.go', 'bestServerSpeedtestServerTries  = 2', 'bestServerSpeedtestServerTries  = 3')
replace('freenet-ui/best_server_speedtest.go', '"--connect-timeout", "3", "--max-time", "4",', '"--connect-timeout", "3", "--max-time", "5",')
replace('freenet-ui/best_server_speedtest.go', '"--connect-timeout", "3", "--max-time", "6",', '"--connect-timeout", "3", "--max-time", "8",')
replace('freenet-ui/best_server_media_quality.go', 'bestServerMediaTimeout       = 10 * time.Second', 'bestServerMediaTimeout       = 22 * time.Second')
replace('freenet-ui/best_server_media_quality.go', 'bestServerServiceTimeout     = 6 * time.Second', 'bestServerServiceTimeout     = 8 * time.Second')

# Avoid spending a full deep-probe slot on a candidate that cannot finish within
# the remaining global budget. The new global budget intentionally supports a
# complete current baseline + preflight + at least one full 4-candidate batch.
replace('freenet-ui/best_server_quality.go', 'if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < bestServerQualityCandidateTimeout+time.Second {', 'if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < bestServerQualityCandidateTimeout+2*time.Second {')

ui = Path('freenet-ui/web/operation-coordinator.js')
s = ui.read_text(encoding='utf-8')
marker = '// Issue #350: v0.3.7 final render-scale alignment.'
if marker in s:
    raise SystemExit('issue 350 UI patch already present')
s += r'''

// Issue #350: v0.3.7 final render-scale alignment.
// Desktop typography and alignment correction only; responsive/mobile contracts remain intact.
(() => {
  function installV037Scale() {
    if (document.getElementById('FreeNetV037FinalScale')) return;
    const style = document.createElement('style');
    style.id = 'FreeNetV037FinalScale';
    style.textContent = `
      @media (min-width:1181px) and (min-height:821px){
        .content{width:min(1370px,calc(100% - 46px))!important;padding-top:18px!important}
        .page[data-page-view="overview"] .page-head{margin-bottom:17px!important}
        .page[data-page-view="overview"] .page-head h1{font-size:34px!important;line-height:1.08!important}
        .page[data-page-view="overview"] .page-head p{font-size:13px!important;line-height:1.4!important;margin-top:6px!important}
        #quickActionsSection{padding:25px 25px 23px!important;border-radius:20px!important}
        .best-v4-shell{grid-template-columns:minmax(365px,390px) minmax(0,1fr)!important;gap:0 32px!important}
        .vpn-current-panel{padding:9px 31px 0 0!important}
        .best-v4-label{font-size:13px!important}
        .best-v4-name{font-size:25px!important;line-height:1.18!important}
        .best-v4-endpoint{font-size:13px!important;margin:11px 0 18px!important}
        .vpn-current-panel #bestCurrentFlag{width:36px!important;height:25px!important;margin-top:4px!important}
        .vpn-current-panel .best-v4-metrics{gap:11px!important}
        .vpn-current-panel .best-v4-pill{min-height:82px!important;padding:13px 12px!important}
        .vpn-current-panel .best-v4-pill span.metric-label{font-size:12px!important}
        .vpn-current-panel .best-v4-pill b{font-size:21px!important}
        .best-quality{font-size:13px!important;line-height:1.42!important;margin:13px 0 12px!important}
        .vpn-current-panel>#bestServerCheckCurrent{min-height:50px!important;font-size:15px!important}
        .fn-endpoint-refresh{min-height:46px!important;padding:9px 15px!important}
        .fn-endpoint-copy strong{font-size:13px!important}
        .fn-endpoint-copy small{font-size:10px!important}
        .current-health{font-size:13px!important;line-height:1.42!important;padding:12px 13px!important}
        .current-help{font-size:12px!important;line-height:1.5!important}
        .vpn-section-head{margin-bottom:14px!important;align-items:center!important}
        .vpn-section-head h3{font-size:22px!important}
        .vpn-section-head .hint{font-size:13px!important}
        .vpn-section-head #bestServerRefresh{min-height:50px!important;font-size:15px!important;padding:10px 20px!important}
        .best-v4-result.show{gap:13px!important}
        .vpn-option{min-height:153px!important;padding:15px 16px 13px!important}
        .vpn-option-head{margin-bottom:12px!important;align-items:center!important}
        .vpn-option-title h4{font-size:18px!important}
        .vpn-option-title .flag-icon{width:36px!important;height:25px!important}
        .vpn-state-badge{min-height:35px!important;font-size:12px!important;padding:6px 13px!important;align-items:center!important}
        .vpn-option .btn{min-height:41px!important;font-size:13px!important;padding:8px 15px!important}
        .vpn-option .best-v4-pill{min-height:61px!important;padding:5px 13px 5px 9px!important}
        .vpn-option .best-v4-pill span.metric-label{font-size:11px!important}
        .vpn-option .best-v4-pill b{font-size:17px!important}
        .best-v4-reason{font-size:12px!important;gap:7px!important;margin-top:11px!important}
        .vpn-detail-chip{min-height:29px!important;font-size:11px!important;padding:4px 10px!important}
        .vpn-rejected-note{font-size:12px!important;line-height:1.42!important}
        .best-v4-status{font-size:12px!important;line-height:1.42!important;padding:11px 13px!important}
        .overview-approved-top.fn-render-facts{align-items:center!important;gap:10px!important}
        .fn-render-fact{min-width:142px!important;min-height:48px!important;padding:8px 12px!important;align-items:center!important}
        .fn-render-fact>span:not(.fn-top-fact-icon){font-size:10px!important}
        .fn-render-fact>strong{font-size:12px!important;line-height:1.2!important}
        .fn-render-xkeen{height:44px!important;font-size:12px!important}
        #bestServerAdvanced.fn-topbar-vpn-picker #profileSearch,#bestServerAdvanced.fn-topbar-vpn-picker #profilesTrigger{height:44px!important;min-height:44px!important;font-size:13px!important}
      }
    `;
    document.head.appendChild(style);
  }
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', installV037Scale, {once:true}); else installV037Scale();
})();
'''
ui.write_text(s, encoding='utf-8')

Path('freenet-ui/overview_v037_contract_test.go').write_text(r'''package main

import (
    "os"
    "strings"
    "testing"
    "time"
)

func TestV037BestServerBudgets(t *testing.T) {
    if bestServerQualityCandidateTimeout < 50*time.Second { t.Fatalf("candidate deep-probe budget too small: %v", bestServerQualityCandidateTimeout) }
    if bestServerQualityScanTimeout < 6*bestServerQualityCandidateTimeout { t.Fatalf("global scan budget cannot finish a full measured set: %v", bestServerQualityScanTimeout) }
    if bestServerCurrentScanTimeout < bestServerQualityCandidateTimeout { t.Fatalf("current baseline budget smaller than candidate budget") }
    if bestServerTargetedRetryTimeout < bestServerQualityCandidateTimeout { t.Fatalf("targeted retry budget smaller than candidate budget") }
    if bestServerSpeedtestRunTimeout < 8*time.Second || bestServerSpeedtestServerTries < 3 { t.Fatalf("speedtest retry budget not expanded") }
    if bestServerMediaTimeout < 20*time.Second { t.Fatalf("media budget too small: %v", bestServerMediaTimeout) }
}

func TestV037OverviewScaleContract(t *testing.T) {
    data, err := os.ReadFile("web/operation-coordinator.js")
    if err != nil { t.Fatal(err) }
    s := string(data)
    for _, needle := range []string{
        "Issue #350: v0.3.7 final render-scale alignment",
        "width:min(1370px,calc(100% - 46px))",
        ".page[data-page-view=\\\"overview\\\"] .page-head h1{font-size:34px",
        ".best-v4-shell{grid-template-columns:minmax(365px,390px)",
        ".vpn-option-title h4{font-size:18px",
        ".vpn-option .best-v4-pill b{font-size:17px",
        ".fn-render-fact>strong{font-size:12px",
    } {
        if !strings.Contains(s, needle) { t.Fatalf("v0.3.7 Overview scale contract missing %q", needle) }
    }
}
''', encoding='utf-8')
