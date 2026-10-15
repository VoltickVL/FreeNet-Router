package main

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func TestTechnicalJournalSignalFilter(t *testing.T) {
 cases := []struct{
  name string; event automationEvent; keep bool
 }{
  {"routine",automationEvent{Kind:"AUTO VPN",Result:"same",Message:"VPN healthy; no action"},false},
  {"incident start",automationEvent{Kind:"AUTO VPN",Result:"incident:start",Message:"incident=abc; started"},true},
  {"recovery",automationEvent{Kind:"AUTO VPN",Result:"rollback:failed",Message:"incident=abc; rollback failed"},true},
  {"confirm",automationEvent{Kind:"AUTO VPN",Result:"confirm:failed",Message:"VPN failed twice"},true},
  {"manual",automationEvent{Kind:"vpn_manual",Result:"success",Message:"manual switch"},true},
  {"update",automationEvent{Kind:"freenet_update",Result:"success",Message:"updated"},true},
  {"backup",automationEvent{Kind:"backup",Result:"success",Message:"backup created"},true},
  {"catalog cancel",automationEvent{Kind:"freenet_release_catalog",Result:"canceled",Message:"browser navigation"},false},
  {"catalog error",automationEvent{Kind:"freenet_release_catalog",Result:"failed",Message:"catalog unreachable"},true},
 }
 for _,tc := range cases {
  t.Run(tc.name,func(t *testing.T){
   if actual:=technicalJournalSignificant(tc.event);actual!=tc.keep{t.Fatalf("keep=%v expected=%v",actual,tc.keep)}
  })
 }
}

func TestTechnicalJournalRedactsBeforeQueueAndDoesNotBlock(t *testing.T) {
 old:=technicalJournalQueue
 t.Cleanup(func(){technicalJournalQueue=old})
 technicalJournalQueue=make(chan technicalJournalRecord,1)
 root:=t.TempDir()
 t.Setenv("FREENET_AUTOMATION_STATE",filepath.Join(root,"auto.state"))
 t.Setenv("FREENET_SETTINGS_V3_STATE",filepath.Join(root,"health.state"))
 event:="2026-10-10T00:41:00Z\tAUTO VPN\tincident:start\tincident=incident42; password=secret123; target=server.example.org:443\n"
 technicalJournalObserve(automationHistoryPath(),event)
 select {
 case got:= <-technicalJournalQueue:
  if got.Incident!="incident42" {t.Fatalf("missing correlation id %q",got.Incident)}
  if strings.Contains(got.Message,"secret123")||strings.Contains(got.Message,"server.example.org") {t.Fatalf("private event data leaked %q",got.Message)}
  if got.Source!="auto"||got.Severity!="info" {t.Fatalf("unexpected record %+v",got)}
 default: t.Fatal("significant event not recorded")
 }
 // Saturation of the side channel must never wait for a reader.
 technicalJournalQueue<-technicalJournalRecord{}
 before:=time.Now()
 technicalJournalObserve(automationHistoryPath(),event)
 if time.Since(before)>250*time.Millisecond {t.Fatal("telemetry queue blocked writer")}
}

func TestTechnicalJournalIsBoundedAndCanBeRead(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "technical.jsonl")
	now := time.Now().UTC()
	daily := technicalJournalDailyFilename(path, now)
	// One day is capped, but a terminal incident uses its reserved critical
	// segment rather than disappearing after a routine diagnostic storm.
	if err := os.WriteFile(daily, []byte(strings.Repeat(" ", int(technicalJournalDailyQuota)-32)), 0600); err != nil { t.Fatal(err) }
	record := technicalJournalRecord{
		ObservedAt: now.Format(time.RFC3339Nano), EventAt: now.Format(time.RFC3339),
		Kind: "AUTO VPN", Result: "incident:failed", Severity: "critical",
		Message: "detected confirmed failure",
	}
	if err := appendTechnicalJournalRecord(path, record); err != nil { t.Fatal(err) }
	if _, err := os.Stat(daily+".critical"); err != nil { t.Fatalf("critical reserve missing: %v", err) }
	rows := readTechnicalJournalRecords(path, 10)
	if len(rows)!=1 || rows[0].Result!="incident:failed" { t.Fatalf("critical record unavailable: %+v", rows) }
	st, err := os.Stat(daily+".critical")
	if err!=nil { t.Fatal(err) }
	if st.Size() >= technicalJournalCriticalQuota { t.Fatalf("critical reserve grew unbounded: %d", st.Size()) }
}
