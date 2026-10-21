package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The actual test executable acts as curl. It never opens a network socket,
// invokes an external executable, or writes the path passed to curl's -o flag.
func init() {
	if os.Getenv("FREENET_SPEEDTEST_CURL_FIXTURE") == "1" {
		os.Exit(speedtestCurlFixtureMain())
	}
}

type speedtestCurlFixtureRecord struct {
	PID      int    `json:"pid"`
	MaxTime  string `json:"max_time"`
	MaxCount int    `json:"max_count"`
	WorkMS   int64  `json:"work_ms"`
	Role     string `json:"role"`
}

func speedtestCurlFixtureMain() int {
	go func() {
		time.Sleep(10 * time.Second)
		os.Exit(90)
	}()
	args := os.Args[1:]
	value, count := "", 0
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--max-time" {
			value, count = args[i+1], count+1
		}
	}
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil || !(seconds > 0 && seconds <= 30) || count != 1 {
		return 91
	}
	work := time.Duration(seconds * float64(time.Second))
	target := ""
	if len(args) > 0 {
		target = args[len(args)-1]
	}
	role := "download"
	switch {
	case strings.Contains(target, "/api/js/servers"):
		role = "catalog"
	case strings.Contains(target, "size=256000"):
		role = "preflight"
	case strings.Contains(target, "speed.cloudflare.com/__down"):
		role = "fallback"
	}
	record := speedtestCurlFixtureRecord{
		PID: os.Getpid(), MaxTime: value, MaxCount: count, WorkMS: work.Milliseconds(), Role: role,
	}
	data, err := json.Marshal(record)
	if err != nil {
		return 92
	}
	path := filepath.Join(os.Getenv("FREENET_SPEEDTEST_CURL_RECORDS"), strconv.Itoa(record.PID)+".json")
	if err := os.WriteFile(path+".tmp", data, 0600); err != nil {
		return 93
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		return 94
	}
	mode := os.Getenv("FREENET_SPEEDTEST_CURL_MODE")
	if mode == "hang" {
		select {}
	}
	if role == "catalog" {
		fmt.Fprint(os.Stdout, `[{"url":"https://speed-one.invalid/upload.php"},{"url":"https://speed-two.invalid/upload.php"},{"url":"https://speed-three.invalid/upload.php"}]`)
		return 0
	}
	if role == "preflight" {
		fmt.Fprint(os.Stdout, "200\t256000")
		return 0
	}
	if mode == "alternate_partial" && role == "fallback" {
		fmt.Fprint(os.Stdout, "403\t500\t0.1\t0.2")
		return 0
	}
	if mode == "instant" {
		fmt.Fprint(os.Stdout, "200\t8000000\t0.2\t1.2")
		return 0
	}
	// curl's numeric writeout is produced on its own timeout path, after the
	// original equal-budget CommandContext would already have killed it.
	time.Sleep(work)
	time.Sleep(25 * time.Millisecond)
	bytes := 8_000_000
	if mode == "small_partial" {
		bytes--
	}
	fmt.Fprintf(os.Stdout, "200\t%d\t0.2\t1.2", bytes)
	fmt.Fprint(os.Stderr, "private fixture stderr must never enter numeric results")
	return 28
}

func newSpeedtestCurlFixture(t *testing.T, mode string) (string, string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Setenv("FREENET_SPEEDTEST_CURL_FIXTURE", "1")
	t.Setenv("FREENET_SPEEDTEST_CURL_RECORDS", dir)
	t.Setenv("FREENET_SPEEDTEST_CURL_MODE", mode)
	return executable, dir
}

func speedtestCurlRecords(t *testing.T, dir string) []speedtestCurlFixtureRecord {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	records := make([]speedtestCurlFixtureRecord, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var record speedtestCurlFixtureRecord
		if err := json.Unmarshal(data, &record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	return records
}

func waitSpeedtestCurlRecord(t *testing.T, dir string) speedtestCurlFixtureRecord {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if records := speedtestCurlRecords(t, dir); len(records) > 0 {
			return records[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("owned curl fixture did not start")
	return speedtestCurlFixtureRecord{}
}

func requireSpeedtestCurlReaped(t *testing.T, records []speedtestCurlFixtureRecord) {
	t.Helper()
	for _, record := range records {
		if err := syscall.Kill(record.PID, 0); !errors.Is(err, syscall.ESRCH) {
			t.Fatalf("curl PID %d was not gone after runner returned: %v", record.PID, err)
		}
	}
}

func requireSpeedtestPartialMetrics(t *testing.T, output []byte, err error) {
	t.Helper()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 28 {
		t.Fatalf("expected natural curl timeout 28, got %v", err)
	}
	mbps, ok := parseBestServerDownloadMbpsAtLeast(string(output), bestServerSpeedtestBytes/5)
	if !ok || mbps != 64 {
		t.Fatalf("useful partial writeout was lost: mbps=%v ok=%v output=%q", mbps, ok, output)
	}
	if strings.Contains(string(output), "private") {
		t.Fatal("stderr entered numeric writeout")
	}
}

func TestBestServerCurlOwnDeadlineRetainsPartialMetrics(t *testing.T) {
	executable, dir := newSpeedtestCurlFixture(t, "at_deadline")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	output, err := runBestServerSpeedtestCurlWithReserve(ctx, executable, 120*time.Millisecond, 350*time.Millisecond, "fixture-download")
	requireSpeedtestPartialMetrics(t, output, err)
	records := speedtestCurlRecords(t, dir)
	if len(records) != 1 || records[0].MaxTime != "0.120" || records[0].MaxCount != 1 {
		t.Fatalf("unexpected child budget: %+v", records)
	}
	requireSpeedtestCurlReaped(t, records)
}

func TestBestServerCurlRemainingParentBudgetKeepsFinishReserve(t *testing.T) {
	executable, dir := newSpeedtestCurlFixture(t, "at_deadline")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	output, err := runBestServerSpeedtestCurl(ctx, executable, bestServerSpeedtestRunTimeout, "fixture-download")
	requireSpeedtestPartialMetrics(t, output, err)
	records := speedtestCurlRecords(t, dir)
	if len(records) != 1 || records[0].WorkMS <= 0 || records[0].WorkMS > 1000 {
		t.Fatalf("curl did not fit inside parent minus the 1s reserve: %+v", records)
	}
	if ctx.Err() != nil {
		t.Fatalf("normal curl writeout consumed the hard parent deadline: %v", ctx.Err())
	}
	requireSpeedtestCurlReaped(t, records)
}

func TestBestServerCurlDoesNotStartWithoutPositiveBudget(t *testing.T) {
	for _, mode := range []string{"parent-canceled", "parent-no-reserve", "submillisecond", "zero-work", "zero-reserve"} {
		t.Run(mode, func(t *testing.T) {
			executable, dir := newSpeedtestCurlFixture(t, "instant")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			work, reserve := time.Second, bestServerCurlCompletionReserve
			switch mode {
			case "parent-canceled":
				cancel()
			case "parent-no-reserve":
				var timeoutCancel context.CancelFunc
				ctx, timeoutCancel = context.WithTimeout(ctx, 100*time.Millisecond)
				defer timeoutCancel()
			case "submillisecond":
				work = 500 * time.Microsecond
			case "zero-work":
				work = 0
			case "zero-reserve":
				reserve = 0
			}
			output, err := runBestServerSpeedtestCurlWithReserve(ctx, executable, work, reserve, "fixture-download")
			if err == nil || len(output) != 0 || len(speedtestCurlRecords(t, dir)) != 0 {
				t.Fatalf("invalid/expired window started a child: err=%v output=%q", err, output)
			}
		})
	}
	t.Run("small-positive-never-zero", func(t *testing.T) {
		executable, dir := newSpeedtestCurlFixture(t, "instant")
		output, err := runBestServerSpeedtestCurlWithReserve(context.Background(), executable, time.Millisecond, 350*time.Millisecond, "fixture-download")
		if err != nil || len(output) == 0 {
			t.Fatalf("small positive bounded request failed: %v", err)
		}
		records := speedtestCurlRecords(t, dir)
		if len(records) != 1 || records[0].MaxTime != "0.001" {
			t.Fatalf("small budget became curl's unlimited zero: %+v", records)
		}
		requireSpeedtestCurlReaped(t, records)
	})
}

func TestBestServerCurlParentCancellationReapsChild(t *testing.T) {
	executable, dir := newSpeedtestCurlFixture(t, "hang")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := runBestServerSpeedtestCurl(ctx, executable, 5*time.Second, "fixture-download")
		done <- err
	}()
	record := waitSpeedtestCurlRecord(t, dir)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("parent cancellation became success")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canceled child was not reaped")
	}
	requireSpeedtestCurlReaped(t, []speedtestCurlFixtureRecord{record})
}

func TestBestServerCurlHardParentBudgetIsNotExtended(t *testing.T) {
	executable, dir := newSpeedtestCurlFixture(t, "hang")
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	started := time.Now()
	output, err := runBestServerSpeedtestCurlWithReserve(ctx, executable, 5*time.Second, 150*time.Millisecond, "fixture-download")
	if err == nil || len(output) != 0 || time.Since(started) > 1500*time.Millisecond {
		t.Fatalf("parent budget was replaced by a fresh child work window: err=%v output=%q", err, output)
	}
	records := speedtestCurlRecords(t, dir)
	if len(records) != 1 || records[0].WorkMS <= 0 || records[0].WorkMS > 450 {
		t.Fatalf("child escaped remaining parent budget: %+v", records)
	}
	requireSpeedtestCurlReaped(t, records)
}

func TestBestServerCurlCompletionReserveReapsUnresponsiveChild(t *testing.T) {
	executable, dir := newSpeedtestCurlFixture(t, "hang")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	output, err := runBestServerSpeedtestCurlWithReserve(ctx, executable, 120*time.Millisecond, 350*time.Millisecond, "fixture-download")
	if !errors.Is(err, context.DeadlineExceeded) || len(output) != 0 {
		t.Fatalf("unresponsive child did not terminate as bounded unknown: %v %q", err, output)
	}
	if ctx.Err() != nil {
		t.Fatal("only the outer parent deadline terminated the hung child")
	}
	records := speedtestCurlRecords(t, dir)
	if len(records) != 1 {
		t.Fatalf("expected one owned child: %+v", records)
	}
	requireSpeedtestCurlReaped(t, records)
}

func TestBestServerSpeedtestLeavesUseCompletionRunner(t *testing.T) {
	t.Run("catalog", func(t *testing.T) {
		executable, dir := newSpeedtestCurlFixture(t, "instant")
		servers, issue := discoverBestServerSpeedtestServers(context.Background(), executable, "unused-socks")
		records := speedtestCurlRecords(t, dir)
		if len(servers) != 3 || issue != "" || len(records) != 1 || records[0].Role != "catalog" || records[0].MaxTime != "8.000" {
			t.Fatalf("catalog did not use bounded leaf runner: servers=%d issue=%q records=%+v", len(servers), issue, records)
		}
		requireSpeedtestCurlReaped(t, records)
	})
	t.Run("preflight", func(t *testing.T) {
		executable, dir := newSpeedtestCurlFixture(t, "instant")
		server := bestServerSpeedtestServer{URL: "https://speed-one.invalid/upload.php"}
		qualified := qualifyBestServerSpeedtestServers(context.Background(), executable, "unused-socks", []bestServerSpeedtestServer{server})
		records := speedtestCurlRecords(t, dir)
		if len(qualified) != 1 || len(records) != 1 || records[0].Role != "preflight" || records[0].MaxTime != "4.000" {
			t.Fatalf("preflight did not use bounded leaf runner: records=%+v", records)
		}
		requireSpeedtestCurlReaped(t, records)
	})
	for _, mode := range []string{"fallback_partial", "alternate_partial"} {
		t.Run(mode, func(t *testing.T) {
			executable, dir := newSpeedtestCurlFixture(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			var speeds []float64
			var issue string
			if mode == "fallback_partial" {
				speeds, issue = probeBestServerFallbackConcurrentSized(ctx, executable, "unused-socks", bestServerMediaChunkRuns, bestServerSpeedtestBytes, 120*time.Millisecond)
			} else {
				speeds, issue = probeBestServerSpeedtestConcurrentSized(ctx, executable, "unused-socks", bestServerMediaChunkRuns, bestServerSpeedtestBytes, 120*time.Millisecond)
			}
			if len(speeds) != bestServerMediaChunkRuns || bestServerAggregateThroughputMbps(speeds) != 256 || issue != "" {
				t.Fatalf("actual throughput leaf lost timed-out partial metrics: speeds=%v issue=%q", speeds, issue)
			}
			records := speedtestCurlRecords(t, dir)
			roles := map[string]int{}
			for _, record := range records {
				if record.MaxCount != 1 || record.WorkMS <= 0 {
					t.Fatalf("unbounded/duplicate curl max-time: %+v", record)
				}
				roles[record.Role]++
				if (record.Role == "fallback" || record.Role == "download") && record.MaxTime != "0.120" {
					t.Fatalf("actual transfer did not use the work budget: %+v", record)
				}
			}
			if roles["fallback"] != bestServerMediaChunkRuns {
				t.Fatalf("missing actual fallback path: %v", roles)
			}
			if mode == "alternate_partial" && (roles["catalog"] != 1 || roles["preflight"] != 3 || roles["download"] != bestServerMediaChunkRuns) {
				t.Fatalf("alternate path did not traverse catalog/preflight/download: %v", roles)
			}
			requireSpeedtestCurlReaped(t, records)
		})
	}
}

func TestBestServerSpeedtestTooSmallPartialMetricsStayRejected(t *testing.T) {
	executable, dir := newSpeedtestCurlFixture(t, "small_partial")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	speeds, issue := probeBestServerFallbackConcurrentSized(ctx, executable, "unused-socks", bestServerMediaChunkRuns, bestServerSpeedtestBytes, 120*time.Millisecond)
	if len(speeds) != 0 || !strings.Contains(issue, "HTTP 200; curl 28; получено 7999999 байт") {
		t.Fatalf("completion reserve relaxed the minimum useful body gate: speeds=%v issue=%q", speeds, issue)
	}
	records := speedtestCurlRecords(t, dir)
	if len(records) != bestServerMediaChunkRuns {
		t.Fatalf("expected four bounded failed transfers: %+v", records)
	}
	requireSpeedtestCurlReaped(t, records)
}
