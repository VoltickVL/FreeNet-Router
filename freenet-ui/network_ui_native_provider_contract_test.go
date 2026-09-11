package main

import (
	"strings"
	"testing"
)

func TestNetworkUIHasExplicitNativeProviderSelector(t *testing.T) {
	b, err := webFS.ReadFile("web/vpn-ux-fix.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"nativeDNSProviderSelect",
		"native_dns_provider",
		"Текущие DNS роутера",
		"Яндекс Basic",
		"77.88.8.8",
		"77.88.8.1",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("UI contract missing %q", want)
		}
	}
	compact := compactJSContract(s)
	for _, want := range []string{"option.hidden=true", "option.disabled=true"} {
		if !strings.Contains(compact, want) {
			t.Fatalf("UI contract missing %q", want)
		}
	}
}

func TestNetworkUILegacyAutoAndCustomAreNotSelectable(t *testing.T) {
	b, err := webFS.ReadFile("web/vpn-ux-fix.js")
	if err != nil {
		t.Fatal(err)
	}
	compact := compactJSContract(string(b))
	if !strings.Contains(compact, "for(constvalueof['auto','custom'])") {
		t.Fatal("legacy Auto/Custom selector suppression is missing")
	}
}
