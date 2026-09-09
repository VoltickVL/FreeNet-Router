package main

import "encoding/json"

// Provider-specific transfer diagnostics are kept internal. The public API
// exposes only normalized rejection reasons. Profile identity remains raw on
// the backend, while presentation gets a deterministic country code and a
// label without emoji/ISO prefixes so Windows and macOS render the same UI.
func (c bestServerQualityCandidate) MarshalJSON() ([]byte, error) {
	type candidateAlias bestServerQualityCandidate
	clean := candidateAlias(c)
	clean.DownloadIssue = ""
	clean.MediaIssue = ""
	if code := profileCountryCode(clean.Name); code != "" {
		clean.CountryCode = code
	}
	clean.Name = profileDisplayName(clean.Name)
	return json.Marshal(clean)
}
