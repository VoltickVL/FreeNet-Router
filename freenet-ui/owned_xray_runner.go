package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	ownedXrayProtocol = "FREENET_OWNED_XRAY_V1"
	ownedXrayMaxFrame = 512
	ownedXrayMaxRequests = 16
)

var errOwnedXrayFrameChanged = errors.New("owned Xray request changed during read")

// Only these fixed operations are available. Callers supply the exact config
// paths; a helper request cannot supply an executable or extra arguments.
// This primitive never acquires a shared/exclusive probe lease.
type ownedXrayStage struct {
	ConfigDir string
	LogPath string
	Timeout time.Duration
	Action string // test, start, stop
}

type ownedXrayProcess struct {
	cmd *exec.Cmd
	cancel context.CancelFunc
	done chan struct{}
	err error
	configDir string
	action string
}

type ownedXraySession struct {
	dir string
	workDir string
	nonce string
	parent string
	parentInfo os.FileInfo
	dirInfo os.FileInfo
	workInfo os.FileInfo
	xrayPath string
	env []string
	stages map[string]ownedXrayStage

	ctx context.Context
	cancel context.CancelFunc
	mu sync.Mutex
	pid int
	started bool
	closed bool
	busy bool
	interrupted bool
	failure error
	lastRequest []byte
	active *ownedXrayProcess
	done chan struct{}
	failed chan error
}

func ownedXraySafePath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path &&
		!strings.ContainsAny(path, "\x00\r\n\t")
}

func newOwnedXraySession(dir, nonce, xrayPath string, env []string, stages map[string]ownedXrayStage) (*ownedXraySession, error) {
	bad := errors.New("cannot prepare owned Xray channel")
	parentInfo, err := os.Lstat(dir)
	if err != nil || !parentInfo.IsDir() || !providerFinalizationOwned(parentInfo, 0700) ||
		!ownedXraySafePath(dir) || len(nonce) != 32 {
		return nil, bad
	}
	for _, ch := range nonce {
		if !(ch >= '0' && ch <= '9') && !(ch >= 'a' && ch <= 'f') {
			return nil, bad
		}
	}
	if !filepath.IsAbs(xrayPath) {
		xrayPath, err = exec.LookPath(xrayPath)
		if err != nil {
			return nil, bad
		}
		xrayPath, err = filepath.Abs(xrayPath)
		if err != nil {
			return nil, bad
		}
	}
	if !ownedXraySafePath(xrayPath) || len(stages) == 0 {
		return nil, bad
	}
	channelDir := filepath.Join(dir, "owned-xray")
	workDir := filepath.Join(dir, "helper")
	if err := os.Mkdir(channelDir, 0700); err != nil {
		return nil, bad
	}
	if err := os.Mkdir(workDir, 0700); err != nil {
		_ = os.Remove(channelDir)
		return nil, bad
	}
	channelInfo, err := os.Lstat(channelDir)
	if err != nil || !channelInfo.IsDir() || !providerFinalizationOwned(channelInfo, 0700) {
		return nil, bad
	}
	workInfo, err := os.Lstat(workDir)
	if err != nil || !workInfo.IsDir() || !providerFinalizationOwned(workInfo, 0700) {
		return nil, bad
	}
	alias, err := isolatedXrayProbePath(channelDir, xrayPath)
	if err != nil {
		return nil, bad
	}
	copyStages := make(map[string]ownedXrayStage, len(stages))
	for name, stage := range stages {
		if name == "" || len(name) > 32 || !ownedXraySafePath(stage.ConfigDir) {
			return nil, bad
		}
		for _, ch := range name {
			if !(ch >= 'a' && ch <= 'z') && ch != '-' {
				return nil, bad
			}
		}
		if stage.Action != "test" && stage.Action != "start" && stage.Action != "stop" {
			return nil, bad
		}
		if stage.Action != "stop" && stage.Timeout <= 0 {
			return nil, bad
		}
		if (stage.Action == "start" || stage.Action == "stop") && filepath.Dir(stage.ConfigDir) != workDir {
			return nil, bad
		}
		if stage.LogPath != "" && (!ownedXraySafePath(stage.LogPath) || filepath.Dir(stage.LogPath) != workDir) {
			return nil, bad
		}
		copyStages[name] = stage
	}
	// The executable never inherits the helper's private control credentials.
	// Only the helper can ask the parent to launch a known read-only stage.
	childEnv := make([]string, 0, len(env)+1)
	for _, value := range env {
		key, _, _ := strings.Cut(value, "=")
		if strings.HasPrefix(key, "FREENET_OWNED_XRAY_") ||
			key == "FREENET_PROVIDER_FINALIZATION_STATUS" ||
			key == "FREENET_PROVIDER_FINALIZATION_NONCE" {
			continue
		}
		childEnv = append(childEnv, value)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &ownedXraySession{
		dir: channelDir, workDir: workDir, parent: dir, nonce: nonce,
		parentInfo: parentInfo, dirInfo: channelInfo, workInfo: workInfo,
		xrayPath: alias, env: isolatedXrayProbeEnv(childEnv), stages: copyStages,
		ctx: ctx, cancel: cancel, done: make(chan struct{}), failed: make(chan error, 1),
	}, nil
}

func (s *ownedXraySession) Attach(cmd *exec.Cmd) {
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env,
		"FREENET_OWNED_XRAY_DIR="+s.dir,
		"FREENET_OWNED_XRAY_NONCE="+s.nonce,
		"FREENET_OWNED_XRAY_WORK_DIR="+s.workDir,
	)
}

func (s *ownedXraySession) Start(pid int) {
	s.mu.Lock()
	s.pid = pid
	s.started = true
	s.mu.Unlock()
	go s.serve()
}

func (s *ownedXraySession) Errors() <-chan error { return s.failed }

func (s *ownedXraySession) Quiescent() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.busy && s.active == nil && s.failure == nil
}

// Closing first prevents an in-flight failed test from replying to the helper
// and starting rollback while its parent is trying to abort. Each Xray is a
// direct Go child: cancellation is always followed by its one actual Wait.
func (s *ownedXraySession) CloseAndDrain() bool {
	s.mu.Lock()
	s.interrupted = s.interrupted || s.busy || s.active != nil
	s.closed = true
	s.cancel()
	process := s.active
	started := s.started
	s.mu.Unlock()
	if process != nil {
		process.cancel()
		<-process.done
	}
	if started {
		<-s.done
	}
	data, readErr := s.readRequest()
	s.mu.Lock()
	defer s.mu.Unlock()
	if readErr != nil || (len(data) != 0 && !bytes.Equal(data, s.lastRequest)) {
		s.interrupted = true
	}
	return s.interrupted || s.failure != nil
}

func ownedXraySameDirectory(path string, original os.FileInfo) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir() && providerFinalizationOwned(info, 0700) &&
		os.SameFile(original, info)
}

func (s *ownedXraySession) directoriesUnchanged() bool {
	return ownedXraySameDirectory(s.parent, s.parentInfo) &&
		ownedXraySameDirectory(s.dir, s.dirInfo)
}

func (s *ownedXraySession) readRequest() ([]byte, error) {
	bad := errors.New("invalid owned Xray request")
	if !s.directoriesUnchanged() {
		return nil, bad
	}
	path := filepath.Join(s.dir, "request")
	before, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil || !before.Mode().IsRegular() || !providerFinalizationOwned(before, 0600) ||
		before.Size() < 1 || before.Size() > ownedXrayMaxFrame {
		return nil, bad
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, bad
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) || !providerFinalizationOwned(opened, 0600) {
		return nil, bad
	}
	data, err := io.ReadAll(io.LimitReader(file, ownedXrayMaxFrame+1))
	after, statErr := os.Lstat(path)
	if err != nil || statErr != nil || len(data) > ownedXrayMaxFrame || !s.directoriesUnchanged() ||
		!after.Mode().IsRegular() || !providerFinalizationOwned(after, 0600) {
		return nil, bad
	}
	if !os.SameFile(before, after) {
		// A helper atomically replaces request only after the preceding reply.
		// Retry a changed owned file; never execute an unpinned sample.
		return nil, errOwnedXrayFrameChanged
	}
	if int64(len(data)) != before.Size() || before.Size() != after.Size() ||
		!before.ModTime().Equal(after.ModTime()) {
		return nil, bad
	}
	return data, nil
}

func (s *ownedXraySession) fail(err error) {
	s.mu.Lock()
	if s.failure == nil && !s.closed {
		s.failure = err
		select {
		case s.failed <- err:
		default:
		}
	}
	s.mu.Unlock()
}

func (s *ownedXraySession) serve() {
	defer close(s.done)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	var previous []byte
	sequence := 0
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		}
		data, err := s.readRequest()
		if errors.Is(err, errOwnedXrayFrameChanged) {
			continue
		}
		if err != nil {
			s.fail(err)
			return
		}
		if len(data) == 0 || bytes.Equal(data, previous) {
			continue
		}
		fields := strings.Split(strings.TrimSuffix(string(data), "\n"), "\t")
		if len(fields) != 6 || bytes.Count(data, []byte("\n")) != 1 || !bytes.HasSuffix(data, []byte("\n")) ||
			fields[0] != ownedXrayProtocol || fields[1] != s.nonce || fields[2] != strconv.Itoa(s.pid) ||
			fields[3] != strconv.Itoa(sequence+1) || sequence >= ownedXrayMaxRequests {
			s.fail(errors.New("owned Xray request identity is invalid"))
			return
		}
		stage, ok := s.stages[fields[4]]
		if !ok || fields[5] != stage.ConfigDir {
			s.fail(errors.New("owned Xray stage or path is not allowed"))
			return
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return
		}
		s.busy = true
		s.mu.Unlock()
		ok, pid, err := s.perform(stage)
		if err != nil {
			s.fail(err)
			return
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return
		}
		s.busy = false
		result := "FAIL"
		if ok {
			result = "OK"
		}
		response := fmt.Sprintf("%s\t%s\t%d\t%d\t%s\t%s\t%d\n",
			ownedXrayProtocol, s.nonce, s.pid, sequence+1, fields[4], result, pid)
		err = s.writeResponse(response)
		if err == nil {
			s.lastRequest = append(s.lastRequest[:0], data...)
		}
		s.mu.Unlock()
		if err != nil {
			s.fail(err)
			return
		}
		previous = append(previous[:0], data...)
		sequence++
	}
}

func (s *ownedXraySession) writeResponse(response string) error {
	if !s.directoriesUnchanged() {
		return errors.New("owned Xray response directory changed")
	}
	file, err := os.CreateTemp(s.dir, "response-")
	if err != nil {
		return errors.New("cannot prepare owned Xray response")
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err = io.WriteString(file, response); err != nil {
		_ = file.Close()
		return errors.New("cannot write owned Xray response")
	}
	if err = file.Close(); err != nil {
		return errors.New("cannot close owned Xray response")
	}
	if err = os.Rename(path, filepath.Join(s.dir, "response")); err != nil {
		return errors.New("cannot publish owned Xray response")
	}
	return nil
}

// Never open/truncate an existing helper-supplied path. A fresh owned regular
// inode is atomically published instead, so a FIFO cannot block cancellation
// and a hardlink cannot redirect a log write into an unrelated file.
func (s *ownedXraySession) openStageLog(path string) (*os.File, error) {
	bad := errors.New("cannot open owned Xray stage log")
	if !ownedXraySameDirectory(s.workDir, s.workInfo) {
		return nil, bad
	}
	before, err := os.Lstat(path)
	if err == nil {
		stat, ok := before.Sys().(*syscall.Stat_t)
		if !before.Mode().IsRegular() || !providerFinalizationOwned(before, 0600) || !ok || stat.Nlink != 1 {
			return nil, bad
		}
	} else if !os.IsNotExist(err) {
		return nil, bad
	}
	file, err := os.CreateTemp(s.workDir, "xray-log-")
	if err != nil {
		return nil, bad
	}
	tmpPath := file.Name()
	defer os.Remove(tmpPath)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || !providerFinalizationOwned(info, 0600) ||
		!ownedXraySameDirectory(s.workDir, s.workInfo) {
		_ = file.Close()
		return nil, bad
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = file.Close()
		return nil, bad
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, after) || !ownedXraySameDirectory(s.workDir, s.workInfo) {
		_ = file.Close()
		return nil, bad
	}
	return file, nil
}

func (s *ownedXraySession) perform(stage ownedXrayStage) (bool, int, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false, 0, nil
	}
	if stage.Action == "stop" {
		process := s.active
		s.mu.Unlock()
		if process == nil || process.action != "start" || process.configDir != stage.ConfigDir {
			return false, 0, errors.New("owned Xray stop has no owned probe")
		}
		process.cancel()
		<-process.done
		s.mu.Lock()
		s.active = nil
		s.mu.Unlock()
		return true, 0, nil
	}
	if s.active != nil {
		s.mu.Unlock()
		return false, 0, errors.New("owned Xray stage overlaps an owned probe")
	}
	if !ownedXraySameDirectory(s.workDir, s.workInfo) {
		s.mu.Unlock()
		return false, 0, errors.New("owned Xray work directory changed")
	}
	// Running a probe may only use its real private work subdirectory. A
	// symlink to the production config must never turn a probe into a core.
	if filepath.Dir(stage.ConfigDir) == s.workDir {
		info, err := os.Lstat(stage.ConfigDir)
		if err != nil || !info.IsDir() {
			s.mu.Unlock()
			return false, 0, errors.New("owned Xray config directory is not private")
		}
	}
	ctx, cancel := context.WithTimeout(s.ctx, stage.Timeout)
	args := []string{"run", "-confdir", stage.ConfigDir}
	if stage.Action == "test" {
		args = []string{"run", "-test", "-confdir", stage.ConfigDir}
	}
	cmd := exec.CommandContext(ctx, s.xrayPath, args...)
	cmd.Env = s.env
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	var log *os.File
	if stage.LogPath != "" {
		var err error
		log, err = s.openStageLog(stage.LogPath)
		if err != nil {
			cancel()
			s.mu.Unlock()
			return false, 0, errors.New("cannot open owned Xray stage log")
		}
		cmd.Stdout = log
		cmd.Stderr = log
	}
	if err := cmd.Start(); err != nil {
		if log != nil {
			_ = log.Close()
		}
		cancel()
		s.mu.Unlock()
		return false, 0, nil
	}
	process := &ownedXrayProcess{
		cmd: cmd, cancel: cancel, done: make(chan struct{}),
		configDir: stage.ConfigDir, action: stage.Action,
	}
	s.active = process
	s.mu.Unlock()
	go func() {
		process.err = cmd.Wait()
		cancel()
		if log != nil {
			_ = log.Close()
		}
		close(process.done)
	}()
	if stage.Action == "start" {
		return true, cmd.Process.Pid, nil
	}
	<-process.done
	s.mu.Lock()
	s.active = nil
	s.mu.Unlock()
	return process.err == nil, 0, nil
}

