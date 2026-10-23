package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestCurrentExactProfileLocationFromInstalledExtra(t *testing.T) {
	for _, tc := range []struct {
		label, code, country, city string
		ok bool
	}{
		{"🇫🇷 Париж, Франция, Extra", "fr", "Франция", "Париж", true},
		{"🇯🇵 Токио, Япония, Extra", "jp", "Япония", "Токио", true},
		{"FR Paris, France, Extra", "fr", "France", "Paris", true},
		{"Париж, Франция, Extra", "", "Франция", "Париж", true},
		{"🇫🇷 Париж, Франция, Extra Whitelist", "", "", "", false},
		{"🇫🇷 Париж, Extra", "", "", "", false},
		{"🇫🇷 Париж, , Extra", "", "", "", false},
		{"", "", "", "", false},
	} {
		code, country, city, ok := currentExactProfileLocation(tc.label)
		if code != tc.code || country != tc.country || city != tc.city || ok != tc.ok {
			t.Errorf("label=%q got code=%q country=%q city=%q ok=%t", tc.label, code, country, city, ok)
		}
	}
}

func TestStatusExactExtraIdentityAgreesWithPickerAndAutomation(t *testing.T) {
	dir := t.TempDir()
	filter := filepath.Join(dir, "profile.regex")
	out := filepath.Join(dir, "04_outbounds.json")
	if err := os.WriteFile(out, []byte(`{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"198.51.100.12","port":443}]}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: config{FilterPath: filter, OutPath: out, ConfigPath: filepath.Join(dir, "freenet.conf")},
		sem: make(chan struct{}, 1)}
	for _, tc := range []struct {
		name, filter, code, country, city, label string
	}{
		{"france", regexp.QuoteMeta("🇫🇷 Париж, Франция, Extra"), "fr", "Франция", "Париж", "🇫🇷 Париж, Франция, Extra"},
		{"japan", regexp.QuoteMeta("🇯🇵 Токио, Япония, Extra"), "jp", "Япония", "Токио", "🇯🇵 Токио, Япония, Extra"},
		{"legacy", "Frankfurt|Germany|Германия", "de", "Германия", "Frankfurt", "Germany · Frankfurt"},
		{"ambiguous", "🇫🇷.*|🇯🇵.*", "", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filter, []byte(tc.filter+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			got := a.status()
			if got.CountryCode != tc.code || got.Country != tc.country || got.City != tc.city || got.ProfileLabel != tc.label {
				t.Fatalf("status mismatch: code=%q country=%q city=%q profile=%q", got.CountryCode, got.Country, got.City, got.ProfileLabel)
			}
			automation := a.automationSnapshot()
			if automation.CurrentProfile != got.ProfileLabel || automation.CountryCode != got.CountryCode {
				t.Fatalf("status/automation identity mismatch: %+v %+v", got, automation)
			}
		})
	}
}

func TestLegacyRotateNotEnabledByNewExtraCountryDetection(t *testing.T) {
	if err := rotateBaselineValid(statusResponse{CountryCode: "fr", Endpoint: "198.51.100.12:443"}); err == nil {
		t.Fatal("new Extra ISO identity silently enabled unsupported legacy rotate")
	}
	if err := rotateBaselineValid(statusResponse{CountryCode: "de", Endpoint: "198.51.100.12:443"}); err != nil {
		t.Fatalf("legacy supported group regressed: %v", err)
	}
}
