package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAutomationHealthLockReclaimsDeadPIDImmediately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auto-health.lock")
	t.Setenv("FREENET_AUTO_HEALTH_LOCK", path)
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "pid"), []byte("1073741824\n"), 0600); err != nil {
		t.Fatal(err)
	}
	release, err := acquireAutomationHealthLock()
	if err != nil {
		t.Fatalf("dead PID fence was not reclaimed: %v", err)
	}
	release()
}

func TestAutomationHealthLockNeverReclaimsLiveOldPID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auto-health.lock")
	t.Setenv("FREENET_AUTO_HEALTH_LOCK", path)
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "pid"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-20 * time.Minute)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if release, err := acquireAutomationHealthLock(); !errors.Is(err, errAutomationBusy) {
		if release != nil {
			release()
		}
		t.Fatalf("live old fence must remain protected, err=%v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("live fence was removed: %v", err)
	}
}

func TestAutomationApplicationLatencyGate(t *testing.T) {
	if !automationApplicationPathHealthy(bestServerQualityMaxApplicationMS) {
		t.Fatal("application RTT at the safety ceiling must remain healthy")
	}
	if automationApplicationPathHealthy(bestServerQualityMaxApplicationMS + 1) {
		t.Fatal("application RTT above the safety ceiling must trigger recovery")
	}
	if automationApplicationPathHealthy(0) {
		t.Fatal("missing application RTT must not be treated as healthy")
	}
}

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

func TestAutomationReachableDegradationNeverTriggersRecovery(t *testing.T) {
	if got := classifyAutomationReachableQuality(bestServerQualityMaxApplicationMS+80, 4, 4); got.State != automationHealthUncertain {
		t.Fatalf("high-latency reachable VPN state=%q want uncertain/no-mutation", got.State)
	}
	if got := classifyAutomationReachableQuality(bestServerQualityMaxApplicationMS, 3, 4); got.State != automationHealthUncertain {
		t.Fatalf("partial service degradation state=%q want uncertain/no-mutation", got.State)
	}
	if got := classifyAutomationReachableQuality(bestServerQualityMaxApplicationMS, 4, 4); got.State != automationHealthHealthy {
		t.Fatalf("fully healthy VPN state=%q want healthy", got.State)
	}
}

func TestAutomationSingleOriginFailureCannotTriggerRecovery(t *testing.T) {
	if got := classifyAutomationApplicationFailure(true); got.State != automationHealthUncertain {
		t.Fatalf("working VPN transport with named-origin failure state=%q want uncertain/no-mutation", got.State)
	}
	if got := classifyAutomationApplicationFailure(false); got.State != automationHealthFailed {
		t.Fatalf("multi-origin plus transport failure state=%q want failed", got.State)
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
	if err := os.WriteFile(path, []byte("AUTO_XKEEN_GEODATA=no\nAUTO_VPN_HEALTH_INTERVAL=1m\nAUTO_VPN_FAILOVER=yes\nAUTO_VPN_FAILOVER_CRON='*/5 * * * *'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	settings := automationSettings{Enabled: true, Interval: "1h", Mode: automationModeBest, Policy: automationPolicyBetter, CountryScope: automationCountryRegion, AutoApply: true}
	got, err := buildManagedAutomationCron(path, settings, []byte("*/5 * * * * /opt/bin/vpn failover\n"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.Contains(text, "* * * * * "+automationRunnerPath()+" automation-health-watch") {
		t.Fatalf("health watchdog is not scheduled with the configured 1-minute fallback:\n%s", text)
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

func TestEndpointEmergencyUsesCanonicalCurrentProfileRefresh(t *testing.T) {
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
	if !strings.Contains(segment, "automationEndpointCurrentRefresh") {
		t.Fatal("endpoint emergency must use canonical executeBestServerCurrentRefresh path")
	}
	for _, legacy := range []string{"automationEndpointUpdateCommand", "automationEndpointPostProbe", "runCommand(ctx, a.cfg.VPNPath", "ensureAutomationHelper()", "helper, \"run\""} {
		if strings.Contains(segment, legacy) {
			t.Fatalf("health recovery still contains legacy/duplicate endpoint engine %q", legacy)
		}
	}
	if strings.Contains(segment, "executeProviderProfileApply") || strings.Contains(segment, "runAutomationBestEmergencyCycle") {
		t.Fatal("current-profile refresh itself must not change logical VPN")
	}
}

func TestEndpointEmergencyInterpretsCanonicalRefreshResult(t *testing.T) {
	oldRefresh := automationEndpointCurrentRefresh
	t.Cleanup(func() { automationEndpointCurrentRefresh = oldRefresh })
	a := &app{}
	settings := automationSettings{Mode: automationModeEndpoint, AutoApply: true}

	automationEndpointCurrentRefresh = func(_ *app, _ context.Context) (int, bestServerRefreshResponse) {
		return 200, bestServerRefreshResponse{
			Success: true, Applied: true, Outcome: "applied", Mutation: "APPLIED", RollbackState: "NOT_NEEDED",
			Message: "fresh current profile applied and post-checked",
		}
	}
	result, err := a.runAutomationEndpointEmergency(context.Background(), settings)
	if err != nil || result.State != automationHealthHealthy || !result.Mutated {
		t.Fatalf("applied canonical refresh result=%+v err=%v want healthy+mutated", result, err)
	}

	automationEndpointCurrentRefresh = func(_ *app, _ context.Context) (int, bestServerRefreshResponse) {
		return 200, bestServerRefreshResponse{
			Success: true, Applied: false, Outcome: "check_failed", Mutation: "NONE", RollbackState: "NOT_NEEDED",
			Message: "fresh current profile did not pass readiness",
		}
	}
	result, err = a.runAutomationEndpointEmergency(context.Background(), settings)
	if err != nil || result.State != automationHealthFailed || result.Mutated {
		t.Fatalf("safe non-applied refresh result=%+v err=%v want failed/no-mutation so Best fallback may continue", result, err)
	}
}

func TestEndpointEmergencyConflictAndRollbackUnknownFailClosed(t *testing.T) {
	oldRefresh := automationEndpointCurrentRefresh
	t.Cleanup(func() { automationEndpointCurrentRefresh = oldRefresh })
	a := &app{}
	settings := automationSettings{Mode: automationModeEndpoint, AutoApply: true}

	automationEndpointCurrentRefresh = func(_ *app, _ context.Context) (int, bestServerRefreshResponse) {
		return 409, bestServerRefreshResponse{Success: false, Mutation: "NONE", RollbackState: "NOT_APPLIED", Error: "current VPN changed"}
	}
	result, err := a.runAutomationEndpointEmergency(context.Background(), settings)
	if !errors.Is(err, errAutomationBusy) || result.State != automationHealthUncertain {
		t.Fatalf("conflict result=%+v err=%v want uncertain/busy", result, err)
	}

	automationEndpointCurrentRefresh = func(_ *app, _ context.Context) (int, bestServerRefreshResponse) {
		return 502, bestServerRefreshResponse{Success: false, Mutation: "ROLLED_BACK", RollbackState: "FAILED/UNKNOWN", Error: "rollback unknown"}
	}
	result, err = a.runAutomationEndpointEmergency(context.Background(), settings)
	if err == nil || result.State != automationHealthCritical {
		t.Fatalf("unknown rollback result=%+v err=%v want critical STOP", result, err)
	}
}
