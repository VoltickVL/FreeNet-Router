package main

import (
	"os"
	"strings"
	"testing"
)

func TestBestServerUIReplacesManualQuickCountries(t *testing.T) {
	data, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{
		"Лучший VPN",
		"/api/vpn/best",
		"Переключиться на лучший",
		"Проверить заново",
		"countries.remove()",
		"operation: 'provider'",
		"profile_id: recommendation.id",
		"MUTATION: NONE",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("Best Server UI contract missing %q", want)
		}
	}
}

func TestBestServerRouteIsRegistered(t *testing.T) {
	data, err := os.ReadFile("geodata_api.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "registerBestServerAPI(mux, a)") {
		t.Fatal("Best Server API must be registered at startup")
	}
}

func TestBestServerUIKeepsExactProfileFallback(t *testing.T) {
	index, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	js, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), `id="profileSearch"`) || !strings.Contains(string(index), `id="profilesTrigger"`) {
		t.Fatal("manual exact profile selector disappeared")
	}
	if !strings.Contains(string(js), "Ручной выбор Extra-профиля") {
		t.Fatal("manual exact selector must be clearly demoted to fallback")
	}
}
