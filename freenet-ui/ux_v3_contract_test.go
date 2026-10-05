package main

import (
	"os"
	"strings"
	"testing"
)

func TestApprovedOverviewReadabilityContract(t *testing.T) {
	b, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, needle := range []string{
		"FreeNetApprovedOverview",
		"Текущий VPN",
		"DNS",
		"Проверить текущий VPN",
		"Подобрать серверы",
		"Топ-3 альтернативы на основе реальных измерений",
		"Лучший из альтернатив",
		"Для сравнения",
		"Не прошёл проверку",
		"best-v4-metrics",
		"current-health",
		"vpn-detail-chip",
	} {
		if !strings.Contains(s, needle) {
			t.Fatalf("approved Overview contract missing %q", needle)
		}
	}
}

func TestApprovedOverviewDoesNotShowTautologicalAvailability(t *testing.T) {
	b, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "FreeNet доступен") {
		t.Fatal("topbar must report factual VPN/DNS health, not tautological FreeNet availability")
	}
	if !strings.Contains(string(b), `summary.innerHTML = '<div class="overview-approved-fact"><span>DNS</span><strong id="topDNSValue">—</strong></div>'`) {
		t.Fatal("approved Overview must keep the topbar DNS-only")
	}
}
