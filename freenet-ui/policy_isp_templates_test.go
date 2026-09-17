package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPolicyISPPresetTemplatesFollowProductBaseline(t *testing.T) {
	presets, err := buildPolicyISPPresets()
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]policyISPPresetTemplate, len(presets))
	for _, preset := range presets {
		byID[preset.ID] = preset
		if preset.Compiled.Rules == nil || preset.Compiled.Payload == nil || preset.Compiled.DNS == nil {
			t.Fatalf("preset %s has nil compiled policy slices: %+v", preset.ID, preset.Compiled)
		}
	}

	for _, id := range []string{"vladlink", "alliancetelecom"} {
		preset := byID[id]
		if preset.YouTubeRoute != "direct" || len(preset.Rules) != 1 {
			t.Fatalf("%s template=%+v", id, preset)
		}
		rule := preset.Compiled.Rules[0]
		if rule.Selector.Kind != PolicySelectorGeoSite || rule.Selector.Value != "youtube" || rule.Action != PolicyActionDirect || rule.PayloadOutbound != "direct" || rule.DNSLeg != "dns-direct" {
			t.Fatalf("%s must compile YouTube DIRECT, got %+v", id, rule)
		}
	}

	rostelecom := byID["rostelecom"]
	if rostelecom.YouTubeRoute != "vpn" || len(rostelecom.Rules) != 1 {
		t.Fatalf("rostelecom template=%+v", rostelecom)
	}
	if rule := rostelecom.Compiled.Rules[0]; rule.Selector.Kind != PolicySelectorGeoSite || rule.Selector.Value != "youtube" || rule.Action != PolicyActionVPN || rule.PayloadOutbound != "vless-reality" || rule.DNSLeg != "dns-vless" {
		t.Fatalf("rostelecom must compile YouTube VPN, got %+v", rule)
	}

	for _, id := range []string{"auto", "podryad", "custom"} {
		preset := byID[id]
		if len(preset.Rules) != 0 || len(preset.Compiled.Rules) != 0 || len(preset.Compiled.Payload) != 0 || len(preset.Compiled.DNS) != 0 {
			t.Fatalf("%s must not inherit policy from another ISP: %+v", id, preset)
		}
	}
	if !strings.Contains(byID["podryad"].Message, "не наследует") {
		t.Fatalf("podryad must explain no inherited policy: %q", byID["podryad"].Message)
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
	if strings.Contains(rr.Body.String(), "uuid") || strings.Contains(rr.Body.String(), "subscription") || strings.Contains(rr.Body.String(), "vless://") {
		t.Fatal("ISP templates endpoint leaked secret-bearing content")
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
