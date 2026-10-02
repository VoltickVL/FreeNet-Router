package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBestServerAsyncJobFitsBrowserBudget(t *testing.T) {
	const browserBudget = 210 * time.Second
	const minimumSlack = 10 * time.Second
	if bestServerAsyncJobTimeout >= browserBudget {
		t.Fatalf("Best Server async job timeout %s must be below browser budget %s", bestServerAsyncJobTimeout, browserBudget)
	}
	if browserBudget-bestServerAsyncJobTimeout < minimumSlack {
		t.Fatalf("Best Server async job needs at least %s browser slack; got %s", minimumSlack, browserBudget-bestServerAsyncJobTimeout)
	}
}

func TestBestServerJobBudgetCompletesThreeVisibleAttempts(t *testing.T) {
	// The UI promises up to three real measured alternatives. Reserving only
	// enough time to START the third deep probe is insufficient: parent deadline
	// cancellation makes that batch untrusted and it is intentionally dropped.
	// Cover bounded preflight plus three complete deep windows and their TCP
	// probes, with a small scheduler/process-cleanup reserve.
	tcpWindow := time.Duration(bestServerQualityTCPRuns) * bestServerQualityTCPTimeout
	const completionReserve = 5 * time.Second
	minimum := bestServerPreflightPhaseTimeout +
		time.Duration(bestServerVisibleAlternatives)*(bestServerQualityCandidateTimeout+tcpWindow) +
		completionReserve
	if bestServerAsyncJobTimeout < minimum {
		t.Fatalf("Best Server async job %s is below Top-3 completion floor %s", bestServerAsyncJobTimeout, minimum)
	}
}

func TestBestServerForeignDoesNotImplicitlyRecheckCurrent(t *testing.T) {
	data, err := os.ReadFile("best_server_ux.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, "baselineResponse := a.scanActiveCurrentVPNQuality") {
		t.Fatal("Best Server alternatives job must not spend its bounded budget on an implicit current VPN Speedtest")
	}
	for _, want := range []string{
		"loadBestServerCurrentQuality(currentEndpoint, currentFilter)",
		"bestServerMinimumDeepAttemptBudget",
		"bestServerAttemptContext{Context: ctx}",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("Top-3 bounded completion contract missing %q", want)
		}
	}
}

func TestBestServerAttemptContextHidesDeadlineButKeepsCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	ctx := bestServerAttemptContext{Context: parent}
	if _, ok := ctx.Deadline(); ok {
		t.Fatal("deep-attempt wrapper must hide the absolute deadline from the generic full-window guard")
	}
	cancel()
	select {
	case <-ctx.Done():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("deep-attempt wrapper must preserve parent cancellation")
	}
}

func TestBestServerBrowserTimeoutContract(t *testing.T) {
	data, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Date.now()-started>210000") {
		t.Fatal("browser Best Server timeout marker changed; update the server/browser budget contract together")
	}
}


func TestBestServerQuickRTTSweepCoversTypicalPool(t *testing.T) {
	if bestServerProfilePingHTTPRuns != 1 {
		t.Fatalf("quick RTT runs=%d want=1; ranking must stay lightweight while deep quality owns strict acceptance", bestServerProfilePingHTTPRuns)
	}
	if providerProfileRTTWorkers != isolatedXrayProbeLimit {
		t.Fatalf("RTT workers=%d isolated limit=%d; canonical sweep must respect the global safety cap", providerProfileRTTWorkers, isolatedXrayProbeLimit)
	}
	if bestServerPreflightShortlist != 10 {
		t.Fatalf("deep shortlist=%d want hard max 10", bestServerPreflightShortlist)
	}
	const typicalPool = 48
	waves := (typicalPool + providerProfileRTTWorkers - 1) / providerProfileRTTWorkers
	worstSweep := time.Duration(waves) * bestServerProfilePingTimeout
	if worstSweep > bestServerPreflightPhaseTimeout {
		t.Fatalf("48-profile quick RTT worst sweep %s exceeds preflight phase %s", worstSweep, bestServerPreflightPhaseTimeout)
	}
	data, err := os.ReadFile("best_server_preflight.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	if !strings.Contains(src, "same per-logical-profile") || !strings.Contains(src, "strict HTTP/throughput/services/stability acceptance") {
		t.Fatal("preflight must explicitly remain ranking-only and share the picker RTT engine")
	}
}

func TestBestServerDeepProgressIsCumulativeAcrossBatches(t *testing.T) {
	type progress struct {
		stage     string
		completed int
		total     int
	}
	var got []progress
	parent := context.WithValue(context.Background(), bestServerProgressKey{}, func(stage string, completed, total int) {
		got = append(got, progress{stage: stage, completed: completed, total: total})
	})
	ctx := bestServerDeepProgressContext(parent, 2, 7)
	reportBestServerProgress(ctx, "quality", 0, 1)
	reportBestServerProgress(ctx, "quality", 1, 1)
	if len(got) != 2 || got[0].completed != 2 || got[0].total != 7 || got[1].completed != 3 || got[1].total != 7 {
		t.Fatalf("deep progress must map batch-local progress onto the global candidate sequence: %#v", got)
	}
}

func TestBestServerBrowserShowsAdaptiveDeepProgress(t *testing.T) {
	data, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	if !strings.Contains(src, "Глубоко проверяем лучшие VPN · проверено ${job.completed} · цель до 3 подходящих") {
		t.Fatal("Best Server UI must show cumulative adaptive deep-check progress")
	}
	if strings.Contains(src, "Глубоко проверяем лучшие VPN · завершено ${job.completed} из ${job.total}") {
		t.Fatal("Best Server UI must not reset sequential deep checks to misleading 0 из 1 progress")
	}
}


func TestBestServerRTTShortlistPromotesLateLowLatencyProfile(t *testing.T) {
	candidates := make([]bestServerInternalCandidate, 48)
	items := make([]providerProfileRTTItem, 48)
	for i := range candidates {
		id := fmt.Sprintf("p-%02d", i)
		candidates[i].Profile = subscriptionProfile{ID: id, Address: fmt.Sprintf("203.0.113.%d", i+1), Port: 443}
		items[i] = providerProfileRTTItem{ProfileID: id, Reachable: true, Attempted: true, Status: "reachable", RTTMS: 260 + i}
	}
	items[47].RTTMS = 150
	items[46].RTTMS = 160
	items[45].RTTMS = 170
	items[44].RTTMS = 180

	got := selectBestServerRTTShortlistIndexes(candidates, items, -1)
	if len(got) != bestServerPreflightShortlist {
		t.Fatalf("shortlist len=%d want=%d", len(got), bestServerPreflightShortlist)
	}
	want := []string{"p-47", "p-46", "p-45", "p-44"}
	for i, id := range want {
		if candidates[got[i]].Profile.ID != id {
			t.Fatalf("late low-RTT profile lost at position %d: got=%s want=%s shortlist=%#v", i, candidates[got[i]].Profile.ID, id, got)
		}
	}
}

func TestBestServerRTTShortlistUsesUnknownOnlyAsReserve(t *testing.T) {
	candidates := make([]bestServerInternalCandidate, 14)
	items := make([]providerProfileRTTItem, 0, len(candidates))
	for i := range candidates {
		id := fmt.Sprintf("p-%02d", i)
		candidates[i].Profile = subscriptionProfile{ID: id}
		switch {
		case i < 8:
			items = append(items, providerProfileRTTItem{ProfileID: id, Reachable: true, Attempted: true, Status: "reachable", RTTMS: 100 + i})
		case i < 11:
			items = append(items, providerProfileRTTItem{ProfileID: id, Status: "unknown"})
		case i < 13:
			items = append(items, providerProfileRTTItem{ProfileID: id, Attempted: true, Status: "transport_only"})
		default:
			items = append(items, providerProfileRTTItem{ProfileID: id, Attempted: true, Status: "unreachable"})
		}
	}
	got := selectBestServerRTTShortlistIndexes(candidates, items, -1)
	if len(got) != 10 {
		t.Fatalf("shortlist len=%d want=10: %#v", len(got), got)
	}
	for i := 0; i < 8; i++ {
		if candidates[got[i]].Profile.ID != fmt.Sprintf("p-%02d", i) {
			t.Fatalf("confirmed RTT ordering changed: %#v", got)
		}
	}
	if candidates[got[8]].Profile.ID != "p-08" || candidates[got[9]].Profile.ID != "p-09" {
		t.Fatalf("UNKNOWN reserve must follow all confirmed RTT results and precede transport-only: %#v", got)
	}
}
