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
		validation = `case "$0" in
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
	targetFailsInstalled bool
	xkeenExit            int
	wantSuccess          bool
	wantRollback         string
	wantCurrent          string
	wantErrorContains    string
	wantRestartCalls     int
	wantStopEvent        bool
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
			wantRestartCalls:     1,
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
			if err := os.WriteFile(binary, xrayCoreFixtureScript(previous, false), 0755); err != nil {
				t.Fatal(err)
			}

			restartLog := filepath.Join(root, "xkeen-restarts.log")
			xkeenPath := filepath.Join(root, "xkeen")
			xkeen := fmt.Sprintf("#!/bin/sh\nprintf 'restart\\n' >> %q\nexit %d\n", restartLog, scenario.xkeenExit)
			if err := os.WriteFile(xkeenPath, []byte(xkeen), 0755); err != nil {
				t.Fatal(err)
			}

			configPath := filepath.Join(root, "freenet.conf")
			if err := os.WriteFile(configPath, []byte("ISP_ID=auto\nDNS_MODE=auto\n"), 0600); err != nil {
				t.Fatal(err)
			}
			historyPath := filepath.Join(root, "history.log")
			t.Setenv("FREENET_XRAY_BIN", binary)
			t.Setenv("FREENET_ROUTING_CONFIG_DIR", configDir)
			t.Setenv("FREENET_SETTINGS_V3_HISTORY", historyPath)

			assetName, ok := xrayCoreAssetName(runtime.GOARCH)
			if !ok {
				t.Skipf("fixture architecture %s is not supported", runtime.GOARCH)
			}
			targetScript := xrayCoreFixtureScript(target, scenario.targetFailsInstalled)
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

			result := a.applyXrayCore(context.Background(), target)
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
