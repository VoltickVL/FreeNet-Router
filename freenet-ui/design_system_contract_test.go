package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAcceptedUXSharedDesignSystemContract(t *testing.T) {
	a := &app{}
	req := httptest.NewRequest("GET", "/accepted-ux.js", nil)
	w := httptest.NewRecorder()
	a.handleIndex(w, req)
	if w.Code != 200 {
		t.Fatalf("accepted UX asset status = %d", w.Code)
	}
	asset := w.Body.String()

	for _, want := range []string{
		"FreeNet UI design tokens: shared baseline for current and future Control Center tabs",
		`--fn-ui-font:"Segoe UI Variable","Segoe UI",ui-sans-serif,system-ui`,
		"--fn-ui-weight-regular:400",
		"--fn-ui-weight-medium:500",
		"--fn-ui-weight-semibold:600",
		"--fn-ui-weight-bold:700",
		"--fn-ui-title:34px",
		"--fn-ui-subtitle:15px",
		"--fn-ui-section:19px",
		"--fn-ui-label:14px",
		"--fn-ui-value:25px",
		"--fn-ui-button:15px",
		".fn-ui-card{",
		".fn-ui-section-title{",
		".fn-ui-label{",
		".fn-ui-value{",
		".fn-ui-button{",
		".subscription-approved .fn-sub-label{",
		"font-size:var(--fn-ui-label)!important",
		"font-weight:var(--fn-ui-weight-semibold)!important",
		".subscription-approved .fn-sub-table{",
		".subscription-approved .fn-sub-info-grid dt{",
	} {
		if !strings.Contains(asset, want) {
			t.Fatalf("shared design-system contract missing %q", want)
		}
	}

	for _, forbidden := range []string{
		"fonts.googleapis.com",
		"fonts.gstatic.com",
		"@font-face",
		"MutationObserver",
	} {
		if strings.Contains(asset, forbidden) {
			t.Fatalf("shared design-system contract contains forbidden %q", forbidden)
		}
	}
}
