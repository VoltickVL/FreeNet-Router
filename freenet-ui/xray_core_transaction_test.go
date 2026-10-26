package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type xrayCoreRoundTripFunc func(*http.Request) (*http.Response, error)

func (f xrayCoreRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func xrayCoreFixtureScript(version string, failAfterInstall bool) []byte {
	validation := ""
	if failAfterInstall {
		// The runner now uses an isolated fn-xray-probe alias. Distinguish
		// the pre-install staged binary from the installed binary through its
		// symlink target rather than assuming the process is named xray.
		validation = `case "$(readlink "$0" 2>/dev/null || printf '%s' "$0")" in
  *".freenet-xray-candidate-"*) ;;
  *) exit 23 ;;
esac`
	}
	return []byte(fmt.Sprintf(`#!/bin/sh
if [ "$1" = "version" ]; then
  echo "Xray %s (FreeNet fixture)"
  exit 0
fi
if [ "$1" = "run" ]; then
%s
  exit 0
fi
exit 0
`, strings.TrimPrefix(version, "v"), validation))
}

func xrayCoreZipFixture(t *testing.T, script []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: "xray", Method: zip.Store}
	h.SetMode(0755)
	entry, err := zw.CreateHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(script); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func startNamedXrayProcess(t *testing.T, root string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("Xray runtime status uses /proc and is Linux-specific")
	}
	sleepPath, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(sleepPath)
	if err != nil {
		t.Fatal(err)
	}
	serviceDir := filepath.Join(root, "service")
	if err := os.MkdirAll(serviceDir, 0700); err != nil {
		t.Fatal(err)
	}
	servicePath := filepath.Join(serviceDir, "xray")
	if err := os.WriteFile(servicePath, data, 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(servicePath, "60")
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_CONFDIR="+filepath.Join(root, "configs"))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if processRunning("xray") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("fixture process is not visible as xray in /proc")
}

type xrayCoreScenario struct {
	name                 string
	badDigest            bool
	invalidCurrent       bool
	invalidCandidate     bool
	holdReadOnlyProbes    bool
	probeAdmissionTimeout bool
	xkeenUnavailable     bool
	targetFailsInstalled bool
	xkeenExit            int
	wantSuccess          bool
	wantRollback         string
	wantCurrent          string
	wantErrorContains    string
	wantRestartCalls     int
	wantStopEvent        bool
	cancelDuringRestart  bool
}

func TestXrayCoreTransactionalApply(t *testing.T) {
	const previous = "v26.9.9"
	const target = "v26.10.1"

	scenarios := []xrayCoreScenario{
		{
			name:              "integrity failure is no-mutation preflight",
			badDigest:         true,
			xkeenExit:         0,
			wantSuccess:       false,
			wantRollback:      "NOT_NEEDED",
			wantCurrent:       previous,
			wantErrorContains: "SHA-256 verification failed",
			wantRestartCalls:  0,
		},
		{
			name:              "native update does not repeat expensive live config test",
			invalidCurrent:    true,
			wantSuccess:       true,
			wantRollback:      "NOT_NEEDED",
			wantCurrent:       target,
			wantRestartCalls:  1,
		},
		{
			name:              "candidate reporting wrong version stops before replacement",
			invalidCandidate:  true,
			wantSuccess:       false,
			wantRollback:      "NOT_NEEDED",
			wantCurrent:       previous,
			wantErrorContains: "downloaded Xray binary does not match selected version",
			wantRestartCalls:  0,
		},
		{
			name:              "unavailable XKeen UI is no-mutation preflight",
			xkeenUnavailable:  true,
			wantSuccess:       false,
			wantRollback:      "NOT_NEEDED",
			wantCurrent:       previous,
			wantErrorContains: "native XKeen UI",
			wantRestartCalls:  0,
		},
		{
			name:               "read-only probe pool cannot delay a native core update",
			holdReadOnlyProbes: true,
			xkeenExit:          0,
			wantSuccess:        true,
			wantRollback:       "NOT_NEEDED",
			wantCurrent:        target,
			wantRestartCalls:   1,
		},
		{
			name:             "success installs target once",
			xkeenExit:        0,
			wantSuccess:      true,
			wantRollback:     "NOT_NEEDED",
			wantCurrent:      target,
			wantRestartCalls: 1,
		},
		{
			name:                 "apply failure rolls back previous core",
			targetFailsInstalled: true,
			xkeenExit:            0,
			wantSuccess:          false,
			wantRollback:         "SUCCESS",
			wantCurrent:          previous,
			wantErrorContains:    "предыдущая версия восстановлена",
			wantRestartCalls:     2,
		},
		{
			name:                 "browser disconnect cannot cancel successful native core update",
			cancelDuringRestart:  true,
			xkeenExit:            0,
			wantSuccess:          true,
			wantRollback:         "NOT_NEEDED",
			wantCurrent:          target,
			wantRestartCalls:     1,
		},
		{
			name:                 "rollback failure enters STOP",
			targetFailsInstalled: true,
			xkeenExit:            1,
			wantSuccess:          false,
			wantRollback:         "FAILED",
			wantCurrent:          previous,
			wantErrorContains:    "rollback failed",
			wantRestartCalls:     2,
			wantStopEvent:        true,
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			root := t.TempDir()
			startNamedXrayProcess(t, root)

			configDir := filepath.Join(root, "configs")
			if err := os.MkdirAll(configDir, 0700); err != nil {
				t.Fatal(err)
			}
			outPath := filepath.Join(configDir, "04_outbounds.json")
			outbound := `{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"198.51.100.10","port":443}]}},{"tag":"dns-out","settings":{}}]}`
			if err := os.WriteFile(outPath, []byte(outbound), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(configDir, "01_log.json"), []byte(`{"log":{}}`), 0600); err != nil {
				t.Fatal(err)
			}

			coreDir := filepath.Join(root, "core")
			if err := os.MkdirAll(coreDir, 0700); err != nil {
				t.Fatal(err)
			}
			binary := filepath.Join(coreDir, "xray")
			oldCore := xrayCoreFixtureScript(previous, false)
			if scenario.invalidCurrent {
				oldCore = []byte("#!/bin/sh\nif [ \"$1\" = \"version\" ]; then echo 'Xray 26.9.9'; exit 0; fi\nexit 27\n")
			}
			if err := os.WriteFile(binary, oldCore, 0755); err != nil {
				t.Fatal(err)
			}

			restartLog := filepath.Join(root, "xkeen-restarts.log")
			xkeenPath := filepath.Join(root, "xkeen")
			if err := os.WriteFile(xkeenPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil { t.Fatal(err) }
			restartCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if scenario.xkeenUnavailable {
					http.Error(w, "unavailable", http.StatusServiceUnavailable)
					return
				}
				switch {
				case req.URL.Path != "/api/control":
					http.NotFound(w, req)
				case req.Method == http.MethodGet:
					_, _ = io.WriteString(w, `{"success":true,"running":true,"currentCore":"xray"}`)
				case req.Method == http.MethodPost:
					restartCount++
					if err := os.WriteFile(restartLog, bytes.Repeat([]byte("restart\n"), restartCount), 0600); err != nil {
						t.Error(err)
					}
					if scenario.cancelDuringRestart && restartCount == 1 { time.Sleep(120 * time.Millisecond) }
					if scenario.xkeenExit != 0 {
						_, _ = io.WriteString(w, `{"success":false}`)
					} else {
						_, _ = io.WriteString(w, `{"success":true}`)
					}
				default:
					http.Error(w, "unsupported", http.StatusMethodNotAllowed)
				}
			}))
			t.Cleanup(server.Close)
			previousEndpoint := xrayCoreNativeControlEndpoint
			xrayCoreNativeControlEndpoint = server.URL + "/api/control"
			t.Cleanup(func() { xrayCoreNativeControlEndpoint = previousEndpoint })
			previousProof := xrayCoreNativeRuntimeProof
			xrayCoreNativeRuntimeProof = func(binary string) bool {
				testCtx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				current, err := xrayCoreVersionAt(testCtx, binary)
				if err != nil { return false }
				return !(scenario.targetFailsInstalled && current == target)
			}
			t.Cleanup(func(){ xrayCoreNativeRuntimeProof = previousProof })
			oldForwardTimeout, oldRollbackTimeout := xrayCoreApplyTimeout, xrayCoreRollbackTimeout
			xrayCoreApplyTimeout, xrayCoreRollbackTimeout = 450*time.Millisecond, 3*time.Second
			t.Cleanup(func(){ xrayCoreApplyTimeout, xrayCoreRollbackTimeout = oldForwardTimeout, oldRollbackTimeout })

			configPath := filepath.Join(root, "freenet.conf")
			if err := os.WriteFile(configPath, []byte("DNS_MODE=auto\n"), 0600); err != nil {
				t.Fatal(err)
			}
			historyPath := filepath.Join(root, "history.log")
			t.Setenv("FREENET_XRAY_BIN", binary)
			t.Setenv("FREENET_ROUTING_CONFIG_DIR", configDir)
			t.Setenv("FREENET_XRAY_PROBE_LOCK_FILE", filepath.Join(root, "probe.lock"))
			t.Setenv("FREENET_SETTINGS_V3_HISTORY", historyPath)

			assetName, ok := xrayCoreAssetName(runtime.GOARCH)
			if !ok {
				t.Skipf("fixture architecture %s is not supported", runtime.GOARCH)
			}
			targetScript := xrayCoreFixtureScript(target, scenario.targetFailsInstalled)
			if scenario.invalidCandidate {
				targetScript = xrayCoreFixtureScript("v26.10.0", false)
			}
			archive := xrayCoreZipFixture(t, targetScript)
			sum := sha256.Sum256(archive)
			digest := hex.EncodeToString(sum[:])
			if scenario.badDigest {
				digest = strings.Repeat("0", 64)
			}
			assetURL := "https://github.com/XTLS/Xray-core/releases/download/" + target + "/" + assetName
			catalogJSON := fmt.Sprintf(`[
  {"tag_name":%q,"published_at":"2026-09-15T00:00:00Z","prerelease":false,"draft":false,"body":"fixture target","assets":[{"name":%q,"browser_download_url":%q,"digest":%q,"size":%d}]},
  {"tag_name":%q,"published_at":"2026-09-09T00:00:00Z","prerelease":false,"draft":false,"body":"fixture current","assets":[]}
]`, target, assetName, assetURL, "sha256:"+digest, len(archive), previous)

			oldClient := xrayCoreHTTPClient
			xrayCoreHTTPClient = &http.Client{Transport: xrayCoreRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				var payload []byte
				switch {
				case req.URL.String() == xrayCoreReleasesURL:
					payload = []byte(catalogJSON)
				case req.URL.String() == assetURL:
					payload = archive
				default:
					return nil, fmt.Errorf("unexpected fixture URL: %s", req.URL.String())
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(payload)), Request: req}, nil
			})}
			t.Cleanup(func() { xrayCoreHTTPClient = oldClient })

			oldRunning := xrayServiceProcessRunning
			xrayServiceProcessRunning = func(string) bool { return true }
			t.Cleanup(func() { xrayServiceProcessRunning = oldRunning })

			a := &app{cfg: config{
				FilterPath: filepath.Join(root, "profile.regex"),
				OutPath:    outPath,
				GeoDataDir: filepath.Join(root, "assets"),
				XKeenPath:  xkeenPath,
				LockPath:   filepath.Join(root, "updater.lock"),
				ConfigPath: configPath,
				SubPath:    filepath.Join(root, "subscription.url"),
				UpdateLock: filepath.Join(root, "self-update.lock"),
			}, sem: make(chan struct{}, 1)}

			var result xrayCoreApplyResponse
			if scenario.cancelDuringRestart {
				requestCtx, cancelRequest := context.WithCancel(context.Background())
				resultCh := make(chan xrayCoreApplyResponse, 1)
				go func() { resultCh <- a.applyXrayCore(requestCtx, target) }()
				deadline := time.Now().Add(10 * time.Second)
				started := false
				for time.Now().Before(deadline) {
					if logBytes, err := os.ReadFile(restartLog); err == nil && len(logBytes) > 0 {
						started = true
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				cancelRequest()
				result = <-resultCh
				if !started {
					t.Fatal("fixture did not reach forward restart before cancellation")
				}
			} else if scenario.holdReadOnlyProbes {
				// Read-only quality probes do not own the core binary. Holding
				// their admission slots cannot delay a native XKeen UI update.
				var releases []func()
				for i := 0; i < isolatedXrayProbeLimit; i++ {
					release, ok := acquireIsolatedXrayProbe(context.Background())
					if !ok { t.Fatal("could not occupy probe pool") }
					releases = append(releases, release)
				}
				result = a.applyXrayCore(context.Background(), target)
				for _, release := range releases { release() }
			} else {
				result = a.applyXrayCore(context.Background(), target)
			}
			if result.Success != scenario.wantSuccess {
				t.Fatalf("success: got %v want %v; result=%+v", result.Success, scenario.wantSuccess, result)
			}
			if result.Rollback != scenario.wantRollback {
				t.Fatalf("rollback: got %q want %q; result=%+v", result.Rollback, scenario.wantRollback, result)
			}
			if result.CurrentVersion != scenario.wantCurrent {
				t.Fatalf("current version: got %q want %q; result=%+v", result.CurrentVersion, scenario.wantCurrent, result)
			}
			if scenario.wantErrorContains != "" && !strings.Contains(result.Error, scenario.wantErrorContains) {
				t.Fatalf("error %q does not contain %q", result.Error, scenario.wantErrorContains)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			installed, err := xrayCoreVersionAt(ctx, binary)
			cancel()
			if err != nil {
				t.Fatalf("read installed fixture version: %v", err)
			}
			if installed != scenario.wantCurrent {
				t.Fatalf("binary version: got %q want %q", installed, scenario.wantCurrent)
			}
			// Failed rollback must retain the original snapshot for further
			// controlled recovery. Confirmed rollback must clean it up.
			entries, err := os.ReadDir(coreDir)
			if err != nil { t.Fatal(err) }
			backups := 0
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".freenet-xray-backup-") {
					backups++
				}
			}
			if scenario.wantRollback == "FAILED" && backups != 1 {
				t.Fatalf("failed rollback must retain one old-core snapshot, got %d", backups)
			}
			if scenario.wantRollback != "FAILED" && backups != 0 {
				t.Fatalf("terminal core operation left %d unneeded backups", backups)
			}

			restarts := 0
			if data, err := os.ReadFile(restartLog); err == nil {
				restarts = len(strings.Fields(string(data)))
			} else if !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if restarts != scenario.wantRestartCalls {
				t.Fatalf("controlled restart calls: got %d want %d", restarts, scenario.wantRestartCalls)
			}

			if scenario.wantStopEvent {
				history, err := os.ReadFile(historyPath)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(history), "STOP") {
					t.Fatalf("rollback failure must record STOP, history=%q", string(history))
				}
			}
		})
	}
}
