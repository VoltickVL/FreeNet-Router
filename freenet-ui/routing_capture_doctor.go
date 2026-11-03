package main

// A passive kernel interception inventory. Hook files and a valid Xray
// configuration are not evidence that LAN packets are being captured.
import (
 "context"
 "net/http"
 "os/exec"
 "strings"
 "time"
)

const captureDoctorTimeout = 3 * time.Second
const captureDoctorMaxOutput = 128 << 10

type captureDoctorResponse struct {
 Success bool `json:"success"`
 Mutation string `json:"mutation"`
 State string `json:"state"`
 Netfilter string `json:"netfilter"`
 PolicyRule string `json:"policy_rule"`
 LocalRoute string `json:"local_route"`
 Hook string `json:"hook"`
 Reason string `json:"reason"`
}

// Deliberately never return raw command output, individual rules, addresses,
// credentials, marks or environment details to the web client.
func captureDoctorRead(ctx context.Context, command string, args ...string) (string, bool) {
 binary, err := exec.LookPath(command)
 if err != nil { return "", false }
 cmdCtx, cancel := context.WithTimeout(ctx, captureDoctorTimeout)
 defer cancel()
 out, err := exec.CommandContext(cmdCtx, binary, args...).Output()
 if err != nil || len(out) > captureDoctorMaxOutput { return "", false }
 return string(out), true
}

func captureDoctorHasRule(text string) bool {
 // iptables-save uses -j TPROXY; nft uses "tproxy to".
 lower := strings.ToLower(text)
 return strings.Contains(lower, "-j tproxy") || strings.Contains(lower, " tproxy to ") ||
  strings.Contains(lower, "\ntproxy to ") || strings.Contains(lower, " tproxy ip to ")
}

func captureDoctorHasPolicyRule(text string) bool {
 lower := strings.ToLower(text)
 return strings.Contains(lower, "fwmark ")
}

func captureDoctorHasLocalRoute(text string) bool {
 lower := strings.ToLower(text)
 for _, line := range strings.Split(lower, "\n") {
  if strings.Contains(line, "local default dev lo") || strings.Contains(line, "local 0.0.0.0/0 dev lo") {
   return true
  }
 }
 return false
}

func captureDoctorClassify(ctx context.Context, read func(context.Context,string,...string)(string,bool), hook string) captureDoctorResponse {
 result := captureDoctorResponse{Success:true, Mutation:"NONE", State:"unverified",
  Netfilter:"unknown", PolicyRule:"unknown", LocalRoute:"unknown", Hook:hook, Reason:"kernel-capture-unverified"}
 var filterSeen bool
 if rules, ok := read(ctx,"iptables-save","-t","mangle"); ok {
  filterSeen = true
  result.Netfilter = "tproxy-rule-absent"
  if captureDoctorHasRule(rules) { result.Netfilter = "tproxy-rule-present" }
 }
 if result.Netfilter!="tproxy-rule-present" {
  if rules, ok := read(ctx,"nft","list","ruleset"); ok {
   filterSeen = true
   if captureDoctorHasRule(rules) { result.Netfilter = "tproxy-rule-present" } else { result.Netfilter = "tproxy-rule-absent" }
  }
 }
 if !filterSeen { result.Reason="netfilter-unavailable" }
 if rules,ok:=read(ctx,"ip","-4","rule","show");ok {
  result.PolicyRule="fwmark-rule-absent"
  if captureDoctorHasPolicyRule(rules) { result.PolicyRule="fwmark-rule-present" }
 }
 if routes,ok:=read(ctx,"ip","-4","route","show","table","all");ok {
  result.LocalRoute="local-default-absent"
  if captureDoctorHasLocalRoute(routes) { result.LocalRoute="local-default-present" }
 }
 // Even an installed ruleset proves neither hits nor correct per-client
 // routing, so never return "healthy" without measured LAN egress.
 switch {
 case result.Netfilter=="tproxy-rule-absent":
  result.State="capture-rule-missing";result.Reason="no-kernel-tproxy-rule"
 case result.Netfilter=="tproxy-rule-present" && result.PolicyRule=="fwmark-rule-absent":
  result.State="policy-rule-missing";result.Reason="kernel-fwmark-rule-missing"
 case result.Netfilter=="tproxy-rule-present" && result.PolicyRule=="fwmark-rule-present" && result.LocalRoute=="local-default-absent":
  result.State="local-route-missing";result.Reason="kernel-local-route-missing"
 case result.Netfilter=="tproxy-rule-present" && result.PolicyRule=="fwmark-rule-present" && result.LocalRoute=="local-default-present":
  result.State="installed-unverified";result.Reason="lan-packet-hits-not-measured"
 }
 return result
}

func registerCaptureDoctor(mux *http.ServeMux,a *app) {
 mux.HandleFunc("GET /api/routing/capture-doctor",a.requireAuth(a.handleCaptureDoctor))
}

func (a *app) handleCaptureDoctor(w http.ResponseWriter,r *http.Request) {
 ctx,cancel:=context.WithTimeout(r.Context(),12*time.Second)
 defer cancel()
 response:=captureDoctorClassify(ctx,captureDoctorRead,xrayDiagnosticHookState("/opt/etc/ndm/netfilter.d/proxy.sh"))
 writeJSON(w,http.StatusOK,response)
}
