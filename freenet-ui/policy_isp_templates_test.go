package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPolicyISPPresetTemplatesAreExplicitOnly(t *testing.T) {
	presets, err := buildPolicyISPPresets()
	if err != nil {
		t.Fatal(err)
	}
	if len(presets) != len(policyISPPresetSpecs) {
		t.Fatalf("presets=%d specs=%d", len(presets), len(policyISPPresetSpecs))
	}
	for _, preset := range presets {
		if preset.Compiled.Rules == nil || preset.Compiled.Payload == nil || preset.Compiled.DNS == nil {
			t.Fatalf("preset %s has nil compiled policy slices: %+v", preset.ID, preset.Compiled)
		}
		if len(preset.Rules) != 0 || len(preset.Compiled.Rules) != 0 || len(preset.Compiled.Payload) != 0 || len(preset.Compiled.DNS) != 0 {
			t.Fatalf("preset %s must not create ISP-derived routing policy: %+v", preset.ID, preset)
		}
		message := strings.ToLower(preset.Message)
		if strings.Contains(message, "baseline") || strings.Contains(message, "youtube →") || strings.Contains(message, "youtube ->") {
			t.Fatalf("preset %s contains old YouTube baseline wording: %q", preset.ID, preset.Message)
		}
	}
}

func TestPolicyISPPresetsEndpointIsReadOnly(t *testing.T) {
	a := &app{}
	req := httptest.NewRequest(http.MethodGet, "http://router/api/policy/isp-presets", nil)
	rr := httptest.NewRecorder()
	a.handlePolicyISPPresets(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp policyISPPresetsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success || resp.Mutation != "NONE" || len(resp.Presets) != len(policyISPPresetSpecs) {
		t.Fatalf("unexpected response: %+v", resp)
	}
	body := rr.Body.String()
	if strings.Contains(body, "youtube_route") || strings.Contains(body, "\"youtube\"") || strings.Contains(body, "uuid") || strings.Contains(body, "subscription") || strings.Contains(body, "vless://") {
		t.Fatal("ISP templates endpoint leaked old routing baseline or secret-bearing content")
	}
}

func TestPolicyPreviewRegistrationIncludesISPPresets(t *testing.T) {
	a := &app{}
	mux := http.NewServeMux()
	registerPolicyPreviewAPI(mux, a)
	req := httptest.NewRequest(http.MethodGet, "http://router/api/policy/isp-presets", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code == http.StatusNotFound {
		t.Fatal("/api/policy/isp-presets was not registered")
	}
}
