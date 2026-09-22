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
