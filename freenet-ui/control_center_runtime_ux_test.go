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

func TestProviderHintDoesNotInventRoutingInheritance(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	if strings.Contains(ui, "Подряд использует") || strings.Contains(ui, "policy Ростелекома") {
		t.Fatal("ISP hint must not invent Podryad -> Rostelecom routing inheritance")
	}
	for _, want := range []string{"Routing policy задаётся только явными правилами", "не наследуется от другого провайдера"} {
		if !strings.Contains(ui, want) {
			t.Fatalf("explicit-only routing hint missing %q", want)
		}
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
		"window.FreeNetFlags",
		"flags.has(safe)",
		"flags.apply(n,safe)",
		"safe.toUpperCase()",
	} {
		if !strings.Contains(ui, required) {
			t.Fatalf("country marker contract missing %q", required)
		}
	}
	flags, err := webFS.ReadFile("web/vpn-ux-fix.js")
	if err != nil {
		t.Fatal(err)
	}
	flagSource := string(flags)
	for _, required := range []string{"window.FreeNetFlags", "ae:", "fr:", "cz:", "ie:"} {
		if !strings.Contains(flagSource, required) {
			t.Fatalf("canonical portable flag source missing %q", required)
		}
	}
	for _, text := range []string{ui, flagSource} {
		for _, forbidden := range []string{"🇩🇪", "🇵🇱", "🇫🇮", "🇳🇱"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("UI reintroduced platform emoji dependency %q", forbidden)
			}
		}
	}
}


func TestCurrentVPNSpeedUIIsStrictComparableOnly(t *testing.T) {
	data, err := webFS.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	for _, forbidden := range []string{
		"Быстрый замер",
		"быстрый контроль",
		"не для сравнения",
		"fallback_speed",
		"fallback_download_mbps",
		"throughput_source === 'current_fallback'",
	} {
		if strings.Contains(ui, forbidden) {
			t.Fatalf("current VPN UI still exposes non-comparable speed %q", forbidden)
		}
	}
	for _, want := range []string{
		"'Скорость VPN'",
		"throughput_source === 'strict_aggregate'",
		"candidate.download_mbps",
	} {
		if !strings.Contains(ui, want) {
			t.Fatalf("strict comparable current-speed UI contract missing %q", want)
		}
	}
}
