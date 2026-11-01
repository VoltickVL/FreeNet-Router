package main

import (
	"os/exec"
	"strings"
	"testing"
)

func nativeDNSRepairUISource(t *testing.T) string {
	t.Helper()
	data, err := webFS.ReadFile("web/settings-dns-ui.js")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestNativeDNSRepairUIUsesOnlyGuardedProductAPI(t *testing.T) {
	src := nativeDNSRepairUISource(t)
	for _, required := range []string{
		"function nativeDNSPlanReady(plan)",
		"plan.success === true",
		"plan.supported === true",
		"plan.active === false",
		"plan.dns_mode === 'firmware'",
		"plan.active_dns_mode === 'firmware'",
		"plan.native_dns_provider === plan.active_native_dns_provider",
		"plan.dns_routing_mode === 'native-resolver-selection-replace'",
		"plan.native_filter_engine_confirm_required === false",
		"plan.port53_owner === 'ndnproxy'",
		"plan.mutation === 'NONE'",
		"plan.expected_no_delta.includes('no VPN credential rewrite')",
		"state.dirty",
		"state.applying",
		"loadNativeDNSRepairPlan()",
	} {
		if !strings.Contains(src, required) {
			t.Fatalf("missing native DNS safety guard: %s", required)
		}
	}
	start := strings.Index(src, "  async function applyNativeDNSRepair() {")
	end := strings.Index(src, "\n  function renderRepairAction()")
	if start < 0 || end < start {
		t.Fatal("native DNS repair transaction not found")
	}
	code := src[start:end]
	last := 0
	for _, step := range []string{
		"state.applying = true;",
		"fresh = await fetchJSON('/api/network-profile/plan'",
		"fresh.expected_delta !== state.nativePlan.expected_delta",
		"window.confirm(confirmation)",
		"fetchJSON('/api/network-profile/apply'",
		"body:JSON.stringify({operation:'network',dns_mode:'firmware',confirm:true})",
		"const after = await fetchJSON('/api/network-profile/plan'",
		"after.active !== true",
		"after.dns_routing_mode !== 'native'",
	} {
		n := strings.Index(code[last:], step)
		if n < 0 {
			t.Fatalf("guarded DNS steps not in required order: %q", step)
		}
		last += n + len(step)
	}
	if strings.Contains(code, "exec sh") || strings.Contains(code, "shell:") {
		t.Fatal("native DNS repair UI must not expose raw shell")
	}
}

func TestNativeDNSRepairUIRejectsUnknownOrContradictoryPlans(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable; static contract still checked")
	}
	src := nativeDNSRepairUISource(t)
	start := strings.Index(src, "  function nativeDNSPlanReady(plan) {")
	if start < 0 {
		t.Fatal("native DNS plan predicate missing")
	}
	end := strings.Index(src[start:], "\n  async function loadNativeDNSRepairPlan()")
	if end < 0 {
		t.Fatal("native DNS plan predicate end missing")
	}
	predicate := src[start : start+end]
	tests := []string{
		"const good = {success:true,supported:true,active:false,dns_mode:'firmware',active_dns_mode:'firmware',native_dns_provider:'yandex-basic',active_native_dns_provider:'yandex-basic',dns_routing_mode:'native-resolver-selection-replace',native_filter_engine_confirm_required:false,port53_owner:'ndnproxy',proxy_dns:'off',mutation:'NONE',expected_delta:'replace active System resolver selection with Yandex Basic',expected_no_delta:'no VPN credential rewrite'};",
		"if (!nativeDNSPlanReady(good)) throw new Error('valid coherent native DNS drift rejected');",
		"const invalid = [",
		"  {success:false},{supported:false},{active:true},",
		"  {dns_mode:'xkeen'},{active_dns_mode:'xkeen'},",
		"  {native_dns_provider:'router-current'},{active_native_dns_provider:'router-current'},",
		"  {dns_routing_mode:'unknown'},{native_filter_engine_confirm_required:true},",
		"  {port53_owner:'xray'},{proxy_dns:'on'},{mutation:'APPLY'},",
		"  {expected_delta:'no changes'},{expected_no_delta:'unsafe rewrite'}",
		"];",
		"for (const mutation of invalid) {",
		"  if (nativeDNSPlanReady({...good, ...mutation})) throw new Error('unsafe native DNS state accepted: ' + JSON.stringify(mutation));",
		"}",
		"if (nativeDNSPlanReady(null)) throw new Error('null plan accepted');",
	}
	cmd := exec.Command(node, "-e", predicate+"\n"+strings.Join(tests, "\n"))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native DNS UI JS guard test failed: %v\n%s", err, output)
	}
}
