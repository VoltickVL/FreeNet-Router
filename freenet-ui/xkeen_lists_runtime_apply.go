package main

// Native XKeen 2.1 kernel-list activation. The upstream XKeen implementation
// loads these two hash:net sets with a staged ipset swap; this code intentionally
// never restarts Xray, XKeen or the firmware firewall.
import (
 "bytes"
 "context"
 "crypto/rand"
 "encoding/hex"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "net/netip"
 "os"
 "os/exec"
 "path/filepath"
 "sort"
 "strconv"
 "strings"
 "time"
)

type xkeenIPSetPlanResponse struct {
 Success bool `json:"success"`
 Ready bool `json:"ready"`
 State string `json:"state"`
 Reason string `json:"reason,omitempty"`
 Mutation string `json:"mutation"`
 DiskSHA256 string `json:"disk_sha256,omitempty"`
 RuntimeSHA256 string `json:"runtime_sha256,omitempty"`
 Before int `json:"before"`
 After int `json:"after"`
 Add int `json:"add"`
 Remove int `json:"remove"`
 IPv6 string `json:"ipv6"`
 ClientEgress string `json:"client_egress"`
}
type xkeenIPSetApplyRequest struct {
 ExpectedDiskSHA256 string `json:"expected_disk_sha256"`
 ExpectedRuntimeSHA256 string `json:"expected_runtime_sha256"`
 Confirm string `json:"confirm"`
}
type xkeenIPSetApplyResponse struct {
 Success bool `json:"success"`
 State string `json:"state"`
 Mutation string `json:"mutation"`
 Rollback string `json:"rollback"`
 Reason string `json:"reason,omitempty"`
 DiskSHA256 string `json:"disk_sha256,omitempty"`
 ClientEgress string `json:"client_egress"`
}
type xkeenIPSetDiskState struct {
 State string `json:"state"`
 Rollback string `json:"rollback"`
 Snapshot string `json:"snapshot,omitempty"`
 DiskSHA256 string `json:"disk_sha256,omitempty"`
 RuntimeSHA256 string `json:"runtime_sha256,omitempty"`
}

type xkeenIPSet struct {
 name string
 family string
 raw string
 opts []string
 active []string
 desired []string
 changed bool
 present bool
 tmp string
}
var xkeenIPSetRun = func(ctx context.Context, stdin string, args ...string) (string,error) {
 path,err:=exec.LookPath("ipset")
 if err!=nil { path="/opt/sbin/ipset" }
 cmd:=exec.CommandContext(ctx,path,args...)
 if stdin!="" {cmd.Stdin=strings.NewReader(stdin)}
 output,e:=cmd.CombinedOutput()
 if len(output)>(1<<20){return "",errors.New("oversized ipset response")}
 if e!=nil {return "",errors.New("ipset command failed")}
 return string(output),nil
}
var xkeenIPSetHookRoot = func() string {return "/tmp/.xkeen"}
var xkeenIPSetAdmission = func(a *app) bool {
 if a==nil || a.cfg.XKeenPath=="" {return false}
 v,ch,e:=readXKeenInstalledMetadata(a.cfg.XKeenPath)
 if e!=nil || v!="2.1" || ch!="stable" {return false}
 reason,need:=xkeenInitRepairNeed(xkeenInitDir())
 if need || reason!="compatible" {return false}
 if !xkeenUpdateProductionReady(a) {return false}
 info,e:=os.Lstat(filepath.Join(xkeenIPSetHookRoot(),"ready"))
 return e==nil && info.Mode().IsRegular()
}

func xkeenIPSetStatePath() string {return filepath.Join(configStudioXKeenDir(),".freenet-ip-exclude-apply.json")}
func xkeenIPSetReadState()(xkeenIPSetDiskState,error){
 data,err:=os.ReadFile(xkeenIPSetStatePath())
 if errors.Is(err,os.ErrNotExist){return xkeenIPSetDiskState{State:"IDLE",Rollback:"NOT_NEEDED"},nil}
 if err!=nil || len(data)>4096 {return xkeenIPSetDiskState{},errors.New("activation journal unreadable")}
 var state xkeenIPSetDiskState
 if json.Unmarshal(data,&state)!=nil || state.State=="" {return state,errors.New("activation journal invalid")}
 return state,nil
}
func xkeenIPSetWriteState(state xkeenIPSetDiskState) error {
 root:=configStudioXKeenDir()
 raw,err:=json.Marshal(state);if err!=nil{return err}
 f,err:=os.CreateTemp(root,".freenet-ipset-state-*")
 if err!=nil{return err}
 tmp:=f.Name();defer os.Remove(tmp)
 if err=f.Chmod(0600);err!=nil{_ =f.Close();return err}
 if _,err=f.Write(raw);err!=nil{_ =f.Close();return err}
 if err=f.Sync();err!=nil{_ =f.Close();return err}
 if err=f.Close();err!=nil{return err}
 if err=os.Rename(tmp,xkeenIPSetStatePath());err!=nil{return err}
 return visionQUICSyncDir(root)
}
func xkeenIPSetSnapshot(sets []xkeenIPSet)(string,error){
 root:=filepath.Join(configStudioXKeenDir(),".freenet-backups")
 if err:=os.MkdirAll(root,0700);err!=nil{return "",err}
 info,err:=os.Lstat(root)
 if err!=nil||!info.IsDir()||info.Mode()&os.ModeSymlink!=0 {return "",errors.New("unsafe snapshot root")}
 dir,err:=os.MkdirTemp(root,"xkeen-ipset-*")
 if err!=nil{return "",err}
 for _,s:=range sets{
  if !s.present {continue}
  f,e:=os.OpenFile(filepath.Join(dir,s.name+".save"),os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600)
  if e!=nil{return "",e}
  _,e=f.WriteString(s.raw);if e==nil{e=f.Sync()}
  ce:=f.Close();if e==nil{e=ce}
  if e!=nil{return "",e}
 }
 if visionQUICSyncDir(dir)!=nil||visionQUICSyncDir(root)!=nil{return "",errors.New("snapshot not durable")}
 return dir,nil
}
func xkeenIPSetCanonical(value string, ipv6 bool)(string,error){
 if a,e:=netip.ParseAddr(value);e==nil{
  if a.Is6()!=ipv6{return "",errors.New("family mismatch")}
  bits:=32;if ipv6{bits=128}
  return netip.PrefixFrom(a,bits).Masked().String(),nil
 }
 p,e:=netip.ParsePrefix(value)
 if e!=nil||!p.IsValid()||p.Addr().Is6()!=ipv6 {return "",errors.New("invalid kernel prefix")}
 return p.Masked().String(),nil
}
func xkeenIPSetUnique(entries []string) []string{
 m:=make(map[string]bool,len(entries));r:=make([]string,0,len(entries))
 for _,e:=range entries {if !m[e]{m[e]=true;r=append(r,e)}}
 sort.Strings(r);return r
}
func xkeenIPSetParse(raw,name,family string)([]string,[]string,error){
 if len(raw)>(1<<20){return nil,nil,errors.New("oversized runtime set")}
 found:=false;var entries,opts []string
 for _,line:=range strings.Split(strings.TrimSpace(raw),"\n"){
  if strings.TrimSpace(line)==""{continue}
  f:=strings.Fields(line)
  if len(f)==0{continue}
  switch f[0]{
  case "create":
   if found || len(f)<5 || f[1]!=name || f[2]!="hash:net" || f[3]!="family" || f[4]!=family || (len(f)-5)%2!=0{return nil,nil,errors.New("unexpected ipset kind")}
   seen:=map[string]bool{}
   for i:=5;i<len(f);i+=2{
    key,value:=f[i],f[i+1]
    if seen[key]{return nil,nil,errors.New("duplicate ipset option")}
    seen[key]=true
    switch key {
    // The official XKeen 2.1 uses plain hash:net sets. ipset versions
    // may serialize implementation-only bucketsize/initval; these remain
    // part of the exact staged set definition, never silently dropped.
    case "hashsize","maxelem","bucketsize":
     n,e:=strconv.Atoi(value)
     limit:=1<<20
     if key=="bucketsize"{limit=64}
     if e!=nil||n<1||n>limit{return nil,nil,errors.New("invalid ipset capacity option")}
    case "initval":
     if len(value)<3||len(value)>10||!strings.HasPrefix(value,"0x"){
      return nil,nil,errors.New("invalid ipset initial hash")}
     for _,c:=range value[2:]{if !strings.ContainsRune("0123456789abcdefABCDEF",c){
      return nil,nil,errors.New("invalid ipset initial hash")}}
    default:
     // timeout/nomatch/counters/comment/netmask/bitmask/forceadd may alter
     // membership semantics. Never replace a set whose effects are unknown.
     return nil,nil,errors.New("unsupported ipset options")
    }
   }
   found=true;opts=append([]string(nil),f[5:]...)
  case "add":
   if !found||len(f)!=3||f[1]!=name{return nil,nil,errors.New("unknown ipset entry")}
   p,e:=xkeenIPSetCanonical(f[2],family=="inet6");if e!=nil{return nil,nil,e}
   entries=append(entries,p)
  default:return nil,nil,errors.New("unsupported ipset save format")
  }
 }
 if !found{return nil,nil,errors.New("missing ipset definition")}
 return xkeenIPSetUnique(entries),opts,nil
}
func xkeenIPSetDesired(text string)([]string,[]string,error){
 // Native XKeen and the partner XKeen UIs allow both host IP addresses and
 // CIDRs in existing ip_exclude.lst. FreeNet's guarded editor may require an
 // explicit mask when saving; activation must still understand valid files
 // already saved by the working XKeen UI without rewriting user content.
 if len(text)>maxXKeenListEditBytes||strings.ContainsRune(text,0){
  return nil,nil,errors.New("invalid exclusion list")
 }
 var v4,v6 []string
 for _,line:=range strings.Split(strings.ReplaceAll(text,"\r\n","\n"),"\n"){
  value:=strings.TrimSpace(strings.SplitN(line,"#",2)[0])
  if value==""{continue}
  var p netip.Prefix
  if ip,e:=netip.ParseAddr(value);e==nil{
   bits:=128;if ip.Is4(){bits=32}
   p=netip.PrefixFrom(ip,bits)
  }else{
   parsed,e:=netip.ParsePrefix(value)
   if e!=nil{return nil,nil,errors.New("invalid existing IP/CIDR exclusion")}
   p=parsed
  }
  if !p.IsValid()||xkeenDangerouslyBroadLAN(p){
   return nil,nil,errors.New("unsafe broad LAN exclusion")
  }
  p=p.Masked()
  if p.Addr().Is4(){v4=append(v4,p.String())}else{v6=append(v6,p.String())}
 }
 v4=xkeenIPSetUnique(v4);v6=xkeenIPSetUnique(v6)
 if len(v4)+len(v6)>1024{return nil,nil,errors.New("list exceeds activation limit")}
 return v4,v6,nil
}
func xkeenIPSetSame(a,b []string)bool{return strings.Join(a,"\n")==strings.Join(b,"\n")}
func xkeenIPSetFingerprint(sets []xkeenIPSet) string {
 var b strings.Builder
 for _,s:=range sets{b.WriteString(s.name);b.WriteByte('\n')
  if !s.present{b.WriteString("ABSENT\n")}else{b.WriteString(strings.Join(s.active,"\n"));b.WriteByte('\n')}
 }
 return sha256Hex([]byte(b.String()))
}
func xkeenIPSetRead(ctx context.Context,name,family string,desired []string,allowMissing bool)(xkeenIPSet,error){
 s:=xkeenIPSet{name:name,family:family,desired:desired}
 raw,e:=xkeenIPSetRun(ctx,"","save",name)
 if e!=nil {
  if allowMissing && len(desired)==0 {
   // A failed save is safe to treat as absent only when an independent
   // read-only inventory proves the named set does not exist.
   names,listErr:=xkeenIPSetRun(ctx,"","list","-name")
   if listErr==nil {
    found:=false
    for _,line:=range strings.Split(names,"\n"){if strings.TrimSpace(line)==name{found=true}}
    if !found{return s,nil}
   }
  }
  return s,errors.New("required kernel set unavailable")
 }
 active,opts,e:=xkeenIPSetParse(raw,name,family)
 if e!=nil{return s,e}
 s.present=true;s.raw=raw;s.active=active;s.opts=opts;s.changed=!xkeenIPSetSame(active,desired)
 return s,nil
}
func xkeenIPSetBusy(a *app)bool{
 if a==nil||a.cfg.LockPath==""||a.updateLockHeld()||a.selfUpdateStateAdmission()!=nil||automationMutationBlockedState()||
    vpnTransactionPending()||xkeenUpdateLocalActive()||xkeenInitRepairMutationBlocked(){return true}
 state:=xrayCoreOperationSnapshot().State
 upstream:=publicXKeenUpdateState(a.cfg.XKeenPath).State
 return state=="RUNNING"||state=="STOP"||upstream=="RUNNING"||upstream=="STOP"
}
func xkeenIPSetPlan(ctx context.Context,a *app)(xkeenIPSetPlanResponse,[]xkeenIPSet){
 p:=xkeenIPSetPlanResponse{Success:true,State:"STOP",Reason:"runtime-unverified",Mutation:"NONE",IPv6:"unverified",ClientEgress:"NOT_VERIFIED"}
 if !xkeenIPSetAdmission(a){return p,nil}
 state,e:=xkeenIPSetReadState()
 if e!=nil || state.State=="RUNNING"||state.State=="STOP"||state.Rollback=="FAILED"||state.Rollback=="UNKNOWN"{
  p.Reason="previous-activation-unresolved";return p,nil
 }
 if xkeenIPSetBusy(a){p.Reason="operation-busy-or-unknown";return p,nil}
 raw,present,e:=readXKeenListSafely(configStudioXKeenDir(),"ip_exclude.lst")
 if e!=nil||!present{p.Reason="list-unavailable";return p,nil}
 desired4,desired6,e:=xkeenIPSetDesired(string(raw))
 if e!=nil{p.Reason="unsafe-or-invalid-list";return p,nil}
 v4,e:=xkeenIPSetRead(ctx,"user_exclude","inet",desired4,false)
 if e!=nil{p.Reason="ipv4-ipset-unverified";return p,nil}
 v6,e:=xkeenIPSetRead(ctx,"user_exclude6","inet6",desired6,true)
 if e!=nil{p.Reason="ipv6-ipset-unverified";return p,nil}
 sets:=[]xkeenIPSet{v4,v6}
 p.Ready=true;p.State="READY";p.Reason="ready-for-explicit-apply"
 p.DiskSHA256=sha256Hex(raw);p.RuntimeSHA256=xkeenIPSetFingerprint(sets)
 p.IPv6="active";if !v6.present{p.IPv6="not-installed"}
 for _,s:=range sets{
  p.Before+=len(s.active);p.After+=len(s.desired)
  old:=map[string]bool{};next:=map[string]bool{}
  for _,value:=range s.active{old[value]=true}
  for _,value:=range s.desired{next[value]=true;if !old[value]{p.Add++}}
  for _,value:=range s.active{if !next[value]{p.Remove++}}
 }
 if p.Add==0&&p.Remove==0{p.State="SYNCED";p.Reason="kernel-matches-saved-list"}
 return p,sets
}
func xkeenIPSetNewTemp()(string,error){
 var raw [6]byte
 if _,e:=rand.Read(raw[:]);e!=nil{return "",e}
 return "fnip_"+hex.EncodeToString(raw[:]),nil
}
func xkeenIPSetPrepare(ctx context.Context,s *xkeenIPSet)error{
 tmp,e:=xkeenIPSetNewTemp();if e!=nil{return e}
 s.tmp=tmp
 args:=append([]string{"create",tmp,"hash:net","family",s.family},s.opts...)
 if _,e=xkeenIPSetRun(ctx,"",args...);e!=nil{return e}
 var input strings.Builder
 for _,p:=range s.desired{input.WriteString("add ");input.WriteString(tmp);input.WriteByte(' ');input.WriteString(p);input.WriteByte('\n')}
 if input.Len()>0{if _,e=xkeenIPSetRun(ctx,input.String(),"restore");e!=nil{return e}}
 after,_,e:=xkeenIPSetReadTemp(ctx,tmp,s.family)
 if e!=nil||!xkeenIPSetSame(after,s.desired){return errors.New("staged candidate mismatch")}
 return nil
}
func xkeenIPSetReadTemp(ctx context.Context,name,family string)([]string,[]string,error){
 raw,e:=xkeenIPSetRun(ctx,"","save",name);if e!=nil{return nil,nil,e}
 return xkeenIPSetParse(raw,name,family)
}
func xkeenIPSetCleanup(ctx context.Context,sets []xkeenIPSet)bool{
 ok:=true
 for _,s:=range sets{if s.tmp!=""{if _,e:=xkeenIPSetRun(ctx,"","destroy",s.tmp);e!=nil{ok=false}}}
 return ok
}
func xkeenIPSetUnchanged(ctx context.Context,before []xkeenIPSet)bool{
 for _,s:=range before{
  current,e:=xkeenIPSetRead(ctx,s.name,s.family,s.desired,!s.present)
  if e!=nil||current.present!=s.present||!xkeenIPSetSame(current.active,s.active){return false}
 }
 return true
}
func xkeenIPSetRollback(ctx context.Context,swapped []xkeenIPSet,before []xkeenIPSet)bool{
 ok:=true
 for i:=len(swapped)-1;i>=0;i--{
  s:=swapped[i]
  if _,e:=xkeenIPSetRun(ctx,"","swap",s.tmp,s.name);e!=nil{ok=false}
 }
 return ok&&xkeenIPSetUnchanged(ctx,before)
}
// XKeen 2.1 has two different actors:
// - xkeen -start/-stop/-restart obtains proxy.mutex.d before firewall work;
// - the netfilter.d event hook obtains netfilter.lock.d.
// Acquiring both in the exact upstream order is needed. Holding only the NF
// lock is insufficient: emergency stop may forcibly recover that lock.
func xkeenIPSetHookLock()(func()bool,error){
 dir:=xkeenIPSetHookRoot()
 info,e:=os.Lstat(dir)
 if e!=nil||!info.IsDir()||info.Mode().Perm()!=0700||info.Mode()&os.ModeSymlink!=0{return nil,errors.New("XKeen hook root unverified")}
 ready,e:=os.Lstat(filepath.Join(dir,"ready"))
 if e!=nil||!ready.Mode().IsRegular()||ready.Mode()&os.ModeSymlink!=0{return nil,errors.New("XKeen runtime not ready")}
 for _,name:=range []string{"starting.lock.d","coldstart.lock.d"}{
  if _,err:=os.Lstat(filepath.Join(dir,name));!errors.Is(err,os.ErrNotExist){
   return nil,errors.New("XKeen service lifecycle busy or unknown")
  }
 }
 owner:=strconv.Itoa(os.Getpid())
 acquire:=func(name string)error{
  lock:=filepath.Join(dir,name)
  if err:=os.Mkdir(lock,0700);err!=nil{return errors.New("XKeen external operation holds lock")}
  pid:=filepath.Join(lock,"pid")
  if err:=os.WriteFile(pid,[]byte(owner+"\n"),0600);err!=nil{
   _=os.Remove(pid);_=os.Remove(lock);return errors.New("XKeen lock ownership unavailable")
  }
  return nil
 }
 release:=func(name string)bool{
  lock:=filepath.Join(dir,name)
  pid:=filepath.Join(lock,"pid")
  data,err:=os.ReadFile(pid)
  if err!=nil||strings.TrimSpace(string(data))!=owner{return false}
  if err=os.Remove(pid);err!=nil{return false}
  return os.Remove(lock)==nil
 }
 // Official xkeen uses the proxy mutex before calling clean_firewall, then
 // clean_firewall obtains the NF lock. Never acquire them in reverse order.
 if e=acquire("proxy.mutex.d");e!=nil{return nil,e}
 if e=acquire("netfilter.lock.d");e!=nil{
  if !release("proxy.mutex.d"){return nil,errors.New("XKeen proxy mutex release unverified")}
  return nil,e
 }
 return func()bool{
  // Never delete a foreign/replaced lock. An incomplete release means that
  // the next apply must be stopped until runtime ownership is reconciled.
  netfilterOK:=release("netfilter.lock.d")
  proxyOK:=release("proxy.mutex.d")
  return netfilterOK && proxyOK
 },nil
}
func (a *app) handleXKeenIPSetPlan(w http.ResponseWriter,r *http.Request){
 ctx,cancel:=context.WithTimeout(r.Context(),8*time.Second);defer cancel()
 p,_:=xkeenIPSetPlan(ctx,a);writeJSON(w,http.StatusOK,p)
}
func (a *app) handleXKeenIPSetApply(w http.ResponseWriter,r *http.Request){
 fail:=func(status int,state,mutation,rollback,reason string){
  writeJSON(w,status,xkeenIPSetApplyResponse{Success:false,State:state,Mutation:mutation,Rollback:rollback,Reason:reason,ClientEgress:"NOT_VERIFIED"})
 }
 if !sameOrigin(r){fail(http.StatusForbidden,"STOP","NONE","NOT_NEEDED","cross-origin-rejected");return}
 if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))),"application/json"){
  fail(http.StatusUnsupportedMediaType,"STOP","NONE","NOT_NEEDED","json-required");return
 }
 raw,e:=io.ReadAll(http.MaxBytesReader(w,r.Body,2048))
 if e!=nil||rejectDuplicateJSONKeys(raw)!=nil{fail(http.StatusBadRequest,"STOP","NONE","NOT_NEEDED","invalid-request");return}
 dec:=json.NewDecoder(bytes.NewReader(raw));dec.DisallowUnknownFields()
 var req xkeenIPSetApplyRequest
 if dec.Decode(&req)!=nil{fail(http.StatusBadRequest,"STOP","NONE","NOT_NEEDED","invalid-request");return}
 var extra any
 if dec.Decode(&extra)!=io.EOF||req.Confirm!="apply-ip-exclude-kernel"||
    !isHexLowerSHA(req.ExpectedDiskSHA256)||!isHexLowerSHA(req.ExpectedRuntimeSHA256){
  fail(http.StatusBadRequest,"STOP","NONE","NOT_NEEDED","missing-explicit-confirmation-or-revision");return
 }
 if a==nil||xkeenIPSetBusy(a){fail(http.StatusConflict,"STOP","NONE","NOT_NEEDED","operation-busy");return}
 select{case a.sem<-struct{}{}:defer func(){<-a.sem}();default:
  fail(http.StatusConflict,"STOP","NONE","NOT_NEEDED","operation-busy");return
 }
 if e=os.Mkdir(a.cfg.LockPath,0700);e!=nil{fail(http.StatusConflict,"STOP","NONE","NOT_NEEDED","shared-writer-busy");return}
 owner:=filepath.Join(a.cfg.LockPath,"xkeen-ip-exclude-owner")
 const mark="freenet-ip-exclude-kernel-v1\n"
 if e=os.WriteFile(owner,[]byte(mark),0600);e!=nil{
  _ =os.Remove(a.cfg.LockPath);fail(http.StatusServiceUnavailable,"STOP","NONE","NOT_NEEDED","writer-unavailable");return
 }
 retain:=false
 defer func(){if retain{return};if current,e:=os.ReadFile(owner);e==nil&&string(current)==mark{
  _ =os.Remove(owner);_ =os.Remove(a.cfg.LockPath)
 }}()
 halt:=func(reason string){retain=true;setAutomationMutationBlocked(true);fail(http.StatusServiceUnavailable,"STOP","UNKNOWN","FAILED/UNKNOWN",reason)}
 if xkeenIPSetBusy(a){fail(http.StatusConflict,"STOP","NONE","NOT_NEEDED","state-became-busy");return}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 plan,sets:=xkeenIPSetPlan(ctx,a)
 if !plan.Ready||plan.DiskSHA256!=req.ExpectedDiskSHA256||plan.RuntimeSHA256!=req.ExpectedRuntimeSHA256{
  fail(http.StatusConflict,"STOP","NONE","NOT_NEEDED","plan-stale-or-blocked");return
 }
 if plan.State=="SYNCED"{
  writeJSON(w,http.StatusOK,xkeenIPSetApplyResponse{Success:true,State:"SYNCED",Mutation:"NONE",Rollback:"NOT_NEEDED",DiskSHA256:plan.DiskSHA256,ClientEgress:"NOT_VERIFIED"});return
 }
 unlock,e:=xkeenIPSetHookLock()
 if e!=nil{fail(http.StatusConflict,"STOP","NONE","NOT_NEEDED","xkeen-service-or-netfilter-busy");return}
 released:=false
 defer func(){if !released && !unlock(){setAutomationMutationBlocked(true)}}()
 // After acquiring native XKeen service + NF locks, the candidate must
 // still match the previously validated ready runtime.
 if !xkeenIPSetAdmission(a){
  fail(http.StatusConflict,"STOP","NONE","NOT_NEEDED","xkeen-admission-changed");return
 }
 if !xkeenIPSetUnchanged(ctx,sets){fail(http.StatusConflict,"STOP","NONE","NOT_NEEDED","kernel-changed-before-apply");return}
 snapshot,e:=xkeenIPSetSnapshot(sets)
 if e!=nil{fail(http.StatusServiceUnavailable,"STOP","NONE","NOT_NEEDED","snapshot-unavailable");return}
 disk:=xkeenIPSetDiskState{State:"RUNNING",Rollback:"PENDING",Snapshot:snapshot,DiskSHA256:plan.DiskSHA256,RuntimeSHA256:plan.RuntimeSHA256}
 if xkeenIPSetWriteState(disk)!=nil{fail(http.StatusServiceUnavailable,"STOP","NONE","NOT_NEEDED","journal-not-durable");return}
 prepared:=[]xkeenIPSet{}
 for i:=range sets{
  if !sets[i].changed{continue}
  if e=xkeenIPSetPrepare(ctx,&sets[i]);e!=nil{
   _=xkeenIPSetCleanup(ctx,sets)
   disk.State="FAILED";disk.Rollback="NOT_NEEDED"
   if xkeenIPSetWriteState(disk)!=nil{halt("candidate-failed-journal-unknown");return}
   fail(http.StatusUnprocessableEntity,"FAILED","NONE","NOT_NEEDED","candidate-validation-failed");return
  }
  prepared=append(prepared,sets[i])
 }
 if !xkeenIPSetUnchanged(ctx,sets){
  _=xkeenIPSetCleanup(ctx,prepared);disk.State="FAILED";disk.Rollback="NOT_NEEDED"
  if xkeenIPSetWriteState(disk)!=nil{halt("preapply-journal-unknown");return}
  fail(http.StatusConflict,"FAILED","NONE","NOT_NEEDED","runtime-changed-before-swap");return
 }
 swapped:=[]xkeenIPSet{}
 mutationErr:=false
 for _,s:=range prepared{
  _,swapErr:=xkeenIPSetRun(ctx,"","swap",s.tmp,s.name)
  if swapErr!=nil{
   // An ipset failure may occur after the kernel already performed the swap.
   cur,err:=xkeenIPSetRead(ctx,s.name,s.family,s.desired,false)
   if err!=nil{halt("swap-outcome-unknown");return}
   if xkeenIPSetSame(cur.active,s.desired){swapped=append(swapped,s)}else if !xkeenIPSetSame(cur.active,s.active){
    halt("swap-outcome-unknown");return
   }
   mutationErr=true;break
  }
  swapped=append(swapped,s)
  cur,err:=xkeenIPSetRead(ctx,s.name,s.family,s.desired,false)
  if err!=nil||!xkeenIPSetSame(cur.active,s.desired){mutationErr=true;break}
 }
 if !mutationErr{
  for _,s:=range sets{
   cur,err:=xkeenIPSetRead(ctx,s.name,s.family,s.desired,!s.present)
   if err!=nil||cur.present!=s.present||!xkeenIPSetSame(cur.active,s.desired){mutationErr=true;break}
  }
 }
 if mutationErr{
  // Use a fresh bounded rollback context, even when the apply timeout has fired.
  rbctx,rbcancel:=context.WithTimeout(context.Background(),12*time.Second);defer rbcancel()
  if !xkeenIPSetRollback(rbctx,swapped,sets){halt("rollback-failed-or-unverified");return}
  if !xkeenIPSetCleanup(rbctx,prepared){halt("rollback-cleanup-unverified");return}
  disk.State="ROLLED_BACK";disk.Rollback="SUCCESS"
  if xkeenIPSetWriteState(disk)!=nil{halt("rollback-receipt-not-durable");return}
  fail(http.StatusBadGateway,"ROLLED_BACK","ROLLED_BACK","SUCCESS","kernel-apply-failed-previous-membership-restored");return
 }
 if !xkeenIPSetCleanup(ctx,prepared){
  // Do not write SUCCESS until temporary sets are verified removed.
  // The durable RUNNING receipt remains a persistent STOP on restart.
  halt("temporary-ipsets-unresolved");return
 }
 // Do not send SUCCESS if the locks cannot be returned to upstream.
 released=true
 if !unlock(){halt("native-XKeen-lock-release-unverified");return}
 if xkeenIPSetWriteState(xkeenIPSetDiskState{State:"SUCCESS",Rollback:"NOT_NEEDED",Snapshot:snapshot,DiskSHA256:plan.DiskSHA256,RuntimeSHA256:plan.RuntimeSHA256})!=nil{
  halt("kernel-applied-but-terminal-receipt-unavailable");return
 }
 writeJSON(w,http.StatusOK,xkeenIPSetApplyResponse{Success:true,State:"SUCCESS",Mutation:"KERNEL_APPLIED",Rollback:"NOT_NEEDED",DiskSHA256:plan.DiskSHA256,ClientEgress:"NOT_VERIFIED"})
}
func registerXKeenIPSetActivation(mux *http.ServeMux,a *app){
 mux.HandleFunc("GET /api/config-studio/list/ipset-plan",a.requireAuth(a.handleXKeenIPSetPlan))
 mux.HandleFunc("POST /api/config-studio/list/ipset-apply",a.requireAuth(a.handleXKeenIPSetApply))
}
