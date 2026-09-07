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

// The capability gate changes what FreeNet may select next, but it deliberately
// keeps dnsModes["xkeen"] intact. A 512 MiB router can already be in Split due to
// an older release; preserving that value lets status/UI report the real active
// state while the user performs one controlled transition back to native DNS.
func applySplitDNSMemoryGate(capability hardwareCapabilitiesResponse) {
	if capability.SplitDNSSupported {
		splitDNSMemoryGateReason = ""
		return
	}

	splitDNSMemoryGateReason = capability.Reason
	for _, id := range []string{"vladlink", "alliancetelecom"} {
		profile, ok := ispProfiles[id]
		if !ok || profile.RecommendedDNSMode != "xkeen" {
			continue
		}
		profile.RecommendedDNSMode = "firmware"
		ispProfiles[id] = profile
	}
}

func splitDNSSelectionError(dnsMode string) error {
	if strings.TrimSpace(dnsMode) != "xkeen" {
		return nil
	}
	capability := currentHardwareCapabilities()
	if capability.SplitDNSSupported {
		return nil
	}
	if capability.Reason != "" {
		return errors.New(capability.Reason)
	}
	return errors.New("XKeen/Xray DNS заблокирован для этого устройства")
}

func init() {
	applySplitDNSMemoryGate(currentHardwareCapabilities())
}

// Capability discovery is intentionally available before Control Center login.
// The Network page is mounted before authentication and must be able to decide
// whether Split DNS may be offered without entering a finite 401 retry loop.
// Pre-auth responses expose only the boolean capability and threshold; the exact
// RAM amount and detailed failure reason remain available only after login.
func registerHardwareCapabilityAPI(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /api/capabilities", a.handleHardwareCapabilities)
}

func (a *app) handleHardwareCapabilities(w http.ResponseWriter, r *http.Request) {
	capability := currentHardwareCapabilities()
	if !a.isAuthenticated(r) {
		capability.MemoryTotalMiB = 0
		if !capability.SplitDNSSupported {
			capability.Reason = "XKeen/Xray DNS недоступен для этого устройства"
		} else {
			capability.Reason = ""
		}
	}
	writeJSON(w, http.StatusOK, capability)
}
