package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutomationHealthRequiresConfirmedWANAndTwoVPNFailures(t *testing.T) {
	failed := automationHealthProbe{State: automationHealthFailed, Reason: "failed"}
	healthy := automationHealthProbe{State: automationHealthHealthy, Reason: "healthy"}
	uncertain := automationHealthProbe{State: automationHealthUncertain, Reason: "uncertain"}

	if got := classifyAutomationHealth(healthy, true, failed); got.State != automationHealthHealthy {
		t.Fatalf("healthy first probe state=%q want healthy", got.State)
	}
	if got := classifyAutomationHealth(failed, false, failed); got.State != automationHealthUncertain {
		t.Fatalf("WAN ambiguity state=%q want uncertain", got.State)
	}
	if got := classifyAutomationHealth(failed, true, healthy); got.State != automationHealthHealthy {
		t.Fatalf("recovered second probe state=%q want healthy", got.State)
	}
	if got := classifyAutomationHealth(failed, true, uncertain); got.State != automationHealthUncertain {
		t.Fatalf("uncertain second probe state=%q want uncertain", got.State)
	}
	if got := classifyAutomationHealth(failed, true, failed); got.State != automationHealthCritical {
		t.Fatalf("confirmed double failure state=%q want critical", got.State)
	}
}

func TestManagedCronSeparatesHealthWatchdogFromHeavyBestScan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "freenet.conf")
	if err := os.WriteFile(path, []byte("AUTO_XKEEN_GEODATA=no\nAUTO_VPN_FAILOVER=yes\nAUTO_VPN_FAILOVER_CRON='*/5 * * * *'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	settings := automationSettings{Enabled: true, Interval: "1h", Mode: automationModeBest, Policy: automationPolicyBetter, CountryScope: automationCountryRegion, AutoApply: true}
	got, err := buildManagedAutomationCron(path, settings, []byte("*/5 * * * * /opt/bin/vpn failover\n"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.Contains(text, "*/5 * * * * "+automationRunnerPath()+" automation-health-watch") {
		t.Fatalf("health watchdog is not scheduled every 5 minutes:\n%s", text)
	}
	if !strings.Contains(text, "0 * * * * "+automationRunnerPath()+" automation-best-run") {
		t.Fatalf("heavy Best run does not keep the selected 1h interval:\n%s", text)
	}
	if strings.Contains(text, "/opt/bin/vpn failover") {
		t.Fatalf("legacy failover scheduler must be removed to avoid duplicate mutation:\n%s", text)
	}
}

func TestManagedCronDoesNotAutoRunWhenAutomationIsManual(t *testing.T) {
	path := filepath.Join(t.TempDir(), "freenet.conf")
	if err := os.WriteFile(path, []byte("AUTO_XKEEN_GEODATA=no\nAUTO_VPN_FAILOVER=no\n"), 0600); err != nil {
		t.Fatal(err)
	}
	settings := automationSettings{Enabled: true, Interval: "manual", Mode: automationModeBest, Policy: automationPolicyDegraded, CountryScope: automationCountryRegion, AutoApply: true}
	got, err := buildManagedAutomationCron(path, settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if strings.Contains(text, "automation-health-watch") || strings.Contains(text, "automation-best-run") {
		t.Fatalf("manual mode must not schedule automatic checks:\n%s", text)
	}
}

func TestEmergencyBestPathBypassesOnlyOptimizationCooldown(t *testing.T) {
	healthSource, err := os.ReadFile("automation_health.go")
	if err != nil {
		t.Fatal(err)
	}
	healthText := string(healthSource)
	start := strings.Index(healthText, "func (a *app) runAutomationBestEmergencyCycle")
	end := strings.Index(healthText, "func (a *app) runAutomationEndpointEmergency")
	if start < 0 || end <= start {
		t.Fatal("emergency Best cycle contract is missing")
	}
	if strings.Contains(healthText[start:end], "automationCooldownActive(") {
		t.Fatal("critical emergency path must not be blocked by optimization cooldown")
	}

	normalSource, err := os.ReadFile("automation_v2.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(normalSource), "if automationCooldownActive(parseAutomationLastSwitch(automationStatePath()), time.Now().UTC())") {
		t.Fatal("normal Best optimization path must retain the 6-hour anti-flapping cooldown")
	}
}

func TestEndpointEmergencyStaysOnEndpointOnlyHelper(t *testing.T) {
	data, err := os.ReadFile("automation_health.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "func (a *app) runAutomationEndpointEmergency")
	end := strings.Index(text, "func (a *app) runAutomationHealthWatch")
	if start < 0 || end <= start {
		t.Fatal("endpoint emergency contract is missing")
	}
	segment := text[start:end]
	if !strings.Contains(segment, "ensureAutomationHelper()") || !strings.Contains(segment, "helper, \"run\"") {
		t.Fatal("endpoint emergency must reuse the canonical endpoint-only helper")
	}
	if strings.Contains(segment, "executeProviderProfileApply") || strings.Contains(segment, "runAutomationBestEmergencyCycle") {
		t.Fatal("endpoint-only emergency must not change country/profile")
	}
}
