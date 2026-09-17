package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	xrayCoreReleasesURL      = "https://api.github.com/repos/XTLS/Xray-core/releases?per_page=20"
	xrayCoreMaxCatalogItems  = 16
	xrayCoreMaxArchiveBytes  = 128 << 20
	xrayCoreMaxBinaryBytes   = 160 << 20
	xrayCoreOperationTimeout = 95 * time.Second
)

var xrayCoreHTTPClient = &http.Client{Timeout: 55 * time.Second}

type xrayCoreAsset struct {
	Name      string `json:"name"`
	URL       string `json:"browser_download_url"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	Available bool   `json:"available,omitempty"`
}

type xrayCoreRelease struct {
	Version     string        `json:"version"`
	PublishedAt string        `json:"published_at,omitempty"`
	Prerelease  bool          `json:"prerelease"`
	Current     bool          `json:"current"`
	Latest      bool          `json:"latest"`
	Description string        `json:"description,omitempty"`
	Asset       xrayCoreAsset `json:"asset"`
}

type xrayCoreCatalogResponse struct {
	Success        bool              `json:"success"`
	CurrentVersion string            `json:"current_version,omitempty"`
	LatestVersion  string            `json:"latest_version,omitempty"`
	Architecture   string            `json:"architecture"`
	AssetName      string            `json:"asset_name"`
	Releases       []xrayCoreRelease `json:"releases"`
	Error          string            `json:"error,omitempty"`
}

type xrayCoreApplyRequest struct {
	TargetVersion string `json:"target_version"`
}

type xrayCoreApplyResponse struct {
	Success        bool              `json:"success"`
	Action         string            `json:"action,omitempty"`
	Previous       string            `json:"previous_version,omitempty"`
	CurrentVersion string            `json:"current_version,omitempty"`
	TargetVersion  string            `json:"target_version,omitempty"`
	Rollback       string            `json:"rollback"`
	Message        string            `json:"message,omitempty"`
	Events         []automationEvent `json:"events"`
	Error          string            `json:"error,omitempty"`
}

type xrayCoreGitHubRelease struct {
	TagName     string `json:"tag_name"`
	PublishedAt string `json:"published_at"`
	Prerelease  bool   `json:"prerelease"`
	Draft       bool   `json:"draft"`
	Body        string `json:"body"`
	Assets      []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Digest string `json:"digest"`
		Size   int64  `json:"size"`
	} `json:"assets"`
}

func xrayCoreAssetName(goarch string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(goarch)) {
	case "amd64":
		return "Xray-linux-64.zip", true
	case "386":
		return "Xray-linux-32.zip", true
	case "arm64":
		return "Xray-linux-arm64-v8a.zip", true
	case "arm":
		return "Xray-linux-arm32-v7a.zip", true
	case "mipsle":
		return "Xray-linux-mips32le.zip", true
	case "mips":
		return "Xray-linux-mips32.zip", true
	default:
		return "", false
	}
}

func validXrayCoreTag(tag string) bool {
	tag = strings.TrimSpace(tag)
	if len(tag) < 2 || len(tag) > 48 || tag[0] != 'v' {
		return false
	}
	if tag[1] < '0' || tag[1] > '9' {
		return false
	}
	for _, r := range tag[1:] {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '.' || r == '-' || r == '+' {
			continue
		}
		return false
	}
	return true
}

func normalizeXrayCoreTag(raw string) string {
	line := strings.TrimSpace(strings.SplitN(raw, "\n", 2)[0])
	for _, prefix := range []string{"Xray-core ", "Xray-core", "Xray "} {
		line = strings.TrimSpace(strings.TrimPrefix(line, prefix))
	}
	field := strings.Fields(line)
	if len(field) == 0 {
		return ""
	}
	tag := strings.TrimSpace(field[0])
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	if !validXrayCoreTag(tag) {
		return ""
	}
	return tag
}

func normalizeXrayCoreDigest(raw string) (string, bool) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	raw = strings.TrimPrefix(raw, "sha256:")
	if len(raw) != 64 {
		return "", false
	}
	if _, err := hex.DecodeString(raw); err != nil {
		return "", false
	}
	return raw, true
}

func xrayCoreReleaseSummary(body string) string {
	body = strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(body)
	body = strings.Join(strings.Fields(body), " ")
	if body == "" {
		return ""
	}
	runes := []rune(body)
	if len(runes) > 420 {
		return strings.TrimSpace(string(runes[:417])) + "…"
	}
	return body
}

func parseXrayCoreCatalog(raw []byte, current, assetName string) (xrayCoreCatalogResponse, error) {
	var upstream []xrayCoreGitHubRelease
	if err := json.Unmarshal(raw, &upstream); err != nil {
		return xrayCoreCatalogResponse{}, errors.New("invalid Xray release catalog")
	}
	resp := xrayCoreCatalogResponse{Success: true, CurrentVersion: current, Architecture: runtime.GOARCH, AssetName: assetName, Releases: []xrayCoreRelease{}}
	latestSet := false
	for _, release := range upstream {
		if release.Draft || !validXrayCoreTag(release.TagName) {
			continue
		}
		var asset xrayCoreAsset
		for _, candidate := range release.Assets {
			if candidate.Name != assetName {
				continue
			}
			digest, digestOK := normalizeXrayCoreDigest(candidate.Digest)
			urlOK := strings.HasPrefix(candidate.URL, "https://github.com/XTLS/Xray-core/releases/download/")
			asset = xrayCoreAsset{Name: candidate.Name, URL: candidate.URL, Digest: digest, Size: candidate.Size, Available: digestOK && urlOK && candidate.Size > 0 && candidate.Size <= xrayCoreMaxArchiveBytes}
			break
		}
		entry := xrayCoreRelease{Version: release.TagName, PublishedAt: release.PublishedAt, Prerelease: release.Prerelease, Current: release.TagName == current, Description: xrayCoreReleaseSummary(release.Body), Asset: asset}
		if !latestSet && !release.Prerelease && asset.Available {
			entry.Latest = true
			resp.LatestVersion = release.TagName
			latestSet = true
		}
		resp.Releases = append(resp.Releases, entry)
		if len(resp.Releases) >= xrayCoreMaxCatalogItems {
			break
		}
	}
	if len(resp.Releases) == 0 {
		return xrayCoreCatalogResponse{}, errors.New("no compatible Xray releases found")
	}
	return resp, nil
}

func (a *app) resolvedXrayCoreBinary() (string, os.FileInfo, error) {
	path := strings.TrimSpace(a.routingXrayBin())
	if path == "" {
		return "", nil, errors.New("Xray binary path is unavailable")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", nil, errors.New("Xray binary is unavailable")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return "", nil, errors.New("Xray binary is unavailable")
	}
	return resolved, info, nil
}

func xrayCoreVersionAt(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, path, "version")
	out, err := cmd.Output()
	if err != nil || ctx.Err() != nil {
		return "", errors.New("cannot read Xray version")
	}
	tag := normalizeXrayCoreTag(string(out))
	if tag == "" {
		return "", errors.New("cannot parse Xray version")
	}
	return tag, nil
}

func (a *app) fetchXrayCoreCatalog(ctx context.Context) (xrayCoreCatalogResponse, error) {
	assetName, ok := xrayCoreAssetName(runtime.GOARCH)
	if !ok {
		return xrayCoreCatalogResponse{}, fmt.Errorf("Xray update is not supported for %s", runtime.GOARCH)
	}
	binary, _, err := a.resolvedXrayCoreBinary()
	if err != nil {
		return xrayCoreCatalogResponse{}, err
	}
	current, err := xrayCoreVersionAt(ctx, binary)
	if err != nil {
		return xrayCoreCatalogResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, xrayCoreReleasesURL, nil)
	if err != nil {
		return xrayCoreCatalogResponse{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "FreeNet-Router")
	response, err := xrayCoreHTTPClient.Do(req)
	if err != nil {
		return xrayCoreCatalogResponse{}, errors.New("Xray release catalog is unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return xrayCoreCatalogResponse{}, errors.New("Xray release catalog is unavailable")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return xrayCoreCatalogResponse{}, errors.New("cannot read Xray release catalog")
	}
	return parseXrayCoreCatalog(body, current, assetName)
}

func (a *app) handleXrayCoreCatalog(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	catalog, err := a.fetchXrayCoreCatalog(ctx)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, xrayCoreCatalogResponse{Success: false, Architecture: runtime.GOARCH, Releases: []xrayCoreRelease{}, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, catalog)
}

func decodeXrayCoreApply(w http.ResponseWriter, r *http.Request) (string, bool) {
	failure := func(status int, message string) (string, bool) {
		writeJSON(w, status, xrayCoreApplyResponse{Success: false, Rollback: "NOT_NEEDED", Events: xrayServiceEvents(8), Error: message})
		return "", false
	}
	if !sameOrigin(r) {
		return failure(http.StatusForbidden, "cross-origin request rejected")
	}
	if ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))); !strings.HasPrefix(ct, "application/json") {
		return failure(http.StatusUnsupportedMediaType, "application/json required")
	}
	body := http.MaxBytesReader(w, r.Body, 8<<10)
	defer body.Close()
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var req xrayCoreApplyRequest
	if err := dec.Decode(&req); err != nil || !validXrayCoreTag(strings.TrimSpace(req.TargetVersion)) {
		return failure(http.StatusBadRequest, "invalid Xray target version")
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return failure(http.StatusBadRequest, "invalid Xray target version")
	}
	return strings.TrimSpace(req.TargetVersion), true
}

func findXrayCoreRelease(catalog xrayCoreCatalogResponse, target string) (xrayCoreRelease, bool) {
	for _, release := range catalog.Releases {
		if release.Version == target {
			return release, true
		}
	}
	return xrayCoreRelease{}, false
}

func downloadXrayCoreArchive(ctx context.Context, release xrayCoreRelease, dir string) (string, error) {
	if !release.Asset.Available {
		return "", errors.New("selected Xray release has no verified asset for this router")
	}
	expected, ok := normalizeXrayCoreDigest(release.Asset.Digest)
	if !ok {
		return "", errors.New("selected Xray asset has no trusted SHA-256 digest")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, release.Asset.URL, nil)
	if err != nil {
		return "", errors.New("cannot prepare Xray download")
	}
	req.Header.Set("User-Agent", "FreeNet-Router")
	response, err := xrayCoreHTTPClient.Do(req)
	if err != nil {
		return "", errors.New("cannot download selected Xray release")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", errors.New("cannot download selected Xray release")
	}
	file, err := os.CreateTemp(dir, ".freenet-xray-*.zip")
	if err != nil {
		return "", errors.New("cannot stage Xray archive")
	}
	path := file.Name()
	cleanup := true
	defer func() {
		_ = file.Close()
		if cleanup {
			_ = os.Remove(path)
		}
	}()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, xrayCoreMaxArchiveBytes+1))
	if err != nil || written <= 0 || written > xrayCoreMaxArchiveBytes {
		return "", errors.New("Xray archive download is invalid or too large")
	}
	if err := file.Sync(); err != nil {
		return "", errors.New("cannot persist staged Xray archive")
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != expected {
		return "", errors.New("Xray archive SHA-256 verification failed")
	}
	cleanup = false
	return path, nil
}

func extractXrayCoreCandidate(archivePath, dir string) (string, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", errors.New("Xray archive is invalid")
	}
	defer reader.Close()
	var source *zip.File
	for _, file := range reader.File {
		if file.FileInfo().IsDir() || filepath.Base(file.Name) != "xray" {
			continue
		}
		if file.UncompressedSize64 == 0 || file.UncompressedSize64 > xrayCoreMaxBinaryBytes {
			return "", errors.New("Xray binary in archive is invalid or too large")
		}
		source = file
		break
	}
	if source == nil {
		return "", errors.New("Xray binary is missing from archive")
	}
	input, err := source.Open()
	if err != nil {
		return "", errors.New("cannot read Xray binary from archive")
	}
	defer input.Close()
	candidate, err := os.CreateTemp(dir, ".freenet-xray-candidate-*")
	if err != nil {
		return "", errors.New("cannot stage Xray binary")
	}
	path := candidate.Name()
	cleanup := true
	defer func() {
		_ = candidate.Close()
		if cleanup {
			_ = os.Remove(path)
		}
	}()
	written, err := io.Copy(candidate, io.LimitReader(input, xrayCoreMaxBinaryBytes+1))
	if err != nil || written <= 0 || written > xrayCoreMaxBinaryBytes {
		return "", errors.New("cannot extract Xray binary")
	}
	if err := candidate.Chmod(0755); err != nil || candidate.Sync() != nil {
		return "", errors.New("cannot prepare staged Xray binary")
	}
	cleanup = false
	return path, nil
}

func (a *app) validateXrayCoreCandidate(parent context.Context, candidate, target string) error {
	ctx, cancel := context.WithTimeout(parent, 22*time.Second)
	defer cancel()
	version, err := xrayCoreVersionAt(ctx, candidate)
	if err != nil || version != target {
		return errors.New("downloaded Xray binary does not match selected version")
	}
	tmpDir, err := os.MkdirTemp("", "freenet-xray-core-candidate.*")
	if err != nil {
		return errors.New("cannot create Xray candidate workspace")
	}
	defer os.RemoveAll(tmpDir)
	if err := copyRoutingCandidateBase(a.routingConfigDir(), tmpDir); err != nil {
		return errors.New("cannot prepare current Xray configuration for validation")
	}
	cmd := exec.CommandContext(ctx, candidate, "run", "-test", "-confdir", tmpDir)
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+a.routingAssetDir())
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil || ctx.Err() != nil {
		return errors.New("selected Xray version is not compatible with the current configuration")
	}
	return nil
}

func copyXrayCoreBinary(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = out.Close()
		if !ok {
			_ = os.Remove(dst)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	ok = true
	return nil
}

func xrayCoreAction(previous, target string) string {
	if previous == target {
		return "none"
	}
	return "change"
}

func (a *app) rollbackXrayCore(parent context.Context, binary, backup, previous string) error {
	if err := os.Rename(backup, binary); err != nil {
		return errors.New("previous Xray binary could not be restored")
	}
	if err := a.restartXrayControlled(parent); err != nil {
		return errors.New("previous Xray binary was restored, but restart/post-check failed")
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	version, err := xrayCoreVersionAt(ctx, binary)
	if err != nil || version != previous {
		return errors.New("rollback Xray version could not be confirmed")
	}
	return nil
}

func (a *app) applyXrayCore(parent context.Context, target string) xrayCoreApplyResponse {
	result := xrayCoreApplyResponse{Success: false, TargetVersion: target, Rollback: "NOT_NEEDED", Events: xrayServiceEvents(8)}
	ctx, cancel := context.WithTimeout(parent, xrayCoreOperationTimeout)
	defer cancel()

	binary, info, err := a.resolvedXrayCoreBinary()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	previous, err := xrayCoreVersionAt(ctx, binary)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Previous = previous
	result.CurrentVersion = previous
	result.Action = xrayCoreAction(previous, target)
	if previous == target {
		result.Error = "selected Xray version is already installed"
		return result
	}
	if err := a.validateConfigStudioLive(ctx); err != nil {
		result.Error = "current Xray configuration is invalid; version change cancelled"
		return result
	}
	before := a.status()
	catalog, err := a.fetchXrayCoreCatalog(ctx)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	release, ok := findXrayCoreRelease(catalog, target)
	if !ok || !release.Asset.Available {
		result.Error = "selected Xray version is not available for this router"
		return result
	}
	archivePath, err := downloadXrayCoreArchive(ctx, release, filepath.Dir(binary))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer os.Remove(archivePath)
	candidatePath, err := extractXrayCoreCandidate(archivePath, filepath.Dir(binary))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	candidateOwned := true
	defer func() {
		if candidateOwned {
			_ = os.Remove(candidatePath)
		}
	}()
	if err := a.validateXrayCoreCandidate(ctx, candidatePath, target); err != nil {
		result.Error = err.Error()
		return result
	}
	backupFile, err := os.CreateTemp(filepath.Dir(binary), ".freenet-xray-backup-*")
	if err != nil {
		result.Error = "cannot create Xray rollback snapshot"
		return result
	}
	backupPath := backupFile.Name()
	_ = backupFile.Close()
	_ = os.Remove(backupPath)
	if err := copyXrayCoreBinary(binary, backupPath, info.Mode()); err != nil {
		result.Error = "cannot create Xray rollback snapshot"
		return result
	}
	backupOwned := true
	defer func() {
		if backupOwned {
			_ = os.Remove(backupPath)
		}
	}()
	if err := os.Chmod(candidatePath, info.Mode().Perm()); err != nil {
		result.Error = "cannot prepare Xray candidate permissions"
		return result
	}
	if err := os.Rename(candidatePath, binary); err != nil {
		result.Error = "atomic Xray binary replacement failed"
		return result
	}
	candidateOwned = false

	applyErr := a.restartXrayControlled(ctx)
	if applyErr == nil {
		var confirmed string
		confirmed, applyErr = xrayCoreVersionAt(ctx, binary)
		if applyErr == nil && confirmed != target {
			applyErr = errors.New("installed Xray version does not match selected target")
		}
		if applyErr == nil {
			after := a.status()
			if !after.XrayOnline {
				applyErr = errors.New("Xray is offline after version change")
			} else if before.Endpoint != "" && before.Endpoint != "—" && after.Endpoint != "" && after.Endpoint != "—" && before.Endpoint != after.Endpoint {
				applyErr = errors.New("VPN endpoint changed unexpectedly after Xray version change")
			}
		}
	}
	if applyErr != nil {
		if rbErr := a.rollbackXrayCore(ctx, binary, backupPath, previous); rbErr != nil {
			backupOwned = false
			result.Rollback = "FAILED"
			result.Error = sanitizeAutomationReason(applyErr.Error() + "; rollback failed: " + rbErr.Error())
			v3AppendEvent("xray", "failed", "Изменение версии Xray не завершено; автоматический откат не подтверждён. STOP.")
			result.Events = xrayServiceEvents(8)
			return result
		}
		backupOwned = false
		result.Rollback = "SUCCESS"
		result.CurrentVersion = previous
		result.Error = sanitizeAutomationReason(applyErr.Error() + "; предыдущая версия восстановлена")
		v3AppendEvent("xray", "failed", "Изменение версии Xray отменено; предыдущая версия восстановлена.")
		result.Events = xrayServiceEvents(8)
		return result
	}

	result.Success = true
	result.Rollback = "NOT_NEEDED"
	result.CurrentVersion = target
	if target == catalog.LatestVersion {
		result.Message = "Xray обновлён до " + target + "."
	} else {
		result.Message = "Xray переключён: " + previous + " → " + target + "."
	}
	v3AppendEvent("xray", "success", result.Message)
	result.Events = xrayServiceEvents(8)
	return result
}

func (a *app) handleXrayCoreApply(w http.ResponseWriter, r *http.Request) {
	if a.mutationBlockedBySelfUpdate(w) {
		return
	}
	target, ok := decodeXrayCoreApply(w, r)
	if !ok {
		return
	}
	select {
	case a.sem <- struct{}{}:
		defer func() { <-a.sem }()
	default:
		writeJSON(w, http.StatusConflict, xrayCoreApplyResponse{Success: false, TargetVersion: target, Rollback: "NOT_NEEDED", Events: xrayServiceEvents(8), Error: "другая операция FreeNet уже выполняется"})
		return
	}
	result := a.applyXrayCore(r.Context(), target)
	if result.Success {
		writeJSON(w, http.StatusOK, result)
		return
	}
	if result.Rollback == "FAILED" {
		writeJSON(w, http.StatusInternalServerError, result)
		return
	}
	if result.Rollback == "SUCCESS" {
		writeJSON(w, http.StatusBadGateway, result)
		return
	}
	writeJSON(w, http.StatusUnprocessableEntity, result)
}
