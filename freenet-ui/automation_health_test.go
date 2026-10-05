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

func TestAutomationHealthDueUsesProbeStartCadence(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "freenet.conf")
	statePath := filepath.Join(dir, "settings.state")
	t.Setenv("FREENET_SETTINGS_V3_STATE", statePath)
	if err := os.WriteFile(configPath, []byte("AUTO_VPN_HEALTH_INTERVAL=1m\n"), 0600); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 3, 6, 30, 0, 0, time.UTC)
	if err := os.WriteFile(statePath, []byte("HEALTH_SCHEDULE_LAST="+base.Format(time.RFC3339)+"\nHEALTH_LAST="+base.Add(8*time.Second).Format(time.RFC3339)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if automationHealthDue(configPath, base.Add(59*time.Second)) {
		t.Fatal("1-minute watchdog became due before one minute from probe start")
	}
	if !automationHealthDue(configPath, base.Add(time.Minute)) {
		t.Fatal("1-minute watchdog did not become due one minute from probe start")
	}
}

func TestPostUpdateGuardAllowsConfirmedFailureRecoveryButKeepsUncertainFailClosed(t *testing.T) {
	dir := t.TempDir()
	updateState := filepath.Join(dir, "self-update.state")
	automationState := filepath.Join(dir, "automation.state")
	t.Setenv("FREENET_AUTOMATION_STATE", automationState)
	t.Setenv("FREENET_AUTOMATION_HISTORY", filepath.Join(dir, "automation.history"))
	if err := os.WriteFile(updateState, []byte("STATE=SUCCESS\nTARGET_VERSION=v"+version+"\nUPDATED_AT=2026-10-03T08:00:00Z\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: config{UpdateState: updateState}}

	recovery, handled := automationPostUpdateGuardResult(a, automationHealthProbe{State: automationHealthFailed, Reason: "current VPN failed"})
	if handled || recovery.State != "" {
		t.Fatalf("confirmed failed VPN must continue into normal double-check recovery: handled=%v result=%+v", handled, recovery)
	}
	if pending := automationPendingPostUpdateTarget(a); pending != "v"+version {
		t.Fatalf("post-update target must remain pending until healthy/apply acceptance: %q", pending)
	}

	blocked, handled := automationPostUpdateGuardResult(a, automationHealthProbe{State: automationHealthUncertain, Reason: "probe busy"})
	if !handled || blocked.State != automationHealthUncertain {
		t.Fatalf("uncertain post-update state must remain fail-closed: handled=%v result=%+v", handled, blocked)
	}
	if got := parseAutomationState(automationState)["POST_UPDATE_ACK"]; got != "" {
		t.Fatalf("uncertain post-update state unexpectedly acknowledged: %q", got)
	}

	healthy, handled := automationPostUpdateGuardResult(a, automationHealthProbe{State: automationHealthHealthy, Reason: "exact current VPN healthy"})
	if !handled || healthy.State != automationHealthHealthy {
		t.Fatalf("healthy post-update state must be accepted read-only: handled=%v result=%+v", handled, healthy)
	}
	if got := parseAutomationState(automationState)["POST_UPDATE_ACK"]; got != "v"+version {
		t.Fatalf("healthy acceptance did not persist post-update acknowledgement: %q", got)
	}
	if pending := automationPendingPostUpdateTarget(a); pending != "" {
		t.Fatalf("post-update hold survived healthy acceptance: %q", pending)
	}
}

func TestRollbackGuardClearsOnlyAfterFactualStateIsEstablished(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FREENET_AUTOMATION_STATE", filepath.Join(dir, "automation.state"))
	t.Setenv("FREENET_AUTOMATION_HISTORY", filepath.Join(dir, "automation.history"))
	writeAutomationStateV2("failed", "rollback unknown", "FAILED/UNKNOWN", false)

	blocked, handled := automationRollbackGuardResult(nil, automationHealthProbe{State: automationHealthFailed, Reason: "VPN path still failed"})
	if !handled || blocked.State != automationHealthUncertain {
		t.Fatalf("failed probe without readable runtime identity must keep rollback guard: handled=%v result=%+v", handled, blocked)
	}
	if !automationMutationBlockedState() {
		t.Fatal("ambiguous failed read-only check cleared rollback guard")
	}

	outPath := filepath.Join(dir, "04_outbounds.json")
	filterPath := filepath.Join(dir, "profile.filter")
	if err := os.WriteFile(outPath, []byte(`{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"192.0.2.99","port":443}]}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filterPath, []byte("IT Milan, Italy, Extra\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: config{OutPath: outPath, FilterPath: filterPath}}
	reconciled, handled := automationRollbackGuardResult(a, automationHealthProbe{State: automationHealthFailed, Reason: "exact current VPN failed"})
	if handled || reconciled.State != "" {
		t.Fatalf("known failed state must continue into normal double-check recovery: handled=%v result=%+v", handled, reconciled)
	}
	if automationMutationBlockedState() {
		t.Fatal("factual known-failed current state did not clear stale rollback guard")
	}

	writeAutomationStateV2("failed", "rollback unknown again", "FAILED/UNKNOWN", false)
	cleared, handled := automationRollbackGuardResult(a, automationHealthProbe{State: automationHealthHealthy, Reason: "exact current VPN healthy"})
	if !handled || cleared.State != automationHealthHealthy {
		t.Fatalf("healthy factual state must clear rollback guard: handled=%v result=%+v", handled, cleared)
	}
	if automationMutationBlockedState() {
		t.Fatal("healthy exact-current read-only acceptance did not clear rollback guard")
	}
}

func TestReadOnlyHealthObservationDoesNotTakeManualRecoveryFence(t *testing.T) {
	data, err := os.ReadFile("automation_health.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "func (a *app) runAutomationHealthWatch")
	end := strings.Index(text[start:], "\nfunc init()")
	if start < 0 || end < 0 {
		t.Fatal("health watch implementation not found")
	}
	segment := text[start : start+end]
	firstProbe := strings.Index(segment, "first := a.probeAutomationCurrentVPN(probeCtx)")
	postGuard := strings.Index(segment, "automationPostUpdateGuardResult(a, first)")
	lock := strings.Index(segment, "release, err := acquireAutomationHealthLock()")
	confirmProbe := strings.Index(segment, "confirm := a.probeAutomationCurrentVPN(probeCtx)")
	if firstProbe < 0 || postGuard < 0 || lock < 0 || confirmProbe < 0 {
		t.Fatalf("manual-recovery fence contract incomplete: probe=%d guard=%d lock=%d confirm=%d", firstProbe, postGuard, lock, confirmProbe)
	}
	if !(firstProbe < postGuard && postGuard < lock && lock < confirmProbe) {
		t.Fatalf("read-only health probe/guard must finish before exclusive recovery fence: probe=%d guard=%d lock=%d confirm=%d", firstProbe, postGuard, lock, confirmProbe)
	}
	if got := strings.Count(segment, "a.probeAutomationCurrentVPN(probeCtx)"); got != 2 {
		t.Fatalf("AUTO recovery must use exactly initial+fenced confirmation before recovery, got %d VPN probes", got)
	}
	if strings.Contains(segment, "automationHealthConfirmDelay") || strings.Contains(segment, "time.After(") {
		t.Fatal("AUTO recovery must not add a third delayed duplicate VPN confirmation")
	}
}

func TestBusyHealthResultDoesNotAdvanceHealthTimestamp(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "settings.state")
	t.Setenv("FREENET_SETTINGS_V3_STATE", statePath)
	const original = "HEALTH_LAST=2026-10-03T06:30:00Z\nHEALTH_RESULT=healthy\nHEALTH_MESSAGE=ok\n"
	if err := os.WriteFile(statePath, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	recordSettingsV3Health(automationHealthResult{State: "busy", Reason: "another recovery is active"})
	got, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("busy scheduler collision changed health state:\n%s", got)
	}
}

func TestMainStartsSubMinuteHealthScheduler(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "a.startAutomationHealthScheduler()") {
		t.Fatal("long-running FreeNet service must own the 30-second health cadence")
	}
}

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

func TestAutomationReachableQualityUsesSeverityWithoutTriggeringRecovery(t *testing.T) {
	cases := []struct {
		name        string
		appMS       int
		serviceOK   int
		serviceTotal int
		wantState   string
		wantPoints  int
	}{
		{"healthy", 180, 4, 4, automationHealthHealthy, 0},
		{"mild-latency", 235, 4, 4, automationHealthUncertain, 1},
		{"strong-latency", 398, 4, 4, automationHealthUncertain, 2},
		{"severe-latency", 830, 4, 4, automationHealthUncertain, 3},
		{"partial-3-of-4", 180, 3, 4, automationHealthUncertain, 1},
		{"partial-2-of-4", 180, 2, 4, automationHealthUncertain, 2},
		{"zero-of-4", 180, 0, 4, automationHealthUncertain, 3},
		{"stronger-signal-wins", 398, 3, 4, automationHealthUncertain, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyAutomationReachableQuality(tc.appMS, tc.serviceOK, tc.serviceTotal)
			if got.State != tc.wantState || got.QualityPoints != tc.wantPoints || got.QualityDegraded != (tc.wantPoints > 0) {
				t.Fatalf("result=%+v want state=%s points=%d", got, tc.wantState, tc.wantPoints)
			}
		})
	}
	if got := classifyAutomationApplicationFailure(true); got.QualityDegraded || got.QualityPoints != 0 {
		t.Fatalf("ambiguous named-origin failure must not count as quality degradation: %+v", got)
	}
}

func TestAutomationQualityOptimizationUsesWeightedSeverity(t *testing.T) {
	base := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	state := map[string]string{}

	updates, due := automationQualityOptimizationPlan(state, base, 1)
	if due || updates["QUALITY_DEGRADED_COUNT"] != "1" {
		t.Fatalf("first mild point updates=%v due=%v", updates, due)
	}
	for k, v := range updates {
		state[k] = v
	}

	// Healthy observations do not erase recent degradation inside the window.
	updates, due = automationQualityOptimizationPlan(state, base.Add(5*time.Minute), 0)
	if due {
		t.Fatal("healthy sample unexpectedly triggered optimization")
	}
	for k, v := range updates {
		state[k] = v
	}

	updates, due = automationQualityOptimizationPlan(state, base.Add(10*time.Minute), 1)
	if due || updates["QUALITY_DEGRADED_COUNT"] != "2" {
		t.Fatalf("second mild point updates=%v due=%v", updates, due)
	}
	for k, v := range updates {
		state[k] = v
	}

	updates, due = automationQualityOptimizationPlan(state, base.Add(20*time.Minute), 1)
	if !due {
		t.Fatalf("three mild points inside %s must trigger optimization: updates=%v", automationQualityStrikeWindow, updates)
	}

	state = map[string]string{}
	updates, due = automationQualityOptimizationPlan(state, base, 2)
	if due {
		t.Fatalf("one strong sample must not yet trigger optimization: %v", updates)
	}
	for k, v := range updates {
		state[k] = v
	}
	updates, due = automationQualityOptimizationPlan(state, base.Add(5*time.Minute), 2)
	if !due {
		t.Fatalf("two strong samples must trigger optimization: %v", updates)
	}

	state = map[string]string{}
	updates, due = automationQualityOptimizationPlan(state, base, 3)
	if !due || updates["QUALITY_OPTIMIZATION_LAST"] == "" {
		t.Fatalf("one severe sample must trigger accelerated optimization: updates=%v due=%v", updates, due)
	}
}

func TestAutomationQualityOptimizationCooldownDependsOnSeverity(t *testing.T) {
	base := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	state := map[string]string{
		"QUALITY_DEGRADED_COUNT": "1",
		"QUALITY_DEGRADED_SINCE": base.Format(time.RFC3339),
	}
	updates, due := automationQualityOptimizationPlan(state, base.Add(automationQualityStrikeWindow+time.Minute), 0)
	if due || updates["QUALITY_DEGRADED_COUNT"] != "0" {
		t.Fatalf("expired mild spike must clear without scan: updates=%v due=%v", updates, due)
	}

	last := base.Add(-30 * time.Minute)
	state = map[string]string{
		"QUALITY_DEGRADED_COUNT": "2",
		"QUALITY_DEGRADED_SINCE": base.Add(-10 * time.Minute).Format(time.RFC3339),
		"QUALITY_OPTIMIZATION_LAST": last.Format(time.RFC3339),
	}
	updates, due = automationQualityOptimizationPlan(state, base, 1)
	if due {
		t.Fatalf("normal quality scan ignored %s cooldown: %v", automationQualityOptimizationCooldown, updates)
	}

	state["QUALITY_DEGRADED_COUNT"] = "0"
	state["QUALITY_DEGRADED_SINCE"] = ""
	updates, due = automationQualityOptimizationPlan(state, base, 3)
	if !due {
		t.Fatalf("severe degradation older than %s must bypass normal cooldown: %v", automationQualitySevereCooldown, updates)
	}

	state["QUALITY_OPTIMIZATION_LAST"] = base.Add(-5 * time.Minute).Format(time.RFC3339)
	state["QUALITY_DEGRADED_COUNT"] = "0"
	state["QUALITY_DEGRADED_SINCE"] = ""
	updates, due = automationQualityOptimizationPlan(state, base, 3)
	if due {
		t.Fatalf("severe degradation must still respect short %s cooldown: %v", automationQualitySevereCooldown, updates)
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

func TestManagedCronKeepsHealthWatchdogAndRetiresPeriodicBest(t *testing.T) {
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
	if !strings.Contains(text, "* * * * * "+v3ShellQuote(automationRunnerPath())+" automation-health-watch --config "+v3ShellQuote(path)) {
		t.Fatalf("health watchdog is not scheduled with the configured 1-minute fallback:\n%s", text)
	}
	for _, forbidden := range []string{"automation-best-run", "/opt/lib/freenet/auto_vpn.sh", "/opt/bin/vpn failover"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("periodic legacy/heavy scheduler path %q must be retired:\n%s", forbidden, text)
		}
	}
}

func TestManagedCronManualModeStillKeepsHealthWatchdog(t *testing.T) {
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
	if !strings.Contains(text, "automation-health-watch") {
		t.Fatalf("manual optimization mode must still keep VPN liveness watchdog:\n%s", text)
	}
	if strings.Contains(text, "automation-best-run") || strings.Contains(text, "settings-v3-endpoint-refresh") {
		t.Fatalf("manual optimization mode must not schedule heavy optimization or endpoint refresh:\n%s", text)
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
	if !strings.Contains(string(normalSource), "if currentState == \"healthy\" && automationCooldownActive(parseAutomationLastSwitch(automationStatePath()), time.Now().UTC())") {
		t.Fatal("normal Best optimization must keep anti-flap cooldown only while the current VPN is still healthy")
	}
}

func TestHealthRecoveryStopsSameCycleWhenRollbackLatchIsSet(t *testing.T) {
	data, err := os.ReadFile("automation_health.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "endpointResult, endpointErr := a.runAutomationEndpointEmergency")
	best := strings.Index(text[start:], "best, bestErr := a.runAutomationBestEmergencyCycle")
	if start < 0 || best < 0 {
		t.Fatal("health recovery endpoint/Best stages not found")
	}
	between := text[start : start+best]
	if !strings.Contains(between, "if automationMutationBlockedState()") {
		t.Fatal("health recovery must STOP before Best fallback when endpoint rollback latch is active")
	}
}

func TestEmergencyBestApplyCannotBypassMeasuredSelectionSnapshot(t *testing.T) {
	data, err := os.ReadFile("automation_health.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "func (a *app) runAutomationBestEmergencyCycle")
	end := strings.Index(text, "func (a *app) runAutomationEndpointEmergency")
	if start < 0 || end <= start {
		t.Fatal("emergency Best cycle contract is missing")
	}
	segment := text[start:end]
	tokenCheck := strings.Index(segment, "validBestServerSelectionToken(candidates.SelectionToken)")
	applyCall := strings.Index(segment, "a.executeProviderProfileApply")
	tokenPass := strings.Index(segment, "SelectionToken: candidates.SelectionToken")
	if tokenCheck < 0 || applyCall < 0 || tokenPass < 0 {
		t.Fatalf("AUTO measured-snapshot contract incomplete: check=%d apply=%d pass=%d", tokenCheck, applyCall, tokenPass)
	}
	if !(tokenCheck < applyCall && tokenPass > applyCall) {
		t.Fatalf("AUTO provider mutation is not gated by measured selection token: check=%d apply=%d pass=%d", tokenCheck, applyCall, tokenPass)
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
	if !strings.Contains(segment, "context.WithTimeout(parent, automationEndpointRecoveryTimeout)") {
		t.Fatal("endpoint emergency must be one bounded fast-path")
	}
	if automationEndpointRecoveryTimeout != 20*time.Second {
		t.Fatalf("endpoint recovery timeout=%s want=20s fast-path", automationEndpointRecoveryTimeout)
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
