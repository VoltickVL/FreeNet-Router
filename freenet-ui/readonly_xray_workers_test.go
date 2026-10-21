package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

type readonlyWorkerRecord struct {
	PID        int
	Arg0       string
	Comm       string
	Probe      bool
	Dir        string
	URLInArgs  bool
	URLInStdin bool
}

// Execute the test binary itself through the production alias, without a
// shell wrapper that would change comm or introduce an unowned descendant.
// The parent test sets this flag only after its own package initialization.
func init() {
	if os.Getenv("FREENET_READONLY_WORKER_FIXTURE") == "1" {
		os.Exit(readonlyWorkerFixtureMain())
	}
}

func readonlyWorkerFixtureMain() int {
	go func() {
		time.Sleep(10 * time.Second)
		os.Exit(90) // A failing fixture must not leave an indefinite worker.
	}()
	args := os.Args[1:]
	value := func(flag string) string {
		for i := 0; i+1 < len(args); i++ {
			if args[i] == flag {
				return args[i+1]
			}
		}
		return ""
	}
	role := "run"
	if filepath.Base(os.Args[0]) == "curl" {
		role = "curl"
	} else if len(args) == 1 && args[0] == "version" {
		role = "version"
	} else {
		for _, arg := range args {
			if arg == "-test" {
				role = "test"
			}
		}
	}
	comm, err := os.ReadFile("/proc/self/comm")
	if err != nil {
		return 91
	}
	record := readonlyWorkerRecord{
		PID: os.Getpid(), Arg0: os.Args[0], Comm: strings.TrimSpace(string(comm)),
		Probe: os.Getenv("FREENET_XRAY_PROBE") == "1", Dir: filepath.Dir(os.Args[0]),
	}
	for _, arg := range args {
		record.URLInArgs = record.URLInArgs || strings.Contains(arg, "private-token")
	}
	var listener net.Listener
	if role == "test" || role == "run" {
		record.Dir = value("-confdir")
		data, readErr := os.ReadFile(filepath.Join(record.Dir, "00_fetch.json"))
		var config struct {
			Inbounds []struct {
				Listen string
				Port   int
			}
		}
		if readErr != nil || json.Unmarshal(data, &config) != nil || len(config.Inbounds) != 1 || config.Inbounds[0].Listen != "127.0.0.1" {
			return 92
		}
		if role == "run" {
			listener, err = net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(config.Inbounds[0].Port)))
			if err != nil {
				return 93
			}
			defer listener.Close()
		}
	}
	if role == "curl" {
		record.Dir = ""
		data, readErr := io.ReadAll(io.LimitReader(os.Stdin, 4096))
		if readErr != nil {
			return 94
		}
		record.URLInStdin = strings.Contains(string(data), "private-token")
		socks := value("--socks5-hostname")
		host, _, splitErr := net.SplitHostPort(socks)
		if splitErr != nil || host != "127.0.0.1" {
			return 95
		}
		conn, dialErr := net.DialTimeout("tcp", socks, time.Second)
		if dialErr != nil {
			return 96
		}
		_ = conn.Close()
	}
	recordPath := filepath.Join(os.Getenv("FREENET_READONLY_WORKER_RECORDS"), role+".json")
	encoded, err := json.Marshal(record)
	if err != nil || os.WriteFile(recordPath+".tmp", encoded, 0600) != nil || os.Rename(recordPath+".tmp", recordPath) != nil {
		return 97
	}
	if os.Getenv("FREENET_READONLY_WORKER_BLOCK") == role {
		for {
			if _, err := os.Stat(recordPath + ".release"); err == nil {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	switch role {
	case "version":
		fmt.Println("Xray 26.9.9")
	case "test":
		return 0
	case "curl":
		fmt.Println("subscription-body")
	case "run":
		for {
			conn, err := listener.Accept()
			if err != nil {
				return 98
			}
			_ = conn.Close()
		}
	}
	return 0
}

func setupReadonlyWorkerFixture(t *testing.T, block string) (*app, string, string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the router process-identity contract uses Linux procfs")
	}
	dir := t.TempDir()
	records := filepath.Join(dir, "records")
	tmp := filepath.Join(dir, "tmp")
	for _, path := range []string{records, tmp} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"xray", "curl"} {
		if err := os.Symlink(executable, filepath.Join(dir, role)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("FREENET_READONLY_WORKER_FIXTURE", "1")
	t.Setenv("FREENET_READONLY_WORKER_RECORDS", records)
	t.Setenv("FREENET_READONLY_WORKER_BLOCK", block)
	t.Setenv("FREENET_XRAY_BIN", filepath.Join(dir, "xray"))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMPDIR", tmp)
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	outPath := filepath.Join(dir, "04_outbounds.json")
	out := `{"outbounds":[{"tag":"vless-reality","protocol":"vless","settings":{"vnext":[{"address":"127.0.0.1","port":443}]}}]}`
	if err := os.WriteFile(outPath, []byte(out), 0600); err != nil {
		t.Fatal(err)
	}
	return &app{cfg: config{OutPath: outPath, GeoDataDir: dir}}, records, tmp
}

func waitReadonlyWorkerRecord(t *testing.T, records, role string) readonlyWorkerRecord {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(filepath.Join(records, role+".json"))
		var record readonlyWorkerRecord
		if err == nil && json.Unmarshal(data, &record) == nil {
			return record
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("owned %s worker did not reach the fixture barrier", role)
	return readonlyWorkerRecord{}
}

func assertReadonlyWorkerIsolation(t *testing.T, record readonlyWorkerRecord) {
	t.Helper()
	if !record.Probe || record.Comm != isolatedXrayProbeName || filepath.Base(record.Arg0) != isolatedXrayProbeName {
		t.Fatalf("read-only Xray was not isolated: %+v", record)
	}
}

func assertReadonlyWorkerLease(t *testing.T) {
	t.Helper()
	if len(isolatedXrayProbeGate) != 1 {
		t.Fatalf("worker owns %d probe slots, want exactly one", len(isolatedXrayProbeGate))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	release, ok := acquireIsolatedXrayProbe(ctx)
	if !ok {
		t.Fatal("one worker consumed both shared slots")
	}
	release()
	cutoverCtx, cancelCutover := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancelCutover()
	if release, ok := acquireExclusiveIsolatedXrayProbe(cutoverCtx); ok {
		release()
		t.Fatal("exclusive cutover overlapped a live read-only worker")
	}
}

func readonlyWorkerCutoverAfterCleanup(t *testing.T, records []readonlyWorkerRecord) <-chan error {
	t.Helper()
	result := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		release, ok := acquireExclusiveIsolatedXrayProbe(ctx)
		if !ok {
			result <- fmt.Errorf("exclusive cutover did not resume after worker exit")
			return
		}
		defer release()
		for _, record := range records {
			if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(record.PID))); !os.IsNotExist(err) {
				result <- fmt.Errorf("probe slot released before PID %d was reaped: %v", record.PID, err)
				return
			}
			if record.Dir != "" {
				if _, err := os.Stat(record.Dir); !os.IsNotExist(err) {
					result <- fmt.Errorf("probe slot released before temporary directory cleanup: %v", err)
					return
				}
			}
		}
		result <- nil
	}()
	// Wait until the exclusive caller owns the remaining slot and is blocked
	// on this live worker. Cancellation then exercises the actual handoff.
	deadline := time.Now().Add(time.Second)
	for len(isolatedXrayProbeGate) != isolatedXrayProbeLimit {
		if time.Now().After(deadline) {
			t.Fatal("exclusive cutover did not reach the active worker barrier")
		}
		time.Sleep(time.Millisecond)
	}
	return result
}

func TestReadonlyXrayWorkerLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name   string
		api    string
		block  string
		cancel bool
	}{
		{"version_success", "version", "version", false},
		{"version_cancel", "version", "version", true},
		{"subscription_validation_cancel", "subscription", "test", true},
		{"subscription_fetch_cancel", "subscription", "curl", true},
		{"subscription_success", "subscription", "curl", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, records, tmp := setupReadonlyWorkerFixture(t, tc.block)
			if tc.api == "version" {
				// Preserve exec.Command's original PATH lookup semantics.
				t.Setenv("FREENET_XRAY_BIN", "xray")
			}
			before, err := os.ReadFile(a.cfg.OutPath)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			done := make(chan struct{})
			var data string
			var callErr error
			go func() {
				defer close(done)
				if tc.api == "version" {
					data = a.configStudioXrayStatus(ctx).Version
				} else {
					body, err := a.fetchSubscriptionBodyViaActiveVPN(ctx, &url.URL{Scheme: "https", Host: "subscription.invalid", Path: "/private-token"})
					data, callErr = string(body), err
				}
			}()
			defer func() {
				cancel()
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Error("owned worker did not terminate after cancellation")
				}
			}()
			active := waitReadonlyWorkerRecord(t, records, tc.block)
			workers := []readonlyWorkerRecord{active}
			if tc.block == "curl" {
				for _, role := range []string{"test", "run"} {
					record := waitReadonlyWorkerRecord(t, records, role)
					assertReadonlyWorkerIsolation(t, record)
					workers = append(workers, record)
				}
				if active.URLInArgs || !active.URLInStdin {
					t.Fatal("subscription URL must remain on curl stdin only")
				}
			} else {
				assertReadonlyWorkerIsolation(t, active)
			}
			assertReadonlyWorkerLease(t)
			cutover := readonlyWorkerCutoverAfterCleanup(t, workers)
			if tc.cancel {
				cancel()
			} else if err := os.WriteFile(filepath.Join(records, tc.block+".json.release"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("read-only operation did not finish")
			}
			if err := <-cutover; err != nil {
				t.Fatal(err)
			}
			if tc.cancel {
				if data != "" || (tc.api == "subscription" && callErr == nil) {
					t.Fatalf("canceled worker produced success: data=%q err=%v", data, callErr)
				}
			} else {
				want := "26.9.9"
				if tc.api == "subscription" {
					want = "subscription-body\n"
				}
				if callErr != nil || data != want {
					t.Fatalf("successful worker result=%q err=%v", data, callErr)
				}
			}
			entries, err := os.ReadDir(tmp)
			if err != nil || len(entries) != 0 {
				t.Fatalf("read-only staging was not cleaned: count=%d err=%v", len(entries), err)
			}
			after, err := os.ReadFile(a.cfg.OutPath)
			if err != nil || string(before) != string(after) {
				t.Fatal("read-only worker changed the live outbound")
			}
		})
	}
}

func TestReadonlyXrayWorkersGateWaitUsesCallerDeadline(t *testing.T) {
	for _, api := range []string{"version", "subscription"} {
		t.Run(api, func(t *testing.T) {
			a, records, tmp := setupReadonlyWorkerFixture(t, "")
			gateCtx, cancelGate := context.WithTimeout(context.Background(), time.Second)
			defer cancelGate()
			release, ok := acquireExclusiveIsolatedXrayProbe(gateCtx)
			if !ok {
				t.Fatal("cannot reserve the cutover gate")
			}
			defer release()
			ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
			defer cancel()
			start := time.Now()
			if api == "version" {
				if result := a.configStudioXrayStatus(ctx); result.Version != "" {
					t.Fatal("version worker started during exclusive cutover")
				}
			} else if _, err := a.fetchSubscriptionBodyViaActiveVPN(ctx, &url.URL{Scheme: "https", Host: "subscription.invalid", Path: "/private-token"}); err == nil {
				t.Fatal("subscription worker started during exclusive cutover")
			}
			if ctx.Err() != context.DeadlineExceeded || time.Since(start) > time.Second {
				t.Fatal("waiting for a slot restarted or ignored the caller budget")
			}
			for _, dir := range []string{records, tmp} {
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 0 {
					t.Fatalf("blocked worker spawned or staged work: count=%d err=%v", len(entries), err)
				}
			}
		})
	}
}
