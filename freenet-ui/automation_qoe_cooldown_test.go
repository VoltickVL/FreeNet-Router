package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func automationQoECooldownFixture(t *testing.T, age time.Duration, origin string) *app {
	t.Helper()
	dir := t.TempDir()
	for key, name := range map[string]string{
		"FREENET_AUTO_HEALTH_LOCK":    "health.lock",
		"FREENET_AUTO_BEST_LOCK":      "best.lock",
		"FREENET_AUTOMATION_STATE":    "automation.state",
		"FREENET_AUTOMATION_HISTORY":  "automation.history",
		"FREENET_SETTINGS_V3_STATE":   "settings.state",
		"FREENET_SETTINGS_V3_HISTORY": "settings.history",
		"FREENET_TECH_JOURNAL":        "technical.jsonl",
		"FREENET_VPN_TRANSACTION_DIR": "checkpoint.pending",
	} {
		t.Setenv(key, filepath.Join(dir, name))
	}
	writeAutomationStatePayload(automationStatePath(), map[string]string{
		"LAST_SWITCH":        time.Now().UTC().Add(-age).Format(time.RFC3339),
		"LAST_SWITCH_ORIGIN": origin,
		"MUTATION_BLOCKED":   "no",
	})
	a := &app{cfg: config{
		ConfigPath: filepath.Join(dir, "freenet.conf"),
		OutPath:    filepath.Join(dir, "outbounds.json"),
		FilterPath: filepath.Join(dir, "provider.filter"),
	}, sem: make(chan struct{}, 1)}
	if err := os.WriteFile(a.cfg.FilterPath, []byte("de Fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAutomationQoECooldownSkipsOnlyAutomaticHealthyOptimization(t *testing.T) {
	oldCurrent, oldForeign := automationBestCurrentQuality, automationBestForeignScan
	t.Cleanup(func() {
		automationBestCurrentQuality, automationBestForeignScan = oldCurrent, oldForeign
	})
	currentErr := errors.New("current measurement failed")
	foreignErr := errors.New("foreign scan reached")
	for _, tc := range []struct {
		name, health, policy, origin, want string
		age                                time.Duration
		manual, autoApply, foreign         bool
	}{
		{"healthy automatic", "healthy", "better", "auto", "cooldown", time.Hour, false, true, false},
		{"degraded better", "degraded", "better", "auto", "failed", time.Hour, false, true, true},
		{"degraded recovery policy", "degraded", "degraded", "auto", "failed", time.Hour, false, true, true},
		{"healthy recovery policy", "healthy", "degraded", "auto", "same", time.Hour, false, true, false},
		{"unavailable is not confirmed failure", "unavailable", "better", "auto", "uncertain", time.Hour, false, true, false},
		{"unmeasured current", "unmeasured", "better", "auto", "uncertain", time.Hour, false, true, false},
		{"measurement error", "error", "better", "auto", "failed", time.Hour, false, true, false},
		{"expired automatic cooldown", "healthy", "better", "auto", "failed", 7 * time.Hour, false, true, true},
		{"manual settling window", "healthy", "better", "manual", "cooldown", 2 * time.Minute, false, true, false},
		{"expired manual settling window", "healthy", "better", "manual", "failed", 6 * time.Minute, false, true, true},
		{"explicit manual scan", "healthy", "better", "auto", "failed", time.Hour, true, true, true},
		{"candidate only scan", "healthy", "better", "auto", "failed", time.Hour, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := automationQoECooldownFixture(t, tc.age, tc.origin)
			before := parseAutomationState(automationStatePath())
			currentCalls, foreignCalls := 0, 0
			automationBestCurrentQuality = func(*app, context.Context) (bestServerQualityResponse, error) {
				currentCalls++
				if tc.health == "error" {
					return bestServerQualityResponse{}, currentErr
				}
				candidate := bestServerQualityCandidate{Tested: true, Available: true, Eligible: true, CountryCode: "de"}
				switch tc.health {
				case "degraded":
					candidate.Eligible = false
				case "unavailable":
					candidate.Available = false
				case "unmeasured":
					candidate.Tested = false
				}
				return bestServerQualityResponse{Candidates: []bestServerQualityCandidate{candidate}}, nil
			}
			automationBestForeignScan = func(*app, context.Context, automationSettings, string) (bestServerQualityResponse, error) {
				foreignCalls++
				// Stop at the scan boundary: no network, candidate apply or shell helper.
				return bestServerQualityResponse{}, foreignErr
			}
			result, err := a.runAutomationBestCycleWithSettings(context.Background(), automationSettings{
				Enabled: true, Mode: automationModeBest, Policy: tc.policy,
				CountryScope: automationCountryRegion, AutoApply: tc.autoApply,
			}, tc.manual)
			wantForeign := 0
			if tc.foreign {
				wantForeign = 1
			}
			if currentCalls != 1 || foreignCalls != wantForeign {
				t.Fatalf("current calls=%d foreign calls=%d; want 1/%d", currentCalls, foreignCalls, wantForeign)
			}
			if tc.foreign && !errors.Is(err, foreignErr) || tc.health == "error" && !errors.Is(err, currentErr) || !tc.foreign && tc.health != "error" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Result != tc.want || result.Mutated || result.ProfileID != "" {
				t.Fatalf("result=%+v want %s without mutation or invented candidate", result, tc.want)
			}
			after := parseAutomationState(automationStatePath())
			if after["LAST_SWITCH"] != before["LAST_SWITCH"] || after["LAST_SWITCH_ORIGIN"] != before["LAST_SWITCH_ORIGIN"] {
				t.Fatal("observation changed the last-switch baseline")
			}
			for _, path := range []string{automationHealthLockPath(), automationBestLockPath()} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("cycle retained lock: %s", path)
				}
			}
		})
	}
}

func TestAutomationQoECooldownKeepsEndpointRefreshFirst(t *testing.T) {
	oldRefresh, oldCurrent, oldForeign := automationBestCurrentRefresh, automationBestCurrentQuality, automationBestForeignScan
	t.Cleanup(func() {
		automationBestCurrentRefresh, automationBestCurrentQuality, automationBestForeignScan = oldRefresh, oldCurrent, oldForeign
	})
	for _, outcome := range []string{"applied", "unsafe", "unchanged"} {
		t.Run(outcome, func(t *testing.T) {
			a := automationQoECooldownFixture(t, time.Hour, "auto")
			calls := []string{}
			automationBestCurrentRefresh = func(*app, context.Context) (int, bestServerRefreshResponse) {
				calls = append(calls, "refresh")
				if outcome == "unsafe" {
					return 503, bestServerRefreshResponse{Mutation: "NONE", RollbackState: "NOT_APPLIED"}
				}
				return 200, bestServerRefreshResponse{Success: true, Applied: outcome == "applied", RollbackState: "NOT_NEEDED"}
			}
			automationBestCurrentQuality = func(*app, context.Context) (bestServerQualityResponse, error) {
				calls = append(calls, "current")
				return bestServerQualityResponse{Candidates: []bestServerQualityCandidate{{Tested: true, Available: true, Eligible: true}}}, nil
			}
			automationBestForeignScan = func(*app, context.Context, automationSettings, string) (bestServerQualityResponse, error) {
				t.Fatal("endpoint/healthy cooldown path reached foreign scan")
				return bestServerQualityResponse{}, nil
			}
			result, err := a.runAutomationBestCycleWithSettings(context.Background(), automationSettings{
				Enabled: true, Mode: automationModeBest, Policy: automationPolicyBetter,
				AutoApply: true, AutoEndpointUpdate: true,
			}, false)
			wantCalls, wantResult := "refresh", "updated"
			if outcome == "unsafe" {
				wantResult = "uncertain"
			}
			if outcome == "unchanged" {
				wantCalls, wantResult = "refresh,current", "cooldown"
			}
			if strings.Join(calls, ",") != wantCalls || result.Result != wantResult || result.Mutated != (outcome == "applied") || (err != nil) != (outcome == "unsafe") {
				t.Fatalf("calls=%v result=%+v err=%v; want %s/%s", calls, result, err, wantCalls, wantResult)
			}
		})
	}
}

func TestAutomationQoECooldownDoesNotSuppressConfirmedFailureRecovery(t *testing.T) {
	a := automationQoECooldownFixture(t, time.Hour, "auto")
	if !automationOptimizationCooldownActive(automationStatePath(), time.Now().UTC()) {
		t.Fatal("fixture must have an active optimization cooldown")
	}
	failed := classifyAutomationApplicationFailure(false, true)
	if classifyAutomationHealth(failed, true, failed).State != automationHealthCritical {
		t.Fatal("two confirmed VPN failures with healthy WAN must authorize emergency recovery")
	}
	oldDiscover := automationEmergencyDiscoverCandidates
	t.Cleanup(func() { automationEmergencyDiscoverCandidates = oldDiscover })
	scanCalls := 0
	scanErr := errors.New("emergency scan reached")
	automationEmergencyDiscoverCandidates = func(*app, context.Context) ([]bestServerInternalCandidate, bool, error) {
		scanCalls++
		return nil, false, scanErr
	}
	result, err := a.runAutomationBestEmergencyCycle(context.Background(), automationSettings{
		Enabled: true, Mode: automationModeBest, Policy: automationPolicyDegraded,
		CountryScope: automationCountryRegion, AutoApply: true,
	})
	if scanCalls != 1 || !errors.Is(err, scanErr) || result.Mutated || result.Result == "cooldown" {
		t.Fatalf("confirmed failure suppressed: scan calls=%d result=%+v err=%v", scanCalls, result, err)
	}
}
