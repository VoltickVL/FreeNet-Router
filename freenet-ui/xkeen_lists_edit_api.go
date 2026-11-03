package main

// XKeen's external lists have a lifecycle distinct from Xray JSON. Saving
// a list does not imply that XKeen has reloaded its kernel ipsets/iptables.
import (
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
  // Prevent executable shell content and ambiguous inline fragments. XKeen's
  // documented list grammar accepts only address/CIDR or port/range.
  if strings.ContainsAny(val," \t#;|&$<>\\") { return "",errors.New("invalid XKeen list entry") }
  if name=="ip_exclude.lst" {
   if ip,err:=netip.ParseAddr(val);err==nil {
    if !ip.IsValid(){return "",errors.New("invalid IP exclusion")}
    continue
   }
   p,err:=netip.ParsePrefix(val)
   if err!=nil {return "",errors.New("invalid IP or CIDR in XKeen list")}
   if xkeenDangerouslyBroadLAN(p) {
    return "",errors.New("broad LAN CIDR exclusion would bypass VPN for clients; comment out that line")
   }
  } else {
   parts:=strings.Split(val,":")
   if len(parts)<1||len(parts)>2{return "",errors.New("invalid port range")}
   lower,err:=strconv.Atoi(parts[0]);if err!=nil||lower<1||lower>65535{return "",errors.New("invalid port")}
   if len(parts)==2 {
    upper,err:=strconv.Atoi(parts[1]);if err!=nil||upper<lower||upper>65535{return "",errors.New("invalid port range")}
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
  info.Size()>maxConfigStudioFileBytes{return nil,false,errors.New("XKeen list unsafe or unreadable")}
 data,err:=os.ReadFile(path);if err!=nil{return nil,false,errors.New("XKeen list unreadable")}
 return data,true,nil
}

func registerXKeenListEditAPI(mux *http.ServeMux,a *app){
 mux.HandleFunc("POST /api/config-studio/list/save",a.requireAuth(a.handleXKeenListSave))
}

func (a *app) handleXKeenListSave(w http.ResponseWriter,r *http.Request){
 fail:=func(status int,mutation,rollback,err string){
  writeJSON(w,status,xkeenListSaveResponse{Success:false,Mutation:mutation,Rollback:rollback,Activation:"UNVERIFIED",Error:err})
 }
 if a.mutationBlockedBySelfUpdate(w){return}
 if !sameOrigin(r){fail(http.StatusForbidden,"NONE","NOT_NEEDED","cross-origin request rejected");return}
 if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))),"application/json"){
  fail(http.StatusUnsupportedMediaType,"NONE","NOT_NEEDED","application/json required");return
 }
 body:=http.MaxBytesReader(w,r.Body,maxXKeenListEditBytes+1024)
 defer body.Close()
 dec:=json.NewDecoder(body);dec.DisallowUnknownFields()
 var req xkeenListSaveRequest
 if err:=dec.Decode(&req);err!=nil{fail(http.StatusBadRequest,"NONE","NOT_NEEDED","invalid list request");return}
 var extra any
 if err:=dec.Decode(&extra);err!=io.EOF{fail(http.StatusBadRequest,"NONE","NOT_NEEDED","invalid list request");return}
 normalized,err:=validateXKeenListText(req.File,req.Text)
 if err!=nil{fail(http.StatusUnprocessableEntity,"NONE","NOT_NEEDED",err.Error());return}
 if req.ExpectedSHA256!="" && (len(req.ExpectedSHA256)!=64||!isHexLowerSHA(req.ExpectedSHA256)){
  fail(http.StatusBadRequest,"NONE","NOT_NEEDED","invalid expected hash");return
 }
 select{
 case a.sem<-struct{}{}:defer func(){<-a.sem}()
 default:fail(http.StatusConflict,"NONE","NOT_NEEDED","another FreeNet mutation is running");return
 }
 dir:=configStudioXKeenDir()
 old,present,err:=readXKeenListSafely(dir,req.File)
 if err!=nil{fail(http.StatusServiceUnavailable,"NONE","NOT_NEEDED",err.Error());return}
 actual:=""
 if present {actual=sha256Hex(old)}
 if actual!=req.ExpectedSHA256 {fail(http.StatusConflict,"NONE","NOT_NEEDED","XKeen list changed; reload before saving");return}
 backup,err:=createConfigStudioSnapshot(dir,req.File,configStudioBackup{Data:old,Present:present})
 if err!=nil{fail(http.StatusServiceUnavailable,"NONE","NOT_NEEDED","cannot snapshot XKeen list");return}
 if err:=atomicWriteRoutingManagedFile(dir,req.File,[]byte(normalized));err!=nil{
  fail(http.StatusServiceUnavailable,"FAILED","NOT_NEEDED","cannot atomically save XKeen list");return
 }
 after,ok,err:=readXKeenListSafely(dir,req.File)
 if err!=nil||!ok||string(after)!=normalized {
  if rbErr:=restoreConfigStudioBackup(dir,req.File,backup);rbErr!=nil{
   fail(http.StatusInternalServerError,"STOP","FAILED","list verification failed; rollback failed");return
  }
  fail(http.StatusBadGateway,"ROLLED_BACK","SUCCESS","list verification failed; previous file restored");return
 }
 // The authoritative file is saved; do NOT pretend the runtime has reloaded.
 // A separately guarded XKeen lifecycle operation is required for activation.
 writeJSON(w,http.StatusOK,xkeenListSaveResponse{Success:true,Mutation:"SAVED",Activation:"XKEEN_RELOAD_REQUIRED",Rollback:"NOT_NEEDED",SHA256:sha256Hex(after)})
}

// No regexp or filesystem path interpolation is accepted from the client.
func isHexLowerSHA(s string)bool{
 for _,c:=range s {if (c<'0'||c>'9')&&(c<'a'||c>'f'){return false}}
 return len(s)==64
}

