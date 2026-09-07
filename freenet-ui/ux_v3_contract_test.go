package main

import (
	"os"
	"strings"
	"testing"
)

func TestOverviewV3ReadabilityContract(t *testing.T) {
	b, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, needle := range []string{
		"FreeNetOverviewV3",
		"Текущий VPN",
		"ISP",
		"DNS",
		"Проверить текущий VPN",
		"Почему рекомендуем",
		"best-v3-metrics",
	} {
		if !strings.Contains(s, needle) {
			t.Fatalf("Overview v3 contract missing %q", needle)
		}
	}
}

func TestOverviewV3DoesNotShowTautologicalAvailability(t *testing.T) {
	b, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "FreeNet доступен") {
		t.Fatal("topbar must report factual VPN/DNS health, not tautological FreeNet availability")
	}
}
