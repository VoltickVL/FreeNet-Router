package main

import (
	"fmt"
	"context"
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


func TestBestServerPreflightUsesTwoBoundedRankingSamples(t *testing.T) {
	if bestServerPreflightHTTPRuns != 2 {
		t.Fatalf("preflight HTTP runs=%d want=2; ranking should resist a single transient sample", bestServerPreflightHTTPRuns)
	}
	const socksStartupBudget = 3 * time.Second
	const perHTTPBudget = 2 * time.Second
	minimumCandidateBudget := socksStartupBudget + time.Duration(bestServerPreflightHTTPRuns)*perHTTPBudget
	if bestServerPreflightCandidateTimeout < minimumCandidateBudget {
		t.Fatalf("preflight candidate timeout=%s below two-sample floor %s", bestServerPreflightCandidateTimeout, minimumCandidateBudget)
	}
	data, err := os.ReadFile("best_server_preflight.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	if !strings.Contains(src, "ranking-only evidence") || !strings.Contains(src, "strict acceptance") {
		t.Fatal("preflight must remain explicitly ranking-only; acceptance belongs to deep quality")
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


func TestBestServerPreflightTimeoutKeepsUnknownProfilesAsReserve(t *testing.T) {
	candidates := make([]bestServerInternalCandidate, 20)
	for i := range candidates {
		candidates[i].Profile = subscriptionProfile{ID: fmt.Sprintf("p-%02d", i)}
	}
	attempted := map[int]bool{}
	for i := 0; i < 8; i++ {
		attempted[i] = true
	}
	measured := []bestServerPreflightResult{{
		Index: 7,
		Probe: bestServerProbeResult{OK: true, Median: 80},
	}}

	got := selectBestServerPreflightIndexes(candidates, measured, attempted, -1)
	if len(got) != bestServerPreflightShortlist {
		t.Fatalf("preflight reserve len=%d want=%d: %#v", len(got), bestServerPreflightShortlist, got)
	}
	if got[0] != 7 {
		t.Fatalf("measured success must stay first, got %#v", got)
	}
	for _, index := range got[1:] {
		if index < 8 {
			t.Fatalf("timed-out unknown profiles must be preferred over explicit preflight failures, got %#v", got)
		}
	}
}

func TestBestServerPreflightZeroSuccessStillReturnsUnknownReserve(t *testing.T) {
	candidates := make([]bestServerInternalCandidate, 20)
	for i := range candidates {
		candidates[i].Profile = subscriptionProfile{ID: fmt.Sprintf("p-%02d", i)}
	}
	attempted := map[int]bool{}
	for i := 0; i < 8; i++ {
		attempted[i] = true
	}

	got := selectBestServerPreflightIndexes(candidates, nil, attempted, -1)
	if len(got) != bestServerPreflightShortlist {
		t.Fatalf("zero-success preflight must retain a full unknown reserve: len=%d got=%#v", len(got), got)
	}
	if got[0] != 8 {
		t.Fatalf("first unattempted profile should lead the reserve, got %#v", got)
	}
}
