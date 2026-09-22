package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func writeRoutingTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func routingTestApp(t *testing.T) (*app, string) {
	t.Helper()
	dir := t.TempDir()
	writeRoutingTestFile(t, filepath.Join(dir, "04_outbounds.json"), `{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"192.0.2.10","users":[{"id":"SECRET-UUID-MUST-NOT-LEAK"}]}]}}]}`, 0600)
	writeRoutingTestFile(t, filepath.Join(dir, "05_routing.json"), `{"routing":{"domainStrategy":"AsIs","rules":[{"type":"field","domain":["geosite:youtube"],"outboundTag":"direct"}]}}`, 0600)
	writeRoutingTestFile(t, filepath.Join(dir, "06_policy.json"), `{"policy":{"levels":{"0":{"handshake":4}}}}`, 0600)
	return &app{cfg: config{OutPath: filepath.Join(dir, "04_outbounds.json"), GeoDataDir: dir}}, dir
}

func TestRoutingConfigGetExposesOnlyManagedSections(t *testing.T) {
	a, dir := routingTestApp(t)
	before04, err := os.ReadFile(filepath.Join(dir, "04_outbounds.json"))
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	a.handleRoutingConfigGet(rec, httptest.NewRequest(http.MethodGet, "http://router/api/routing/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body routingConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Success || body.Mutation != "NONE" || !body.RoutingPresent || !body.PolicyPresent {
		t.Fatalf("unexpected response: %+v", body)
	}
	if body.RoutingSHA256 == "" || body.PolicySHA256 == "" {
		t.Fatal("managed section hashes are required")
	}
	if strings.Contains(rec.Body.String(), "SECRET-UUID-MUST-NOT-LEAK") || strings.Contains(rec.Body.String(), "outbounds") {
		t.Fatal("04_outbounds or credentials leaked through routing API")
	}
	after04, err := os.ReadFile(filepath.Join(dir, "04_outbounds.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before04, after04) {
		t.Fatal("read-only routing snapshot mutated 04_outbounds.json")
	}
}

func TestRoutingValidateUsesTemporaryCandidateWithoutLiveMutation(t *testing.T) {
	a, dir := routingTestApp(t)
	fakeXray := filepath.Join(t.TempDir(), "xray")
	writeRoutingTestFile(t, fakeXray, `#!/bin/sh
set -eu
[ "$1" = "run" ]
[ "$2" = "-test" ]
[ "$3" = "-confdir" ]
dir="$4"
[ -f "$dir/04_outbounds.json" ]
[ -f "$dir/05_routing.json" ]
[ -f "$dir/06_policy.json" ]
grep -q 'candidate.example' "$dir/05_routing.json"
grep -q 'SECRET-UUID-MUST-NOT-LEAK' "$dir/04_outbounds.json"
exit 0
`, 0700)
	t.Setenv("FREENET_XRAY_BIN", fakeXray)

	before05, err := os.ReadFile(filepath.Join(dir, "05_routing.json"))
	if err != nil {
		t.Fatal(err)
	}
	before06, err := os.ReadFile(filepath.Join(dir, "06_policy.json"))
	if err != nil {
		t.Fatal(err)
	}

	payload := `{"routing":{"routing":{"domainStrategy":"AsIs","rules":[{"type":"field","domain":["domain:candidate.example"],"outboundTag":"direct"}]}},"policy":{"policy":{"levels":{"0":{"handshake":5}}}}}`
	req := httptest.NewRequest(http.MethodPost, "http://router/api/routing/validate", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://router")
	rec := httptest.NewRecorder()
	a.handleRoutingConfigValidate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body routingValidateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Success || !body.XrayValid || body.Mutation != "NONE" {
		t.Fatalf("unexpected validation response: %+v", body)
	}

	after05, _ := os.ReadFile(filepath.Join(dir, "05_routing.json"))
	after06, _ := os.ReadFile(filepath.Join(dir, "06_policy.json"))
	if !bytes.Equal(before05, after05) || !bytes.Equal(before06, after06) {
		t.Fatal("candidate validation mutated live 05/06 config")
	}
}

func TestRoutingValidateRejectsUnsafeRequestsBeforeXray(t *testing.T) {
	a, _ := routingTestApp(t)
	t.Setenv("FREENET_XRAY_BIN", filepath.Join(t.TempDir(), "must-not-run"))

	tests := []struct {
		name        string
		contentType string
		origin      string
		body        string
		wantStatus  int
	}{
		{name: "cross origin", contentType: "application/json", origin: "http://attacker.invalid", body: `{"routing":{"routing":{}},"policy":{"policy":{}}}`, wantStatus: http.StatusForbidden},
		{name: "wrong content type", contentType: "text/plain", origin: "http://router", body: `{}`, wantStatus: http.StatusUnsupportedMediaType},
		{name: "unknown field", contentType: "application/json", origin: "http://router", body: `{"routing":{"routing":{}},"policy":{"policy":{}},"extra":true}`, wantStatus: http.StatusBadRequest},
		{name: "trailing json", contentType: "application/json", origin: "http://router", body: `{"routing":{"routing":{}},"policy":{"policy":{}}} {}`, wantStatus: http.StatusBadRequest},
		{name: "wrong routing root", contentType: "application/json", origin: "http://router", body: `{"routing":{"dns":{}},"policy":{"policy":{}}}`, wantStatus: http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://router/api/routing/validate", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.contentType)
			req.Header.Set("Origin", tc.origin)
			rec := httptest.NewRecorder()
			a.handleRoutingConfigValidate(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"mutation":"NONE"`) {
				t.Fatalf("failure lost no-mutation contract: %s", rec.Body.String())
			}
		})
	}
}


func TestRoutingApplyStopsBeforeMutationWhenSharedLockIsBusy(t *testing.T) {
	a, dir := routingTestApp(t)
	fakeXray := filepath.Join(t.TempDir(), "xray")
	writeRoutingTestFile(t, fakeXray, "#!/bin/sh\nexit 0\n", 0700)
	t.Setenv("FREENET_XRAY_BIN", fakeXray)
	lock := filepath.Join(t.TempDir(), "vpn.lock")
	t.Setenv("FREENET_LOCK_DIR", lock)
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lock, "pid"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	before05, _ := os.ReadFile(filepath.Join(dir, "05_routing.json"))
	before06, _ := os.ReadFile(filepath.Join(dir, "06_policy.json"))
	payload := `{"routing":{"routing":{"domainStrategy":"AsIs","rules":[{"type":"field","domain":["domain:busy.example"],"outboundTag":"direct"}]}},"policy":{"policy":{"levels":{"0":{"handshake":9}}}}}`
	req := httptest.NewRequest(http.MethodPost, "http://router/api/routing/apply", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://router")
	rec := httptest.NewRecorder()
	a.handleRoutingConfigApply(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"mutation":"NONE"`) || !strings.Contains(rec.Body.String(), `"rollback":"NOT_APPLIED"`) {
		t.Fatalf("busy response lost no-mutation contract: %s", rec.Body.String())
	}
	after05, _ := os.ReadFile(filepath.Join(dir, "05_routing.json"))
	after06, _ := os.ReadFile(filepath.Join(dir, "06_policy.json"))
	if !bytes.Equal(before05, after05) || !bytes.Equal(before06, after06) {
		t.Fatal("routing files changed while canonical mutation lock was busy")
	}
}

func TestRoutingV2AssetContract(t *testing.T) {
	s := string(routingV2Asset)
	for _, want := range []string{
		`data-mode="rules"`, `data-mode="config"`, `DIRECT`, `VPN`, `BLOCK`,
		`domain`, `geosite`, `ip`, `cidr`, `geoip`,
		`/api/policy/compile`, `/api/geodata/search`, `/api/routing/config`, `/api/routing/validate`,
		`05_routing.json`, `06_policy.json`, `Проверить Xray`, `MUTATION: NONE`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("Routing v2 asset missing %q", want)
		}
	}
	for _, forbidden := range []string{`/api/routing/apply`, `04_outbounds.json\"`, `subscription URL`, `VLESS UUID`} {
		if forbidden == `04_outbounds.json\"` {
			continue
		}
		// Human-facing safety copy may mention the secret classes, but there must be
		// no live apply endpoint in this candidate-only cycle.
		if strings.HasPrefix(forbidden, "/api/") && strings.Contains(s, forbidden) {
			t.Fatalf("Routing v2 candidate cycle unexpectedly exposes %q", forbidden)
		}
	}
}
