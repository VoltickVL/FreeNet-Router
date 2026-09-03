package main

import (
	"os"
	"strings"
	"testing"
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
	if err := validateActionPostcondition("rotate", before, statusResponse{CountryCode: "pl", Endpoint: "10.0.0.2:443", DNSOut: false}); err == nil || !strings.Contains(err.Error(), "dns-out") {
		t.Fatalf("missing dns-out must fail, got %v", err)
	}
}

func TestCountrySwitchRequiresDNSOut(t *testing.T) {
	before := statusResponse{CountryCode: "pl", Endpoint: "10.0.0.1:443", DNSOut: true, ISP: "vladlink", DNSMode: "xkeen"}
	if err := validateActionPostcondition("de", before, statusResponse{CountryCode: "de", Endpoint: "10.0.0.2:443", DNSOut: true, ISP: "vladlink", DNSMode: "xkeen"}); err != nil {
		t.Fatalf("valid country switch rejected: %v", err)
	}
	if err := validateActionPostcondition("de", before, statusResponse{CountryCode: "de", Endpoint: "10.0.0.2:443", DNSOut: false, ISP: "vladlink", DNSMode: "xkeen"}); err == nil || !strings.Contains(err.Error(), "dns-out") {
		t.Fatalf("country switch without dns-out must fail, got %v", err)
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

func TestRotateUIContract(t *testing.T) {
	b, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(b)
	for _, want := range []string{
		`id="quickActionsSection"`,
		`id="quickNetworkGuard"`,
		`data-action="rotate"`,
		`Сменить сервер в текущей стране`,
		`Ищем другой VPN-сервер…`,
		`requestedAction!=='rotate'`,
		`Если альтернативы нет, конфигурация не изменится.`,
		`Сохраняются: ISP `,
		`DNS `,
		`FreeNet требует сохранённый dns-out`,
	} {
		if !strings.Contains(ui, want) {
			t.Fatalf("rotate/quick-action UI contract missing %q", want)
		}
	}
}
