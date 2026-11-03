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
 TCPRedirect string `json:"tcp_redirect"`
 TCPPrerouting string `json:"tcp_prerouting"`
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

// Classify the active TCP path separately from UDP TPROXY. A generated
// xkeen chain and UDP TPROXY do not prove a normal TCP client can reach it.
// The diagnostic intentionally reports only fixed states and never rule text.
func captureDoctorNATState(raw string) (string, string) {
 redirect := "tcp-redirect-absent"
 hook := "tcp-prerouting-hook-absent"
 invalidOnly := false
 for _, rawLine := range strings.Split(raw, "\n") {
  line := strings.ToLower(strings.TrimSpace(rawLine))
  if strings.HasPrefix(line, "-a xkeen ") && strings.Contains(line, "-p tcp ") &&
   strings.Contains(line, "-j redirect") {
   redirect = "tcp-redirect-present"
  }
  if !strings.HasPrefix(line, "-a prerouting ") ||
   !strings.Contains(line, "-j xkeen") {
   continue
  }
  if strings.Contains(line, "-p udp ") {
   continue
  }
  // The Xkeen INVALID+DSCP emergency path is NOT a general TCP intercept.
  if strings.Contains(line, "--ctstate invalid") ||
   strings.Contains(line, "--dscp ") {
   invalidOnly = true
   continue
  }
  hook = "tcp-prerouting-hook-present"
 }
 if hook == "tcp-prerouting-hook-absent" && invalidOnly {
  hook = "tcp-prerouting-invalid-only"
 }
 return redirect, hook
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
  Netfilter:"unknown", TCPRedirect:"unknown", TCPPrerouting:"unknown", PolicyRule:"unknown", LocalRoute:"unknown", Hook:hook, Reason:"kernel-capture-unverified"}
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
 if rules, ok := read(ctx, "iptables-save", "-t", "nat"); ok {
  result.TCPRedirect, result.TCPPrerouting = captureDoctorNATState(rules)
 }
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
 case result.TCPRedirect == "tcp-redirect-absent":
  result.State = "tcp-redirect-missing"
  result.Reason = "no-kernel-tcp-redirect"
 case result.TCPRedirect == "tcp-redirect-present" &&
  result.TCPPrerouting != "tcp-prerouting-hook-present":
  result.State = "tcp-hook-missing"
  result.Reason = "no-normal-tcp-prerouting-hook"
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
