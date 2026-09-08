package main

import "encoding/json"

// Provider-specific transfer diagnostics are kept internal. The public API
// exposes only normalized rejection reasons, so the browser cannot label a
// Speedtest failure with legacy provider-specific text from an older UI bundle.
func (c bestServerQualityCandidate) MarshalJSON() ([]byte, error) {
	type candidateAlias bestServerQualityCandidate
	clean := candidateAlias(c)
	clean.DownloadIssue = ""
	clean.MediaIssue = ""
	return json.Marshal(clean)
}
