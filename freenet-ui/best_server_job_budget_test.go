package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestBestServerAsyncJobFitsBrowserBudget(t *testing.T) {
	const browserBudget = 180 * time.Second
	const minimumSlack = 10 * time.Second
	if bestServerAsyncJobTimeout >= browserBudget {
		t.Fatalf("Best Server async job timeout %s must be below browser budget %s", bestServerAsyncJobTimeout, browserBudget)
	}
	if browserBudget-bestServerAsyncJobTimeout < minimumSlack {
		t.Fatalf("Best Server async job needs at least %s browser slack; got %s", minimumSlack, browserBudget-bestServerAsyncJobTimeout)
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
