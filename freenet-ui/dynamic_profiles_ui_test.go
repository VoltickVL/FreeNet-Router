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
		`id="profileSearch"`,
		`id="profilesSelect"`,
		`id="selectedProfileCard"`,
		`id="refreshProfilesBtn"`,
		"renderExtraProfiles",
		"renderProfileOptions",
		"extra_profiles",
		"profiles_error",
		"formatProfileEndpoint",
		"document.createElement('option')",
		"option.textContent=",
	} {
		if !strings.Contains(ui, required) {
			t.Fatalf("dynamic profiles UI missing %q", required)
		}
	}
	if strings.Contains(ui, "profilesList.innerHTML") || strings.Contains(ui, "profilesSelect.innerHTML") {
		t.Fatal("provider-controlled profile labels must not be rendered through innerHTML")
	}
	if strings.Contains(ui, "profile-row") {
		t.Fatal("Extra profiles must not render as an always-expanded row list")
	}
}

func TestProfileSearchFiltersClientSideOptions(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	for _, required := range []string{
		"profileSearch').addEventListener('input',renderProfileOptions)",
		"hay.includes(query)",
		"extraProfiles.filter",
	} {
		if !strings.Contains(ui, required) {
			t.Fatalf("searchable profile selector missing %q", required)
		}
	}
}

func TestSavingSubscriptionRefreshesProfiles(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	start := strings.Index(ui, "async function saveSubscription()")
	end := strings.Index(ui[start:], "async function selectProviderProfile")
	if start < 0 || end < 0 {
		t.Fatal("subscription UI functions not found")
	}
	body := ui[start : start+end]
	if !strings.Contains(body, "await loadNetworkPlan(") {
		t.Fatal("successful subscription save must refresh dynamic Extra profiles")
	}
}
