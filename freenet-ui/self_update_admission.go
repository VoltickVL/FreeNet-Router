package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Application repair must not depend on a working VPN. Only an unresolved
// application transaction is a hard stop here. The helper owns the cross-
// process VPN/network fence before replacing files; VPN evidence is preserved.
func (a *app) selfUpdateStateAdmission() error {
	if a == nil {
		return errors.New("FreeNet update context unavailable")
	}
	if a.cfg.UpdateState == "" {
		return nil
	}
	info, err := os.Lstat(a.cfg.UpdateState)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 128*1024 {
		return errors.New("Состояние обновления приложения нельзя безопасно прочитать; откройте Recovery.")
	}
	if _, err := os.ReadFile(a.cfg.UpdateState); err != nil {
		return errors.New("Состояние обновления приложения недоступно; изменения не начаты.")
	}
	kv := readStateFile(a.cfg.UpdateState)
	state, rollback := strings.TrimSpace(kv["STATE"]), strings.TrimSpace(kv["ROLLBACK_STATE"])
	if state == "ROLLBACK_FAILED" || rollback == "FAILED_UNKNOWN" || rollback == "FAILED/UNKNOWN" {
		return errors.New("Откат предыдущего обновления приложения не подтверждён; требуется Recovery, не повторное применение.")
	}
	switch state {
	case "", "IDLE", "BUSY", "CHECKING", "SNAPSHOT", "FAILED", "SUCCESS", "UPDATING", "RECONNECTING":
	default:
		return errors.New("Неизвестная стадия предыдущего обновления приложения; изменения не начаты.")
	}
	if (rollback == "PENDING" || state == "UPDATING" || state == "RECONNECTING") && !a.updateLockHeld() {
		return errors.New("Предыдущее обновление прервано без подтверждённого отката; состояние сохранено для Recovery.")
	}
	return nil
}

func temporaryRecoveryUpdaterPath(path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) != "self_update.sh" {
		return false
	}
	dir := filepath.Dir(path)
	if filepath.Dir(dir) != filepath.Clean(os.TempDir()) {
		return false
	}
	name := filepath.Base(dir)
	return strings.HasPrefix(name, "freenet-browser-recovery.") ||
		strings.HasPrefix(name, "freenet-legacy-bridge.") ||
		strings.HasPrefix(name, "freenet-self-update.")
}

// Called only after the normal update state and /proc checks prove a dead,
// pre-mutation updater. Never remove a VPN writer's independently owned lock.
func (a *app) releaseStaleUpdateFence() error {
	if a.cfg.LockPath == "" {
		return nil
	}
	marker, err := os.ReadFile(filepath.Join(a.cfg.LockPath, "freenet-update-owner"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return errors.New("Не удалось проверить владельца блокировки сетевой операции.")
	}
	if strings.TrimSpace(string(marker)) != a.cfg.UpdateLock {
		return nil
	}
	owner, err1 := os.ReadFile(filepath.Join(a.cfg.UpdateLock, "owner.pid"))
	fence, err2 := os.ReadFile(filepath.Join(a.cfg.LockPath, "pid"))
	pid, err3 := strconv.Atoi(strings.TrimSpace(string(owner)))
	if err1 != nil || err2 != nil || err3 != nil || pid <= 0 || strings.TrimSpace(string(owner)) != strings.TrimSpace(string(fence)) {
		return errors.New("Владелец блокировки обновления не подтверждён; блокировка сохранена.")
	}
	if err := os.RemoveAll(a.cfg.LockPath); err != nil {
		return errors.New("Не удалось снять подтверждённую блокировку завершённого updater.")
	}
	return nil
}
