package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestJournalRetentionAndPageBounds(t *testing.T) {
	if journalCanonicalRetentionLimit != 15000 {
		t.Fatalf("canonical retention=%d want 15000", journalCanonicalRetentionLimit)
	}
	if journalHistoryFileLimit != 20000 {
		t.Fatalf("history file limit=%d want 20000", journalHistoryFileLimit)
	}
	if journalDefaultPageSize != 100 || journalMaxPageSize != 500 {
		t.Fatalf("page bounds default=%d max=%d", journalDefaultPageSize, journalMaxPageSize)
	}
}

func TestJournalFilterPaginationAndStats(t *testing.T) {
	events := []automationEvent{
		{At:"2026-10-05T10:03:00Z", Kind:"auto_vpn", Result:"healthy", Message:"ok"},
		{At:"2026-10-05T10:02:00Z", Kind:"auto_vpn", Result:"uncertain", Message:"lag"},
		{At:"2026-10-05T10:01:00Z", Kind:"freenet_update", Result:"failed", Message:"failed update"},
		{At:"2026-10-04T10:00:00Z", Kind:"subscription", Result:"success", Message:"old"},
	}
	req := httptest.NewRequest("GET", "/api/journal?page=1&page_size=50&category=auto&from=2026-10-05T00:00:00Z&to=2026-10-06T00:00:00Z", nil)
	query, err := parseJournalQuery(req)
	if err != nil { t.Fatal(err) }
	filtered := filterJournalEvents(events, query)
	if len(filtered) != 2 { t.Fatalf("filtered=%d want 2", len(filtered)) }
	stats := journalStatsFor(filtered)
	if stats.Total != 2 || stats.Success != 1 || stats.Neutral != 1 || stats.Errors != 0 {
		t.Fatalf("stats=%+v", stats)
	}
	page, gotPage, pages := paginateJournalEvents(filtered, 1, 50)
	if len(page) != 2 || gotPage != 1 || pages != 1 {
		t.Fatalf("page len=%d page=%d pages=%d", len(page), gotPage, pages)
	}
}

func TestJournalCompactsRoutineAutoNoiseButKeepsHeartbeat(t *testing.T) {
	events := []automationEvent{
		{At:"2026-10-07T12:00:00Z", Kind:"auto_vpn", Result:"success", Message:"Текущий VPN и сервисные маршруты работают стабильно."},
		{At:"2026-10-07T11:30:00Z", Kind:"auto_vpn", Result:"success", Message:"Текущий VPN и сервисные маршруты работают стабильно."},
		{At:"2026-10-07T05:59:59Z", Kind:"auto_vpn", Result:"success", Message:"Текущий VPN и сервисные маршруты работают стабильно."},
	}
	got := compactRoutineJournalEvents(events)
	if len(got) != 2 {
		t.Fatalf("compacted routine rows=%d want 2: %+v", len(got), got)
	}
	if got[0].At != "2026-10-07T12:00:00Z" || got[1].At != "2026-10-07T05:59:59Z" {
		t.Fatalf("heartbeat retention mismatch: %+v", got)
	}
}

func TestJournalNeverCompactsIncidentRecoveryStages(t *testing.T) {
	events := []automationEvent{
		{At:"2026-10-07T12:00:00Z", Kind:"AUTO VPN", Result:"incident:recovered", Message:"incident=abc; recovered"},
		{At:"2026-10-07T11:59:30Z", Kind:"AUTO VPN", Result:"incident:recovered", Message:"incident=abc; recovered"},
		{At:"2026-10-07T11:59:00Z", Kind:"AUTO VPN", Result:"failed", Message:"PRIMARY ERROR: transport failed"},
	}
	got := compactRoutineJournalEvents(events)
	if len(got) != len(events) {
		t.Fatalf("incident/recovery rows were compacted: got=%d want=%d", len(got), len(events))
	}
}

func TestJournalRoutineClassifierDoesNotHideManualOrMutationEvents(t *testing.T) {
	cases := []automationEvent{
		{Kind:"VPN", Result:"success", Message:"Текущий VPN и сервисные маршруты работают стабильно."},
		{Kind:"auto_vpn", Result:"success", Message:"VPN-интернет восстановлен через аварийный fast-path: Париж"},
		{Kind:"auto_vpn", Result:"selection", Message:"AUTO VPN Top-3: ..."},
		{Kind:"auto_vpn", Result:"apply:success", Message:"Изменения применены."},
	}
	for _, event := range cases {
		if journalEventIsRoutineAutoNoise(event) {
			t.Fatalf("event must stay visible: %+v", event)
		}
	}
}

func TestJournalCSVIsChronologicalAndEscaped(t *testing.T) {
	events := []automationEvent{
		{At:"2026-10-05T10:02:00Z", Kind:"auto_vpn", Result:"healthy", Message:"new, value"},
		{At:"2026-10-05T10:01:00Z", Kind:"freenet_update", Result:"start", Message:"old"},
	}
	payload, err := journalCSV(events)
	if err != nil { t.Fatal(err) }
	text := string(payload)
	if !strings.Contains(text, "\"new, value\"") {
		t.Fatalf("csv comma escaping missing: %q", text)
	}
	if strings.Index(text, "10:01:00Z") > strings.Index(text, "10:02:00Z") {
		t.Fatalf("export must be chronological oldest->newest: %q", text)
	}
}

func TestJournalQueryRejectsUnsupportedPageSizeAndBadRange(t *testing.T) {
	for _, raw := range []string{
		"/api/journal?page_size=1000",
		"/api/journal?from=2026-10-06T00:00:00Z&to=2026-10-05T00:00:00Z",
	} {
		req := httptest.NewRequest("GET", raw, nil)
		if _, err := parseJournalQuery(req); err == nil {
			t.Fatalf("query %s unexpectedly accepted", raw)
		}
	}
}

func TestJournalRangeIsHalfOpen(t *testing.T) {
	events := []automationEvent{
		{At:"2026-10-05T00:00:00Z", Kind:"auto_vpn", Result:"healthy"},
		{At:"2026-10-06T00:00:00Z", Kind:"auto_vpn", Result:"healthy"},
	}
	req := httptest.NewRequest("GET", "/api/journal?from=2026-10-05T00:00:00Z&to=2026-10-06T00:00:00Z", nil)
	q, err := parseJournalQuery(req); if err != nil { t.Fatal(err) }
	got := filterJournalEvents(events, q)
	if len(got) != 1 || got[0].At != "2026-10-05T00:00:00Z" {
		t.Fatalf("half-open range mismatch: %+v", got)
	}
}

func TestJournalExportFilenameUsesRange(t *testing.T) {
	q := journalQuery{HasFrom:true, HasTo:true, From:time.Date(2026,10,5,0,0,0,0,time.UTC), To:time.Date(2026,10,6,0,0,0,0,time.UTC)}
	if got := journalExportFilename(q); got != "freenet-journal-20261005-20261005.csv" {
		t.Fatalf("filename=%q", got)
	}
}
