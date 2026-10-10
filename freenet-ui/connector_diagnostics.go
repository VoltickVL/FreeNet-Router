package main

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
)

// Connector preparation only: browser-authenticated, same-origin read-only
// machine schema. No device token, MCP listener or external transport.
func (a *app) handleConnectorDiagnostics(w http.ResponseWriter, r *http.Request) {
 w.Header().Set("Cache-Control","no-store")
 q:=r.URL.Query()
 tool:=q.Get("tool")
 if len(q)>5 {
  writeJSON(w,http.StatusBadRequest,map[string]any{"success":false,"error":"unsupported parameters","mutation":"NONE"})
  return
 }
 for key:=range q {
  if key!="tool" && key!="host" && key!="client" && key!="port" && key!="network" {
   writeJSON(w,http.StatusBadRequest,map[string]any{"success":false,"error":"unsupported parameters","mutation":"NONE"})
   return
  }
  if len(q[key])!=1 { writeJSON(w,http.StatusBadRequest,map[string]any{"success":false,"error":"duplicate parameter","mutation":"NONE"});return }
 }
 if tool!="test_route" && len(q)!=1 {
  writeJSON(w,http.StatusBadRequest,map[string]any{"success":false,"error":"unexpected tool parameters","mutation":"NONE"})
  return
 }
 base:=map[string]any{"success":true,"schema_version":1,"tool":tool,"mutation":"NONE","transport":"LOCAL_BROWSER_ONLY"}
 switch tool {
 case "get_status","get_component_health":
  status:=a.status()
  base["version"]=status.Version
  base["xray_process_running"]=status.XrayOnline
  base["xkeen_ui_running"]=status.XKeenUI
  base["dns_out_present"]=status.DNSOut
  base["busy"]=status.Busy || status.UpdaterBusy
  base["runtime_route_observed"]=false
  base["health_confidence"]="PROCESS_AND_CONFIG_ONLY"
 case "get_recent_journal":
  // Never forward raw journal lines or free-form diagnostic messages; they
  // can contain credentials and arbitrary remote-controlled content.
  base["events"]=[]any{}
  base["available"]=false
  base["reason"]="safe journal event projection is not implemented"
 case "test_route":
  request:=httptest.NewRequest(http.MethodGet,"/api/admin/route?"+r.URL.RawQuery,r.Body)
  values:=request.URL.Query();values.Del("tool");request.URL.RawQuery=values.Encode()
  recorder:=httptest.NewRecorder()
  a.handleAdminRoute(recorder,request)
  var result map[string]any
  if err:=json.Unmarshal(recorder.Body.Bytes(),&result);err!=nil {
   writeJSON(w,http.StatusInternalServerError,map[string]any{"success":false,"error":"route diagnostic unavailable","mutation":"NONE"});return
  }
  if recorder.Code!=http.StatusOK {
   writeJSON(w,recorder.Code,map[string]any{"success":false,"error":"invalid route diagnostic request","mutation":"NONE"});return
  }
  for _,key:=range []string{"expected_action","confidence","observed_route","matched_rule_order","rules_sha256","network","port"} {
   if value,ok:=result[key];ok {base[key]=value}
  }
  base["runtime_route_observed"]=false
  base["note"]="on-disk rule estimate only; no live client traffic observation"
 default:
  writeJSON(w,http.StatusBadRequest,map[string]any{"success":false,"error":"unsupported diagnostic tool","mutation":"NONE"})
  return
 }
 writeJSON(w,http.StatusOK,base)
}
