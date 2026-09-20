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
		`fn-version-picker-mode`,
		`positionVersionPicker`,
		`anchored: true`,
		`update-available`,
		`renderTopbarVersion`,
		`openTopbarUpdateModal`,
		`/api/system/update/plan`,
		`/api/system/update/releases`,
		`/api/system/update/releases?fresh=1`,
		`fnVersionList`,
		`filtered.slice(0, 5)`,
		`Math.min(540, vw - 24)`,
		`requestAnimationFrame(positionVersionPicker)`,
		`versionActionLabel`,
		`Откатить до`,
		`AbortSignal.timeout(15000)`,
		`renderTopbarVersion(current, !!latest && latest !== current, latest)`,
		`await startUpdate()`,
		`/api/system/update/apply`,
		`location.hash === '#system'`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing topbar/sidebar contract marker %q", want)
		}
	}
	for _, want := range []string{
		`j.operation_id || plan.target_tag`,
		`state.target_version`,
		`state.update_lock_held`,
		`targetMismatch`,
		`staleTerminalWhileRunning`,
		`activeState = ['CHECKING','SNAPSHOT','UPDATING','RECONNECTING','BUSY']`,
		`Подключаемся к текущей операции обновления`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing updater stale-state/reconnect marker %q", want)
		}
	}

	for _, unwanted := range []string{
		`const control = qs('#topXkeenLink')`,
		`control.removeAttribute('target')`,
		`control.href = '#overview'`,
		`.nav{gap:8px}`,
		`.nav-btn{min-height:48px;padding:12px 14px`,
		`.nav-icon{width:21px;height:21px;font-size:16px}`,
		`openVersionTargetConfirmation`,
	} {
		if strings.Contains(s, unwanted) {
			t.Fatalf("topbar update must not own XKeen or restyle approved sidebar: %q", unwanted)
		}
	}
	if strings.Count(s, "/api/system/update/releases?fresh=1") != 1 {
		t.Fatal("explicit version picker must have exactly one fresh release-catalog request")
	}
	if !strings.Contains(s, "fetch('/api/system/update/releases', {cache: 'no-store', signal: AbortSignal.timeout(15000)})") {
		t.Fatal("background topbar polling must keep the cached release-catalog endpoint")
	}
	if strings.Contains(s, "MutationObserver") {
		t.Fatal("topbar/sidebar implementation must not introduce MutationObserver")
	}

	accepted, err := os.ReadFile("web/accepted-ux.js")
	if err != nil {
		t.Fatal(err)
	}
	ux := string(accepted)
	for _, want := range []string{
		"fnUpdateNotesTitle", "fnUpdateNotes", "Что нового в", "release_notes",
		".topbar.overview-approved{height:82px",
		"#fnVpnPickerToggle{height:60px",
		"fn-shell-dns",
		"Раздельный",
	} {
		if !strings.Contains(ux, want) {
			t.Fatalf("missing updater release-notes UX marker %q", want)
		}
	}
	for _, unwanted := range []string{"Обновление безопасно", "Backup, SHA-256, staging и post-check выполняются автоматически", "fn-update-safety"} {
		if strings.Contains(ux, unwanted) {
			t.Fatalf("redundant updater safety copy must stay removed: %q", unwanted)
		}
	}
}
