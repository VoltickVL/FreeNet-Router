package main

import (
	"bufio"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
)

const splitDNSMinMemoryMiB int64 = 768

var splitDNSMemoryGateReason string

type hardwareCapabilitiesResponse struct {
	Success           bool   `json:"success"`
	MemoryTotalMiB    int64  `json:"memory_total_mib"`
	SplitDNSSupported bool   `json:"split_dns_supported"`
	SplitDNSMinMiB    int64  `json:"split_dns_min_mib"`
	Reason            string `json:"reason,omitempty"`
}

func hardwareMemInfoPath() string {
	if path := strings.TrimSpace(os.Getenv("FREENET_MEMINFO_PATH")); path != "" {
		return path
	}
	return "/proc/meminfo"
}

func readMemTotalKiB(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || fields[0] != "MemTotal:" {
			continue
		}
		value, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || value <= 0 {
			return 0, errors.New("invalid MemTotal")
		}
		if len(fields) >= 3 && !strings.EqualFold(fields[2], "kB") {
			return 0, errors.New("unsupported MemTotal unit")
		}
		return value, nil
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return 0, errors.New("MemTotal not found")
}

func currentHardwareCapabilities() hardwareCapabilitiesResponse {
	result := hardwareCapabilitiesResponse{
		Success:        true,
		SplitDNSMinMiB: splitDNSMinMemoryMiB,
	}
	memKiB, err := readMemTotalKiB(hardwareMemInfoPath())
	if err != nil {
		result.Success = false
		result.SplitDNSSupported = false
		result.Reason = "не удалось безопасно определить объём RAM; XKeen/Xray DNS заблокирован"
		return result
	}
	result.MemoryTotalMiB = memKiB / 1024
	result.SplitDNSSupported = memKiB >= splitDNSMinMemoryMiB*1024
	if !result.SplitDNSSupported {
		result.Reason = fmt.Sprintf("XKeen/Xray DNS требует не менее %d MiB RAM; обнаружено %d MiB. Используйте DNS напрямую через роутер", splitDNSMinMemoryMiB, result.MemoryTotalMiB)
	}
	return result
}

func applySplitDNSMemoryGate(capability hardwareCapabilitiesResponse) {
	if capability.SplitDNSSupported {
		splitDNSMemoryGateReason = ""
		return
	}

	splitDNSMemoryGateReason = capability.Reason
	delete(dnsModes, "xkeen")
	for _, id := range []string{"vladlink", "alliancetelecom"} {
		profile, ok := ispProfiles[id]
		if !ok || profile.RecommendedDNSMode != "xkeen" {
			continue
		}
		profile.RecommendedDNSMode = "firmware"
		ispProfiles[id] = profile
	}
}

func init() {
	applySplitDNSMemoryGate(currentHardwareCapabilities())
}

func registerHardwareCapabilityAPI(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /api/capabilities", a.requireAuth(a.handleHardwareCapabilities))
}

func (a *app) handleHardwareCapabilities(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, currentHardwareCapabilities())
}
