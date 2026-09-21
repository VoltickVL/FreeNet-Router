package main

import (
	"os"
	"strings"
	"testing"
)

func TestDNSUserFacingCopyIsCanonical(t *testing.T) {
	required := map[string][]string{
		"web/runtime-acceptance.js": {
			"return mode === 'xkeen' ? 'Раздельный' : 'Прямой';",
			"dnsLabels.firmware = 'Прямой';",
			"dnsLabels.xkeen = 'Раздельный';",
			"return 'Прямой';",
		},
		"web/accepted-ux.js": {
			"value.textContent = 'Раздельный';",
			"value.textContent = 'Прямой';",
		},
		"web/settings-dns-ui.js": {
			"<span>Прямой</span>",
			"<span>Раздельный</span>",
			"? 'Раздельный' : 'Прямой'",
			"В режиме «Раздельный»",
		},
		"web/settings-v3-core.js": {
			"<option value=\"firmware\">Прямой</option>",
			"<option value=\"xkeen\">Раздельный</option>",
		},
		"web/self-update.js": {
			"firmwareOption.textContent = 'Прямой'",
			"dnsLabels.firmware = 'Прямой'",
		},
		"web/vpn-ux-fix.js": {
			"? 'Раздельный' : 'Прямой'",
			"direct.textContent='Прямой'",
			"split.textContent='Раздельный'",
			"Используйте режим «Прямой».",
		},
		"web/operation-coordinator.js": {
			"? 'Раздельный' : 'Прямой'",
			"VPN OK · DNS: Прямой",
		},
		"web/routing-v2.js": {
			"DNS: Прямой",
			"DNS: Раздельный",
		},
		"web/index.html": {
			"<option value=\"firmware\">Прямой</option>",
			"<option value=\"xkeen\">Раздельный</option>",
			"firmware:'Прямой',xkeen:'Раздельный'",
		},
	}
	for path, needles := range required {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		src := string(data)
		for _, needle := range needles {
			if !strings.Contains(src, needle) {
				t.Fatalf("%s missing canonical DNS copy %q", path, needle)
			}
		}
	}

	// These assets emit user-visible mode names directly. Historical copy must
	// not reappear there. runtime-acceptance.js / accepted-ux.js intentionally
	// retain old phrases only inside legacy-input recognition regexes.
	entries, err := os.ReadDir("web")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := "web/" + entry.Name()
		if path == "web/runtime-acceptance.js" || path == "web/accepted-ux.js" {
			continue // legacy-input recognition is intentionally retained here.
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		src := string(data)
		for _, forbidden := range []string{
			"Через роутер",
			"DNS через роутер",
			"DNS напрямую через роутер",
			"Штатный DNS роутера",
			"DNS напрямую",
			"XKeen/Xray DNS",
			"Раздельный DNS",
		} {
			if strings.Contains(src, forbidden) {
				t.Fatalf("%s still contains legacy user-facing DNS mode copy %q", path, forbidden)
			}
		}
	}

	runtime, err := os.ReadFile("web/runtime-acceptance.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(runtime)
	for _, legacyInput := range []string{
		"DNS напрямую",
		"Штатный DNS роутера",
		"DNS через роутер",
		"Через роутер",
		"Раздельный(?: DNS)?",
	} {
		if !strings.Contains(src, legacyInput) {
			t.Fatalf("runtime legacy-input compatibility missing %q", legacyInput)
		}
	}
	if strings.Contains(src, "return 'Через роутер';") || strings.Contains(src, "dnsLabels.firmware = 'DNS через роутер'") {
		t.Fatal("runtime acceptance still emits old direct DNS copy")
	}
}
