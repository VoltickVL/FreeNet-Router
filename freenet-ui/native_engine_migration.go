package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const defaultNativeDNSStateDir = "/opt/etc/freenet/native-dns"

var legacyNativeFilterEngineChoices = []string{"public", "interceptor", "nextdns", "skydns"}

func nativeDNSStateDir() string {
	if p := strings.TrimSpace(os.Getenv("FREENET_NATIVE_DNS_STATE_DIR")); p != "" {
		return p
	}
	return defaultNativeDNSStateDir
}

func nativeFilterEngineSnapshotPath() string {
	return filepath.Join(nativeDNSStateDir(), "filter-engine.native")
}

func nativeFilterEngineSnapshotMissing() (bool, error) {
	_, err := os.Stat(nativeFilterEngineSnapshotPath())
	if err == nil {
		return false, nil
	}
	if os.IsNotExist(err) {
		return true, nil
	}
	return false, err
}

func validLegacyNativeFilterEngine(value string) bool {
	value = strings.TrimSpace(value)
	for _, allowed := range legacyNativeFilterEngineChoices {
		if value == allowed {
			return true
		}
	}
	return false
}

func enrichNativeFilterEngineMigration(plan *networkPlanResponse) {
	if plan == nil || !plan.Supported || plan.EffectiveDNSMode != "firmware" || plan.ProxyDNS != "off" || plan.Port53Owner != "xray" || plan.DNSRoutingMode != "split" || !plan.DNSOut || !plan.VLESSProfile {
		return
	}
	missing, err := nativeFilterEngineSnapshotMissing()
	if err != nil || !missing {
		return
	}
	plan.NativeFilterEngineConfirmRequired = true
	plan.NativeFilterEngineChoices = append([]string(nil), legacyNativeFilterEngineChoices...)
}

func persistConfirmedLegacyNativeFilterEngine(plan networkPlanResponse, value string) error {
	value = strings.TrimSpace(value)
	if !plan.NativeFilterEngineConfirmRequired {
		if value != "" {
			return errors.New("подтверждение native DNS engine не требуется для текущего плана")
		}
		return nil
	}
	if !validLegacyNativeFilterEngine(value) {
		return errors.New("выберите прежний native DNS engine перед возвратом из legacy Split")
	}

	missing, err := nativeFilterEngineSnapshotMissing()
	if err != nil {
		return errors.New("не удалось проверить native DNS engine snapshot")
	}
	if !missing {
		return errors.New("native DNS engine snapshot изменился после проверки; обновите план")
	}

	dir := nativeDNSStateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errors.New("не удалось подготовить каталог native DNS state")
	}
	f, err := os.CreateTemp(dir, ".filter-engine.native.*")
	if err != nil {
		return errors.New("не удалось создать временный native DNS engine snapshot")
	}
	tmp := f.Name()
	keep := false
	defer func() {
		_ = f.Close()
		if !keep {
			_ = os.Remove(tmp)
		}
	}()
	if err := f.Chmod(0o644); err != nil {
		return errors.New("не удалось установить права native DNS engine snapshot")
	}
	if _, err := f.WriteString(value + "\n"); err != nil {
		return errors.New("не удалось записать native DNS engine snapshot")
	}
	if err := f.Close(); err != nil {
		return errors.New("не удалось сохранить native DNS engine snapshot")
	}
	if err := os.Rename(tmp, nativeFilterEngineSnapshotPath()); err != nil {
		return errors.New("не удалось зафиксировать native DNS engine snapshot")
	}
	keep = true
	return nil
}
