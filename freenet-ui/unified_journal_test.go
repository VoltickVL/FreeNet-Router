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
		"journalResultFilter: 'all'",
		"journalQuery: ''",
		"journalLive: true",
		"journalFiltersOpen: false",
		"['all','Все']",
		"['vpn','VPN']",
		"['auto','AUTO VPN']",
		"['subscription','Подписка']",
		"['system','Система']",
		"function journalCategory(event)",
		"if (kind === 'vpn') return 'vpn'",
		"if (kind === 'auto vpn' || kind === 'auto_vpn') return 'auto'",
		"if (kind === 'freenet_update' || kind === 'freenet_update_recovery') return ['Обновление FreeNet', 'system']",
		"window.openFreeNetJournal = filter =>",
		"source.slice(0, target === '#fn3JournalFull' ? state.journalPageSize : 4)",
		"function journalStage(event)",
		"candidate_selection:'Подбор'",
		"selection:'Решение'",
		"function loadJournal(force = false)",
		"fetchJSON('/api/journal?' + journalQueryString(true), {cache:'no-store'})",
		"window.setInterval(() =>",
		"journalPageActive() && state.journalLive",
		"data-journal-result",
		"id=\"fn3JournalSearch\"",
		"id=\"fn3JournalFiltersToggle\"",
		"id=\"fn3JournalAdvancedFilters\"",
		"state.journalFiltersOpen = false",
		"state.journalFiltersOpen = !state.journalFiltersOpen",
		"Пауза",
		"Возобновить",
		"До 15 000 значимых событий",
		"Штатная отметка — не чаще 1 раза в 6 часов",
		"id=\"fn3JournalRetentionRange\"",
		"class=\"fn3-journal-event-list\"",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("unified Journal UI contract missing %q", want)
		}
	}
	if strings.Contains(source, ": 'AUTO VPN';") {
		t.Fatal("unknown event kinds must not default to AUTO VPN")
	}
	for _, obsolete := range []string{"Авто · 5 с", "Авто выключено"} {
		if strings.Contains(source, obsolete) {
			t.Fatalf("Journal primary UI still exposes technical live-refresh copy %q", obsolete)
		}
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


func TestSelfUpdateStartWritesExplicitSystemJournalEvent(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `v3AppendEvent("freenet_update", "started", "Запущено обновление FreeNet до "+target+".")`) {
		t.Fatal("self-update apply must write an explicit timestamped Journal start event")
	}
}


func TestUnifiedJournalBackendHasReadOnlyEndpointAndSemanticDedupe(t *testing.T) {
	data, err := os.ReadFile("settings_v3.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, want := range []string{
		`mux.HandleFunc("GET /api/journal", a.requireAuth(a.handleJournalGet))`,
		"journalHistoryFileLimit         = 20000",
		"journalCanonicalRetentionLimit  = 15000",
		"journalSemanticDedupeWindow     = 15 * time.Second",
		"journalRoutineHeartbeatWindow   = 6 * time.Hour",
		"func dedupeCanonicalJournalEvents(events []automationEvent)",
		"func compactRoutineJournalEvents(events []automationEvent)",
		"merged = compactRoutineJournalEvents(merged)",
		"func appendBoundedJournalLine(path, line string)",
		"all := canonicalJournalEvents(journalCanonicalRetentionLimit, a.cfg.UpdateState)",
		`mux.HandleFunc("GET /api/journal/export", a.requireAuth(a.handleJournalExport))`,
		"writer.Comma = ';'",
		"writer.UseCRLF = true",
		"Дата (UTC)",
		"Событие / этап",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("Journal backend contract missing %q", want)
		}
	}
}

func TestUnifiedJournalReleaseCatalogSuppressesOnlyCanceledBrowserRequests(t *testing.T) {
	data, err := os.ReadFile("self_update_versions.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, want := range []string{
		"func selfUpdateRequestCanceled(r *http.Request, err error) bool",
		"errors.Is(r.Context().Err(), context.Canceled)",
		"errors.Is(err, context.Canceled)",
		`v3AppendEvent("freenet_release_catalog", "degraded", "PRIMARY ERROR: "+primary)`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("release-catalog Journal contract missing %q", want)
		}
	}
}
