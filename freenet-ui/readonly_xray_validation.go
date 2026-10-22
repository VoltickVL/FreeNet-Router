package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

func resolvedIsolatedXrayProbePath(dir, executable string) (string, error) {
	path, err := exec.LookPath(executable)
	if err != nil {
		return "", err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return isolatedXrayProbePath(dir, path)
}

// A config validation is a direct Go child, with the same role and admission
// rules as quality/version probes. The supplied config directory is read-only
// and may be a caller-owned candidate or a live directory; only our alias is
// cleaned here, after the actual child Wait and before releasing its slot.
func runReadOnlyXrayValidation(ctx context.Context, executable, assetDir, configDir string) error {
	releaseProbe, ok := acquireIsolatedXrayProbe(ctx)
	if !ok {
		return isolatedXrayProbeAdmissionError(ctx)
	}
	defer releaseProbe()
	dir, err := os.MkdirTemp("", "freenet-xray-validation-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	probePath, err := resolvedIsolatedXrayProbePath(dir, executable)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, probePath, "run", "-test", "-confdir", configDir)
	cmd.Env = isolatedXrayProbeEnv(append(os.Environ(), "XRAY_LOCATION_ASSET="+assetDir))
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	return cmd.Run()
}
