package main

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "sort"
 "strings"
 "testing"
)

type fakeXKeenIPSet struct{ family string; entries []string }
type fakeXKeenIPSetRuntime struct{
 sets map[string]fakeXKeenIPSet
 failSwapName string
 swapFailed bool
 commands []string
}
func newFakeXKeenIPSets() *fakeXKeenIPSetRuntime {
 return &fakeXKeenIPSetRuntime{sets:map[string]fakeXKeenIPSet{
  "user_exclude":{family:"inet",entries:[]string{"192.168.0.0/16"}},
  "user_exclude6":{family:"inet6",entries:[]string{"2001:db8::1/128"}},
 }}
}
func (f *fakeXKeenIPSetRuntime) run(_ context.Context, stdin string,args ...string)(string,error){
 f.commands=append(f.commands,strings.Join(args," "))
 if len(args)==0{return "",errors.New("missing operation")}
 switch args[0]{
 case "save":
  if len(args)!=2{return "",errors.New("invalid save")}
  s,ok:=f.sets[args[1]];if !ok{return "",errors.New("set absent")}
  var b strings.Builder
  fmt.Fprintf(&b,"create %s hash:net family %s hashsize 1024 maxelem 65536\n",args[1],s.family)
  for _,value:=range s.entries{fmt.Fprintf(&b,"add %s %s\n",args[1],value)}
  return b.String(),nil
 case "list":
  var b strings.Builder
  for name:=range f.sets {b.WriteString(name);b.WriteByte('\n')}
  return b.String(),nil
 case "create":
  if len(args)<5||args[2]!="hash:net"||args[3]!="family"{return "",errors.New("invalid create")}
  if _,ok:=f.sets[args[1]];ok{return "",errors.New("set already exists")}
  f.sets[args[1]]=fakeXKeenIPSet{family:args[4]}
  return "",nil
 case "restore":
  for _,line:=range strings.Split(strings.TrimSpace(stdin),"\n"){
   if line==""{continue}
   tokens:=strings.Fields(line)
   if len(tokens)!=3||tokens[0]!="add"{return "",errors.New("invalid restore")}
   current,ok:=f.sets[tokens[1]];if !ok{return "",errors.New("restore missing set")}
   current.entries=append(current.entries,tokens[2]);sort.Strings(current.entries)
   f.sets[tokens[1]]=current
  }
  return "",nil
 case "swap":
  if len(args)!=3{return "",errors.New("invalid swap")}
  if args[2]==f.failSwapName && !f.swapFailed {
   f.swapFailed=true
   return "",errors.New("injected swap failure")
  }
  a,aok:=f.sets[args[1]];b,bok:=f.sets[args[2]]
  if !aok||!bok||a.family!=b.family{return "",errors.New("invalid swap sets")}
  f.sets[args[1]]=b;f.sets[args[2]]=a;return "",nil
 case "destroy":
  if len(args)!=2{return "",errors.New("invalid destroy")}
  if _,ok:=f.sets[args[1]];!ok{return "",errors.New("missing")}
  delete(f.sets,args[1]);return "",nil
 default:return "",fmt.Errorf("unexpected ipset command %q",args[0])
 }
}
func withFakeXKeenIPSet(t *testing.T) (*app,string,*fakeXKeenIPSetRuntime) {
 t.Helper()
 a,dir:=xkeenListTestHarness(t)
 hook:=t.TempDir()
 if err:=os.Chmod(hook,0700);err!=nil{t.Fatal(err)}
 fake:=newFakeXKeenIPSets()
 prevRun,prevAdmit,prevHook:=xkeenIPSetRun,xkeenIPSetAdmission,xkeenIPSetHookRoot
 xkeenIPSetRun=fake.run
 xkeenIPSetAdmission=func(_ *app)bool{return true}
 xkeenIPSetHookRoot=func()string{return hook}
 t.Cleanup(func(){xkeenIPSetRun=prevRun;xkeenIPSetAdmission=prevAdmit;xkeenIPSetHookRoot=prevHook})
 return a,dir,fake
}
func fakeXKeenIPSetApply(t *testing.T,a *app,p xkeenIPSetPlanResponse)(int,xkeenIPSetApplyResponse){
 t.Helper()
 body,_:=json.Marshal(xkeenIPSetApplyRequest{ExpectedDiskSHA256:p.DiskSHA256,ExpectedRuntimeSHA256:p.RuntimeSHA256,Confirm:"apply-ip-exclude-kernel"})
 r:=httptest.NewRequest(http.MethodPost,"http://router/api/config-studio/list/ipset-apply",bytes.NewReader(body))
 r.Header.Set("Content-Type","application/json")
 w:=httptest.NewRecorder();a.handleXKeenIPSetApply(w,r)
 var result xkeenIPSetApplyResponse
 if json.Unmarshal(w.Body.Bytes(),&result)!=nil{t.Fatalf("invalid response %d %s",w.Code,w.Body.String())}
 return w.Code,result
}
func TestXKeenIPSetPlanAndTransactionalIPv4IPv6Apply(t *testing.T){
 a,dir,fake:=withFakeXKeenIPSet(t)
 target:="# keep comment\n198.51.100.11/32\n2001:db8::2/128\n"
 if err:=os.WriteFile(filepath.Join(dir,"ip_exclude.lst"),[]byte(target),0600);err!=nil{t.Fatal(err)}
 plan,sets:=xkeenIPSetPlan(context.Background(),a)
 if !plan.Success||!plan.Ready||plan.State!="READY"||plan.Add!=2||plan.Remove!=2||len(sets)!=2{
  t.Fatalf("invalid read-only plan %+v",plan)
 }
 if len(fake.commands)!=2||fake.commands[0]!="save user_exclude"||fake.commands[1]!="save user_exclude6"{
  t.Fatalf("GET plan performed mutation: %v",fake.commands)
 }
 status,result:=fakeXKeenIPSetApply(t,a,plan)
 if status!=http.StatusOK||!result.Success||result.State!="SUCCESS"||result.Mutation!="KERNEL_APPLIED"||
   result.Rollback!="NOT_NEEDED"||result.ClientEgress!="NOT_VERIFIED" {
  t.Fatalf("activation invalid status=%d result=%+v",status,result)
 }
 if got:=strings.Join(fake.sets["user_exclude"].entries,",");got!="198.51.100.11/32"{t.Fatal(got)}
 if got:=strings.Join(fake.sets["user_exclude6"].entries,",");got!="2001:db8::2/128"{t.Fatal(got)}
 if got,_:=os.ReadFile(filepath.Join(dir,"ip_exclude.lst"));string(got)!=target{t.Fatal("activation rewrote saved comments")}
 if _,e:=os.Stat(a.cfg.LockPath);!os.IsNotExist(e){t.Fatal("writer lock leaked")}
 p,_:=xkeenIPSetPlan(context.Background(),a)
 if p.State!="SYNCED"||p.Add!=0||p.Remove!=0{t.Fatalf("postcheck not synced %+v",p)}
 snapshots,_:=filepath.Glob(filepath.Join(dir,".freenet-backups","xkeen-ipset-*","user_exclude.save"))
 if len(snapshots)!=1{t.Fatal("live kernel snapshot missing")}
}
func TestXKeenIPSetStalePlanAndUnknownJournalStop(t *testing.T){
 a,dir,fake:=withFakeXKeenIPSet(t)
 _=os.WriteFile(filepath.Join(dir,"ip_exclude.lst"),[]byte("203.0.113.9/32\n"),0600)
 plan,_:=xkeenIPSetPlan(context.Background(),a)
 fake.sets["user_exclude"]=fakeXKeenIPSet{family:"inet",entries:[]string{"203.0.113.17/32"}}
 status,res:=fakeXKeenIPSetApply(t,a,plan)
 if status!=http.StatusConflict||res.Mutation!="NONE"{t.Fatalf("stale CAS accepted %d %+v",status,res)}
 for _,c:=range fake.commands{if strings.HasPrefix(c,"swap "){t.Fatal("stale plan changed kernel")}}
 if e:=xkeenIPSetWriteState(xkeenIPSetDiskState{State:"RUNNING",Rollback:"PENDING"});e!=nil{t.Fatal(e)}
 p,_:=xkeenIPSetPlan(context.Background(),a)
 if p.Ready||p.Reason!="previous-activation-unresolved"{t.Fatalf("unknown journal not STOP %+v",p)}
}
func TestXKeenIPSetPartialFailureRestoresBothFamilies(t *testing.T){
 a,dir,fake:=withFakeXKeenIPSet(t)
 _=os.WriteFile(filepath.Join(dir,"ip_exclude.lst"),[]byte("198.51.100.2/32\n2001:db8::2/128\n"),0600)
 plan,_:=xkeenIPSetPlan(context.Background(),a)
 fake.failSwapName="user_exclude6"
 status,result:=fakeXKeenIPSetApply(t,a,plan)
 if status!=http.StatusBadGateway||result.State!="ROLLED_BACK"||result.Rollback!="SUCCESS"{
  t.Fatalf("kernel rollback not confirmed %d %+v",status,result)
 }
 if got:=strings.Join(fake.sets["user_exclude"].entries,",");got!="192.168.0.0/16"{t.Fatal(got)}
 if got:=strings.Join(fake.sets["user_exclude6"].entries,",");got!="2001:db8::1/128"{t.Fatal(got)}
 state,e:=xkeenIPSetReadState()
 if e!=nil||state.State!="ROLLED_BACK"||state.Rollback!="SUCCESS"{t.Fatalf("rollback receipt missing %+v %v",state,e)}
}
func TestXKeenIPSetRejectsUnsafeDiskAndUnknownRuntime(t *testing.T){
 a,dir,fake:=withFakeXKeenIPSet(t)
 _=os.WriteFile(filepath.Join(dir,"ip_exclude.lst"),[]byte("192.168.0.0/16\n"),0600)
 p,_:=xkeenIPSetPlan(context.Background(),a)
 if p.Ready||p.Reason!="unsafe-or-invalid-list"{t.Fatalf("broad LAN not blocked %+v",p)}
 _=os.WriteFile(filepath.Join(dir,"ip_exclude.lst"),[]byte("2001:db8::2/128\n"),0600)
 delete(fake.sets,"user_exclude6")
 p,_=xkeenIPSetPlan(context.Background(),a)
 if p.Ready||p.Reason!="ipv6-ipset-unverified"{t.Fatalf("missing IPv6 runtime not blocked %+v",p)}
}

// Upstream XKeen 2.1 populates hash:net sets. ipset versions differ in
// serialization of the hash metadata; active semantic flags remain STOP.
func TestXKeenIPSetOfficialCompatibleHashSave(t *testing.T){
 const saved="create user_exclude hash:net family inet hashsize 1024 maxelem 65536 bucketsize 12 initval 0xabcd1234\nadd user_exclude 198.51.100.8\n"
 members,opts,err:=xkeenIPSetParse(saved,"user_exclude","inet")
 if err!=nil || len(members)!=1 || members[0]!="198.51.100.8/32" ||
  strings.Join(opts," ")!="hashsize 1024 maxelem 65536 bucketsize 12 initval 0xabcd1234"{
  t.Fatalf("upstream hash:net serialization rejected: members=%v opts=%v error=%v",members,opts,err)
 }
 for _,extra:=range []string{" timeout 20"," counters"," comment"," netmask 24"," bucketsize 0"," initval ../../bad"}{
  prefix:=strings.Replace(saved," initval 0xabcd1234","",1)
  prefix=strings.Replace(prefix,"\nadd ",extra+"\nadd ",1)
  if _,_,err:=xkeenIPSetParse(prefix,"user_exclude","inet");err==nil{
   t.Fatalf("unsafe hash:net option accepted: %q",extra)
  }
 }
}

func TestXKeenIPSetExistingPartnerUIHostAddressParity(t *testing.T){
 v4,v6,err:=xkeenIPSetDesired("# owned by XKeen UI\n77.88.8.8\n2001:db8::11\n198.51.100.0/24\n")
 if err!=nil{t.Fatal(err)}
 if got:=strings.Join(v4,",");got!="198.51.100.0/24,77.88.8.8/32"{
  t.Fatalf("native XKeen host/CIDR grammar not preserved: %s",got)
 }
 if got:=strings.Join(v6,",");got!="2001:db8::11/128"{t.Fatal(got)}
 for _,bad:=range []string{"192.168.0.0/16\n","1.1.1.1;reboot\n","fc00::/7\n"}{
  if _,_,err:=xkeenIPSetDesired(bad);err==nil{t.Fatalf("unsafe exclusion allowed: %q",bad)}
 }
}
