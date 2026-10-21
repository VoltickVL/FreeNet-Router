package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	providerFinalizationGrace = 120 * time.Second
	providerFinalizationProtocol = "FREENET_PROVIDER_FINALIZATION_V1"
	providerFinalizationMaxBytes = 256
)

// Production provider work has its own finite budget. Programmatic fixtures
// with a zero ProviderTimeout retain their deliberately short action budget.
func providerWorkTimeout(cfg config) time.Duration {
	if cfg.ProviderTimeout != 0 {
		return cfg.ProviderTimeout
	}
	return cfg.Timeout
}

func providerTimeoutOverride(action, provider time.Duration, explicitAction, explicitProvider bool) time.Duration {
	if explicitAction && !explicitProvider {
		return action
	}
	return provider
}

func ownedXrayEnvValue(env []string, key, fallback string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if value, ok := strings.CutPrefix(env[i], key+"="); ok {
			if value != "" {
				return value
			}
			return fallback
		}
	}
	return fallback
}

func newProviderOwnedXraySession(cmd *exec.Cmd, dir, nonce string, mutating bool) (*ownedXraySession, error) {
	workDir := filepath.Join(dir, "helper")
	probeDir := filepath.Join(workDir, "provider-route-probe")
	configDir := ownedXrayEnvValue(cmd.Env, "FREENET_CONFIG_DIR", "/opt/etc/xray/configs")
	xrayPath := ownedXrayEnvValue(cmd.Env, "FREENET_XRAY_BIN", "/opt/sbin/xray")
	assetDir := ownedXrayEnvValue(cmd.Env, "FREENET_ASSET_DIR", "/opt/etc/xray/dat")
	stages := map[string]ownedXrayStage{
		"candidate-test": {Action: "test", ConfigDir: filepath.Join(workDir, "conf"), LogPath: filepath.Join(workDir, "xray-test.log"), Timeout: 90 * time.Second},
		"route-test": {Action: "test", ConfigDir: probeDir, Timeout: 15 * time.Second},
		"route-start": {Action: "start", ConfigDir: probeDir, Timeout: 40 * time.Second},
		"route-stop": {Action: "stop", ConfigDir: probeDir},
	}
	if mutating {
		stages["live-test"] = ownedXrayStage{Action: "test", ConfigDir: configDir, LogPath: filepath.Join(workDir, "xray-test.log"), Timeout: 90 * time.Second}
	}
	env := append(append([]string(nil), cmd.Env...), "XRAY_LOCATION_ASSET="+assetDir)
	return newOwnedXraySession(dir, nonce, xrayPath, env, stages)
}

// Plans and legacy apply share child ownership, but cannot obtain the managed
// core-only finalization reserve. The caller owns any probe lease and temp dir.
func runProviderCommandWithOwnedXray(ctx context.Context, cmd *exec.Cmd, dir string, mutating bool) ([]byte, error) {
	status, err := newProviderFinalizationStatus(dir)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	owner, err := newProviderOwnedXraySession(cmd, dir, status.nonce, mutating)
	if err != nil {
		return nil, err
	}
	owner.Attach(cmd)
	defer owner.CloseAndDrain()
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	owner.Start(cmd.Process.Pid)
	type waitResult struct { err error; interrupted bool }
	done := make(chan waitResult, 1)
	go func() {
		waitErr := cmd.Wait()
		done <- waitResult{waitErr, owner.CloseAndDrain()}
	}()
	unknown := func(reason string, cause error) error {
		if mutating {
			return &providerFinalizationUnknownError{reason: reason, cause: cause}
		}
		return errors.New(reason)
	}
	stop := func(reason string, cause error) ([]byte, error) {
		owner.CloseAndDrain()
		_ = cmd.Process.Kill()
		<-done
		return output.Bytes(), unknown(reason, cause)
	}
	finish := func(waited waitResult) ([]byte, error) {
		if waited.interrupted {
			return output.Bytes(), unknown("provider helper ended with unfinished or invalid owned Xray work", waited.err)
		}
		if errors.Is(waited.err, exec.ErrWaitDelay) && ctx.Err() == nil && cmd.ProcessState != nil && cmd.ProcessState.Success() {
			waited.err = nil
		}
		return output.Bytes(), waited.err
	}
	select {
	case waited := <-done:
		return finish(waited)
	case ownerErr := <-owner.Errors():
		return stop("provider owned Xray work was not confirmed", ownerErr)
	case <-ctx.Done():
		select {
		case waited := <-done:
			return finish(waited)
		default:
		}
		return stop("provider helper ended before owned Xray work completed", ctx.Err())
	}
}

// Cache refresh happens in the caller before this leaf plan lease: the active
// VPN fetch has its own shared slot. Release follows Xray/helper Wait and temp
// cleanup. Mutating callers already hold the exclusive probe fence.
func runProviderHelperOwned(ctx context.Context, mode, profileID string) ([]byte, error) {
	if mode == "plan" {
		release, ok := acquireIsolatedXrayProbe(ctx)
		if !ok {
			return nil, errors.New("provider plan is waiting for isolated Xray diagnostics")
		}
		defer release()
	} else if mode != "apply" {
		return nil, errors.New("unsupported owned provider helper mode")
	}
	dir, err := os.MkdirTemp("", "freenet-provider-work-")
	if err != nil {
		return nil, errors.New("cannot prepare provider work directory")
	}
	defer os.RemoveAll(dir)
	cmd := exec.Command(providerHelperPath(), mode, profileID)
	cmd.Env = append(os.Environ(), "PATH=/opt/bin:/opt/sbin:/opt/usr/bin:/opt/usr/sbin:/bin:/sbin:/usr/bin:/usr/sbin")
	cmd.WaitDelay = 2 * time.Second
	return runProviderCommandWithOwnedXray(ctx, cmd, dir, mode == "apply")
}

// This error always means the managed helper has no verified terminal result.
// In particular, stdout alone cannot turn a hard timeout into a safe rollback.
type providerFinalizationUnknownError struct {
	reason string
	cause error
}

func (e *providerFinalizationUnknownError) Error() string { return e.reason }
func (e *providerFinalizationUnknownError) Unwrap() error { return e.cause }

func providerFinalizationUnknown(err error) bool {
	var unknown *providerFinalizationUnknownError
	return errors.As(err, &unknown)
}

type providerFinalizationRecord struct {
	phase string
	outcome string
}

func (r providerFinalizationRecord) terminalExitCode() int {
	switch {
	case r.phase == "rollback-finalizing" && r.outcome == "rolled-back":
		return 1
	case r.phase == "accepted-finalizing" && r.outcome == "accepted":
		return 0
	default:
		return -1
	}
}

type providerFinalizationStatus struct {
	path string
	nonce string
	directory os.FileInfo
}

func providerFinalizationOwned(info os.FileInfo, mode os.FileMode) bool {
	if info == nil || info.Mode().Perm() != mode ||
		info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func newProviderFinalizationStatus(dir string) (*providerFinalizationStatus, error) {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || !providerFinalizationOwned(info, 0700) {
		return nil, errors.New("cannot prepare owned provider finalization status")
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return nil, errors.New("cannot prepare provider finalization nonce")
	}
	return &providerFinalizationStatus{
		path: filepath.Join(dir, "finalization.status"),
		nonce: hex.EncodeToString(nonce),
		directory: info,
	}, nil
}

func (s *providerFinalizationStatus) directoryUnchanged() bool {
	info, err := os.Lstat(filepath.Dir(s.path))
	return err == nil && info.IsDir() && providerFinalizationOwned(info, 0700) &&
		os.SameFile(s.directory, info)
}

// The status is separate from profile/helper stdout. Each read pins an owned,
// regular 0600 file inside the original 0700 selection directory. Atomic helper
// replacement may advance its phase, but cannot change its invocation nonce.
func (s *providerFinalizationStatus) read(pid int) (providerFinalizationRecord, bool, error) {
	invalid := errors.New("invalid provider finalization status")
	if !s.directoryUnchanged() {
		return providerFinalizationRecord{}, true, invalid
	}
	before, err := os.Lstat(s.path)
	if os.IsNotExist(err) {
		return providerFinalizationRecord{}, false, nil
	}
	if err != nil || !before.Mode().IsRegular() ||
		!providerFinalizationOwned(before, 0600) ||
		before.Size() < 1 || before.Size() > providerFinalizationMaxBytes {
		return providerFinalizationRecord{}, true, invalid
	}
	// NOFOLLOW/NONBLOCK also reject a symlink/FIFO replacement between Lstat
	// and Open; the file identity is checked again after the bounded read.
	file, err := os.OpenFile(s.path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return providerFinalizationRecord{}, true, invalid
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) ||
		!opened.Mode().IsRegular() || !providerFinalizationOwned(opened, 0600) {
		return providerFinalizationRecord{}, true, invalid
	}
	data, err := io.ReadAll(io.LimitReader(file, providerFinalizationMaxBytes+1))
	after, statErr := os.Lstat(s.path)
	if err != nil || statErr != nil || len(data) > providerFinalizationMaxBytes ||
		int64(len(data)) != before.Size() || !os.SameFile(before, after) ||
		before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) ||
		!providerFinalizationOwned(after, 0600) || !s.directoryUnchanged() {
		return providerFinalizationRecord{}, true, invalid
	}
	fields := strings.Split(strings.TrimSuffix(string(data), "\n"), "\t")
	if len(fields) != 5 || !bytes.HasSuffix(data, []byte("\n")) ||
		fields[0] != providerFinalizationProtocol || fields[1] != s.nonce ||
		fields[2] != strconv.Itoa(pid) {
		return providerFinalizationRecord{}, true, invalid
	}
	record := providerFinalizationRecord{phase: fields[3], outcome: fields[4]}
	switch record.phase {
	case "rollback-finalizing":
		if record.outcome != "pending" && record.outcome != "rolled-back" {
			return providerFinalizationRecord{}, true, invalid
		}
	case "accepted-finalizing":
		if record.outcome != "pending" && record.outcome != "accepted" {
			return providerFinalizationRecord{}, true, invalid
		}
	default:
		return providerFinalizationRecord{}, true, invalid
	}
	return record, true, nil
}

func runProviderCommandWithFinalization(ctx context.Context, cmd *exec.Cmd, dir string) ([]byte, error) {
	return runProviderCommandWithFinalizationGrace(ctx, cmd, dir, providerFinalizationGrace)
}

// Only the two non-mutating terminal-finalization phases may outlive the
// ordinary action deadline. The reserve is granted once; it cannot be renewed.
// The caller retains its writer/probe fences until this function has waited
// for the real child exit, including after a hard kill.
func runProviderCommandWithFinalizationGrace(ctx context.Context, cmd *exec.Cmd, dir string, grace time.Duration) ([]byte, error) {
	status, err := newProviderFinalizationStatus(dir)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, &providerFinalizationUnknownError{reason: "provider profile apply ended before helper start", cause: err}
	}
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env,
		"FREENET_PROVIDER_FINALIZATION_STATUS="+status.path,
		"FREENET_PROVIDER_FINALIZATION_NONCE="+status.nonce,
	)
	owner, err := newProviderOwnedXraySession(cmd, dir, status.nonce, true)
	if err != nil {
		return nil, err
	}
	owner.Attach(cmd)
	defer owner.CloseAndDrain()
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	owner.Start(cmd.Process.Pid)
	type waitResult struct {
		err error
		interrupted bool
	}
	done := make(chan waitResult, 1)
	go func() {
		waitErr := cmd.Wait()
		done <- waitResult{err: waitErr, interrupted: owner.CloseAndDrain()}
	}()

	finish := func(waited waitResult, expectedPhase string, receiptRequired bool) ([]byte, error) {
		waitErr := waited.err
		if waited.interrupted {
			return output.Bytes(), &providerFinalizationUnknownError{
				reason: "provider helper ended with unfinished or invalid owned Xray work", cause: waitErr,
			}
		}
		record, present, readErr := status.read(cmd.Process.Pid)
		if receiptRequired || present {
			if readErr != nil || !present || record.terminalExitCode() < 0 ||
				(expectedPhase != "" && record.phase != expectedPhase) ||
				cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != record.terminalExitCode() {
				return output.Bytes(), &providerFinalizationUnknownError{
					reason: "provider helper terminal finalization was not confirmed", cause: waitErr,
				}
			}
		}
		// Preserve the established handling of inherited output descriptors,
		// but only after any announced finalization has its terminal receipt.
		if errors.Is(waitErr, exec.ErrWaitDelay) && cmd.ProcessState != nil && cmd.ProcessState.Success() {
			waitErr = nil
		}
		return output.Bytes(), waitErr
	}
	hardStop := func(reason string, cause error) ([]byte, error) {
		// Read-only Xray is a direct Go child. Drain it before killing the
		// mutating shell; never signal a process group containing live Xray.
		owner.CloseAndDrain()
		_ = cmd.Process.Kill()
		<-done
		return output.Bytes(), &providerFinalizationUnknownError{reason: reason, cause: cause}
	}

	select {
	case waitErr := <-done:
		return finish(waitErr, "", false)
	case ownerErr := <-owner.Errors():
		return hardStop("provider owned Xray work was not confirmed", ownerErr)
	case <-ctx.Done():
		// A completed child wins a simultaneous deadline. No worker is left
		// running merely because the context happened to expire first.
		select {
		case waitErr := <-done:
			return finish(waitErr, "", false)
		default:
		}
		if ctx.Err() != context.DeadlineExceeded {
			return hardStop("provider profile apply canceled before terminal finalization", ctx.Err())
		}
		record, present, readErr := status.read(cmd.Process.Pid)
		if readErr != nil || !present || grace <= 0 || !owner.Quiescent() {
			return hardStop("provider profile apply timed out before terminal finalization", context.DeadlineExceeded)
		}
		// A finalization phase cannot launch more tests/probes. Seal the
		// channel before granting its single reserve; a pending request is
		// contradictory evidence and remains UNKNOWN.
		if owner.CloseAndDrain() {
			return hardStop("provider profile apply entered finalization with unfinished Xray work", context.DeadlineExceeded)
		}
		timer := time.NewTimer(grace)
		defer timer.Stop()
		select {
		case waitErr := <-done:
			return finish(waitErr, record.phase, true)
		case <-timer.C:
			select {
			case waitErr := <-done:
				return finish(waitErr, record.phase, true)
			default:
			}
			return hardStop("provider profile apply finalization reserve expired", context.DeadlineExceeded)
		}
	}
}
