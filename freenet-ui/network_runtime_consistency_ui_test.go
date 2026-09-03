package main

import (
	"strings"
	"testing"
)

func TestNetworkRuntimeConsistencyUIContract(t *testing.T) {
	js, err := webFS.ReadFile("web/self-update.js")
	if err != nil {
		t.Fatal(err)
	}
	text := string(js)
	for _, required := range []string{
		"Проверить изменения",
		"Применить",
		"Проверяем выбранные настройки без сохранения",
		"Активный ISP/DNS будет сохранён только после успешной проверки результата",
		"if (oldSave) oldSave.hidden = true",
		"body: JSON.stringify({operation: 'network', isp: isp.value, dns_mode: dns.value, confirm: true})",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("network flow contract missing %q", required)
		}
	}
}
