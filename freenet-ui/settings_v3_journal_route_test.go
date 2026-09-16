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
