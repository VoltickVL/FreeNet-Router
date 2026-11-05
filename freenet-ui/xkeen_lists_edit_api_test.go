package main

import (
 "bytes"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func xkeenListTestHarness(t *testing.T) (*app,string) {
 t.Helper()
 dir:=t.TempDir()
 t.Setenv("FREENET_XKEEN_CONFIG_DIR",dir)
 a:=&app{cfg:config{
  LockPath:filepath.Join(dir,"writer.lock"),
  UpdateLock:filepath.Join(dir,"self-update.lock"),
  UpdateState:filepath.Join(dir,"update.state"),
 },sem:make(chan struct{},1)}
 return a,dir
}
func xkeenListSaveTestRequest(t *testing.T,a *app,req xkeenListSaveRequest) (int,xkeenListSaveResponse) {
 t.Helper()
 raw,err:=json.Marshal(req);if err!=nil{t.Fatal(err)}
 r:=httptest.NewRequest(http.MethodPost,"http://router/api/config-studio/list/save",bytes.NewReader(raw))
 r.Header.Set("Content-Type","application/json")
 w:=httptest.NewRecorder()
 a.handleXKeenListSave(w,r)
 var result xkeenListSaveResponse
 if err:=json.Unmarshal(w.Body.Bytes(),&result);err!=nil{t.Fatalf("non JSON response status=%d",w.Code)}
 return w.Code,result
}

func TestXKeenListValidationRejectsDangerousAndAmbiguousContent(t *testing.T) {
 for _,tc:=range []struct{name,file,text string;want bool}{
  {"commented-lan","ip_exclude.lst","#192.168.0.0/16\n198.51.100.4/32\n",true},
  {"ipv6-host","ip_exclude.lst","2001:db8::5/128\n",true},
  {"single-port","port_proxying.lst","443\n",true},
  {"range","port_proxying.lst","# example\n596:599\n",true},
  {"unsafe-lan","ip_exclude.lst","192.168.0.0/16\n",false},
  {"unsafe-private","ip_exclude.lst","10.0.0.0/8\n",false},
  {"unsafe-ipv6","ip_exclude.lst","fc00::/7\n",false},
  {"bare-IP","ip_exclude.lst","203.0.113.1\n",false},
  {"bad-prefix","ip_exclude.lst","not-an-ip/32\n",false},
  {"bad-port-0","port_proxying.lst","0\n",false},
  {"bad-port-65536","port_exclude.lst","65536\n",false},
  {"backward-range","port_proxying.lst","800:700\n",false},
  {"same-range","port_proxying.lst","443:443\n",false},
  {"invalid-port","port_proxying.lst","+443\n",false},
  {"shell-injection","port_exclude.lst","443;reboot\n",false},
  {"inline-comment","port_proxying.lst","443 # comment\n",false},
  {"unknown-file","xkeen.json","443\n",false},
  {"nul-byte","port_proxying.lst","443\x00\n",false},
 } {
  t.Run(tc.name,func(t *testing.T){
   got,err:=validateXKeenListText(tc.file,tc.text)
   if (err==nil)!=tc.want {t.Fatalf("valid=%v want=%v err=%v",err==nil,tc.want,err)}
   if err==nil && got!=tc.text {t.Fatal("comments or line contents changed unexpectedly")}
  })
 }
}

func TestXKeenListSaveCASAtomicSnapshotAndNoRuntimeApply(t *testing.T) {
 a,dir:=xkeenListTestHarness(t)
 old:=[]byte("# comment stays\n443\n")
 if err:=os.WriteFile(filepath.Join(dir,"port_proxying.lst"),old,0600);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(dir,"port_exclude.lst"),[]byte("# empty\n"),0600);err!=nil{t.Fatal(err)}
 updated:="# comment stays\n443\n8443\n"
 code,result:=xkeenListSaveTestRequest(t,a,xkeenListSaveRequest{
  File:"port_proxying.lst",Text:updated,ExpectedSHA256:sha256Hex(old)})
 if code!=http.StatusOK || !result.Success || result.Mutation!="SAVED" ||
    result.Activation!="XKEEN_RELOAD_REQUIRED" || result.Rollback!="NOT_NEEDED" {
  t.Fatalf("save rejected: status=%d result=%+v",code,result)
 }
 data,e:=os.ReadFile(filepath.Join(dir,"port_proxying.lst"))
 if e!=nil || string(data)!=updated {t.Fatalf("saved bytes differ: %v",e)}
 if result.SHA256!=sha256Hex(data) {t.Fatal("reported hash differs from live file")}
 files,e:=filepath.Glob(filepath.Join(dir,".freenet-backups","config-studio-*","port_proxying.lst"))
 if e!=nil || len(files)!=1 {t.Fatalf("durable snapshot absent: %v",e)}
 snapshot,e:=os.ReadFile(files[0])
 if e!=nil || !bytes.Equal(snapshot,old) {t.Fatal("snapshot did not preserve exact original bytes")}
 if _,e:=os.Stat(a.cfg.LockPath);!os.IsNotExist(e){t.Fatal("successful save left writer lock")}
 staleCode,stale:=xkeenListSaveTestRequest(t,a,xkeenListSaveRequest{
  File:"port_proxying.lst",Text:"443\n9000\n",ExpectedSHA256:sha256Hex(old)})
 if staleCode!=http.StatusConflict || stale.Mutation!="NONE" {t.Fatal("stale CAS silently overwrote live list")}
 verify,_:=os.ReadFile(filepath.Join(dir,"port_proxying.lst"))
 if string(verify)!=updated {t.Fatal("stale CAS mutated list")}
}

func TestXKeenListSaveRejectsConflictingPortPoliciesAndSymlinks(t *testing.T) {
 a,dir:=xkeenListTestHarness(t)
 if err:=os.WriteFile(filepath.Join(dir,"port_proxying.lst"),[]byte("80\n443\n"),0600);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(dir,"port_exclude.lst"),[]byte("# no entries\n"),0600);err!=nil{t.Fatal(err)}
 original,_:=os.ReadFile(filepath.Join(dir,"port_exclude.lst"))
 code,result:=xkeenListSaveTestRequest(t,a,xkeenListSaveRequest{
  File:"port_exclude.lst",Text:"53\n",ExpectedSHA256:sha256Hex(original)})
 if code!=http.StatusUnprocessableEntity || result.Mutation!="NONE" {t.Fatal("conflicting port policies accepted")}
 after,_:=os.ReadFile(filepath.Join(dir,"port_exclude.lst"))
 if !bytes.Equal(after,original){t.Fatal("conflicting ports changed disk")}
 outside:=filepath.Join(t.TempDir(),"private")
 if err:=os.WriteFile(outside,[]byte("203.0.113.5/32\n"),0600);err!=nil{t.Fatal(err)}
 if err:=os.Symlink(outside,filepath.Join(dir,"ip_exclude.lst"));err!=nil{t.Skip(err)}
 code,result=xkeenListSaveTestRequest(t,a,xkeenListSaveRequest{File:"ip_exclude.lst",Text:"198.51.100.1/32\n"})
 if code!=http.StatusServiceUnavailable || result.Success {t.Fatal("symlinked list was edited")}
 out,_:=os.ReadFile(outside)
 if string(out)!="203.0.113.5/32\n" {t.Fatal("symlink target mutated")}
 tab:=readConfigStudioListFile("ip_exclude.lst")
 if tab.Access!="read-only" {t.Fatal("symlink should be blocked in editor")}
}

func TestXKeenListSaveRejectsDuplicateJSONAndConcurrentWriter(t *testing.T) {
 a,dir:=xkeenListTestHarness(t)
 body:=[]byte(`{"file":"port_proxying.lst","file":"port_exclude.lst","text":"53","expected_sha256":""}`)
 r:=httptest.NewRequest(http.MethodPost,"http://router/api/config-studio/list/save",bytes.NewReader(body))
 r.Header.Set("Content-Type","application/json")
 w:=httptest.NewRecorder()
 a.handleXKeenListSave(w,r)
 if w.Code!=http.StatusBadRequest {t.Fatalf("ambiguous JSON accepted: %d",w.Code)}
 if err:=os.Mkdir(a.cfg.LockPath,0700);err!=nil{t.Fatal(err)}
 code,result:=xkeenListSaveTestRequest(t,a,xkeenListSaveRequest{File:"port_proxying.lst",Text:"443\n"})
 if code!=http.StatusConflict || result.Mutation!="NONE" {t.Fatal("concurrent writer lock bypassed")}
 if _,err:=os.Stat(filepath.Join(dir,"port_proxying.lst"));!os.IsNotExist(err){t.Fatal("blocked write unexpectedly created file")}
}

func TestXKeenListTabsAreEditableOnlyForSafeSources(t *testing.T) {
 _,dir:=xkeenListTestHarness(t)
 missing:=readConfigStudioListFile("port_proxying.lst")
 if missing.Access!="editable" || missing.Present {t.Fatal("missing supported list must permit guarded creation")}
 textValue:="# keep comments\n443\n"
 if err:=os.WriteFile(filepath.Join(dir,"port_proxying.lst"),[]byte(textValue),0600);err!=nil{t.Fatal(err)}
 present:=readConfigStudioListFile("port_proxying.lst")
 if present.Access!="editable" || !present.Present || present.Text!=textValue ||
  !strings.EqualFold(present.SHA256,sha256Hex([]byte(textValue))){
  t.Fatal("editor omitted live bytes, revision or comment fidelity")
 }
}
