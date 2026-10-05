package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestV3MergeEventsOrdersAcrossSourcesBeforeLimit(t *testing.T) {
	autoEvents := []automationEvent{
		{At: "2026-09-16T04:17:00Z", Kind: "auto_vpn", Result: "success", Message: "older auto"},
		{At: "2026-09-16T04:15:00Z", Kind: "auto_vpn", Result: "same", Message: "older auto 2"},
	}
	settingsEvents := []automationEvent{
		{At: "2026-09-16T23:20:00Z", Kind: "geodata", Result: "success", Message: "newest settings"},
		{At: "2026-09-16T23:05:00Z", Kind: "auto_vpn", Result: "success", Message: "newer settings"},
		{At: "2026-09-15T22:00:00Z", Kind: "backup", Result: "success", Message: "old settings"},
	}

	got := v3MergeEvents(4, autoEvents, settingsEvents)
	want := []string{
		"2026-09-16T23:20:00Z",
		"2026-09-16T23:05:00Z",
		"2026-09-16T04:17:00Z",
		"2026-09-16T04:15:00Z",
	}
	if len(got) != len(want) {
		t.Fatalf("unexpected event count: got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].At != want[i] {
			t.Fatalf("event %d order mismatch: got %q want %q", i, got[i].At, want[i])
		}
	}
}

func TestV3MergeEventsKeepsMalformedTimestampsAfterValidEvents(t *testing.T) {
	got := v3MergeEvents(0,
		[]automationEvent{{At: "broken-a", Message: "a"}, {At: "2026-09-17T00:00:00Z", Message: "valid"}},
		[]automationEvent{{At: "broken-b", Message: "b"}},
	)
	if len(got) != 3 {
		t.Fatalf("unexpected event count: %d", len(got))
	}
	if got[0].Message != "valid" || got[1].Message != "a" || got[2].Message != "b" {
		t.Fatalf("unexpected deterministic order: %#v", got)
	}
}

func TestCanonicalJournalEventsMergesServerHistoriesAndDropsLegacyProviderRows(t *testing.T) {
	dir := t.TempDir()
	autoPath := filepath.Join(dir, "automation.history")
	settingsPath := filepath.Join(dir, "settings.history")
	t.Setenv("FREENET_AUTOMATION_HISTORY", autoPath)
	t.Setenv("FREENET_SETTINGS_V3_HISTORY", settingsPath)

	auto := strings.Join([]string{
		"2026-09-22T03:00:00Z\tAUTO VPN\tsuccess\tauto healthy",
		"2026-09-22T03:01:00Z\tVPN switch\tsuccess\tlegacy duplicate",
	}, "\n") + "\n"
	settings := strings.Join([]string{
		"2026-09-22T03:02:00Z\tVPN\tsuccess\tmanual vpn",
		"2026-09-22T03:03:00Z\tsubscription\tsuccess\tsubscription refreshed",
	}, "\n") + "\n"
	if err := os.WriteFile(autoPath, []byte(auto), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}

	got := canonicalJournalEvents(50)
	if len(got) != 3 {
		t.Fatalf("canonical journal events=%d want 3: %#v", len(got), got)
	}
	wantKinds := []string{"subscription", "VPN", "AUTO VPN"}
	for i, want := range wantKinds {
		if got[i].Kind != want {
			t.Fatalf("event %d kind=%q want %q", i, got[i].Kind, want)
		}
	}
	for _, item := range got {
		if strings.EqualFold(item.Kind, "VPN switch") {
			t.Fatalf("legacy generic provider row leaked into canonical Journal: %#v", item)
		}
	}
}

func TestCanonicalJournalEventsIsBoundedToFiftyNewestRows(t *testing.T) {
	dir := t.TempDir()
	autoPath := filepath.Join(dir, "automation.history")
	settingsPath := filepath.Join(dir, "settings.history")
	t.Setenv("FREENET_AUTOMATION_HISTORY", autoPath)
	t.Setenv("FREENET_SETTINGS_V3_HISTORY", settingsPath)

	var auto strings.Builder
	var settings strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&auto, "2026-09-21T%02d:%02d:00Z\tAUTO VPN\tsuccess\tauto %02d\n", i/60, i%60, i)
		fmt.Fprintf(&settings, "2026-09-22T%02d:%02d:00Z\tsubscription\tsuccess\tsub %02d\n", i/60, i%60, i)
	}
	if err := os.WriteFile(autoPath, []byte(auto.String()), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(settings.String()), 0600); err != nil {
		t.Fatal(err)
	}

	got := canonicalJournalEvents(50)
	if len(got) != 50 {
		t.Fatalf("canonical journal events=%d want 50", len(got))
	}
	if got[0].Message != "sub 39" {
		t.Fatalf("newest event=%q want sub 39", got[0].Message)
	}
}

func TestSettingsV3HealthJournalCoalescesIdenticalFiveMinuteStates(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "settings.state")
	historyPath := filepath.Join(dir, "settings.history")
	t.Setenv("FREENET_SETTINGS_V3_STATE", statePath)
	t.Setenv("FREENET_SETTINGS_V3_HISTORY", historyPath)

	healthy := automationHealthResult{State: automationHealthHealthy, Reason: "Текущий VPN и сервисные маршруты работают стабильно."}
	recordSettingsV3Health(healthy)
	recordSettingsV3Health(healthy)
	recordSettingsV3Health(healthy)

	events := readAutomationEvents(historyPath, 20)
	if len(events) != 1 {
		t.Fatalf("identical healthy checks must produce one Journal transition, got %d: %#v", len(events), events)
	}

	recordSettingsV3Health(automationHealthResult{State: automationHealthFailed, Reason: "VPN path failed"})
	recordSettingsV3Health(automationHealthResult{State: automationHealthHealthy, Reason: "Текущий VPN и сервисные маршруты работают стабильно."})
	events = readAutomationEvents(historyPath, 20)
	if len(events) != 3 {
		t.Fatalf("state transitions must remain visible, got %d: %#v", len(events), events)
	}
}


func TestCanonicalJournalIncludesTerminalSelfUpdateTimestamp(t *testing.T) {
	dir := t.TempDir()
	autoPath := filepath.Join(dir, "automation.history")
	settingsPath := filepath.Join(dir, "settings.history")
	updatePath := filepath.Join(dir, "self-update.state")
	t.Setenv("FREENET_AUTOMATION_HISTORY", autoPath)
	t.Setenv("FREENET_SETTINGS_V3_HISTORY", settingsPath)

	if err := os.WriteFile(settingsPath, []byte("2026-10-04T10:00:00Z\tVPN\tsuccess\tVPN переключён\n"), 0600); err != nil {
		t.Fatal(err)
	}
	updateState := strings.Join([]string{
		"STATE=SUCCESS",
		"FROM_VERSION=v0.4.73",
		"TARGET_VERSION=v0.4.74",
		"MESSAGE=Выбранная версия FreeNet установлена и проверена",
		"UPDATED_AT=2026-10-04T10:01:05Z",
	}, "\n") + "\n"
	if err := os.WriteFile(updatePath, []byte(updateState), 0600); err != nil {
		t.Fatal(err)
	}

	got := canonicalJournalEvents(50, updatePath)
	if len(got) != 2 {
		t.Fatalf("canonical journal events=%d want 2: %#v", len(got), got)
	}
	if got[0].Kind != "freenet_update" || got[0].At != "2026-10-04T10:01:05Z" {
		t.Fatalf("update event not ordered by exact updater timestamp: %#v", got[0])
	}
	if !strings.Contains(got[0].Message, "v0.4.73 → v0.4.74") {
		t.Fatalf("update transition missing from journal message: %q", got[0].Message)
	}
	if got[1].Kind != "VPN" || got[1].At != "2026-10-04T10:00:00Z" {
		t.Fatalf("VPN event order mismatch: %#v", got[1])
	}
}

func TestSelfUpdateJournalIgnoresNonTerminalProgress(t *testing.T) {
	dir := t.TempDir()
	updatePath := filepath.Join(dir, "self-update.state")
	if err := os.WriteFile(updatePath, []byte("STATE=UPDATING\nUPDATED_AT=2026-10-04T10:01:05Z\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := selfUpdateJournalEvents(updatePath); len(got) != 0 {
		t.Fatalf("non-terminal updater progress must not duplicate journal rows: %#v", got)
	}
}


func TestCanonicalJournalDedupesSameAutoOutcomeAcrossHistories(t *testing.T) {
	dir := t.TempDir()
	autoPath := filepath.Join(dir, "automation.history")
	settingsPath := filepath.Join(dir, "settings.history")
	t.Setenv("FREENET_AUTOMATION_HISTORY", autoPath)
	t.Setenv("FREENET_SETTINGS_V3_HISTORY", settingsPath)

	message := "Текущий VPN заменён на проверенный вариант: Франкфурт-на-Майне, Германия, Extra"
	auto := strings.Join([]string{
		"2026-10-04T00:05:01Z\tAUTO VPN\tsuccess\t" + message,
		"2026-10-04T00:04:00Z\tAUTO VPN\tselection\tAUTO VPN Top-3: Frankfurt [VPN 175 мс, сайты 177 мс, скорость 151 Мбит/с, стабильность 17 мс]. Выбран: Frankfurt.",
	}, "\n") + "\n"
	settings := "2026-10-04T00:05:00Z\tauto_vpn\tsame\t" + message + "\n"
	if err := os.WriteFile(autoPath, []byte(auto), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}

	got := canonicalJournalEvents(50)
	if len(got) != 2 {
		t.Fatalf("semantic duplicate was not collapsed: %#v", got)
	}
	if got[0].Message != message || got[0].Result != "success" {
		t.Fatalf("canonical switch row=%#v want newest success outcome", got[0])
	}
	if got[1].Result != "selection" || !strings.Contains(got[1].Message, "Top-3") {
		t.Fatalf("decision evidence must remain visible: %#v", got[1])
	}
}

func TestCanonicalJournalKeepsSameMessageOutsideDedupeWindow(t *testing.T) {
	message := "VPN health transition"
	got := dedupeCanonicalJournalEvents([]automationEvent{
		{At: "2026-10-04T00:10:30Z", Kind: "AUTO VPN", Result: "success", Message: message},
		{At: "2026-10-04T00:10:00Z", Kind: "auto_vpn", Result: "success", Message: message},
	})
	if len(got) != 2 {
		t.Fatalf("events outside dedupe window must remain separate: %#v", got)
	}
}

func TestJournalHistoryWriterKeepsNewestBoundedRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.history")
	for i := 0; i < journalHistoryFileLimit+17; i++ {
		appendBoundedJournalLine(path, fmt.Sprintf("2026-10-04T00:%02d:%02dZ\ttest\tsuccess\trow-%03d\n", (i/60)%60, i%60, i))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != journalHistoryFileLimit {
		t.Fatalf("history rows=%d want=%d", len(lines), journalHistoryFileLimit)
	}
	wantFirst := "row-017"
	wantLast := fmt.Sprintf("row-%03d", journalHistoryFileLimit+16)
	if !strings.Contains(lines[0], wantFirst) || !strings.Contains(lines[len(lines)-1], wantLast) {
		t.Fatalf("bounded history did not preserve newest rows: first=%q last=%q wantFirst=%q wantLast=%q", lines[0], lines[len(lines)-1], wantFirst, wantLast)
	}
}


func TestCanonicalJournalHidesHistoricalCanceledReleaseCatalogNoise(t *testing.T) {
	dir := t.TempDir()
	autoPath := filepath.Join(dir, "automation.history")
	settingsPath := filepath.Join(dir, "settings.history")
	t.Setenv("FREENET_AUTOMATION_HISTORY", autoPath)
	t.Setenv("FREENET_SETTINGS_V3_HISTORY", settingsPath)

	settings := strings.Join([]string{
		"2026-10-04T00:11:00Z\tfreenet_release_catalog\tfailed\tLatest fallback failed: latest release metadata unavailable: context canceled",
		"2026-10-04T00:10:59Z\tfreenet_release_catalog\tdegraded\tPRIMARY ERROR: release catalog page 1 unavailable: context canceled",
		"2026-10-04T00:10:00Z\tfreenet_release_catalog\tfailed\tPRIMARY ERROR: release catalog page 1 unavailable: resolver failure",
	}, "\n") + "\n"
	if err := os.WriteFile(settingsPath, []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}

	got := canonicalJournalEvents(50)
	if len(got) != 1 {
		t.Fatalf("canceled release-catalog noise not suppressed: %#v", got)
	}
	if !strings.Contains(got[0].Message, "resolver failure") {
		t.Fatalf("real release-catalog failure must remain visible: %#v", got)
	}
}
