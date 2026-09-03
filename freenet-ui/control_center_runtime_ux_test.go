package main

import (
	"strings"
	"testing"
)

func TestControlCenterRuntimeStateUXContract(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	for _, required := range []string{
		"VPN работает",
		"DNS защищён",
		"XKeen работает",
		"FreeNet готов",
		"VPN перезапускается. Ответ на запрос ещё не получен — подтверждаем итог по фактическому состоянию…",
		"Сейчас активно:",
		"Быстрое переключение выше выбирает страну/группу",
		"Конкретный профиль из подписки для ручного apply не выбран — это нормально после быстрого переключения.",
		"renderInstallScenario(s)",
	} {
		if !strings.Contains(ui, required) {
			t.Fatalf("runtime UX contract missing %q", required)
		}
	}
	if strings.Contains(ui, "Соединение прервалось во время перезапуска VPN. Проверяем фактическое состояние") {
		t.Fatal("successful VPN restart must not be presented as a connection error before status acceptance")
	}
	if strings.Contains(ui, `<span>dns-out</span>`) {
		t.Fatal("dns-out must not be a first-layer user-facing status label")
	}
}

func TestExtraProfileCountryMarkerHasPortableFallback(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	for _, required := range []string{
		"makeCountryMarker(p.country_code)",
		"country-code-badge",
		"countryFlagCodes.has(safe)",
		"safe.toUpperCase()",
		"flag-ae",
		"flag-fr",
		"flag-cz",
		"flag-ie",
	} {
		if !strings.Contains(ui, required) {
			t.Fatalf("country marker contract missing %q", required)
		}
	}
	for _, forbidden := range []string{"🇩🇪", "🇵🇱", "🇫🇮", "🇳🇱"} {
		if strings.Contains(ui, forbidden) {
			t.Fatalf("UI reintroduced platform emoji dependency %q", forbidden)
		}
	}
}
