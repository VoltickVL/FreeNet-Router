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
	if rec.Code != http.StatusOK || !result.Success || result.Valid || result.Mutation != "NONE" ||
		!strings.Contains(result.Reason, "reason=xray-live-config-test-failed") {
		t.Fatalf("invalid live validation must return read-only STOP reason: %+v, HTTP %d", result, rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret-subscription-credential") || strings.Contains(rec.Body.String(), configDir) {
		t.Fatal("read-only Xray validation response leaked private runtime detail")
	}
	if _, err := os.Stat(filepath.Join(configDir, "04_outbounds.json")); err != nil {
		t.Fatal("read-only Xray validation changed live config")
	}
}
