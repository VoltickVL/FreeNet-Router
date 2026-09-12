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

func TestSettingsV2RenderUsesApprovedNavigationAndDNSNames(t *testing.T) {
	data, err := automationWebFS.ReadFile("web/automation.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, required := range []string{
		"Настройки",
		"Маршрутизация",
		"Система",
		"Интернет и DNS",
		"DNS через роутер",
		"Раздельный DNS",
		"AUTO VPN",
		"Только endpoint",
		"Лучший VPN автоматически",
		"Текущий VPN под наблюдением",
		"Журнал автоматических операций",
		"Дополнительные автоматизации",
		"GeoData / GeoIP",
		"grid-template-columns:minmax(0,1.08fr) minmax(0,1fr)",
		".side-bottom{display:none!important}",
		"var(--line)",
		"var(--text)",
	} {
		if !strings.Contains(s, required) {
			t.Fatalf("Settings v2 render missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"XKeen/Xray DNS",
		"Автоматически — рекомендуется",
		"data-page=\"access\"",
		"publicKey",
		"subscription_url",
	} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("Settings v2 contains forbidden user/security surface %q", forbidden)
		}
	}
	if strings.Contains(strings.ToLower(s), "font-family") {
		t.Fatal("Settings v2 must inherit the existing FreeNet font stack")
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
