package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
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

func legacyNativeReturnPlan(plan networkPlanResponse) bool {
	return plan.Supported &&
		plan.EffectiveDNSMode == "firmware" &&
		plan.ProxyDNS == "off" &&
		plan.Port53Owner == "xray" &&
		plan.DNSRoutingMode == "split" &&
		plan.DNSOut &&
		plan.VLESSProfile
}

func enrichNativeFilterEngineMigration(plan *networkPlanResponse) {
	if plan == nil || !legacyNativeReturnPlan(*plan) {
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
	legacyReturn := legacyNativeReturnPlan(plan)

	if !plan.NativeFilterEngineConfirmRequired {
		if value != "" {
			return errors.New("подтверждение native DNS engine не требуется для текущего плана")
		}
		if legacyReturn {
			if err := ensureLegacyNativeDNSSnapshot(); err != nil {
				return err
			}
		}
		return nil
	}
	if !legacyReturn {
		return errors.New("legacy native DNS migration больше не соответствует текущему плану; обновите план")
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

	// The engine choice is a user-confirmed historical fact, but it is useful only
	// together with the exact native Xray DNS baseline. Recover and validate that
	// baseline first so a failed migration never creates another partial state.
	if err := ensureLegacyNativeDNSSnapshot(); err != nil {
		return err
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

func legacyNativeRoot() string {
	if root := strings.TrimSpace(os.Getenv("FREENET_ROOT")); root != "" {
		return root
	}
	return "/opt"
}

func legacyNativeBackupRoot() string {
	if p := strings.TrimSpace(os.Getenv("FREENET_BACKUP_ROOT")); p != "" {
		return p
	}
	return filepath.Join(legacyNativeRoot(), "backups")
}

func legacyNativeConfigDir() string {
	if p := strings.TrimSpace(os.Getenv("FREENET_CONFIG_DIR")); p != "" {
		return p
	}
	return filepath.Join(legacyNativeRoot(), "etc", "xray", "configs")
}

func legacyNativeXrayBin() string {
	if p := strings.TrimSpace(os.Getenv("FREENET_XRAY_BIN")); p != "" {
		return p
	}
	return filepath.Join(legacyNativeRoot(), "sbin", "xray")
}

func legacyNativeXrayAssetDir() string {
	if p := strings.TrimSpace(os.Getenv("FREENET_XRAY_ASSET_DIR")); p != "" {
		return p
	}
	return filepath.Join(legacyNativeRoot(), "etc", "xray", "dat")
}

func legacyNativeDNSSnapshotPaths() (string, string) {
	return filepath.Join(nativeDNSStateDir(), "02_dns.native"), filepath.Join(nativeDNSStateDir(), "02_dns.native.sha256")
}

func legacyNativeDNSSnapshotStatus() (valid bool, missing bool, err error) {
	dnsPath, hashPath := legacyNativeDNSSnapshotPaths()
	dnsInfo, dnsErr := os.Stat(dnsPath)
	hashInfo, hashErr := os.Stat(hashPath)
	_ = dnsInfo
	_ = hashInfo

	dnsMissing := os.IsNotExist(dnsErr)
	hashMissing := os.IsNotExist(hashErr)
	if dnsMissing && hashMissing {
		return false, true, nil
	}
	if dnsErr != nil && !dnsMissing {
		return false, false, errors.New("не удалось проверить native 02_dns snapshot")
	}
	if hashErr != nil && !hashMissing {
		return false, false, errors.New("не удалось проверить native 02_dns checksum")
	}
	if dnsMissing != hashMissing {
		return false, false, errors.New("native 02_dns snapshot неполон; отказ от перезаписи")
	}

	data, err := os.ReadFile(dnsPath)
	if err != nil {
		return false, false, errors.New("не удалось прочитать native 02_dns snapshot")
	}
	expectedBytes, err := os.ReadFile(hashPath)
	if err != nil {
		return false, false, errors.New("не удалось прочитать native 02_dns checksum")
	}
	expected := strings.TrimSpace(string(expectedBytes))
	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])
	if expected == "" || expected != actual {
		return false, false, errors.New("native 02_dns checksum не совпадает; отказ от перезаписи")
	}
	return true, false, nil
}

func ensureLegacyNativeDNSSnapshot() error {
	valid, missing, err := legacyNativeDNSSnapshotStatus()
	if err != nil {
		return err
	}
	if valid {
		return nil
	}
	if !missing {
		return errors.New("native 02_dns snapshot имеет неизвестное состояние")
	}

	backup, err := findNewestSafeLegacyNativeBackup()
	if err != nil {
		return err
	}
	dnsData, err := os.ReadFile(filepath.Join(backup, "02_dns.json"))
	if err != nil {
		return errors.New("не удалось прочитать native 02_dns из historical backup")
	}
	if err := validateRecoveredNativeDNSCandidate(dnsData); err != nil {
		return fmt.Errorf("native 02_dns backup %s не прошёл Xray candidate validation", filepath.Base(backup))
	}
	if err := persistRecoveredNativeDNS(dnsData); err != nil {
		return err
	}
	return nil
}

type legacyNativeBackupCandidate struct {
	path    string
	modTime time.Time
}

func findNewestSafeLegacyNativeBackup() (string, error) {
	entries, err := os.ReadDir(legacyNativeBackupRoot())
	if err != nil {
		if os.IsNotExist(err) {
			return "", errors.New("не найден historical network backup для восстановления native 02_dns")
		}
		return "", errors.New("не удалось прочитать каталог historical network backups")
	}
	candidates := make([]legacyNativeBackupCandidate, 0)
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "freenet-network-") {
			continue
		}
		path := filepath.Join(legacyNativeBackupRoot(), entry.Name())
		native, err := legacyNativeBackupIsNative(path)
		if err != nil || !native {
			continue
		}
		if _, err := os.Stat(filepath.Join(path, "02_dns.json")); err != nil {
			continue
		}
		if err := legacyNativeValidateConfdir(path); err != nil {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		candidates = append(candidates, legacyNativeBackupCandidate{path: path, modTime: info.ModTime()})
	}
	if len(candidates) == 0 {
		return "", errors.New("не найден однозначный native network backup для восстановления 02_dns; STOP без догадки")
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.After(candidates[j].modTime)
	})
	return candidates[0].path, nil
}

func legacyNativeBackupIsNative(dir string) (bool, error) {
	inbounds, err := legacyNativeReadJSONObject(filepath.Join(dir, "03_inbounds.json"))
	if err != nil {
		return false, err
	}
	outbounds, err := legacyNativeReadJSONObject(filepath.Join(dir, "04_outbounds.json"))
	if err != nil {
		return false, err
	}
	routing, err := legacyNativeReadJSONObject(filepath.Join(dir, "05_routing.json"))
	if err != nil {
		return false, err
	}

	items, err := legacyNativeObjectSlice(inbounds, "inbounds")
	if err != nil {
		return false, err
	}
	for _, item := range items {
		if legacyNativePortIncludes53(item["port"]) {
			return false, nil
		}
	}

	items, err = legacyNativeObjectSlice(outbounds, "outbounds")
	if err != nil {
		return false, err
	}
	for _, item := range items {
		if legacyNativeString(item["tag"]) == "dns-out" {
			return false, nil
		}
	}

	routingObj, ok := routing["routing"].(map[string]any)
	if !ok {
		return false, errors.New("historical routing config не содержит routing object")
	}
	items, err = legacyNativeObjectSlice(routingObj, "rules")
	if err != nil {
		return false, err
	}
	for _, rule := range items {
		if legacyNativeDNSRoutingRule(rule) {
			return false, nil
		}
	}
	return true, nil
}

func validateRecoveredNativeDNSCandidate(dnsData []byte) error {
	configDir := legacyNativeConfigDir()
	inbounds, err := legacyNativeReadJSONObject(filepath.Join(configDir, "03_inbounds.json"))
	if err != nil {
		return err
	}
	outbounds, err := legacyNativeReadJSONObject(filepath.Join(configDir, "04_outbounds.json"))
	if err != nil {
		return err
	}
	routing, err := legacyNativeReadJSONObject(filepath.Join(configDir, "05_routing.json"))
	if err != nil {
		return err
	}
	if err := legacyNativeStripDNSDelta(inbounds, outbounds, routing); err != nil {
		return err
	}

	tmp, err := os.MkdirTemp("", "freenet-native-recovery-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := os.WriteFile(filepath.Join(tmp, "02_dns.json"), dnsData, 0o644); err != nil {
		return err
	}
	for name, obj := range map[string]map[string]any{
		"03_inbounds.json": inbounds,
		"04_outbounds.json": outbounds,
		"05_routing.json":  routing,
	} {
		data, err := json.MarshalIndent(obj, "", "  ")
		if err != nil {
			return err
		}
		data = append(data, '\n')
		if err := os.WriteFile(filepath.Join(tmp, name), data, 0o644); err != nil {
			return err
		}
	}
	return legacyNativeValidateConfdir(tmp)
}

func legacyNativeValidateConfdir(dir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, legacyNativeXrayBin(), "run", "-test", "-confdir", dir)
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+legacyNativeXrayAssetDir())
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func persistRecoveredNativeDNS(data []byte) error {
	valid, missing, err := legacyNativeDNSSnapshotStatus()
	if err != nil {
		return err
	}
	if valid {
		return nil
	}
	if !missing {
		return errors.New("native 02_dns snapshot изменился во время recovery; STOP")
	}
	if err := os.MkdirAll(nativeDNSStateDir(), 0o755); err != nil {
		return errors.New("не удалось подготовить каталог native DNS state")
	}
	dnsPath, hashPath := legacyNativeDNSSnapshotPaths()
	dnsTmp, err := os.CreateTemp(nativeDNSStateDir(), ".02_dns.native.*")
	if err != nil {
		return errors.New("не удалось создать временный native 02_dns snapshot")
	}
	dnsTmpPath := dnsTmp.Name()
	defer os.Remove(dnsTmpPath)
	if err := dnsTmp.Chmod(0o644); err != nil {
		_ = dnsTmp.Close()
		return errors.New("не удалось установить права native 02_dns snapshot")
	}
	if _, err := dnsTmp.Write(data); err != nil {
		_ = dnsTmp.Close()
		return errors.New("не удалось записать native 02_dns snapshot")
	}
	if err := dnsTmp.Close(); err != nil {
		return errors.New("не удалось сохранить native 02_dns snapshot")
	}

	sum := sha256.Sum256(data)
	hashValue := hex.EncodeToString(sum[:]) + "\n"
	hashTmp, err := os.CreateTemp(nativeDNSStateDir(), ".02_dns.native.sha256.*")
	if err != nil {
		return errors.New("не удалось создать временный native 02_dns checksum")
	}
	hashTmpPath := hashTmp.Name()
	defer os.Remove(hashTmpPath)
	if err := hashTmp.Chmod(0o644); err != nil {
		_ = hashTmp.Close()
		return errors.New("не удалось установить права native 02_dns checksum")
	}
	if _, err := hashTmp.WriteString(hashValue); err != nil {
		_ = hashTmp.Close()
		return errors.New("не удалось записать native 02_dns checksum")
	}
	if err := hashTmp.Close(); err != nil {
		return errors.New("не удалось сохранить native 02_dns checksum")
	}

	if _, err := os.Stat(dnsPath); !os.IsNotExist(err) {
		return errors.New("native 02_dns snapshot появился во время recovery; обновите план")
	}
	if _, err := os.Stat(hashPath); !os.IsNotExist(err) {
		return errors.New("native 02_dns checksum появился во время recovery; обновите план")
	}
	if err := os.Rename(dnsTmpPath, dnsPath); err != nil {
		return errors.New("не удалось зафиксировать native 02_dns snapshot")
	}
	if err := os.Rename(hashTmpPath, hashPath); err != nil {
		_ = os.Remove(dnsPath)
		return errors.New("не удалось зафиксировать native 02_dns checksum")
	}
	return nil
}

func legacyNativeReadJSONObject(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	normalized, err := legacyNativeNormalizeJSONC(data)
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	if err := json.Unmarshal(normalized, &obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func legacyNativeNormalizeJSONC(data []byte) ([]byte, error) {
	withoutComments := make([]byte, 0, len(data))
	inString := false
	escaped := false
	lineComment := false
	blockComment := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		var next byte
		if i+1 < len(data) {
			next = data[i+1]
		}
		if lineComment {
			if c == '\n' {
				lineComment = false
				withoutComments = append(withoutComments, c)
			}
			continue
		}
		if blockComment {
			if c == '*' && next == '/' {
				blockComment = false
				i++
				continue
			}
			if c == '\n' {
				withoutComments = append(withoutComments, c)
			}
			continue
		}
		if inString {
			withoutComments = append(withoutComments, c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			withoutComments = append(withoutComments, c)
			continue
		}
		if c == '/' && next == '/' {
			lineComment = true
			i++
			continue
		}
		if c == '/' && next == '*' {
			blockComment = true
			i++
			continue
		}
		withoutComments = append(withoutComments, c)
	}
	if blockComment {
		return nil, errors.New("незавершённый JSONC block comment")
	}

	out := make([]byte, 0, len(withoutComments))
	inString = false
	escaped = false
	for i := 0; i < len(withoutComments); i++ {
		c := withoutComments[i]
		if inString {
			out = append(out, c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			out = append(out, c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(withoutComments) && (withoutComments[j] == ' ' || withoutComments[j] == '\t' || withoutComments[j] == '\r' || withoutComments[j] == '\n') {
				j++
			}
			if j < len(withoutComments) && (withoutComments[j] == '}' || withoutComments[j] == ']') {
				continue
			}
		}
		out = append(out, c)
	}
	return out, nil
}

func legacyNativeObjectSlice(obj map[string]any, field string) ([]map[string]any, error) {
	value, ok := obj[field]
	if !ok || value == nil {
		return []map[string]any{}, nil
	}
	raw, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("поле %s не является массивом", field)
	}
	items := make([]map[string]any, 0, len(raw))
	for _, value := range raw {
		item, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("элемент %s не является object", field)
		}
		items = append(items, item)
	}
	return items, nil
}

func legacyNativeString(value any) string {
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func legacyNativePortIncludes53(value any) bool {
	switch v := value.(type) {
	case float64:
		return v == 53
	case string:
		for _, token := range strings.Split(v, ",") {
			token = strings.TrimSpace(token)
			if token == "53" {
				return true
			}
			if strings.Contains(token, "-") {
				parts := strings.SplitN(token, "-", 2)
				lo, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
				hi, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
				if err1 == nil && err2 == nil && lo <= 53 && 53 <= hi {
					return true
				}
			}
		}
	}
	return false
}

func legacyNativeInboundHasDNSTag(value any) bool {
	isDNS := func(s string) bool {
		s = strings.TrimSpace(s)
		return s == "dns-vless" || s == "dns-direct" || s == "dns-in" || s == "dns"
	}
	switch v := value.(type) {
	case string:
		return isDNS(v)
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && isDNS(s) {
				return true
			}
		}
	}
	return false
}

func legacyNativeDNSRoutingRule(rule map[string]any) bool {
	return legacyNativeString(rule["outboundTag"]) == "dns-out" ||
		legacyNativeInboundHasDNSTag(rule["inboundTag"]) ||
		legacyNativePortIncludes53(rule["port"])
}

func legacyNativeStripDNSDelta(inbounds, outbounds, routing map[string]any) error {
	inItems, err := legacyNativeObjectSlice(inbounds, "inbounds")
	if err != nil {
		return err
	}
	newIn := make([]any, 0, len(inItems))
	for _, item := range inItems {
		if !legacyNativePortIncludes53(item["port"]) {
			newIn = append(newIn, item)
		}
	}
	inbounds["inbounds"] = newIn

	outItems, err := legacyNativeObjectSlice(outbounds, "outbounds")
	if err != nil {
		return err
	}
	newOut := make([]any, 0, len(outItems))
	for _, item := range outItems {
		if legacyNativeString(item["tag"]) != "dns-out" {
			newOut = append(newOut, item)
		}
	}
	outbounds["outbounds"] = newOut

	routingObj, ok := routing["routing"].(map[string]any)
	if !ok {
		return errors.New("current routing config не содержит routing object")
	}
	rules, err := legacyNativeObjectSlice(routingObj, "rules")
	if err != nil {
		return err
	}
	newRules := make([]any, 0, len(rules))
	for _, rule := range rules {
		if !legacyNativeDNSRoutingRule(rule) {
			newRules = append(newRules, rule)
		}
	}
	routingObj["rules"] = newRules
	return nil
}
