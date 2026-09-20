package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCanonicalRoutingV2ScriptFixesProductionDefects(t *testing.T) {
	raw := string(routingV2Asset)
	canonical := canonicalRoutingV2Script()
	// Historical builds contained a Markdown-style backtick pair around
	// 04_outbounds.json inside a JS template literal. The sanitizer remains
	// backward-compatible, but current product copy no longer has to preserve
	// that defect as a test fixture.
	if strings.Contains(raw, "`04_outbounds.json`") && strings.Contains(canonical, "`04_outbounds.json`") {
		t.Fatal("canonical Routing v2 still contains template-literal breaking backticks")
	}
	for _, required := range []string{
		`[data-page-view="routing"]`,
		`[data-page-view="network"]`,
		`const page = qs('[data-page-view="routing"],[data-page-view="network"]');`,
		`:is([data-page-view="routing"],[data-page-view="network"]).fn-routing-v2>.card.fn-routing-v2-legacy`,
		`routingV2Workspace`,
		`Config Studio`,
	} {
		if !strings.Contains(canonical, required) {
			t.Fatalf("canonical Routing v2 source missing %q", required)
		}
	}
}

func TestRoutingApplyUIClosesCanonicalRouteRace(t *testing.T) {
	automation, err := automationWebFS.ReadFile("web/automation.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(automation), `routingPage.dataset.pageView = 'routing'`) {
		t.Fatal("accepted shell race precondition changed: automation no longer renames network to routing")
	}

	js := string(routingApplyUIAsset)
	for _, required := range []string{
		`[data-page-view="routing"]`,
		`[data-page-view="network"]`,
		`routingV2Workspace`,
		`policyBuilderPreview`,
		`showMountFailure`,
		`Live routing не изменён. Применение заблокировано.`,
	} {
		if !strings.Contains(js, required) {
			t.Fatalf("routing canonical compatibility layer missing %q", required)
		}
	}
	if strings.Contains(js, `/routing-v2.js?canonical-retry=1`) {
		t.Fatal("canonical apply layer must not retry the historical raw Routing v2 asset")
	}
}

func TestRoutingApplyUIRequiresValidatedCandidateAndFailClosedStop(t *testing.T) {
	js := string(routingApplyUIAsset)
	for _, required := range []string{
		`/api/routing/config`,
		`/api/routing/validate`,
		`/api/routing/apply`,
		`validatedCandidate`,
		`xray_valid === true`,
		`ROLLBACK FAILED/UNKNOWN = STOP`,
		`rollback === 'FAILED'`,
		`rollback === 'UNKNOWN'`,
		`mutation === 'STOP'`,
		`не повторяйте mutation`,
	} {
		if !strings.Contains(js, required) {
			t.Fatalf("routing apply lifecycle missing %q", required)
		}
	}
	for _, forbidden := range []string{"vless://", "privateKey", "shortId", "subscription_url"} {
		if strings.Contains(js, forbidden) {
			t.Fatalf("routing UI must not expose secret-bearing token %q", forbidden)
		}
	}
}

func TestCanonicalIndexEmbedsRoutingApplyUI(t *testing.T) {
	a := &app{}
	req := httptest.NewRequest("GET", "http://router.local/", nil)
	rr := httptest.NewRecorder()
	a.handleIndex(rr, req)
	if rr.Code != 200 {
		t.Fatalf("base index status=%d", rr.Code)
	}
	html, err := canonicalizeControlCenterIndex(rr.Body.String())
	if err != nil {
		t.Fatal(err)
	}
	routingAt := strings.Index(html, `<script id="freenetRoutingV2">`)
	applyAt := strings.Index(html, `<script id="freenetRoutingApplyUI">`)
	releaseAt := strings.Index(html, `id="freenetCanonicalBootRelease"`)
	if routingAt < 0 || applyAt < 0 || releaseAt < 0 {
		t.Fatalf("canonical routing scripts missing: routing=%d apply=%d release=%d", routingAt, applyAt, releaseAt)
	}
	if !(routingAt < applyAt && applyAt < releaseAt) {
		t.Fatalf("routing apply compatibility must load after Routing v2 and before boot release: routing=%d apply=%d release=%d", routingAt, applyAt, releaseAt)
	}
	if strings.Contains(html, `<script src="/routing-v2.js"></script>`) {
		t.Fatal("canonical shell must not depend on the historical raw Routing v2 asset")
	}
}
