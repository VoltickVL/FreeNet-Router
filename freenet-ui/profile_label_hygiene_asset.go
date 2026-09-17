package main

import _ "embed"

// profileLabelHygieneAsset normalizes only user-facing Settings profile labels.
// It never changes runtime profile identity or performs network mutations.
//go:embed web/profile-label-hygiene.js
var profileLabelHygieneAsset []byte
