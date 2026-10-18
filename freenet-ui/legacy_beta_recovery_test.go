package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func legacyHandoffTestState(t *testing.T) (config, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := config{ConfigPath: filepath.Join(dir, "freenet.conf"), UpdateState: filepath.Join(dir, "update.state"),
		UpdateLock: filepath.Join(dir, "update.lock"), SelfUpdatePath: filepath.Join(dir, "self_update.sh")}
	checkpoint := filepath.Join(dir, "vpn-transaction.pending")
	t.Setenv("FREENET_VPN_TRANSACTION_DIR", checkpoint)
	t.Setenv("FREENET_SETTINGS_V3_STATE", filepath.Join(dir, "settings.state"))
	return cfg, checkpoint
}

func TestLegacyHandoffDetectionPreservesNormalStableBoot(t *testing.T) {
	cfg, checkpoint := legacyHandoffTestState(t)
	if got := legacyHandoffReason(cfg); got != "" {
		t.Fatal("clean Stable boot entered recovery:", got)
	}
	for _, state := range []string{
		"STATE=SUCCESS\nFROM_VERSION=v0.8.0-beta.1\nTARGET_VERSION=v0.7.18\n",
		"STATE=CHECKING\nFROM_VERSION=v0.7.18\nTARGET_VERSION=v0.8.0-beta.5\n",
	} {
		if err := os.WriteFile(cfg.UpdateState, []byte(state), 0600); err != nil { t.Fatal(err) }
		if got := legacyHandoffReason(cfg); got != "BETA_HANDOFF" { t.Fatal(got) }
	}
	_ = os.Remove(cfg.UpdateState)
	if err := os.Mkdir(checkpoint, 0700); err != nil { t.Fatal(err) }
	if got := legacyHandoffReason(cfg); got != "VPN_CHECKPOINT_PRESERVED" { t.Fatal(got) }
	if err := os.Remove(checkpoint); err != nil { t.Fatal(err) }
	if err := os.WriteFile(settingsV3StatePath(), []byte("MUTATION_BLOCKED=yes\n"), 0600); err != nil { t.Fatal(err) }
	if got := legacyHandoffReason(cfg); got != "VPN_ROLLBACK_UNCONFIRMED" { t.Fatal(got) }
	if err := os.Symlink("missing-target", cfg.UpdateState); err != nil { t.Fatal(err) }
	if got := legacyHandoffReason(cfg); got != "UPDATE_STATE_UNKNOWN" { t.Fatal(got) }
}

func TestLegacyHandoffExposesNoNetworkMutationRoutes(t *testing.T) {
	cfg, _ := legacyHandoffTestState(t)
	a := &app{cfg: cfg, sem: make(chan struct{}, 1)}
	handler := legacyHandoffHandler(a)
	for _, path := range []string{"/api/action", "/api/network-profile/apply", "/api/xray/service", "/api/settings-v3", "/api/system/update/apply", "/recovery/repair-update"} {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, recoveryUpdateRequest("http://router.test"+path))
		if rr.Code < 400 { t.Fatalf("unexpected mutation route %s: %d", path, rr.Code) }
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, recoveryUpdateRequest("http://router.test/recovery/continue"))
	if rr.Code != http.StatusForbidden { t.Fatalf("unauthenticated continuation: %d", rr.Code) }
	for _, state := range []string{"STATE=ROLLBACK_FAILED\n", "ROLLBACK_STATE=FAILED_UNKNOWN\n", "ROLLBACK_STATE=PENDING\n"} {
		if err := os.WriteFile(cfg.UpdateState, []byte(state), 0600); err != nil { t.Fatal(err) }
		if !legacyHandoffUpdateBlocked(a) { t.Fatal("updater rollback hard stop bypassed") }
	}
}

func TestLegacyBeta1RecoveryDispatchDoesNotConsultVPNLatch(t *testing.T) {
	// self_update_recovery.go has the identical blob in released Beta1 and
	// v0.7.17. Exercise that OLD handler, not a newly invented endpoint.
	cfg, checkpoint := legacyHandoffTestState(t)
	if err := os.Mkdir(checkpoint, 0700); err != nil { t.Fatal(err) }
	before := []byte("PENDING\n")
	if err := os.WriteFile(filepath.Join(checkpoint, "state"), before, 0600); err != nil { t.Fatal(err) }
	if err := os.WriteFile(settingsV3StatePath(), []byte("MUTATION_BLOCKED=yes\n"), 0600); err != nil { t.Fatal(err) }
	marker := filepath.Join(filepath.Dir(cfg.ConfigPath), "applied")
	t.Setenv("RECOVERY_MARKER", marker)
	helper := []byte("#!/bin/sh\ncase \"$1\" in\nplan) printf '%s\\n' SUCCESS=yes READY=yes CURRENT_VERSION=v0.8.0-beta.1 TARGET_TAG=v0.7.18 UPDATE_AVAILABLE=yes MANIFEST_VERIFIED=yes;;\napply) printf '%s\\n' \"$2\" > \"$RECOVERY_MARKER\";;\n*) exit 2;;\nesac\n")
	hash := sha256.Sum256(helper)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest": _, _ = io.WriteString(w, `{"tag_name":"v0.7.18"}`)
		case "/v0.7.18/SHA256SUMS": _, _ = fmt.Fprintf(w, "%s  self_update.sh\n", hex.EncodeToString(hash[:]))
		case "/v0.7.18/self_update.sh": _, _ = w.Write(helper)
		default: http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("FREENET_RECOVERY_LATEST_URL", server.URL+"/latest")
	t.Setenv("FREENET_RECOVERY_RELEASE_ROOT", server.URL)
	a := &app{cfg: cfg}
	rr := httptest.NewRecorder()
	a.handleSelfUpdateRecover(rr, recoveryUpdateRequest("http://router.test/api/system/update/recover"))
	if rr.Code != http.StatusAccepted { t.Fatalf("old Recovery blocked: %d %s", rr.Code, rr.Body.String()) }
	deadline := time.Now().Add(3*time.Second)
	for {
		if b, err := os.ReadFile(marker); err == nil && strings.TrimSpace(string(b)) == "v0.7.18" { break }
		if time.Now().After(deadline) { t.Fatal("verified updater was not invoked") }
		time.Sleep(20*time.Millisecond)
	}
	if b, err := os.ReadFile(filepath.Join(checkpoint, "state")); err != nil || string(b) != string(before) { t.Fatal("VPN checkpoint changed") }
	if b, _ := os.ReadFile(settingsV3StatePath()); string(b) != "MUTATION_BLOCKED=yes\n" { t.Fatal("VPN latch was cleared") }
}

func TestLegacyHandoffBootFencePrecedesCLIAndDaemon(t *testing.T) {
	if testing.Short() { t.Skip("native subprocess gate") }
	cfg, checkpoint := legacyHandoffTestState(t)
	if err := os.Mkdir(checkpoint, 0700); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(checkpoint, "state"), []byte("PENDING\n"), 0600); err != nil { t.Fatal(err) }
	if err := os.WriteFile(cfg.ConfigPath, []byte("AUTO_VPN_V1=yes\n"), 0600); err != nil { t.Fatal(err) }
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	listed, err := exec.CommandContext(ctx, "go", "list", "-json", ".").Output()
	if err != nil { t.Fatal(err) }
	var pkg struct { GoFiles []string }
	if err := json.Unmarshal(listed, &pkg); err != nil { t.Fatal(err) }
	if len(pkg.GoFiles) == 0 || pkg.GoFiles[0] != "000_legacy_beta_recovery.go" { t.Fatal("early-init build order changed", pkg.GoFiles) }
	binary := filepath.Join(filepath.Dir(cfg.ConfigPath), "freenet-ui")
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", binary, ".").CombinedOutput(); err != nil { t.Fatalf("build: %v %s", err, out) }
	cli := exec.CommandContext(ctx, binary, "automation-health-watch", "--config", cfg.ConfigPath, "--update-state", cfg.UpdateState)
	if out, err := cli.CombinedOutput(); err == nil || !strings.Contains(string(out), "recovery-only") { t.Fatalf("CLI bypass: %v %s", err, out) }
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil { t.Fatal(err) }
	addr := ln.Addr().String()
	_ = ln.Close()
	cmd := exec.CommandContext(ctx, binary, "--listen", addr, "--config", cfg.ConfigPath, "--update-state", cfg.UpdateState)
	if err := cmd.Start(); err != nil { t.Fatal(err) }
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	client := &http.Client{Timeout: 300*time.Millisecond}
	deadline := time.Now().Add(8*time.Second)
	for {
		resp, err := client.Get("http://"+addr+"/recovery")
		if err == nil {
			body, _ := io.ReadAll(resp.Body); _ = resp.Body.Close()
			if resp.StatusCode != 200 || !strings.Contains(string(body), "safe") && !strings.Contains(string(body), "промежуточный") { t.Fatalf("wrong application entered: %d", resp.StatusCode) }
			break
		}
		if time.Now().After(deadline) { t.Fatal("recovery listener unavailable", err) }
		time.Sleep(30*time.Millisecond)
	}
	resp, err := client.Get("http://"+addr+"/api/status")
	if err != nil { t.Fatal(err) }
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound { t.Fatal("full application unexpectedly started") }
	if b, _ := os.ReadFile(filepath.Join(checkpoint, "state")); string(b) != "PENDING\n" { t.Fatal("checkpoint modified by boot") }
}
