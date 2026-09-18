package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func selfUpdateApplyRequest(t *testing.T, a *app, target string) (*httptest.ResponseRecorder, actionResult) {
	t.Helper()
	body := []byte(`{"target_tag":"` + target + `"}`)
	req := httptest.NewRequest(http.MethodPost, "http://router.test/api/system/update/apply", bytes.NewReader(body))
	req.Host = "router.test"
	req.Header.Set("Origin", "http://router.test")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	a.handleSelfUpdateApply(rr, req)
	var result actionResult
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode apply response: %v body=%s", err, rr.Body.String())
	}
	return rr, result
}

func TestSelfUpdateApplyAttachesToRunningOperation(t *testing.T) {
	dir := t.TempDir()
	lockDir := filepath.Join(dir, "update.lock")
	stateFile := filepath.Join(dir, "update.state")
	helper := filepath.Join(dir, "self_update.sh")
	marker := filepath.Join(dir, "unexpected-start")
	if err := os.Mkdir(lockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateFile, []byte("STATE=CHECKING\nTARGET_VERSION=v0.3.81\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nprintf started > '"+marker+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	a := &app{cfg: config{SelfUpdatePath: helper, UpdateLock: lockDir, UpdateState: stateFile}}
	rr, result := selfUpdateApplyRequest(t, a, "v0.3.82")
	if rr.Code != http.StatusAccepted {
		t.Fatalf("running update should attach with 202, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !result.Success || result.Action != "self-update" {
		t.Fatalf("unexpected attach response: %+v", result)
	}
	if result.OperationID != "v0.3.81" {
		t.Fatalf("attached target=%q want v0.3.81", result.OperationID)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("repeat apply started another updater process: %v", err)
	}
}

func TestSelfUpdateApplyKeepsRollbackFailedStopGate(t *testing.T) {
	dir := t.TempDir()
	lockDir := filepath.Join(dir, "update.lock")
	stateFile := filepath.Join(dir, "update.state")
	helper := filepath.Join(dir, "self_update.sh")
	if err := os.Mkdir(lockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateFile, []byte("STATE=ROLLBACK_FAILED\nTARGET_VERSION=v0.3.81\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	a := &app{cfg: config{SelfUpdatePath: helper, UpdateLock: lockDir, UpdateState: stateFile}}
	rr, result := selfUpdateApplyRequest(t, a, "v0.3.82")
	if rr.Code != http.StatusConflict {
		t.Fatalf("rollback-failed lock must remain a hard stop, got %d body=%s", rr.Code, rr.Body.String())
	}
	if result.Success || result.Error == "" {
		t.Fatalf("rollback-failed stop gate was weakened: %+v", result)
	}
}

func TestSelfUpdateStateReportsLaunchingBeforeShellLock(t *testing.T) {
	dir := t.TempDir()
	a := &app{
		cfg:             config{UpdateLock: filepath.Join(dir, "missing.lock"), UpdateState: filepath.Join(dir, "missing.state")},
		updateLaunching: true,
		updateTarget:    "v0.3.82",
	}
	req := httptest.NewRequest(http.MethodGet, "http://router.test/api/system/update/state", nil)
	rr := httptest.NewRecorder()
	a.handleSelfUpdateState(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("state status=%d", rr.Code)
	}
	var state selfUpdateStateResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if !state.UpdateLockHeld {
		t.Fatal("launching updater must be reported busy before shell lock exists")
	}
}
