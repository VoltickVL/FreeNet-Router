package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func configStudioTestApp(t *testing.T) (*app, string) {
	t.Helper()
	dir := t.TempDir()
	writeRoutingTestFile(t, filepath.Join(dir, "01_log.json"), `{"log":{"loglevel":"warning"}}`, 0600)
	writeRoutingTestFile(t, filepath.Join(dir, "02_dns.json"), `{"dns":{"servers":["1.1.1.1"]}}`, 0600)
	writeRoutingTestFile(t, filepath.Join(dir, "03_inbounds.json"), `{"inbounds":[{"tag":"test-in","settings":{"auth":"TEST-INBOUND-AUTH"}}]}`, 0600)
	writeRoutingTestFile(t, filepath.Join(dir, "04_outbounds.json"), `{"outbounds":[{"tag":"test-out","settings":{"vnext":[{"address":"192.0.2.10","users":[{"id":"TEST-UUID-00000000"}]}]}}]}`, 0600)
	writeRoutingTestFile(t, filepath.Join(dir, "05_routing.json"), `{"routing":{"domainStrategy":"AsIs","rules":[]}}`, 0600)
	writeRoutingTestFile(t, filepath.Join(dir, "06_policy.json"), `{"policy":{"levels":{"0":{"handshake":4}}}}`, 0600)
	return &app{cfg: config{OutPath: filepath.Join(dir, "04_outbounds.json"), GeoDataDir: dir}}, dir
}

func configStudioFakeXray(t *testing.T, script string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "xray")
	writeRoutingTestFile(t, bin, script, 0700)
	t.Setenv("FREENET_XRAY_BIN", bin)
	return bin
}

func TestConfigStudioGetReturnsAuthenticatedFullJSONWorkspace(t *testing.T) {
	a, _ := configStudioTestApp(t)
	configStudioFakeXray(t, `#!/bin/sh
if [ "${1:-}" = "version" ]; then echo 'Xray 26.9.9'; exit 0; fi
exit 0
`)

	rec := httptest.NewRecorder()
	a.handleConfigStudioGet(rec, httptest.NewRequest(http.MethodGet, "http://router/api/config-studio", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var body configStudioResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Success || body.Mutation != "NONE" || body.Xray.Version != "26.9.9" {
		t.Fatalf("unexpected response metadata: success=%v mutation=%s version=%s", body.Success, body.Mutation, body.Xray.Version)
	}
	byName := map[string]configStudioTab{}
	for _, tab := range body.Tabs {
		byName[tab.Name] = tab
	}
	for _, name := range []string{"01_log", "02_dns", "03_inbounds", "04_outbounds"} {
		tab := byName[name]
		if !tab.Present || tab.Access != "editable" || len(tab.Content) == 0 || tab.SHA256 == "" {
			t.Fatalf("editable tab %s missing authenticated content metadata", name)
		}
	}
	if !bytes.Contains(byName["03_inbounds"].Content, []byte("TEST-INBOUND-AUTH")) {
		t.Fatal("03_inbounds raw authenticated content missing")
	}
	if !bytes.Contains(byName["04_outbounds"].Content, []byte("TEST-UUID-00000000")) {
		t.Fatal("04_outbounds raw authenticated content missing")
	}
	for _, name := range []string{"05_routing", "06_policy"} {
		if !byName[name].Present || byName[name].Access != "routing-managed" || len(byName[name].Content) == 0 {
			t.Fatalf("routing-managed tab %s missing content", name)
		}
	}
}

func TestConfigStudioValidateUsesTemporaryFullCandidate(t *testing.T) {
	a, dir := configStudioTestApp(t)
	configStudioFakeXray(t, `#!/bin/sh
set -eu
[ "$1" = "run" ]
[ "$2" = "-test" ]
[ "$3" = "-confdir" ]
dir="$4"
grep -q 'debug' "$dir/01_log.json"
grep -q 'TEST-UUID-00000000' "$dir/04_outbounds.json"
exit 0
`)
	before, err := os.ReadFile(filepath.Join(dir, "01_log.json"))
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"file":"01_log.json","content":{"log":{"loglevel":"debug"}}}`
	req := httptest.NewRequest(http.MethodPost, "http://router/api/config-studio/validate", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://router")
	rec := httptest.NewRecorder()
	a.handleConfigStudioValidate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"xray_valid":true`) || !strings.Contains(rec.Body.String(), `"mutation":"NONE"`) {
		t.Fatalf("unexpected validation response metadata")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "01_log.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("Config Studio validation mutated live file")
	}
}

func TestConfigStudioValidateAllowsOutboundsArrayCandidateWithoutMutation(t *testing.T) {
	a, dir := configStudioTestApp(t)
	configStudioFakeXray(t, `#!/bin/sh
set -eu
[ "$1" = "run" ]
[ "$2" = "-test" ]
[ "$3" = "-confdir" ]
grep -q 'TEST-UUID-NEW' "$4/04_outbounds.json"
exit 0
`)
	before, err := os.ReadFile(filepath.Join(dir, "04_outbounds.json"))
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"file":"04_outbounds.json","content":{"outbounds":[{"tag":"test-out","settings":{"id":"TEST-UUID-NEW"}}]}}`
	req := httptest.NewRequest(http.MethodPost, "http://router/api/config-studio/validate", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://router")
	rec := httptest.NewRecorder()
	a.handleConfigStudioValidate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "04_outbounds.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("outbounds validation mutated live file")
	}
}

func TestConfigStudioApplyWritesOnlyAllowedFile(t *testing.T) {
	a, dir := configStudioTestApp(t)
	configStudioFakeXray(t, `#!/bin/sh
set -eu
[ "$1" = "run" ]
[ "$2" = "-test" ]
[ "$3" = "-confdir" ]
exit 0
`)
	before04, err := os.ReadFile(filepath.Join(dir, "04_outbounds.json"))
	if err != nil {
		t.Fatal(err)
	}
	before02, err := os.ReadFile(filepath.Join(dir, "02_dns.json"))
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"file":"01_log.json","content":{"log":{"loglevel":"debug"}}}`
	req := httptest.NewRequest(http.MethodPost, "http://router/api/config-studio/apply", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://router")
	rec := httptest.NewRecorder()
	a.handleConfigStudioApply(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"mutation":"APPLIED"`) || !strings.Contains(rec.Body.String(), `"core_restart":false`) {
		t.Fatalf("unexpected apply response metadata")
	}
	after04, _ := os.ReadFile(filepath.Join(dir, "04_outbounds.json"))
	after02, _ := os.ReadFile(filepath.Join(dir, "02_dns.json"))
	if !bytes.Equal(before04, after04) || !bytes.Equal(before02, after02) {
		t.Fatal("Config Studio apply changed an unrelated config file")
	}
	after01, _ := os.ReadFile(filepath.Join(dir, "01_log.json"))
	if !strings.Contains(string(after01), `"loglevel": "debug"`) {
		t.Fatal("allowed config was not atomically updated")
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".freenet-backups", "config-studio-*", "01_log.json"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("snapshot not created: matches=%d err=%v", len(matches), err)
	}
}

func TestConfigStudioApplyOutboundsUsesSnapshotAndPostValidation(t *testing.T) {
	a, dir := configStudioTestApp(t)
	configStudioFakeXray(t, `#!/bin/sh
set -eu
[ "$1" = "run" ]
[ "$2" = "-test" ]
[ "$3" = "-confdir" ]
exit 0
`)
	before01, err := os.ReadFile(filepath.Join(dir, "01_log.json"))
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"file":"04_outbounds.json","content":{"outbounds":[{"tag":"new-test-out","settings":{"id":"TEST-UUID-NEW"}}]}}`
	req := httptest.NewRequest(http.MethodPost, "http://router/api/config-studio/apply", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://router")
	rec := httptest.NewRecorder()
	a.handleConfigStudioApply(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"mutation":"APPLIED"`) {
		t.Fatal("outbounds apply was not accepted")
	}
	after04, _ := os.ReadFile(filepath.Join(dir, "04_outbounds.json"))
	if !strings.Contains(string(after04), "TEST-UUID-NEW") {
		t.Fatal("outbounds candidate was not written")
	}
	after01, _ := os.ReadFile(filepath.Join(dir, "01_log.json"))
	if !bytes.Equal(before01, after01) {
		t.Fatal("outbounds apply changed unrelated config")
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".freenet-backups", "config-studio-*", "04_outbounds.json"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("outbounds snapshot not created: matches=%d err=%v", len(matches), err)
	}
}

func TestConfigStudioRejectsManagedOrMalformedMutationBeforeXray(t *testing.T) {
	a, _ := configStudioTestApp(t)
	t.Setenv("FREENET_XRAY_BIN", filepath.Join(t.TempDir(), "must-not-run"))
	tests := []struct {
		name       string
		origin     string
		content    string
		body       string
		wantStatus int
	}{
		{name: "routing stays pair-managed", origin: "http://router", content: "application/json", body: `{"file":"05_routing.json","content":{"routing":{}}}`, wantStatus: http.StatusBadRequest},
		{name: "wrong log root", origin: "http://router", content: "application/json", body: `{"file":"01_log.json","content":{"dns":{}}}`, wantStatus: http.StatusBadRequest},
		{name: "outbounds root must be array", origin: "http://router", content: "application/json", body: `{"file":"04_outbounds.json","content":{"outbounds":{}}}`, wantStatus: http.StatusBadRequest},
		{name: "unknown request field", origin: "http://router", content: "application/json", body: `{"file":"01_log.json","content":{"log":{}},"extra":true}`, wantStatus: http.StatusBadRequest},
		{name: "cross origin", origin: "http://attacker.invalid", content: "application/json", body: `{"file":"01_log.json","content":{"log":{}}}`, wantStatus: http.StatusForbidden},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://router/api/config-studio/apply", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.content)
			req.Header.Set("Origin", tc.origin)
			rec := httptest.NewRecorder()
			a.handleConfigStudioApply(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d", rec.Code, tc.wantStatus)
			}
		})
	}
}
