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

const routingV2RowBoardPolishScript = `
;(() => {
  'use strict';
  const styleId = 'routingV2RowPolishStyles';
  function installRoutingRowPolish() {
    if (document.getElementById(styleId)) return;
    const style = document.createElement('style');
    style.id = styleId;
    style.textContent = [
      ':is([data-page-view="routing"],[data-page-view="network"]) .rv4-board-grid{grid-template-columns:1fr!important;gap:12px!important;align-items:stretch!important}',
      ':is([data-page-view="routing"],[data-page-view="network"]) .rv4-board{display:grid!important;grid-template-columns:minmax(250px,320px) minmax(0,1fr)!important;border-left:4px solid var(--accent)!important;overflow:visible!important}',
      ':is([data-page-view="routing"],[data-page-view="network"]) .rv4-board-head{min-height:100%;border-bottom:0!important;border-right:1px solid #203650!important;align-content:flex-start!important}',
      ':is([data-page-view="routing"],[data-page-view="network"]) .rv4-board-title{min-width:0!important;max-width:none!important}',
      ':is([data-page-view="routing"],[data-page-view="network"]) .rv4-board-title-line strong{white-space:normal!important;overflow:visible!important;text-overflow:clip!important}',
      ':is([data-page-view="routing"],[data-page-view="network"]) .rv4-board-head-actions{width:100%!important;justify-content:flex-start!important;margin-top:8px!important}',
      ':is([data-page-view="routing"],[data-page-view="network"]) .rv4-board-count{min-width:36px!important}',
      ':is([data-page-view="routing"],[data-page-view="network"]) .rv4-board-add{white-space:nowrap!important}',
      ':is([data-page-view="routing"],[data-page-view="network"]) .rv4-board-body{padding:12px 15px!important;min-height:unset!important}',
      ':is([data-page-view="routing"],[data-page-view="network"]) .rv4-empty{min-height:72px!important;text-align:left!important;place-items:center start!important}',
      '@media(max-width:760px){:is([data-page-view="routing"],[data-page-view="network"]) .rv4-board{grid-template-columns:1fr!important}:is([data-page-view="routing"],[data-page-view="network"]) .rv4-board-head{border-right:0!important;border-bottom:1px solid #203650!important}:is([data-page-view="routing"],[data-page-view="network"]) .rv4-board-head-actions{width:auto!important}}'
    ].join('\n');
    document.head.appendChild(style);
  }
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', installRoutingRowPolish, {once:true});
  else installRoutingRowPolish();
})();
`

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
	return source + "\n" + routingV2RowBoardPolishScript
}
