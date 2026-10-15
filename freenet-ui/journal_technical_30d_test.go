package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A normal router exports all available *significant* technical events from
// the preceding 30 UTC calendar days. No network request or active Xray
// inspection is made while preparing or downloading this archive.
func TestThirtyDayTechnicalJournalArchiveAndPruning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "technical.jsonl")
	t.Setenv("FREENET_TECH_JOURNAL", path)
	now := time.Now().UTC()
	old := now.AddDate(0, 0, -28)
	today := technicalJournalRecord{
		ObservedAt: now.Format(time.RFC3339Nano), EventAt: now.Format(time.RFC3339),
		Source: "auto", Kind: "AUTO VPN", Result: "incident:recovered",
		Severity: "info", Incident: "incident-1",
		Message: "password=hunter2; endpoint=server.example.org; recovered",
	}
	oldRecord := technicalJournalRecord{
		ObservedAt: old.Format(time.RFC3339Nano), EventAt: old.Format(time.RFC3339),
		Source: "auto", Kind: "AUTO VPN", Result: "incident:start",
		Severity: "info", Incident: "incident-1", Message: "starting",
	}
	for _, record := range []technicalJournalRecord{oldRecord, today} {
		if err := appendTechnicalJournalRecord(path, record); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if _, err := os.Stat(technicalJournalDailyFilename(path, old)); err != nil {
		t.Fatalf("29th-day evidence not stored: %v", err)
	}
	if _, err := os.Stat(technicalJournalDailyFilename(path, now)); err != nil {
		t.Fatalf("current-day evidence not stored: %v", err)
	}
	// A pre-existing older file must disappear during retention cleanup, but
	// only the known dated files belonging to this exact technical log.
	stale := now.AddDate(0, 0, -31)
	stalePath := technicalJournalDailyFilename(path, stale)
	if err := os.WriteFile(stalePath, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	neighbor := filepath.Join(dir, "another-system.2001-01-01")
	if err := os.WriteFile(neighbor, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := pruneTechnicalJournalDays(path, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("stale technical day not pruned: %v", err)
	}
	if got, err := os.ReadFile(neighbor); err != nil || string(got)!="untouched" {
		t.Fatalf("pruner touched unrelated log %q err=%v", got, err)
	}

	before, err := os.ReadFile(technicalJournalDailyFilename(path, now))
	if err != nil { t.Fatal(err) }
	a := &app{}
	w := httptest.NewRecorder()
	a.handleTechnicalJournalArchive(w, httptest.NewRequest("GET", "/api/journal/technical-archive", nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Disposition"), "30d-") {
		t.Fatalf("archive response: status=%d headers=%v", w.Code, w.Header())
	}
	gr, err := gzip.NewReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil { t.Fatal(err) }
	data, err := io.ReadAll(gr)
	if err != nil { t.Fatal(err) }
	_ = gr.Close()
	if strings.Contains(string(data), "hunter2") || strings.Contains(string(data), "server.example.org") {
		t.Fatal("export leaked credential or server identity")
	}
	var manifest map[string]any
	events := []technicalJournalRecord{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		raw := scanner.Bytes()
		if bytes.Contains(raw, []byte(`"type":"manifest"`)) {
			if err := json.Unmarshal(raw, &manifest); err != nil { t.Fatal(err) }
			continue
		}
		var record technicalJournalRecord
		if err := json.Unmarshal(raw, &record); err != nil { t.Fatalf("invalid JSONL: %v", err) }
		events = append(events, record)
	}
	if err := scanner.Err(); err != nil { t.Fatal(err) }
	if manifest["requested_days"] != float64(30) || manifest["read_only"] != true {
		t.Fatalf("invalid archive contract: %+v", manifest)
	}
	if len(events)!=2 || events[0].Result!="incident:start" || events[1].Result!="incident:recovered" {
		t.Fatalf("30-day incident correlation incomplete: %+v", events)
	}
	after, err := os.ReadFile(technicalJournalDailyFilename(path, now))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("read-only archive mutated source logs")
	}
}

func TestThirtyDayTechnicalJournalMarksDailyQuotaGap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "technical.jsonl")
	now := time.Now().UTC()
	daily := technicalJournalDailyFilename(path, now)
	if err := os.WriteFile(daily, []byte(strings.Repeat(" ", int(technicalJournalDailyQuota)-32)), 0600); err != nil {
		t.Fatal(err)
	}
	record := technicalJournalRecord{
		ObservedAt: now.Format(time.RFC3339Nano), Source: "auto",
		Kind: "AUTO VPN", Result: "candidate:failed", Severity: "warning",
		Message: "bounded error",
	}
	if err := appendTechnicalJournalRecord(path, record); err == nil {
		t.Fatal("unbounded daily log append was incorrectly accepted")
	}
	_, truncated := technicalJournalDailyFiles(path, now)
	if len(truncated)!=1 || truncated[0] != now.Format("2006-01-02") {
		t.Fatalf("archive failed to expose incomplete coverage: %v", truncated)
	}
	critical := record
	critical.Result = "incident:failed"
	critical.Severity = "critical"
	if err := appendTechnicalJournalRecord(path, critical); err != nil {
		t.Fatalf("critical event must fit its reserved segment: %v", err)
	}
	if _, err := os.Stat(daily+".critical"); err != nil {
		t.Fatalf("critical segment not durable: %v", err)
	}
}

func TestThirtyDayArchiveUIAndAuthenticatedEndpointContract(t *testing.T) {
	ui, err := webFS.ReadFile("web/settings-v3-core.js")
	if err != nil { t.Fatal(err) }
	if !strings.Contains(string(ui), `id="fn3JournalArchive30d"`) ||
		!strings.Contains(string(ui), "/api/journal/technical-archive") {
		t.Fatal("journal does not expose 30-day read-only archive")
	}
	api, err := os.ReadFile("settings_v3.go")
	if err != nil { t.Fatal(err) }
	if !strings.Contains(string(api), `mux.HandleFunc("GET /api/journal/technical-archive", a.requireAuth(a.handleTechnicalJournalArchive))`) {
		t.Fatal("technical archive endpoint must require normal FreeNet authentication")
	}
}
