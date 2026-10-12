package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestBetaExtra2AdmittedWithoutOtherWhitelistFamilies(t *testing.T) {
	body := strings.Join([]string{
		"vless://A@203.0.113.10:443?security=reality#DE%20Frankfurt%2C%20Germany%2C%20Extra",
		"vless://B@203.0.113.11:443?security=reality#DE%20Germany%2C%20Extra%20Whitelist",
		"vless://C@203.0.113.12:443?security=tls&type=ws&sni=front.example&host=ws.example&path=%2Fsecure%2Fv1#DE%20Germany%2C%20Extra%20Whitelist2",
		"vless://D@203.0.113.13:443?security=reality#Expired%20Germany%2C%20Extra",
		"vless://E@203.0.113.14:443?security=reality#DE%20Germany%2C%20Extra%20Whitelist3",
	}, "\n")

	profiles, err := parseSubscriptionBody([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 {
		t.Fatalf("selector profiles=%d want base Extra and exact Whitelist2: %+v", len(profiles), profiles)
	}
	if extraProfileFamily(profiles[0].Name) != "extra" || extraProfileFamily(profiles[1].Name) != "extra whitelist2" {
		t.Fatalf("wrong eligible profile families: %+v", profiles)
	}
	visible := selectableSubscriptionProfiles(profiles)
	if len(visible) != 2 || visible[1].ID != profiles[1].ID {
		t.Fatalf("browser selector dropped Whitelist2: %+v", visible)
	}
	best, total, truncated, err := parseBestServerCandidates([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if truncated || total != 2 || len(best) != 2 {
		t.Fatalf("Best Server eligibility total=%d len=%d truncated=%v", total, len(best), truncated)
	}
	if best[1].Profile.ID != profiles[1].ID {
		t.Fatal("manual, Best Server and AUTO must share the exact Whitelist2 logical ID")
	}
}

func TestForeignBestServerPoolIncludesWhitelist2WithoutRussia(t *testing.T) {
	body := strings.Join([]string{
		"vless://A@203.0.113.10:443?security=reality#DE%20Frankfurt%2C%20Germany%2C%20Extra",
		"vless://B@203.0.113.11:443?security=reality#RU%20Moscow%2C%20Russia%2C%20Extra",
		"vless://C@203.0.113.12:443?security=tls&type=ws&sni=front.example&host=ws.example&path=%2Fws#PL%20Warsaw%2C%20Poland%2C%20Extra%20Whitelist2",
		"vless://D@203.0.113.13:443?security=reality#Expired%20NL%20Amsterdam%2C%20Netherlands%2C%20Extra",
		"vless://E@203.0.113.14:443?security=reality#FR%20Paris%2C%20France%2C%20Extra%20Whitelist",
	}, "\n")

	best, total, truncated, err := parseBestServerCandidates([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if truncated || total != 3 || len(best) != 3 {
		t.Fatalf("candidate pool total=%d len=%d truncated=%v want three incl RU", total, len(best), truncated)
	}
	foreign := filterForeignBestServerCandidates(best)
	if len(foreign) != 2 || foreign[0].Profile.CountryCode != "de" || foreign[1].Profile.CountryCode != "pl" {
		t.Fatalf("foreign candidate pool=%+v want DE ordinary and PL Whitelist2 only", foreign)
	}
	// Country presets must offer a region represented solely by Whitelist2.
	profiles := []subscriptionProfile{best[0].Profile, best[1].Profile, best[2].Profile}
	visible := selectableSubscriptionProfiles(profiles)
	if len(visible) != 2 {
		t.Fatalf("country selector visible profiles=%d want=2: %+v", len(visible), visible)
	}
	options := settingsCountryOptions(visible, nil)
	if len(options) != 2 || options[0].Code != "de" || options[1].Code != "pl" {
		t.Fatalf("countries available from Extra+Whitelist2=%+v", options)
	}
}

func TestExtraFamiliesKeepDistinctProfilesSharingEndpoint(t *testing.T) {
	body := strings.Join([]string{
		"vless://A@198.51.100.25:443?security=reality&sni=a.example#DE%20Frankfurt%2C%20Germany%2C%20Extra",
		"vless://B@198.51.100.25:443?security=tls&type=ws&sni=b.example&host=ws.example&path=%2Fws#DE%20Frankfurt%2C%20Germany%2C%20Extra%20Whitelist2",
	}, "\n")
	profiles, err := parseSubscriptionBody([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 || profileEndpoint(profiles[0]) != profileEndpoint(profiles[1]) || profiles[0].ID == profiles[1].ID {
		t.Fatalf("distinct logical profiles sharing listener were collapsed: %+v", profiles)
	}
}

func TestSelectableExtraNameBoundary(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{"Frankfurt, Germany, Extra", "extra"},
		{"Example Extra", "extra"},
		{"Extra", "extra"},
		{"Germany, Extra Whitelist", ""},
		{"Germany, Extra Whitelist2", "extra whitelist2"},
		{"🇩🇪 Германия, Extra Whitelist2", "extra whitelist2"},
		{"DE Германия, EXTRA WHITELIST2", "extra whitelist2"},
		{"Extra Whitelist2", "extra whitelist2"},
		{"Extra Whitelist 2", ""},
		{"Extra Whitelist3", ""},
		{"Extra Whitelist20", ""},
		{"Extra Whitelist2 backup", ""},
		{"Expired Germany, Extra Whitelist2", ""},
		{"SomeExtra", ""},
		{"Regular profile", ""},
	} {
		if got := extraProfileFamily(tc.name); got != tc.want {
			t.Fatalf("extraProfileFamily(%q)=%q want=%q", tc.name, got, tc.want)
		}
		if got := isSelectableExtraProfileName(tc.name); got != (tc.want != "") {
			t.Fatalf("isSelectableExtraProfileName(%q)=%v want=%v", tc.name, got, tc.want != "")
		}
	}
}

func TestWhitelist2TransportPreservesSNIHostAndPath(t *testing.T) {
	raw := "vless://TEST-UUID@203.0.113.10:443?security=tls&type=ws&sni=front.example&host=cdn.example&path=%2Fapi%2Fv1%2Fstreams&fp=firefox#DE%20Germany%2C%20Extra%20Whitelist2"
	profile, ok := parseSafeVLESSProfile(raw)
	if !ok {
		t.Fatal("supported VLESS WS+TLS Whitelist2 must be selectable")
	}
	outbound, err := buildBestServerProbeOutbound(raw, profile)
	if err != nil {
		t.Fatal(err)
	}
	stream, ok := outbound["streamSettings"].(map[string]any)
	if !ok {
		t.Fatalf("invalid transport: %#v", outbound)
	}
	if stream["network"] != "ws" || stream["security"] != "tls" {
		t.Fatalf("Whitelist2 must stay TLS+WS, not Reality: %#v", stream)
	}
	tls, ok := stream["tlsSettings"].(map[string]any)
	if !ok || tls["serverName"] != "front.example" || tls["fingerprint"] != "firefox" {
		t.Fatalf("SNI/fingerprint changed: %#v", tls)
	}
	ws, ok := stream["wsSettings"].(map[string]any)
	if !ok || ws["path"] != "/api/v1/streams" {
		t.Fatalf("WebSocket path changed: %#v", ws)
	}
	headers, ok := ws["headers"].(map[string]any)
	if !ok || headers["Host"] != "cdn.example" {
		t.Fatalf("WebSocket host changed: %#v", ws)
	}
}

func TestBestCandidateCapPreservesWhitelist2AtSubscriptionTail(t *testing.T) {
	lines := make([]string, 0, 100)
	for i := 0; i < 80; i++ {
		lines = append(lines, fmt.Sprintf("vless://BASE%d@203.0.113.10:443?security=reality#DE%%20City%%20%02d%%2C%%20Germany%%2C%%20Extra", i, i))
	}
	for i := 0; i < 20; i++ {
		lines = append(lines, fmt.Sprintf("vless://WL%d@203.0.113.11:443?security=tls&type=ws&sni=front.example&host=cdn.example&path=%%2Fstream%%2F%d#DE%%20City%%20%02d%%2C%%20Germany%%2C%%20Extra%%20Whitelist2", i, i, i))
	}
	all, total, truncated, err := parseBestServerCandidates([]byte(strings.Join(lines, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || total != 100 || len(all) != bestServerMaxCandidates {
		t.Fatalf("bounded pool total=%d len=%d truncated=%v", total, len(all), truncated)
	}
	families := map[string]int{}
	for _, candidate := range all {
		families[extraProfileFamily(candidate.Profile.Name)]++
	}
	if families["extra"] != 44 || families["extra whitelist2"] != 20 {
		t.Fatalf("family-balanced pool=%+v want 44 ordinary + 20 Extra2", families)
	}
	// The 100-row selector has a separate limit and must not silently erase Extra2.
	selector, err := parseSubscriptionBody([]byte(strings.Join(lines, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	if len(selector) != 100 {
		t.Fatalf("selector profiles=%d want=100", len(selector))
	}
}

func TestBestCandidateCapSingleFamilyKeepsOriginalOrder(t *testing.T) {
	lines := make([]string, 0, 75)
	for i := 0; i < 75; i++ {
		lines = append(lines, fmt.Sprintf("vless://BASE%d@203.0.113.10:443?security=reality#DE%%20City%%20%02d%%2C%%20Germany%%2C%%20Extra", i, i))
	}
	rows, total, truncated, err := parseBestServerCandidates([]byte(strings.Join(lines, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || total != 75 || len(rows) != bestServerMaxCandidates {
		t.Fatalf("single-family cap len=%d total=%d truncated=%v", len(rows), total, truncated)
	}
	if !strings.Contains(rows[0].Profile.Name, "00") || !strings.Contains(rows[len(rows)-1].Profile.Name, "63") {
		t.Fatal("single-family candidate order changed unexpectedly")
	}
}
