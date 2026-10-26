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
	xrayCoreMaxCatalogBytes  = 8 << 20
	xrayCoreMaxArchiveBytes  = 128 << 20
	xrayCoreMaxBinaryBytes   = 160 << 20
	xrayCoreOperationTimeout = 95 * time.Second // read-only catalog, download and validation
    xrayCoreSemanticTimeout = 205 * time.Second // two progress-bounded tests + snapshot under owned fence
    xrayCoreSingleTestTimeout = 35 * time.Second // unchanged initial budget AFTER admission
    xrayCoreSingleTestMaximumTimeout = 95 * time.Second // only if verified child CPU progress
    xrayCoreTestProgressWindow = 5 * time.Second // CPU must advance near soft deadline
)

var xrayCoreHTTPClient = newXrayCoreDirectHTTPClient()

// Separate forward and rollback windows allow controlled recovery even if
// the browser disconnects. Tests shorten them to exercise hard failures.
var (
    xrayCoreApplyTimeout = 30 * time.Second
    xrayCoreRollbackTimeout = 30 * time.Second
)

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

func stripXrayCoreMarkdownLinks(line string) string {
	for {
		open := strings.Index(line, "[")
		if open < 0 {
			break
		}
		midRel := strings.Index(line[open:], "](")
		if midRel < 0 {
			break
		}
		mid := open + midRel
		closeRel := strings.Index(line[mid+2:], ")")
		if closeRel < 0 {
			break
		}
		close := mid + 2 + closeRel
		label := line[open+1 : mid]
		if strings.HasPrefix(label, "!") || strings.Contains(strings.ToLower(label), "<img") {
			label = ""
		}
		line = line[:open] + label + line[close+1:]
	}
	return strings.TrimSpace(strings.NewReplacer("**", "", "__", "", "`", "", "<br>", " ", "<br/>", " ").Replace(line))
}

func xrayCoreReleaseSummary(body string) string {
	body = strings.ReplaceAll(body, "\r", "")
	lines := strings.Split(body, "\n")
	parts := make([]string, 0, 3)
	blockedSection := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			heading := strings.ToLower(strings.TrimSpace(strings.TrimLeft(line, "#")))
			blockedSection = strings.Contains(heading, "sponsor") || strings.Contains(heading, "donation") || strings.Contains(heading, "nft")
			if blockedSection {
				continue
			}
			line = strings.TrimSpace(strings.TrimLeft(line, "#"))
		}
		if blockedSection || strings.HasPrefix(line, "[![") || strings.HasPrefix(line, "[<img") || strings.HasPrefix(strings.ToLower(line), "<img") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "see http://") || strings.HasPrefix(lower, "see https://") {
			target := line
			if i := strings.LastIndex(target, "/"); i >= 0 && i+1 < len(target) {
				target = strings.TrimSpace(target[i+1:])
			}
			if validXrayCoreTag(target) {
				return "Описание изменений объединено с релизом " + target + " upstream."
			}
			return "Описание изменений upstream опубликовано в связанном релизе."
		}
		line = stripXrayCoreMarkdownLinks(line)
		line = strings.TrimSpace(strings.TrimLeft(line, "-*•> "))
		lower = strings.ToLower(line)
		if line == "" || strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") ||
			strings.Contains(lower, "sponsor xray") || strings.Contains(lower, "trx(") || strings.Contains(lower, "usdt") ||
			strings.Contains(lower, "btc:") || strings.Contains(lower, "xmr:") || strings.Contains(lower, "nft") {
			continue
		}
		if i := strings.Index(line, " http://"); i > 0 {
			line = strings.TrimSpace(line[:i])
		} else if i := strings.Index(line, " https://"); i > 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		parts = append(parts, line)
		if len(parts) >= 3 {
			break
		}
	}
	if len(parts) == 0 {
		return "Описание изменений upstream не опубликовано."
	}
	summary := strings.Join(parts, " • ")
	runes := []rune(summary)
	if len(runes) > 360 {
		return strings.TrimSpace(string(runes[:357])) + "…"
	}
	return summary
}

func readXrayCoreCatalogBody(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, xrayCoreMaxCatalogBytes+1))
	if err != nil {
		return nil, errors.New("cannot read Xray release catalog")
	}
	if len(body) > xrayCoreMaxCatalogBytes {
		return nil, errors.New("Xray release catalog is too large")
	}
	return body, nil
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
	body, err := a.fetchXrayCoreCatalogResilient(ctx)
	if err != nil {
		return xrayCoreCatalogResponse{}, err
	}
	return parseXrayCoreCatalog(body, current, assetName)
}

func (a *app) handleXrayCoreCatalog(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), xrayCoreCatalogRequestTimeout)
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
	return downloadXrayCoreArchiveWithClient(ctx, release, dir, xrayCoreHTTPClient)
}

func downloadXrayCoreArchiveWithClient(ctx context.Context, release xrayCoreRelease, dir string, client *http.Client) (string, error) {
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
	response, err := client.Do(req)
	if err != nil {
		return "", xrayCoreDownloadNetworkError(ctx, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", xrayCoreDownloadHTTPError(response.StatusCode)
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
	if err != nil {
		return "", xrayCoreDownloadError("stream", true)
	}
	if written <= 0 || written > xrayCoreMaxArchiveBytes || (release.Asset.Size > 0 && written != release.Asset.Size) {
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

// The live directory is authoritative for an installed XKeen/Xray stack.
// Copying only regular *.json files discards relative assets, subdirectories
// and symlinked config fragments, making an otherwise running core appear
// invalid. The version upgrade is read-only until its separate swap phase.
func (a *app) xrayCoreLiveConfigDir() (string, error) {
	dir := a.routingConfigDir()
	if !filepath.IsAbs(dir) {
		return "", errors.New("reason=xray-live-config-path-unavailable; Xray binary unchanged")
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", errors.New("reason=xray-live-config-unavailable; Xray binary unchanged")
	}
	return dir, nil
}

// runIsolatedXrayCoreTest must only be called by the version-changing worker
// while it owns the *exclusive* isolated-probe fence. It must not reacquire a
// shared lease, which would deadlock against its own exclusive ownership.
// No production config or process is modified by the -test child.
func (a *app) runIsolatedXrayCoreTest(ctx context.Context, binary, configDir string) error {
    dir, err := os.MkdirTemp("", "freenet-xray-core-check-")
    if err != nil { return errors.New("cannot stage isolated Xray validation") }
    defer os.RemoveAll(dir)
    probePath, err := resolvedIsolatedXrayProbePath(dir, binary)
    if err != nil { return errors.New("Xray validation binary is unavailable") }
    env := isolatedXrayProbeEnv(append(os.Environ(), "XRAY_LOCATION_ASSET="+a.routingAssetDir()))
    return runXrayCoreProgressiveSemanticTest(
        ctx, probePath, configDir, env,
        xrayCoreSingleTestTimeout, xrayCoreSingleTestMaximumTimeout,
        xrayCoreTestProgressWindow, readXrayCoreCPUJiffies,
    )
}

// The installed binary must pass the current on-disk live config BEFORE swap.
// This check is now invoked only after the update worker drained and owns all
// probe leases. Background quality scans cannot spend the test deadline.
func (a *app) validateXrayCoreBaseline(parent context.Context, binary string) error {
    configDir, err := a.xrayCoreLiveConfigDir()
    if err != nil { return err }
    ctx, cancel := context.WithTimeout(parent, xrayCoreSingleTestMaximumTimeout)
    defer cancel()
    err = a.runIsolatedXrayCoreTest(ctx, binary, configDir)
    if err == nil { return nil }
    switch {
    case errors.Is(err, errXrayCoreSemanticIdle):
        return errors.New("reason=xray-live-test-idle; Xray binary unchanged")
    case errors.Is(err, errXrayCoreSemanticProgressDeadline):
        return errors.New("reason=xray-live-test-progress-timeout; Xray binary unchanged")
    case ctx.Err() != nil:
        return errors.New("reason=xray-live-test-timeout; Xray binary unchanged")
    case errors.Is(err, exec.ErrNotFound), errors.Is(err, os.ErrNotExist):
        return errors.New("reason=xray-test-executable-unavailable; Xray binary unchanged")
    default:
        return errors.New("reason=xray-live-config-test-failed; Xray binary unchanged")
    }
}

func (a *app) validateXrayCoreCandidate(parent context.Context, candidate, target string) error {
    ctx, cancel := context.WithTimeout(parent, xrayCoreSingleTestMaximumTimeout)
    defer cancel()
    version, err := xrayCoreVersionAt(ctx, candidate)
    if err != nil || version != target {
        return errors.New("downloaded Xray binary does not match selected version")
    }
    configDir, err := a.xrayCoreLiveConfigDir()
    if err != nil { return err }
    err = a.runIsolatedXrayCoreTest(ctx, candidate, configDir)
    if err != nil || ctx.Err() != nil {
        if errors.Is(err, errXrayCoreSemanticIdle) {
            return errors.New("reason=xray-candidate-test-idle; Xray binary unchanged")
        }
        if errors.Is(err, errXrayCoreSemanticProgressDeadline) {
            return errors.New("reason=xray-candidate-test-progress-timeout; Xray binary unchanged")
        }
        if ctx.Err() != nil {
            return errors.New("reason=xray-candidate-test-timeout; Xray binary unchanged or rollback required")
        }
        return errors.New("reason=xray-candidate-config-test-failed; selected Xray version is not compatible with the live configuration")
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

// The FreeNet-owned process lifecycle is implemented in xray_core_native.go.
// Never repeat a long isolated config test AFTER a production restart.
// Rollback must never inherit a canceled request, exhausted download deadline
// or forward-restart deadline. Always reserve its own bounded work window.
func (a *app) rollbackXrayCore(binary, backup, previous string) error {
	ctx, cancel := context.WithTimeout(context.Background(), xrayCoreRollbackTimeout)
	defer cancel()
	// Preserve the original snapshot until rollback is confirmed. Renaming
	// the backup over the live binary would consume the only recovery copy
	// even when restart/post-check subsequently failed.
	backupInfo, err := os.Stat(backup)
	if err != nil || !backupInfo.Mode().IsRegular() {
		return errors.New("previous Xray backup is unavailable")
	}
	restoreFile, err := os.CreateTemp(filepath.Dir(binary), ".freenet-xray-restore-*")
	if err != nil {
		return errors.New("cannot stage previous Xray binary")
	}
	restorePath := restoreFile.Name()
	_ = restoreFile.Close()
	_ = os.Remove(restorePath)
	defer os.Remove(restorePath)
	if err := copyXrayCoreBinary(backup, restorePath, backupInfo.Mode()); err != nil {
		return errors.New("previous Xray binary could not be staged")
	}
	if err := os.Rename(restorePath, binary); err != nil {
		return errors.New("previous Xray binary could not be restored")
	}
	if err := a.restartXrayCoreUpdateOwned(ctx, binary, previous); err != nil {
		return errors.New("previous Xray binary was restored, but restart/post-check failed")
	}
	verifyCtx, verifyCancel := context.WithTimeout(ctx, 5*time.Second)
	defer verifyCancel()
	version, err := xrayCoreVersionAt(verifyCtx, binary)
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
	// FreeNet takes responsibility for this Xray version-change transaction.
	// Before download, verify the existing production process and routing GID
	// 11111 without requiring XKeen UI or its CLI.
	if err := xrayCoreNativePreflight(ctx, binary); err != nil {
		result.Error = err.Error()
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
	archivePath, err := a.downloadXrayCoreArchiveResilient(ctx, release, filepath.Dir(binary))
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
	// Reject a wrong binary immediately, without holding the exclusive fence
	// during catalog/download/ZIP work. Full semantic validation follows it.
	candidateVersion, err := xrayCoreVersionAt(ctx, candidatePath)
	if err != nil || candidateVersion != target {
		result.Error = "downloaded Xray binary does not match selected version"
		return result
	}
	if ctx.Err() != nil {
		result.Error = "Xray update download/preflight deadline expired; no binary changed"
		return result
	}
	// No separate 35-95 second 'run -test' cycles. The selected official
	// archive already passed SHA-256 and the extracted candidate reports the
	// exact requested version. FreeNet checks the real running process and
	// binary inode before replacing the installed core.
	if automationMutationBlockedState() {
		result.Error = "previous recovery is still blocked; reconcile the actual native runtime before update"
		return result
	}
	if a.manualRecoveryCoreState() != "running" {
		result.Error = "production Xray process inventory is ambiguous; no Xray binary changed"
		return result
	}
	preflightCtx, cancelPreflight := context.WithTimeout(context.Background(), 8*time.Second)
	preflightErr := xrayCoreNativePreflight(preflightCtx, binary)
	cancelPreflight()
	if preflightErr != nil {
		result.Error = preflightErr.Error()
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
	// This last bounded pre-swap guard prevents replacing the binary when
	// the update preparation deadline has already expired.
	if ctx.Err() != nil {
		result.Error = "Xray update preparation expired; no binary changed"
		return result
	}
	if err := os.Rename(candidatePath, binary); err != nil {
		result.Error = "atomic Xray binary replacement failed"
		return result
	}
	candidateOwned = false

	// FreeNet owns this restart. The backup is retained until the new running
	// executable and GID=11111 are confirmed or rollback succeeds.
	applyCtx, cancelApply := context.WithTimeout(context.Background(), xrayCoreApplyTimeout)
	applyErr := a.restartXrayCoreUpdateOwned(applyCtx, binary, target)
	if applyErr == nil {
		var confirmed string
		confirmed, applyErr = xrayCoreVersionAt(applyCtx, binary)
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
	cancelApply()
	if applyErr != nil {
		if rbErr := a.rollbackXrayCore(binary, backupPath, previous); rbErr != nil {
			backupOwned = false
			result.Rollback = "FAILED"
			result.Error = sanitizeAutomationReason(applyErr.Error() + "; rollback failed: " + rbErr.Error())
			v3AppendEvent("xray", "failed", "Изменение версии Xray не завершено; автоматический откат не подтверждён. STOP.")
			result.Events = xrayServiceEvents(8)
			return result
		}
		// A verified rollback no longer needs the retained copy: defer cleanup.
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
	if xrayCoreOperationSnapshot().State == "STOP" {
		writeJSON(w, http.StatusConflict, xrayCoreApplyResponse{Success: false, TargetVersion: target, Rollback: "FAILED", Error: "previous Xray rollback is unconfirmed; STOP before another version change"})
		return
	}
	select {
	case a.sem <- struct{}{}:
		// The worker, not the HTTP handler, owns this semaphore until terminal
		// apply/rollback. A browser disconnect must never start another writer.
	default:
		writeJSON(w, http.StatusConflict, xrayCoreApplyResponse{Success: false, TargetVersion: target, Rollback: "NOT_NEEDED", Events: xrayServiceEvents(8), Error: "другая операция FreeNet уже выполняется"})
		return
	}
	done := a.runXrayCoreUpdateDetached(target, func() { <-a.sem })
	var result xrayCoreApplyResponse
	select {
	case result = <-done:
	case <-r.Context().Done():
		// The worker continues to a bounded terminal receipt, which can be
		// read after reconnect from GET /api/xray/core/operation.
		return
	}
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
