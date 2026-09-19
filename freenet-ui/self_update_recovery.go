package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	recoveryLatestURL   = "https://api.github.com/repos/VoltickVL/FreeNet-Router/releases/latest"
	recoveryReleaseRoot = "https://github.com/VoltickVL/FreeNet-Router/releases/download"
)

type recoveryReleaseMetadata struct {
	TagName string `json:"tag_name"`
}

func recoveryHTTPClient(dnsServer string) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if dnsServer != "" {
		resolver := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				d := net.Dialer{Timeout: 5 * time.Second}
				return d.DialContext(ctx, "udp", dnsServer)
			},
		}
		transport.DialContext = (&net.Dialer{
			Timeout:   12 * time.Second,
			KeepAlive: 20 * time.Second,
			Resolver:  resolver,
		}).DialContext
	}
	return &http.Client{
		Transport: transport,
		Timeout:   35 * time.Second,
	}
}

func recoveryDownload(ctx context.Context, rawURL string, maxBytes int64) ([]byte, error) {
	var lastErr error
	for _, dnsServer := range []string{"", "77.88.8.8:53", "8.8.8.8:53"} {
		for attempt := 0; attempt < 2; attempt++ {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("User-Agent", "FreeNet-Control-Center/"+version)
			resp, err := recoveryHTTPClient(dnsServer).Do(req)
			if err != nil {
				lastErr = err
			} else {
				body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
				resp.Body.Close()
				if readErr != nil {
					lastErr = readErr
				} else if resp.StatusCode != http.StatusOK {
					lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
				} else if int64(len(body)) > maxBytes {
					lastErr = errors.New("response exceeds recovery size limit")
				} else {
					return body, nil
				}
			}
			if attempt == 0 {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(600 * time.Millisecond):
				}
			}
		}
	}
	if lastErr == nil {
		lastErr = errors.New("download failed")
	}
	return nil, lastErr
}

func recoveryLatestTag(ctx context.Context) (string, error) {
	url := strings.TrimSpace(os.Getenv("FREENET_RECOVERY_LATEST_URL"))
	if url == "" {
		url = recoveryLatestURL
	}
	body, err := recoveryDownload(ctx, url, 1024*1024)
	if err != nil {
		return "", fmt.Errorf("latest release metadata unavailable: %w", err)
	}
	var meta recoveryReleaseMetadata
	if err := json.Unmarshal(body, &meta); err != nil {
		return "", errors.New("latest release metadata is invalid")
	}
	tag := strings.TrimSpace(meta.TagName)
	if !validReleaseTag(tag) {
		return "", errors.New("latest release tag is invalid")
	}
	return tag, nil
}

func recoveryReleaseBase(tag string) string {
	root := strings.TrimSpace(os.Getenv("FREENET_RECOVERY_RELEASE_ROOT"))
	if root == "" {
		root = recoveryReleaseRoot
	}
	return strings.TrimRight(root, "/") + "/" + tag
}

func recoveryManifestSHA(manifest []byte, name string) (string, error) {
	for _, line := range strings.Split(strings.ReplaceAll(string(manifest), "\r", ""), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		sum := strings.ToLower(strings.TrimSpace(fields[0]))
		if len(sum) != 64 {
			return "", errors.New("invalid SHA-256 manifest entry")
		}
		if _, err := hex.DecodeString(sum); err != nil {
			return "", errors.New("invalid SHA-256 manifest entry")
		}
		return sum, nil
	}
	return "", errors.New("SHA-256 manifest entry missing for self_update.sh")
}

func (a *app) recoveryUpdaterEnv(target string) []string {
	return append(os.Environ(),
		"PATH=/opt/bin:/opt/sbin:/opt/usr/bin:/opt/usr/sbin:/bin:/sbin:/usr/bin:/usr/sbin",
		"FREENET_CURRENT_VERSION=v"+version,
		"FREENET_UPDATE_STATE_FILE="+a.cfg.UpdateState,
		"FREENET_UPDATE_LOCK_DIR="+a.cfg.UpdateLock,
		"FREENET_LATEST_TAG="+target,
	)
}

func (a *app) prepareVerifiedRecoveryUpdater(ctx context.Context) (string, string, selfUpdatePlanResponse, error) {
	target, err := recoveryLatestTag(ctx)
	if err != nil {
		return "", "", selfUpdatePlanResponse{}, err
	}
	base := recoveryReleaseBase(target)
	manifest, err := recoveryDownload(ctx, base+"/SHA256SUMS", 2*1024*1024)
	if err != nil {
		return "", "", selfUpdatePlanResponse{}, fmt.Errorf("release manifest unavailable: %w", err)
	}
	expected, err := recoveryManifestSHA(manifest, "self_update.sh")
	if err != nil {
		return "", "", selfUpdatePlanResponse{}, err
	}
	helper, err := recoveryDownload(ctx, base+"/self_update.sh", 2*1024*1024)
	if err != nil {
		return "", "", selfUpdatePlanResponse{}, fmt.Errorf("verified updater download failed: %w", err)
	}
	actualBytes := sha256.Sum256(helper)
	actual := hex.EncodeToString(actualBytes[:])
	if actual != expected {
		return "", "", selfUpdatePlanResponse{}, errors.New("SHA-256 mismatch for recovery updater")
	}

	dir, err := os.MkdirTemp("", "freenet-browser-recovery.*")
	if err != nil {
		return "", "", selfUpdatePlanResponse{}, errors.New("cannot create recovery staging directory")
	}
	script := filepath.Join(dir, "self_update.sh")
	if err := os.WriteFile(script, helper, 0o700); err != nil {
		os.RemoveAll(dir)
		return "", "", selfUpdatePlanResponse{}, errors.New("cannot stage verified recovery updater")
	}

	planCtx, cancel := context.WithTimeout(ctx, 55*time.Second)
	defer cancel()
	cmd := exec.CommandContext(planCtx, "/bin/sh", script, "plan")
	cmd.Env = a.recoveryUpdaterEnv(target)
	out, cmdErr := cmd.CombinedOutput()
	kv := parseKVOutput(string(out))
	plan := selfUpdatePlanResponse{
		Success:          boolKV(kv["SUCCESS"]),
		Ready:            boolKV(kv["READY"]),
		CurrentVersion:   kv["CURRENT_VERSION"],
		LatestVersion:    kv["LATEST_VERSION"],
		TargetTag:        kv["TARGET_TAG"],
		UpdateAvailable:  boolKV(kv["UPDATE_AVAILABLE"]),
		ManifestVerified: boolKV(kv["MANIFEST_VERIFIED"]),
		ExpectedDelta:    kv["EXPECTED_DELTA"],
		ExpectedNoDelta:  kv["EXPECTED_NO_DELTA"],
		ReleaseNotes:     kv["RELEASE_NOTES"],
		Error:            kv["ERROR"],
	}
	if plan.CurrentVersion == "" {
		plan.CurrentVersion = "v" + version
	}
	if cmdErr != nil || !plan.Success || !plan.Ready || !plan.ManifestVerified || plan.TargetTag != target {
		os.RemoveAll(dir)
		if plan.Error != "" {
			return "", "", plan, errors.New(plan.Error)
		}
		return "", "", plan, errors.New("verified recovery updater did not confirm the exact release plan")
	}
	if !plan.UpdateAvailable {
		os.RemoveAll(dir)
		return "", "", plan, errors.New("installed FreeNet is already current")
	}
	return dir, script, plan, nil
}

func (a *app) releaseRecoveryLaunch() {
	a.updateMu.Lock()
	a.updateLaunching = false
	a.updateTarget = ""
	a.updateMu.Unlock()
}

func (a *app) handleSelfUpdateRecover(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, actionResult{Success: false, Error: "cross-origin request rejected"})
		return
	}

	a.updateMu.Lock()
	if a.updateLaunching {
		target := a.updateTarget
		a.updateMu.Unlock()
		writeJSON(w, http.StatusAccepted, actionResult{
			Success: true, Action: "self-update-recovery", OperationID: target,
			Message: "Восстановление updater уже выполняется. Показываем текущий прогресс.",
		})
		return
	}
	a.updateLaunching = true
	a.updateTarget = "recovery"
	a.updateMu.Unlock()

	held, stale := a.updateLockStatus()
	if held {
		if !stale {
			a.releaseRecoveryLaunch()
			v3AppendEvent("freenet_update_recovery", "blocked", "Browser recovery остановлен: updater активен либо lock/state нельзя безопасно снять.")
			writeJSON(w, http.StatusConflict, actionResult{Success: false, Error: "Updater ещё активен либо его состояние нельзя безопасно подтвердить. Recovery ничего не изменил."})
			return
		}
		if err := a.unlockStaleUpdateLock(); err != nil {
			a.releaseRecoveryLaunch()
			v3AppendEvent("freenet_update_recovery", "failed", "Browser recovery не смог безопасно снять подтверждённую stale-lock.")
			writeJSON(w, http.StatusConflict, actionResult{Success: false, Error: "Не удалось безопасно снять зависшую блокировку updater: " + err.Error()})
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 80*time.Second)
	defer cancel()
	dir, script, plan, err := a.prepareVerifiedRecoveryUpdater(ctx)
	if err != nil {
		a.releaseRecoveryLaunch()
		v3AppendEvent("freenet_update_recovery", "failed", "Browser recovery остановлен до mutation: "+err.Error())
		writeJSON(w, http.StatusBadGateway, actionResult{Success: false, Error: "Восстановление updater не подтверждено: " + err.Error()})
		return
	}

	a.updateMu.Lock()
	a.updateTarget = plan.TargetTag
	a.updateMu.Unlock()

	cmd := exec.Command("/bin/sh", script, "apply", plan.TargetTag)
	cmd.Env = a.recoveryUpdaterEnv(plan.TargetTag)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		os.RemoveAll(dir)
		a.releaseRecoveryLaunch()
		v3AppendEvent("freenet_update_recovery", "failed", "Verified updater подтверждён, но процесс apply не запустился.")
		writeJSON(w, http.StatusInternalServerError, actionResult{Success: false, Error: "Проверенный updater подтверждён, но не удалось запустить apply."})
		return
	}

	target := plan.TargetTag
	v3AppendEvent("freenet_update_recovery", "started", "Browser recovery проверил SHA-256 нового updater и запустил штатное обновление до "+target+".")
	go func() {
		_ = cmd.Wait()
		os.RemoveAll(dir)
		a.releaseRecoveryLaunch()
	}()

	writeJSON(w, http.StatusAccepted, actionResult{
		Success: true,
		Action: "self-update-recovery",
		OperationID: target,
		Message: "Механизм обновления восстановлен из проверенного релиза. Запущено штатное обновление FreeNet.",
	})
}
