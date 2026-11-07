package main

// A clean FreeNet 0.9 install does NOT migrate FreeNet 0.8. The existing
// XKeen/Xray policy and subscription URL are existing inputs, left untouched.
// Legacy 0.8 may have embedded its own Split DNS into standard Xray JSON;
// file existence does not mean the file is safe to reuse as 0.9 policy.
// This API never reads their contents, even when they contain credentials.
type nativePreservedSources struct {
 State string `json:"state"`
 Runtime string `json:"runtime"`
 ConfigPolicy string `json:"config_policy"`
 DNS string `json:"dns"`
 Outbounds string `json:"outbounds"`
 Routing string `json:"routing"`
 Subscription string `json:"subscription"`
 XKeenConfig string `json:"xkeen_config"`
 GeoData string `json:"geodata"`
}

func preservedNativeSources(root string) nativePreservedSources {
 out:=nativePreservedSources{
  Runtime:"NOT_VERIFIED",
  ConfigPolicy:"REVIEW_LEGACY_SPLIT_DNS_DEPENDENCIES_BEFORE_APPLY",
  DNS:observedPath(root,"etc/xray/configs/02_dns.json",false),
  Outbounds:observedPath(root,"etc/xray/configs/04_outbounds.json",false),
  Routing:observedPath(root,"etc/xray/configs/05_routing.json",false),
  Subscription:observedPath(root,"etc/xray/blanc_subscription.url",false),
  XKeenConfig:observedPath(root,"etc/xkeen/xkeen.json",false),
  GeoData:observedPath(root,"etc/xray/dat",true),
 }
 // No source bytes, URL, credentials, Xray rules or DNS entries ever leave
 // this read-only API. A missing optional source is not a fabricated error.
 out.State="NATIVE_CONFIG_REVIEW_REQUIRED"
 for _,state:=range []string{out.DNS,out.Outbounds,out.Routing,out.Subscription,out.XKeenConfig,out.GeoData}{
  if state=="UNKNOWN" {out.State="STOP";return out}
 }
 if out.DNS!="PRESENT"||out.Outbounds!="PRESENT"||out.Routing!="PRESENT"{
  out.State="NATIVE_POLICY_INCOMPLETE"
 }
 // Files may contain FreeNet 0.8-managed dns-direct/dns-vless/dns-out
 // transport and DNS-only routing references. Never classify files as
 // migration-ready or reuse them automatically merely because they exist.
 // This means only files are present, not that they are valid or working.
 return out
}
