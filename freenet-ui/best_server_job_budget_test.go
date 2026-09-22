package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestBestServerAsyncJobFitsInteractiveBrowserBudget(t *testing.T) {
	const browserBudget = 60 * time.Second
	const minimumSlack = 10 * time.Second
	if bestServerAsyncJobTimeout >= browserBudget {
		t.Fatalf("Best Server async job timeout %s must be below browser budget %s", bestServerAsyncJobTimeout, browserBudget)
	}
	if browserBudget-bestServerAsyncJobTimeout < minimumSlack {
		t.Fatalf("Best Server async job needs at least %s browser slack; got %s", minimumSlack, browserBudget-bestServerAsyncJobTimeout)
	}
}

func TestBestServerCascadeBudgetCoversNormalExpressAndQuickPath(t *testing.T) {
	// 8 quick candidates / 4 workers = two bounded waves. DIRECT express runs
	// concurrently and should fit in a few seconds; keep explicit room for one
	// bounded fresh-catalog read without returning to the former 190 s job.
	quickWaves := (bestServerCascadeShortlist + bestServerQuickWorkers - 1) / bestServerQuickWorkers
	minimum := time.Duration(quickWaves)*bestServerQuickCandidateTimeout +
		bestServerFreshCatalogTimeout + 5*time.Second
	if bestServerAsyncJobTimeout < minimum {
		t.Fatalf("Best Server async job %s below cascade floor %s", bestServerAsyncJobTimeout, minimum)
	}
	if bestServerAsyncJobTimeout >= 60*time.Second {
		t.Fatalf("interactive Best Server budget regressed to long-running scan: %s", bestServerAsyncJobTimeout)
	}
}

func TestBestServerForeignUsesCascadeAndDoesNotImplicitlyRecheckCurrent(t *testing.T) {
	data, err := os.ReadFile("best_server_ux.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	start := strings.Index(s, "func (a *app) scanBestServerForeign(ctx context.Context)")
	if start < 0 {
		t.Fatal("scanBestServerForeign missing")
	}
	body := s[start:]
	if strings.Contains(body, "baselineResponse := a.scanActiveCurrentVPNQuality") {
		t.Fatal("Best Server alternatives job must not spend discovery budget on implicit current VPN Speedtest")
	}
	for _, want := range []string{
		"loadBestServerCurrentQuality(currentEndpoint, currentFilter)",
		"scanBestServerCascade(ctx, candidates, poolSize",
		"Heavy Speedtest/media acceptance is deliberately",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("cascade contract missing %q", want)
		}
	}
}

func TestBestServerBrowserTimeoutContract(t *testing.T) {
	data, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	if !strings.Contains(src, "const maxWait = mode === 'best' ? 60000 : 90000") {
		t.Fatal("browser Best Server timeout must remain 60 s while current-quality keeps a larger bounded window")
	}
	if strings.Contains(src, "Date.now()-started>210000") {
		t.Fatal("legacy 210 s interactive Best Server timeout must stay removed")
	}
}

func TestBestServerBrowserShowsCascadeProgressAndTruthfulTelemetry(t *testing.T) {
	data, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	for _, want := range []string{
		"DIRECT express: проверено ${job.completed} из ${job.total}",
		"VPN quick: проверено ${job.completed} из ${job.total}",
		"Пул: ${pool} · DIRECT: ${express} · VPN quick: ${quick} · строгих: ${strict}",
		"Проверить и использовать",
		"Строгая проверка пройдена. Переключаем VPN",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("cascade browser marker missing %q", want)
		}
	}
	if strings.Contains(src, "Проверено профилей: ${data.profiles_scanned") {
		t.Fatal("pool size must not be presented as fully checked profiles")
	}
}

func TestBestServerExpressAndQuickAreRankingOnly(t *testing.T) {
	data, err := os.ReadFile("best_server_cascade.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	for _, want := range []string{
		"DIRECT-only",
		"Quick evidence is ranking-only. Never set Eligible here.",
		"probeBestServerApplicationPreflight",
		"strictBestServerFinalists",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("cascade safety marker missing %q", want)
		}
	}
}
