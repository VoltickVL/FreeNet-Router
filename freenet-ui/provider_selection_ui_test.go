package main

import (
	"strings"
	"testing"
)

func TestProviderSelectionUIContract(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	for _, required := range []string{
		`id="providerPlan"`,
		`id="applyProviderBtn"`,
		`id="providerNotice"`,
		"selectedProviderID",
		"selectProviderProfile",
		"applyProviderProfile",
		"provider_profile_id=",
		"operation:'provider'",
		"profile_id:selectedProviderID",
		"candidate_xray_valid",
		"PRIMARY ERROR:",
		"ROLLBACK:",
		"Не повторяйте вслепую",
	} {
		if !strings.Contains(ui, required) {
			t.Fatalf("provider selection UI missing %q", required)
		}
	}
}

func TestProviderRowsUseTextContentAndExplicitButton(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	start := strings.Index(ui, "function renderExtraProfiles")
	end := strings.Index(ui[start:], "async function loadStatus")
	if start < 0 || end < 0 {
		t.Fatal("dynamic profile renderer not found")
	}
	body := ui[start : start+end]
	for _, required := range []string{
		"name.textContent=p.name",
		"endpoint.textContent=formatProfileEndpoint(p)",
		"pick.textContent=",
		"pick.addEventListener('click'",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("profile renderer missing safe selection contract %q", required)
		}
	}
	if strings.Contains(body, "innerHTML") {
		t.Fatal("provider-controlled profile data must not use innerHTML")
	}
}

func TestProviderApplyRequiresPlanReadyInUI(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	if !strings.Contains(ui, "!selectedProviderID||!providerPlanReady||providerApplying") {
		t.Fatal("provider apply must be locally gated by selected id and validated plan")
	}
	if !strings.Contains(ui, "pp&&pp.success&&pp.candidate_xray_valid&&pp.mutation==='NONE'") {
		t.Fatal("provider plan readiness must require validated read-only candidate")
	}
}
