package main

import (
	"os"
	"strings"
	"testing"
)

func TestNetworkHelperIsolatesFirmwareNDMCFromEntwareLibraries(t *testing.T) {
	b, err := os.ReadFile("../scripts/apply_network_profile.sh")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, marker := range []string{
		"ndmc_resolve() {",
		"FREENET_NDMC_BIN",
		"/bin/ndmc",
		"LD_LIBRARY_PATH= \"$BIN\" \"$@\"",
	} {
		if !strings.Contains(s, marker) {
			t.Fatalf("missing ndmc environment isolation marker %q", marker)
		}
	}

	if strings.Contains(s, "export LD_LIBRARY_PATH=") {
		t.Fatal("network helper must not clear LD_LIBRARY_PATH globally; only firmware ndmc may receive the sanitized environment")
	}
}
