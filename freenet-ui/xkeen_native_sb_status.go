package main

// Native XKeen 2.1 speed-balancer inventory. All answers are read-only.
// Calling "xkeen -sb status" is intentionally forbidden here: in upstream
// 2.1, scripts/xkeen runs Entware package self-heal before that command.
// The installed XKeen/Xray stack is the owner of balancing, not FreeNet.
import (
 "bytes"
 "encoding/json"
 "errors"
 "net/http"
 "os"
 "path/filepath"
 "strconv"
 "strings"
)

const xkeenNativeSBConfigLimit = 256 << 10

type xkeenNativeSBStatus struct {
 Success bool `json:"success"`
 Owner string `json:"owner"`
 State string `json:"state"`
 Reason string `json:"reason,omitempty"`
 Mutation string `json:"mutation"`
 InstalledVersion string `json:"installed_version,omitempty"`
 InstalledChannel string `json:"installed_channel,omitempty"`
 Enabled bool `json:"enabled"`
 IntervalMinutes int `json:"interval_minutes,omitempty"`
 HysteresisPercent int `json:"hysteresis_percent,omitempty"`
 NativeCommands []string `json:"native_commands,omitempty"`
 Runtime string `json:"runtime"`
}

var xkeenNativeSBMetadata = readXKeenInstalledMetadata

// Derived from exact official XKeen release tags, not newer default-branch
// commands. Unknown versions remain STOP until upstream behavior is reviewed.
func xkeenNativeSBCapabilities(version,channel string)([]string,bool){
 switch {
 case version=="2.1"&&channel=="stable":
  return []string{"on","off","status"},true
 case (version=="2.1.1"||version=="2.2")&&channel=="beta":
  return []string{"on","off","status","list","auto","next","prev","N"},true
 default:
  return nil,false
 }
}

// Mirrors the lexer semantics of XKeen 2.1 strip_json_comments in
// 01_info/01_info_common.sh. The source is never returned or logged.
func xkeenNativeStripJSONComments(data []byte)([]byte,error){
 if len(data)>xkeenNativeSBConfigLimit{return nil,errors.New("config oversized")}
 out:=make([]byte,0,len(data))
 str,esc,block,line:=false,false,false,false
 for i:=0;i<len(data);i++{
  c:=data[i]
  if line {
   if c=='\n'{line=false;out=append(out,'\n')}
   continue
  }
  if block {
   if c=='*' && i+1<len(data)&&data[i+1]=='/'{block=false;i++;continue}
   if c=='\n'{out=append(out,'\n')}
   continue
  }
  if str {
   out=append(out,c)
   if esc{esc=false}else if c=='\\'{esc=true}else if c=='"'{str=false}
   continue
  }
  if c=='"'{str=true;out=append(out,c);continue}
  if c=='/'&&i+1<len(data)&&data[i+1]=='/'{line=true;i++;continue}
  if c=='/'&&i+1<len(data)&&data[i+1]=='*'{block=true;i++;continue}
  out=append(out,c)
 }
 if block||str{return nil,errors.New("unterminated JSONC")}
 return out,nil
}

func xkeenNativeSBPositiveInt(raw json.RawMessage, fallback,min,max int)(int,error){
 if len(raw)==0||bytes.Equal(raw,[]byte("null")){return fallback,nil}
 var number int
 if err:=json.Unmarshal(raw,&number);err!=nil{
  var value string
  if json.Unmarshal(raw,&value)!=nil{return 0,errors.New("native numeric setting invalid")}
  parsed,parseErr:=strconv.Atoi(value)
  if parseErr!=nil{return 0,errors.New("native numeric setting invalid")}
  number=parsed
 }
 if number<min || number>max{return 0,errors.New("native numeric setting outside supported bounds")}
 return number,nil
}

func xkeenNativeSBReadConfig(root string)(xkeenNativeSBStatus,error){
 status:=xkeenNativeSBStatus{Success:true,Owner:"xkeen",State:"disabled",Mutation:"NONE",Runtime:"NOT_VERIFIED",NativeCommands:[]string{"on","off","status"},IntervalMinutes:15,HysteresisPercent:25}
 info,err:=os.Lstat(root)
 if err!=nil||!info.IsDir()||info.Mode()&os.ModeSymlink!=0{return status,errors.New("XKeen config directory unavailable")}
 path:=filepath.Join(root,"xkeen.json")
 info,err=os.Lstat(path)
 if errors.Is(err,os.ErrNotExist){status.State="not-configured";return status,nil}
 if err!=nil||!info.Mode().IsRegular()||info.Mode()&os.ModeSymlink!=0||info.Size()>xkeenNativeSBConfigLimit{
  return status,errors.New("unsafe XKeen config")
 }
 raw,err:=os.ReadFile(path)
 if err!=nil||len(raw)>xkeenNativeSBConfigLimit{return status,errors.New("unreadable XKeen config")}
 newInfo,err:=os.Lstat(path)
 if err!=nil||!os.SameFile(info,newInfo)||newInfo.Size()!=info.Size()||!newInfo.ModTime().Equal(info.ModTime()){
  return status,errors.New("XKeen config changed during read")
 }
 clean,err:=xkeenNativeStripJSONComments(raw)
 if err!=nil||rejectDuplicateJSONKeys(clean)!=nil{return status,errors.New("invalid XKeen JSONC")}
 var doc struct {
  XKeen struct {
   Xray struct {
    Balancer json.RawMessage `json:"speed_balancer"`
   } `json:"xray"`
  } `json:"xkeen"`
 }
 if json.Unmarshal(clean,&doc)!=nil{return status,errors.New("invalid XKeen JSON structure")}
 if len(doc.XKeen.Xray.Balancer)==0||bytes.Equal(doc.XKeen.Xray.Balancer,[]byte("null")){return status,nil}
 var settings map[string]json.RawMessage
 if json.Unmarshal(doc.XKeen.Xray.Balancer,&settings)!=nil||settings==nil{
  return status,errors.New("invalid native XKeen speed_balancer section")
 }
 if value,ok:=settings["enabled"];ok{
  var enabled bool
  if json.Unmarshal(value,&enabled)==nil {status.Enabled=enabled}else{
   var v string
   if json.Unmarshal(value,&v)!=nil || (v!="true" && v!="false"){
    return status,errors.New("invalid native enabled setting")
   }
   status.Enabled=v=="true"
  }
 }
 status.IntervalMinutes,err=xkeenNativeSBPositiveInt(settings["interval"],15,1,1440)
 if err!=nil{return status,err}
 status.HysteresisPercent,err=xkeenNativeSBPositiveInt(settings["hysteresis"],25,0,1000)
 if err!=nil{return status,err}
 if tag,ok:=settings["balancer"];ok{
  var s string
  if json.Unmarshal(tag,&s)!=nil || len(s)==0||len(s)>128 || strings.ContainsAny(s,"\r\n\x00"){
   return status,errors.New("native balancer tag invalid")
  }
  // The tag is deliberately not returned: user-defined Xray tags may
  // contain private identifiers, and no live API evidence has been read.
 }
 if status.Enabled{status.State="configured-enabled"}
 return status,nil
}

func (a *app) handleXKeenNativeSBStatus(w http.ResponseWriter,r *http.Request){
 response:=xkeenNativeSBStatus{Owner:"xkeen",State:"STOP",Reason:"native-status-unverified",Mutation:"NONE",Runtime:"NOT_VERIFIED"}
 if a==nil||strings.TrimSpace(a.cfg.XKeenPath)==""{
  writeJSON(w,http.StatusServiceUnavailable,response);return
 }
 version,channel,err:=xkeenNativeSBMetadata(a.cfg.XKeenPath)
 commands,supported:=xkeenNativeSBCapabilities(version,channel)
 if err!=nil||!supported{
  response.Reason="installed-XKeen-release-capabilities-unverified"
  writeJSON(w,http.StatusConflict,response);return
 }
 config,err:=xkeenNativeSBReadConfig(configStudioXKeenDir())
 if err!=nil{
  response.Reason="native-balancer-config-unknown"
  writeJSON(w,http.StatusConflict,response);return
 }
 config.InstalledVersion=version
 config.InstalledChannel=channel
 config.NativeCommands=commands
 // True here is configuration state only. We never report API, Xray
 // process, actual selected node or client egress as verified.
 writeJSON(w,http.StatusOK,config)
}

func registerXKeenNativeSBAPI(mux *http.ServeMux,a *app){
 mux.HandleFunc("GET /api/xkeen/speed-balancer",a.requireAuth(a.handleXKeenNativeSBStatus))
}
