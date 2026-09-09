from pathlib import Path
import re


def sub_once(path, pattern, replacement, flags=0):
    p = Path(path)
    text = p.read_text()
    out, count = re.subn(pattern, replacement, text, count=1, flags=flags)
    if count != 1:
        raise SystemExit(f"{path}: expected one regex match, got {count}: {pattern[:80]}")
    p.write_text(out)


def replace_once(path, old, new):
    p = Path(path)
    text = p.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected one literal match, got {count}: {old[:80]!r}")
    p.write_text(text.replace(old, new, 1))


# Backend: full Best Server scan owns a current baseline, even for Whitelist or
# other specialized current profiles excluded from the foreign alternatives.
backend = "freenet-ui/best_server_ux.go"
pattern = r'''\tcandidates := filterForeignBestServerCandidates\(all\)\n\tif len\(candidates\) == 0 \{.*?\n\tprofilesScanned := len\(candidates\)\n\tcachedCurrent, cachedOK := loadBestServerCurrentQuality\(currentEndpoint, currentFilter\)\n\tif cachedOK \{\n\t\tif currentIndex := bestServerCurrentCandidateIndex\(candidates, currentEndpoint, currentFilter\); currentIndex >= 0 \{\n\t\t\tcandidates = withoutBestServerCandidate\(candidates, currentIndex\)\n\t\t\}\n\t\}\n'''
replacement = '''\tcandidates := filterForeignBestServerCandidates(all)
\tprofilesScanned := len(candidates)

\t// A full comparison must always have its own current baseline. Reuse a
\t// fresh complete baseline when available; otherwise measure the active live
\t// outbound before probing alternatives. This is independent of whether the
\t// current profile belongs to the foreign alternative pool (for example a
\t// Whitelist profile). The measurement is read-only and never mutates VPN.
\tcurrentBaseline, currentBaselineOK := loadBestServerCurrentQuality(currentEndpoint, currentFilter)
\tcurrentBaselineCached := currentBaselineOK
\tif !currentBaselineOK {
\t\tbaselineCtx, cancelBaseline := context.WithTimeout(ctx, bestServerCurrentScanTimeout)
\t\tbaselineResponse := a.scanActiveCurrentVPNQuality(baselineCtx, currentEndpoint, currentFilter)
\t\tcancelBaseline()
\t\tif candidate, ok := currentBestServerQualityCandidate(baselineResponse); ok {
\t\t\tcurrentBaseline = candidate
\t\t\tcurrentBaselineOK = true
\t\t}
\t}
\tif currentIndex := bestServerCurrentCandidateIndex(candidates, currentEndpoint, currentFilter); currentIndex >= 0 {
\t\tcandidates = withoutBestServerCandidate(candidates, currentIndex)
\t}
\tif len(candidates) == 0 {
\t\tcurrentCandidates := []bestServerQualityCandidate{}
\t\tif currentBaselineOK {
\t\t\tcurrentCandidates = append(currentCandidates, currentBaseline)
\t\t}
\t\treturn bestServerQualityResponse{
\t\t\tSuccess: true, Available: currentBaselineOK && currentBaseline.Eligible, Candidates: currentCandidates, ProfilesScanned: profilesScanned, ProfilesTotal: profilesScanned,
\t\t\tProfilesTruncated: truncated, Mutation: "NONE", ScannedAt: time.Now().UTC().Format(time.RFC3339), CurrentEndpoint: currentEndpoint,
\t\t\tMessage: "Подходящих зарубежных Extra-профилей нет; текущий VPN сохранён и проверен отдельно от списка замен.",
\t\t}, nil
\t}
'''
sub_once(backend, pattern, lambda _: replacement, re.S)

pattern = r'''\tresponse := a\.rankMeasuredBestServerBatches\(ctx, candidates, profilesScanned, truncated, currentEndpoint, currentFilter\)\n\tif ctx\.Err\(\) != nil && len\(response\.Candidates\) == 0 \{\n\t\treturn bestServerQualityResponse\{\}, ctx\.Err\(\)\n\t\}\n\tif cachedOK \{\n\t\tresponse\.Candidates = append\(response\.Candidates, cachedCurrent\)\n\t\} else if candidate, ok := currentBestServerQualityCandidate\(response\); ok \{\n\t\tstoreBestServerCurrentQuality\(currentEndpoint, currentFilter, candidate\)\n\t\}\n'''
replacement = '''\tresponse := a.rankMeasuredBestServerBatches(ctx, candidates, profilesScanned, truncated, currentEndpoint, currentFilter)
\tif ctx.Err() != nil && len(response.Candidates) == 0 && !currentBaselineOK {
\t\treturn bestServerQualityResponse{}, ctx.Err()
\t}
\tif currentBaselineOK {
\t\tresponse.Candidates = append(response.Candidates, currentBaseline)
\t\tif !currentBaselineCached {
\t\t\tstoreBestServerCurrentQuality(currentEndpoint, currentFilter, currentBaseline)
\t\t}
\t}
'''
sub_once(backend, pattern, lambda _: replacement)
replace_once(
    backend,
    '\tif cachedOK {\n\t\tresponse.Message += " Свежий подтверждённый замер текущего VPN переиспользован без повторной тяжёлой Speedtest-проверки."\n\t}\n',
    '\tif currentBaselineCached {\n\t\tresponse.Message += " Свежий подтверждённый замер текущего VPN переиспользован без повторной тяжёлой Speedtest-проверки."\n\t} else if currentBaselineOK {\n\t\tresponse.Message += " Текущий VPN измерен в рамках этого полного сравнения."\n\t}\n',
)

# UI: move the real search + exact VPN selector into topbar, no shortcut.
js = "freenet-ui/web/operation-coordinator.js"
pattern = r'''    const guard = qs\('#quickNetworkGuard'\);\n    let manual = qs\('#bestServerAdvanced'\);\n    if \(profilesList && !manual\) \{.*?    if \(manual && guard && guard\.parentNode !== manual\) manual\.appendChild\(guard\);\n'''
replacement = '''    const guard = qs('#quickNetworkGuard');
    let manual = qs('#bestServerAdvanced');
    const topbar = qs('.topbar.overview-approved') || qs('.topbar');
    const topSummary = qs('#overviewApprovedTop');
    const topActions = qs('.top-actions');
    if (profilesList && !manual) {
      manual = document.createElement('section'); manual.id = 'bestServerAdvanced';
    }
    if (manual) manual.classList.add('fn-topbar-vpn-picker');
    if (manual && topbar && manual.parentNode !== topbar) topbar.insertBefore(manual, topSummary || topActions || null);
    if (manual && profilesList && profilesList.parentNode !== manual) manual.appendChild(profilesList);
    const exact = qs('#exactConnectRow'); if (manual && exact && exact.parentNode !== manual) manual.appendChild(exact);
    if (manual && guard && guard.parentNode !== manual) manual.appendChild(guard);
'''
sub_once(js, pattern, lambda _: replacement, re.S)

pattern = r'''  function polishTopbar\(\) \{.*?\n  \}\n\n  function fitCurrentMetrics'''
replacement = '''  function polishTopbar() {
    q('#fnManualShortcut')?.remove();
    const manual = q('#bestServerAdvanced');
    const topbar = q('.topbar.overview-approved') || q('.topbar');
    const summary = q('#overviewApprovedTop');
    const actions = q('.top-actions');
    if (manual) manual.classList.add('fn-topbar-vpn-picker');
    if (manual && topbar && manual.parentNode !== topbar) topbar.insertBefore(manual, summary || actions || null);
    const exact = q('#exactConnectRow'); if (manual && exact && exact.parentNode !== manual) manual.appendChild(exact);
  }

  function fitCurrentMetrics'''
sub_once(js, pattern, lambda _: replacement, re.S)

pattern = r'''      \.top-actions\{gap:10px!important\}\.top-status\.fn-health-pill.*?      @media\(min-width:821px\) and \(max-height:820px\)\{\.vpn-current-panel \.best-v4-pill b,\.vpn-current-panel \.best-v4-pill b\.fn-long-value\{font-size:13px!important;letter-spacing:-\.03em\}\}\n'''
replacement = '''      .top-actions{gap:10px!important}.topbar.overview-approved{overflow:visible!important;gap:14px!important}.topbar.overview-approved .top-status{display:none!important}.overview-approved-top{margin-left:0!important;gap:18px!important}
      #bestServerAdvanced.fn-topbar-vpn-picker{position:relative;display:block!important;flex:1 1 560px;max-width:620px;min-width:390px;margin:0 0 0 auto!important;padding:0!important;border:0!important;background:transparent!important;min-height:0!important;z-index:60}#bestServerAdvanced.fn-topbar-vpn-picker:before,#bestServerAdvanced.fn-topbar-vpn-picker>h3{display:none!important}
      #bestServerAdvanced.fn-topbar-vpn-picker #profilesList.profiles{display:grid!important;grid-template-columns:minmax(170px,.82fr) minmax(220px,1.18fr);gap:8px;align-items:center;width:100%;margin:0!important;min-height:0!important}#bestServerAdvanced.fn-topbar-vpn-picker #profilesList .field{position:relative;padding:0;border:0;background:none;min-height:0!important}#bestServerAdvanced.fn-topbar-vpn-picker #profilesList .field label{display:none!important}#bestServerAdvanced.fn-topbar-vpn-picker #profilesList .profile-combobox{margin:0!important;min-height:0!important;position:relative}#bestServerAdvanced.fn-topbar-vpn-picker #profileSearch,#bestServerAdvanced.fn-topbar-vpn-picker #profilesTrigger{min-height:36px!important;height:36px!important;background:#0b1929;border:1px solid #315276;border-radius:9px;font-size:11px;color:#dce8f7}#bestServerAdvanced.fn-topbar-vpn-picker #profileSearch{padding-left:38px}#bestServerAdvanced.fn-topbar-vpn-picker .manual-search-icon{left:11px;width:16px;height:16px}#bestServerAdvanced.fn-topbar-vpn-picker #profilesMenu{z-index:120}#bestServerAdvanced.fn-topbar-vpn-picker #profilesError{grid-column:1/-1;margin:0;position:absolute;top:42px;left:0;right:0;z-index:122}#bestServerAdvanced.fn-topbar-vpn-picker #selectedProfileCard{position:absolute;top:43px;left:0;right:222px;z-index:121;margin:0;padding:8px 10px;border:1px solid #315276;border-radius:10px;background:#0c1a2b;box-shadow:0 12px 30px rgba(0,0,0,.38);font-size:10px}#bestServerAdvanced.fn-topbar-vpn-picker:has(#exactConnectRow[hidden]) #selectedProfileCard{display:none!important}#bestServerAdvanced.fn-topbar-vpn-picker #exactConnectRow{position:absolute;top:43px;right:0;z-index:122;width:214px;display:grid;grid-template-columns:1fr 1fr;gap:6px;margin:0;padding:7px;border:1px solid #315276;border-radius:10px;background:#0c1a2b;box-shadow:0 12px 30px rgba(0,0,0,.38)}#bestServerAdvanced.fn-topbar-vpn-picker #exactConnectRow[hidden]{display:none!important}#bestServerAdvanced.fn-topbar-vpn-picker #exactConnectRow .btn{min-height:34px;padding:5px 7px;font-size:9px}#bestServerAdvanced.fn-topbar-vpn-picker #quickNetworkGuard{display:none!important}
      @media(max-width:1180px){.vpn-current-panel .best-v4-pill b{font-size:16.5px!important}.vpn-current-panel .best-v4-pill b.fn-long-value{font-size:15px!important}#bestServerAdvanced.fn-topbar-vpn-picker{min-width:320px;max-width:520px}.overview-approved-top{gap:12px!important}.overview-approved-fact span{font-size:9px!important}.overview-approved-fact strong{font-size:11px!important}}
      @media(max-width:900px){.overview-approved-top{display:none!important}#bestServerAdvanced.fn-topbar-vpn-picker{max-width:none;min-width:300px}}
      @media(max-width:820px){.vpn-current-panel .best-v4-pill b,.vpn-current-panel .best-v4-pill b.fn-long-value{font-size:15px!important;letter-spacing:-.035em}.topbar.overview-approved{height:auto!important;min-height:64px;flex-wrap:wrap;padding-bottom:8px!important}#bestServerAdvanced.fn-topbar-vpn-picker{order:20;flex:1 0 100%;max-width:none;min-width:0;margin:0!important}#bestServerAdvanced.fn-topbar-vpn-picker #profilesList.profiles{grid-template-columns:1fr 1.15fr}}
      @media(max-width:560px){#bestServerAdvanced.fn-topbar-vpn-picker #profilesList.profiles{grid-template-columns:1fr}#bestServerAdvanced.fn-topbar-vpn-picker #selectedProfileCard,#bestServerAdvanced.fn-topbar-vpn-picker #exactConnectRow{position:static;width:auto;grid-column:1/-1;margin-top:6px}#bestServerAdvanced.fn-topbar-vpn-picker #profilesError{position:static;grid-column:1/-1}}
      @media(min-width:821px) and (max-height:820px){.vpn-current-panel .best-v4-pill b,.vpn-current-panel .best-v4-pill b.fn-long-value{font-size:13px!important;letter-spacing:-.03em}}
'''
sub_once(js, pattern, lambda _: replacement, re.S)

# User-facing selector wording.
replace_once(
    "freenet-ui/web/index.html",
    "extraProfiles.length?'Выбрать конкретный Extra-профиль':'Профили не загружены'",
    "extraProfiles.length?'Выбрать VPN':'Профили не загружены'",
)

# Contracts.
Path("freenet-ui/overview_runtime_polish_test.go").write_text(r'''package main

import (
	"os"
	"strings"
	"testing"
)

func TestOverviewRuntimePolishContract(t *testing.T) {
	data, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	mustContain := []string{
		"FreeNetFinalRuntimePolish",
		"fn-best-alternative",
		"Лучший из вариантов",
		"fn-clean-flag",
		"flagSVG",
		"fn-long-value",
		"fn-topbar-vpn-picker",
		"topbar.insertBefore(manual, topSummary || topActions || null)",
		".topbar.overview-approved .top-status{display:none!important}",
		"vpn-current-panel .best-v4-pill{grid-template-columns:20px minmax(0,1fr)!important",
		".flag-icon.fn-clean-flag:before,.flag-icon.fn-clean-flag:after{content:none!important",
		"hr: '<rect width=\"30\" height=\"6.667\" fill=\"#ff0000\"",
		"sk: '<rect width=\"30\" height=\"6.667\" fill=\"#fff\"",
		"ru: '<rect width=\"30\" height=\"6.667\" fill=\"#fff\"",
		"dk: '<rect width=\"30\" height=\"20\" fill=\"#c8102e\"",
		"no: '<rect width=\"30\" height=\"20\" fill=\"#ba0c2f\"",
	}
	for _, needle := range mustContain {
		if !strings.Contains(s, needle) {
			t.Fatalf("runtime Overview polish contract missing %q", needle)
		}
	}
	for _, forbidden := range []string{"Всё работает", "Сервер вручную", "fn-health-pill", "fn-manual-shortcut"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("obsolete Overview topbar UI still present: %q", forbidden)
		}
	}
	index, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), "extraProfiles.length?'Выбрать VPN':'Профили не загружены'") {
		t.Fatal("manual VPN selector must use user-facing label 'Выбрать VPN'")
	}
}
''')

Path("freenet-ui/best_server_full_current_test.go").write_text(r'''package main

import (
	"os"
	"strings"
	"testing"
)

func TestFullBestServerScanOwnsCurrentBaseline(t *testing.T) {
	data, err := os.ReadFile("best_server_ux.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{
		"currentBaseline, currentBaselineOK := loadBestServerCurrentQuality",
		"baselineResponse := a.scanActiveCurrentVPNQuality",
		"response.Candidates = append(response.Candidates, currentBaseline)",
		"currentIndex := bestServerCurrentCandidateIndex(candidates, currentEndpoint, currentFilter)",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("full Best Server current-baseline contract missing %q", want)
		}
	}
	if strings.Contains(s, "if cachedOK {\n\t\tif currentIndex := bestServerCurrentCandidateIndex") {
		t.Fatal("current removal must not depend on a pre-existing cache")
	}
}
''')

# Remove temporary scaffolding from the final product branch.
Path(".github/workflows/patch-342.yml").unlink(missing_ok=True)
Path(".github/patch342.py").unlink(missing_ok=True)
