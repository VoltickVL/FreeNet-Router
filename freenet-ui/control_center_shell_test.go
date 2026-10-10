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
		`data-page="settings"`,
		`data-page="network"`,
		`data-page="journal"`,
		`data-page="admin"`,
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
			t.Fatalf("Control Center shell missing %q", required)
		}
	}

	navStart := strings.Index(ui, `<nav class="nav" aria-label="Навигация Control Center">`)
	if navStart < 0 {
		t.Fatal("canonical source navigation missing")
	}
	navEndRel := strings.Index(ui[navStart:], `</nav>`)
	if navEndRel < 0 {
		t.Fatal("canonical source navigation end missing")
	}
	nav := ui[navStart : navStart+navEndRel+len(`</nav>`)]
	for _, retired := range []string{`data-page="vpn"`, `data-page="subscription"`, `data-page="automation"`, `data-page="system"`, `data-page="access"`} {
		if strings.Contains(nav, retired) {
			t.Fatalf("retired navigation route leaked into source shell: %s", retired)
		}
	}
	if got := strings.Count(nav, `data-freenet-shell="1"`); got != 5 {
		t.Fatalf("source shell must ship five final navigation icons, got %d", got)
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
	overviewStart := strings.Index(ui, `data-page-view="overview"`)
	overviewEnd := strings.Index(ui, `data-page-view="vpn"`)
	subscriptionStart := strings.Index(ui, `data-page-view="subscription"`)
	subscriptionEnd := strings.Index(ui, `data-page-view="network"`)
	if overviewStart < 0 || overviewEnd < 0 || subscriptionStart < 0 || subscriptionEnd < 0 {
		t.Fatal("page ranges not found")
	}
	overview := ui[overviewStart:overviewEnd]
	subscription := ui[subscriptionStart:subscriptionEnd]
	if strings.Contains(overview, `id="subscriptionInput"`) {
		t.Fatal("subscription key input must not stay on Dashboard")
	}
	if !strings.Contains(subscription, `id="subscriptionInput"`) || !strings.Contains(subscription, `id="refreshProfilesBtn"`) {
		t.Fatal("subscription controls must live on dedicated Subscription page")
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
		"Основные действия доступны без SSH",
	} {
		if !strings.Contains(ui, want) {
			t.Fatalf("future capability slot missing %q", want)
		}
	}
	if strings.Contains(ui, "raw shell terminal") {
		t.Fatal("Control Center 2.0 must not introduce a raw shell shortcut")
	}
}

func TestDNSAndRoutingRemainSeparateSurfaces(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	if !strings.Contains(ui, "Routing policy настраивается отдельно явными правилами.") {
		t.Fatal("DNS UI must keep routing explicitly separate")
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
