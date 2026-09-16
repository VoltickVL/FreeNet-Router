package main

import "testing"

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
