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
		"ОСНОВНАЯ ОШИБКА:",
		"ОТКАТ:",
		"Не повторяйте вслепую",
	} {
		if !strings.Contains(ui, required) {
			t.Fatalf("в UI выбора VPN-профиля отсутствует контракт %q", required)
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
		t.Fatal("не найден renderer динамических VPN-профилей")
	}
	body := ui[start : start+end]
	for _, required := range []string{
		"name.textContent=p.name",
		"endpoint.textContent=formatProfileEndpoint(p)",
		"pick.textContent=",
		"pick.addEventListener('click'",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("в renderer профилей отсутствует безопасный контракт %q", required)
		}
	}
	if strings.Contains(body, "innerHTML") {
		t.Fatal("данные VPN-провайдера нельзя выводить через innerHTML")
	}
}

func TestProviderApplyRequiresPlanReadyInUI(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	if !strings.Contains(ui, "!selectedProviderID||!providerPlanReady||providerApplying") {
		t.Fatal("применение VPN-профиля должно требовать выбранный ID и проверенный план")
	}
	if !strings.Contains(ui, "pp&&pp.success&&pp.candidate_xray_valid&&pp.mutation==='NONE'") {
		t.Fatal("готовность provider plan должна требовать валидный read-only Xray-кандидат")
	}
}
