package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// The test executable stands in for curl. It records only numeric fixture
// metadata, never opens a network socket, and never opens curl's -o target.
func init() {
	if os.Getenv("FREENET_SERVICE_CURL_FIXTURE") == "1" {
		os.Exit(serviceCurlFixtureMain())
	}
}

type serviceCurlFixtureRecord struct {
	PID     int `json:"pid"`
	Service int `json:"service"`
}

func serviceCurlFixtureMain() int {
	args := os.Args[1:]
	if len(args) == 0 {
		return 90
	}
	flagCount := func(flag, value string) int {
		count := 0
		for index, arg := range args {
			if arg == flag && (value == "" || (index+1 < len(args) && args[index+1] == value)) {
				count++
			}
		}
		return count
	}
	if flagCount("-I", "") != 1 || flagCount("-L", "") != 1 ||
		flagCount("--socks5-hostname", "") != 1 || flagCount("--max-time", "3") != 1 ||
		flagCount("--connect-timeout", "2") != 1 || flagCount("--max-redirs", "2") != 1 {
		return 91
	}
	index := -1
	for candidate, target := range bestServerMediaServiceURLs {
		if args[len(args)-1] == target {
			index = candidate
		}
	}
	if index < 0 && args[len(args)-1] == bestServerChatGPTHTTPSURL {
		index = 4 // Optional diagnostic: not one of the four AUTO QoE service bits.
	}
	if index < 0 {
		return 92
	}
	record := serviceCurlFixtureRecord{PID: os.Getpid(), Service: index}
	data, err := json.Marshal(record)
	if err != nil {
		return 93
	}
	path := filepath.Join(os.Getenv("FREENET_SERVICE_CURL_RECORDS"), strconv.Itoa(record.PID)+".json")
	if err := os.WriteFile(path+".tmp", data, 0600); err != nil {
		return 94
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		return 95
	}
	code := "204"
	switch os.Getenv("FREENET_SERVICE_CURL_MODE") {
	case "hang":
		time.Sleep(12 * time.Second)
		return 96
	case "mixed":
		if index == 0 {
			code = "403"
		}
		if index == 1 {
			return 28
		}
	case "transient-once":
		if index == 1 {
			// Exactly one transport failure, then a successful HTTPS response.
			// Create-exclusive metadata works across the two subprocesses
			// without any real network access or shared in-memory state.
			marker := filepath.Join(os.Getenv("FREENET_SERVICE_CURL_RECORDS"), "transient-1.flag")
			file, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err == nil {
				_ = file.Close()
				return 28
			}
			if !os.IsExist(err) {
				return 98
			}
		}
	case "certificate":
		if index == 0 || index == 4 {
			return 60
		}
	case "malformed":
		if index == 0 || index == 4 {
			fmt.Fprint(os.Stdout, "204\tNaN\tInf")
			return 0
		}
	case "status":
		code = os.Getenv("FREENET_SERVICE_CURL_STATUS")
		status, err := strconv.Atoi(code)
		if err != nil || len(code) != 3 || status < 200 || status > 599 {
			return 97
		}
	}
	fmt.Fprintf(os.Stdout, "%s\t0.10\t0.20", code)
	fmt.Fprint(os.Stderr, "private synthetic diagnostic must not enter service evidence")
	return 0
}

func newServiceCurlFixture(t *testing.T, mode string) (string, string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Setenv("FREENET_SERVICE_CURL_FIXTURE", "1")
	t.Setenv("FREENET_SERVICE_CURL_RECORDS", dir)
	t.Setenv("FREENET_SERVICE_CURL_MODE", mode)
	return executable, dir
}

func serviceCurlFixtureRecords(t *testing.T, dir string) []serviceCurlFixtureRecord {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	records := make([]serviceCurlFixtureRecord, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var record serviceCurlFixtureRecord
		if err := json.Unmarshal(data, &record); err != nil || record.PID <= 0 || record.Service < 0 || record.Service > 4 {
			t.Fatalf("invalid service child record: %s", filepath.Base(path))
		}
		records = append(records, record)
	}
	return records
}

func assertServiceCurlChildrenReaped(t *testing.T, records []serviceCurlFixtureRecord) {
	t.Helper()
	for _, record := range records {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(record.PID, &status, syscall.WNOHANG, nil)
		if !errors.Is(err, syscall.ECHILD) {
			t.Fatalf("probe returned before child Wait: child=%d wait=%d err=%v", record.PID, pid, err)
		}
	}
}

func TestBestServerServiceDetailedPreservesIDsAndCounts(t *testing.T) {
	curl, recordsDir := newServiceCurlFixture(t, "mixed")
	result := probeBestServerServiceReachabilityDetailed(context.Background(), curl, "127.0.0.1:18181")
	if !result.Complete || result.OKMask != 12 || result.BadMask != 3 {
		t.Fatalf("HTTP restriction/target timeout lost service identity: %+v", result)
	}
	ok, total := probeBestServerServiceReachability(context.Background(), curl, "127.0.0.1:18181")
	if ok != 2 || total != 4 {
		t.Fatalf("existing counts API changed: %d/%d", ok, total)
	}
	records := serviceCurlFixtureRecords(t, recordsDir)
	if len(records) != 8 {
		t.Fatalf("expected exactly one bounded HEAD per service per call, got %d", len(records))
	}
	assertServiceCurlChildrenReaped(t, records)
}

func TestBestServerServiceHTTPRestrictionsAreQoEOnly(t *testing.T) {
	for _, status := range []string{"403", "405", "429", "503"} {
		t.Run(status, func(t *testing.T) {
			curl, recordsDir := newServiceCurlFixture(t, "status")
			t.Setenv("FREENET_SERVICE_CURL_STATUS", status)
			result := probeBestServerServiceReachabilityDetailed(context.Background(), curl, "127.0.0.1:18181")
			if !result.Complete || result.OKMask != 0 || result.BadMask != bestServerServiceAllMask {
				t.Fatalf("completed HTTP response was not bounded service evidence: %+v", result)
			}
			probe := classifyAutomationReachableQuality(180, 0, 4)
			if probe.State != automationHealthUncertain || probe.QualityPoints != 0 ||
				classifyAutomationHealth(probe, true, probe).State == automationHealthCritical {
				t.Fatal("service response authorized a global VPN outage")
			}
			assertServiceCurlChildrenReaped(t, serviceCurlFixtureRecords(t, recordsDir))
		})
	}
}

func TestBestServerServiceLocalAndIncompleteFailuresStayUnknown(t *testing.T) {
	for _, mode := range []string{"certificate", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			curl, recordsDir := newServiceCurlFixture(t, mode)
			result := probeBestServerServiceReachabilityDetailed(context.Background(), curl, "127.0.0.1:18181")
			if result.Complete || result.OKMask != 14 || result.BadMask != 0 {
				t.Fatalf("unknown service result became a bad-service strike: %+v", result)
			}
			assertServiceCurlChildrenReaped(t, serviceCurlFixtureRecords(t, recordsDir))
		})
	}
	missing := filepath.Join(t.TempDir(), "missing-curl")
	if result := probeBestServerServiceReachabilityDetailed(context.Background(), missing, "127.0.0.1:18181"); result.Complete || result.OKMask != 0 || result.BadMask != 0 {
		t.Fatalf("curl startup failure became VPN quality evidence: %+v", result)
	}
}

func TestBestServerServiceParentCancelWaitsAndProducesNoCompleteEvidence(t *testing.T) {
	curl, recordsDir := newServiceCurlFixture(t, "hang")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bestServerServiceReachabilityResult, 1)
	completed := make(chan struct{})
	go func() {
		defer close(completed)
		done <- probeBestServerServiceReachabilityDetailed(ctx, curl, "127.0.0.1:18181")
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-completed:
		case <-time.After(5 * time.Second):
			t.Error("service probe did not drain after cancellation")
		}
	})
	deadline := time.Now().Add(2 * time.Second)
	for len(serviceCurlFixtureRecords(t, recordsDir)) < 4 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	records := serviceCurlFixtureRecords(t, recordsDir)
	if len(records) != 4 {
		t.Fatal("fixture did not start all four actual children")
	}
	cancel()
	select {
	case result := <-done:
		if result.Complete || result.OKMask != 0 || result.BadMask != 0 {
			t.Fatalf("parent cancellation became partial-service evidence: %+v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled service children were not waited")
	}
	assertServiceCurlChildrenReaped(t, records)
}

func TestBestServerServiceOwnTargetDeadlineIsQoEOnlyAndWaits(t *testing.T) {
	curl, recordsDir := newServiceCurlFixture(t, "hang")
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	result := probeBestServerServiceReachabilityDetailed(ctx, curl, "127.0.0.1:18181")
	if ctx.Err() != nil || !result.Complete || result.OKMask != 0 || result.BadMask != bestServerServiceAllMask {
		t.Fatalf("target timeout was confused with whole-probe cancellation: %+v ctx=%v", result, ctx.Err())
	}
	records := serviceCurlFixtureRecords(t, recordsDir)
	if len(records) != 4 {
		t.Fatalf("not all target timeouts were measured: %d", len(records))
	}
	assertServiceCurlChildrenReaped(t, records)
}

func TestBestServerManualMediaQualityRetriesOneTransientTransportFailure(t *testing.T) {
	curl, recordsDir := newServiceCurlFixture(t, "transient-once")
	ctx, cancel := context.WithTimeout(context.Background(), bestServerServiceTimeout)
	defer cancel()
	ok, total := probeBestServerServiceReachabilityForMedia(ctx, curl, "127.0.0.1:18181")
	if ok != 4 || total != 4 {
		t.Fatalf("one recovered TCP transport error should produce observed 4/4, got %d/%d", ok, total)
	}
	records := serviceCurlFixtureRecords(t, recordsDir)
	attempts := map[int]int{}
	for _, record := range records {
		attempts[record.Service]++
	}
	if len(records) != 5 || attempts[0] != 1 || attempts[1] != 2 || attempts[2] != 1 || attempts[3] != 1 {
		t.Fatalf("manual quality must retry only the failed service exactly once, got %+v", attempts)
	}
	assertServiceCurlChildrenReaped(t, records)
}

func TestBestServerManualMediaQualityNeverTurnsPermanentRestrictionIntoSuccess(t *testing.T) {
	curl, recordsDir := newServiceCurlFixture(t, "mixed")
	ok, total := probeBestServerServiceReachabilityForMedia(context.Background(), curl, "127.0.0.1:18181")
	if ok != 2 || total != 4 {
		t.Fatalf("HTTP 403 and repeated transport failure must remain unavailable, got %d/%d", ok, total)
	}
	records := serviceCurlFixtureRecords(t, recordsDir)
	attempts := map[int]int{}
	for _, record := range records {
		attempts[record.Service]++
	}
	if len(records) != 5 || attempts[0] != 1 || attempts[1] != 2 || attempts[2] != 1 || attempts[3] != 1 {
		t.Fatalf("HTTP 403 must not retry and transient failure must retry exactly once: %+v", attempts)
	}
	assertServiceCurlChildrenReaped(t, records)
}

func TestBestServerManualMediaQualityLocalFailureDoesNotBecomeBadService(t *testing.T) {
	curl, recordsDir := newServiceCurlFixture(t, "certificate")
	res := probeBestServerServiceReachabilityMode(context.Background(), curl, "127.0.0.1:18181", true)
	if res.Complete || res.OKMask != 14 || res.BadMask != 0 {
		t.Fatalf("unknown local/certificate failure must stay unknown: %+v", res)
	}
	records := serviceCurlFixtureRecords(t, recordsDir)
	if len(records) != 4 {
		t.Fatalf("unknown failures must not launch a retry: %+v", records)
	}
	assertServiceCurlChildrenReaped(t, records)
}

func TestBestServerAUTOServiceEvidenceDoesNotRetryTransientTransportFailure(t *testing.T) {
	curl, recordsDir := newServiceCurlFixture(t, "transient-once")
	result := probeBestServerServiceReachabilityDetailed(context.Background(), curl, "127.0.0.1:18181")
	if !result.Complete || result.OKMask != 13 || result.BadMask != 2 {
		t.Fatalf("AUTO must keep one-observation service mask and confirmation timing: %+v", result)
	}
	records := serviceCurlFixtureRecords(t, recordsDir)
	if len(records) != 4 {
		t.Fatalf("AUTO must still use exactly one target probe per service, got %d", len(records))
	}
	assertServiceCurlChildrenReaped(t, records)
}
