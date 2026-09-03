package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSameOriginTrustedLoopbackProxy(t *testing.T) {
	r := httptest.NewRequest("POST", "http://127.0.0.1:1001/api/action", nil)
	r.Host = "127.0.0.1:1001"
	r.RemoteAddr = "127.0.0.1:43123"
	r.Header.Set("Origin", "https://freenet.example.test:8443")
	r.Header.Set("X-Forwarded-Host", "freenet.example.test:8443")
	r.Header.Set("X-Forwarded-Proto", "https")
	if !sameOrigin(r) {
		t.Fatal("trusted loopback reverse proxy origin rejected")
	}
}

func TestSameOriginDoesNotTrustForwardedHeadersFromLANPeer(t *testing.T) {
	r := httptest.NewRequest("POST", "http://192.168.50.1:1001/api/action", nil)
	r.Host = "192.168.50.1:1001"
	r.RemoteAddr = "192.168.50.20:43123"
	r.Header.Set("Origin", "https://freenet.example.test:8443")
	r.Header.Set("X-Forwarded-Host", "freenet.example.test:8443")
	r.Header.Set("X-Forwarded-Proto", "https")
	if sameOrigin(r) {
		t.Fatal("non-loopback peer bypassed origin check with spoofed forwarded headers")
	}
}

func TestSameOriginRejectsForwardedProtoMismatchAndCrossSite(t *testing.T) {
	r := httptest.NewRequest("POST", "http://127.0.0.1:1001/api/action", nil)
	r.Host = "127.0.0.1:1001"
	r.RemoteAddr = "127.0.0.1:43123"
	r.Header.Set("Origin", "https://freenet.example.test:8443")
	r.Header.Set("X-Forwarded-Host", "freenet.example.test:8443")
	r.Header.Set("X-Forwarded-Proto", "http")
	if sameOrigin(r) {
		t.Fatal("forwarded protocol mismatch accepted")
	}
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	if sameOrigin(r) {
		t.Fatal("cross-site request accepted through loopback proxy")
	}
}

func TestPortableCountryFlagsDoNotDependOnEmoji(t *testing.T) {
	b, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	for _, want := range []string{"flag-icon flag-de", "flag-icon flag-pl", "flag-icon flag-fi", "flag-icon flag-nl", "renderHeroFlag", ".flag-fi::before", ".flag-fi::after"} {
		if !strings.Contains(html, want) {
			t.Fatalf("portable flag contract missing %q", want)
		}
	}
	for _, forbidden := range []string{"🇩🇪", "🇵🇱", "🇫🇮", "🇳🇱", "const flags="} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("UI still depends on emoji flag material %q", forbidden)
		}
	}
}
