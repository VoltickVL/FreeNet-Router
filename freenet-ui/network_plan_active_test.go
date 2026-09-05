package main

import (
	"strings"
	"testing"
)

func matchingSplitPlanOutput() string {
	return strings.Join([]string{
		"ISP_ID=auto",
		"DNS_MODE=xkeen",
		"EFFECTIVE_DNS_MODE=xkeen",
		"SUPPORTED=yes",
		"REASON=Split DNS через XKeen/Xray выбран явно",
		"PROXY_DNS=off",
		"NDM_DNS_OVERRIDE=on",
		"NDM_FILTER_ENGINE=opkg",
		"NDM_DNS_INTERCEPT=off",
		"NDM_DNS_ASSIGNMENTS=none",
		"PORT53_OWNER=xray",
		"XRAY_DNS_INBOUND_COUNT=1",
		"XRAY_RUNNING=yes",
		"XRAY_GID=11111",
		"DNS_ROUTING_MODE=split",
		"DNS_OUT=yes",
		"VLESS_PROFILE=yes",
		"EXPECTED_DELTA=route dns-vless through vless-reality",
		"EXPECTED_NO_DELTA=no XKeen DNS interception",
		"MUTATION=NONE",
	}, "\n")
}

func TestParseNetworkPlanMarksMatchingSplitActive(t *testing.T) {
	plan, err := parseNetworkPlan(matchingSplitPlanOutput())
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Active {
		t.Fatalf("matching Split runtime must be active: %+v", plan)
	}
	if plan.NDMDNSOverride != "on" || plan.NDMFilterEngine != "opkg" || plan.Port53Owner != "xray" || plan.XrayDNSInboundCount != "1" || !plan.XrayRunning {
		t.Fatalf("authoritative runtime facts not exposed: %+v", plan)
	}
}

func TestParseNetworkPlanSplitActiveFailsClosedOnRuntimeMismatch(t *testing.T) {
	tests := []struct {
		name string
		old  string
		new  string
		want string
	}{
		{name: "dns override off", old: "NDM_DNS_OVERRIDE=on", new: "NDM_DNS_OVERRIDE=off", want: "dns-override=off"},
		{name: "native filter engine", old: "NDM_FILTER_ENGINE=opkg", new: "NDM_FILTER_ENGINE=public", want: "filter-engine=public"},
		{name: "native intercept", old: "NDM_DNS_INTERCEPT=off", new: "NDM_DNS_INTERCEPT=on", want: "native-intercept=on"},
		{name: "native assignments", old: "NDM_DNS_ASSIGNMENTS=none", new: "NDM_DNS_ASSIGNMENTS=present", want: "native-assignments=present"},
		{name: "wrong port owner", old: "PORT53_OWNER=xray", new: "PORT53_OWNER=ndnproxy", want: "owner:53=ndnproxy"},
		{name: "missing dns inbound", old: "XRAY_DNS_INBOUND_COUNT=1", new: "XRAY_DNS_INBOUND_COUNT=0", want: "xray-dns-inbound=0"},
		{name: "xray offline", old: "XRAY_RUNNING=yes", new: "XRAY_RUNNING=no", want: "xray-running=no"},
		{name: "wrong gid", old: "XRAY_GID=11111", new: "XRAY_GID=1000", want: "xray-gid=1000"},
		{name: "wrong routing", old: "DNS_ROUTING_MODE=split", new: "DNS_ROUTING_MODE=standard", want: "dns-routing=standard"},
		{name: "dns out missing", old: "DNS_OUT=yes", new: "DNS_OUT=no", want: "dns-out отсутствует"},
		{name: "vless missing", old: "VLESS_PROFILE=yes", new: "VLESS_PROFILE=no", want: "vless-reality отсутствует"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := strings.Replace(matchingSplitPlanOutput(), tt.old, tt.new, 1)
			plan, err := parseNetworkPlan(out)
			if err != nil {
				t.Fatal(err)
			}
			if plan.Active {
				t.Fatalf("runtime mismatch must never be marked active: %+v", plan)
			}
			if !strings.Contains(plan.Reason, tt.want) {
				t.Fatalf("runtime mismatch must be visible in reason; want %q, got %q", tt.want, plan.Reason)
			}
		})
	}
}

func TestParseNetworkPlanMissingAuthoritativeFactsIsNotActive(t *testing.T) {
	out := strings.Join([]string{
		"ISP_ID=auto",
		"DNS_MODE=xkeen",
		"EFFECTIVE_DNS_MODE=xkeen",
		"SUPPORTED=yes",
		"REASON=Split DNS через XKeen/Xray выбран явно",
		"PROXY_DNS=off",
		"PORT53_OWNER=xray",
		"XRAY_GID=11111",
		"DNS_ROUTING_MODE=split",
		"DNS_OUT=yes",
		"VLESS_PROFILE=yes",
		"MUTATION=NONE",
	}, "\n")
	plan, err := parseNetworkPlan(out)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Active {
		t.Fatalf("incomplete runtime evidence must fail closed: %+v", plan)
	}
}

func TestParseNetworkPlanDoesNotMarkNativeInterceptSplitActive(t *testing.T) {
	out := strings.Replace(matchingSplitPlanOutput(), "DNS_ROUTING_MODE=split", "DNS_ROUTING_MODE=split-intercept", 1)
	plan, err := parseNetworkPlan(out)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Active {
		t.Fatalf("Split runtime with native Keenetic interception must remain repair-ready: %+v", plan)
	}
}

func TestParseNetworkPlanDoesNotMarkMismatchedRuntimeActive(t *testing.T) {
	out := strings.Replace(matchingSplitPlanOutput(), "DNS_ROUTING_MODE=split", "DNS_ROUTING_MODE=standard", 1)
	plan, err := parseNetworkPlan(out)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Active {
		t.Fatalf("mismatched routing must remain apply-ready, not active: %+v", plan)
	}
}

func TestNetworkPlanUIStateContract(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	for _, required := range []string{
		"ПРОФИЛЬ УЖЕ АКТИВЕН",
		"ТРЕБУЮТСЯ ИЗМЕНЕНИЯ",
		"ПРИМЕНЕНИЕ ЗАБЛОКИРОВАНО",
		"Проверяем сетевой профиль…",
		"Профиль активен",
		"Есть подтверждённые изменения",
		"Требует внимания",
		"Есть несохранённые изменения",
		"VPN и DNS работают",
		"VPN/DNS требуют внимания",
		"networkChecking",
		"networkPlanError",
		"lastNetworkPlan.supported&&lastNetworkPlan.active",
		"lastNetworkPlan.supported&&!lastNetworkPlan.active",
	} {
		if !strings.Contains(ui, required) {
			t.Fatalf("network UI state contract missing %q", required)
		}
	}
}

func TestNetworkPlanUIHidesCompletedActions(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	for _, required := range []string{
		"save.hidden=!networkDirty",
		"plan.hidden=networkDirty||state==='active'",
		"apply.hidden=state!=='changes'",
	} {
		if !strings.Contains(ui, required) {
			t.Fatalf("state-aware network controls missing %q", required)
		}
	}
}

func TestNetworkPlanUIKeepsBlindApplyGuard(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	for _, required := range []string{
		"if(networkDirty||!networkPlanReady||networkApplying)return",
		"networkPlanReady=!!(j.supported&&!j.active)",
		"apply.disabled=busy||state!=='changes'",
	} {
		if !strings.Contains(ui, required) {
			t.Fatalf("blind network apply guard missing %q", required)
		}
	}
}
