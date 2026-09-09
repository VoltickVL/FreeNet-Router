package main

import (
	"os"
	"strings"
	"testing"
)

func TestApprovedOverviewPixelContract(t *testing.T) {
	data, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	mustContain := []string{
		"grid-template-columns:minmax(312px,342px) minmax(0,1fr)",
		".vpn-current-panel .best-v4-pill{min-height:78px",
		".vpn-current-panel>#bestServerCheckCurrent{width:100%;min-height:48px",
		".vpn-option{min-height:145px",
		".vpn-option-title .flag-icon{width:34px;height:24px",
		".vpn-state-badge{display:inline-flex;align-items:center;gap:7px;min-height:34px",
		".vpn-detail-chip{display:inline-flex;align-items:center;gap:6px;min-height:28px",
		".best-v4-status.summary{display:flex;align-items:flex-start;gap:10px}",
		"#bestServerAdvanced #profilesList.profiles{order:2;display:grid!important",
		"#profilesList input,#profilesTrigger{min-height:48px",
		"quick.parentNode.insertBefore(manual, quick.nextSibling)",
		"makeIcon(key, 'metric-icon')",
		"makeIcon(state.icon, 'status-icon')",
		".flag-no:after{content:'';position:absolute;inset:0",
	}
	for _, needle := range mustContain {
		if !strings.Contains(s, needle) {
			t.Fatalf("approved Overview design contract missing %q", needle)
		}
	}

	mustNotContain := []string{
		"#bestServerAdvanced{grid-column:1/-1",
		".vpn-option .best-v4-pill:before{display:none}",
	}
	for _, needle := range mustNotContain {
		if strings.Contains(s, needle) {
			t.Fatalf("obsolete compact Overview rule returned: %q", needle)
		}
	}
}
