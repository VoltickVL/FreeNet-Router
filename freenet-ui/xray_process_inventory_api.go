package main

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// The inventory reports only bounded role counts and finite reason codes.
// It never exposes PIDs, cmdline, environment, paths or VLESS credentials.
// It is read-only and cannot authorize terminating an unknown production core.
type xrayProcessInventoryResponse struct {
	Success         bool   `json:"success"`
	Mutation        string `json:"mutation"`
	State           string `json:"state"`
	ProductionCount int    `json:"production_count"`
	ProbeCount      int    `json:"probe_count"`
	UnknownCount    int    `json:"unknown_count"`
	ZombieCount     int    `json:"zombie_count"`
	RestartReady    bool   `json:"restart_ready"`
	// Safe finite reasons with aggregate counts only. Never expose PID, path,
	// argv or environment. A reason is diagnostic, NOT restart admission.
	Reasons         []xrayProcessInventoryReason `json:"reasons,omitempty"`
}

type xrayProcessInventoryReason struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

func xrayProcessInventoryState(result xrayProcessInventoryResponse) string {
	switch {
	case result.UnknownCount != 0:
		return "unknown-xray-process"
	case result.ZombieCount != 0:
		return "unreaped-xray-process"
	case result.ProductionCount > 1:
		return "multiple-production-xray"
	case result.ProbeCount != 0:
		return "isolated-probe-active"
	case result.ProductionCount == 0:
		return "production-xray-absent"
	case result.ProductionCount == 1:
		return "ready"
	}
	return "inventory-unknown"
}

func isXrayProcessComm(comm string) bool {
	comm = strings.TrimSpace(comm)
	// Linux task comm is truncated to 15 characters. FreeNet isolated probe
	// binaries use a distinct alias and FREENET_XRAY_PROBE=1.
	return comm == "xray" || strings.HasPrefix(comm, "fn-xray")
}

func xrayProcState(procDir string) string {
	data, err := os.ReadFile(filepath.Join(procDir, "status"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "State:") {
			tokens := strings.Fields(line)
			if len(tokens) >= 2 {
				return tokens[1]
			}
			return ""
		}
	}
	return ""
}

func scanXrayProcessInventory(procRoot, configDir, binary string) xrayProcessInventoryResponse {
	result := xrayProcessInventoryResponse{Success: true, Mutation: "NONE"}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		result.Success = false
		result.State = "proc-unavailable"
		return result
	}
	configDir = filepath.Clean(configDir)
	binaryInfo, binaryErr := os.Stat(binary)
	reasonCounts := make(map[string]int)
	unknown := func(code string) {
		result.UnknownCount++
		// This map is intentionally populated exclusively with literals below.
		// No path or process data may enter a public reason code.
		reasonCounts[code]++
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		dir := filepath.Join(procRoot, entry.Name())
		data, err := os.ReadFile(filepath.Join(dir, "comm"))
		if os.IsNotExist(err) {
			continue // process retired while enumerating
		}
		if err != nil {
			unknown("process-comm-unreadable") // never infer safety
			continue
		}
		comm := strings.TrimSpace(string(data))
		if !isXrayProcessComm(comm) {
			continue
		}
		state := xrayProcState(dir)
		if state == "" {
			unknown("process-state-unavailable")
			continue
		}
		if state == "Z" || state == "X" || state == "x" {
			result.ZombieCount++
			continue
		}
		env, envErr := os.ReadFile(filepath.Join(dir, "environ"))
		if envErr != nil {
			unknown("process-environment-unavailable")
			continue
		}
		if envContains(env, isolatedXrayProbeFlag) {
			result.ProbeCount++
			continue
		}
		// Preserve the *exact* fail-closed admission contract; only split
		// its rejected cases into bounded, actionable read-only reason codes.
		switch {
		case comm != "xray":
			unknown("untrusted-xray-alias")
			continue
		case binaryErr != nil:
			unknown("expected-binary-unavailable")
			continue
		case configDir == ".":
			unknown("live-config-identity-unavailable")
			continue
		case !envContains(env, "XRAY_LOCATION_CONFDIR="+configDir):
			unknown("live-config-environment-mismatch")
			continue
		case !xrayCoreNativeCorrectGID(dir):
			unknown("routing-gid-mismatch")
			continue
		}
		exe, err := os.Stat(filepath.Join(dir, "exe"))
		if err != nil {
			unknown("process-executable-unavailable")
			continue
		}
		if !os.SameFile(binaryInfo, exe) {
			unknown("binary-inode-mismatch")
			continue
		}
		result.ProductionCount++
	}
	// Stable order makes UI diagnostics and regression tests deterministic.
	// Every key is a fixed enum, never an observed string.
	if len(reasonCounts) != 0 {
		codes := make([]string, 0, len(reasonCounts))
		for code := range reasonCounts {
			codes = append(codes, code)
		}
		sort.Strings(codes)
		for _, code := range codes {
			result.Reasons = append(result.Reasons, xrayProcessInventoryReason{
				Code: code, Count: reasonCounts[code],
			})
		}
	}
	result.State = xrayProcessInventoryState(result)
	result.RestartReady = result.State == "ready"
	return result
}

func (a *app) handleXrayProcessInventory(w http.ResponseWriter, r *http.Request) {
	binary, _, binErr := a.resolvedXrayCoreBinary()
	configDir, dirErr := a.xrayCoreLiveConfigDir()
	if binErr != nil || dirErr != nil {
		writeJSON(w, http.StatusServiceUnavailable, xrayProcessInventoryResponse{
			Success: false, Mutation: "NONE", State: "runtime-identity-unavailable",
		})
		return
	}
	response := scanXrayProcessInventory("/proc", configDir, binary)
	writeJSON(w, http.StatusOK, response)
}
