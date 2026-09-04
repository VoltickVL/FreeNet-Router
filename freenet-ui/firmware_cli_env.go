package main

import (
	"os"
	"path/filepath"
	"strings"
)

const firmwareCLIWrapperDir = "/tmp/freenet-firmware-cli"

const ndmcWrapperScript = `#!/bin/sh
# ndmc is a Keenetic/Netcraze firmware binary. Entware's inherited library
# search path can make non-interactive ndmc load incompatible /opt libraries.
# Sanitize only this firmware subprocess; the caller keeps its Entware env.
export LD_LIBRARY_PATH=
if [ -n "${FREENET_NDMC_BIN:-}" ]; then
    [ -x "$FREENET_NDMC_BIN" ] || exit 127
    exec "$FREENET_NDMC_BIN" "$@"
fi
for BIN in /bin/ndmc /usr/bin/ndmc /sbin/ndmc /usr/sbin/ndmc; do
    [ -x "$BIN" ] && exec "$BIN" "$@"
done
exit 127
`

const networkHelperWrapperScript = `#!/bin/sh
WRAP_DIR="${FREENET_FIRMWARE_CLI_DIR:-/tmp/freenet-firmware-cli}"
PATH="$WRAP_DIR:$PATH"
export PATH
TARGET="${FREENET_NETWORK_HELPER_TARGET:-/opt/lib/freenet/apply_network_profile.sh}"
exec "$TARGET" "$@"
`

func writePrivateExecutable(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".freenet-cli-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Chmod(0700); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func installFirmwareCLIWrappers(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return "", err
	}
	if err := writePrivateExecutable(filepath.Join(dir, "ndmc"), ndmcWrapperScript); err != nil {
		return "", err
	}
	networkWrapper := filepath.Join(dir, "apply_network_profile.sh")
	if err := writePrivateExecutable(networkWrapper, networkHelperWrapperScript); err != nil {
		return "", err
	}
	return networkWrapper, nil
}

func configureFirmwareCLIEnvironment() error {
	target := strings.TrimSpace(os.Getenv("FREENET_NETWORK_HELPER"))
	if target == "" || target == filepath.Join(firmwareCLIWrapperDir, "apply_network_profile.sh") {
		target = defaultNetworkHelperPath
	}
	wrapper, err := installFirmwareCLIWrappers(firmwareCLIWrapperDir)
	if err != nil {
		return err
	}
	if err := os.Setenv("FREENET_NETWORK_HELPER_TARGET", target); err != nil {
		return err
	}
	if err := os.Setenv("FREENET_FIRMWARE_CLI_DIR", firmwareCLIWrapperDir); err != nil {
		return err
	}
	return os.Setenv("FREENET_NETWORK_HELPER", wrapper)
}

func init() {
	_ = configureFirmwareCLIEnvironment()
}
