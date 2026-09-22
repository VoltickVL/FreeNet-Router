package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSupportedActionIncludesRotate(t *testing.T) {
	for _, action := range []string{"update", "rotate", "de", "pl", "fi", "nl"} {
		if !supportedAction(action) {
			t.Fatalf("supportedAction(%q)=false", action)
		}
	}
	if supportedAction("unknown") {
		t.Fatal("unknown action must be rejected")
	}
}

func TestRotatePostcondition(t *testing.T) {
	before := statusResponse{CountryCode: "pl", Endpoint: "10.0.0.1:443", DNSOut: true}
	if err := rotateBaselineValid(before); err != nil {
		t.Fatalf("valid rotate baseline rejected: %v", err)
	}
	if err := validateActionPostcondition("rotate", before, statusResponse{CountryCode: "pl", Endpoint: "10.0.0.2:443", DNSOut: true}); err != nil {
		t.Fatalf("valid rotation rejected: %v", err)
	}
	if err := validateActionPostcondition("rotate", before, statusResponse{CountryCode: "pl", Endpoint: "10.0.0.1:443", DNSOut: true}); err == nil || !strings.Contains(err.Error(), "did not change") {
		t.Fatalf("same endpoint must fail, got %v", err)
	}
	if err := validateActionPostcondition("rotate", before, statusResponse{CountryCode: "de", Endpoint: "10.0.0.2:443", DNSOut: true}); err == nil || !strings.Contains(err.Error(), "profile group") {
		t.Fatalf("profile-group change must fail, got %v", err)
	}
	if err := validateActionPostcondition("rotate", before, statusResponse{CountryCode: "pl", Endpoint: "10.0.0.2:443", DNSOut: false}); err != nil {
		t.Fatalf("direct-DNS rotation without dns-out must be accepted, got %v", err)
	}
}

func TestCountrySwitchAllowsDirectDNSWithoutDNSOut(t *testing.T) {
	before := statusResponse{CountryCode: "pl", Endpoint: "10.0.0.1:443", DNSOut: false, ISP: "custom", DNSMode: "firmware"}
	if err := validateActionPostcondition("de", before, statusResponse{CountryCode: "de", Endpoint: "10.0.0.2:443", DNSOut: false, ISP: "custom", DNSMode: "firmware"}); err != nil {
		t.Fatalf("country switch with direct DNS rejected: %v", err)
	}
	if err := validateActionPostcondition("de", before, statusResponse{CountryCode: "de", Endpoint: "10.0.0.2:443", DNSOut: true, ISP: "custom", DNSMode: "xkeen"}); err != nil {
		t.Fatalf("country switch with split DNS rejected: %v", err)
	}
}

func TestCountrySwitchDoesNotUseNetworkProfileMutationSurface(t *testing.T) {
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	start := strings.Index(src, "func (a *app) runAction(action string)")
	end := strings.Index(src[start:], "func (a *app) takeSnapshot()")
	if start < 0 || end < 0 {
		t.Fatal("runAction source not found")
	}
	body := src[start : start+end]
	if !strings.Contains(body, "runCommand(ctx, a.cfg.VPNPath, action)") {
		t.Fatal("quick VPN action must delegate to the VPN lifecycle helper")
	}
	if strings.Contains(body, "writeNetworkProfileConfig") || strings.Contains(body, "applyNetworkProfile") {
		t.Fatal("quick VPN action must not mutate saved ISP/DNS profile")
	}
}

func TestRotateBaselineRequiresKnownProfileAndEndpoint(t *testing.T) {
	for _, s := range []statusResponse{
		{Endpoint: "10.0.0.1:443"},
		{CountryCode: "pl", Endpoint: "—"},
		{CountryCode: "pl"},
	} {
		if err := rotateBaselineValid(s); err == nil {
			t.Fatalf("invalid rotate baseline accepted: %+v", s)
		}
	}
}


func TestQuickVPNBusyDoesNotRollbackConcurrentOwnerState(t *testing.T) {
	dir := t.TempDir()
	filter := filepath.Join(dir, "profile.regex")
	out := filepath.Join(dir, "04_outbounds.json")
	vpn := filepath.Join(dir, "vpn")
	if err := os.WriteFile(filter, []byte("before-filter\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, []byte("{\"before\":true}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf 'other-owner-filter\\n' > " + filter + "\nprintf '{\"other_owner\":true}\\n' > " + out + "\necho 'another updater instance is already running' >&2\nexit 1\n"
	if err := os.WriteFile(vpn, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: config{
		VPNPath: vpn, FilterPath: filter, OutPath: out,
		LockPath: filepath.Join(dir, "no-preexisting-lock"), Timeout: 5 * time.Second,
	}}
	result := a.runAction("de")
	if result.Success || !strings.Contains(result.Error, "another VPN/Xray mutation is already running") {
		t.Fatalf("busy result=%+v", result)
	}
	gotFilter, _ := os.ReadFile(filter)
	gotOut, _ := os.ReadFile(out)
	if string(gotFilter) != "other-owner-filter\n" || !strings.Contains(string(gotOut), "other_owner") {
		t.Fatalf("busy child state was overwritten by outer rollback: filter=%q out=%q", gotFilter, gotOut)
	}
}

func TestRotateUIContract(t *testing.T) {
	indexData, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	fixData, err := webFS.ReadFile("web/vpn-ux-fix.js")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(indexData)
	fix := string(fixData)
	for _, want := range []string{
		`id="quickActionsSection"`,
		`id="quickNetworkGuard"`,
		`data-action="rotate"`,
		`Ищем другой VPN-сервер…`,
		`requestedAction!=='rotate'`,
		`Если альтернативы нет, конфигурация не изменится.`,
	} {
		if !strings.Contains(ui, want) {
			t.Fatalf("rotate/quick-action UI contract missing %q", want)
		}
	}
	for _, want := range []string{
		"Обновить профиль",
		"Сменить сервер",
		"VPN-действия не меняют ISP и DNS",
		"Текущий DNS-режим:",
	} {
		if !strings.Contains(fix, want) {
			t.Fatalf("VPN UX compatibility layer missing %q", want)
		}
	}
}
