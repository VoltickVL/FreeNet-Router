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
