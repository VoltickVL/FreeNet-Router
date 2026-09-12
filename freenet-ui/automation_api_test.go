package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
	if got := automationConfigValue(path, "AUTO_VPN_V1", ""); got != "" {
		t.Fatalf("missing AUTO_VPN_V1 must stay absent, got %q", got)
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

func TestAutomationRenderUsesSharedDesignTokensAndSafetyContract(t *testing.T) {
	data, err := automationWebFS.ReadFile("web/automation.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, required := range []string{
		"AUTO VPN v1",
		"Смена страны отключена",
		"Текущий профиль под наблюдением",
		"Журнал автоматических операций",
		"Дополнительные автоматизации",
		"GeoData / GeoIP",
		"Правила безопасности",
		"var(--accent2)",
		"var(--line2)",
		"var(--ok)",
		"class=\"card automation-card\"",
		"При неоднозначности ничего не менять",
	} {
		if !strings.Contains(s, required) {
			t.Fatalf("automation render missing %q", required)
		}
	}
	if strings.Contains(strings.ToLower(s), "font-family") {
		t.Fatal("automation page must inherit the existing FreeNet font stack")
	}
	for _, forbidden := range []string{"publicKey", "shortId", "subscription_url"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("automation UI contains forbidden secret surface marker %q", forbidden)
		}
	}
}

func TestAutomationStateParserOnlyAcceptsSafeKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state")
	content := "LAST_RUN=2026-09-12T00:00:00Z\nLAST_RESULT=same\nLAST_REASON=current endpoint actual\nROLLBACK_READY=no\nSUBSCRIPTION_URL=https://secret.example\nUUID=secret\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	state := parseAutomationState(path)
	if len(state) != 4 {
		t.Fatalf("safe state keys=%v", state)
	}
	if _, ok := state["SUBSCRIPTION_URL"]; ok {
		t.Fatal("secret-bearing keys must not enter automation API state")
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
