package main

import (
	"os"
	"strings"
	"testing"
)

func TestOperationCoordinatorBrowserReconcilesWithoutBlindRetry(t *testing.T) {
	body, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, token := range []string{
		"/api/operation/state",
		"kind: 'quick'",
		"kind: 'provider'",
		"matchingFreshOperation",
		"previousFetch(input, init)",
		"reconcile(meta)",
	} {
		if !strings.Contains(s, token) {
			t.Fatalf("missing coordinator UI token %q", token)
		}
	}
	if strings.Count(s, "previousFetch(input, init)") != 2 {
		t.Fatalf("mutation wrapper should issue one original request per branch, got %d references", strings.Count(s, "previousFetch(input, init)"))
	}
}

func TestMainServesOperationCoordinatorAsset(t *testing.T) {
	body, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, token := range []string{
		"web/operation-coordinator.js",
		"GET /api/operation/state",
		"/operation-coordinator.js?v=v%s",
	} {
		if !strings.Contains(s, token) {
			t.Fatalf("main.go missing %q", token)
		}
	}
}
