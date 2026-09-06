package main

import (
	"errors"
	"strings"
)

var networkBridgeYandexBasicResolvers = []string{"77.88.8.8", "77.88.8.1"}

// The network helper bridge runs in its own short-lived process for every
// plan/apply. Keep the resolver lines removed during forward staging only long
// enough to make a failed apply restore the exact pre-apply active selection.
// This is rollback state, not persistent configuration.
var networkBridgeNativeResolverStageRemoved []string

func networkBridgeNameServerAddress(line string) string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "ip name-server ") {
		return ""
	}
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return ""
	}
	return strings.Trim(strings.TrimSpace(fields[2]), "\"")
}

func networkBridgeAddressMatchesLAN(address, lanIP string) bool {
	return address == lanIP || address == lanIP+":53"
}

// networkBridgeHasNativeResolverSelection answers whether Keenetic has an
// explicit independent system resolver selection that can still feed native
// ndnproxy after the Split-owned LAN_IP:53 pointer is removed.
//
// Passive resolver definitions and interface/WAN hints are deliberately NOT
// sufficient evidence. dns-proxy tls/https/dns53 upstream lines are profile
// definitions, while interface-scoped name-servers / ip dhcp client dns-routes
// describe interface capabilities or learned-DNS behaviour without proving that
// the system resolver will actually select a usable upstream after the Split
// pointer disappears.
func networkBridgeHasNativeResolverSelection(config, lanIP string) bool {
	for _, raw := range strings.Split(strings.ReplaceAll(config, "\r", ""), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		indented := len(raw) > 0 && (raw[0] == ' ' || raw[0] == '\t')
		if indented {
			continue
		}
		if strings.HasPrefix(line, "ip name-server ") {
			address := networkBridgeNameServerAddress(line)
			if address != "" && !networkBridgeAddressMatchesLAN(address, lanIP) {
				return true
			}
		}
		if strings.HasPrefix(line, "ipv6 name-server ") {
			return true
		}
	}
	return false
}

func networkBridgeResolverLineSet(lines []string) map[string]bool {
	set := make(map[string]bool, len(lines))
	for _, line := range lines {
		key := networkBridgeResolverSelectionKey(strings.TrimSpace(line))
		if key != "" {
			set[key] = true
		}
	}
	return set
}

func networkBridgeResolverSelectionsEqual(left, right []string) bool {
	leftSet := networkBridgeResolverLineSet(left)
	rightSet := networkBridgeResolverLineSet(right)
	if len(leftSet) != len(rightSet) {
		return false
	}
	for line := range leftSet {
		if !rightSet[line] {
			return false
		}
	}
	return true
}

func networkBridgeYandexBasicResolverLines() []string {
	lines := make([]string, 0, len(networkBridgeYandexBasicResolvers))
	for _, address := range networkBridgeYandexBasicResolvers {
		lines = append(lines, "ip name-server "+address)
	}
	return lines
}

// Until the Native DNS Provider selector is productized, the normal FreeNet
// native target is one explicit deterministic resolver set: Yandex Basic.
//
// Existing active global name-server lines and the historical
// resolver-selection.native snapshot are NOT target policy. They are preserved
// only as rollback/migration evidence. This is intentional: HOME and WORK both
// proved that inherited/default active resolvers can survive a transition and
// cause parallel/conflicting DNS paths. Explicit DNS Apply therefore owns the
// active System resolver selection and converges it to the target set.
func networkBridgeCanonicalNativeResolverTarget() ([]string, string, error) {
	return networkBridgeYandexBasicResolverLines(), "yandex-basic", nil
}

func networkBridgeNativeResolverStatus(lanIP string) (string, error) {
	config, err := networkBridgeRunningConfig()
	if err != nil {
		return "", err
	}
	current, err := networkBridgeNativeResolverSelectionLines(config, lanIP)
	if err != nil {
		return "", err
	}
	target, _, err := networkBridgeCanonicalNativeResolverTarget()
	if err != nil {
		return "", err
	}
	if networkBridgeResolverSelectionsEqual(current, target) {
		return "existing-native-resolver-ready", nil
	}
	if len(current) == 0 {
		return "yandex-basic-fallback-needed", nil
	}
	return "yandex-basic-replace-needed", nil
}

func networkBridgeNameServerLinesForAddress(config, address string) []string {
	var lines []string
	for _, raw := range strings.Split(strings.ReplaceAll(config, "\r", ""), "\n") {
		line := strings.TrimSpace(raw)
		if networkBridgeNameServerAddress(line) == address {
			lines = append(lines, line)
		}
	}
	return lines
}

// Stage exactly the canonical native resolver set before removing the Split-owned
// local pointer. The operation is intentionally strict but simple:
//   1. read the current active global resolver selection;
//   2. add missing target lines so there is never a zero-upstream window;
//   3. remove every active global line that is not part of the target;
//   4. remember removed lines only for exact rollback if the wider transaction
//      fails.
//
// Passive/named DNS profiles, DoT/DoH definitions, assignments and WAN hints are
// outside this active-selection surface and are untouched.
func networkBridgeEnsureNativeResolverReady(lanIP string) ([]string, string, error) {
	networkBridgeNativeResolverStageRemoved = nil

	config, err := networkBridgeRunningConfig()
	if err != nil {
		return nil, "", err
	}
	current, err := networkBridgeNativeResolverSelectionLines(config, lanIP)
	if err != nil {
		return nil, "", err
	}
	target, source, err := networkBridgeCanonicalNativeResolverTarget()
	if err != nil {
		return nil, "", err
	}

	currentSet := networkBridgeResolverLineSet(current)
	targetSet := networkBridgeResolverLineSet(target)
	missing := make([]string, 0, len(target))
	for _, line := range target {
		line = strings.TrimSpace(line)
		key := networkBridgeResolverSelectionKey(line)
		if key != "" && !currentSet[key] {
			missing = append(missing, line)
		}
	}

	added, err := networkBridgeAddResolverSelectionLines(missing)
	if err != nil {
		return nil, "", err
	}

	removed := make([]string, 0, len(current))
	for _, line := range current {
		line = strings.TrimSpace(line)
		key := networkBridgeResolverSelectionKey(line)
		if line == "" || (key != "" && targetSet[key]) {
			continue
		}
		if err := networkBridgeRemoveResolverSelectionLines([]string{line}); err != nil {
			_, _ = networkBridgeAddResolverSelectionLines(removed)
			_ = networkBridgeRemoveAddedNativeResolvers(added)
			return nil, "", err
		}
		removed = append(removed, line)
	}

	updated, err := networkBridgeRunningConfig()
	if err != nil {
		_, _ = networkBridgeAddResolverSelectionLines(removed)
		_ = networkBridgeRemoveAddedNativeResolvers(added)
		return nil, "", err
	}
	updatedSelection, err := networkBridgeNativeResolverSelectionLines(updated, lanIP)
	if err != nil || !networkBridgeResolverSelectionsEqual(updatedSelection, target) {
		_, _ = networkBridgeAddResolverSelectionLines(removed)
		_ = networkBridgeRemoveAddedNativeResolvers(added)
		if err != nil {
			return nil, "", err
		}
		return nil, "", errors.New("native resolver selection did not converge to canonical target")
	}

	networkBridgeNativeResolverStageRemoved = append([]string(nil), removed...)
	return added, "canonical-" + source, nil
}

func networkBridgeRemoveAddedNativeResolvers(lines []string) error {
	return networkBridgeRemoveResolverSelectionLines(lines)
}

func networkBridgeRollbackNativeResolverStage(lanIP string, restoreLocal bool, added []string) error {
	removed := append([]string(nil), networkBridgeNativeResolverStageRemoved...)
	networkBridgeNativeResolverStageRemoved = nil

	if restoreLocal {
		if err := networkBridgeAddLocalPointer(lanIP); err != nil {
			return err
		}
	}
	if err := networkBridgeRemoveAddedNativeResolvers(added); err != nil {
		return err
	}
	if _, err := networkBridgeAddResolverSelectionLines(removed); err != nil {
		return err
	}
	return networkBridgeSave()
}

func augmentNetworkBridgeNativeResolverPlan(output, status string) string {
	values := parseNetworkBridgeValues(output)
	delta := values["EXPECTED_DELTA"]
	var extra string
	switch status {
	case "yandex-basic-fallback-needed":
		extra = "set canonical native Yandex Basic resolver 77.88.8.8/77.88.8.1 before DNS acceptance"
	case "yandex-basic-replace-needed":
		extra = "remove inherited active System resolvers and set canonical native Yandex Basic 77.88.8.8/77.88.8.1 before DNS acceptance"
	}
	if extra != "" {
		if delta == "" {
			delta = extra
		} else if !strings.Contains(delta, extra) {
			delta += "; " + extra
		}
	}
	out := strings.TrimRight(output, "\r\n")
	if delta != "" {
		out += "\nEXPECTED_DELTA=" + delta
	}
	out += "\nNATIVE_RESOLVER_SELECTION=" + status
	// A router can already be structurally native while still carrying an
	// inherited/unmanaged active System resolver set. Mark that as a real plan
	// delta so initial setup does not incorrectly report "already active" and
	// force the user to delete DNS addresses manually.
	if status != "existing-native-resolver-ready" && networkBridgeRuntimeNative(values) {
		out += "\nDNS_ROUTING_MODE=native-resolver-selection-replace"
	}
	out += "\n"
	return out
}
