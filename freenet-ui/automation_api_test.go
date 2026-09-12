package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAutomationIntervalContract(t *testing.T) {
	cases := map[string]string{
		"30m": "*/30 * * * *",
		"1h":  "0 * * * *",
		"3h":  "0 */3 * * *",
		"6h":  "0 */6 * * *",
		"manual": "",
	}
	for interval, want := range cases {
		got, ok := automationCron(interval)
		if !ok || got != want {
			t.Fatalf("automationCron(%q)=(%q,%v), want (%q,true)", interval, got, ok, want)
		}
	}
	if _, ok := automationCron("15m"); ok {
		t.Fatal("unsupported interval must fail closed")
	}
}

func TestAutomationConfigLegacyMigrationIsFailClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "freenet.conf")
	if err := os.WriteFile(path, []byte("AUTO_ENDPOINT_UPDATE=yes\nAUTO_ENDPOINT_CRON='0 * * * *'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	legacy := automationConfigValue(path, "AUTO_ENDPOINT_UPDATE", "no") == "yes"
	if !legacy {
		t.Fatal("legacy enabled state must remain detectable for migration warning")
	}
	if got := automationIntervalFromCron(automationConfigValue(path, "AUTO_ENDPOINT_CRON", "")); got != "1h" {
		t.Fatalf("legacy cron interval=%q want 1h", got)
	}
	settings := readAutomationSettings(path)
	if settings.Mode != automationModeEndpoint || settings.Policy != automationPolicyDegraded || settings.CountryScope != automationCountryRegion {
		t.Fatalf("safe Settings v2 defaults=%+v", settings)
	}
}

func TestAutomationNextRunIsDerivedFromFact(t *testing.T) {
	last := "2026-09-12T00:00:00Z"
	if got := automationNextRun(last, "3h"); got != "2026-09-12T03:00:00Z" {
		t.Fatalf("next run=%q", got)
	}
	if got := automationNextRun(last, "manual"); got != "" {
		t.Fatalf("manual next run must be empty, got %q", got)
	}
	if got := automationNextRun("not-a-date", "1h"); got != "" {
		t.Fatalf("invalid last run must fail closed, got %q", got)
	}
}

func TestLegacySettingsV2RendererIsRetired(t *testing.T) {
	bootstrapData, err := automationWebFS.ReadFile("web/automation.js")
	if err != nil {
		t.Fatal(err)
	}
	settingsData, err := automationWebFS.ReadFile("web/settings-v3.js")
	if err != nil {
		t.Fatal(err)
	}
	asyncData, err := automationWebFS.ReadFile("web/automation-async.js")
	if err != nil {
		t.Fatal(err)
	}
	bootstrap := string(bootstrapData)
	settings := string(settingsData)
	async := string(asyncData)

	for _, required := range []string{
		"__freenetAcceptedSettingsBootstrapLoaded",
		"setNav(routing, 'routing', 'Маршрутизация'",
		"ensureJournal(nav)",
		"fn-routing-source-hidden",
		"delete pageLabels.system",
		"freenet:controls-busy",
		"#fn3Save",
	} {
		if !strings.Contains(bootstrap, required) {
			t.Fatalf("canonical shell bootstrap missing %q", required)
		}
	}
	for _, required := range []string{
		"ensureSettingsPage()",
		"page.dataset.pageView = 'settings'",
		"ensureSettingsNav()",
		"pageLabels.settings = 'Настройки'",
		"window.setPage('settings')",
		"mountSettings();",
	} {
		if !strings.Contains(settings, required) {
			t.Fatalf("Settings v3 canonical lifecycle missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"settingsPage.dataset.pageView = 'settings'",
		"q('[data-page-view=\"settings\"]') || q('[data-page-view=\"automation\"]')",
		"window.dispatchEvent(new Event('hashchange'))",
	} {
		if strings.Contains(bootstrap, forbidden) || strings.Contains(async, forbidden) {
			t.Fatalf("legacy Settings lifecycle bridge survived: %q", forbidden)
		}
	}
	for _, forbidden := range []string{
		"Интернет и DNS",
		"Только endpoint",
		"Лучший VPN автоматически",
		"hysteresis",
		"cooldown",
		"settingsMarkup()",
		"fnSaveSettings",
		"fnCheckNow",
	} {
		if strings.Contains(bootstrap, forbidden) {
			t.Fatalf("legacy Settings v2 renderer survived in bootstrap: %q", forbidden)
		}
	}
}

func TestAutomationStateParserOnlyAcceptsSafeKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state")
	content := "LAST_RUN=2026-09-12T00:00:00Z\nLAST_RESULT=same\nLAST_REASON=current endpoint actual\nROLLBACK_READY=no\nLAST_SWITCH=2026-09-11T20:00:00Z\nSUBSCRIPTION_URL=https://secret.example\nUUID=secret\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	state := parseAutomationState(path)
	if len(state) != 5 || state["LAST_SWITCH"] == "" {
		t.Fatalf("safe state keys=%v", state)
	}
	if _, ok := state["SUBSCRIPTION_URL"]; ok {
		t.Fatal("secret-bearing keys must not enter automation API state")
	}
}

func TestAutomationCountriesAreNormalizedAndRussiaExcluded(t *testing.T) {
	got := normalizeAutomationCountries([]string{"DE", "pl, nl", "ru", "de", "bad"})
	want := []string{"de", "nl", "pl"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("countries=%v want=%v", got, want)
	}
}

func TestAutomationCountryScopeIsFailClosed(t *testing.T) {
	settings := automationSettings{CountryScope: automationCountryCurrent}
	if !automationCountryAllowed(settings, "pl", "pl") || automationCountryAllowed(settings, "pl", "de") {
		t.Fatal("current-country scope changed country")
	}
	settings.CountryScope = automationCountryRegion
	if !automationCountryAllowed(settings, "pl", "de") || automationCountryAllowed(settings, "pl", "us") {
		t.Fatal("region scope did not stay in current region")
	}
	settings.CountryScope = automationCountryAllowlist
	settings.Countries = []string{"nl", "de"}
	if !automationCountryAllowed(settings, "pl", "nl") || automationCountryAllowed(settings, "pl", "fr") || automationCountryAllowed(settings, "pl", "ru") {
		t.Fatal("allow-list scope was not exact/fail-closed")
	}
}

func TestAutomationMeaningfulImprovementUsesHysteresis(t *testing.T) {
	current := bestServerQualityCandidate{Available: true, Eligible: true, DownloadMbps: 100, ApplicationMS: 180, JitterMS: 10}
	better := bestServerQualityCandidate{Available: true, Eligible: true, DownloadMbps: 130, ApplicationMS: 150, JitterMS: 8}
	noise := bestServerQualityCandidate{Available: true, Eligible: true, DownloadMbps: 103, ApplicationMS: 176, JitterMS: 10}
	partial := bestServerQualityCandidate{Available: true, Eligible: true, DownloadMbps: 150}
	if !automationMeaningfullyBetter(current, better) {
		t.Fatal("clear improvement must pass hysteresis")
	}
	if automationMeaningfullyBetter(current, noise) {
		t.Fatal("measurement noise must not trigger auto-switch")
	}
	if automationMeaningfullyBetter(current, partial) {
		t.Fatal("incomplete metrics must fail closed")
	}
}

func TestAutomationCurrentQualityIncompleteIsUncertain(t *testing.T) {
	candidate := bestServerQualityCandidate{Tested: true, Available: false, Eligible: false}
	_, state := automationCurrentQualityState(bestServerQualityResponse{Candidates: []bestServerQualityCandidate{candidate}})
	if state != "uncertain" {
		t.Fatalf("incomplete current quality state=%q want uncertain", state)
	}
	candidate.Available = true
	_, state = automationCurrentQualityState(bestServerQualityResponse{Candidates: []bestServerQualityCandidate{candidate}})
	if state != "degraded" {
		t.Fatalf("fully measured rejected current quality state=%q want degraded", state)
	}
}

func TestAutomationCooldownPreventsFlapping(t *testing.T) {
	now := time.Date(2026, 9, 12, 3, 0, 0, 0, time.UTC)
	if !automationCooldownActive(now.Add(-2*time.Hour), now) {
		t.Fatal("recent switch must activate cooldown")
	}
	if automationCooldownActive(now.Add(-7*time.Hour), now) {
		t.Fatal("expired cooldown must not block a switch")
	}
}

func TestBestAutomationCandidateIsEligibleOnly(t *testing.T) {
	response := bestServerQualityResponse{Candidates: []bestServerQualityCandidate{
		{ID: "0123456789abcdef", Name: "diagnostic", Available: true, Eligible: false},
		{ID: "fedcba9876543210", Name: "eligible", Available: true, Eligible: true},
	}}
	got, ok := bestAutomationCandidate(response)
	if !ok || got.ID != "fedcba9876543210" {
		t.Fatalf("candidate=%+v ok=%v; diagnostic must never auto-apply", got, ok)
	}
}

func TestManagedCronSelectsOneAutoVPNEngine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "freenet.conf")
	if err := os.WriteFile(path, []byte("AUTO_XKEEN_GEODATA=no\nAUTO_VPN_FAILOVER=no\n"), 0600); err != nil {
		t.Fatal(err)
	}
	settings := automationSettings{Enabled: true, Interval: "1h", Mode: automationModeBest, Policy: automationPolicyDegraded, CountryScope: automationCountryRegion, AutoApply: true}
	got, err := buildManagedAutomationCron(path, settings, []byte("0 * * * * /opt/lib/freenet/auto_vpn.sh run\n"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.Contains(text, "freenet-ui automation-best-run") || strings.Contains(text, "/opt/lib/freenet/auto_vpn.sh run") {
		t.Fatalf("best mode cron must select exactly one engine:\n%s", text)
	}
}

func TestAutoVPNShellContract(t *testing.T) {
	cmd := exec.Command("sh", "../tests/test_auto_vpn.sh")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("AUTO VPN shell contract failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "AUTO VPN v1 contract: PASS") {
		t.Fatalf("unexpected AUTO VPN contract output: %s", out)
	}
}
