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

const routingApplyCandidatePayload = `{"routing":{"routing":{"domainStrategy":"AsIs","rules":[{"type":"field","domain":["domain:candidate.example"],"outboundTag":"direct"}]}},"policy":{"policy":{"levels":{"0":{"handshake":5}}}}}`

func installCountingRoutingXray(t *testing.T, failCounts ...string) string {
	t.Helper()
	dir := t.TempDir()
	counter := filepath.Join(dir, "count")
	failExpr := ""
	for _, n := range failCounts {
		failExpr += "[ \"$count\" = \"" + n + "\" ] && exit 1\n"
	}
	fakeXray := filepath.Join(dir, "xray")
	writeRoutingTestFile(t, fakeXray, `#!/bin/sh
set -eu
[ "$1" = "run" ]
[ "$2" = "-test" ]
[ "$3" = "-confdir" ]
dir="$4"
count=1
if [ -f "`+counter+`" ]; then count=$(( $(cat "`+counter+`") + 1 )); fi
printf '%s' "$count" > "`+counter+`"
[ -f "$dir/04_outbounds.json" ]
[ -f "$dir/05_routing.json" ]
[ -f "$dir/06_policy.json" ]
grep -q 'SECRET-UUID-MUST-NOT-LEAK' "$dir/04_outbounds.json"
`+failExpr+`exit 0
`, 0700)
	return fakeXray
}

func performRoutingApply(t *testing.T, a *app, body string) (*httptest.ResponseRecorder, routingApplyResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "http://router/api/routing/apply", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://router")
	rec := httptest.NewRecorder()
	a.handleRoutingConfigApply(rec, req)
	var resp routingApplyResponse
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("invalid response JSON: %v body=%s", err, rec.Body.String())
		}
	}
	return rec, resp
}

func TestRoutingApplyWritesManagedSectionsWithSnapshot(t *testing.T) {
	a, dir := routingTestApp(t)
	t.Setenv("FREENET_XRAY_BIN", installCountingRoutingXray(t))
	before04, err := os.ReadFile(filepath.Join(dir, "04_outbounds.json"))
	if err != nil {
		t.Fatal(err)
	}

	rec, resp := performRoutingApply(t, a, routingApplyCandidatePayload)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !resp.Success || resp.Mutation != "APPLIED" || !resp.Applied || resp.Rollback != "NOT_NEEDED" || !resp.XrayValid {
		t.Fatalf("unexpected apply response: %+v", resp)
	}
	if resp.Snapshot == "" || !strings.Contains(resp.Snapshot, ".freenet-backups") {
		t.Fatalf("snapshot path missing: %+v", resp)
	}
	if _, err := os.Stat(filepath.Join(resp.Snapshot, "05_routing.json")); err != nil {
		t.Fatalf("snapshot missing 05_routing.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(resp.Snapshot, "06_policy.json")); err != nil {
		t.Fatalf("snapshot missing 06_policy.json: %v", err)
	}
	if resp.Before["05_routing.json"] == "" || resp.After["05_routing.json"] == "" || resp.Before["05_routing.json"] == resp.After["05_routing.json"] {
		t.Fatalf("routing hashes must describe delta: %+v", resp)
	}
	after05, err := os.ReadFile(filepath.Join(dir, "05_routing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after05), "candidate.example") {
		t.Fatalf("candidate routing was not applied: %s", string(after05))
	}
	after04, err := os.ReadFile(filepath.Join(dir, "04_outbounds.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before04, after04) {
		t.Fatal("routing apply mutated 04_outbounds.json")
	}
	if strings.Contains(rec.Body.String(), "SECRET-UUID-MUST-NOT-LEAK") || strings.Contains(rec.Body.String(), "vless://") || strings.Contains(rec.Body.String(), "subscription") {
		t.Fatalf("apply response leaked secret-bearing content: %s", rec.Body.String())
	}
}

func TestRoutingApplyCandidateValidationFailureDoesNotWrite(t *testing.T) {
	a, dir := routingTestApp(t)
	t.Setenv("FREENET_XRAY_BIN", installCountingRoutingXray(t, "1"))
	before05, _ := os.ReadFile(filepath.Join(dir, "05_routing.json"))
	before06, _ := os.ReadFile(filepath.Join(dir, "06_policy.json"))

	rec, resp := performRoutingApply(t, a, routingApplyCandidatePayload)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if resp.Mutation != "NONE" || resp.Rollback != "NOT_NEEDED" || resp.Applied {
		t.Fatalf("pre-validation failure must not mutate: %+v", resp)
	}
	after05, _ := os.ReadFile(filepath.Join(dir, "05_routing.json"))
	after06, _ := os.ReadFile(filepath.Join(dir, "06_policy.json"))
	if !bytes.Equal(before05, after05) || !bytes.Equal(before06, after06) {
		t.Fatal("candidate validation failure mutated live files")
	}
}

func TestRoutingApplyPostValidationFailureRestoresBackup(t *testing.T) {
	a, dir := routingTestApp(t)
	t.Setenv("FREENET_XRAY_BIN", installCountingRoutingXray(t, "2"))
	before05, _ := os.ReadFile(filepath.Join(dir, "05_routing.json"))
	before06, _ := os.ReadFile(filepath.Join(dir, "06_policy.json"))

	rec, resp := performRoutingApply(t, a, routingApplyCandidatePayload)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if resp.Success || resp.Mutation != "ROLLED_BACK" || resp.Rollback != "SUCCESS" || resp.Applied {
		t.Fatalf("post-validation failure must rollback successfully: %+v", resp)
	}
	after05, _ := os.ReadFile(filepath.Join(dir, "05_routing.json"))
	after06, _ := os.ReadFile(filepath.Join(dir, "06_policy.json"))
	if !bytes.Equal(before05, after05) || !bytes.Equal(before06, after06) {
		t.Fatal("rollback did not restore live managed files")
	}
}

func TestRoutingApplyRollbackValidationFailureStops(t *testing.T) {
	a, dir := routingTestApp(t)
	t.Setenv("FREENET_XRAY_BIN", installCountingRoutingXray(t, "2", "3"))
	before05, _ := os.ReadFile(filepath.Join(dir, "05_routing.json"))
	before06, _ := os.ReadFile(filepath.Join(dir, "06_policy.json"))

	rec, resp := performRoutingApply(t, a, routingApplyCandidatePayload)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if resp.Mutation != "STOP" || resp.Rollback != "FAILED" {
		t.Fatalf("rollback validation failure must STOP: %+v", resp)
	}
	after05, _ := os.ReadFile(filepath.Join(dir, "05_routing.json"))
	after06, _ := os.ReadFile(filepath.Join(dir, "06_policy.json"))
	if !bytes.Equal(before05, after05) || !bytes.Equal(before06, after06) {
		t.Fatal("rollback restore should still put bytes back before reporting failed validation")
	}
}

func TestRoutingApplyRejectsUnsafeRequestsBeforeMutation(t *testing.T) {
	a, dir := routingTestApp(t)
	t.Setenv("FREENET_XRAY_BIN", filepath.Join(t.TempDir(), "must-not-run"))
	before05, _ := os.ReadFile(filepath.Join(dir, "05_routing.json"))

	tests := []struct {
		name        string
		contentType string
		origin      string
		body        string
		wantStatus  int
	}{
		{name: "cross origin", contentType: "application/json", origin: "http://attacker.invalid", body: routingApplyCandidatePayload, wantStatus: http.StatusForbidden},
		{name: "wrong content type", contentType: "text/plain", origin: "http://router", body: routingApplyCandidatePayload, wantStatus: http.StatusUnsupportedMediaType},
		{name: "unknown field", contentType: "application/json", origin: "http://router", body: `{"routing":{"routing":{}},"policy":{"policy":{}},"extra":true}`, wantStatus: http.StatusBadRequest},
		{name: "trailing json", contentType: "application/json", origin: "http://router", body: routingApplyCandidatePayload + ` {}`, wantStatus: http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://router/api/routing/apply", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.contentType)
			req.Header.Set("Origin", tc.origin)
			rec := httptest.NewRecorder()
			a.handleRoutingConfigApply(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			after05, _ := os.ReadFile(filepath.Join(dir, "05_routing.json"))
			if !bytes.Equal(before05, after05) {
				t.Fatal("unsafe request mutated live routing file")
			}
		})
	}
}
