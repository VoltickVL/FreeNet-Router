package main

import _ "embed"

// routingApplyUIAsset is injected into the canonical Control Center shell after
// Routing v2. It owns only the user-facing apply lifecycle and the compatibility
// guard between the canonical `routing` route and the legacy `network` anchor.
//go:embed web/routing-apply-ui.js
var routingApplyUIAsset []byte
