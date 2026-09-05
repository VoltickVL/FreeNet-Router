package main

import (
	"os"
	"strings"
	"testing"
)

func TestLegacyNativeEngineConfirmationIsBrowserOnlyAndExplicit(t *testing.T) {
	data, err := os.ReadFile("web/vpn-ux-fix.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, marker := range []string{
		"nativeEngineConfirmation",
		"native_filter_engine_confirm_required",
		"native_filter_engine_choices",
		"native_filter_engine = nativeEngineChoice",
		"Выберите прежний режим",
		"FreeNet не угадывает потерянное состояние",
		"Публичные DNS-резолверы",
		"NextDNS",
		"SkyDNS",
	} {
		if !strings.Contains(js, marker) {
			t.Fatalf("missing browser migration marker %q", marker)
		}
	}
	if strings.Contains(js, "nativeEngineChoice = 'public'") || strings.Contains(js, `native_filter_engine:"public"`) {
		t.Fatal("browser migration must not silently default the lost native engine")
	}
	if !strings.Contains(js, "if (apply && !nativeEngineChoice) apply.disabled = true") {
		t.Fatal("Apply must stay disabled until the one-time engine choice is explicit")
	}
}
