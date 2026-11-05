package main

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func TestJournalDiagnosticsRedactsSecrets(t *testing.T) {
 input := "token=abc123; password=hunter2 vless://abcdef@example.net:443 ip=192.0.2.12 UUID=123e4567-e89b-12d3-a456-426614174000 Authorization: Bearer secretBearer999 email=alice@example.org"
 redacted := journalDiagnosticRedact(input)
 for _, needle := range []string{"abc123", "hunter2", "abcdef", "example.net", "192.0.2.12", "123e4567", "secretBearer999", "alice@example.org"} {
  if strings.Contains(redacted, needle) { t.Fatalf("secret leaked: %q in %q",needle,redacted) }
 }
}

func TestJournalDiagnosticsIsReadOnlyAndRetainsStages(t *testing.T) {
 root := t.TempDir()
 auto := filepath.Join(root,"auto.history")
 settings := filepath.Join(root,"settings.history")
 autoState := filepath.Join(root,"auto.state")
 settingsState := filepath.Join(root,"settings.state")
 cfg := filepath.Join(root,"freenet.conf")
 update := filepath.Join(root,"update.state")
 for path,content := range map[string]string{
  auto: "2026-10-10T00:01:00Z\tAUTO VPN\tincident:start\tincident=abc; first\n2026-10-10T00:01:01Z\tAUTO VPN\tincident:start\tincident=abc; first\n2026-10-10T00:01:02Z\tAUTO VPN\tconfirm:failed\tserver.example.com\n",
  settings:"2026-10-10T00:02:00Z\tfreenet_update\tsuccess\tupdated\n",
  autoState:"LAST_RESULT=failed\nMUTATION_BLOCKED=yes\n",
  settingsState:"HEALTH_LAST=2026-10-10T00:02:00Z\nQUALITY_DEGRADED_COUNT=2\nHEALTH_MEDIA_LAST=2026-10-10T00:01:30Z\nQUALITY_MEDIA_COUNT=2\nQUALITY_MEDIA_SINCE=2026-10-10T00:00:00Z\nQUALITY_MEDIA_LAST=2026-10-10T00:01:00Z\nQUALITY_MEDIA_HAD_GOOD=yes\nQUALITY_MEDIA_IDENTITY=private-dont-export-identity\nCLIENT_CAPTURE_LAST=2026-10-10T00:57:00Z\nCLIENT_CAPTURE_STATE=installed-unverified\nCLIENT_CAPTURE_TCP=tcp-prerouting-hook-present\nCLIENT_CAPTURE_UDP=tproxy-rule-present\nCLIENT_FLOW_OBSERVED_AT=2026-10-10T00:57:00Z\nCLIENT_FLOW_TCP_RESULT=observed\nCLIENT_FLOW_UDP_RESULT=no-increase\nCLIENT_FLOW_TCP_PACKETS=987654321\n",
  cfg:"AUTO_VPN_V1=yes\nAUTO_VPN_MODE=best\nAUTO_VPN_POLICY=degraded\nAUTO_VPN_HEALTH_INTERVAL=5m\n",
  update:"STATE=SUCCESS\nFROM_VERSION=v0.7.0\nTARGET_VERSION=v0.7.1\n",
 } {
  if err:=os.WriteFile(path,[]byte(content),0600);err!=nil{t.Fatal(err)}
 }
 t.Setenv("FREENET_AUTOMATION_HISTORY",auto)
 t.Setenv("FREENET_SETTINGS_V3_HISTORY",settings)
 t.Setenv("FREENET_AUTOMATION_STATE",autoState)
 t.Setenv("FREENET_SETTINGS_V3_STATE",settingsState)
 before,_:=os.ReadFile(auto)
 a:=&app{cfg:config{ConfigPath:cfg,UpdateState:update}}
 report:=a.journalDiagnostics(time.Date(2026,10,10,1,0,0,0,time.UTC))
 if report.Mode!="read-only" || report.SourceCount["auto"]!=3 || len(report.Events)!=4 {t.Fatalf("incomplete diagnostic snapshot: %+v",report)}
 if !report.AutoVPN.MutationBlocked || report.AutoVPN.QualityDegradedCount!=2 {t.Fatalf("missing state: %+v",report.AutoVPN)}
 if report.AutoVPN.QualityMediaCount==nil || *report.AutoVPN.QualityMediaCount!=2 ||
  report.AutoVPN.QualityMediaSince!="2026-10-10T00:00:00Z" ||
  report.AutoVPN.QualityMediaLast!="2026-10-10T00:01:00Z" ||
  report.AutoVPN.HealthMediaLast!="2026-10-10T00:01:30Z" ||
  report.AutoVPN.QualityMediaHadGood!="yes" {
   t.Fatalf("media evidence missing from read-only snapshot: %+v",report.AutoVPN)
 }
 if report.AutoVPN.ClientCapture.State!="installed-unverified" ||
  report.AutoVPN.ClientCapture.TCP!="tcp-prerouting-hook-present" ||
  report.AutoVPN.ClientCapture.UDP!="tproxy-rule-present" ||
  report.AutoVPN.ClientCapture.HookActivity.TCP!="observed" ||
  report.AutoVPN.ClientCapture.HookActivity.UDP!="no-increase" {
   t.Fatalf("read-only journal lost automatic LAN inventory: %+v",report.AutoVPN.ClientCapture)
 }
 if strings.Contains(report.Events[1].Message,"server.example.com"){t.Fatal("endpoint hostname leaked")}
 after,_:=os.ReadFile(auto)
 if string(before)!=string(after){t.Fatal("diagnostic modified source log")}
 rec:=httptest.NewRecorder()
 a.handleJournalDiagnostics(rec,httptest.NewRequest(http.MethodGet,"/api/journal/diagnostics",nil))
 if rec.Code!=200{t.Fatalf("HTTP %d: %s",rec.Code,rec.Body.String())}
 var decoded journalDiagnosticReport
 if err:=json.Unmarshal(rec.Body.Bytes(),&decoded);err!=nil{t.Fatal(err)}
 if len(decoded.Events)!=4 {t.Fatalf("export lost events: %d",len(decoded.Events))}
 if decoded.AutoVPN.QualityMediaCount==nil || *decoded.AutoVPN.QualityMediaCount!=2 ||
  !strings.Contains(rec.Body.String(), "\"quality_media_count\": 2") {
   t.Fatalf("JSON lost media evidence: %s", rec.Body.String())
 }
 if !strings.Contains(rec.Body.String(),"\"client_capture\"") {
  t.Fatalf("client capture omitted: %s",rec.Body.String())
 }
 if strings.Contains(rec.Body.String(),"987654321") {
  t.Fatal("internal netfilter packet count leaked into technical export")
 }
if strings.Contains(rec.Body.String(),"private-dont-export-identity") {
  t.Fatal("internal exact VPN identity leaked into diagnostics")
 }
}

func TestJournalMediaEvidenceIsBoundedAndMissingIsUnknown(t *testing.T) {
 for _,tc:=range []struct{raw string; want *int}{
  {"",nil}, {"-1",nil}, {"4",nil}, {"9999999999999999999999999",nil},
  {"bad",nil}, {"0",func() *int {v:=0;return &v}()},
  {"1",func() *int {v:=1;return &v}()}, {"2",func() *int {v:=2;return &v}()},
  {"3",func() *int {v:=3;return &v}()},
 } {
  got:=journalDiagnosticMediaCount(tc.raw)
  if (got==nil)!=(tc.want==nil) || got!=nil && *got!=*tc.want {
   t.Fatalf("raw=%q got=%v expected=%v",tc.raw,got,tc.want)
  }
 }
 now:=time.Date(2026,10,10,1,0,0,0,time.UTC)
 for _,tc:=range []struct{raw,want string}{
  {"",""}, {"broken",""}, {"2026-10-10T01:02:00Z",""}, // future >1m
  {"2026-10-10T00:59:59.123456789Z","2026-10-10T00:59:59.123456789Z"},
 } {
  if got:=journalDiagnosticMediaTime(tc.raw,now);got!=tc.want{
   t.Fatalf("time raw=%q got=%q want=%q",tc.raw,got,tc.want)
  }
 }
 for _,tc:=range []struct{raw,want string}{
  {"yes","yes"},{"no",""},{"yes injected-token",""},{"",""},{"anything",""},
 } {
  if got:=journalDiagnosticMediaGoodFlag(tc.raw);got!=tc.want {
   t.Fatalf("flag raw=%q got=%q want=%q",tc.raw,got,tc.want)
  }
 }
 // A real stored zero is different from missing evidence in the JSON contract.
 zero:=journalDiagnosticMediaCount("0")
 payload,err:=json.Marshal(journalDiagnosticAuto{QualityMediaCount:zero})
 if err!=nil {t.Fatal(err)}
 if !strings.Contains(string(payload), "\"quality_media_count\":0") {
  t.Fatalf("stored zero was lost: %s",payload)
 }
 absent,err:=json.Marshal(journalDiagnosticAuto{})
 if err!=nil {t.Fatal(err)}
 if strings.Contains(string(absent),"quality_media_count") {
  t.Fatalf("unknown media count was invented: %s",absent)
 }
}
