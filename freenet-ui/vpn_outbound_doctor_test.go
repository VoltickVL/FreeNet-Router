package main

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestSafeVLESSQuerySchemaNeverReturnsValues(t *testing.T) {
	raw := "vless://secret-uuid@example.com:443?security=reality&type=grpc&serviceName=hidden&pbk=secret-key&sid=secret-id&sni=secret.example&fp=chrome#Extra"
	transport, security, keys := safeVLESSQuerySchema(raw)
	if transport != "grpc" || security != "reality" {
		t.Fatalf("schema transport=%q security=%q", transport, security)
	}
	want := []string{"fp", "pbk", "security", "serviceName", "sid", "sni", "type"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys=%#v want=%#v", keys, want)
	}
	for _, forbidden := range []string{"secret-uuid", "secret-key", "secret-id", "secret.example", "hidden"} {
		for _, key := range keys {
			if key == forbidden {
				t.Fatalf("secret value leaked as schema key: %q", forbidden)
			}
		}
	}
}

func TestVPNTransportModelFlagsNonBasicTransports(t *testing.T) {
	for _, transport := range []string{"", "tcp", "raw"} {
		if got := vpnTransportModel(transport); got != "fully-modeled" {
			t.Fatalf("transport %q model=%q", transport, got)
		}
	}
	for _, transport := range []string{"grpc", "ws", "xhttp", "httpupgrade"} {
		if got := vpnTransportModel(transport); got != "generic-network-only" {
			t.Fatalf("transport %q model=%q", transport, got)
		}
	}
}

func TestVLESSOutboundParityComparesCredentialsWithoutExposingThem(t *testing.T) {
	makeOutbound := func(shortID string) map[string]any {
		return map[string]any{
			"protocol": "vless",
			"settings": map[string]any{"vnext": []any{map[string]any{
				"address": "203.0.113.10", "port": 443,
				"users": []any{map[string]any{"id": "uuid-secret", "flow": "xtls-rprx-vision", "encryption": "none"}},
			}}},
			"streamSettings": map[string]any{
				"network": "tcp", "security": "reality",
				"realitySettings": map[string]any{
					"fingerprint": "chrome", "serverName": "example.com",
					"publicKey": "pbk-secret", "shortId": shortID, "spiderX": "/",
				},
			},
		}
	}
	if !vlessOutboundParity(makeOutbound("same"), makeOutbound("same")) {
		t.Fatal("equal outbound identities must match")
	}
	if vlessOutboundParity(makeOutbound("old"), makeOutbound("new")) {
		t.Fatal("credential mismatch must be detected")
	}
}

func TestVPNOutboundDoctorRouteRequiresAuthentication(t *testing.T) {
	a := &app{sem: make(chan struct{}, 1)}
	mux := http.NewServeMux()
	registerVPNOutboundDoctor(mux, a)
	req := httptest.NewRequest(http.MethodGet, "http://router.test/api/vpn/runtime-doctor", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code == http.StatusNotFound {
		t.Fatal("VPN runtime doctor route is not mounted")
	}
	if rr.Code == http.StatusOK {
		t.Fatal("VPN runtime doctor must not bypass authentication")
	}
}
