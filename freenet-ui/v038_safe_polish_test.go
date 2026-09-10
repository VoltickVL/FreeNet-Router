package main

import (
	"os"
	"strings"
	"testing"
)

func TestSafeV038PolishHasNoSelfMutatingObserver(t *testing.T) {
	data, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	marker := "// Issue #363: safe v0.3.8 visual polish without a DOM observer."
	pos := strings.Index(src, marker)
	if pos < 0 {
		t.Fatal("safe v0.3.8 polish marker missing")
	}
	safe := src[pos:]
	if strings.Contains(safe, "new MutationObserver(") {
		t.Fatal("safe v0.3.8 block must not create a MutationObserver")
	}
	bad := "new MutationObserver(run).observe(document.documentElement, {subtree:true, childList:true, characterData:true})"
	if strings.Contains(src, bad) {
		t.Fatal("v0.3.8 self-mutating observer regression detected")
	}
	if !strings.Contains(safe, "const safeBaseFetch = window.fetch.bind(window);") || !strings.Contains(safe, "safeBaseFetch(input, init)") {
		t.Fatal("safe current-refresh lifecycle must use its isolated fetch delegate")
	}
	for _, needle := range []string{
		"FreeNetV038SafePolish",
		"fn-current-refresh-visible",
		"white-space:pre-line",
		"min-height:52px",
		"height:46px",
		"font-size:14px",
	} {
		if !strings.Contains(safe, needle) {
			t.Fatalf("safe polish contract missing %q", needle)
		}
	}
}
