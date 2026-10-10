package main

import (
 "context"
 "crypto/sha256"
 "crypto/subtle"
 "encoding/hex"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "os"
 "os/exec"
 "path/filepath"
 "regexp"
 "runtime"
 "strings"
 "syscall"
 "time"
)

const connectorHealthURL = "http://127.0.0.1:12031/readyz"
var connectorTunnelIDPattern=regexp.MustCompile(`^tunnel_[a-f0-9]{32}$`)
var connectorRuntimeKeyPattern=regexp.MustCompile(`^sk-[A-Za-z0-9_-]{20,512}$`)

type connectorTunnelProfile struct {
 Version int `json:"version"`
 TunnelID string `json:"tunnel_id"`
 Enabled bool `json:"enabled"`
}

func (a *app) connectorTunnelDir() string {return filepath.Join(filepath.Dir(a.cfg.ConfigPath),"connector_tunnel")}
func (a *app) connectorTunnelProfilePath() string {return filepath.Join(a.connectorTunnelDir(),"profile.json")}
func (a *app) connectorTransportKeyPath() string {return filepath.Join(a.connectorTunnelDir(),"runtime_key")}
func (a *app) connectorTransportHeaderPath() string {return filepath.Join(a.connectorTunnelDir(),"mcp_header")}
func (a *app) connectorTunnelConfigPath() string {return filepath.Join(a.connectorTunnelDir(),"client.yaml")}
func (a *app) connectorTunnelLockPath() string {return filepath.Join(a.connectorTunnelDir(),"process.lock")}
func (a *app) connectorTunnelAttestationPath() string {return filepath.Join(a.connectorTunnelDir(),"installed.sha256")}

func connectorReadPrivateFile(path string, max int64)([]byte,error){
 st,err:=os.Lstat(path)
 if err!=nil{return nil,err}
 if !st.Mode().IsRegular() || st.Mode().Perm()!=0600 || st.Size()<1 || st.Size()>max {return nil,errors.New("unsafe connector file")}
 f,err:=os.Open(path);if err!=nil{return nil,err};defer f.Close()
 data,err:=io.ReadAll(io.LimitReader(f,max+1));if err!=nil||int64(len(data))>max{return nil,errors.New("unreadable connector file")}
 st2,err:=f.Stat();if err!=nil||!st2.Mode().IsRegular()||st2.Mode().Perm()!=0600{return nil,errors.New("unsafe connector descriptor")}
 return data,nil
}

func (a *app) connectorPrivateDir() error {
 d:=a.connectorTunnelDir()
 st,err:=os.Lstat(d)
 if os.IsNotExist(err){if err=os.Mkdir(d,0700);err!=nil{return err};st,err=os.Lstat(d)}
 if err!=nil||!st.IsDir()||st.Mode().Perm()!=0700{return errors.New("connector directory not private")}
 return nil
}

func (a *app) readTunnelProfile()(connectorTunnelProfile,error){
 var p connectorTunnelProfile
 data,err:=connectorReadPrivateFile(a.connectorTunnelProfilePath(),1024)
 if err!=nil{return p,err}
 if json.Unmarshal(data,&p)!=nil || p.Version!=1 || !connectorTunnelIDPattern.MatchString(p.TunnelID){return connectorTunnelProfile{},errors.New("invalid connector profile")}
 return p,nil
}

func (a *app) connectorTransportConfigured() bool {
 p,err:=a.readTunnelProfile();if err!=nil{return false}
 key,err:=connectorReadPrivateFile(a.connectorTransportKeyPath(),1024)
 if err!=nil||!connectorRuntimeKeyPattern.Match(key){return false}
 token,err:=connectorReadPrivateFile(a.connectorTransportHeaderPath(),96)
 if err!=nil||len(token)!=len("Bearer ")+64||!strings.HasPrefix(string(token),"Bearer "){return false}
 if _,err=hex.DecodeString(strings.TrimPrefix(string(token),"Bearer "));err!=nil{return false}
 yaml,err:=connectorReadPrivateFile(a.connectorTunnelConfigPath(),4096)
 if err!=nil{return false}
 expected,err:=a.connectorTunnelYAML(p.TunnelID)
 return err==nil && string(yaml)==expected
}

// The profile contains file references, never the API key or MCP bearer.
func (a *app) connectorTunnelYAML(tunnelID string) (string,error) {
 readiness:=connectorReadinessFor(runtime.GOOS,runtime.GOARCH,a.cfg.Listen)
 if !readiness.MCPReadyOnRouter || !connectorTunnelIDPattern.MatchString(tunnelID){return "",errors.New("loopback MCP not confirmed")}
 return "config_version: 1\n"+
   "control_plane:\n  base_url: https://api.openai.com\n  tunnel_id: "+tunnelID+"\n  api_key: file:"+a.connectorTransportKeyPath()+"\n  max_inflight_requests: 4\n"+
   "mcp:\n  server_urls:\n    - channel: main\n      url: "+readiness.LoopbackMCP+"\n  extra_headers:\n    Authorization: file:"+a.connectorTransportHeaderPath()+"\n  discovery_extra_headers:\n    Authorization: file:"+a.connectorTransportHeaderPath()+"\n  max_concurrent_requests: 2\n"+
   "health:\n  listen_addr: 127.0.0.1:12031\n"+
   "log:\n  level: warn\n  format: json\nadmin_ui:\n  open_browser: false\n",nil
}

func (a *app) handleTunnelConfigure(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","no-store")
 if !connectorAdminSecureOrigin(r) || !connectorAdminSameOrigin(r){writeJSON(w,403,map[string]any{"success":false,"error":"secure same-origin browser required"});return}
 var request struct {Confirm bool `json:"confirm"`; TunnelID string `json:"tunnel_id"`; RuntimeKey string `json:"runtime_api_key"`}
 dec:=json.NewDecoder(http.MaxBytesReader(w,r.Body,4096));dec.DisallowUnknownFields()
 if err:=dec.Decode(&request);err!=nil||!request.Confirm||!connectorTunnelIDPattern.MatchString(request.TunnelID)||!connectorRuntimeKeyPattern.MatchString(request.RuntimeKey){
  writeJSON(w,400,map[string]any{"success":false,"error":"invalid runtime credentials or explicit confirmation missing"});return
 }
 var extra any;if dec.Decode(&extra)!=io.EOF {writeJSON(w,400,map[string]any{"success":false,"error":"unexpected data"});return}
 a.connectorTunnelMu.Lock();defer a.connectorTunnelMu.Unlock()
 if a.connectorTunnelCmd!=nil {writeJSON(w,409,map[string]any{"success":false,"error":"STOP: connector already running"});return}
 if err:=a.connectorPrivateDir();err!=nil{writeJSON(w,503,map[string]any{"success":false,"error":"private state unavailable"});return}
 for _,p:=range []string{a.connectorTunnelProfilePath(),a.connectorTransportKeyPath(),a.connectorTransportHeaderPath(),a.connectorTunnelConfigPath()}{
  if _,err:=os.Lstat(p);!os.IsNotExist(err){writeJSON(w,409,map[string]any{"success":false,"error":"STOP: existing/unknown connector credential state; never overwrite"});return}
 }
 token,err:=randomSessionToken();if err!=nil{writeJSON(w,503,map[string]any{"success":false,"error":"transport credential unavailable"});return}
 yaml,err:=a.connectorTunnelYAML(request.TunnelID);if err!=nil{writeJSON(w,409,map[string]any{"success":false,"error":"local MCP unavailable"});return}
 profileBytes,_:=json.Marshal(connectorTunnelProfile{Version:1,TunnelID:request.TunnelID,Enabled:false})
 records:=[]struct{path string;data []byte}{
  {a.connectorTransportKeyPath(),[]byte(request.RuntimeKey)},
  {a.connectorTransportHeaderPath(),[]byte("Bearer "+token)},
  {a.connectorTunnelConfigPath(),[]byte(yaml)},
  {a.connectorTunnelProfilePath(),profileBytes},
 }
 written:=[]string{}
 for _,item:=range records {
  if err:=atomicWrite(item.path,item.data,0600);err!=nil {
   for _,p:=range written{if removeErr:=os.Remove(p);removeErr!=nil{writeJSON(w,503,map[string]any{"success":false,"error":"ROLLBACK UNKNOWN: credential cleanup failed"});return}}
   writeJSON(w,503,map[string]any{"success":false,"error":"credential write failed; rollback complete"});return
  }
  written=append(written,item.path)
 }
 v3AppendEvent("connector","success","OpenAI Tunnel настроен в защищённом хранилище. Клиент ещё не запущен.")
 writeJSON(w,200,map[string]any{"success":true,"state":"CONFIGURED_NOT_STARTED","mutation":"CREDENTIALS_ONLY","connected":false})
}

func (a *app) connectorTunnelVerifiedBinary() error {
 dest:=tunnelInstallPath()
 st,err:=os.Lstat(dest)
 if err!=nil||!st.Mode().IsRegular()||st.Mode().Perm()!=0700||st.Size()<=0||st.Size()>tunnelMaxBinary{return errors.New("official binary not installed or unsafe")}
 if err:=tunnelSafeELF(dest,runtime.GOARCH);err!=nil{return errors.New("binary ELF/ABI not confirmed")}
 digest,err:=connectorReadPrivateFile(a.connectorTunnelAttestationPath(),64)
 if err!=nil||len(digest)!=64{return errors.New("installer provenance missing")}
 f,err:=os.Open(dest);if err!=nil{return errors.New("candidate unreadable")};defer f.Close()
 h:=sha256.New();n,err:=io.Copy(h,io.LimitReader(f,tunnelMaxBinary+1))
 if err!=nil||n!=st.Size(){return errors.New("candidate integrity not established")}
 got:=hex.EncodeToString(h.Sum(nil))
 if subtle.ConstantTimeCompare([]byte(got),digest)!=1{return errors.New("candidate SHA mismatch")}
 ctx,cancel:=context.WithTimeout(context.Background(),7*time.Second);defer cancel()
 out,err:=exec.CommandContext(ctx,dest,"--version").Output()
 if err!=nil{return errors.New("candidate executable/ABI unavailable")}
 if !tunnelVersionVerified(out){return errors.New("candidate pinned version mismatch")}
 return nil
}

func (a *app) connectorTunnelStartLocked() error {
 if a.connectorTunnelCmd!=nil{return errors.New("client already running")}
 if runtime.GOOS!="linux" || (runtime.GOARCH!="arm64"&&runtime.GOARCH!="amd64"){return errors.New("unsupported platform")}
 p,err:=a.readTunnelProfile();if err!=nil{return errors.New("tunnel profile unavailable")}
 if !a.connectorTransportConfigured(){return errors.New("runtime credentials unavailable")}
 meminfo,err:=os.ReadFile("/proc/meminfo")
 if err!=nil||len(meminfo)>connectorHostMeminfoLimit{return errors.New("router memory status unknown; STOP")}
 available,ok:=connectorAvailableMemory(meminfo)
 if !ok||available<128<<20{return errors.New("less than 128 MiB available RAM or unknown; STOP")}
 if err:=a.connectorTunnelVerifiedBinary();err!=nil{return err}
 client:=&http.Client{Timeout:400*time.Millisecond}
 if rsp,err:=client.Get(connectorHealthURL);err==nil{rsp.Body.Close();return errors.New("health port already occupied")}
 lock,err:=os.OpenFile(a.connectorTunnelLockPath(),os.O_CREATE|os.O_RDWR,0600)
 if err!=nil{return errors.New("process lock unavailable")}
 if err=syscall.Flock(int(lock.Fd()),syscall.LOCK_EX|syscall.LOCK_NB);err!=nil{lock.Close();return errors.New("another client supervisor exists")}
 cmd:=exec.Command(tunnelInstallPath(),"run","--config",a.connectorTunnelConfigPath())
 cmd.Stdin=nil;cmd.Stdout=io.Discard;cmd.Stderr=io.Discard
 cmd.SysProcAttr=&syscall.SysProcAttr{Pdeathsig:syscall.SIGTERM}
 if err:=cmd.Start();err!=nil{lock.Close();return errors.New("verified client failed to start")}
 p.Enabled=true
 data,_:=json.Marshal(p)
 if err:=atomicWrite(a.connectorTunnelProfilePath(),data,0600);err!=nil{cmd.Process.Kill();cmd.Wait();lock.Close();return errors.New("cannot persist supervised state")}
 a.connectorTunnelCmd=cmd
 a.connectorTunnelLock=lock
 a.connectorTunnelState="STARTING"
 go a.connectorTunnelWait(cmd,lock)
 return nil
}

func (a *app) connectorTunnelWait(cmd *exec.Cmd, lock *os.File){
 _=cmd.Wait()
 retry:=false
 a.connectorTunnelMu.Lock()
 if a.connectorTunnelCmd==cmd {
  a.connectorTunnelCmd=nil
  a.connectorTunnelState="STOPPED_OR_FAILED"
  a.connectorRemoteRevoke()
  if profile,err:=a.readTunnelProfile();err==nil&&profile.Enabled {
   if a.connectorTunnelRestartCount<3 {
    a.connectorTunnelRestartCount++
    retry=true
    a.connectorTunnelState="RECONNECT_WAIT"
   }else{
    a.connectorTunnelState="RECOVERY_STOP"
   }
  }
 }
 if a.connectorTunnelLock==lock {a.connectorTunnelLock=nil}
 a.connectorTunnelMu.Unlock()
 _=lock.Close()
 if retry {
  go func(){
   time.Sleep(6*time.Second)
   a.connectorTunnelMu.Lock()
   defer a.connectorTunnelMu.Unlock()
   if a.connectorTunnelCmd!=nil||a.connectorTunnelState!="RECONNECT_WAIT"{return}
   profile,err:=a.readTunnelProfile()
   if err!=nil||!profile.Enabled{a.connectorTunnelState="RECOVERY_STOP";return}
   if err:=a.connectorTunnelStartLocked();err!=nil {
    a.connectorTunnelState="RECOVERY_STOP"
    v3AppendEvent("connector","warning","Автовосстановление Tunnel STOP: требуется проверка в браузере.")
   }
  }()
 }
}

func (a *app) handleTunnelStart(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","no-store")
 if !connectorConfirm(w,r){return}
 if !connectorAdminSecureOrigin(r){writeJSON(w,403,map[string]any{"success":false,"error":"secure same-origin administration required"});return}
 a.connectorTunnelMu.Lock();a.connectorTunnelRestartCount=0;err:=a.connectorTunnelStartLocked();a.connectorTunnelMu.Unlock()
 if err!=nil{writeJSON(w,409,map[string]any{"success":false,"state":"STOP","error":err.Error()});return}
 v3AppendEvent("connector","success","Проверенный клиент OpenAI Tunnel запущен. Readiness и ChatGPT MCP ещё не подтверждены.")
 writeJSON(w,200,map[string]any{"success":true,"state":"STARTING","mutation":"TUNNEL_PROCESS_ONLY","connected":false})
}

func (a *app) connectorTunnelStopLocked() error {
 // A STOP attempt immediately removes read consent even if local state is
 // corrupt or a process termination operation later fails.
 a.connectorRemoteRevoke()
 p,err:=a.readTunnelProfile()
 if err!=nil{return errors.New("unknown tunnel profile; STOP")}
 p.Enabled=false
 data,_:=json.Marshal(p)
 if err:=atomicWrite(a.connectorTunnelProfilePath(),data,0600);err!=nil{return errors.New("cannot persist disabled state")}
 // The diagnostic approval was revoked at entry, before disk IO.
 if a.connectorTunnelCmd!=nil {
  cmd:=a.connectorTunnelCmd
  if err:=cmd.Process.Signal(syscall.SIGTERM);err!=nil{a.connectorTunnelState="STOP_UNKNOWN";return errors.New("tunnel signal failed; STOP UNKNOWN")}
  // A bounded emergency escalation is only applied to this owned process,
  // never to a system-wide PID lookup or another router service.
  go func(){
   time.Sleep(6*time.Second)
   a.connectorTunnelMu.Lock()
   defer a.connectorTunnelMu.Unlock()
   if a.connectorTunnelCmd==cmd{_ =cmd.Process.Kill()}
  }()
 }
 a.connectorTunnelState="STOP_REQUESTED"
 return nil
}

func (a *app) handleTunnelStop(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","no-store")
 if !connectorConfirm(w,r){return}
 if !connectorAdminSecureOrigin(r){writeJSON(w,403,map[string]any{"success":false,"error":"secure same-origin administration required"});return}
 a.connectorTunnelMu.Lock();err:=a.connectorTunnelStopLocked();a.connectorTunnelMu.Unlock()
 if err!=nil{writeJSON(w,503,map[string]any{"success":false,"state":"STOP_UNKNOWN","error":err.Error()});return}
 v3AppendEvent("connector","success","Остановка OpenAI Tunnel запрошена; временные разрешения отозваны.")
 writeJSON(w,200,map[string]any{"success":true,"state":"STOP_REQUESTED","mutation":"TUNNEL_PROCESS_ONLY"})
}

func (a *app) handleTunnelConnectionStatus(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","no-store")
 if len(r.URL.Query())!=0{writeJSON(w,400,map[string]any{"success":false,"error":"parameters forbidden"});return}
 a.connectorTunnelMu.Lock()
 cmd:=a.connectorTunnelCmd
 state:=a.connectorTunnelState
 a.connectorTunnelMu.Unlock()
 running:=cmd!=nil
 ready:=false
 if running {
  client:=&http.Client{Timeout:1500*time.Millisecond}
  rsp,err:=client.Get(connectorHealthURL)
  if err==nil {ready=rsp.StatusCode==200;rsp.Body.Close()}
 }
 if !running && state==""{state="NOT_STARTED"}
 if running && !ready {state="RUNNING_NOT_READY"}
 a.connectorRemoteMu.Lock()
 lastRequest:=a.connectorRemote.LastRequest
 a.connectorRemoteMu.Unlock()
 writeJSON(w,200,map[string]any{
  "success":true,"configured":a.connectorTransportConfigured(),"client_running":running,"client_ready":ready,
  "state":state,"chatgpt_plugin_connected":false,"external_mcp_verified":false,
  "mcp_access_request_observed":!lastRequest.IsZero(),"mcp_access_request_at":lastRequest,
  "mutation":"NONE",
 })
}

func (a *app) connectorTunnelResumeOnStartup(){
 profile,err:=a.readTunnelProfile()
 if err!=nil||!profile.Enabled{return}
 // The previous child receives Pdeathsig on FreeNet shutdown. Allow it
 // to release its own health listener and process lock before re-spawning.
 time.Sleep(3*time.Second)
 for attempt:=0;attempt<3;attempt++{
  a.connectorTunnelMu.Lock()
  err=a.connectorTunnelStartLocked()
  if err==nil{a.connectorTunnelMu.Unlock();return}
  a.connectorTunnelState="RECOVERY_STOP"
  a.connectorTunnelMu.Unlock()
  // Retry only a known handover race, never broken credentials or ABI.
  if err.Error()!="health port already occupied" && err.Error()!="another client supervisor exists" {break}
  time.Sleep(time.Duration(attempt+1)*3*time.Second)
 }
 v3AppendEvent("connector","warning","Автозапуск Tunnel STOP: требуется проверка через Control Center.")
}

// Attestation exists only if the verified official installer created this binary.
func (a *app) connectorTunnelAttestInstalled(destination string) error {
 if err:=a.connectorPrivateDir();err!=nil{return err}
 f,err:=os.Open(destination);if err!=nil{return err};defer f.Close()
 h:=sha256.New();n,err:=io.Copy(h,io.LimitReader(f,tunnelMaxBinary+1))
 if err!=nil||n<=0||n>tunnelMaxBinary{return errors.New("candidate attestation unavailable")}
 return atomicWrite(a.connectorTunnelAttestationPath(),[]byte(hex.EncodeToString(h.Sum(nil))),0600)
}

func (a *app) handleTunnelForget(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","no-store")
 if !connectorConfirm(w,r){return}
 if !connectorAdminSecureOrigin(r){
  writeJSON(w,http.StatusForbidden,map[string]any{"success":false,"error":"secure origin required"});return
 }
 a.connectorRemoteRevoke()
 a.connectorTunnelMu.Lock()
 defer a.connectorTunnelMu.Unlock()
 if a.connectorTunnelCmd!=nil{
  writeJSON(w,http.StatusConflict,map[string]any{"success":false,"error":"STOP: first stop the supervised client"});return
 }
 if resp,err:=(&http.Client{Timeout:300*time.Millisecond}).Get(connectorHealthURL);err==nil{
  resp.Body.Close()
  writeJSON(w,http.StatusConflict,map[string]any{"success":false,"error":"STOP: health port occupied; process ownership unknown"});return
 }
 // A partial private state is recoverable only after each existing file is
 // proven regular/private. Never follow or delete an unexpected symlink.
 paths:=[]string{a.connectorTransportKeyPath(),a.connectorTransportHeaderPath(),a.connectorTunnelConfigPath(),a.connectorTunnelProfilePath()}
 for _,path:=range paths{
  st,err:=os.Lstat(path)
  if os.IsNotExist(err){continue}
  if err!=nil||!st.Mode().IsRegular()||st.Mode().Perm()!=0600{
   writeJSON(w,http.StatusServiceUnavailable,map[string]any{"success":false,"error":"STOP: unsafe or unknown credential state"});return
  }
 }
 for _,path:=range paths{
  if err:=os.Remove(path);err!=nil&&!os.IsNotExist(err){
   writeJSON(w,http.StatusServiceUnavailable,map[string]any{"success":false,"error":"ROLLBACK UNKNOWN: partial credential removal"});return
  }
 }
 a.connectorRemoteRevoke()
 a.connectorTunnelState="CREDENTIALS_REMOVED"
 v3AppendEvent("connector","success","Runtime credentials FreeNet Connector удалены. Проверь отзыв OpenAI key в Platform отдельно.")
 writeJSON(w,http.StatusOK,map[string]any{"success":true,"state":"CREDENTIALS_REMOVED","mutation":"CREDENTIALS_DELETED","connected":false})
}
