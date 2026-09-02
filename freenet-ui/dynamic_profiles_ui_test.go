package main

import (
	"strings"
	"testing"
)

func TestDynamicProfilesUIContract(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	for _, required := range []string{
		`id="profilesList"`,
		`id="refreshProfilesBtn"`,
		"renderExtraProfiles",
		"extra_profiles",
		"profiles_error",
		"formatProfileEndpoint",
		"document.createElement('div')",
		"textContent=p.name",
	} {
		if !strings.Contains(ui, required) {
			t.Fatalf("dynamic profiles UI missing %q", required)
		}
	}
	if strings.Contains(ui, "profilesList.innerHTML") {
		t.Fatal("provider-controlled profile labels must not be rendered through innerHTML")
	}
}

func TestSavingSubscriptionRefreshesProfiles(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	start := strings.Index(ui, "async function saveSubscription()")
	end := strings.Index(ui[start:], "async function saveNetworkProfile()")
	if start < 0 || end < 0 {
		t.Fatal("subscription UI functions not found")
	}
	body := ui[start : start+end]
	if !strings.Contains(body, "await loadNetworkPlan(") {
		t.Fatal("successful subscription save must refresh dynamic Extra profiles")
	}
}
