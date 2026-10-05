package main

import (
	"os"
	"strings"
	"testing"
)

func TestSettingsV3JournalRouteBridgeHandlesSlashHash(t *testing.T) {
	bridgeData, err := os.ReadFile("web/automation-async.js")
	if err != nil {
		t.Fatal(err)
	}
	bridge := string(bridgeData)
	for _, want := range []string{
		`const routeName = () => String(location.hash || '').replace(/^#\/?/, '')`,
		`if (routeName() !== 'journal') return false;`,
		`const trigger = q('#fn3AllEvents');`,
		`trigger.click();`,
		`const settingsMountObserver = new MutationObserver(() => {`,
		`if (!q('#fn3AllEvents')) return;`,
		`if (event.target?.closest?.('.nav-btn[data-page]')) schedule();`,
	} {
		if !strings.Contains(bridge, want) {
			t.Fatalf("Journal lifecycle bridge missing %q", want)
		}
	}

	settingsData, err := os.ReadFile("web/settings-v3.js")
	if err != nil {
		t.Fatal(err)
	}
	settings := string(settingsData)
	for _, want := range []string{
		`q('#fn3AllEvents').onclick = () =>`,
		`window.setPage('journal')`,
		`mountJournalPage();`,
	} {
		if !strings.Contains(settings, want) {
			t.Fatalf("Journal bridge must delegate to canonical Settings v3 mount; missing %q", want)
		}
	}
}

func TestJournalAnalysisControlsAndExportRoute(t *testing.T) {
	coreData, err := os.ReadFile("web/settings-v3-core.js")
	if err != nil {
		t.Fatal(err)
	}
	core := string(coreData)
	for _, want := range []string{
		"Экспорт CSV",
		"Сегодня",
		"24 часа",
		"7 дней",
		"Страница ${state.journalPage} из ${state.journalPages}",
		"<option>50</option>",
		"<option selected>100</option>",
		"<option>200</option>",
		"<option>500</option>",
		"/api/journal/export?",
		"? 'Обновление' : 'Запущено'",
	} {
		if !strings.Contains(core, want) {
			t.Fatalf("Journal analysis UI missing %q", want)
		}
	}

	goData, err := os.ReadFile("settings_v3.go")
	if err != nil {
		t.Fatal(err)
	}
	goSource := string(goData)
	for _, want := range []string{
		`mux.HandleFunc("GET /api/journal/export", a.requireAuth(a.handleJournalExport))`,
		"journalCanonicalRetentionLimit  = 15000",
		"journalHistoryFileLimit         = 20000",
		"journalDefaultPageSize          = 100",
		"journalMaxPageSize              = 500",
	} {
		if !strings.Contains(goSource, want) {
			t.Fatalf("Journal analysis backend missing %q", want)
		}
	}
}

