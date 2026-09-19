package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const settingsSchedulerReconcileTimeout = 5 * time.Second

func crontabMeansEmpty(output string) bool {
	lower := strings.ToLower(strings.TrimSpace(output))
	return strings.Contains(lower, "no crontab") || strings.Contains(lower, "no such file or directory")
}

func readAutomationCrontabStrict(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, automationCrontabBin(), "-l")
	out, err := cmd.CombinedOutput()
	if err == nil {
		return out, nil
	}
	if crontabMeansEmpty(string(out)) {
		return []byte{}, nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, errors.New("crontab read timed out")
	}
	return nil, errors.New("cannot read current crontab safely")
}

func installAutomationCrontabStrict(ctx context.Context, data []byte) error {
	file, err := os.CreateTemp("", "freenet-crontab-reconcile-*.txt")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	out, err := exec.CommandContext(ctx, automationCrontabBin(), name).CombinedOutput()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errors.New("crontab install timed out")
		}
		return fmt.Errorf("cannot install managed cron: %s", strings.TrimSpace(sanitizeOutput(string(out))))
	}
	return nil
}

func (a *app) settingsV3SchedulerCurrent() (bool, error) {
	if a == nil {
		return false, errors.New("FreeNet app is unavailable")
	}
	if _, err := os.Stat(a.cfg.ConfigPath); err != nil {
		return false, errors.New("FreeNet config is unavailable")
	}

	readCtx, cancelRead := context.WithTimeout(context.Background(), settingsSchedulerReconcileTimeout)
	existing, err := readAutomationCrontabStrict(readCtx)
	cancelRead()
	if err != nil {
		return false, err
	}
	desired, err := buildManagedAutomationCronV3(a, existing, settingsV3ManagedCronValuesFromConfig(a.cfg.ConfigPath))
	if err != nil {
		return false, fmt.Errorf("cannot build canonical scheduler: %w", err)
	}
	return bytes.Equal(existing, desired), nil
}

func (a *app) reconcileSettingsV3Scheduler() (bool, error) {
	if _, err := os.Stat(a.cfg.ConfigPath); err != nil {
		return false, errors.New("FreeNet config is unavailable")
	}

	readCtx, cancelRead := context.WithTimeout(context.Background(), settingsSchedulerReconcileTimeout)
	existing, err := readAutomationCrontabStrict(readCtx)
	cancelRead()
	if err != nil {
		return false, err
	}

	desired, err := buildManagedAutomationCronV3(a, existing, settingsV3ManagedCronValuesFromConfig(a.cfg.ConfigPath))
	if err != nil {
		return false, fmt.Errorf("cannot build canonical scheduler: %w", err)
	}
	if bytes.Equal(existing, desired) {
		return false, nil
	}

	installCtx, cancelInstall := context.WithTimeout(context.Background(), settingsSchedulerReconcileTimeout)
	err = installAutomationCrontabStrict(installCtx, desired)
	cancelInstall()
	if err == nil {
		verifyCtx, cancelVerify := context.WithTimeout(context.Background(), settingsSchedulerReconcileTimeout)
		installed, verifyErr := readAutomationCrontabStrict(verifyCtx)
		cancelVerify()
		if verifyErr == nil && bytes.Equal(installed, desired) {
			return true, nil
		}
		if verifyErr != nil {
			err = verifyErr
		} else {
			err = errors.New("installed scheduler does not match the canonical plan")
		}
	}

	rollbackCtx, cancelRollback := context.WithTimeout(context.Background(), settingsSchedulerReconcileTimeout)
	rollbackErr := installAutomationCrontabStrict(rollbackCtx, existing)
	cancelRollback()
	if rollbackErr != nil {
		return false, errors.New("scheduler reconcile failed; rollback failed or is unknown")
	}
	rollbackVerifyCtx, cancelRollbackVerify := context.WithTimeout(context.Background(), settingsSchedulerReconcileTimeout)
	rolledBack, rollbackVerifyErr := readAutomationCrontabStrict(rollbackVerifyCtx)
	cancelRollbackVerify()
	if rollbackVerifyErr != nil || !bytes.Equal(rolledBack, existing) {
		return false, errors.New("scheduler reconcile failed; rollback failed or is unknown")
	}
	return false, fmt.Errorf("scheduler reconcile failed; previous scheduler restored and verified: %w", err)
}

func settingsV3SchedulerStartupEligible() bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("FREENET_DISABLE_SCHEDULER_RECONCILE")), "yes") {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("FREENET_FORCE_SCHEDULER_RECONCILE")), "yes") {
		return true
	}
	executable, err := os.Executable()
	if err != nil {
		return false
	}
	expected := automationRunnerPath()
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	if resolved, err := filepath.EvalSymlinks(expected); err == nil {
		expected = resolved
	}
	return filepath.Clean(executable) == filepath.Clean(expected)
}

func reconcileSettingsV3SchedulerOnStartup(a *app) {
	if a == nil || !settingsV3SchedulerStartupEligible() {
		return
	}
	changed, err := a.reconcileSettingsV3Scheduler()
	if err != nil {
		log.Printf("FreeNet AUTO scheduler reconcile skipped: %v", err)
		v3AppendEvent("auto_vpn_scheduler", "failed", "Планировщик FreeNet не удалось безопасно синхронизировать. Настройки не изменены.")
		return
	}
	if changed {
		log.Printf("FreeNet AUTO scheduler reconciled with current Settings policy")
		v3AppendEvent("auto_vpn_scheduler", "reconciled", "Планировщик FreeNet синхронизирован с текущими настройками.")
	}
}
