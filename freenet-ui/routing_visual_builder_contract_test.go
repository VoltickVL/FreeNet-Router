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
		"function aggregateSelectors(items)",
		"function renderActionBoard(action, items)",
		"function openInlineComposer(action, preserve = false)",
		"function closeInlineComposer(clear = true)",
		"rv4-board-grid",
		"rv4-board direct",
		"rv4-board vpn",
		"rv4-board block",
		`data-add-action="DIRECT"`,
		`data-add-action="VPN"`,
		`data-add-action="BLOCK"`,
		"rv2DirectContent",
		"rv2VPNContent",
		"rv2BlockContent",
		"rv2InlineComposer",
		"rv2ComposerTitle",
		"rv2SystemToggle",
		"Системные правила",
		"Черновик изменений",
		"rv2DraftCard",
		"rv2ValidateRules",
		"rv2ApplyRules",
		"validateRulesCandidate",
		"clone(base.routing.rules)",
		"managed.concat(existing)",
		"ext:([^:]+):(.+)",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("Routing UX v4 missing %q", want)
		}
	}
	for _, unwanted := range []string{
		"rv2-policy-summary",
		"rv2SummaryRules",
		"rv2DirectRules",
		"rv2VPNRules",
		"rv2BlockRules",
		"rv2-policy-rule",
		"rv2-policy-order",
		"Номер # — реальный приоритет правила",
		"rv2-add-card",
		"rv2-family",
		`data-action="DIRECT"`,
		"font-size:9.5px",
		"font-size:8.5px",
	} {
		if strings.Contains(js, unwanted) {
			t.Fatalf("Routing UX v4 still contains obsolete table/global-builder UI %q", unwanted)
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
