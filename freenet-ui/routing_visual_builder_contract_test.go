package main

import (
	"os"
	"strings"
	"testing"
)

func TestVisualRoutingBuilderContract(t *testing.T) {
	data, err := os.ReadFile("web/routing-v2.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{
		"function renderLiveRules()",
		"function presentLiveRule(rule, index)",
		"function parseLiveSelector(raw, family)",
		"function groupSelectors(selectors)",
		"function renderPolicyRule(container, item)",
		"function renderSystemRule(container, item)",
		"Маршрутизация сейчас",
		"rv2-policy-summary",
		"rv2DirectRules",
		"rv2VPNRules",
		"rv2BlockRules",
		"rv2SystemToggle",
		"Системные правила",
		"Номер # — реальный приоритет правила",
		"+${item.selectors.length - limit} ещё",
		"rv2DraftCard",
		"rv2-draft-card",
		"Добавить правило",
		"Сайты / GeoSite",
		"IP / GeoIP",
		"Найти в GeoData",
		"rv2ValidateRules",
		"rv2ApplyRules",
		"validateRulesCandidate",
		"clone(base.routing.rules)",
		"managed.concat(existing)",
		"ext:([^:]+):(.+)",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("Routing UX v3 missing %q", want)
		}
	}
	for _, unwanted := range []string{
		"Как работает маршрутизация",
		"Системное правило",
		"Технические детали",
		"rv2LiveRuleList",
		"rv2-live-rule",
		"rv2-selector-chip",
		"font-size:9.5px",
		"font-size:8.5px",
	} {
		if strings.Contains(js, unwanted) {
			t.Fatalf("Routing UX v3 still contains obsolete dump-style UI %q", unwanted)
		}
	}
}

func TestVisualRoutingBuilderUsesSharedTransactionalApply(t *testing.T) {
	data, err := os.ReadFile("web/routing-apply-ui.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{
		"applyButtons()",
		"#rv2ApplyConfig",
		"#rv2ApplyRules",
		"freenet:routing-draft-changed",
		"/api/routing/validate",
		"/api/routing/apply",
		"snapshot",
		"rollback",
		"STOP",
		"Проверка Xray пройдена",
		"Применить проверенные правила",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("shared visual routing apply contract missing %q", want)
		}
	}
	if strings.Count(js, "originalFetch('/api/routing/apply'") != 1 {
		t.Fatal("routing mutation must have one shared apply path")
	}
}
