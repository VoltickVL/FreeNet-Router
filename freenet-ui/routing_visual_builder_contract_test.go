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
		"rv2LiveRuleList",
		"Сейчас действует",
		"Как работает маршрутизация",
		"Добавить правило",
		"Изменения перед применением",
		"Сайты и GeoSite",
		"IP и GeoIP",
		"Найти группу в GeoData",
		"rv2ValidateRules",
		"rv2ApplyRules",
		"validateRulesCandidate",
		"clone(base.routing.rules)",
		"managed.concat(existing)",
		"только просмотр",
		"ext:([^:]+):(.+)",
		"document.createTextNode(String(selector.value))",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("visual routing builder missing %q", want)
		}
	}
	if strings.Contains(js, "chip.innerHTML") {
		t.Fatal("live routing selector must not interpolate config values into innerHTML")
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
