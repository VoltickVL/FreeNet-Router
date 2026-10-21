package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

type readinessFixtureCall struct {
	Origin  string `json:"origin"`
	Format  string `json:"format"`
	MaxTime string `json:"max_time"`
}

// Only the external Xray/curl processes are fixtures. The tests exercise the
// real probeBestServerApplication -> HTTP runner/parser -> readiness gate.
// The curl fixture supports both legacy time_total and canonical -w formats,
// so the baseline fails for its decision, not because its format is rejected.
func TestFreshEndpointReadinessHelperProcess(t *testing.T) {
	if os.Getenv("FREENET_READINESS_HELPER_PROCESS") != "1" {
		return
	}
	var args []string
	for i, arg := range os.Args {
		if arg == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	if len(args) == 0 {
		os.Exit(64)
	}
	role, args := args[0], args[1:]
	value := func(flag string) string {
		for i := 0; i+1 < len(args); i++ {
			if args[i] == flag {
				return args[i+1]
			}
		}
		return ""
	}
	if role == "xray" {
		data, err := os.ReadFile(filepath.Join(value("-confdir"), "00_probe.json"))
		var cfg struct {
			Inbounds []struct {
				Listen string `json:"listen"`
				Port   int    `json:"port"`
			} `json:"inbounds"`
		}
		if err != nil || json.Unmarshal(data, &cfg) != nil || len(cfg.Inbounds) != 1 || cfg.Inbounds[0].Listen != "127.0.0.1" {
			os.Exit(65)
		}
		for _, arg := range args {
			if arg == "-test" {
				os.Exit(0)
			}
		}
		listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Inbounds[0].Port)))
		if err != nil {
			os.Exit(66)
		}
		for {
			conn, err := listener.Accept()
			if err != nil {
				os.Exit(67)
			}
			_ = conn.Close()
		}
	}
	if role != "curl" || len(args) == 0 {
		os.Exit(64)
	}
	// No request can leave loopback, even though the fixture receives the real
	// production origin arguments and the real isolated SOCKS listener address.
	socks := value("--socks5-hostname")
	host, _, err := net.SplitHostPort(socks)
	if err != nil || host != "127.0.0.1" {
		os.Exit(68)
	}
	conn, err := net.DialTimeout("tcp", socks, time.Second)
	if err != nil {
		os.Exit(69)
	}
	_ = conn.Close()
	origin := ""
	switch args[len(args)-1] {
	case bestServerApplicationProbeURLs[0]:
		origin = "primary"
	case bestServerApplicationProbeURLs[1]:
		origin = "secondary"
	default:
		os.Exit(70)
	}
	logPath := os.Getenv("FREENET_READINESS_CALLS")
	previous, _ := os.ReadFile(logPath)
	call := readinessFixtureCall{Origin: origin, Format: value("-w"), MaxTime: value("--max-time")}
	line, _ := json.Marshal(call)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(71)
	}
	_, _ = f.Write(append(line, '\n'))
	_ = f.Close()
	code, pretransfer, starttransfer, total := "204", "0.600", "0.760", "0.800"
	switch os.Getenv("FREENET_READINESS_SCENARIO") {
	case "fallback":
		if origin == "primary" {
			os.Exit(28)
		}
	case "both_failed":
		os.Exit(28)
	case "partial":
		if len(previous) != 0 {
			os.Exit(28)
		}
	case "canceled":
		time.Sleep(30 * time.Second) // The inherited caller context must kill us.
		os.Exit(72)
	case "above_ceiling":
		starttransfer, total = "0.821", "0.850"
	case "bad_status":
		code, pretransfer, starttransfer, total = "403", "0.020", "0.180", "0.180"
	}
	switch call.Format {
	case "%{http_code}\t%{time_total}":
		fmt.Printf("%s\t%s", code, total)
	case "%{http_code}\t%{time_pretransfer}\t%{time_starttransfer}":
		fmt.Printf("%s\t%s\t%s", code, pretransfer, starttransfer)
	default:
		os.Exit(73)
	}
	os.Exit(0)
}

func setupReadinessProcessFixture(t *testing.T, scenario string) (*app, bestServerInternalCandidate, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("FREENET_READINESS_HELPER_PROCESS", "1")
	t.Setenv("FREENET_READINESS_SCENARIO", scenario)
	logPath := filepath.Join(dir, "calls.jsonl")
	t.Setenv("FREENET_READINESS_CALLS", logPath)
	t.Setenv("TMPDIR", dir)
	// A race-instrumented subprocess must not spend a second sleeping at exit.
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"xray", "curl"} {
		path := filepath.Join(dir, role)
		script := "#!/bin/sh\nexec " + v3ShellQuote(executable) + " -test.run=^TestFreshEndpointReadinessHelperProcess$ -- " + role + " \"$@\"\n"
		if err := os.WriteFile(path, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("FREENET_XRAY_BIN", filepath.Join(dir, "xray"))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	raw := "vless://11111111-1111-4111-8111-111111111111@" + listener.Addr().String() +
		"?type=tcp&security=tls&sni=example.test#DE%20Frankfurt%2C%20Germany%2C%20Extra"
	profile, ok := parseSafeVLESSProfile(raw)
	if !ok {
		t.Fatal("fixture profile was not accepted")
	}
	return &app{cfg: config{GeoDataDir: dir}, sem: make(chan struct{}, 1)}, bestServerInternalCandidate{Profile: profile, Raw: raw}, logPath
}

func readinessFixtureCalls(t *testing.T, path string) []readinessFixtureCall {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var calls []readinessFixtureCall
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var call readinessFixtureCall
		if err := json.Unmarshal([]byte(line), &call); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, call)
	}
	return calls
}

func readinessCancelOnFirstRequest(ctx context.Context, cancel context.CancelFunc, path string) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if info, err := os.Stat(path); err == nil && info.Size() > 0 {
					cancel()
					return
				}
			}
		}
	}()
	return done
}

func TestFreshEndpointReadinessCanonicalProductionPath(t *testing.T) {
	for _, tc := range []struct {
		name      string
		eligible  bool
		available bool
		samples   int
		ms        int
		origins   string
	}{
		{"slow_setup", true, true, 2, 160, "primary,primary"},
		{"fallback", true, true, 2, 160, "primary,secondary,primary,secondary"},
		{"both_failed", false, false, 0, 0, "primary,secondary,primary,secondary"},
		{"partial", false, false, 1, 0, "primary,primary,secondary"},
		{"canceled", false, false, 0, 0, "primary"},
		{"above_ceiling", false, true, 2, 221, "primary,primary"},
		{"bad_status", false, false, 0, 0, "primary,secondary,primary,secondary"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, candidate, logPath := setupReadinessProcessFixture(t, tc.name)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if tc.name == "canceled" {
				done := readinessCancelOnFirstRequest(ctx, cancel, logPath)
				defer func() { cancel(); <-done }()
			}
			started := time.Now()
			got := a.probeBestServerFreshEndpointReadiness(ctx, candidate)
			if got == nil || got.Eligible != tc.eligible || got.Available != tc.available || got.HTTPSamples != tc.samples || got.ApplicationMS != tc.ms {
				t.Fatalf("readiness=%+v want eligible=%v available=%v samples=%d HTTP RTT=%d", got, tc.eligible, tc.available, tc.samples, tc.ms)
			}
			if got.Tested != (tc.name != "canceled") {
				t.Fatalf("tested=%v for %s", got.Tested, tc.name)
			}
			if got.DownloadMbps != 0 || got.MediaSamples != 0 {
				t.Fatalf("lightweight readiness must not run Speedtest/media: %+v", got)
			}
			if tc.name == "canceled" && time.Since(started) >= 2*time.Second {
				t.Fatal("readiness escaped the canceled caller budget")
			}
			var origins []string
			for _, call := range readinessFixtureCalls(t, logPath) {
				origins = append(origins, call.Origin)
				seconds, err := strconv.ParseFloat(call.MaxTime, 64)
				if err != nil || seconds <= 0 || seconds > bestServerApplicationProbePerTargetTimeout.Seconds() {
					t.Fatalf("per-origin curl budget=%q", call.MaxTime)
				}
			}
			if strings.Join(origins, ",") != tc.origins {
				t.Fatalf("origins=%v want=%s", origins, tc.origins)
			}
		})
	}
}

func TestFreshEndpointRejectedProductionProbeDoesNotApply(t *testing.T) {
	for _, scenario := range []string{"both_failed", "partial", "canceled", "above_ceiling", "bad_status"} {
		t.Run(scenario, func(t *testing.T) {
			a, fresh, logPath := setupReadinessProcessFixture(t, scenario)
			dir := filepath.Dir(logPath)
			a.cfg.OutPath = filepath.Join(dir, "04_outbounds.json")
			a.cfg.FilterPath = filepath.Join(dir, "profile.filter")
			a.cfg.SubPath = filepath.Join(dir, "subscription.url")
			a.cfg.ConfigPath = filepath.Join(dir, "freenet.conf")
			t.Setenv("FREENET_PROFILE_FILE", filepath.Join(dir, "vpn_profile_name"))
			t.Setenv("FREENET_AUTOMATION_STATE", filepath.Join(dir, "automation.state"))
			t.Setenv("FREENET_PROVIDER_SUBSCRIPTION_CACHE", filepath.Join(dir, "provider.lkg"))
			t.Setenv("FREENET_PROVIDER_SUBSCRIPTION_SOURCE", filepath.Join(dir, "provider.source"))
			applyMarker := filepath.Join(dir, "must-not-apply")
			provider := filepath.Join(dir, "provider")
			if err := os.WriteFile(provider, []byte("#!/bin/sh\nprintf called > "+v3ShellQuote(applyMarker)+"\nexit 1\n"), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("FREENET_PROVIDER_HELPER", provider)
			current := fresh.Profile
			current.Port = 1 // A different old endpoint, confined to loopback.
			outbound, err := buildBestServerProbeOutbound(fresh.Raw, current)
			if err != nil {
				t.Fatal(err)
			}
			out, err := json.Marshal(map[string]any{"outbounds": []any{outbound}})
			if err != nil {
				t.Fatal(err)
			}
			before := map[string][]byte{
				a.cfg.OutPath:                     out,
				a.cfg.FilterPath:                  []byte(regexp.QuoteMeta(fresh.Profile.Name)),
				os.Getenv("FREENET_PROFILE_FILE"): []byte(fresh.Profile.Name),
			}
			for path, data := range before {
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(a.cfg.SubPath, []byte("https://example.test/subscription"), 0600); err != nil {
				t.Fatal(err)
			}
			oldFetch := directSubscriptionBodyFetch
			t.Cleanup(func() { directSubscriptionBodyFetch = oldFetch })
			// Only catalog retrieval is substituted; the changed readiness path
			// still executes the real curl runner and canonical response parser.
			directSubscriptionBodyFetch = func(context.Context, *url.URL) ([]byte, error) {
				return []byte(fresh.Raw + "\n"), nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if scenario == "canceled" {
				done := readinessCancelOnFirstRequest(ctx, cancel, logPath)
				defer func() { cancel(); <-done }()
			}
			status, got := a.executeBestServerCurrentRefresh(ctx)
			if got.Applied || got.Mutation != "NONE" || got.Outcome != "check_failed" || got.Candidate == nil || got.Candidate.Eligible {
				t.Fatalf("rejected probe crossed apply gate: HTTP=%d response=%+v", status, got)
			}
			if status != http.StatusOK && status != http.StatusGatewayTimeout {
				t.Fatalf("unexpected rejection status=%d", status)
			}
			if _, err := os.Stat(applyMarker); !os.IsNotExist(err) {
				t.Fatalf("provider helper was invoked after rejected probe: %v", err)
			}
			for path, want := range before {
				got, err := os.ReadFile(path)
				if err != nil || string(got) != string(want) {
					t.Fatalf("live VPN file changed after rejected readiness: %s", filepath.Base(path))
				}
			}
			if len(readinessFixtureCalls(t, logPath)) == 0 {
				t.Fatal("test did not reach the production HTTP probe")
			}
		})
	}
}
