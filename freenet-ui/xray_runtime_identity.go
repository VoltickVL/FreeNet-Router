package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	isolatedXrayProbeName = "fn-xray-probe"
	isolatedXrayProbeFlag = "FREENET_XRAY_PROBE=1"
)

func isolatedXrayProbePath(tmpDir, xrayPath string) (string, error) {
	probePath := filepath.Join(tmpDir, isolatedXrayProbeName)
	if err := os.Symlink(xrayPath, probePath); err != nil {
		return "", err
	}
	return probePath, nil
}

func isolatedXrayProbeEnv(base []string) []string {
	out := append([]string(nil), base...)
	out = append(out, isolatedXrayProbeFlag)
	return out
}

func liveXrayProcessRunning(name string) bool {
	if name != "xray" {
		return processRunning(name)
	}
	return liveXrayProcessRunningInProc("/proc", filepath.Dir(defaultOutPath))
}

func (a *app) liveXrayRunning() bool {
	if a == nil {
		return false
	}
	return liveXrayProcessRunningInProc("/proc", filepath.Dir(a.cfg.OutPath))
}

func liveXrayProcessRunningInProc(procRoot, configDir string) bool {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return false
	}
	configDir = filepath.Clean(strings.TrimSpace(configDir))
	exact := 0
	gidFallback := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		pidDir := filepath.Join(procRoot, entry.Name())
		comm, err := os.ReadFile(filepath.Join(pidDir, "comm"))
		if err != nil || strings.TrimSpace(string(comm)) != "xray" {
			continue
		}
		env, _ := os.ReadFile(filepath.Join(pidDir, "environ"))
		if envContains(env, isolatedXrayProbeFlag) {
			continue
		}
		if configDir != "" && configDir != "." && envContains(env, "XRAY_LOCATION_CONFDIR="+configDir) {
			exact++
			continue
		}
		status, _ := os.ReadFile(filepath.Join(pidDir, "status"))
		for _, line := range strings.Split(string(status), "\n") {
			if !strings.HasPrefix(line, "Gid:") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[1] == "11111" {
				gidFallback++
			}
			break
		}
	}
	if exact > 0 {
		return exact == 1
	}
	return gidFallback == 1
}

func envContains(env []byte, want string) bool {
	if len(env) == 0 || want == "" {
		return false
	}
	needle := []byte(want)
	for _, item := range bytes.Split(env, []byte{0}) {
		if bytes.Equal(item, needle) {
			return true
		}
	}
	return false
}
