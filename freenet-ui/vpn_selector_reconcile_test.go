package main

import (
 "net/http/httptest"
 "strings"
 "testing"
)

func TestVPNSelectorReconcileContract(t *testing.T) {
 ux := string(vpnSelectorReconcileAsset)
 if ux == "" { t.Fatal("embedded VPN selector reconcile asset is empty") }
 for _, required := range []string{
  "freenet:manual-vpn-verified",
  "finishVerifiedManualSwitch",
  "proof.profile_id",
  "proof.endpoint",
  "closeProfileMenu",
  "previousUpdateStatusViews.apply",
 } {
  if !strings.Contains(ux,required) { t.Fatalf("VPN selector contract missing %q", required) }
 }
 for _, forbidden := range []string{
  "/api/network-profile/apply", "method: 'POST'", "setTimeout(",
  "reconcilePendingManualSwitch", "status.endpoint === expectedEndpoint",
 } {
  if strings.Contains(ux,forbidden) { t.Fatalf("untrusted status reconciles VPN: %q",forbidden) }
 }
}

func TestCanonicalIndexEmbedsVPNSelectorReconcile(t *testing.T) {
 a := &app{}
 req := httptest.NewRequest("GET", "http://router.local/", nil)
 rr := httptest.NewRecorder()
 a.handleIndex(rr, req)
 if rr.Code != 200 { t.Fatalf("base index status=%d",rr.Code) }
 body, err := canonicalizeControlCenterIndex(rr.Body.String())
 if err != nil { t.Fatal(err) }
 for _, required := range []string{
  `<script id="freenetVPNSelectorReconcile">`,
  "finishVerifiedManualSwitch",
  "freenet:manual-vpn-verified",
 } {
  if !strings.Contains(body,required) { t.Fatalf("canonical index missing %q",required) }
 }
 if strings.Contains(body,`/vpn-selector-reconcile.js?v=`) { t.Fatal("reconcile must be embedded") }
}
