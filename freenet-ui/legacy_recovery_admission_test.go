package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLegacyHandoffUnknownUpdaterCannotContinue(t *testing.T) {
	cfg, _ := legacyHandoffTestState(t)
	a := &app{cfg: cfg}
	for _, state := range []string{
		"", "not key value", "STATE=UNKNOWN\n", "STATE=CHECKING\nROLLBACK_STATE=NOT_NEEDED\n",
		"STATE=RECONNECTING\nROLLBACK_STATE=PENDING\n", "STATE=FAILED\nROLLBACK_STATE=FAILED_UNKNOWN\n",
		"STATE=SUCCESS\nROLLBACK_STATE=PENDING\n", "STATE=FAILED\n",
	} {
		legacyHandoffWrite(t, cfg.UpdateState, []byte(state))
		if !legacyHandoffUpdateBlocked(a) { t.Fatalf("unknown/nonterminal state was accepted: %q", state) }
	}
	for _, state := range []string{"STATE=SUCCESS\nROLLBACK_STATE=NOT_NEEDED\n", "STATE=FAILED\nROLLBACK_STATE=SUCCESS\n"} {
		legacyHandoffWrite(t, cfg.UpdateState, []byte(state))
		if legacyHandoffUpdateBlocked(a) { t.Fatal("known terminal result blocked", state) }
	}
}

func TestLegacyHandoffAuthenticatedExactBetaContinuation(t *testing.T) {
	cfg, checkpoint := legacyHandoffTestState(t)
	legacyHandoffWrite(t, filepath.Join(checkpoint, "state"), []byte("PENDING\n"))
	legacyHandoffWrite(t, cfg.UpdateState, []byte("STATE=SUCCESS\nROLLBACK_STATE=NOT_NEEDED\nFROM_VERSION=v0.8.0-beta.1\nTARGET_VERSION=v0.7.18\n"))
	marker := filepath.Join(filepath.Dir(cfg.ConfigPath), "apply.receipt")
	t.Setenv("LEGACY_HANDOFF_RECEIPT", marker)
	legacyHandoffWrite(t, cfg.SelfUpdatePath, []byte("#!/bin/sh\ncase \"$1\" in\nplan) printf '%s\\n' SUCCESS=yes READY=yes MANIFEST_VERIFIED=yes UPDATE_AVAILABLE=yes TARGET_TAG=v0.8.0-beta.5;;\napply) printf '%s|%s\\n' \"$2\" \"$FREENET_UPDATE_BETA_OPT_IN\" > \"$LEGACY_HANDOFF_RECEIPT\";;\n*) exit 2;;\nesac\n"))
	a := &app{cfg: cfg, sem: make(chan struct{}, 1)}
	loginResponse := httptest.NewRecorder()
	if err := a.newSession(loginResponse, recoveryUpdateRequest("http://router.test/recovery/login")); err != nil { t.Fatal(err) }
	cookies := loginResponse.Result().Cookies()
	if len(cookies) == 0 { t.Fatal("session was not created") }
	handler := legacyHandoffHandler(a)
	request := func(method, target, origin string, values url.Values) *http.Request {
		var body io.Reader
		if values != nil { body = strings.NewReader(values.Encode()) }
		r := httptest.NewRequest(method, "http://router.test"+target, body)
		r.Host = "router.test"
		r.Header.Set("Origin", origin)
		if values != nil { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") }
		for _, cookie := range cookies { r.AddCookie(cookie) }
		return r
	}
	for _, tc := range []struct { origin, target, confirm string; code int }{
		{"http://foreign.test", legacyHandoffTarget, "yes", http.StatusForbidden},
		{"http://router.test", "v0.8.0-beta.4", "yes", http.StatusBadRequest},
		{"http://router.test", legacyHandoffTarget, "no", http.StatusBadRequest},
	} {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, request(http.MethodPost, "/recovery/continue", tc.origin, url.Values{"target":{tc.target}, "confirm":{tc.confirm}}))
		if rr.Code != tc.code { t.Fatalf("admission: %d want %d", rr.Code, tc.code) }
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, request(http.MethodGet, "/recovery?check=1", "http://router.test", nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Подтверждаю установку BETA") { t.Fatalf("exact read-only plan failed: %d %s", rr.Code, rr.Body.String()) }
	if _, err := os.Stat(marker); !os.IsNotExist(err) { t.Fatal("plan or rejected request applied anything") }
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, request(http.MethodPost, "/recovery/continue", "http://router.test", url.Values{"target":{legacyHandoffTarget}, "confirm":{"yes"}}))
	if rr.Code != http.StatusAccepted { t.Fatalf("confirmed Beta was not accepted: %d %s", rr.Code, rr.Body.String()) }
	deadline := time.Now().Add(3*time.Second)
	for {
		if data, err := os.ReadFile(marker); err == nil {
			if strings.TrimSpace(string(data)) != legacyHandoffTarget+"|yes" { t.Fatal("target or opt-in changed") }
			break
		}
		if time.Now().After(deadline) { t.Fatal("confirmed updater never ran") }
		time.Sleep(20*time.Millisecond)
	}
	if data, _ := os.ReadFile(filepath.Join(checkpoint, "state")); string(data) != "PENDING\n" { t.Fatal("VPN checkpoint cleared during application update") }
}
