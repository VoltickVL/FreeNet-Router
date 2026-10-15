package main

import (
 "bytes"
 "context"
 "errors"
 "net/http"
 "net/http/httptest"
 "os"
 "strings"
 "testing"
)

func TestBetaReleaseTagsAndSemanticOrder(t *testing.T) {
 for _, tag := range []string{"v0.8.0-beta.1","v0.8.0-beta.9","v0.8.0-beta.99"} {
  if !validBetaReleaseTag(tag) || !validSelectableReleaseTag(tag) || validReleaseTag(tag) { t.Fatalf("beta masquerades as stable: %q",tag) }
 }
 for _, tag := range []string{"v0.8.0-beta.0","v0.8.0-beta.100","v0.8.0-beta.01","v0.8.0-beta.-1","v0.8.0-rc.1","v0.8.0-BETA.1","v0.8.0-beta.1-extra","v0.8.100-beta.1","v0.8.0-beta.1;reboot"} {
  if validBetaReleaseTag(tag) || validSelectableReleaseTag(tag) { t.Fatalf("unsafe prerelease tag accepted: %q",tag) }
 }
 for _, tc := range []struct{newer,older string}{
  {"v0.8.0-beta.1","v0.7.14"}, {"v0.8.0-beta.2","v0.8.0-beta.1"},
  {"v0.8.0","v0.8.0-beta.99"}, {"v0.8.1-beta.1","v0.8.0"},
 } {
  if !releaseVersionGreater(tc.newer,tc.older) || releaseVersionGreater(tc.older,tc.newer) {
   t.Fatalf("incorrect semver: %s > %s",tc.newer,tc.older)
  }
 }
}

func TestBetaAndStableCatalogsAreDisjoint(t *testing.T) {
 raw:=[]githubFreeNetRelease{
  {TagName:"v0.7.14"}, {TagName:"v0.7.13"},
  {TagName:"v0.8.0", Prerelease:true},
  {TagName:"v0.8.0-beta.1", Prerelease:true},
  {TagName:"v0.8.0-beta.2", Prerelease:true},
  {TagName:"v0.8.0-beta.3", Prerelease:false},
  {TagName:"v0.8.0-beta.4", Prerelease:true,Draft:true},
 }
 stable:=normalizeSelfUpdateReleases(raw,"v0.7.14")
 if len(stable)!=2 || stable[0].Version!="v0.7.14" || stable[0].Prerelease {t.Fatalf("stable channel exposed Beta: %+v",stable)}
 beta:=normalizeSelfUpdateReleasesForChannel(raw,"v0.8.0-beta.1","beta")
 if len(beta)!=2 || beta[0].Version!="v0.8.0-beta.2" || !beta[0].Latest ||
    beta[1].Version!="v0.8.0-beta.1" || !beta[1].Current || !beta[1].Prerelease {
  t.Fatalf("Beta channel includes wrong tags: %+v",beta)
 }
}

func TestReleaseCacheIsolatedByChannel(t *testing.T) {
 oldDL,oldAPI:=selfUpdateReleaseDownload,selfUpdateReleasesAPI
 t.Cleanup(func(){selfUpdateReleaseDownload,selfUpdateReleasesAPI=oldDL,oldAPI;resetSelfUpdateReleaseCacheForTest()})
 resetSelfUpdateReleaseCacheForTest()
 selfUpdateReleasesAPI="https://unit.test/releases"
 calls:=0
 selfUpdateReleaseDownload=func(context.Context,string,int64)([]byte,error) {
  calls++
  return []byte("[{\"tag_name\":\"v0.7.14\"},{\"tag_name\":\"v0.8.0-beta.1\",\"prerelease\":true}]"),nil
 }
 stable,err:=fetchSelfUpdateReleaseCatalogForChannel(context.Background(),"v0.7.14",true,"stable")
 if err!=nil || len(stable)!=1 || stable[0].Version!="v0.7.14" || !stable[0].Current {t.Fatalf("stable: %+v %v",stable,err)}
 beta,err:=fetchSelfUpdateReleaseCatalogForChannel(context.Background(),"v0.7.14",true,"beta")
 if err!=nil || len(beta)!=1 || beta[0].Version!="v0.8.0-beta.1" || !beta[0].Prerelease {t.Fatalf("beta: %+v %v",beta,err)}
 if calls!=1 {t.Fatalf("Stable to Beta downloaded same published catalog twice: requests=%d",calls)}
 stable,err=fetchSelfUpdateReleaseCatalogForChannel(context.Background(),"v0.8.0-beta.1",true,"stable")
 if err!=nil || len(stable)!=1 || stable[0].Current {t.Fatalf("cached channel leaked wrong current flag: %+v %v",stable,err)}
 if calls!=1 {t.Fatalf("Beta to Stable repeated download: requests=%d",calls)}
 selfUpdateReleaseCache.Lock()
 selfUpdateReleaseCache.at=selfUpdateReleaseCache.at.Add(-2*60*1000000000)
 selfUpdateReleaseCache.Unlock()
 _,err=fetchSelfUpdateReleaseCatalogForChannel(context.Background(),"v0.7.14",true,"beta")
 if err!=nil || calls!=2 {t.Fatalf("expired fresh snapshot was not refreshed: requests=%d error=%v",calls,err)}
}

func TestBetaUnavailableNeverFallsBackToStableLatest(t *testing.T) {
 oldDL:=selfUpdateReleaseDownload
 t.Cleanup(func(){selfUpdateReleaseDownload=oldDL;resetSelfUpdateReleaseCacheForTest()})
 resetSelfUpdateReleaseCacheForTest()
 selfUpdateReleaseDownload=func(context.Context,string,int64)([]byte,error){return nil,errors.New("Beta catalogue offline")}
 t.Setenv("FREENET_SETTINGS_V3_HISTORY",t.TempDir()+"/history.tsv")
 rr:=httptest.NewRecorder()
 (&app{}).handleSelfUpdateReleases(rr,httptest.NewRequest("GET","/api/system/update/releases?channel=beta",nil))
 if rr.Code!=http.StatusBadGateway || !strings.Contains(rr.Body.String(),"Каталог Beta сейчас недоступен") {
  t.Fatalf("Beta fail-open into Stable: status=%d body=%s",rr.Code,rr.Body.String())
 }
}

func TestBetaPlanNeedsExplicitChannelBeforeHelper(t *testing.T) {
 for _,tc:=range []struct{query string;code int}{
  {"target=v0.8.0-beta.1",http.StatusForbidden},
  {"target=v0.8.0-beta.1&channel=stable",http.StatusForbidden},
  {"target=v0.7.14&channel=beta",http.StatusBadRequest},
  {"target=v0.8.0-beta.100&channel=beta",http.StatusBadRequest},
  {"channel=rc",http.StatusBadRequest},
 } {
  rr:=httptest.NewRecorder()
  (&app{}).handleSelfUpdatePlan(rr,httptest.NewRequest("GET","/api/system/update/plan?"+tc.query,nil))
  if rr.Code!=tc.code {t.Fatalf("query %s: code=%d want=%d, body=%s",tc.query,rr.Code,tc.code,rr.Body.String())}
 }
}

func TestBetaApplyCannotStartWithoutAcknowledgement(t *testing.T) {
 root:=t.TempDir()
 t.Setenv("FREENET_AUTOMATION_STATE",root+"/auto.state")
 t.Setenv("FREENET_VPN_TRANSACTION_DIR",root+"/no-pending")
 req:=httptest.NewRequest("POST","http://router.test/api/system/update/apply",
    bytes.NewBufferString("{\"target_tag\":\"v0.8.0-beta.1\"}"))
 req.Header.Set("Content-Type","application/json")
 req.Header.Set("Origin","http://router.test")
 rr:=httptest.NewRecorder()
 (&app{}).handleSelfUpdateApply(rr,req)
 if rr.Code!=http.StatusForbidden || !strings.Contains(rr.Body.String(),"explicit Beta confirmation required") {
  t.Fatalf("Beta apply without consent: status=%d body=%s",rr.Code,rr.Body.String())
 }
}

func TestBetaWorkflowAndBrowserContract(t *testing.T) {
 required:=map[string][]string{
  "web/self-update.js":{"channel=beta","Beta · тестовая","beta_acknowledged: true","Установить экспериментальную","onConfirm: startUpdate"},
  "../scripts/self_update.sh":{"FREENET_UPDATE_BETA_OPT_IN","META_PRE","explicit Beta opt-in required"},
  "../install.sh":{"FREENET_INSTALL_RELEASE_TAG","FREENET_INSTALL_BETA","releases/latest/download"},
  "../.github/workflows/release.yml":{"beta/v0.8.0","--prerelease --latest=false","stable/v0.7","notes-file"},
 }
 for p,phrases:=range required {
  data,err:=os.ReadFile(p);if err!=nil{t.Fatal(err)}
  for _,want:=range phrases{if !strings.Contains(string(data),want){t.Fatalf("%s missing contract %q",p,want)}}
 }
}
