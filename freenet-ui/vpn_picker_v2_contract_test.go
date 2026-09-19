package main

import (
	"os"
	"strings"
	"testing"
)

func TestVPNPickerV2CanonicalContract(t *testing.T) {
	data, err := webFS.ReadFile("web/vpn-picker-v2.js")
	if err != nil { t.Fatal(err) }
	js := string(data)
	for _, required := range []string{
		"__freenetVPNPickerV2Mounted",
		"getBoundingClientRect()",
		"flag-icon ",
		"flag-' + code",
		"#bestCurrentFlag",
		"#bestCurrentName",
		"countryNames",
		"xray-vpn-dns-freenet",
		"position:fixed!important",
		"overflow-x:hidden!important",
		"selectProviderProfile(profile)",
		"Сейчас подключено",
		"Подключено",
	} {
		if !strings.Contains(js, required) { t.Fatalf("VPN picker v2 missing %q", required) }
	}
	for _, forbidden := range []string{"🇧🇪", "🇩🇪", "🇳🇱", "/api/network-profile/apply", "method:'POST'", "method: 'POST'"} {
		if strings.Contains(js, forbidden) { t.Fatalf("VPN picker v2 must not contain %q", forbidden) }
	}
}

func TestVPNPickerV2DeliveryAndLegacyOwnerGate(t *testing.T) {
	mainData, err := os.ReadFile("main.go")
	if err != nil { t.Fatal(err) }
	mainSource := string(mainData)
	for _, required := range []string{
		"web/vpn-picker-v2.js",
		"window.__freenetVPNPickerV2=true;",
		"/vpn-picker-v2.js?v=v%s",
	} {
		if !strings.Contains(mainSource, required) { t.Fatalf("VPN picker v2 delivery missing %q", required) }
	}
	operationData, err := webFS.ReadFile("web/operation-coordinator.js")
	if err != nil { t.Fatal(err) }
	if !strings.Contains(string(operationData), "if (window.__freenetVPNPickerV2) return;") {
		t.Fatal("legacy Issue #562 picker owner is not gated under v2")
	}
}

func TestVPNExactConnectKeepsPrimaryCTALabel(t *testing.T) {
	data, err := webFS.ReadFile("web/vpn-ux-fix.js")
	if err != nil { t.Fatal(err) }
	js := string(data)
	for _, forbidden := range []string{
		"controls.connect.textContent = 'Проверяем…';",
		"controls.connect.textContent = 'Сервер недоступен';",
		"controls.connect.textContent = 'Подключаем…';",
	} {
		if strings.Contains(js, forbidden) { t.Fatalf("primary VPN CTA must stay stable, found %q", forbidden) }
	}
	if !strings.Contains(js, "controls.connect.textContent = 'Подключиться';") {
		t.Fatal("stable VPN connect CTA label missing")
	}
}
