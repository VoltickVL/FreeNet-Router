package main

import (
	"os"
	"strings"
	"testing"
)

func TestVersionPickerShowsBusyProgressAndFreezesBackground(t *testing.T) {
	data, err := os.ReadFile("web/settings-v3.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{
		"__freenetVersionPickerProgress",
		"fn-version-plan-checking",
		"fn-version-plan-busy",
		"fn-version-plan-page-frozen",
		"fn-version-progress",
		"Проверка версии FreeNet выполняется",
		"Формируем безопасный plan",
		"release metadata, manifest/SHA-256 и compatibility",
		"Изменения пока не применяются",
		"#topFreenetUpdate",
		"#fnModalRoot .fn-modal-backdrop",
		"event.stopImmediatePropagation()",
		"setVersionPickerBusy(busy)",
		"btn.disabled = busy",
		"search.disabled = busy",
		"root.setAttribute('aria-busy', busy ? 'true' : 'false')",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing version picker progress marker %q", want)
		}
	}

	bodyStart := strings.Index(s, "function watchVersionPickerProgress()")
	if bodyStart < 0 {
		t.Fatal("watchVersionPickerProgress is missing")
	}
	body := s[bodyStart:]
	for _, forbidden := range []string{
		"/api/system/update/apply",
		"/api/network-profile/apply",
		"fetch(",
		"location.reload()",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("progress patch must not introduce mutation/fetch behavior: %q", forbidden)
		}
	}
}
