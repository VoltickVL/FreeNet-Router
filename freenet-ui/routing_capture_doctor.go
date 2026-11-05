package main

// A passive kernel interception inventory. Hook files and a valid Xray
// configuration are not evidence that LAN packets are being captured.
import (
 "context"
 "net/http"
 "net/netip"
 "os"
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
 LANExclude string `json:"lan_exclude"`
 LANExcludeReason string `json:"lan_exclude_reason"`
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

// The normal XKeen TCP hook is commonly guarded by "!" "--ctstate" "INVALID":
// it accepts *non-INVALID* connections. Do not confuse the negation with the
// emergency INVALID-only/DSCP-only hooks. Unknown restricted state matches
// remain conservative; this is never a live packet/stream quality assertion.
func captureDoctorSpecialOnlyTCPRule(fields []string) bool {
	for i, field := range fields {
		switch field {
		case "--dscp", "--dscp-class":
			return true
		case "--ctstate":
			if i+1 >= len(fields) {
				return true
			}
			value := fields[i+1]
			negated := i > 0 && fields[i-1] == "!"
			if negated && value == "invalid" {
				continue // normal TCP: ! INVALID
			}
			if !negated && (value == "new" || value == "new,established" || value == "established,new") {
				continue
			}
			return true
		}
	}
	return false
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
  // A negated INVALID state is the normal XKeen hook, not an
  // INVALID-only emergency path. DSCP-constrained hooks are still special.
  if captureDoctorSpecialOnlyTCPRule(strings.Fields(line)) {
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


 // The two sources are independently sampled: a saved file does not prove
 // an ipset membership, and a running ipset may outlive a config edit.
 // All returned states are bounded enums; never return IPs, CIDRs or rules.
func captureDoctorLANRoutePrefixes(raw string) []netip.Prefix {
 var result []netip.Prefix
 for _, line := range strings.Split(raw, "\n") {
  fields := strings.Fields(line)
  if len(fields)<2 || (!strings.Contains(" "+line+" "," scope link ") && !strings.Contains(" "+line+" "," proto kernel ")) { continue }
  prefix, err := netip.ParsePrefix(fields[0])
  if err!=nil || !prefix.Addr().Is4() || !prefix.Addr().IsPrivate() { continue }
  result=append(result,prefix.Masked())
 }
 return result
}

func captureDoctorExcludePrefixes(raw string, runtime bool) []netip.Prefix {
 var result []netip.Prefix
 for _, rawLine := range strings.Split(raw,"\n") {
  line:=strings.TrimSpace(strings.SplitN(rawLine,"#",2)[0])
  if line=="" { continue }
  fields:=strings.Fields(line)
  value:=line
  if runtime {
   if len(fields)<3 || fields[0]!="add" || fields[1]!="user_exclude" { continue }
   value=fields[2]
  } else if len(fields)!=1 { continue }
  if addr,err:=netip.ParseAddr(value);err==nil { if addr.Is4() { result=append(result,netip.PrefixFrom(addr,32)) };continue }
  prefix,err:=netip.ParsePrefix(value)
  if err==nil && prefix.Addr().Is4() { result=append(result,prefix.Masked()) }
 }
 return result
}

func captureDoctorExcludesLAN(connected, excluded []netip.Prefix) bool {
 for _, lan:=range connected {
  for _, item:=range excluded {
   if item.Bits()<=lan.Bits() && item.Contains(lan.Addr()) { return true }
  }
 }
 return false
}

// Missing any source is UNKNOWN, never a clean bill of health.
func captureDoctorLANExcludeState(routes string, routesOK bool, disk string, diskOK bool, active string, activeOK bool)(string,string) {
 if !routesOK { return "unverified","connected-lan-unavailable" }
 connected:=captureDoctorLANRoutePrefixes(routes)
 if len(connected)==0 { return "unverified","connected-lan-unavailable" }
 diskRisk, liveRisk:=false,false
 if diskOK { diskRisk=captureDoctorExcludesLAN(connected,captureDoctorExcludePrefixes(disk,false)) }
 if activeOK { liveRisk=captureDoctorExcludesLAN(connected,captureDoctorExcludePrefixes(active,true)) }
 switch {
 case diskOK && activeOK && diskRisk && liveRisk: return "both","broad-lan-exclusion-active-and-persistent"
 case diskOK && activeOK && diskRisk: return "disk-only","broad-lan-exclusion-persistent-only"
 case diskOK && activeOK && liveRisk: return "runtime-only","broad-lan-exclusion-active-only"
 case diskRisk: return "disk-risk-runtime-unknown","runtime-exclusions-unavailable"
 case liveRisk: return "runtime-risk-disk-unknown","persistent-exclusions-unavailable"
 case !diskOK || !activeOK: return "unverified","exclusion-source-unavailable"
 default: return "no-overlap","no-broad-connected-lan-exclusion-detected"
 }
}

func captureDoctorLANExclude(ctx context.Context, read func(context.Context,string,...string)(string,bool), readFile func(string)([]byte,error))(string,string) {
 routes,routeOK:=read(ctx,"ip","-4","route","show")
 active,activeOK:=read(ctx,"ipset","save","user_exclude")
 disk,err:=readFile("/opt/etc/xkeen/ip_exclude.lst")
 return captureDoctorLANExcludeState(routes,routeOK,string(disk),err==nil,active,activeOK)
}

func registerCaptureDoctor(mux *http.ServeMux,a *app) {
 mux.HandleFunc("GET /api/routing/capture-doctor",a.requireAuth(a.handleCaptureDoctor))
}

func (a *app) handleCaptureDoctor(w http.ResponseWriter,r *http.Request) {
 ctx,cancel:=context.WithTimeout(r.Context(),12*time.Second)
 defer cancel()
 response:=captureDoctorClassify(ctx,captureDoctorRead,xrayDiagnosticHookState("/opt/etc/ndm/netfilter.d/proxy.sh"))
 response.LANExclude,response.LANExcludeReason=captureDoctorLANExclude(ctx,captureDoctorRead,os.ReadFile)
 writeJSON(w,http.StatusOK,response)
}
