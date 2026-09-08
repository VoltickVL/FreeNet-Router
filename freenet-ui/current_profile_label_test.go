package main

import (
 "os"
 "path/filepath"
 "regexp"
 "strings"
 "testing"
)

func TestCurrentExactProfileLabel(t *testing.T) {
 path := filepath.Join(t.TempDir(), "filter")
 cases := []struct { filter, want string }{
  {regexp.QuoteMeta("🇱🇹 Lithuania, Extra Whitelist"), "🇱🇹 Lithuania, Extra Whitelist"},
  {regexp.QuoteMeta("ZZ Custom (Extra) + city"), "ZZ Custom (Extra) + city"},
  {".*Poland.*Extra.*", ""}, {"Lithuania|Poland", ""}, {"(?i)Lithuania", ""},
  {"[", ""}, {strings.Repeat("a", 2049), ""},
  {regexp.QuoteMeta("https://example.invalid/private"), "Extra profile"},
 }
 for _, c := range cases {
  if err := os.WriteFile(path, []byte(c.filter), 0600); err != nil { t.Fatal(err) }
  if got := currentExactProfileLabel(path); got != c.want { t.Fatalf("got %q, want %q", got, c.want) }
 }
 if got := currentExactProfileLabel(filepath.Join(t.TempDir(), "missing")); got != "" { t.Fatal(got) }
}
