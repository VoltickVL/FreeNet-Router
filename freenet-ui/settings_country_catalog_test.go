package main

import "testing"

func TestSettingsCountryOptionsUsesEligibleExtraCatalog(t *testing.T) {
	profiles := []subscriptionProfile{
		{Name: "US Лос-Анджелес, США, Extra", CountryCode: "us"},
		{Name: "NL Амстердам, Нидерланды, Extra", CountryCode: "nl"},
		{Name: "NL Роттердам, Нидерланды, Extra", CountryCode: "nl"},
		{Name: "GB Лондон, Великобритания, Extra", CountryCode: "gb"},
		{Name: "RU Москва, Россия, Extra", CountryCode: "ru"},
		{Name: "DE Франкфурт, Германия, Whitelist, Extra", CountryCode: "de"},
		{Name: "FR Париж, Франция, Expired, Extra", CountryCode: "fr"},
	}

	got := settingsCountryOptions(profiles, []string{"nl", "fr"})
	byCode := map[string]settingsCountryOption{}
	for _, option := range got {
		if _, duplicate := byCode[option.Code]; duplicate {
			t.Fatalf("duplicate country option for %q", option.Code)
		}
		byCode[option.Code] = option
	}

	for _, code := range []string{"us", "nl", "gb"} {
		option, ok := byCode[code]
		if !ok || !option.Available {
			t.Fatalf("eligible Extra country %q missing or unavailable: %#v", code, option)
		}
	}
	if byCode["us"].Name != "США" {
		t.Fatalf("unexpected US presentation name: %q", byCode["us"].Name)
	}
	if byCode["nl"].Name != "Нидерланды" {
		t.Fatalf("unexpected NL presentation name: %q", byCode["nl"].Name)
	}
	if _, ok := byCode["ru"]; ok {
		t.Fatal("RU must not be offered as AUTO VPN foreign replacement country")
	}
	if _, ok := byCode["de"]; ok {
		t.Fatal("Whitelist profile must not become an AUTO VPN country option")
	}
	fr, ok := byCode["fr"]
	if !ok || fr.Available {
		t.Fatalf("previously selected unavailable country must be preserved readably: %#v", fr)
	}
}

func TestSettingsCountryNameFallsBackWithoutLeakingProfileIdentity(t *testing.T) {
	option := settingsCountryOptions([]subscriptionProfile{{Name: "US Extra", CountryCode: "us"}}, nil)
	if len(option) != 1 || option[0].Name != "Страна VPN" {
		t.Fatalf("unexpected fallback country presentation: %#v", option)
	}
}
