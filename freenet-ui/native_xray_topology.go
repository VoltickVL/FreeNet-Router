package main

import (
 "bytes"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "os"
 "path/filepath"
 "regexp"
 "strconv"
 "strings"
)

const (
 nativeXrayInventoryMaxFile = int64(2 << 20)
 nativeXrayInventoryMaxNodes = 256
 nativeXrayInventoryMaxBalancers = 32
)

type nativeXrayServerView struct {
 Order int `json:"order"`
 Name string `json:"name"`
 Protocol string `json:"protocol"`
 Eligible bool `json:"eligible"`
}

type nativeXrayBalancerView struct {
 Name string `json:"name"`
 Strategy string `json:"strategy"`
 Selectors int `json:"selectors"`
 EligibleServers int `json:"eligible_servers"`
 NativeSBCompatible bool `json:"native_sb_compatible"`
}

type nativeXrayTopologyResponse struct {
 Success bool `json:"success"`
 State string `json:"state"`
 Reason string `json:"reason,omitempty"`
 Mutation string `json:"mutation"`
 Runtime string `json:"runtime"`
 OutboundsSHA256 string `json:"outbounds_sha256,omitempty"`
 RoutingSHA256 string `json:"routing_sha256,omitempty"`
 ServerCount int `json:"server_count"`
 BalancerCount int `json:"balancer_count"`
 Servers []nativeXrayServerView `json:"servers"`
 Balancers []nativeXrayBalancerView `json:"balancers"`
}

// Names may be published to an authenticated UI, but credentials and
// subscription URLs may also appear in mistakenly constructed tags.
// Display-only fallback is preferable to guessing whether a token is safe.
var nativeXrayUUIDLike = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

func nativeXrayDisplayName(tag, fallback string) string {
 name := strings.TrimSpace(tag)
 lower := strings.ToLower(name)
 if name=="" || len(name)>100 || nativeXrayUUIDLike.MatchString(name) ||
  strings.ContainsAny(name,"\r\n\t\x00<>") ||
  strings.Contains(lower,"://") || strings.Contains(lower,"@") ||
  strings.Contains(lower,"?") || strings.Contains(lower,"token") ||
  strings.Contains(lower,"password") || strings.Contains(lower,"secret") ||
  strings.Contains(lower,"shortid") || strings.Contains(lower,"privatekey") {
  return fallback
 }
 return name
}

func nativeXrayReadConfigFile(dir,name string)([]byte,string,error){
 root,err:=os.Lstat(dir)
 if err!=nil || !root.IsDir() || root.Mode()&os.ModeSymlink!=0{
  return nil,"",errors.New("native Xray config directory unverified")
 }
 path:=filepath.Join(dir,name)
 info,err:=os.Lstat(path)
 if err!=nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink!=0 ||
  info.Size()<2 || info.Size()>nativeXrayInventoryMaxFile{
  return nil,"",errors.New("native Xray config file unverified")
 }
 file,err:=os.Open(path)
 if err!=nil{return nil,"",errors.New("native Xray config not readable")}
 defer file.Close()
 fdInfo,err:=file.Stat()
 if err!=nil||!os.SameFile(info,fdInfo){return nil,"",errors.New("native Xray file identity changed")}
 data,err:=io.ReadAll(io.LimitReader(file,nativeXrayInventoryMaxFile+1))
 if err!=nil||int64(len(data))>nativeXrayInventoryMaxFile{
  return nil,"",errors.New("native Xray read exceeded limit")
 }
 current,err:=os.Lstat(path)
 if err!=nil||!os.SameFile(info,current)||current.Size()!=int64(len(data))||
  !current.ModTime().Equal(info.ModTime()){
  return nil,"",errors.New("native Xray file changed during read")
 }
 // Official XKeen 2.2 uses strip_json_comments before jq. Reuse the
 // already tested XKeen lexer instead of inventing a second JSONC parser.
 clean,stripErr:=xkeenNativeStripJSONCommentsLimited(data,int(nativeXrayInventoryMaxFile))
 if stripErr!=nil||!json.Valid(clean)||rejectDuplicateJSONKeys(clean)!=nil{
  return nil,"",errors.New("native Xray JSONC invalid or duplicate keys")
 }
 return clean,sha256Hex(data),nil
}

type nativeXrayOutbound struct {
 Tag string `json:"tag"`
 Protocol string `json:"protocol"`
}
type nativeXrayBalancer struct {
 Tag string `json:"tag"`
 Selector []string `json:"selector"`
 Strategy struct {
  Type string `json:"type"`
 } `json:"strategy"`
}

// Match the exact XKeen 2.2_Beta sb_node_list semantics:
// startswith(selector), in original outbound-file order, excluding
// freedom/blackhole/dns/loopback. This is a static plan, NOT live routing.
func nativeXrayEligibleOutbound(out nativeXrayOutbound, selectors []string) bool {
 switch strings.ToLower(out.Protocol) {
 case "freedom","blackhole","dns","loopback":
  return false
 }
 for _,sel:=range selectors{
  if strings.HasPrefix(out.Tag,sel){return true}
 }
 return false
}

func nativeXrayInventory(dir string)(nativeXrayTopologyResponse,error){
 result:=nativeXrayTopologyResponse{Success:true,State:"CONFIG_ONLY",Mutation:"NONE",Runtime:"NOT_VERIFIED",
  Servers:[]nativeXrayServerView{},Balancers:[]nativeXrayBalancerView{}}
 outBytes,outSHA,err:=nativeXrayReadConfigFile(dir,"04_outbounds.json")
 if err!=nil{return nativeXrayTopologyResponse{},err}
 routingBytes,routingSHA,err:=nativeXrayReadConfigFile(dir,"05_routing.json")
 if err!=nil{return nativeXrayTopologyResponse{},err}

 var outDoc struct{Outbounds []nativeXrayOutbound `json:"outbounds"`}
 var routeDoc struct{
  Routing struct{
   Balancers []nativeXrayBalancer `json:"balancers"`
  } `json:"routing"`
 }
 if err=json.Unmarshal(outBytes,&outDoc);err!=nil||outDoc.Outbounds==nil||
  len(outDoc.Outbounds)>nativeXrayInventoryMaxNodes{
  return nativeXrayTopologyResponse{},errors.New("native outbounds invalid")
 }
 if err=json.Unmarshal(routingBytes,&routeDoc);err!=nil||len(routeDoc.Routing.Balancers)>nativeXrayInventoryMaxBalancers{
  return nativeXrayTopologyResponse{},errors.New("native routing invalid")
 }
 // A routing root must exist: missing root must not silently look like
 // an ordinary no-balancer Xray configuration.
 var routeRoot map[string]json.RawMessage
 if json.Unmarshal(routingBytes,&routeRoot)!=nil || len(routeRoot["routing"])==0 ||
   bytes.Equal(bytes.TrimSpace(routeRoot["routing"]),[]byte("null")){
  return nativeXrayTopologyResponse{},errors.New("native routing root missing")
 }

 outboundTags:=map[string]bool{}
 for i,out:=range outDoc.Outbounds {
  if out.Tag==""||out.Protocol==""||len(out.Tag)>512||len(out.Protocol)>80||
    strings.ContainsAny(out.Tag,"\r\n\x00")||outboundTags[out.Tag]{
    return nativeXrayTopologyResponse{},errors.New("native outbound identity unknown")
  }
  outboundTags[out.Tag]=true
  proto:=strings.ToLower(out.Protocol)
  switch proto{
  case "vless","vmess","trojan","shadowsocks","socks","http","wireguard","hysteria","freedom","blackhole","dns","loopback":
  default:
   proto="other"
  }
  result.Servers=append(result.Servers,nativeXrayServerView{Order:i+1,
    Name:nativeXrayDisplayName(out.Tag,"Сервер "+itoaNative(i+1)),Protocol:proto})
 }
 seenBalancer:=map[string]bool{}
 for i,b:=range routeDoc.Routing.Balancers{
  if b.Tag==""||len(b.Tag)>512||strings.ContainsAny(b.Tag,"\r\n\x00")||
   seenBalancer[b.Tag]||len(b.Selector)==0||len(b.Selector)>128{
   return nativeXrayTopologyResponse{},errors.New("native balancer identity invalid")
  }
  seenBalancer[b.Tag]=true
  for _,s:=range b.Selector {
   if s==""||len(s)>512||strings.ContainsAny(s,"\r\n\x00"){
    return nativeXrayTopologyResponse{},errors.New("native selector invalid")
   }
  }
  count:=0
  for k,o:=range outDoc.Outbounds{
   if nativeXrayEligibleOutbound(o,b.Selector){
    count++
    result.Servers[k].Eligible=true
   }
  }
  strategy:=b.Strategy.Type
  if strategy==""{strategy="unverified"}
  if strategy!="leastPing"&&strategy!="unverified"{strategy="other"}
  result.Balancers=append(result.Balancers,nativeXrayBalancerView{
   Name:nativeXrayDisplayName(b.Tag,"Балансировщик "+itoaNative(i+1)),
   Strategy:strategy,Selectors:len(b.Selector),EligibleServers:count,
   NativeSBCompatible:strategy=="leastPing"&&count>=2})
 }
 result.ServerCount=len(result.Servers)
 result.BalancerCount=len(result.Balancers)
 result.OutboundsSHA256=outSHA
 result.RoutingSHA256=routingSHA
 if result.BalancerCount==0{result.State="NO_BALANCER"}
 return result,nil
}

func itoaNative(n int)string{return strconv.Itoa(n)}

func (a *app) handleNativeXrayTopology(w http.ResponseWriter,r *http.Request){
 fail:=nativeXrayTopologyResponse{Success:false,State:"STOP",Reason:"native-xray-config-unavailable-or-contradictory",
  Mutation:"NONE",Runtime:"NOT_VERIFIED",Servers:[]nativeXrayServerView{},Balancers:[]nativeXrayBalancerView{}}
 if a==nil{
  writeJSON(w,http.StatusServiceUnavailable,fail);return
 }
 topology,err:=nativeXrayInventory(a.routingConfigDir())
 if err!=nil{
  writeJSON(w,http.StatusConflict,fail);return
 }
 writeJSON(w,http.StatusOK,topology)
}

func registerNativeXrayTopologyAPI(mux *http.ServeMux,a *app){
 mux.HandleFunc("GET /api/native/xray/topology",a.requireAuth(a.handleNativeXrayTopology))
}
