package main

import (
	"os"
	"strings"
	"testing"
)

func TestApprovedOverviewPixelContract(t *testing.T) {
	data, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	mustContain := []string{
		"grid-template-columns:minmax(312px,342px) minmax(0,1fr)",
		".vpn-current-panel .best-v4-pill{min-height:78px",
		".vpn-current-panel>#bestServerCheckCurrent{width:100%;min-height:48px",
		".vpn-option{min-height:145px",
		".vpn-option-title .flag-icon{width:34px;height:24px",
		".vpn-state-badge{display:inline-flex;align-items:center;gap:7px;min-height:34px",
		".vpn-detail-chip{display:inline-flex;align-items:center;gap:6px;min-height:28px",
		".best-v4-status.summary{display:flex;align-items:flex-start;gap:10px}",
		".topbar.overview-approved .top-status{display:none!important}",
		"makeIcon(key, 'metric-icon')",
		"makeIcon(state.icon, 'status-icon')",
	}
	for _, needle := range mustContain {
		if !strings.Contains(s, needle) {
			t.Fatalf("approved Overview design contract missing %q", needle)
		}
	}
	for _, needle := range []string{
		"topbar.insertBefore(manual",
		"classList.add('fn-topbar-vpn-picker')",
		"fn-clean-flag",
		"const flagSVG = code =>",
		"fn-manual-shortcut",
		"fn-health-pill",
	} {
		if strings.Contains(s, needle) {
			t.Fatalf("retired Overview visual owner returned: %q", needle)
		}
	}

	picker, err := os.ReadFile("web/vpn-picker-v2.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"fnVpnPickerV2Panel", "fnVpnPickerV2Resize", "#bestServerAdvanced{display:none!important}"} {
		if !strings.Contains(string(picker), want) {
			t.Fatalf("canonical picker visual contract missing %q", want)
		}
	}
	flags, err := os.ReadFile("web/vpn-ux-fix.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(flags), "window.FreeNetFlags") {
		t.Fatal("canonical flag visual owner missing")
	}
}
