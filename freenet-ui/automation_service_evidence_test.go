package main

import (
	"math/bits"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func automationServiceTestProbe(identity string, bad uint8, latency int) automationHealthProbe {
	probe := classifyAutomationReachableQuality(latency, 4-bits.OnesCount8(bad), 4)
	probe.CurrentIdentity = identity
	probe.ServiceOKMask = bestServerServiceAllMask &^ bad
	probe.ServiceBadMask = bad
	probe.ServiceComplete = true
	return probe
}

func observeAutomationServiceTest(state map[string]string, now time.Time, probe automationHealthProbe) (automationQualityTrigger, bool) {
	updates, trigger, due := automationReachableQualityOptimizationPlan(state, now, probe)
	for key, value := range updates {
		state[key] = value
	}
	return trigger, due
}

func TestAutomationServiceQualityRequiresRepeatedSameServiceAndTwoMinutes(t *testing.T) {
	identity := bestServerCurrentQualityIdentityHash("vpn.invalid:443", "profile-one")
	base := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	state := map[string]string{}
	for index, mask := range []uint8{3, 3, 1, 5} {
		trigger, due := observeAutomationServiceTest(state, base.Add(time.Duration(index)*40*time.Second), automationServiceTestProbe(identity, mask, 180))
		if due != (index == 3) {
			t.Fatalf("observation %d: due=%t state=%v", index, due, state)
		}
		if due && (trigger.CurrentIdentity != identity || trigger.ServiceBadMask != 1 || trigger.LatencyDegraded) {
			t.Fatalf("comparison lost the common service or invented latency: %+v", trigger)
		}
	}
	if state["QUALITY_SERVICE_COUNT"] != "0" || state["QUALITY_OPTIMIZATION_LAST"] != base.Add(2*time.Minute).Format(time.RFC3339) {
		t.Fatalf("comparison did not reserve cooldown and consume evidence: %v", state)
	}

	state = map[string]string{}
	for index := 0; index < 5; index++ {
		_, due := observeAutomationServiceTest(state, base.Add(time.Duration(index)*30*time.Second), automationServiceTestProbe(identity, 1, 180))
		if due != (index == 4) {
			t.Fatalf("four rapid observations bypassed the two-minute minimum: index=%d due=%t", index, due)
		}
	}

	state = map[string]string{}
	for index, mask := range []uint8{1, 2, 4, 8, 1, 2} {
		if _, due := observeAutomationServiceTest(state, base.Add(time.Duration(index)*time.Minute), automationServiceTestProbe(identity, mask, 180)); due {
			t.Fatal("different one-off service failures were combined into a persistent failure")
		}
	}
}

func TestAutomationServiceQualityResetsUnknownHealthyAndChangedIdentity(t *testing.T) {
	identity := bestServerCurrentQualityIdentityHash("vpn.invalid:443", "profile-one")
	base := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	bad := automationServiceTestProbe(identity, 1, 180)
	type testCase struct {
		name string
		probe automationHealthProbe
	}
	cases := []testCase{
		{"healthy", automationServiceTestProbe(identity, 0, 180)},
		{"unknown", automationHealthProbe{State: automationHealthUncertain}},
		{"failed", automationHealthProbe{State: automationHealthFailed}},
		{"missing-identity", automationServiceTestProbe("", 1, 180)},
		{"invalid-identity", automationServiceTestProbe(strings.Repeat("x", 64), 1, 180)},
		{"changed-endpoint", automationServiceTestProbe(bestServerCurrentQualityIdentityHash("other.invalid:443", "profile-one"), 1, 180)},
		{"changed-filter", automationServiceTestProbe(bestServerCurrentQualityIdentityHash("vpn.invalid:443", "profile-two"), 1, 180)},
	}
	incomplete := bad
	incomplete.ServiceComplete = false
	cases = append(cases, testCase{"incomplete", incomplete})
	overlap := bad
	overlap.ServiceOKMask |= overlap.ServiceBadMask
	cases = append(cases, testCase{"overlapping-masks", overlap})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := map[string]string{}
			for index := 0; index < 3; index++ {
				if _, due := observeAutomationServiceTest(state, base.Add(time.Duration(index)*40*time.Second), bad); due {
					t.Fatal("comparison started before the minimum observations")
				}
			}
			if _, due := observeAutomationServiceTest(state, base.Add(2*time.Minute), tc.probe); due {
				t.Fatal("unknown/recovered/different identity completed the old sequence")
			}
			if _, due := observeAutomationServiceTest(state, base.Add(3*time.Minute), bad); due || state["QUALITY_SERVICE_COUNT"] != "1" {
				t.Fatalf("old service evidence survived reset: %v", state)
			}
		})
	}
}

func TestAutomationServiceQualityRejectsDuplicateExpiredAndFutureEvidence(t *testing.T) {
	identity := strings.Repeat("a", 64)
	probe := automationServiceTestProbe(identity, 3, 180)
	base := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	state := map[string]string{}
	observeAutomationServiceTest(state, base, probe)
	for index := 0; index < 10; index++ {
		if _, due := observeAutomationServiceTest(state, base, probe); due || state["QUALITY_SERVICE_COUNT"] != "1" {
			t.Fatal("duplicate observation advanced the service sequence")
		}
	}
	for _, at := range []time.Time{base.Add(21*time.Minute), base.Add(-time.Minute)} {
		if _, due := observeAutomationServiceTest(state, at, probe); due || state["QUALITY_SERVICE_COUNT"] != "1" {
			t.Fatalf("stale/future evidence was carried into a new sequence: %v", state)
		}
	}
	state["QUALITY_SERVICE_COUNT"] = "999"
	if _, due := observeAutomationServiceTest(state, base, probe); due || state["QUALITY_SERVICE_COUNT"] != "1" {
		t.Fatal("malformed persisted count was trusted")
	}
}

func TestAutomationServiceQualityUsesOneHourCooldownAndNeverEmergency(t *testing.T) {
	identity := strings.Repeat("a", 64)
	base := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	for _, bad := range []uint8{1, 3, 15} {
		probe := automationServiceTestProbe(identity, bad, 180)
		if got := classifyAutomationHealth(probe, true, probe); got.State == automationHealthCritical {
			t.Fatalf("service mask=%d authorized global VPN emergency", bad)
		}
		for _, last := range []string{base.Add(-30*time.Minute).Format(time.RFC3339), base.Add(time.Hour).Format(time.RFC3339), "malformed"} {
			state := map[string]string{"QUALITY_OPTIMIZATION_LAST": last}
			for index := 0; index < 10; index++ {
				if _, due := observeAutomationServiceTest(state, base.Add(time.Duration(index)*time.Minute), probe); due {
					t.Fatalf("service mask=%d bypassed normal cooldown=%q", bad, last)
				}
			}
		}
		state := map[string]string{"QUALITY_OPTIMIZATION_LAST": base.Add(-2*time.Hour).Format(time.RFC3339)}
		for index := 0; index < 4; index++ {
			_, due := observeAutomationServiceTest(state, base.Add(time.Duration(index)*time.Minute), probe)
			if due != (index == 3) {
				t.Fatalf("expired cooldown did not allow a bounded comparison: mask=%d index=%d", bad, index)
			}
		}
	}
}

func TestAutomationQualitySignalsReserveOnePersistentAttempt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.state")
	t.Setenv("FREENET_SETTINGS_V3_STATE", path)
	base := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	identity := strings.Repeat("a", 64)
	if err := v3WriteState(map[string]string{
		"QUALITY_SERVICE_IDENTITY": identity,
		"QUALITY_SERVICE_BAD_MASK": "3",
		"QUALITY_SERVICE_COUNT": "3",
		"QUALITY_SERVICE_SINCE": base.Add(-3*time.Minute).Format(time.RFC3339),
		"QUALITY_SERVICE_LAST": base.Add(-time.Minute).Format(time.RFC3339),
		"QUALITY_DEGRADED_COUNT": "3",
		"QUALITY_DEGRADED_SAMPLES": "2",
		"QUALITY_DEGRADED_SINCE": base.Add(-time.Minute).Format(time.RFC3339),
		"HEALTH_LAST": base.Add(-time.Minute).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	probe := automationServiceTestProbe(identity, 1, 350)
	trigger, due := automationReachableQualityOptimizationDue(base, probe)
	if !due || trigger.CurrentIdentity != identity || trigger.ServiceBadMask != 1 || !trigger.LatencyDegraded {
		t.Fatalf("combined trigger did not preserve both real causes: due=%t trigger=%+v", due, trigger)
	}
	state := v3ParseState(path)
	if state["QUALITY_OPTIMIZATION_LAST"] != base.Format(time.RFC3339) || state["QUALITY_SERVICE_COUNT"] != "0" ||
		state["QUALITY_DEGRADED_COUNT"] != "0" || state["HEALTH_LAST"] == "" {
		t.Fatalf("combined reservation was not persisted without losing other state: %v", state)
	}
	// These calls reload only persisted state; no RAM timestamp is involved.
	for index := 1; index <= 5; index++ {
		if _, again := automationReachableQualityOptimizationDue(base.Add(time.Duration(index)*time.Minute), probe); again {
			t.Fatal("the second signal launched another comparison during the same cooldown")
		}
	}
}

func TestAutomationServiceStateContainsOnlyBoundedIdentityAndNumbers(t *testing.T) {
	state := map[string]string{}
	probe := automationServiceTestProbe(bestServerCurrentQualityIdentityHash("private-endpoint.invalid:443", "private-filter"), 5, 180)
	updates, _, due := automationReachableQualityOptimizationPlan(state, time.Now().UTC(), probe)
	if due || len(updates) != 5 {
		t.Fatalf("first service observation unexpectedly expanded state or started a scan: %v", updates)
	}
	for key, value := range updates {
		if strings.Contains(value, "private-") || strings.Contains(value, ":443") {
			t.Fatalf("raw identity escaped into service evidence key=%s", key)
		}
	}
	if mask, err := strconv.Atoi(updates["QUALITY_SERVICE_BAD_MASK"]); err != nil || mask != 5 {
		t.Fatal("service identity was not stored as the fixed bounded mask")
	}
}

func TestAutomationQualityDueFailsClosedWhenStateCannotBeWritten(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "regular-parent")
	if err := os.WriteFile(blocker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FREENET_SETTINGS_V3_STATE", filepath.Join(blocker, "settings.state"))
	probe := automationServiceTestProbe(strings.Repeat("a", 64), 1, 650)
	for index := 0; index < 6; index++ {
		at := time.Date(2026, 10, 22, 10, index, 0, 0, time.UTC)
		if _, due := automationReachableQualityOptimizationDue(at, probe); due || automationQualityOptimizationDue(at, 3) {
			t.Fatal("quality scan allowed without a persisted cooldown reservation")
		}
	}
}

func TestAutomationQualityMalformedCooldownBlocksBothDuePathsAndAllowsHealthyReset(t *testing.T) {
	base := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	identity := strings.Repeat("a", 64)
	for _, raw := range []string{"malformed", "2026-10-22", "2026-13-22T10:00:00Z"} {
		for _, points := range []int{1, 3} {
			for _, mode := range []string{"latency", "combined"} {
				t.Run(raw+"/"+strconv.Itoa(points)+"/"+mode, func(t *testing.T) {
					path := filepath.Join(t.TempDir(), "settings.state")
					t.Setenv("FREENET_SETTINGS_V3_STATE", path)
					if err := v3WriteState(map[string]string{
						"QUALITY_OPTIMIZATION_LAST": raw,
						"QUALITY_DEGRADED_COUNT": "3",
						"QUALITY_DEGRADED_SAMPLES": "2",
						"QUALITY_DEGRADED_SINCE": base.Add(-time.Minute).Format(time.RFC3339),
						"QUALITY_SERVICE_IDENTITY": identity,
						"QUALITY_SERVICE_BAD_MASK": "1",
						"QUALITY_SERVICE_COUNT": "3",
						"QUALITY_SERVICE_SINCE": base.Add(-3*time.Minute).Format(time.RFC3339),
						"QUALITY_SERVICE_LAST": base.Add(-time.Minute).Format(time.RFC3339),
					}); err != nil {
						t.Fatal(err)
					}
					if mode == "latency" {
						if automationQualityOptimizationDue(base, points) {
							t.Fatal("ready latency evidence bypassed a malformed cooldown")
						}
					} else {
						latency := 350
						if points == 3 {
							latency = 650
						}
						trigger, due := automationReachableQualityOptimizationDue(base, automationServiceTestProbe(identity, 1, latency))
						if due || trigger != (automationQualityTrigger{}) {
							t.Fatalf("ready quality evidence bypassed a malformed cooldown: due=%t trigger=%+v", due, trigger)
						}
					}
					if got := v3ParseState(path)["QUALITY_OPTIMIZATION_LAST"]; got != raw {
						t.Fatalf("unknown cooldown was replaced: got=%q want=%q", got, raw)
					}
					if mode == "latency" {
						if automationQualityOptimizationDue(base.Add(time.Second), 0) {
							t.Fatal("healthy reset authorized a comparison")
						}
					} else {
						if _, due := automationReachableQualityOptimizationDue(base.Add(time.Second), automationServiceTestProbe(identity, 0, 180)); due {
							t.Fatal("healthy reset authorized a comparison")
						}
					}
					state := v3ParseState(path)
					if state["QUALITY_OPTIMIZATION_LAST"] != raw || state["QUALITY_DEGRADED_COUNT"] != "0" ||
						state["QUALITY_DEGRADED_SAMPLES"] != "0" || state["QUALITY_DEGRADED_SINCE"] != "" {
						t.Fatalf("healthy reset failed or erased the unknown cooldown: %v", state)
					}
					if mode == "combined" && state["QUALITY_SERVICE_COUNT"] != "0" {
						t.Fatal("healthy reset retained the pending service sequence")
					}
				})
			}
		}
	}
}
