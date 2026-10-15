package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	automationEmergencyCooldownSchema = 1
	automationEmergencyCooldownDefaultPath = "/opt/var/lib/freenet/auto-emergency-cooldown.json"
	automationEmergencyCooldownResetAfter = 15 * time.Minute
)

// Limits repeated heavy emergency scans while continuing read-only VPN health
// checks. Never substitutes for the runtime rollback/mutation safety fence.
type automationEmergencyCooldownState struct {
	Schema   int       `json:"schema"`
	Identity string    `json:"identity"`
	Failures int       `json:"failures"`
	RetryAt  time.Time `json:"retry_at"`
}

func automationEmergencyCooldownPath() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_AUTO_EMERGENCY_COOLDOWN_STATE")); value != "" {
		return value
	}
	return automationEmergencyCooldownDefaultPath
}

func automationEmergencyCooldownIdentity(a *app) string {
	var filter, endpoint string
	if a != nil {
		filter = currentExactProfileLabel(a.cfg.FilterPath)
		endpoint = readBestServerCurrentEndpoint(a.cfg.OutPath)
	}
	// A new exact profile/endpoint must bypass an old candidate scan cooldown.
	sum := sha256.Sum256([]byte(strings.TrimSpace(filter) + "\x00" + strings.TrimSpace(endpoint)))
	return fmt.Sprintf("%x", sum)
}

func readAutomationEmergencyCooldown() (automationEmergencyCooldownState, error) {
	data, err := os.ReadFile(automationEmergencyCooldownPath())
	if errors.Is(err, os.ErrNotExist) {
		return automationEmergencyCooldownState{}, nil
	}
	if err != nil {
		return automationEmergencyCooldownState{}, err
	}
	if len(data) == 0 || len(data) > 4096 {
		return automationEmergencyCooldownState{}, errors.New("invalid AUTO emergency cooldown state size")
	}
	var state automationEmergencyCooldownState
	if err := json.Unmarshal(data, &state); err != nil {
		return automationEmergencyCooldownState{}, errors.New("invalid AUTO emergency cooldown state")
	}
	if state.Schema != automationEmergencyCooldownSchema || len(state.Identity) != 64 ||
		state.Failures < 1 || state.Failures > 8 || state.RetryAt.IsZero() {
		return automationEmergencyCooldownState{}, errors.New("invalid AUTO emergency cooldown fields")
	}
	return state, nil
}

func emergencyRetryDelay(failures int) time.Duration {
	switch {
	case failures <= 1:
		return 30 * time.Second
	case failures == 2:
		return time.Minute
	case failures == 3:
		return 2 * time.Minute
	case failures == 4:
		return 4 * time.Minute
	default:
		return 5 * time.Minute
	}
}

// Call under the exclusive AUTO health recovery lock only.
func recordAutomationEmergencyFailure(a *app, now time.Time) (time.Time, error) {
	prior, err := readAutomationEmergencyCooldown()
	if err != nil {
		return time.Time{}, err
	}
	identity := automationEmergencyCooldownIdentity(a)
	failures := 1
	if prior.Schema == automationEmergencyCooldownSchema &&
		prior.Identity == identity && prior.Failures > 0 &&
		!now.After(prior.RetryAt.Add(automationEmergencyCooldownResetAfter)) {
		failures = prior.Failures + 1
	}
	if failures > 8 {
		failures = 8
	}
	retryAt := now.Add(emergencyRetryDelay(failures))
	state := automationEmergencyCooldownState{
		Schema: automationEmergencyCooldownSchema, Identity: identity,
		Failures: failures, RetryAt: retryAt,
	}
	data, err := json.Marshal(state)
	if err != nil {
		return time.Time{}, err
	}
	path := automationEmergencyCooldownPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return time.Time{}, err
	}
	if err := atomicWrite(path, append(data, '\n'), 0600); err != nil {
		return time.Time{}, err
	}
	return retryAt, nil
}

// The watchdog still performs light observations during any active cooldown;
// a new exact profile/endpoint invalidates old retry state.
func automationEmergencyCooldownDue(a *app, now time.Time) (time.Time, bool, error) {
	state, err := readAutomationEmergencyCooldown()
	if err != nil {
		return time.Time{}, false, err
	}
	if state.Schema == 0 || state.Identity != automationEmergencyCooldownIdentity(a) {
		return time.Time{}, false, nil
	}
	if now.Before(state.RetryAt) {
		return state.RetryAt, true, nil
	}
	return time.Time{}, false, nil
}

func clearAutomationEmergencyCooldown() error {
	err := os.Remove(automationEmergencyCooldownPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
