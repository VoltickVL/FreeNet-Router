package main

// XKeen's external lists have a lifecycle distinct from Xray JSON. Saving
// a list does not imply that XKeen has reloaded its kernel ipsets/iptables.
import (
 "bytes"
 "encoding/json"
 "errors"
 "io"
 "net/netip"
 "net/http"
 "os"
 "path/filepath"
 "strconv"
 "strings"
 "unicode/utf8"
)

const maxXKeenListEditBytes = 128 << 10

type xkeenListSaveRequest struct {
 File string `json:"file"`
 Text string `json:"text"`
 ExpectedSHA256 string `json:"expected_sha256"`
}
type xkeenListSaveResponse struct {
 Success bool `json:"success"`
 Mutation string `json:"mutation"`
 Activation string `json:"activation"`
 Rollback string `json:"rollback"`
 SHA256 string `json:"sha256,omitempty"`
 Error string `json:"error,omitempty"`
}

func configStudioXKeenDir() string {
 if dir := strings.TrimSpace(os.Getenv("FREENET_XKEEN_CONFIG_DIR")); dir != "" { return dir }
 return defaultXKeenConfigDir
}

func xkeenListNameAllowed(name string) bool {
 switch name {
 case "ip_exclude.lst", "port_exclude.lst", "port_proxying.lst": return true
 default: return false
 }
}

func safeXKeenListDefault(name string) string {
 if name == "ip_exclude.lst" {
  // These are examples, not active exclusions! A /16 source match on a
  // 192.168.x.x LAN silently sends all clients DIRECT.
  return "#192.168.0.0/16\n#2001:db8::/32\n\n# IP / subnet: one entry per line. Remove # only intentionally.\n"
 }
 return ""
}

func xkeenDangerouslyBroadLAN(prefix netip.Prefix) bool {
 if !prefix.IsValid() { return false }
 prefix=prefix.Masked()
 if prefix.Addr().Is4() {
  return prefix.Bits() <= 24 && (prefix.Addr().IsPrivate() ||
   prefix.Contains(netip.MustParseAddr("192.168.50.1")) ||
   prefix.Contains(netip.MustParseAddr("172.16.0.1")) ||
   prefix.Contains(netip.MustParseAddr("10.0.0.1")))
 }
 return prefix.Bits() <= 64 && prefix.Addr().IsPrivate()
}

func validateXKeenListText(name, text string) (string,error) {
 if !xkeenListNameAllowed(name) { return "", errors.New("unsupported XKeen list") }
 if len(text)>maxXKeenListEditBytes || !utf8.ValidString(text) ||
  strings.ContainsRune(text, 0) { return "",errors.New("XKeen list size or encoding invalid") }
 text=strings.ReplaceAll(text,"\r\n","\n")
 if strings.ContainsRune(text,'\r') { return "",errors.New("XKeen list line endings invalid") }
 for _,line:=range strings.Split(text,"\n") {
  val:=strings.TrimSpace(line)
  if val=="" || strings.HasPrefix(val,"#") {continue}
  // XKeen 2.1 and the companion XKeen UIs accept host IPs, CIDRs and
  // inline hash comments. Preserve file text, but validate the exact token
  // the native XKeen sed/grep loader will use. Never execute the list.
  if name=="ip_exclude.lst" {
   val=strings.TrimSpace(strings.SplitN(val,"#",2)[0])
   if val=="" {continue}
  }
  if strings.ContainsAny(val," \\t#;|&$<>\\\\") { return "",errors.New("invalid XKeen list entry") }
  if name=="ip_exclude.lst" {
   var prefix netip.Prefix
   if addr,err:=netip.ParseAddr(val);err==nil {
    bits:=128
    if addr.Is4(){bits=32}
    prefix=netip.PrefixFrom(addr,bits)
   } else {
    parsed,err:=netip.ParsePrefix(val)
    if err!=nil{return "",errors.New("invalid IP or CIDR in XKeen list")}
    prefix=parsed
   }
   if xkeenDangerouslyBroadLAN(prefix) {
    return "",errors.New("broad LAN CIDR exclusion would bypass VPN for clients; comment out that line")
   }
  } else {
   parts:=strings.Split(val,":")
   if len(parts)<1||len(parts)>2{return "",errors.New("invalid port range")}
   lower,err:=strconv.Atoi(parts[0]);if !xkeenPortDecimal(parts[0])||err!=nil||lower<1||lower>65535{return "",errors.New("invalid port")}
   if len(parts)==2 {
    upper,err:=strconv.Atoi(parts[1]);if !xkeenPortDecimal(parts[1])||err!=nil||upper<=lower||upper>65535{return "",errors.New("invalid port range")}
   }
  }
 }
 return text,nil
}

func readXKeenListSafely(dir,name string) ([]byte,bool,error) {
 if !xkeenListNameAllowed(name){return nil,false,errors.New("unsupported XKeen list")}
 d,err:=os.Lstat(dir)
 if err!=nil||!d.IsDir()||d.Mode()&os.ModeSymlink!=0 {return nil,false,errors.New("XKeen directory unavailable")}
 path:=filepath.Join(dir,name)
 info,err:=os.Lstat(path)
 if errors.Is(err,os.ErrNotExist){return nil,false,nil}
 if err!=nil||!info.Mode().IsRegular()||info.Mode()&os.ModeSymlink!=0||
  info.Size()>maxXKeenListEditBytes{return nil,false,errors.New("XKeen list unsafe or unreadable")}
 data,err:=os.ReadFile(path)
 if err!=nil||len(data)>maxXKeenListEditBytes{return nil,false,errors.New("XKeen list unreadable")}
 return data,true,nil
}

func registerXKeenListEditAPI(mux *http.ServeMux,a *app){
 mux.HandleFunc("POST /api/config-studio/list/save",a.requireAuth(a.handleXKeenListSave))
}


func xkeenPortDecimal(s string) bool {
 if s=="" {return false}
 for _,r:=range s {if r<'0'||r>'9'{return false}}
 return true
}
func xkeenListHasEntries(s string) bool {
 for _,line:=range strings.Split(s,"\n") {
  value:=strings.TrimSpace(line)
  if value!="" && !strings.HasPrefix(value,"#"){return true}
 }
 return false
}
func xkeenListAtomicWrite(dir,name string,data []byte) error {
 if !xkeenListNameAllowed(name) {return errors.New("unsupported list")}
 tmp,err:=os.CreateTemp(dir,".freenet-list-*.tmp")
 if err!=nil{return err}
 path:=tmp.Name()
 defer os.Remove(path)
 if err=tmp.Chmod(0600);err!=nil{_ =tmp.Close();return err}
 if _,err=tmp.Write(data);err!=nil{_ =tmp.Close();return err}
 if err=tmp.Sync();err!=nil{_ =tmp.Close();return err}
 if err=tmp.Close();err!=nil{return err}
 if err=os.Rename(path,filepath.Join(dir,name));err!=nil{return err}
 return visionQUICSyncDir(dir)
}
func xkeenListRestore(dir,name string,backup configStudioBackup) error {
 if backup.Present {return xkeenListAtomicWrite(dir,name,backup.Data)}
 err:=os.Remove(filepath.Join(dir,name))
 if err!=nil && !errors.Is(err,os.ErrNotExist){return err}
 return visionQUICSyncDir(dir)
}
func xkeenListSnapshotDurable(backup configStudioBackup,name string) error {
 if backup.Snapshot=="" {return errors.New("snapshot absent")}
 file:=name
 if !backup.Present {file+=".absent"}
 f,err:=os.OpenFile(filepath.Join(backup.Snapshot,file),os.O_RDWR,0)
 if err!=nil {return errors.New("snapshot unreadable")}
 syncErr:=f.Sync()
 closeErr:=f.Close()
 if syncErr!=nil || closeErr!=nil {return errors.New("snapshot durability unverified")}
 if visionQUICSyncDir(backup.Snapshot)!=nil ||
    visionQUICSyncDir(filepath.Dir(backup.Snapshot))!=nil ||
    visionQUICSyncDir(filepath.Dir(filepath.Dir(backup.Snapshot)))!=nil {
   return errors.New("snapshot directory durability unverified")
 }
 return nil
}

func isHexLowerSHA(s string)bool{
 if len(s)!=64{return false}
 for _,c:=range s {if (c<'0'||c>'9')&&(c<'a'||c>'f'){return false}}
 return true
}

func (a *app) handleXKeenListSave(w http.ResponseWriter,r *http.Request){
 fail:=func(status int,mutation,rollback,msg string){
  writeJSON(w,status,xkeenListSaveResponse{Success:false,Mutation:mutation,Rollback:rollback,Activation:"UNVERIFIED",Error:msg})
 }
 if a==nil {fail(http.StatusServiceUnavailable,"NONE","NOT_NEEDED","FreeNet unavailable");return}
 if a.mutationBlockedBySelfUpdate(w){return}
 if !sameOrigin(r){fail(http.StatusForbidden,"NONE","NOT_NEEDED","cross-origin rejected");return}
 if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))),"application/json"){
  fail(http.StatusUnsupportedMediaType,"NONE","NOT_NEEDED","application/json required");return
 }
 body:=http.MaxBytesReader(w,r.Body,256<<10)
 defer body.Close()
 raw,err:=io.ReadAll(body)
 if err!=nil || rejectDuplicateJSONKeys(raw)!=nil {
  fail(http.StatusBadRequest,"NONE","NOT_NEEDED","invalid list request");return
 }
 dec:=json.NewDecoder(bytes.NewReader(raw));dec.DisallowUnknownFields()
 var req xkeenListSaveRequest
 if dec.Decode(&req)!=nil {fail(http.StatusBadRequest,"NONE","NOT_NEEDED","invalid list request");return}
 var trailing any
 if dec.Decode(&trailing)!=io.EOF {fail(http.StatusBadRequest,"NONE","NOT_NEEDED","trailing list request");return}
 normalized,err:=validateXKeenListText(req.File,req.Text)
 if err!=nil {fail(http.StatusUnprocessableEntity,"NONE","NOT_NEEDED",err.Error());return}
 if req.ExpectedSHA256!="" && !isHexLowerSHA(req.ExpectedSHA256){
  fail(http.StatusBadRequest,"NONE","NOT_NEEDED","invalid revision hash");return
 }
 if a.updateLockHeld() || automationMutationBlockedState() || vpnTransactionPending() ||
    xkeenUpdateLocalActive() || a.cfg.LockPath=="" {
  fail(http.StatusConflict,"NONE","NOT_NEEDED","another operation or unknown state blocks editing");return
 }
 select{case a.sem<-struct{}{}:defer func(){<-a.sem}()
 default: fail(http.StatusConflict,"NONE","NOT_NEEDED","FreeNet operation busy");return}
 if err:=os.Mkdir(a.cfg.LockPath,0700);err!=nil {
  fail(http.StatusConflict,"NONE","NOT_NEEDED","shared writer busy");return
 }
 owner:=filepath.Join(a.cfg.LockPath,"xkeen-list-save-owner")
 const ownerMark="freenet-xkeen-list-save-v1\n"
 if os.WriteFile(owner,[]byte(ownerMark),0600)!=nil {
  _=os.Remove(a.cfg.LockPath)
  fail(http.StatusServiceUnavailable,"NONE","NOT_NEEDED","writer lock unavailable");return
 }
 retainLock:=false
 defer func(){
  if retainLock{return}
  data,err:=os.ReadFile(owner)
  if err==nil && string(data)==ownerMark{
   _=os.Remove(owner)
   _=os.Remove(a.cfg.LockPath)
  }
 }()
 hardStop:=func(msg string){
  retainLock=true
  setAutomationMutationBlocked(true)
  fail(http.StatusServiceUnavailable,"STOP","FAILED/UNKNOWN",msg)
 }
 if a.updateLockHeld() || automationMutationBlockedState() || vpnTransactionPending() ||
    xkeenUpdateLocalActive(){
  fail(http.StatusConflict,"NONE","NOT_NEEDED","operation became busy");return
 }
 dir:=configStudioXKeenDir()
 old,present,err:=readXKeenListSafely(dir,req.File)
 if err!=nil{fail(http.StatusServiceUnavailable,"NONE","NOT_NEEDED",err.Error());return}
 actual:=""
 if present{actual=sha256Hex(old)}
 if req.ExpectedSHA256!=actual{
  fail(http.StatusConflict,"NONE","NOT_NEEDED","list changed; reload before saving");return
 }
 if req.File=="port_exclude.lst"||req.File=="port_proxying.lst" {
  other:="port_proxying.lst"
  if req.File==other{other="port_exclude.lst"}
  opposite,_,e:=readXKeenListSafely(dir,other)
  if e!=nil{fail(http.StatusServiceUnavailable,"NONE","NOT_NEEDED","opposite list unavailable");return}
  if xkeenListHasEntries(normalized) && xkeenListHasEntries(string(opposite)){
   fail(http.StatusUnprocessableEntity,"NONE","NOT_NEEDED","proxying and excluded ports cannot both contain active entries");return
  }
 }
 if present && bytes.Equal(old,[]byte(normalized)){
  writeJSON(w,http.StatusOK,xkeenListSaveResponse{Success:true,Mutation:"UNCHANGED",Activation:"UNVERIFIED",Rollback:"NOT_NEEDED",SHA256:actual})
  return
 }
 backup,err:=createConfigStudioSnapshot(dir,req.File,configStudioBackup{Data:old,Present:present})
 if err!=nil{fail(http.StatusServiceUnavailable,"NONE","NOT_NEEDED","cannot snapshot list");return}
 if xkeenListSnapshotDurable(backup,req.File)!=nil{
  fail(http.StatusServiceUnavailable,"NONE","NOT_NEEDED","snapshot durability unverified");return
 }
 latest,latestPresent,err:=readXKeenListSafely(dir,req.File)
 if err!=nil || latestPresent!=present || !bytes.Equal(latest,old) {
  fail(http.StatusConflict,"NONE","NOT_NEEDED","source changed while preparing save");return
 }
 writeErr:=xkeenListAtomicWrite(dir,req.File,[]byte(normalized))
 after,afterPresent,readErr:=readXKeenListSafely(dir,req.File)
 if writeErr!=nil || readErr!=nil || !afterPresent || string(after)!=normalized {
  if rb:=xkeenListRestore(dir,req.File,backup);rb!=nil{
   hardStop("list write uncertain and rollback failed");return
  }
  check,yes,verifyErr:=readXKeenListSafely(dir,req.File)
  if verifyErr!=nil || yes!=present || !bytes.Equal(check,old){
   hardStop("list rollback verification failed");return
  }
  fail(http.StatusBadGateway,"ROLLED_BACK","SUCCESS","list save rejected; exact previous bytes restored");return
 }
 writeJSON(w,http.StatusOK,xkeenListSaveResponse{Success:true,Mutation:"SAVED",Activation:"XKEEN_RELOAD_REQUIRED",Rollback:"NOT_NEEDED",SHA256:sha256Hex(after)})
}
