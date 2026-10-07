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
		"function toggleLiveSelectorRemoval(selector, action)",
		"function setBoardCollapsed(action, collapsed)",
		"function removalGroups()",
		"Черновик изменений",
		"rv2DraftCard",
		"rv2ApplyRules",
		"Применить",
		"prepareRulesCandidateForApply",
		"mergeManagedRulesSafely",
		"hasEarlierFamilyConflict",
		"freenet:xray-config-applied",
		"freenet:routing-mode-changed",
		"state.liveStale",
		"clone(base.routing.rules)",
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
		"id=\"rv2ValidateRules\"",
		"rv2SystemToggle",
		"rv2SystemList",
		"Служебные правила Xray",
		"защищены FreeNet",
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
		"applyRulesOneClick",
		"FreeNetRoutingV2.refreshAfterApply",
		"Применить",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("shared visual routing apply contract missing %q", want)
		}
	}
	if strings.Count(js, "originalFetch('/api/routing/apply'") != 1 {
		t.Fatal("routing mutation must have one shared apply path")
	}
}


func TestRoutingAndConfigStudioLiveSyncContract(t *testing.T) {
	routingData, err := os.ReadFile("web/routing-v2.js")
	if err != nil {
		t.Fatal(err)
	}
	studioData, err := os.ReadFile("web/config-studio-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	routing := string(routingData)
	studio := string(studioData)

	for _, want := range []string{
		"freenet:xray-config-applied",
		"freenet:routing-mode-changed",
		"handleExternalConfigApplied",
		"state.liveStale",
		"reconcileExternalLiveIfSafe",
	} {
		if !strings.Contains(routing, want) {
			t.Fatalf("routing live-sync contract missing %q", want)
		}
	}
	for _, want := range []string{
		"freenet:xray-config-applied",
		"freenet:routing-mode-changed",
		"handleExternalConfigApplied",
		"handleRoutingModeChanged",
		"hasDirtyDrafts",
		"state.stale",
		"Применить",
	} {
		if !strings.Contains(studio, want) {
			t.Fatalf("Config Studio live-sync contract missing %q", want)
		}
	}
}


func TestRoutingWorkspaceHasFirstClassXrayTab(t *testing.T) {
	routingData, err := os.ReadFile("web/routing-v2.js")
	if err != nil {
		t.Fatal(err)
	}
	managerData, err := os.ReadFile("web/xray-core-manager.js")
	if err != nil {
		t.Fatal(err)
	}
	routing := string(routingData)
	manager := string(managerData)

	for _, want := range []string{
		`data-mode="xray"`,
		`data-mode="rules"`,
		`data-mode="config"`,
		`id="rv2XrayPanel"`,
		`id="rv2XrayStart"`,
		`id="rv2XrayStop"`,
		`id="rv2XrayRestart"`,
		`id="rv2XrayVersions"`,
		"freenet:xray-service-changed",
		"FreeNetXrayControl",
	} {
		if !strings.Contains(routing, want) {
			t.Fatalf("embedded Xray workspace missing %q", want)
		}
	}
	for _, obsolete := range []string{
		`id="rv2XrayJournal"`,
		`id="rv2XrayRefresh"`,
	} {
		if strings.Contains(routing, obsolete) {
			t.Fatalf("embedded Xray workspace still contains redundant action %q", obsolete)
		}
	}
	for _, want := range []string{
		"window.FreeNetXrayControl",
		"action: mutateService",
		"refresh: fetchServiceSnapshot",
		"openVersions",
		"openJournal",
	} {
		if !strings.Contains(manager, want) {
			t.Fatalf("canonical Xray owner API missing %q", want)
		}
	}
}


func TestSmartGeoDataAutocompleteContract(t *testing.T) {
	routingData, err := os.ReadFile("web/routing-v2.js")
	if err != nil {
		t.Fatal(err)
	}
	studioData, err := os.ReadFile("web/config-studio-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	routing := string(routingData)
	studio := string(studioData)

	for _, want := range []string{
		"/api/geodata/suggest",
		"rv2GeoAutocomplete",
		"queueGeoAutocomplete",
		"AbortController",
		"ArrowDown",
		"ArrowUp",
		"state.selectedSource",
		"ext:${source}:${value}",
		"position:fixed",
		"z-index:12000",
		"geoAutocompleteBox",
		"document.body.appendChild(box)",
		"positionGeoAutocomplete",
		"overflow-y:auto",
		"overscroll-behavior:contain",
	} {
		if !strings.Contains(routing, want) {
			t.Fatalf("Routing Smart GeoData contract missing %q", want)
		}
	}
	for _, obsolete := range []string{
		"Найти в GeoData",
		`id="rv2GeoSearch"`,
		"function searchGeo()",
		"rv2SearchResults",
	} {
		if strings.Contains(routing, obsolete) {
			t.Fatalf("Routing Smart GeoData still contains redundant manual search surface %q", obsolete)
		}
	}
	for _, want := range []string{
		"/api/geodata/suggest",
		"csGeoAutocomplete",
		"geoEditorToken",
		"queueGeoEditorAutocomplete",
		"ext:([^:",
		"ArrowDown",
		"ArrowUp",
		"chooseGeoEditorSuggestion",
	} {
		if !strings.Contains(studio, want) {
			t.Fatalf("Config Studio Smart GeoData contract missing %q", want)
		}
	}
}
