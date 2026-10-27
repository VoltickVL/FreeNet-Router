package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	xkeenStableReleaseURL = "https://api.github.com/repos/jameszeroX/XKeen/releases/latest"
	xkeenMetadataMaxBytes = 64 << 10
	xkeenCatalogMaxBytes  = 192 << 10
	xkeenCheckTimeout     = 9 * time.Second
)

// This endpoint only observes an existing installation and trusted upstream
// release metadata. It never invokes xkeen, executes the sourced shell file,
// downloads assets, or changes the installed core, DNS, firewall or VPN.
type xkeenUpdateCheckResponse struct {
	Success             bool   `json:"success"`
	Mutation            string `json:"mutation"`
	State               string `json:"state"`
	InstalledVersion    string `json:"installed_version,omitempty"`
	InstalledChannel    string `json:"installed_channel,omitempty"`
	LatestStableVersion string `json:"latest_stable_version,omitempty"`
	UpdateAvailable     bool   `json:"update_available"`
	ErrorCode           string `json:"error_code,omitempty"`
}

type xkeenGitHubStableRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name   string `json:"name"`
		Size   int64  `json:"size"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

func xkeenVersionParts(raw string) ([4]int, bool) {
	var parts [4]int
	chunks := strings.Split(raw, ".")
	if len(chunks) < 2 || len(chunks) > len(parts) {
		return parts, false
	}
	for i, chunk := range chunks {
		if len(chunk) == 0 || len(chunk) > 4 {
			return parts, false
		}
		for _, char := range chunk {
			if char < '0' || char > '9' {
				return parts, false
			}
		}
		n, err := strconv.Atoi(chunk)
		if err != nil || n > 9999 {
			return parts, false
		}
		parts[i] = n
	}
	return parts, true
}

func compareXKeenVersion(installed, latest string) (int, bool) {
	a, okA := xkeenVersionParts(installed)
	b, okB := xkeenVersionParts(latest)
	if !okA || !okB {
		return 0, false
	}
	for i := range a {
		if a[i] < b[i] {
			return -1, true
		}
		if a[i] > b[i] {
			return 1, true
		}
	}
	return 0, true
}

// Matching numeric versions are not proof of the installed Stable build:
// Beta and unknown-channel installations must remain visibly unverified.
func classifyXKeenUpdate(installed, channel, latest string) (string, bool, bool) {
	comparison, valid := compareXKeenVersion(installed, latest)
	if !valid {
		return "", false, false
	}
	switch {
	case comparison < 0:
		return "update-available", true, true
	case comparison > 0:
		return "newer-than-stable", false, true
	case channel == "stable":
		return "up-to-date", false, true
	default:
		return "same-version-build-unverified", false, true
	}
}

// Parse known assignments as *data*, not shell code. Duplicate or malformed
// versions fail closed; no arbitrary contents of this file leave the router.
func xkeenMetadataAssignment(raw, key string) (string, bool) {
	found := false
	value := ""
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, key+"=") {
			continue
		}
		if found {
			return "", false
		}
		found = true
		value = strings.TrimSpace(strings.TrimPrefix(line, key+"="))
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
	}
	return value, found && value != ""
}

func parseXKeenInstalledMetadata(raw []byte) (string, string, error) {
	version, ok := xkeenMetadataAssignment(string(raw), "xkeen_current_version")
	if !ok {
		return "", "", errors.New("XKeen version metadata unavailable")
	}
	if _, valid := xkeenVersionParts(version); !valid {
		return "", "", errors.New("XKeen version metadata invalid")
	}
	build, buildKnown := xkeenMetadataAssignment(string(raw), "xkeen_build")
	channel := "unknown"
	if buildKnown {
		switch strings.ToLower(build) {
		case "stable":
			channel = "stable"
		case "beta":
			channel = "beta"
		default:
			return "", "", errors.New("XKeen channel metadata invalid")
		}
	}
	return version, channel, nil
}

func readXKeenInstalledMetadata(binary string) (string, string, error) {
	info, err := os.Stat(binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return "", "", errors.New("XKeen installation unavailable")
	}
	path := filepath.Join(filepath.Dir(binary), ".xkeen", "01_info", "01_info_variable.sh")
	fileInfo, err := os.Stat(path)
	if err != nil || !fileInfo.Mode().IsRegular() || fileInfo.Size() > xkeenMetadataMaxBytes {
		return "", "", errors.New("XKeen metadata unavailable")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", "", errors.New("XKeen metadata unreadable")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, xkeenMetadataMaxBytes+1))
	if err != nil || len(data) > xkeenMetadataMaxBytes {
		return "", "", errors.New("XKeen metadata invalid")
	}
	return parseXKeenInstalledMetadata(data)
}

func parseXKeenStableRelease(data []byte) (string, error) {
	var release xkeenGitHubStableRelease
	if len(data) == 0 || len(data) > xkeenCatalogMaxBytes || json.Unmarshal(data, &release) != nil {
		return "", errors.New("XKeen release metadata invalid")
	}
	if release.Draft || release.Prerelease {
		return "", errors.New("XKeen release is not stable")
	}
	if _, valid := xkeenVersionParts(release.TagName); !valid {
		return "", errors.New("XKeen release tag invalid")
	}
	for _, asset := range release.Assets {
		if asset.Name != "xkeen.tar.gz" || asset.Size <= 0 {
			continue
		}
		digest := strings.ToLower(strings.TrimPrefix(asset.Digest, "sha256:"))
		if len(asset.Digest) != len("sha256:")+64 || !strings.HasPrefix(asset.Digest, "sha256:") || len(digest) != 64 {
			continue
		}
		if _, err := hex.DecodeString(digest); err == nil {
			return release.TagName, nil
		}
	}
	return "", errors.New("XKeen release asset digest unavailable")
}

func fetchXKeenStableRelease(ctx context.Context, client *http.Client, endpoint string) (string, error) {
	if ctx == nil || client == nil {
		return "", errors.New("XKeen catalog unavailable")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", errors.New("XKeen catalog request unavailable")
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "FreeNet-Router-XKeen-ReadOnly")
	response, err := client.Do(request)
	if err != nil {
		return "", errors.New("XKeen upstream unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", errors.New("XKeen upstream unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, xkeenCatalogMaxBytes+1))
	if err != nil || len(data) > xkeenCatalogMaxBytes {
		return "", errors.New("XKeen upstream response unavailable")
	}
	return parseXKeenStableRelease(data)
}

func (a *app) handleXKeenUpdateCheck(w http.ResponseWriter, r *http.Request) {
	result := xkeenUpdateCheckResponse{Mutation: "NONE", State: "unverified"}
	if a == nil {
		result.ErrorCode = "runtime-unavailable"
		writeJSON(w, http.StatusServiceUnavailable, result)
		return
	}
	installed, channel, err := readXKeenInstalledMetadata(a.cfg.XKeenPath)
	if err != nil {
		result.ErrorCode = "installed-version-unavailable"
		writeJSON(w, http.StatusServiceUnavailable, result)
		return
	}
	result.InstalledVersion = installed
	result.InstalledChannel = channel
	ctx, cancel := context.WithTimeout(r.Context(), xkeenCheckTimeout)
	defer cancel()
	client := &http.Client{Timeout: xkeenCheckTimeout}
	latest, err := fetchXKeenStableRelease(ctx, client, xkeenStableReleaseURL)
	if err != nil {
		result.ErrorCode = "upstream-check-unavailable"
		writeJSON(w, http.StatusBadGateway, result)
		return
	}
	state, update, valid := classifyXKeenUpdate(installed, channel, latest)
	if !valid {
		result.ErrorCode = "version-comparison-unavailable"
		writeJSON(w, http.StatusServiceUnavailable, result)
		return
	}
	result.LatestStableVersion = latest
	result.Success = true
	result.State = state
	result.UpdateAvailable = update
	writeJSON(w, http.StatusOK, result)
}
