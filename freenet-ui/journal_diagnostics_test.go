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
 input := "token=abc123; password=hunter2 vless://abcdef@example.net:443 ip=192.0.2.12 UUID=123e4567-e89b-12d3-a456-426614174000"
 redacted := journalDiagnosticRedact(input)
 for _, needle := range []string{"abc123", "hunter2", "abcdef", "example.net", "192.0.2.12", "123e4567"} {
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
  settingsState:"HEALTH_LAST=2026-10-10T00:02:00Z\nQUALITY_DEGRADED_COUNT=2\n",
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
 if strings.Contains(report.Events[1].Message,"server.example.com"){t.Fatal("endpoint hostname leaked")}
 after,_:=os.ReadFile(auto)
 if string(before)!=string(after){t.Fatal("diagnostic modified source log")}
 rec:=httptest.NewRecorder()
 a.handleJournalDiagnostics(rec,httptest.NewRequest(http.MethodGet,"/api/journal/diagnostics",nil))
 if rec.Code!=200{t.Fatalf("HTTP %d: %s",rec.Code,rec.Body.String())}
 var decoded journalDiagnosticReport
 if err:=json.Unmarshal(rec.Body.Bytes(),&decoded);err!=nil{t.Fatal(err)}
 if len(decoded.Events)!=4 {t.Fatalf("export lost events: %d",len(decoded.Events))}
}
