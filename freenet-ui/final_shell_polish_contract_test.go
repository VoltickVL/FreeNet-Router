package main

import (
	"strings"
	"testing"
)

func TestFinalShellPolishUsesVectorBrandAndPresentationOnlyVPNLabels(t *testing.T) {
	ux, err := webFS.ReadFile("web/accepted-ux.js")
	if err != nil {
		t.Fatal(err)
	}
	index, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}

	js := string(ux)
	html := string(index)

	for _, want := range []string{
		"brandLockupSVG",
		`<svg viewBox="0 0 140 28" role="img" aria-label="FreeNet"`,
		"fn-brand-wordmark fn-brand-lockup-svg",
		"mountVectorBrand(q('.sidebar>.brand'))",
		"mountVectorBrand(q('#authSection .brand'))",
		"function cleanProfileLabel(value)",
		`text = text.replace(/^[A-Za-z]{2}\s+/, '')`,
		"Выбрать VPN-сервер",
		"renderProfileOptions.__freenetShellPolish",
		"#profilesMenu .profile-option-main",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("final shell polish contract missing %q", want)
		}
	}

	// The SVG wordmark must be actual geometry, not browser-rendered text.
	if !strings.Contains(js, `<path d="M34 5v18M34 5h12M34 13h9.5"/>`) {
		t.Fatal("vector wordmark path geometry missing")
	}
	if strings.Contains(js, `<text`) {
		t.Fatal("deterministic wordmark must not use SVG text/font rendering")
	}

	// Filtering/identity/API semantics stay on canonical raw profile data; cleanup is presentation-only.
	for _, want := range []string{
		"const filtered=extraProfiles.filter",
		"main.textContent=p.name||'Extra-профиль'",
		"option.dataset.profileId=p.id||''",
		"selectedProviderID=p.id||''",
		"profile_id:selectedProviderID",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("canonical profile identity/search contract missing %q", want)
		}
	}

	if strings.Contains(js, "MutationObserver") {
		t.Fatal("final shell polish must remain event-driven")
	}
}
