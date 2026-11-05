package main

// This constructs the persistent Xray side of a bounded, no-restart AutoVPN
// pool. Nothing in this file touches a live config, process or firewall.
// The guarded apply and runtime override are separate transaction stages.
import (
 "bytes"
 "encoding/json"
 "errors"
 "fmt"
 "strings"
)

const (
 freeNetAutoPoolLimit = 3
 freeNetAutoBalancerTag = "freenet-auto-vpn"
 freeNetAutoOutboundPrefix = "freenet-auto-node-"
)

type freeNetAutoPoolCandidate struct {
 Outbounds []byte
 Routing []byte
 Tags []string
 MigratedRules int
}

// An explicit whitelist is essential: never admit arbitrary "outbound"
// objects from a subscription as a runtime handler.
func freeNetAutoPoolVLESSCompatible(ob map[string]json.RawMessage) bool {
 var protocol,tag string
 if json.Unmarshal(ob["protocol"],&protocol)!=nil || protocol!="vless" {return false}
 if json.Unmarshal(ob["tag"],&tag)!=nil || strings.TrimSpace(tag)=="" {return false}
 var stream struct {
  Network string `json:"network"`
  Security string `json:"security"`
 }
 if json.Unmarshal(ob["streamSettings"],&stream)!=nil ||
  (stream.Network!="tcp" && stream.Network!="raw") ||
  stream.Security!="reality" {return false}
 // The original complete JSON is retained; only the tag is overwritten.
 // Deep Xray config validation is mandatory before a future live apply.
 var settings struct {
  VNext []struct {
   Address string `json:"address"`
   Port int `json:"port"`
   Users []json.RawMessage `json:"users"`
  } `json:"vnext"`
 }
 return json.Unmarshal(ob["settings"],&settings)==nil && len(settings.VNext)==1 &&
  strings.TrimSpace(settings.VNext[0].Address)!="" &&
  settings.VNext[0].Port>=1 && settings.VNext[0].Port<=65535 &&
  len(settings.VNext[0].Users)==1
}

func freeNetAutoPoolReadObject(data []byte, root string) (map[string]json.RawMessage,map[string]json.RawMessage,error) {
 if len(data)==0 || len(data)>maxConfigStudioFileBytes ||
  rejectDuplicateJSONKeys(data)!=nil {return nil,nil,errors.New("candidate source invalid or ambiguous")}
 var document map[string]json.RawMessage
 if json.Unmarshal(data,&document)!=nil || document==nil {return nil,nil,errors.New("candidate root malformed")}
 var nested map[string]json.RawMessage
 if json.Unmarshal(document[root],&nested)!=nil || nested==nil {return nil,nil,errors.New("candidate section absent")}
 return document,nested,nil
}

func freeNetAutoPoolParseOutbound(data json.RawMessage) (map[string]json.RawMessage,string,error) {
 var item map[string]json.RawMessage
 if json.Unmarshal(data,&item)!=nil || item==nil {return nil,"",errors.New("outbound malformed")}
 var tag string
 if json.Unmarshal(item["tag"],&tag)!=nil || tag=="" {return nil,"",errors.New("outbound without a tag")}
 return item,tag,nil
}

func buildFreeNetAutoPoolCandidate(liveOutbounds,liveRouting []byte, reserve []map[string]any) (freeNetAutoPoolCandidate,error) {
 var out freeNetAutoPoolCandidate
 if len(reserve)<1 || len(reserve)>=freeNetAutoPoolLimit {
  return out,errors.New("pool requires one or two verified alternatives")
 }
 if len(liveOutbounds)==0 || len(liveOutbounds)>maxConfigStudioFileBytes ||
    rejectDuplicateJSONKeys(liveOutbounds)!=nil {
  return out,errors.New("invalid or oversized outbound document")
 }
 var outDoc map[string]json.RawMessage
 if json.Unmarshal(liveOutbounds,&outDoc)!=nil || outDoc==nil{
  return out,errors.New("invalid outbound document")
 }
 var rawOutbound []json.RawMessage
 if json.Unmarshal(outDoc["outbounds"],&rawOutbound)!=nil || len(rawOutbound)==0 {
  return out,errors.New("outbound list absent")
 }
 existing:=make(map[string]bool,len(rawOutbound))
 currentCount:=0
 for _,raw:=range rawOutbound {
  item,tag,e:=freeNetAutoPoolParseOutbound(raw); if e!=nil{return out,e}
  if existing[tag] {return out,errors.New("duplicate original outbound tag")}
  existing[tag]=true
  if tag==freeNetAutoBalancerTag || strings.HasPrefix(tag,freeNetAutoOutboundPrefix){
   return out,errors.New("existing reserved balancer namespace")
  }
  if strings.HasPrefix(tag,"vless-reality") && tag!="vless-reality"{
   return out,errors.New("ambiguous current VLESS outbound prefix")
  }
  if tag=="vless-reality"{
   if !freeNetAutoPoolVLESSCompatible(item){return out,errors.New("current VLESS Reality incompatible")}
   currentCount++
  }
 }
 if currentCount!=1 {return out,errors.New("exactly one live VLESS Reality required")}
 routDoc,routRoot,err:=freeNetAutoPoolReadObject(liveRouting,"routing")
 if err!=nil {return out,err}
 var rules []map[string]json.RawMessage
 if json.Unmarshal(routRoot["rules"],&rules)!=nil || len(rules)==0 {
  return out,errors.New("routing rules unavailable")
 }
 var balancers []json.RawMessage
 if len(routRoot["balancers"])!=0 {
  if json.Unmarshal(routRoot["balancers"],&balancers)!=nil{return out,errors.New("routing balancers malformed")}
  // Fail closed on existing balancers rather than accidentally bypassing
  // an ISP or chain-of-balancers policy.
  if len(balancers)!=0{return out,errors.New("existing routing balancer requires explicit reconciliation")}
 }
 migrated:=0
 for _,rule:=range rules {
  if rule==nil {return out,errors.New("null routing rule")}
  var target string
  if value,ok:=rule["outboundTag"];ok && json.Unmarshal(value,&target)!=nil {
   return out,errors.New("invalid outbound routing target")
  }
  if _,mixed:=rule["balancerTag"];mixed {
   return out,errors.New("existing balanced routing rule requires reconciliation")
  }
  if target=="vless-reality" {
   delete(rule,"outboundTag")
   rule["balancerTag"]=json.RawMessage(`"freenet-auto-vpn"`)
   migrated++
  }
 }
 if migrated==0{return out,errors.New("no exact current VPN routing target")}
 newOutbound:=append([]json.RawMessage(nil),rawOutbound...)
 newTags:=[]string{"vless-reality"}
 for index,alternative:=range reserve {
  b,e:=json.Marshal(alternative)
  if e!=nil || rejectDuplicateJSONKeys(b)!=nil{return out,errors.New("alternative outbound malformed")}
  var item map[string]json.RawMessage
  if json.Unmarshal(b,&item)!=nil || !freeNetAutoPoolVLESSCompatible(item){
   return out,errors.New("alternative must be one VLESS Reality TCP/RAW outbound")
  }
  tag:=fmt.Sprintf("%s%d",freeNetAutoOutboundPrefix,index+1)
  item["tag"],_=json.Marshal(tag)
  raw,e:=json.Marshal(item);if e!=nil{return out,errors.New("alternative encoding failed")}
  newOutbound=append(newOutbound,raw)
  newTags=append(newTags,tag)
 }
 // Xray's selector is a prefix match. We reject any ambiguous original
 // namespace above, so no untrusted outbound can enter this pool.
 balancer:=map[string]any{
  "tag":freeNetAutoBalancerTag,
  "selector":[]string{"vless-reality",freeNetAutoOutboundPrefix},
  "strategy":map[string]any{"type":"leastPing"},
 }
 encodedRules,err:=json.Marshal(rules);if err!=nil{return out,err}
 encodedBalancers,err:=json.Marshal([]any{balancer});if err!=nil{return out,err}
 encodedOutbounds,err:=json.Marshal(newOutbound);if err!=nil{return out,err}
 routRoot["rules"]=encodedRules
 routRoot["balancers"]=encodedBalancers
 outDoc["outbounds"]=encodedOutbounds
 routDoc["routing"],err=json.Marshal(routRoot);if err!=nil{return out,err}
 outDocBytes,err:=json.MarshalIndent(outDoc,"","  ");if err!=nil{return out,err}
 routDocBytes,err:=json.MarshalIndent(routDoc,"","  ");if err!=nil{return out,err}
 if bytes.Equal(outDocBytes,liveOutbounds) || bytes.Equal(routDocBytes,liveRouting){
  return out,errors.New("unexpected noop pool candidate")
 }
 return freeNetAutoPoolCandidate{
  Outbounds:append(outDocBytes,'\n'),Routing:append(routDocBytes,'\n'),
  Tags:newTags,MigratedRules:migrated,
 },nil
}
