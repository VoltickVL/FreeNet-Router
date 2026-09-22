package main

import (
	"os"
	"strings"
	"testing"
)

func TestUnifiedJournalUIHasCanonicalFiltersAndCategories(t *testing.T) {
	data, err := os.ReadFile("web/settings-v3-core.js")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, want := range []string{
		"journalFilter: 'all'",
		"['all','Все']",
		"['vpn','VPN']",
		"['auto','AUTO VPN']",
		"['subscription','Подписка']",
		"['system','Система']",
		"function journalCategory(event)",
		"if (kind === 'vpn') return 'vpn'",
		"if (kind === 'auto vpn' || kind === 'auto_vpn') return 'auto'",
		"window.openFreeNetJournal = filter =>",
		"source.slice(0, target === '#fn3Journal' ? 4 : 50)",
		"Единая серверная история",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("unified Journal UI contract missing %q", want)
		}
	}
	if strings.Contains(source, ": 'AUTO VPN';") {
		t.Fatal("unknown event kinds must not default to AUTO VPN")
	}
}

func TestManualVPNHandlersWriteExplicitVPNJournalEvents(t *testing.T) {
	for _, tc := range []struct {
		path string
		want string
	}{
		{"main.go", `v3AppendEvent("VPN", journalResult, journalMessage)`},
		{"network_apply_api.go", `v3AppendEvent("VPN", journalResult, journalMessage)`},
		{"best_server_targeted.go", `v3AppendEvent("VPN", journalResult, journalMessage)`},
	} {
		data, err := os.ReadFile(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), tc.want) {
			t.Fatalf("%s missing explicit manual VPN Journal event", tc.path)
		}
	}
}

func TestSubscriptionSecretActionsWriteOnlySafeJournalMessages(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, want := range []string{
		`v3AppendEvent("subscription", "failed", "Ключ подписки не сохранён: адрес не прошёл проверку.")`,
		`v3AppendEvent("subscription", "success", "Ключ подписки сохранён или заменён локально; секрет в журнал не записан.")`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("subscription Journal contract missing %q", want)
		}
	}
}
