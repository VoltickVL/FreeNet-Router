package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBestServerAsyncJobFitsBrowserBudget(t *testing.T) {
	const browserBudget = 180 * time.Second
	const minimumSlack = 5 * time.Second
	if bestServerAsyncJobTimeout >= browserBudget {
		t.Fatalf("Best Server async job timeout %s must be below browser budget %s", bestServerAsyncJobTimeout, browserBudget)
	}
	if browserBudget-bestServerAsyncJobTimeout < minimumSlack {
		t.Fatalf("Best Server async job needs at least %s browser slack; got %s", minimumSlack, browserBudget-bestServerAsyncJobTimeout)
	}
}

func TestBestServerJobReservesThirdVisibleAttempt(t *testing.T) {
	// The foreign scan no longer spends this job on an implicit heavy current
	// quality measurement. After the bounded preflight it must have room for two
	// complete deep candidates plus a real third attempt. A partial third result
	// remains diagnostic/non-switchable rather than disappearing from the UI.
	minimum := bestServerPreflightPhaseTimeout + 2*bestServerQualityCandidateTimeout + bestServerMinimumDeepAttemptBudget
	if bestServerAsyncJobTimeout < minimum {
		t.Fatalf("Best Server async job %s is below Top-3 attempt floor %s", bestServerAsyncJobTimeout, minimum)
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
	if !strings.Contains(string(data), "Date.now()-started>180000") {
		t.Fatal("browser Best Server timeout marker changed; update the server/browser budget contract together")
	}
}
