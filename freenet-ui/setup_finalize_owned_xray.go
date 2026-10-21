package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var errSetupFinalizeWorkerState = errors.New("final setup Xray worker lifecycle was not confirmed")

// Finalize has one read-only Xray stage, including its repeated post-apply
// validation. The Go parent owns that child and retains one shared probe lease
// through cancellation, actual Wait and temporary-file cleanup.
func runSetupFinalizeOwned(ctx context.Context, mode string) ([]byte, error) {
	if mode != "plan" && mode != "apply" {
		return nil, errors.New("unsupported final setup operation")
	}
	releaseProbe, ok := acquireIsolatedXrayProbe(ctx)
	if !ok {
		return nil, ctx.Err()
	}
	defer releaseProbe()
	dir, err := os.MkdirTemp("", "freenet-finalize-owned-")
	if err != nil {
		return nil, errors.New("cannot prepare final setup worker")
	}
	defer os.RemoveAll(dir)
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return nil, errors.New("cannot prepare final setup worker identity")
	}
	envPath := func(key, fallback string) string {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
		return fallback
	}
	root := envPath("FREENET_ROOT", "/opt")
	paths := []string{
		envPath("FREENET_XRAY_CONFIG_DIR", filepath.Join(root, "etc/xray/configs")),
		envPath("FREENET_XRAY_ASSET_DIR", filepath.Join(root, "etc/xray/dat")),
		envPath("FREENET_XRAY_BIN", filepath.Join(root, "sbin/xray")),
	}
	for i, path := range paths {
		paths[i], err = filepath.Abs(path)
		if err != nil || !ownedXraySafePath(paths[i]) {
			return nil, errors.New("invalid final setup Xray path")
		}
	}
	env := append(os.Environ(),
		"PATH=/opt/bin:/opt/sbin:/opt/usr/bin:/opt/usr/sbin:/bin:/sbin:/usr/bin:/usr/sbin",
		"FREENET_XRAY_CONFIG_DIR="+paths[0],
		"FREENET_XRAY_ASSET_DIR="+paths[1],
		"FREENET_XRAY_BIN="+paths[2],
		"XRAY_LOCATION_ASSET="+paths[1],
	)
	// This cap does not extend the caller's deadline. The parent cancels and
	// waits for its child before terminating a timed-out shell helper.
	stageTimeout := 40 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		stageTimeout = time.Until(deadline)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if stageTimeout <= 0 {
		return nil, context.DeadlineExceeded
	}
	session, err := newOwnedXraySession(dir, hex.EncodeToString(nonce), paths[2], env, map[string]ownedXrayStage{
		"setup-test": {
			ConfigDir: paths[0], LogPath: filepath.Join(dir, "helper", "xray-test.log"),
			Timeout: stageTimeout, Action: "test",
		},
	})
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(finalizeHelperPath(), mode)
	cmd.Env = env
	cmd.WaitDelay = 2 * time.Second
	session.Attach(cmd)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := ctx.Err(); err != nil {
		session.CloseAndDrain()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		session.CloseAndDrain()
		return nil, err
	}
	session.Start(cmd.Process.Pid)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	finish := func(waitErr error) ([]byte, error) {
		if session.CloseAndDrain() {
			return output.Bytes(), errSetupFinalizeWorkerState
		}
		if errors.Is(waitErr, exec.ErrWaitDelay) && cmd.ProcessState != nil && cmd.ProcessState.Success() {
			waitErr = nil
		}
		return output.Bytes(), waitErr
	}
	stop := func(cause error) ([]byte, error) {
		session.CloseAndDrain()
		_ = cmd.Process.Kill()
		<-done
		return output.Bytes(), cause
	}
	select {
	case waitErr := <-done:
		return finish(waitErr)
	case <-session.Errors():
		return stop(errSetupFinalizeWorkerState)
	case <-ctx.Done():
		select {
		case waitErr := <-done:
			return finish(waitErr)
		default:
		}
		return stop(ctx.Err())
	}
}
