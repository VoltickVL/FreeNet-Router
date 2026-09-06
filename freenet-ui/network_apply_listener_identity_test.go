package main

import (
	"strings"
	"testing"
)

func TestNetworkApplyUsesReplaceableSingleListenerIdentity(t *testing.T) {
	selfUpdate, err := webFS.ReadFile("web/self-update.js")
	if err != nil {
		t.Fatal(err)
	}
	vpnFix, err := webFS.ReadFile("web/vpn-ux-fix.js")
	if err != nil {
		t.Fatal(err)
	}

	self := string(selfUpdate)
	fix := string(vpnFix)

	assign := strings.Index(self, "applyNetworkProfile = applyDraft;")
	listen := strings.Index(self, "applyButton.addEventListener('click', applyDraft);")
	if assign < 0 || listen < 0 || assign > listen {
		t.Fatal("network draft listener must publish its exact function identity before registration")
	}

	for _, required := range []string{
		"const legacyApplyNetworkProfile = applyNetworkProfile;",
		"applyButton.removeEventListener('click', legacyApplyNetworkProfile);",
		"applyNetworkProfile = reconciledNetworkApply;",
		"applyButton.addEventListener('click', applyNetworkProfile);",
	} {
		if !strings.Contains(fix, required) {
			t.Fatalf("network apply replacement contract missing %q", required)
		}
	}
}
