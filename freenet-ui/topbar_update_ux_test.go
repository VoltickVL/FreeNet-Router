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
		`.nav-btn[data-page="system"]{display:none!important}`,
		`topFreenetUpdate`,
		`ensureTopbarUpdateControl`,
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
	for _, unwanted := range []string{
		`const control = qs('#topXkeenLink')`,
		`control.removeAttribute('target')`,
		`control.href = '#overview'`,
		`.nav{gap:8px}`,
		`.nav-btn{min-height:48px;padding:12px 14px`,
		`.nav-icon{width:21px;height:21px;font-size:16px}`,
	} {
		if strings.Contains(s, unwanted) {
			t.Fatalf("topbar update must not own XKeen or restyle approved sidebar: %q", unwanted)
		}
	}
	if strings.Contains(s, "MutationObserver") {
		t.Fatal("topbar/sidebar implementation must not introduce MutationObserver")
	}

	accepted, err := os.ReadFile("web/accepted-ux.js")
	if err != nil {
		t.Fatal(err)
	}
	ux := string(accepted)
	for _, want := range []string{"fnUpdateNotesTitle", "fnUpdateNotes", "Что нового в", "release_notes", "fn-update-safety"} {
		if !strings.Contains(ux, want) {
			t.Fatalf("missing updater release-notes UX marker %q", want)
		}
	}
	if strings.Contains(ux, "Обновление безопасно") {
		t.Fatal("static safety card must not replace release notes in updater")
	}
}
