package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestXrayCoreLivePreflightPreservesRelativeAndSymlinkedConfig(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "live-config")
	if err := os.MkdirAll(filepath.Join(configDir, "fragments"), 0700); err != nil { t.Fatal(err) }
	if err := os.MkdirAll(filepath.Join(configDir, "assets"), 0700); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(configDir, "fragments", "01_log.json"), []byte(`{"log":{}}`), 0600); err != nil { t.Fatal(err) }
	if err := os.Symlink(filepath.Join("fragments", "01_log.json"), filepath.Join(configDir, "01_log.json")); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(configDir, "04_outbounds.json"), []byte(`{"outbounds":[]}`), 0600); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(configDir, "assets", "custom.dat"), []byte("existing-relative-asset"), 0600); err != nil { t.Fatal(err) }

	// A deliberately restrictive mock: the old validation using only a copied
	// *.json subset would omit the symlink and relative asset, then fail.
	binary := filepath.Join(dir, "xray")
	script := `#!/bin/sh
if [ "$1" = "version" ]; then echo 'Xray 26.10.21'; exit 0; fi
[ "$1" = "run" ] && [ "$2" = "-test" ] && [ "$3" = "-confdir" ] || exit 11
[ "$4" = "$XRAY_TEST_LIVE_DIRECTORY" ] || exit 12
[ "$FREENET_XRAY_PROBE" = "1" ] || exit 13
[ -L "$4/01_log.json" ] || exit 14
[ -f "$4/fragments/01_log.json" ] || exit 15
[ -f "$4/assets/custom.dat" ] || exit 16
[ "$(basename "$0")" = "fn-xray-probe" ] || exit 17
exit 0
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil { t.Fatal(err) }
	t.Setenv("XRAY_TEST_LIVE_DIRECTORY", configDir)
	t.Setenv("FREENET_ROUTING_CONFIG_DIR", configDir)
	t.Setenv("FREENET_XRAY_PROBE_LOCK_FILE", filepath.Join(dir, "xray-probe.lock"))
	a := &app{cfg: config{OutPath: filepath.Join(configDir, "04_outbounds.json"), GeoDataDir: filepath.Join(dir, "dat")}}
	release, ok := acquireExclusiveIsolatedXrayProbe(context.Background())
	if !ok { t.Fatal("cannot own exclusive probe fence for semantic checks") }
	defer release()
	if err := a.validateXrayCoreBaseline(context.Background(), binary); err != nil {
		t.Fatalf("existing live Xray config rejected due to incomplete copy: %v", err)
	}
	if err := a.validateXrayCoreCandidate(context.Background(), binary, "v26.10.21"); err != nil {
		t.Fatalf("selected Xray core rejected identical live config: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(configDir, "assets", "custom.dat")); err != nil || string(data) != "existing-relative-asset" {
		t.Fatalf("read-only Xray validation changed live relative file: %v", err)
	}
	if info, err := os.Lstat(filepath.Join(configDir, "01_log.json")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("read-only validation changed live config symlink")
	}
}

func TestXrayCoreInvalidLiveConfigStopsWithoutExposingSecrets(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "configs")
	if err := os.MkdirAll(configDir, 0700); err != nil { t.Fatal(err) }
	binary := filepath.Join(dir, "xray")
	script := `#!/bin/sh
if [ "$1" = "version" ]; then echo 'Xray 26.9.9'; exit 0; fi
echo 'sensitive-token-that-must-not-leak' >&2
exit 27
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil { t.Fatal(err) }
	t.Setenv("FREENET_ROUTING_CONFIG_DIR", configDir)
	t.Setenv("FREENET_XRAY_PROBE_LOCK_FILE", filepath.Join(dir, "xray-probe.lock"))
	a := &app{cfg: config{OutPath: filepath.Join(configDir, "04_outbounds.json")}}
	release, ok := acquireExclusiveIsolatedXrayProbe(context.Background())
	if !ok { t.Fatal("cannot own exclusive probe fence") }
	defer release()
	err := a.validateXrayCoreBaseline(context.Background(), binary)
	if err == nil || !strings.Contains(err.Error(), "reason=xray-live-config-test-failed") {
		t.Fatalf("invalid live config must be STOP with classified reason, got %v", err)
	}
	if strings.Contains(err.Error(), "sensitive-token") || strings.Contains(err.Error(), configDir) {
		t.Fatal("raw Xray validation stderr or private filesystem path was exposed")
	}
}

func TestXrayCoreValidationGETIsReadOnlyAndDoesNotExposeSecretErrors(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "configs")
	if err := os.MkdirAll(configDir, 0700); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(configDir, "04_outbounds.json"), []byte(`{"outbounds":[]}`), 0600); err != nil { t.Fatal(err) }
	binary := filepath.Join(dir, "xray")
	script := `#!/bin/sh
if [ "$1" = "version" ]; then echo 'Xray 26.9.9'; exit 0; fi
echo 'secret-subscription-credential' >&2
exit 33
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil { t.Fatal(err) }
	t.Setenv("FREENET_XRAY_BIN", binary)
	t.Setenv("FREENET_ROUTING_CONFIG_DIR", configDir)
	t.Setenv("FREENET_XRAY_PROBE_LOCK_FILE", filepath.Join(dir, "probe.lock"))
	a := &app{cfg: config{OutPath: filepath.Join(configDir, "04_outbounds.json")}}
	rec := httptest.NewRecorder()
	a.handleXrayCoreValidationGet(rec, httptest.NewRequest(http.MethodGet, "/api/xray/core/validation", nil))
	var result xrayCoreValidationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil { t.Fatal(err) }
	if rec.Code != http.StatusOK || !result.Success || !result.Ready || result.Valid ||
		result.Validation != "DEFERRED" || result.Mutation != "NONE" || result.Reason != "" {
		t.Fatalf("chooser must report readiness without claiming semantic PASS: %+v, HTTP %d", result, rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret-subscription-credential") || strings.Contains(rec.Body.String(), configDir) {
		t.Fatal("read-only Xray validation response leaked private runtime detail")
	}
	if _, err := os.Stat(filepath.Join(configDir, "04_outbounds.json")); err != nil {
		t.Fatal("read-only Xray validation changed live config")
	}
}

func TestXrayCoreVersionChooserNeverWaitsForBusyProbeGate(t *testing.T) {
    dir := t.TempDir()
    configDir := filepath.Join(dir, "config")
    if err := os.MkdirAll(configDir, 0700); err != nil { t.Fatal(err) }
    if err := os.WriteFile(filepath.Join(configDir, "04_outbounds.json"), []byte(`{"outbounds":[]}`), 0600); err != nil { t.Fatal(err) }
    binary := filepath.Join(dir, "xray")
    script := "#!/bin/sh\nif [ \"$1\" = \"version\" ]; then echo 'Xray 26.9.9'; exit 0; fi\nexit 27\n"
    if err := os.WriteFile(binary, []byte(script), 0700); err != nil { t.Fatal(err) }
    t.Setenv("FREENET_XRAY_BIN", binary)
    t.Setenv("FREENET_ROUTING_CONFIG_DIR", configDir)
    t.Setenv("FREENET_XRAY_PROBE_LOCK_FILE", filepath.Join(dir, "probe.lock"))
    releases := make([]func(), 0, isolatedXrayProbeLimit)
    for i := 0; i < isolatedXrayProbeLimit; i++ {
        release, ok := acquireIsolatedXrayProbe(context.Background())
        if !ok { t.Fatal("fixture could not occupy read-only probe gate") }
        releases = append(releases, release)
    }
    defer func(){ for _, release := range releases { release() } }()
    a := &app{cfg: config{OutPath: filepath.Join(configDir, "04_outbounds.json")}}
    reqCtx, cancel := context.WithTimeout(context.Background(), 650*time.Millisecond)
    defer cancel()
    rec := httptest.NewRecorder()
    start := time.Now()
    a.handleXrayCoreValidationGet(rec, httptest.NewRequest(http.MethodGet, "/api/xray/core/validation", nil).WithContext(reqCtx))
    elapsed := time.Since(start)
    var result xrayCoreValidationResponse
    if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil { t.Fatal(err) }
    if elapsed > 500*time.Millisecond || rec.Code != http.StatusOK ||
        !result.Ready || result.Valid || result.Validation != "DEFERRED" || result.Mutation != "NONE" {
        t.Fatalf("version chooser blocked behind background probes (duration %s): %+v, HTTP %d", elapsed, result, rec.Code)
    }
}
