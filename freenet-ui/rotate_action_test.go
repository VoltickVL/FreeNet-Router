package main

import (
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
		`data-action="rotate"`,
		`Сменить сервер в текущей стране`,
		`Ищем другой VPN-сервер…`,
		`requestedAction!=='rotate'`,
		`Если альтернативы нет, конфигурация не изменится.`,
	} {
		if !strings.Contains(ui, want) {
			t.Fatalf("rotate UI contract missing %q", want)
		}
	}
}
