package main

import (
 "archive/zip"
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "runtime"
 "strings"
 "testing"
 "time"
)

func TestTunnelInstallPlanNeverOverwritesAndStopsUnsupported(t *testing.T){
 base:=t.TempDir()
 dst:=filepath.Join(base,"freenet-tunnel-client")
 tests:=[]struct{os,arch string;ready bool}{
  {"linux","arm64",true},{"linux","amd64",true},
  {"linux","mipsle",false},{"linux","mips",false},{"linux","arm",false},{"darwin","arm64",false},
 }
 for _,tc:=range tests {
  p:=tunnelPlanFor(tc.os,tc.arch,dst)
  if p.Ready!=tc.ready || p.ExternalConnected || p.Mutation!="NONE" {t.Fatalf("%s/%s: %+v",tc.os,tc.arch,p)}
 }
 if err:=os.Symlink("/dev/null",dst);err!=nil{t.Fatal(err)}
 if p:=tunnelPlanFor("linux","arm64",dst);p.Ready || p.State!="STOP" {t.Fatalf("symlink was accepted: %+v",p)}
 if err:=os.Remove(dst);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(dst,[]byte("existing-no-overwrite"),0600);err!=nil{t.Fatal(err)}
 if p:=tunnelPlanFor("linux","arm64",dst);p.Ready || p.State!="EXISTING_UNVERIFIED"{t.Fatal("existing state overwritten")}
 b,err:=os.ReadFile(dst);if err!=nil||string(b)!="existing-no-overwrite"{t.Fatal("existing binary changed")}
}

func TestTunnelManifestDigestFailClosed(t *testing.T){
 name:="tunnel-client-v0.0.16-linux-arm64.zip"
 valid:=strings.Repeat("a",64)
 cases:=[]struct{body string;ok bool}{
  {valid+"  "+name+"\n",true},
  {valid+" *"+name+"\n",true},
  {"bad  "+name+"\n",false},
  {valid+"  other.zip\n",false},
  {valid+"  "+name+"\n"+valid+"  "+name+"\n",false},
 }
 for _,tc:=range cases{
  got,err:=tunnelManifestDigest([]byte(tc.body),name)
  if (err==nil)!=tc.ok{t.Fatalf("manifest accepted=%v (%v)",tc.ok,err)}
  if tc.ok&&got!=valid{t.Fatal("digest mismatch")}
 }
}

func writeTunnelZip(t *testing.T,filename,entryName string,payload []byte){
 t.Helper()
 f,err:=os.Create(filename);if err!=nil{t.Fatal(err)}
 z:=zip.NewWriter(f)
 entry,err:=z.Create(entryName);if err!=nil{t.Fatal(err)}
 if _,err:=entry.Write(payload);err!=nil{t.Fatal(err)}
 if err:=z.Close();err!=nil{t.Fatal(err)}
 if err:=f.Close();err!=nil{t.Fatal(err)}
}
func TestTunnelRejectsTraversalAndNonELF(t *testing.T){
 dir:=t.TempDir()
 cases:=[]string{"../tunnel-client","/tunnel-client","nested\\tunnel-client","tunnel-client"}
 for i,name:=range cases{
  zipFile:=filepath.Join(dir,"archive"+string(rune('a'+i))+".zip")
  stage:=filepath.Join(dir,"stage"+string(rune('a'+i)))
  writeTunnelZip(t,zipFile,name,[]byte("not-an-executable"))
  if err:=tunnelCandidateFromZip(zipFile,runtime.GOARCH,stage);err==nil{t.Fatalf("unsafe candidate accepted: %s",name)}
 }
}
func TestTunnelInstallHTTPNoChecksumNoMutation(t *testing.T){
 dir:=t.TempDir()
 dst:=filepath.Join(dir,"freenet-tunnel-client")
 name:=tunnelArchiveName("linux",runtime.GOARCH)
 if name==""{t.Skip("test host platform not supported")}
 zipFile:=filepath.Join(dir,"download.zip")
 writeTunnelZip(t,zipFile,"tunnel-client",[]byte("bad candidate"))
 blob,err:=os.ReadFile(zipFile);if err!=nil{t.Fatal(err)}
 sum:=sha256.Sum256(blob)
 manifest:=hex.EncodeToString(sum[:])+"  "+name+"\n"
 srv:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if strings.HasSuffix(r.URL.Path,"SHA256SUMS.txt"){
   w.Write([]byte(manifest));return
  }
  if strings.HasSuffix(r.URL.Path,name){w.Write(blob);return}
  http.NotFound(w,r)
 }))
 defer srv.Close()
 ctx,cancel:=context.WithTimeout(context.Background(),8*time.Second);defer cancel()
 if err:=installTunnelCandidate(ctx,srv.Client(),srv.URL,runtime.GOARCH,dst);err==nil{t.Fatal("accepted non-ELF binary")}
 if _,err:=os.Lstat(dst);!os.IsNotExist(err){t.Fatal("invalid candidate committed")}
 manifest=strings.Repeat("0",64)+"  "+name+"\n"
 if err:=installTunnelCandidate(ctx,srv.Client(),srv.URL,runtime.GOARCH,dst);err==nil || !strings.Contains(err.Error(),"SHA256 mismatch"){t.Fatalf("checksum failure not enforced: %v",err)}
 if _,err:=os.Lstat(dst);!os.IsNotExist(err){t.Fatal("bad checksum committed")}
 entries,err:=os.ReadDir(dir);if err!=nil{t.Fatal(err)}
 for _,e:=range entries{if strings.Contains(e.Name(),".freenet-tunnel-"){t.Fatalf("temporary installation file persisted: %s",e.Name())}}
}
func TestTunnelPlanAndApplyAuthenticationAndConfirmation(t *testing.T){
 a,mux:=connectorFixture(t)
 dst:=filepath.Join(t.TempDir(),"freenet-tunnel-client")
 t.Setenv("FREENET_TUNNEL_INSTALL_PATH",dst)
 // No bridge to OpenAI or filesystem mutation without admin login.
 for _,path:=range []string{"/api/admin/connector/install/plan","/api/admin/connector/install/apply"}{
  method:=http.MethodGet;body:=""
  if strings.HasSuffix(path,"apply"){method=http.MethodPost;body=`{"confirm":true}`}
  w:=httptest.NewRecorder()
  mux.ServeHTTP(w,connectorRequest(method,path,body))
  if w.Code==200{t.Fatalf("%s anonymously available",path)}
 }
 w:=httptest.NewRecorder()
 a.handleTunnelInstallPlan(w,connectorRequest("GET","/api/admin/connector/install/plan",""))
 if w.Code!=200 || bytes.Contains(w.Body.Bytes(),[]byte("sk-")){t.Fatalf("unsafe plan %d %s",w.Code,w.Body.String())}
 w=httptest.NewRecorder()
 a.handleTunnelInstallPlan(w,connectorRequest("GET","/api/admin/connector/install/plan?binary_url=https://attacker.invalid",""))
 if w.Code!=400{t.Fatal("allowed user-supplied download URL")}
 w=httptest.NewRecorder()
 a.handleTunnelInstall(w,connectorRequest("POST","/api/admin/connector/install/apply",`{"confirm":false}`))
 if w.Code!=400{t.Fatal("unconfirmed install reached network")}
 if _,err:=os.Lstat(dst);!os.IsNotExist(err){t.Fatal("unauthorized install mutated state")}
}
