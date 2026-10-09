package main

import (
 "bufio"
 "encoding/json"
 "os"
 "path/filepath"
 "strconv"
 "strings"
 "sync"
 "sync/atomic"
 "time"
)

// Technical recording is strictly observational. No probe, scheduler, Xray,
// AUTO mutation, config edit, network request or change to the public journal.
const (
 technicalJournalMaxBytes int64 = 2 * 1024 * 1024
 technicalJournalMaxExportLines = 1200
 technicalJournalQueueSize = 128
)

type technicalJournalState struct {
 AutoResult string `json:"auto_result,omitempty"`
 MutationBlocked bool `json:"mutation_blocked"`
 RollbackReady string `json:"rollback_ready,omitempty"`
 HealthResult string `json:"health_result,omitempty"`
 HealthLast string `json:"health_last,omitempty"`
 QualityCount int `json:"quality_count,omitempty"`
}

type technicalJournalRecord struct {
 ObservedAt string `json:"observed_at"`
 EventAt string `json:"event_at"`
 Source string `json:"source"`
 Kind string `json:"kind"`
 Stage string `json:"stage,omitempty"`
 Result string `json:"result"`
 Severity string `json:"severity"`
 Message string `json:"message"`
 Incident string `json:"incident,omitempty"`
 State technicalJournalState `json:"state"`
}

var (
 technicalJournalQueue chan technicalJournalRecord
 technicalJournalInit sync.Once
 technicalJournalDropped atomic.Uint64
 technicalJournalWriteErrors atomic.Uint64
)

// Technical logging is started only by main() after process setup; tests or
// passive journal consumers cannot accidentally start a writer.
func startTechnicalJournalRecorder() {
 technicalJournalInit.Do(func() {
  technicalJournalQueue = make(chan technicalJournalRecord, technicalJournalQueueSize)
  go func(queue <-chan technicalJournalRecord) {
   recent := map[string]time.Time{}
   for record := range queue {
    // Do not squash incident stages or rollback; only repeat routine noise.
    key := record.Kind + "\x00" + record.Result + "\x00" + record.Message
    now := time.Now().UTC()
    if record.Incident == "" && record.Severity == "info" {
     if last, ok := recent[key]; ok && now.Sub(last) < 2*time.Minute { continue }
     recent[key] = now
    }
    if len(recent) > 256 { recent = map[string]time.Time{} }
    if appendTechnicalJournalRecord(technicalJournalPath(), record) != nil { technicalJournalWriteErrors.Add(1) }
   }
  }(technicalJournalQueue)
 })
}

func technicalJournalPath() string {
 if val := strings.TrimSpace(os.Getenv("FREENET_TECH_JOURNAL")); val != "" { return val }
 return "/opt/var/log/freenet-technical.jsonl"
}

func technicalJournalSeverity(result, message string) string {
 combined := strings.ToLower(result + " " + message)
 switch {
 case strings.Contains(combined,"rollback_failed"), strings.Contains(combined,"critical"), strings.Contains(combined,"incident:failed"):
  return "critical"
 case strings.Contains(combined,"failed"), strings.Contains(combined,"blocked"), strings.Contains(combined,"uncertain"), strings.Contains(combined,"error"), strings.Contains(combined,"degrad"):
  return "warning"
 default: return "info"
 }
}

func technicalJournalSignificant(event automationEvent) bool {
 kind := strings.ToLower(strings.TrimSpace(event.Kind))
 result := strings.ToLower(strings.TrimSpace(event.Result))
 msg := strings.ToLower(event.Message)
 if kind == "freenet_release_catalog" && !(strings.Contains(result,"failed") || strings.Contains(result,"error")) { return false }
 if kind == "auto vpn" || kind == "auto_vpn" {
  // Ordinary healthy checks and pending-quality heartbeats stay out of the
  // technical spool. Raw source history is still available in deep export.
  if journalEventIsRoutineAutoNoise(event) || journalEventIsPendingQualityObservation(event) {
   if !strings.Contains(result,"critical") && !strings.Contains(result,"failed") && !strings.Contains(result,"incident") { return false }
  }
  keys := []string{"incident", "rollback", "apply", "post_update", "recovery", "confirm", "candidate", "endpoint_refresh", "selection", "quality", "blocked", "uncertain", "failed", "critical", "degraded", "wan", "detect", "single_flight"}
  for _, key := range keys { if strings.Contains(result,key) || strings.Contains(msg,"incident=") && key=="incident" { return true } }
  return false
 }
 if kind == "vpn switch" { return false } // canonical manual record is separate
 if kind == "vpn" || kind == "vpn_manual" { return true }
 if kind == "freenet_update" || kind == "freenet_update_recovery" || kind == "xray_runtime" { return true }
 if strings.Contains(result,"failed") || strings.Contains(result,"error") || strings.Contains(result,"rollback") { return true }
 return false
}

func technicalJournalIncident(message string) string {
 for _, field := range strings.Split(message,";") {
  item := strings.TrimSpace(field)
  if strings.HasPrefix(item,"incident=") {
   val := strings.TrimSpace(strings.TrimPrefix(item,"incident="))
   if len(val) > 48 { val = val[:48] }
   return journalDiagnosticRedact(val)
  }
 }
 return ""
}

func technicalJournalStateSnapshot() technicalJournalState {
 auto := parseAutomationState(automationStatePath())
 health := v3ParseState(settingsV3StatePath())
 count, _ := strconv.Atoi(health["QUALITY_DEGRADED_COUNT"])
 if count < 0 { count = 0 }
 return technicalJournalState{
  AutoResult: journalDiagnosticRedact(auto["LAST_RESULT"]),
  MutationBlocked: strings.EqualFold(auto["MUTATION_BLOCKED"],"yes"),
  RollbackReady: journalDiagnosticRedact(auto["ROLLBACK_READY"]),
  HealthResult: journalDiagnosticRedact(health["HEALTH_RESULT"]),
  HealthLast: health["HEALTH_LAST"],
  QualityCount: count,
 }
}

// Called only *after* legacy journal write; enqueue cannot block AUTO VPN,
// and a full queue is counted and dropped instead of delaying its decisions.
func technicalJournalObserve(path, line string) {
 queue := technicalJournalQueue
 if queue == nil { return }
 parts := strings.SplitN(strings.TrimSuffix(line,"\n"),"\t",4)
 if len(parts) != 4 { return }
 event := automationEvent{At:parts[0],Kind:parts[1],Result:parts[2],Message:parts[3]}
 if !technicalJournalSignificant(event) { return }
 source := "system"
 if path == automationHistoryPath() { source = "auto" }
 stage := ""
 if i := strings.Index(event.Result,":"); i > 0 { stage = event.Result[:i] }
 record := technicalJournalRecord{
  ObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
  EventAt: event.At, Source: source,
  Kind: journalDiagnosticRedact(event.Kind), Stage: journalDiagnosticRedact(stage),
  Result: journalDiagnosticRedact(event.Result),
  Severity: technicalJournalSeverity(event.Result,event.Message),
  Message: journalDiagnosticRedact(event.Message),
  Incident: technicalJournalIncident(event.Message),
  State: technicalJournalStateSnapshot(),
 }
 select { case queue <- record: default: technicalJournalDropped.Add(1) }
}

func appendTechnicalJournalRecord(path string, record technicalJournalRecord) error {
 payload, err := json.Marshal(record)
 if err != nil { return err }
 payload = append(payload,'\n')
 if err = os.MkdirAll(filepath.Dir(path),0700);err != nil {return err}
 if info, statErr := os.Stat(path);statErr == nil && info.Size()+int64(len(payload)) > technicalJournalMaxBytes {
  _ = os.Remove(path+".1")
  if err := os.Rename(path,path+".1");err != nil { return err }
 }
 f, err := os.OpenFile(path,os.O_CREATE|os.O_APPEND|os.O_WRONLY,0600)
 if err != nil { return err }
 _, err = f.Write(payload)
 closeErr := f.Close()
 if err != nil {return err}
 return closeErr
}

func readTechnicalJournalRecords(path string, limit int) []technicalJournalRecord {
 if limit <= 0 || limit > technicalJournalMaxExportLines {limit = technicalJournalMaxExportLines}
 all := make([]technicalJournalRecord,0,limit)
 for _, part := range []string{path+".1",path} {
  file,err := os.Open(part)
  if err!=nil {continue}
  scanner := bufio.NewScanner(file)
  scanner.Buffer(make([]byte,4096),32*1024)
  for scanner.Scan() {
   var record technicalJournalRecord
   if json.Unmarshal(scanner.Bytes(),&record)==nil {
    // Defense in depth for historical files from another build.
    record.Message = journalDiagnosticRedact(record.Message)
    record.Kind = journalDiagnosticRedact(record.Kind)
    record.Result = journalDiagnosticRedact(record.Result)
    record.Incident = journalDiagnosticRedact(record.Incident)
    record.State.AutoResult = journalDiagnosticRedact(record.State.AutoResult)
    record.State.HealthResult = journalDiagnosticRedact(record.State.HealthResult)
    all = append(all,record)
    if len(all)>limit {all = all[1:]}
   }
  }
  _ = file.Close()
 }
 return all
}
