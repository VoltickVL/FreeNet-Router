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
		"Провайдер",
		"DNS",
		"Проверить текущий VPN",
		"Подобрать серверы",
		"Топ-3 варианта на основе реальных измерений",
		"Лучший вариант",
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
}
