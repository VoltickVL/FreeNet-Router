package main

import (
	"strings"
	"testing"
)

func TestControlCenter2ShellNavigationContract(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	for _, required := range []string{
		`class="app" hidden`,
		`class="sidebar"`,
		`class="topbar"`,
		`id="mobileMenuBtn"`,
		`.sidebar.open`,
		`data-page="overview"`,
		`data-page="vpn"`,
		`data-page="subscription"`,
		`data-page="network"`,
		`data-page="automation"`,
		`data-page="system"`,
		`data-page="access"`,
		`data-page-view="overview"`,
		`data-page-view="subscription"`,
		`data-page-view="system"`,
		`data-page-view="access"`,
		"Обновление FreeNet",
		"Доступ и безопасность",
		"Управление SSH — готовится",
		"Сменить пароль — готовится",
		"Web Update — готовится",
	} {
		if !strings.Contains(ui, required) {
			t.Fatalf("Control Center 2.0 shell missing %q", required)
		}
	}
}

func TestDashboardKeepsPrimaryVPNActionsAboveSettings(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	overview := strings.Index(ui, `data-page-view="overview"`)
	subscription := strings.Index(ui, `data-page-view="subscription"`)
	quick := strings.Index(ui, `id="quickActionsSection"`)
	profiles := strings.Index(ui, `id="profilesTrigger"`)
	if overview < 0 || subscription < 0 || quick < 0 || profiles < 0 {
		t.Fatal("dashboard structure not found")
	}
	if quick < overview || quick > subscription || profiles < overview || profiles > subscription {
		t.Fatal("primary VPN actions and exact Extra selector must stay on Dashboard")
	}
}

func TestSubscriptionAndNetworkAreSeparatePages(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	if strings.Count(ui, `data-page-view="subscription"`) != 1 || strings.Count(ui, `data-page-view="network"`) != 1 {
		t.Fatal("Subscription and Network must each have a dedicated page")
	}
}

func TestSystemAndAccessHaveHonestFutureCapabilitySlots(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	for _, want := range []string{
		"Web Update — готовится",
		"Управление SSH — готовится",
		"Сменить пароль — готовится",
		"Пароль Control Center не переиспользуется как SSH credential",
	} {
		if !strings.Contains(ui, want) {
			t.Fatalf("future capability slot missing %q", want)
		}
	}
	if strings.Contains(ui, "raw shell terminal") {
		t.Fatal("Control Center 2.0 must not introduce a raw shell shortcut")
	}
}

func TestPodryadProductDecisionIsVisibleWithoutClaimingRuntimeAcceptance(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	for _, want := range []string{
		"Подряд использует подтверждённую Management базовую policy Ростелекома",
		"отдельный runtime acceptance",
	} {
		if !strings.Contains(ui, want) {
			t.Fatalf("Podryad product decision hint missing %q", want)
		}
	}
}

func TestDashboardUsesUserFacingHealthLabels(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	for _, want := range []string{"VPN работает", "DNS защищён", "XKeen работает", "FreeNet готов"} {
		if !strings.Contains(ui, want) {
			t.Fatalf("dashboard health label missing %q", want)
		}
	}
	if strings.Contains(ui, `<span>dns-out</span>`) {
		t.Fatal("implementation label dns-out must not be a first-layer Dashboard status")
	}
}
