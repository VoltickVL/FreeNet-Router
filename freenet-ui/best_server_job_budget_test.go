package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestProviderRTTFullPoolFitsHTTPWriteBudget(t *testing.T) {
	const responseSlack = 10 * time.Second
	required := providerProfileRTTDiscoveryTimeout + bestServerRTTSweepTimeout(bestServerMaxCandidates) + responseSlack
	if controlCenterWriteTimeout < required {
		t.Fatalf("provider RTT full-pool budget=%s exceeds HTTP write timeout=%s", required, controlCenterWriteTimeout)
	}
}

func TestBestServerAsyncJobFitsBrowserBudget(t *testing.T) {
	const browserBudget = 340 * time.Second
	const minimumSlack = 10 * time.Second
	if bestServerAsyncJobTimeout >= browserBudget {
		t.Fatalf("Best Server async job timeout %s must be below browser budget %s", bestServerAsyncJobTimeout, browserBudget)
	}
	if browserBudget-bestServerAsyncJobTimeout < minimumSlack {
		t.Fatalf("Best Server async job needs at least %s browser slack; got %s", minimumSlack, browserBudget-bestServerAsyncJobTimeout)
	}
}

func TestBestServerJobBudgetCompletesThreeVisibleAttempts(t *testing.T) {
	// Cover the maximum 64-profile whole-pool quick VPN RTT sweep, the bounded
	// six-finalist median confirmation, and three complete strict deep windows.
	// Raw endpoint TCP is not a selection gate and has no separate ranking budget.
	const completionReserve = 5 * time.Second
	minimum := bestServerRTTSweepTimeout(bestServerMaxCandidates) +
		bestServerConfirmedRTTSweepTimeout +
		time.Duration(bestServerVisibleAlternatives)*bestServerQualityCandidateTimeout +
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
	if !strings.Contains(string(data), "Date.now()-started>340000") {
		t.Fatal("browser Best Server timeout marker changed; update the server/browser budget contract together")
	}
}


func TestBestServerQuickRTTSweepCoversWholePool(t *testing.T) {
	if providerProfileRTTWorkers != isolatedXrayProbeLimit {
		t.Fatalf("RTT workers=%d isolated limit=%d; canonical sweep must respect the global safety cap", providerProfileRTTWorkers, isolatedXrayProbeLimit)
	}
	if bestServerPreflightShortlist != 10 {
		t.Fatalf("deep shortlist=%d want hard max 10", bestServerPreflightShortlist)
	}
	minimumProfileBudget := bestServerSOCKSStartupTimeout + bestServerTransportProbeTimeout + 500*time.Millisecond
	if bestServerProfilePingTimeout < minimumProfileBudget {
		t.Fatalf("quick VPN RTT profile budget=%s below startup+HTTPS floor=%s", bestServerProfilePingTimeout, minimumProfileBudget)
	}
	for _, pool := range []int{48, bestServerMaxCandidates} {
		waves := (pool + providerProfileRTTWorkers - 1) / providerProfileRTTWorkers
		worstSweep := time.Duration(waves) * bestServerProfilePingTimeout
		budget := bestServerRTTSweepTimeout(pool)
		if budget < worstSweep+bestServerRTTSweepSlack {
			t.Fatalf("pool=%d sweep budget=%s below bounded floor=%s", pool, budget, worstSweep+bestServerRTTSweepSlack)
		}
	}
	data, err := os.ReadFile("best_server_preflight.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	if !strings.Contains(src, "probeBestServerCanonicalVPNPing") || !strings.Contains(src, "DNS/named-origin acceptance") {
		t.Fatal("quick ranking must use the canonical VPN-ping owner and defer named-origin acceptance to deep quality")
	}
	ownerData, err := os.ReadFile("best_server_probe_targets.go")
	if err != nil {
		t.Fatal(err)
	}
	owner := string(ownerData)
	if !strings.Contains(owner, "func probeBestServerCanonicalVPNPing") ||
		!strings.Contains(owner, "probeBestServerTransportRTT(ctx, curlPath, socks)") ||
		!strings.Contains(owner, "bestServerTransportProbeURL") {
		t.Fatal("canonical VPN-ping owner must remain fixed-IP HTTPS through the VPN path")
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
	if !strings.Contains(src, "Глубоко проверяем кандидатов · проверено ${job.completed}") {
		t.Fatal("Best Server UI must show cumulative adaptive deep-check progress without a misleading fixed foreign target")
	}
	if strings.Contains(src, "Глубоко проверяем лучшие VPN · завершено ${job.completed} из ${job.total}") {
		t.Fatal("Best Server UI must not reset sequential deep checks to misleading 0 из 1 progress")
	}
}


func TestBestServerBrowserExplainsDeepShortlistAndTop3Target(t *testing.T) {
	data, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	for _, want := range []string{
		"проверено ${job.completed} из ${job.total} · цель 3 подходящих",
		"Глубоко проверено ${deepChecked} из ${deepTotal}",
		"Найдено ${eligibleFound} из ${eligibleTarget} подходящих альтернатив",
		"Топ-3 альтернативы на основе реальных измерений",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("Best Server progress/summary contract missing %q", want)
		}
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
	candidates := make([]bestServerInternalCandidate, 12)
	items := make([]providerProfileRTTItem, 0, len(candidates))
	for i := range candidates {
		id := fmt.Sprintf("p-%02d", i)
		candidates[i].Profile = subscriptionProfile{ID: id}
		switch {
		case i < 8:
			items = append(items, providerProfileRTTItem{ProfileID: id, Reachable: true, Attempted: true, Status: "reachable", RTTMS: 100 + i})
		case i < 10:
			items = append(items, providerProfileRTTItem{ProfileID: id, Status: "unknown"})
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
		t.Fatalf("UNKNOWN reserve must follow all confirmed RTT results: %#v", got)
	}
}
