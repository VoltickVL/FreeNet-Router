package main

import (
	"strings"
	"testing"
)

func TestParseNetworkPlanMarksMatchingSplitActive(t *testing.T) {
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
		"EXPECTED_DELTA=route dns-vless through vless-reality",
		"EXPECTED_NO_DELTA=no XKeen DNS interception",
		"MUTATION=NONE",
	}, "\n")

	plan, err := parseNetworkPlan(out)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Active {
		t.Fatalf("matching Split runtime must be active: %+v", plan)
	}
	if plan.DNSRoutingMode != "split" {
		t.Fatalf("dns routing mode not exposed: %+v", plan)
	}
}

func TestParseNetworkPlanDoesNotMarkMismatchedRuntimeActive(t *testing.T) {
	out := strings.Join([]string{
		"ISP_ID=auto",
		"DNS_MODE=xkeen",
		"EFFECTIVE_DNS_MODE=xkeen",
		"SUPPORTED=yes",
		"REASON=Split DNS через XKeen/Xray выбран явно",
		"PROXY_DNS=off",
		"PORT53_OWNER=xray",
		"XRAY_GID=11111",
		"DNS_ROUTING_MODE=standard",
		"DNS_OUT=yes",
		"VLESS_PROFILE=yes",
		"EXPECTED_DELTA=route dns-vless through vless-reality",
		"MUTATION=NONE",
	}, "\n")

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
		"Профиль уже применён",
		"Сначала сохраните и проверьте",
		"Применение недоступно",
		"Повторить проверку",
		"Проверить ещё раз",
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
