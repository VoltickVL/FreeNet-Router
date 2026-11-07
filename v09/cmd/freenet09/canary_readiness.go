package main

// This is a deliberately read-only HOME-first release preflight.
// It does not inspect private Xray credentials, change any service or decide
// whether an end-user VPN/DNS/HTTPS route works.
import (
 "context"
)

type nativeCanaryReadiness struct {
 State string `json:"state"`
 Reason string `json:"reason"`
 Runtime string `json:"runtime"`
 XKeen string `json:"xkeen"`
 Xray string `json:"xray"`
 Legacy string `json:"legacy"`
}

func canaryReadiness(ctx context.Context, root, procRoot string) nativeCanaryReadiness {
 out:=nativeCanaryReadiness{
  State:"STOP",Reason:"NATIVE_STATE_UNKNOWN",Runtime:"NOT_VERIFIED",
  XKeen:"UNKNOWN",Xray:"UNKNOWN",Legacy:"UNKNOWN",
 }
 xkeen:=observedPath(root,"sbin/xkeen",false)
 xray:=observedPath(root,"sbin/xray",false)
 configs:=observedPath(root,"etc/xray/configs",true)
 init:=observedPath(root,"etc/init.d/S05xkeen",false)
 legacyBin:=observedPath(root,"sbin/freenet-ui",false)
 legacyInit:=observedPath(root,"etc/init.d/S99freenet-ui",false)
 // Even the classification of "missing" is unsafe on unknown/symlink paths.
 for _,state:=range []string{xkeen,xray,configs,init,legacyBin,legacyInit}{
  if state=="UNKNOWN" {return out}
 }
 if legacyBin!=legacyInit {out.Reason="PARTIAL_LEGACY_FREENET";return out}
 if legacyBin=="PRESENT" {out.Legacy="PRESENT"}else{out.Legacy="ABSENT"}
 if xkeen!="PRESENT" || xray!="PRESENT" || configs!="PRESENT" || init!="PRESENT"{
  out.Reason="NATIVE_STACK_INCOMPLETE";return out
 }
 vers:=detectNativeVersions(ctx,root)
 if vers.XKeen.State!="VERSION_DETECTED" || vers.Xray.State!="VERSION_DETECTED"{
  out.Reason="NATIVE_VERSION_UNKNOWN";return out
 }
 out.XKeen=vers.XKeen.Version
 out.Xray=vers.Xray.Version
 // Release baseline is the installed XKeen 2.1 Stable. Do not upgrade
 // the upstream core to 2.2 Beta or accept 2.2-only operations by guessing.
 if out.XKeen!="2.1" {
  out.Reason="XKEEN_21_RELEASE_TARGET_REQUIRED"
  return out
 }
 if out.Legacy=="PRESENT" {
  out.State="STAGING_ONLY"
  out.Reason="LEGACY_08_CUTOVER_BLOCKED"
  return out
 }
 // A running daemon isn't evidence of egress or ability to restore policy.
 if xrayProcessEvidence(procRoot)!="PROCESS_DETECTED" {
  out.Reason="XRAY_PROCESS_NOT_VERIFIED"
  return out
 }
 out.State="CORE_PRECHECK_ONLY"
 out.Reason="BROWSER_SETUP_CUTOVER_AND_EGRESS_NOT_VERIFIED"
 return out
}
