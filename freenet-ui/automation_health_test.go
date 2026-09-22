package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutomationServicePathHealthRequiresAllBoundedTargets(t *testing.T) {
	if !automationServicePathHealthy(4, 4) {
		t.Fatal("4/4 service paths must be healthy")
	}
	for _, tc := range []struct{ ok, total int }{{3, 4}, {2, 4}, {1, 1}, {0, 4}} {
		if automationServicePathHealthy(tc.ok, tc.total) {
			t.Fatalf("partial service path %d/%d must not be healthy", tc.ok, tc.total)
		}
	}
}

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

func TestAutomationRecoveryStageJournalUsesStageResults(t *testing.T) {
	history := filepath.Join(t.TempDir(), "freenet-automation.history")
	t.Setenv("FREENET_AUTOMATION_HISTORY", history)

	appendAutomationRecoveryStage("endpoint_refresh", "failed", "endpoint refresh failed; rollback=unknown")
	appendAutomationRecoveryStage("candidate_selection", "start", "Endpoint refresh did not recover VPN; scanning checked candidates.")
	events := readAutomationEvents(history, 4)
	if len(events) != 2 {
		t.Fatalf("events=%d want 2", len(events))
	}
	if events[0].Kind != "AUTO VPN" || events[0].Result != "candidate_selection:start" {
		t.Fatalf("latest stage event not rendered as stage result: %+v", events[0])
	}
	if events[1].Kind != "AUTO VPN" || events[1].Result != "endpoint_refresh:failed" {
		t.Fatalf("endpoint stage event not rendered as stage result: %+v", events[1])
	}
	text, err := os.ReadFile(history)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"vless://", "publicKey=", "private-token", "TEST-UUID"} {
		if strings.Contains(string(text), forbidden) {
			t.Fatalf("stage journal leaked forbidden token %q in %s", forbidden, string(text))
		}
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

func TestEndpointEmergencyUsesCanonicalManualRefreshAndPostProbe(t *testing.T) {
	data, err := os.ReadFile("automation_health.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "func (a *app) runAutomationEndpointEmergency")
	end := strings.Index(text, "func recordAndReturnHealth")
	if start < 0 || end <= start {
		t.Fatal("endpoint emergency contract is missing")
	}
	segment := text[start:end]
	if !strings.Contains(segment, "automationEndpointUpdateCommand") || !strings.Contains(segment, "automationEndpointPostProbe") {
		t.Fatal("endpoint emergency must use the canonical vpn update path and verify Internet through the refreshed VPN")
	}
	if strings.Contains(segment, "ensureAutomationHelper()") || strings.Contains(segment, "helper, \"run\"") {
		t.Fatal("health recovery must not keep a second legacy endpoint-refresh engine")
	}
	if strings.Contains(segment, "executeProviderProfileApply") || strings.Contains(segment, "runAutomationBestEmergencyCycle") {
		t.Fatal("endpoint refresh itself must not change country/profile")
	}
}

func TestEndpointEmergencyRequiresHealthyPostProbe(t *testing.T) {
	oldCommand, oldProbe := automationEndpointUpdateCommand, automationEndpointPostProbe
	t.Cleanup(func() {
		automationEndpointUpdateCommand = oldCommand
		automationEndpointPostProbe = oldProbe
	})
	outPath := filepath.Join(t.TempDir(), "04_outbounds.json")
	if err := os.WriteFile(outPath, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: config{VPNPath: "/opt/bin/vpn", OutPath: outPath}}
	settings := automationSettings{Mode: automationModeEndpoint, AutoApply: true}

	automationEndpointUpdateCommand = func(_ *app, _ context.Context) ([]byte, error) {
		if err := os.WriteFile(outPath, []byte("after"), 0600); err != nil {
			return nil, err
		}
		return []byte("updated"), nil
	}
	automationEndpointPostProbe = func(_ *app, _ context.Context) automationHealthProbe {
		return automationHealthProbe{State: automationHealthFailed, Reason: "still failed"}
	}
	result, err := a.runAutomationEndpointEmergency(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != automationHealthFailed || !result.Mutated {
		t.Fatalf("post-refresh failed probe result=%+v want failed + mutated", result)
	}

	automationEndpointPostProbe = func(_ *app, _ context.Context) automationHealthProbe {
		return automationHealthProbe{State: automationHealthHealthy, Reason: "healthy"}
	}
	result, err = a.runAutomationEndpointEmergency(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != automationHealthHealthy {
		t.Fatalf("healthy post-refresh probe result=%+v want healthy", result)
	}
}

func TestEndpointEmergencyBusyFailsClosed(t *testing.T) {
	oldCommand, oldProbe := automationEndpointUpdateCommand, automationEndpointPostProbe
	t.Cleanup(func() {
		automationEndpointUpdateCommand = oldCommand
		automationEndpointPostProbe = oldProbe
	})
	probeCalled := false
	automationEndpointUpdateCommand = func(_ *app, _ context.Context) ([]byte, error) {
		return []byte("[blanc-xkeen] ERROR: another updater instance is already running"), errors.New("exit status 1")
	}
	automationEndpointPostProbe = func(_ *app, _ context.Context) automationHealthProbe {
		probeCalled = true
		return automationHealthProbe{State: automationHealthHealthy}
	}
	a := &app{cfg: config{VPNPath: "/opt/bin/vpn", OutPath: filepath.Join(t.TempDir(), "missing")}}
	result, err := a.runAutomationEndpointEmergency(context.Background(), automationSettings{Mode: automationModeEndpoint, AutoApply: true})
	if !errors.Is(err, errAutomationBusy) {
		t.Fatalf("busy error=%v want errAutomationBusy", err)
	}
	if result.State != automationHealthUncertain || probeCalled {
		t.Fatalf("busy recovery result=%+v probeCalled=%v; must fail closed before a second mutation/probe", result, probeCalled)
	}
}
