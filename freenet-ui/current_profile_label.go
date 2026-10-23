package main

import (
 "io"
 "os"
 "regexp/syntax"
 "strings"
)

// Exact provider apply stores an escaped literal profile name in the active
// filter. Read only that literal, never a broad group regex or subscription.
func currentExactProfileLabel(filterPath string) string {
 f, err := os.Open(filterPath)
 if err != nil { return "" }
 defer f.Close()
 data, err := io.ReadAll(io.LimitReader(f, 2049))
 if err != nil || len(data) > 2048 { return "" }
 re, err := syntax.Parse(strings.TrimSpace(string(data)), syntax.Perl)
 if err != nil || re.Op != syntax.OpLiteral || re.Flags&syntax.FoldCase != 0 { return "" }
 return sanitizeProfileName(string(re.Rune))
}


// currentExactProfileLocation describes only a literal, installed Extra
// profile. Never infer an active country from an endpoint or a catalog row:
// several logical profiles may intentionally share the same server address.
func currentExactProfileLocation(label string) (code, country, city string, ok bool) {
	if !isBaseExtraProfileName(label) {
		return "", "", "", false
	}
	parts := strings.Split(profileDisplayName(label), ",")
	if len(parts) < 3 || !strings.EqualFold(strings.TrimSpace(parts[len(parts)-1]), "Extra") {
		return "", "", "", false
	}
	country = strings.TrimSpace(parts[len(parts)-2])
	city = strings.TrimSpace(strings.Join(parts[:len(parts)-2], ","))
	if country == "" || city == "" {
		return "", "", "", false
	}
	return bestServerCountryCodeFromLabel(label), country, city, true
}
