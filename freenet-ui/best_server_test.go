package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestBestServerSubscriptionSecretsStayInternal(t *testing.T) {
	line := "vless://11111111-2222-3333-4444-555555555555@198.51.100.10:443?security=reality&type=tcp&sni=example.com&pbk=PUBLICKEYSECRET&sid=SHORTIDSECRET#DE%20Frankfurt%20Extra"
	candidates, total, truncated, err := parseBestServerCandidates([]byte(line + "\n"))
	if err != nil || total != 1 || truncated || len(candidates) != 1 {
		t.Fatalf("unexpected parse result: total=%d truncated=%v len=%d err=%v", total, truncated, len(candidates), err)
	}
	if !strings.Contains(candidates[0].Raw, "PUBLICKEYSECRET") {
		t.Fatal("test fixture should prove raw credentials remain internal")
	}
	public := bestServerQualityCandidate{
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

func TestBuildBestServerProbeOutboundSupportsTLSWSParity(t *testing.T) {
	profile := subscriptionProfile{ID: "0000000000000002", Name: "DE Frankfurt Extra", Address: "198.51.100.20", Port: 443}
	raw := "vless://11111111-2222-3333-4444-555555555555@198.51.100.20:443?security=tls&type=ws&fp=firefox&sni=tls.example&host=cdn.example&path=%2Fedge#DE%20Frankfurt%20Extra"
	outbound, err := buildBestServerProbeOutbound(raw, profile)
	if err != nil {
		t.Fatalf("valid TLS/WS profile rejected: %v", err)
	}
	stream, ok := outbound["streamSettings"].(map[string]any)
	if !ok || stream["network"] != "ws" || stream["security"] != "tls" {
		t.Fatalf("unexpected TLS/WS stream settings: %#v", outbound["streamSettings"])
	}
	if _, exists := stream["realitySettings"]; exists {
		t.Fatalf("TLS/WS probe must not carry Reality settings: %#v", stream)
	}
	tlsSettings, ok := stream["tlsSettings"].(map[string]any)
	if !ok || tlsSettings["serverName"] != "tls.example" {
		t.Fatalf("TLS settings lost SNI: %#v", tlsSettings)
	}
	wsSettings, ok := stream["wsSettings"].(map[string]any)
	if !ok || wsSettings["path"] != "/edge" {
		t.Fatalf("WS settings lost path: %#v", wsSettings)
	}
	headers, ok := wsSettings["headers"].(map[string]any)
	if !ok || headers["Host"] != "cdn.example" {
		t.Fatalf("WS settings lost Host header: %#v", wsSettings)
	}
	settings := outbound["settings"].(map[string]any)
	vnext := settings["vnext"].([]any)[0].(map[string]any)
	user := vnext["users"].([]any)[0].(map[string]any)
	if _, exists := user["flow"]; exists {
		t.Fatalf("TLS/WS probe must not manufacture XTLS flow: %#v", user)
	}
}

func TestBuildBestServerProbeOutboundRejectsUnsupportedTransport(t *testing.T) {
	profile := subscriptionProfile{ID: "0000000000000003", Name: "DE Frankfurt Extra", Address: "198.51.100.30", Port: 443}
	raw := "vless://11111111-2222-3333-4444-555555555555@198.51.100.30:443?security=tls&type=grpc&sni=grpc.example#DE%20Frankfurt%20Extra"
	if _, err := buildBestServerProbeOutbound(raw, profile); err == nil {
		t.Fatal("unsupported VLESS transport must fail closed")
	}
}

func TestLegacyTCPFirstBestServerEngineIsRemoved(t *testing.T) {
	data, err := os.ReadFile("best_server.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	for _, legacy := range []string{
		"registerBestServerAPI",
		"rankBestServerCandidates(",
		"rankBestServerCandidatesWithFilter(",
		"bestServerCacheTTL",
		"bestServerShortlist",
		"bestServerTCPWorkers",
	} {
		if strings.Contains(src, legacy) {
			t.Fatalf("disconnected legacy Best Server engine returned: %q", legacy)
		}
	}
}
