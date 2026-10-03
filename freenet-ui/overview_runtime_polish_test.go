package main

import (
	"os"
	"strings"
	"testing"
)

func TestOverviewRuntimePolishContract(t *testing.T) {
	data, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, needle := range []string{
		"FreeNetFinalRuntimePolish",
		"fn-best-alternative",
		"Лучший из вариантов",
		"fn-long-value",
		".topbar.overview-approved .top-status{display:none!important}",
		"vpn-current-panel .best-v4-pill{grid-template-columns:20px minmax(0,1fr)!important",
	} {
		if !strings.Contains(s, needle) {
			t.Fatalf("runtime Overview polish contract missing %q", needle)
		}
	}
	for _, forbidden := range []string{
		"Всё работает", "Сервер вручную", "fn-health-pill", "fn-manual-shortcut",
		"fn-clean-flag", "const flagSVG = code =>", "fnVpnPickerPopover",
		"classList.add('fn-topbar-vpn-picker')",
	} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("retired Overview/picker visual owner still present: %q", forbidden)
		}
	}
	picker, err := os.ReadFile("web/vpn-picker-v2.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"fnVpnPickerV2Panel", "xray-vpn-dns-freenet", "#bestServerAdvanced{display:none!important}"} {
		if !strings.Contains(string(picker), want) {
			t.Fatalf("canonical picker v2 contract missing %q", want)
		}
	}
}
