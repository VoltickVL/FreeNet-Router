package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestBestServerRanksVerifiedApplicationQuality(t *testing.T) {
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "0000000000000001", Name: "DE Frankfurt Extra", CountryCode: "de", Address: "de.example", Port: 443}, Raw: "secret-one"},
		{Profile: subscriptionProfile{ID: "0000000000000002", Name: "NL Amsterdam Extra", CountryCode: "nl", Address: "nl.example", Port: 443}, Raw: "secret-two"},
		{Profile: subscriptionProfile{ID: "0000000000000003", Name: "FI Helsinki Extra", CountryCode: "fi", Address: "fi.example", Port: 443}, Raw: "secret-three"},
	}
	tcp := func(_ context.Context, p subscriptionProfile) bestServerProbeResult {
		switch p.ID {
		case "0000000000000001":
			return bestServerProbeResult{OK: true, Median: 45}
		case "0000000000000002":
			return bestServerProbeResult{OK: true, Median: 25}
		default:
			return bestServerProbeResult{OK: true, Median: 18}
		}
	}
	app := func(_ context.Context, c bestServerInternalCandidate) bestServerProbeResult {
		switch c.Profile.ID {
		case "0000000000000001":
			return bestServerProbeResult{OK: true, Samples: []int{88, 94}, Median: 91, Jitter: 6}
		case "0000000000000002":
			return bestServerProbeResult{OK: true, Samples: []int{140, 150}, Median: 145, Jitter: 10}
		default:
			return bestServerProbeResult{OK: false}
		}
	}

	result := rankBestServerCandidates(context.Background(), candidates, 3, false, "de.example:443", tcp, app)
	if !result.Available || result.Recommendation == nil {
		t.Fatalf("expected recommendation, got %#v", result)
	}
	if result.Recommendation.ID != "0000000000000001" {
		t.Fatalf("expected DE candidate to win verified application quality, got %#v", result.Recommendation)
	}
	if !result.Recommendation.Current {
		t.Fatalf("expected current candidate marker")
	}
	if result.Recommendation.Confidence != "high" {
		t.Fatalf("expected high confidence, got %q", result.Recommendation.Confidence)
	}
}

func TestBestServerDoesNotRecommendTCPOnlyCandidate(t *testing.T) {
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "0000000000000001", Name: "DE Frankfurt Extra", Address: "de.example", Port: 443}},
	}
	tcp := func(context.Context, subscriptionProfile) bestServerProbeResult {
		return bestServerProbeResult{OK: true, Median: 10}
	}
	app := func(context.Context, bestServerInternalCandidate) bestServerProbeResult {
		return bestServerProbeResult{}
	}
	result := rankBestServerCandidates(context.Background(), candidates, 1, false, "", tcp, app)
	if result.Available || result.Recommendation != nil {
		t.Fatalf("TCP-only candidate must not become recommendation: %#v", result)
	}
	if !result.Candidates[0].Reachable || result.Candidates[0].Available {
		t.Fatalf("expected reachable but not application-verified candidate: %#v", result.Candidates[0])
	}
}

func TestBestServerTieBreakIsDeterministic(t *testing.T) {
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "0000000000000002", Name: "B Extra", Address: "b.example", Port: 443}},
		{Profile: subscriptionProfile{ID: "0000000000000001", Name: "A Extra", Address: "a.example", Port: 443}},
	}
	tcp := func(context.Context, subscriptionProfile) bestServerProbeResult {
		return bestServerProbeResult{OK: true, Median: 30}
	}
	app := func(context.Context, bestServerInternalCandidate) bestServerProbeResult {
		return bestServerProbeResult{OK: true, Samples: []int{100, 100}, Median: 100}
	}
	result := rankBestServerCandidates(context.Background(), candidates, 2, false, "", tcp, app)
	if result.Recommendation == nil || result.Recommendation.ID != "0000000000000001" {
		t.Fatalf("expected lexicographic ID tie-break, got %#v", result.Recommendation)
	}
}

func TestBestServerSubscriptionSecretsStayInternal(t *testing.T) {
	line := "vless://11111111-2222-3333-4444-555555555555@198.51.100.10:443?security=reality&type=tcp&sni=example.com&pbk=PUBLICKEYSECRET&sid=SHORTIDSECRET#DE%20Frankfurt%20Extra"
	candidates, total, truncated, err := parseBestServerCandidates([]byte(line + "\n"))
	if err != nil || total != 1 || truncated || len(candidates) != 1 {
		t.Fatalf("unexpected parse result: total=%d truncated=%v len=%d err=%v", total, truncated, len(candidates), err)
	}
	if !strings.Contains(candidates[0].Raw, "PUBLICKEYSECRET") {
		t.Fatal("test fixture should prove raw credentials remain internal")
	}
	public := bestServerCandidate{
		ID: candidates[0].Profile.ID, Name: candidates[0].Profile.Name, CountryCode: candidates[0].Profile.CountryCode,
		Endpoint: profileEndpoint(candidates[0].Profile), Reachable: true, Available: true, ApplicationMS: 100,
	}
	encoded, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, secret := range []string{"11111111-2222-3333-4444-555555555555", "PUBLICKEYSECRET", "SHORTIDSECRET", "vless://"} {
		if strings.Contains(text, secret) {
			t.Fatalf("public response leaked secret %q: %s", secret, text)
		}
	}
}

func TestBuildBestServerProbeOutboundRequiresRealityCredentials(t *testing.T) {
	profile := subscriptionProfile{ID: "0000000000000001", Name: "DE Frankfurt Extra", Address: "198.51.100.10", Port: 443}
	valid := "vless://11111111-2222-3333-4444-555555555555@198.51.100.10:443?security=reality&type=tcp&sni=example.com&pbk=key&sid=abcd#DE%20Frankfurt%20Extra"
	if _, err := buildBestServerProbeOutbound(valid, profile); err != nil {
		t.Fatalf("valid Reality profile rejected: %v", err)
	}
	invalid := "vless://11111111-2222-3333-4444-555555555555@198.51.100.10:443?security=reality&type=tcp&sni=example.com#DE%20Frankfurt%20Extra"
	if _, err := buildBestServerProbeOutbound(invalid, profile); err == nil {
		t.Fatal("missing Reality credentials must be rejected")
	}
}
