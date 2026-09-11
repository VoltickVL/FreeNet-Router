package main

import (
	"strings"
	"testing"
)

func TestPolicyUIPreviewUsesAuthenticatedReadOnlyGeodataAPI(t *testing.T) {
	b, err := webFS.ReadFile("web/vpn-ux-fix.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"policyBuilderPreview",
		"Конструктор правил",
		"Домен / GeoSite",
		"IP / GeoIP",
		"/api/geodata/files",
		"/api/geodata/search?kind=",
		"DIRECT",
		"VPN",
		"BLOCK",
		"Payload routing",
		"Split DNS",
		"browser-only draft",
		"MUTATION: NONE",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("policy preview missing %q", want)
		}
	}
}

func TestPolicyUIPreviewHasNoMutationSurface(t *testing.T) {
	b, err := webFS.ReadFile("web/vpn-ux-fix.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	start := strings.Index(s, "const policyPreview =")
	end := strings.Index(s[start:], "\n  function mount()")
	if start < 0 || end < 0 {
		t.Fatal("cannot isolate policy preview block")
	}
	block := s[start : start+end]
	for _, forbidden := range []string{
		"/api/network-profile/apply",
		"method: 'POST'",
		"method: \"POST\"",
		"Применить",
		"Сохранить",
	} {
		if strings.Contains(block, forbidden) {
			t.Fatalf("policy preview exposes mutation surface %q", forbidden)
		}
	}
}

func TestPolicyUIPreviewExplainsDNSApplicability(t *testing.T) {
	b, err := webFS.ReadFile("web/vpn-ux-fix.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"dns-direct",
		"dns-vless",
		"не применяется к IP selector",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("policy leg contract missing %q", want)
		}
	}
	if !strings.Contains(compactJSContract(s), "selected.kind==='domain'||selected.kind==='geosite'") {
		t.Fatal("policy leg contract missing domain/geosite DNS applicability guard")
	}
}
