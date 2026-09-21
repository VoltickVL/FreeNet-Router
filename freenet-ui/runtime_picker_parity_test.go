package main

import (
	"os"
	"strings"
	"testing"
)

func TestRuntimePickerParityContract(t *testing.T) {
	runtimeData, err := os.ReadFile("web/runtime-acceptance.js")
	if err != nil {
		t.Fatal(err)
	}
	runtime := string(runtimeData)
	for _, want := range []string{
		"function dnsTopbarLabel(mode)",
		"return mode === 'xkeen' ? 'Раздельный' : 'Прямой';",
		"value.textContent = topbarText;",
		"return 'Раздельный';",
		"return 'Прямой';",
	} {
		if !strings.Contains(runtime, want) {
			t.Fatalf("runtime DNS parity missing %q", want)
		}
	}

	freeNetData, err := os.ReadFile("web/self-update.js")
	if err != nil {
		t.Fatal(err)
	}
	freeNet := string(freeNetData)
	for _, want := range []string{
		"Math.min(540, vw - 24)",
		"filtered.slice(0, 5)",
		"max-height:236px;overflow-y:auto",
		"body.append(summary, search, list, detail)",
		"Найти версию FreeNet",
	} {
		if !strings.Contains(freeNet, want) {
			t.Fatalf("FreeNet picker parity missing %q", want)
		}
	}
	if strings.Contains(freeNet, "body.append(summary, detail, search, list)") {
		t.Fatal("FreeNet detail must stay below the searchable five-row catalog")
	}

	xrayData, err := os.ReadFile("web/xray-core-manager.js")
	if err != nil {
		t.Fatal(err)
	}
	xray := string(xrayData)
	for _, want := range []string{
		"Math.min(540, vw - 24)",
		"filtered.slice(0, 5)",
		"max-height:236px;overflow-y:auto",
		"className = 'xcm-selection'",
		"Найти версию Xray",
	} {
		if !strings.Contains(xray, want) {
			t.Fatalf("Xray picker parity missing %q", want)
		}
	}
	if strings.Contains(xray, "xcm-release-desc") {
		t.Fatal("Xray picker rows must not expand into release-description cards")
	}
}
