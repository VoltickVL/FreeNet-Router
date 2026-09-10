package main

import (
	"os"
	"strings"
	"testing"
)

func TestTopbarUpdateAndSidebarContract(t *testing.T) {
	data, err := os.ReadFile("web/self-update.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{
		`.side-bottom{display:none!important}`,
		`.nav-btn[data-page=\"system\"]{display:none!important}`,
		`fn-version-control`,
		`update-available`,
		`renderTopbarVersion`,
		`openTopbarUpdateModal`,
		`/api/system/update/plan`,
		`/api/system/update/apply`,
		`location.hash === '#system'`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing topbar/sidebar contract marker %q", want)
		}
	}
	if strings.Contains(s, "MutationObserver") {
		t.Fatal("topbar/sidebar implementation must not introduce MutationObserver")
	}
}
