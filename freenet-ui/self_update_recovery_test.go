package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func recoveryUpdateRequest(target string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, target, nil)
	r.Host = "router.test"
	r.Header.Set("Origin", "http://router.test")
	return r
}

func TestRecoveryManifestSHARequiresExactHelperEntry(t *testing.T) {
	sum := strings.Repeat("a", 64)
	got, err := recoveryManifestSHA([]byte(sum+"  self_update.sh\n"), "self_update.sh")
	if err != nil || got != sum {
		t.Fatalf("manifest lookup got=%q err=%v", got, err)
	}
	if _, err := recoveryManifestSHA([]byte(sum+"  other.sh\n"), "self_update.sh"); err == nil {
		t.Fatal("missing self_update.sh entry must fail")
	}
	if _, err := recoveryManifestSHA([]byte("not-a-sha  self_update.sh\n"), "self_update.sh"); err == nil {
		t.Fatal("invalid SHA must fail")
	}
}

func TestBrowserRecoveryBootstrapsVerifiedUpdaterWithoutInstalledHelper(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "applied")
	t.Setenv("RECOVERY_MARKER", marker)

	helper := []byte(`#!/bin/sh
case "$1" in
  plan)
    echo SUCCESS=yes
    echo READY=yes
    echo CURRENT_VERSION=v0.3.84
    echo LATEST_VERSION=v9.9.9
    echo TARGET_TAG=v9.9.9
    echo UPDATE_AVAILABLE=yes
    echo MANIFEST_VERIFIED=yes
    echo EXPECTED_DELTA=verified-test
    echo EXPECTED_NO_DELTA=network-state
    ;;
  apply)
    printf '%s\n' "$2" > "$RECOVERY_MARKER"
    ;;
  *) exit 2 ;;
esac
`)
	hash := sha256.Sum256(helper)
	sum := hex.EncodeToString(hash[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"tag_name":"v9.9.9"}`)
		case "/releases/download/v9.9.9/SHA256SUMS":
			fmt.Fprintf(w, "%s  self_update.sh\n", sum)
		case "/releases/download/v9.9.9/self_update.sh":
			_, _ = w.Write(helper)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("FREENET_RECOVERY_LATEST_URL", server.URL+"/latest")
	t.Setenv("FREENET_RECOVERY_RELEASE_ROOT", server.URL+"/releases/download")

	a := &app{cfg: config{
		SelfUpdatePath: filepath.Join(dir, "missing-installed-updater.sh"),
		UpdateState:    filepath.Join(dir, "update.state"),
		UpdateLock:     filepath.Join(dir, "update.lock"),
	}}
	req := recoveryUpdateRequest("http://router.test/api/system/update/recover")
	rr := httptest.NewRecorder()
	a.handleSelfUpdateRecover(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "v9.9.9") {
		t.Fatalf("target not returned: %s", rr.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(marker); err == nil {
			if got := strings.TrimSpace(string(b)); got != "v9.9.9" {
				t.Fatalf("apply target=%q", got)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("verified temporary updater was not executed")
}

func TestBrowserRecoveryStopsOnSHAMismatchBeforeApply(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "applied")
	t.Setenv("RECOVERY_MARKER", marker)
	helper := []byte("#!/bin/sh\ntouch \"$RECOVERY_MARKER\"\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			fmt.Fprint(w, `{"tag_name":"v9.9.9"}`)
		case "/releases/download/v9.9.9/SHA256SUMS":
			fmt.Fprintf(w, "%s  self_update.sh\n", strings.Repeat("0", 64))
		case "/releases/download/v9.9.9/self_update.sh":
			_, _ = w.Write(helper)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("FREENET_RECOVERY_LATEST_URL", server.URL+"/latest")
	t.Setenv("FREENET_RECOVERY_RELEASE_ROOT", server.URL+"/releases/download")
	a := &app{cfg: config{
		UpdateState: filepath.Join(dir, "update.state"),
		UpdateLock:  filepath.Join(dir, "update.lock"),
	}}
	rr := httptest.NewRecorder()
	a.handleSelfUpdateRecover(rr, recoveryUpdateRequest("http://router.test/api/system/update/recover"))
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("SHA mismatch reached apply: %v", err)
	}
}

func TestBrowserRecoveryPreservesRollbackFailedHardStop(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "update.lock")
	if err := os.Mkdir(lock, 0o755); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "update.state")
	if err := os.WriteFile(state, []byte("STATE=ROLLBACK_FAILED\nROLLBACK_STATE=FAILED_UNKNOWN\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: config{UpdateState: state, UpdateLock: lock}}
	rr := httptest.NewRecorder()
	a.handleSelfUpdateRecover(rr, recoveryUpdateRequest("http://router.test/api/system/update/recover"))
	if rr.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("hard-stop lock was removed: %v", err)
	}
}
