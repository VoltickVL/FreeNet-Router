package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type rebootAcceptanceFixture struct {
	app      *app
	mux      *http.ServeMux
	cookie   *http.Cookie
	dir      string
	xrayDir  string
	bootID   *string
	rebooted chan struct{}
}

func newRebootAcceptanceFixture(t *testing.T) rebootAcceptanceFixture {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "etc", "freenet", "freenet.conf")
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		t.Fatal(err)
	}
	configText := strings.Join([]string{
		"INSTALL_SCENARIO=existing_stack",
		"SETUP_COMPLETE=yes",
		"AUTO_VPN_V1=no",
		"AUTO_SUBSCRIPTION_REFRESH_ENABLED=yes",
		"AUTO_SUBSCRIPTION_REFRESH_INTERVAL=6h",
		"AUTO_GEODATA_ENABLED=no",
		"AUTO_XKEEN_GEODATA=no",
		"CUSTOM_SENTINEL=do-not-leak-marker",
		"",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(configText), 0600); err != nil {
		t.Fatal(err)
	}

	xrayDir := filepath.Join(dir, "etc", "xray", "configs")
	if err := os.MkdirAll(xrayDir, 0700); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(xrayDir, "04_outbounds.json")
	if err := os.WriteFile(outPath, []byte("{\"outbounds\":[]}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xrayDir, "03_routing.json"), []byte("{\"routing\":{}}\n"), 0600); err != nil {
		t.Fatal(err)
	}

	uiInit := filepath.Join(dir, "S99freenet-ui")
	if err := os.WriteFile(uiInit, []byte("#!/bin/sh\nENABLED=yes\nPROCS=freenet-ui\n. /opt/etc/init.d/rc.func\n"), 0755); err != nil {
		t.Fatal(err)
	}
	xkeenInit := filepath.Join(dir, "S99xkeen")
	if err := os.WriteFile(xkeenInit, []byte("#!/bin/sh\nstart_auto=on\n"), 0755); err != nil {
		t.Fatal(err)
	}
	rebootBin := filepath.Join(dir, "reboot")
	if err := os.WriteFile(rebootBin, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FREENET_UI_INIT_PATH", uiInit)
	t.Setenv("FREENET_XKEEN_INIT_PATH", xkeenInit)
	t.Setenv("FREENET_REBOOT_BIN", rebootBin)
	t.Setenv("FREENET_UI_BIN", "/opt/sbin/freenet-ui")

	cronBin, cronState := writeFakeCrontab(t, dir)
	t.Setenv("FREENET_CRONTAB_BIN", cronBin)
	t.Setenv("FREENET_TEST_CRONTAB_STATE", cronState)

	a := &app{cfg: config{
		ConfigPath: configPath,
		OutPath: outPath,
		LockPath: filepath.Join(dir, "vpn.lock"),
		UpdateLock: filepath.Join(dir, "update.lock"),
	}, sem: make(chan struct{}, 1)}
	t.Cleanup(func() { authStateByApp.Delete(a) })
	if err := a.createCredential("correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.reconcileSettingsV3Scheduler(); err != nil {
		t.Fatalf("prepare canonical scheduler: %v", err)
	}

	oldReadBoot := rebootReadBootID
	oldProcessRunning := rebootProcessRunning
	oldRunner := rebootCommandRunner
	oldDelay := rebootCommandDelay
	bootID := "11111111-1111-1111-1111-111111111111"
	rebooted := make(chan struct{}, 1)
	rebootReadBootID = func() (string, error) { return bootID, nil }
	rebootProcessRunning = func(name string) bool { return name == "xray" }
	rebootCommandDelay = 0
	rebootCommandRunner = func() error {
		select {
		case rebooted <- struct{}{}:
		default:
		}
		return nil
	}
	t.Cleanup(func() {
		rebootReadBootID = oldReadBoot
		rebootProcessRunning = oldProcessRunning
		rebootCommandRunner = oldRunner
		rebootCommandDelay = oldDelay
	})

	mux := http.NewServeMux()
	registerRebootAcceptanceAPI(mux, a)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	loginW := httptest.NewRecorder()
	if err := a.newSession(loginW, loginReq); err != nil {
		t.Fatal(err)
	}
	cookie := loginW.Result().Cookies()[0]

	return rebootAcceptanceFixture{
		app: a, mux: mux, cookie: cookie, dir: dir, xrayDir: xrayDir, bootID: &bootID, rebooted: rebooted,
	}
}

func rebootAPIRequest(f rebootAcceptanceFixture, method, target, body string, authenticated bool) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	if authenticated {
		req.AddCookie(f.cookie)
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://example.com")
	}
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, req)
	return w
}

func TestRebootAcceptancePlanReadyAndReadOnly(t *testing.T) {
	f := newRebootAcceptanceFixture(t)
	beforeConfig, _ := os.ReadFile(f.app.cfg.ConfigPath)
	beforeXray, _ := os.ReadFile(f.app.cfg.OutPath)

	w := rebootAPIRequest(f, http.MethodGet, "/api/system/reboot/plan", "", true)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var resp rebootAcceptancePlanResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success || !resp.Ready || resp.Mutation != "NONE" {
		t.Fatalf("response=%+v", resp)
	}
	for _, check := range resp.Checks {
		if !check.OK {
			t.Fatalf("unexpected failed preflight check: %+v", check)
		}
	}
	afterConfig, _ := os.ReadFile(f.app.cfg.ConfigPath)
	afterXray, _ := os.ReadFile(f.app.cfg.OutPath)
	if string(beforeConfig) != string(afterConfig) || string(beforeXray) != string(afterXray) {
		t.Fatal("read-only reboot plan mutated persistent configuration")
	}
	if _, err := os.Stat(f.app.rebootAcceptancePath()); !os.IsNotExist(err) {
		t.Fatalf("read-only plan unexpectedly wrote marker: %v", err)
	}
}

func TestRebootAcceptanceApplyAwaitAndPassOnNewBoot(t *testing.T) {
	f := newRebootAcceptanceFixture(t)

	w := rebootAPIRequest(f, http.MethodPost, "/api/system/reboot/apply", `{"confirm":"REBOOT"}`, true)
	if w.Code != http.StatusAccepted {
		t.Fatalf("apply code=%d body=%s", w.Code, w.Body.String())
	}
	select {
	case <-f.rebooted:
	case <-time.After(2 * time.Second):
		t.Fatal("reboot command was not scheduled")
	}
	markerBytes, err := os.ReadFile(f.app.rebootAcceptancePath())
	if err != nil {
		t.Fatal(err)
	}
	markerText := string(markerBytes)
	for _, forbidden := range []string{"correct horse battery staple", "do-not-leak-marker", "outbounds"} {
		if strings.Contains(markerText, forbidden) {
			t.Fatalf("reboot marker leaked protected/config content %q: %s", forbidden, markerText)
		}
	}

	w = rebootAPIRequest(f, http.MethodGet, "/api/system/reboot/state", "", true)
	var waiting rebootAcceptanceStateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &waiting); err != nil {
		t.Fatal(err)
	}
	if waiting.State != "AWAITING_REBOOT" || waiting.Complete || waiting.Passed || waiting.Mutation != "NONE" {
		t.Fatalf("waiting response=%+v", waiting)
	}

	*f.bootID = "22222222-2222-2222-2222-222222222222"
	w = rebootAPIRequest(f, http.MethodGet, "/api/system/reboot/state", "", true)
	var done rebootAcceptanceStateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &done); err != nil {
		t.Fatal(err)
	}
	if done.State != "PASS" || !done.Complete || !done.Passed || done.Mutation != "NONE" {
		t.Fatalf("pass response=%+v body=%s", done, w.Body.String())
	}
	for _, check := range done.Checks {
		if !check.OK {
			t.Fatalf("post-boot check failed: %+v", check)
		}
	}
}

func TestRebootAcceptanceDetectsPostBootConfigDrift(t *testing.T) {
	f := newRebootAcceptanceFixture(t)
	w := rebootAPIRequest(f, http.MethodPost, "/api/system/reboot/apply", `{"confirm":"REBOOT"}`, true)
	if w.Code != http.StatusAccepted {
		t.Fatalf("apply code=%d body=%s", w.Code, w.Body.String())
	}
	select {
	case <-f.rebooted:
	case <-time.After(2 * time.Second):
		t.Fatal("reboot command was not scheduled")
	}
	*f.bootID = "33333333-3333-3333-3333-333333333333"
	if err := os.WriteFile(f.app.cfg.OutPath, []byte("{\"outbounds\":[{\"tag\":\"changed\"}]}\n"), 0600); err != nil {
		t.Fatal(err)
	}

	w = rebootAPIRequest(f, http.MethodGet, "/api/system/reboot/state", "", true)
	var resp rebootAcceptanceStateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.State != "FAIL" || !resp.Complete || resp.Passed {
		t.Fatalf("response=%+v", resp)
	}
	found := false
	for _, check := range resp.Checks {
		if check.Key == "xray_config" {
			found = true
			if check.OK {
				t.Fatal("Xray drift was not detected")
			}
		}
	}
	if !found {
		t.Fatal("xray_config acceptance check missing")
	}
}

func TestRebootAcceptanceStopsUnsafeApply(t *testing.T) {
	f := newRebootAcceptanceFixture(t)

	anonymous := rebootAPIRequest(f, http.MethodPost, "/api/system/reboot/apply", `{"confirm":"REBOOT"}`, false)
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous code=%d body=%s", anonymous.Code, anonymous.Body.String())
	}

	missingConfirm := rebootAPIRequest(f, http.MethodPost, "/api/system/reboot/apply", `{"confirm":"no"}`, true)
	if missingConfirm.Code != http.StatusBadRequest {
		t.Fatalf("confirm code=%d body=%s", missingConfirm.Code, missingConfirm.Body.String())
	}

	if err := os.WriteFile(f.app.cfg.UpdateLock, []byte("busy\n"), 0600); err != nil {
		t.Fatal(err)
	}
	blocked := rebootAPIRequest(f, http.MethodPost, "/api/system/reboot/apply", `{"confirm":"REBOOT"}`, true)
	if blocked.Code != http.StatusConflict {
		t.Fatalf("busy code=%d body=%s", blocked.Code, blocked.Body.String())
	}
	if _, err := os.Stat(f.app.rebootAcceptancePath()); !os.IsNotExist(err) {
		t.Fatalf("blocked apply wrote reboot marker: %v", err)
	}
}

func TestRebootAcceptanceRecordsCommandFailure(t *testing.T) {
	f := newRebootAcceptanceFixture(t)
	failed := make(chan struct{}, 1)
	rebootCommandRunner = func() error {
		failed <- struct{}{}
		return errors.New("simulated reboot failure")
	}

	w := rebootAPIRequest(f, http.MethodPost, "/api/system/reboot/apply", `{"confirm":"REBOOT"}`, true)
	if w.Code != http.StatusAccepted {
		t.Fatalf("apply code=%d body=%s", w.Code, w.Body.String())
	}
	select {
	case <-failed:
	case <-time.After(2 * time.Second):
		t.Fatal("failed reboot command was not invoked")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		w = rebootAPIRequest(f, http.MethodGet, "/api/system/reboot/state", "", true)
		var resp rebootAcceptanceStateResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.State == "FAILED_START" {
			if !resp.Complete || resp.Passed {
				t.Fatalf("failed-start response=%+v", resp)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("state never reached FAILED_START: %+v", resp)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
