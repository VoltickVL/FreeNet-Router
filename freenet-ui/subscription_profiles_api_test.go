package main

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

const testSubscriptionPlain = `vless://TEST-ID-A@203.0.113.10:443?security=reality&pbk=TEST-PBK-A&sid=TEST-SID-A#%F0%9F%87%A9%F0%9F%87%AA%20Frankfurt%2C%20Germany%2C%20Extra
vless://TEST-ID-B@198.51.100.20:8443?security=reality&pbk=TEST-PBK-B&sid=TEST-SID-B#%F0%9F%87%B5%F0%9F%87%B1%20Warsaw%2C%20Poland%2C%20Extra
vless://TEST-ID-C@192.0.2.30:443?security=reality#Expired%20Warsaw%20Extra
vless://TEST-ID-D@192.0.2.40:443?security=reality#Regular%20profile
`

func TestParseSubscriptionBodyReturnsOnlySanitizedActiveExtra(t *testing.T) {
	profiles, err := parseSubscriptionBody([]byte(testSubscriptionPlain))
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 {
		t.Fatalf("profiles=%d want=2: %+v", len(profiles), profiles)
	}
	if !strings.Contains(profiles[0].Name, "Frankfurt") || profiles[0].Address != "203.0.113.10" || profiles[0].Port != 443 {
		t.Fatalf("unexpected first profile: %+v", profiles[0])
	}
	if !strings.Contains(profiles[1].Name, "Warsaw") || profiles[1].Address != "198.51.100.20" || profiles[1].Port != 8443 {
		t.Fatalf("unexpected second profile: %+v", profiles[1])
	}
	for _, p := range profiles {
		if len(p.ID) != 16 {
			t.Fatalf("unsafe/invalid id: %q", p.ID)
		}
		encoded, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		text := string(encoded)
		for _, forbidden := range []string{"TEST-ID-", "TEST-PBK", "TEST-SID", "security=reality", "vless://"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("sanitized profile leaked %q: %s", forbidden, text)
			}
		}
	}
}

func TestProfileCountryCodeUsesExplicitASCIIPrefixOnly(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{"NL Amsterdam, Нидерланды, Extra", "nl"},
		{"DE Франкфурт-на-Майне, Германия, Extra", "de"},
		{"AE Фуджейра, ОАЭ, Extra", "ae"},
		{"Frankfurt, Germany, Extra", ""},
		{"de Frankfurt, Germany, Extra", ""},
		{"D Frankfurt, Germany, Extra", ""},
	} {
		if got := profileCountryCode(tc.name); got != tc.want {
			t.Fatalf("profileCountryCode(%q)=%q want %q", tc.name, got, tc.want)
		}
	}

	p, ok := parseSafeVLESSProfile("vless://X@example.test:443?security=reality#PL%20Warsaw%2C%20Poland%2C%20Extra")
	if !ok || p.CountryCode != "pl" {
		t.Fatalf("country metadata missing from sanitized profile: %+v ok=%v", p, ok)
	}
	encoded, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"country_code":"pl"`) {
		t.Fatalf("country metadata missing from JSON: %s", encoded)
	}
}

func TestParseSubscriptionBodyAcceptsBase64Subscription(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte(testSubscriptionPlain))
	profiles, err := parseSubscriptionBody([]byte(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 {
		t.Fatalf("profiles=%d want=2", len(profiles))
	}
}

func TestParseSafeVLESSProfileSupportsIPv6AndHostname(t *testing.T) {
	for _, tc := range []struct {
		uri     string
		address string
		port    int
	}{
		{"vless://X@example.test:443?security=reality#Example%20Extra", "example.test", 443},
		{"vless://X@[2001:db8::1]:8443?security=reality#IPv6%20Extra", "2001:db8::1", 8443},
	} {
		p, ok := parseSafeVLESSProfile(tc.uri)
		if !ok {
			t.Fatalf("profile rejected: %s", tc.uri)
		}
		if p.Address != tc.address || p.Port != tc.port {
			t.Fatalf("got %+v want address=%s port=%d", p, tc.address, tc.port)
		}
	}
}

func TestSanitizeProfileNameBlocksCredentialLikeFragments(t *testing.T) {
	for _, name := range []string{
		"Extra uuid=TEST-SECRET",
		"Extra https://secret.invalid/key",
		"Extra pbk=TEST-KEY",
		"Extra vless://credential",
	} {
		if got := sanitizeProfileName(name); got != "Extra profile" {
			t.Fatalf("credential-like name not neutralized: %q -> %q", name, got)
		}
	}
}

func TestParseSubscriptionBodyRejectsNoExtra(t *testing.T) {
	_, err := parseSubscriptionBody([]byte("vless://X@example.test:443?security=reality#Regular"))
	if err == nil {
		t.Fatal("expected no-Extra error")
	}
}
