package main

import (
	"strings"
	"testing"
)

func TestBaseExtraPolicyExcludesWhitelistFamilies(t *testing.T) {
	body := strings.Join([]string{
		"vless://A@203.0.113.10:443?security=reality#DE%20Frankfurt%2C%20Germany%2C%20Extra",
		"vless://B@203.0.113.11:443?security=reality#DE%20Germany%2C%20Extra%20Whitelist",
		"vless://C@203.0.113.12:443?security=reality#DE%20Germany%2C%20Extra%20Whitelist2",
		"vless://D@203.0.113.13:443?security=reality#Expired%20Germany%2C%20Extra",
	}, "\n")

	profiles, err := parseSubscriptionBody([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 {
		t.Fatalf("selector profiles=%d want=1: %+v", len(profiles), profiles)
	}
	if !strings.HasSuffix(strings.ToLower(profiles[0].Name), "extra") || strings.Contains(strings.ToLower(profiles[0].Name), "whitelist") {
		t.Fatalf("unexpected eligible profile: %+v", profiles[0])
	}

	best, total, truncated, err := parseBestServerCandidates([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if truncated || total != 1 || len(best) != 1 {
		t.Fatalf("Best Server eligibility total=%d len=%d truncated=%v", total, len(best), truncated)
	}
}

func TestBaseExtraPolicyKeepsDistinctProfilesSharingEndpoint(t *testing.T) {
	body := strings.Join([]string{
		"vless://A@198.51.100.25:443?security=reality&sni=a.example#DE%20Frankfurt%2C%20Germany%2C%20Extra",
		"vless://B@198.51.100.25:443?security=reality&sni=b.example#NL%20Amsterdam%2C%20Netherlands%2C%20Extra",
	}, "\n")

	profiles, err := parseSubscriptionBody([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 {
		t.Fatalf("shared listener profiles=%d want=2: %+v", len(profiles), profiles)
	}
	if profileEndpoint(profiles[0]) != profileEndpoint(profiles[1]) {
		t.Fatalf("fixture must share endpoint: %+v", profiles)
	}
	if profiles[0].ID == profiles[1].ID {
		t.Fatal("logical profiles sharing address:port must keep distinct sanitized identities")
	}
}

func TestBaseExtraNameBoundary(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"Frankfurt, Germany, Extra", true},
		{"Example Extra", true},
		{"Extra", true},
		{"Germany, Extra Whitelist", false},
		{"Germany, Extra Whitelist2", false},
		{"Expired Germany, Extra", false},
		{"SomeExtra", false},
		{"Regular profile", false},
	} {
		if got := isBaseExtraProfileName(tc.name); got != tc.want {
			t.Fatalf("isBaseExtraProfileName(%q)=%v want %v", tc.name, got, tc.want)
		}
	}
}
