package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBestServerAsyncJobFitsBrowserBudget(t *testing.T) {
	const browserBudget = 240 * time.Second
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
	if !strings.Contains(string(data), "Date.now()-started>240000") {
		t.Fatal("browser Best Server timeout marker changed; update the server/browser budget contract together")
	}
}


func TestBestServerPreflightIsSingleSampleRankingOnly(t *testing.T) {
	if bestServerPreflightHTTPRuns != 1 {
		t.Fatalf("preflight HTTP runs=%d want=1; deep quality owns strict acceptance", bestServerPreflightHTTPRuns)
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
