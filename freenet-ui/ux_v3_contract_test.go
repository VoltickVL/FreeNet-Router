package main

import (
	"os"
	"strings"
	"testing"
)

func TestOverviewV4ReadabilityContract(t *testing.T) {
	b, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, needle := range []string{
		"FreeNetOverviewV4",
		"Текущий VPN",
		"ISP",
		"DNS",
		"Проверить текущий VPN",
		"Подобрать серверы",
		"данных для сравнения с текущим недостаточно",
		"best-v4-metrics",
	} {
		if !strings.Contains(s, needle) {
			t.Fatalf("Overview v4 contract missing %q", needle)
		}
	}
}

func TestOverviewV4DoesNotShowTautologicalAvailability(t *testing.T) {
	b, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "FreeNet доступен") {
		t.Fatal("topbar must report factual VPN/DNS health, not tautological FreeNet availability")
	}
}
