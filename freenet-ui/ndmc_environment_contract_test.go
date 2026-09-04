package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
}

func TestFirmwareCLIWrapperCleansOnlyNDMCLibraryPath(t *testing.T) {
	dir := t.TempDir()
	wrapper, err := installFirmwareCLIWrappers(dir)
	if err != nil {
		t.Fatal(err)
	}

	fakeNDMC := filepath.Join(dir, "fake-system-ndmc")
	writeTestExecutable(t, fakeNDMC, `#!/bin/sh
if [ -n "${LD_LIBRARY_PATH:-}" ]; then
    echo "NDMC_LD=dirty:$LD_LIBRARY_PATH"
    exit 91
fi
echo "NDMC_LD=clean"
`)

	fakeNetwork := filepath.Join(dir, "fake-network-helper")
	writeTestExecutable(t, fakeNetwork, `#!/bin/sh
echo "HELPER_LD=${LD_LIBRARY_PATH:-}"
ndmc -c 'show running-config'
`)

	cmd := exec.CommandContext(context.Background(), wrapper, "plan")
	cmd.Env = append(os.Environ(),
		"FREENET_FIRMWARE_CLI_DIR="+dir,
		"FREENET_NETWORK_HELPER_TARGET="+fakeNetwork,
		"FREENET_NDMC_BIN="+fakeNDMC,
		"LD_LIBRARY_PATH=/opt/lib:/opt/usr/lib:/lib:/usr/lib",
		"PATH=/usr/bin:/bin",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("wrapped helper failed: %v: %s", err, out)
	}
	got := string(out)
	if !strings.Contains(got, "HELPER_LD=/opt/lib:/opt/usr/lib:/lib:/usr/lib") {
		t.Fatalf("Entware library path was changed for the whole helper: %q", got)
	}
	if !strings.Contains(got, "NDMC_LD=clean") {
		t.Fatalf("firmware ndmc did not receive the clean library environment: %q", got)
	}
}

func TestFirmwareCLIWrapperFailsClosedWhenNDMCOverrideIsInvalid(t *testing.T) {
	dir := t.TempDir()
	wrapper, err := installFirmwareCLIWrappers(dir)
	if err != nil {
		t.Fatal(err)
	}
	fakeNetwork := filepath.Join(dir, "fake-network-helper")
	writeTestExecutable(t, fakeNetwork, "#!/bin/sh\nndmc -c 'show running-config'\n")

	cmd := exec.Command(wrapper, "plan")
	cmd.Env = append(os.Environ(),
		"FREENET_FIRMWARE_CLI_DIR="+dir,
		"FREENET_NETWORK_HELPER_TARGET="+fakeNetwork,
		"FREENET_NDMC_BIN="+filepath.Join(dir, "missing-ndmc"),
		"LD_LIBRARY_PATH=/opt/lib",
		"PATH=/usr/bin:/bin",
	)
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("invalid ndmc override unexpectedly succeeded: %s", out)
	}
}
