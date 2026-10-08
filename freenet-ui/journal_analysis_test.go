package main

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestJournalHealthFreshnessShowsWatchdogIndependentlyOfEvents(t *testing.T) {
	now := time.Date(2026, 10, 8, 11, 35, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		enabled bool
		interval time.Duration
		state map[string]string
		want string
	}{
		{"fresh-completed-no-new-journal-event", true, 30*time.Second, map[string]string{
			"HEALTH_LAST": "2026-10-08T11:34:30Z", "HEALTH_RESULT": "healthy",
		}, "fresh"},
		{"stale-completed-after-17-min-silence", true, 30*time.Second, map[string]string{
			"HEALTH_LAST": "2026-10-08T11:18:00Z", "HEALTH_SCHEDULE_LAST": "2026-10-08T11:18:15Z", "HEALTH_RESULT": "healthy",
		}, "stale"},
		{"stale-stuck-started-with-no-completion", true, 30*time.Second, map[string]string{
			"HEALTH_SCHEDULE_LAST": "2026-10-08T11:18:00Z",
		}, "stale"},
		{"no-known-completion", true, 30*time.Second, map[string]string{}, "unknown"},
		{"disabled", false, 30*time.Second, map[string]string{"HEALTH_LAST":"2026-10-08T11:18:00Z"}, "disabled"},
		{"five-minute-fresh", true, 5*time.Minute, map[string]string{"HEALTH_LAST":"2026-10-08T11:25:00Z"}, "fresh"},
		{"five-minute-stale", true, 5*time.Minute, map[string]string{"HEALTH_LAST":"2026-10-08T11:19:00Z"}, "stale"},
		{"future-clock-skew-unknown", true, 30*time.Second, map[string]string{"HEALTH_LAST":"2026-10-08T11:40:00Z"}, "unknown"},
	} {
		t.Run(tc.name,func(t *testing.T){
			got:=journalHealthFreshness(tc.enabled,tc.interval,tc.state,now)
			if got.Freshness!=tc.want {t.Fatalf("freshness=%q want %q: %+v",got.Freshness,tc.want,got)}
			if got.Enabled!=tc.enabled {t.Fatalf("enabled mismatch %+v",got)}
			if got.IntervalSeconds!=int(tc.interval/time.Second){t.Fatalf("interval mismatch %+v",got)}
		})
	}
}

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

func TestJournalCompactsPendingQualityAcrossChangingMetrics(t *testing.T) {
	events := []automationEvent{
		{At:"2026-10-08T12:00:00Z", Kind:"auto_vpn", Result:"uncertain", Message:"VPN отвечает, но качество соединения ухудшено: отклик 204 мс, сервисы 4/4. AUTO VPN накапливает подтверждение деградации."},
		{At:"2026-10-08T11:30:00Z", Kind:"auto_vpn", Result:"uncertain", Message:"VPN отвечает, но качество соединения ухудшено: отклик 203 мс, сервисы 4/4. AUTO VPN накапливает подтверждение деградации."},
		{At:"2026-10-08T11:10:00Z", Kind:"subscription", Result:"success", Message:"Подписка проверена."},
		{At:"2026-10-08T11:00:00Z", Kind:"auto_vpn", Result:"uncertain", Message:"VPN отвечает, но качество соединения ухудшено: отклик 202 мс, сервисы 4/4. AUTO VPN накапливает подтверждение деградации."},
		{At:"2026-10-08T10:59:00Z", Kind:"auto_vpn", Result:"success", Message:"Текущий VPN и сервисные маршруты работают стабильно."},
		{At:"2026-10-08T10:30:00Z", Kind:"auto_vpn", Result:"uncertain", Message:"VPN отвечает, но качество соединения ухудшено: отклик 205 мс, сервисы 4/4. AUTO VPN накапливает подтверждение деградации."},
		{At:"2026-10-08T10:00:00Z", Kind:"auto_vpn", Result:"uncertain", Message:"VPN отвечает, но качество соединения ухудшено: отклик 201 мс, сервисы 4/4. AUTO VPN накапливает подтверждение деградации."},
	}
	got := compactPendingQualityJournalEvents(events)
	if len(got) != 4 {
		t.Fatalf("pending quality compaction rows=%d want 4: %+v", len(got), got)
	}
	pending := []string{}
	for _, event := range got {
		if journalEventIsPendingQualityObservation(event) {
			pending = append(pending, event.At)
		}
	}
	if len(pending) != 2 || pending[0] != "2026-10-08T11:00:00Z" || pending[1] != "2026-10-08T10:00:00Z" {
		t.Fatalf("pending state transitions mismatch: %+v", pending)
	}
}

func TestJournalPendingQualityKeepsBoundedHeartbeat(t *testing.T) {
	events := []automationEvent{
		{At:"2026-10-08T18:01:00Z", Kind:"auto_vpn", Result:"uncertain", Message:"VPN отвечает, но качество соединения ухудшено: отклик 204 мс, сервисы 4/4. AUTO VPN накапливает подтверждение деградации."},
		{At:"2026-10-08T12:00:00Z", Kind:"auto_vpn", Result:"uncertain", Message:"VPN отвечает, но качество соединения ухудшено: отклик 203 мс, сервисы 4/4. AUTO VPN накапливает подтверждение деградации."},
		{At:"2026-10-08T10:00:00Z", Kind:"auto_vpn", Result:"uncertain", Message:"VPN отвечает, но качество соединения ухудшено: отклик 201 мс, сервисы 4/4. AUTO VPN накапливает подтверждение деградации."},
	}
	got := compactPendingQualityJournalEvents(events)
	if len(got) != 2 || got[0].At != "2026-10-08T18:01:00Z" || got[1].At != "2026-10-08T10:00:00Z" {
		t.Fatalf("pending heartbeat mismatch: %+v", got)
	}
}

func TestJournalPendingQualityNeverHidesTriggerOrRecovery(t *testing.T) {
	events := []automationEvent{
		{At:"2026-10-08T11:00:00Z", Kind:"auto_vpn", Result:"uncertain", Message:"VPN отвечает, но качество соединения ухудшено: отклик 204 мс, сервисы 4/4. AUTO VPN накапливает подтверждение деградации."},
		{At:"2026-10-08T10:30:00Z", Kind:"auto_vpn", Result:"uncertain", Message:"VPN отвечает, но качество соединения критически ухудшено: отклик 520 мс, сервисы 2/4. AUTO VPN запускает ускоренную проверку замены."},
		{At:"2026-10-08T10:20:00Z", Kind:"auto_vpn", Result:"quality_optimization:start", Message:"Критическая деградация качества подтверждена одним severe-наблюдением."},
		{At:"2026-10-08T10:10:00Z", Kind:"auto_vpn", Result:"apply:success", Message:"Изменения применены."},
		{At:"2026-10-08T10:00:00Z", Kind:"auto_vpn", Result:"uncertain", Message:"VPN отвечает, но качество соединения ухудшено: отклик 201 мс, сервисы 4/4. AUTO VPN накапливает подтверждение деградации."},
	}
	got := compactPendingQualityJournalEvents(events)
	if len(got) != len(events) {
		t.Fatalf("trigger/recovery rows were compacted: got=%d want=%d: %+v", len(got), len(events), got)
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

func TestJournalCSVIsChronologicalAndExcelFriendly(t *testing.T) {
	events := []automationEvent{
		{At:"2026-10-05T10:02:00Z", Kind:"auto_vpn", Result:"candidate_selection:start", Message:"new; value"},
		{At:"2026-10-05T10:01:00Z", Kind:"freenet_update", Result:"start", Message:"old"},
	}
	payload, err := journalCSV(events)
	if err != nil { t.Fatal(err) }
	if !bytes.HasPrefix(payload, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatalf("csv must keep UTF-8 BOM: %v", payload[:min(3,len(payload))])
	}
	text := string(payload)
	for _, want := range []string{
		"Дата (UTC);Время (UTC);Категория;Событие / этап;Результат;Описание\r\n",
		"05.10.2026;10:01:00;Система;Обновление FreeNet;Служебное / без изменений;old",
		"05.10.2026;10:02:00;AUTO VPN;Подбор;Служебное / без изменений;\"new; value\"",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("human CSV missing %q: %q", want, text)
		}
	}
	if strings.Index(text, "10:01:00") > strings.Index(text, "10:02:00") {
		t.Fatalf("export must be chronological oldest->newest: %q", text)
	}
	if strings.Contains(text, "timestamp,category,kind,result,message") {
		t.Fatalf("technical CSV headers must be gone: %q", text)
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
