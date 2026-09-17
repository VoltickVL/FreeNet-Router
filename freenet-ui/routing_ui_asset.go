package main

import (
	_ "embed"
	"strings"
)

// routingApplyUIAsset is injected into the canonical Control Center shell after
// Routing v2. It owns only the user-facing apply lifecycle and compatibility
// guard between the canonical `routing` route and the legacy `network` anchor.
//go:embed web/routing-apply-ui.js
var routingApplyUIAsset []byte

// canonicalRoutingV2Script fixes two confirmed production delivery defects in
// the historical Routing v2 asset without changing its Rule Builder semantics:
//   1. the accepted shell can rename `network` to `routing` before Routing v2 mounts;
//   2. a literal Markdown-style backtick pair around 04_outbounds.json lives
//      inside a JavaScript template literal and makes the raw asset unparsable.
//
// The canonical shell uses this sanitized inline source. The transformation is
// intentionally narrow and covered by Go + real Chromium acceptance tests.
func canonicalRoutingV2Script() string {
	source := string(routingV2Asset)
	source = strings.ReplaceAll(source, "`04_outbounds.json`", "04_outbounds.json")
	source = strings.Replace(source,
		`[data-page-view="network"].fn-routing-v2>.card.fn-routing-v2-legacy`,
		`:is([data-page-view="routing"],[data-page-view="network"]).fn-routing-v2>.card.fn-routing-v2-legacy`, 1)
	source = strings.Replace(source,
		`const page = qs('[data-page-view="network"]');`,
		`const page = qs('[data-page-view="routing"],[data-page-view="network"]');`, 1)
	return source
}
