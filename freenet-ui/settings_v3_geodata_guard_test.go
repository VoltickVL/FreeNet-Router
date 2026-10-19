package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func prepareGeoDataMutexTest(t *testing.T) (*app, string) {
	t.Helper()
	a, history, _ := prepareXrayRuntimeWatchTest(t)
	dir := t.TempDir()
	a.cfg.XKeenPath = filepath.Join(dir, "xkeen")
	if err := os.WriteFile(a.cfg.XKeenPath, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FREENET_SETTINGS_V3_STATE", filepath.Join(dir, "settings.state"))
	t.Setenv("FREENET_SETTINGS_V3_HISTORY", history)
	t.Setenv("FREENET_AUTOMATION_STATE", filepath.Join(dir, "auto.state"))
	return a, history
}

func stubGeoDataRunner(t *testing.T, runner func(context.Context, string) error) {
	t.Helper()
	old := settingsV3RunGeoDataXKeen
	settingsV3RunGeoDataXKeen = runner
	t.Cleanup(func() { settingsV3RunGeoDataXKeen = old })
}

func TestGeoDataHoldsCanonicalLockAndSuppressesXrayWatchdog(t *testing.T) {
	a, _, intent := prepareXrayRuntimeWatchTest(t)
	dir := t.TempDir()
	a.cfg.XKeenPath = filepath.Join(dir, "xkeen")
	if err := os.WriteFile(a.cfg.XKeenPath, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FREENET_SETTINGS_V3_STATE", filepath.Join(dir, "geodata.state"))
	t.Setenv("FREENET_AUTOMATION_STATE", filepath.Join(dir, "automation.state"))
	if err := writeXrayRuntimeDesiredState(intent, xrayRuntimeDesiredRunning); err != nil {
		t.Fatal(err)
	}
	online := true
	starts := 0
	installXrayRuntimeMocks(t, &online, func(*app, context.Context) error {
		starts++
		return nil
	})
	entered := make(chan struct{})
	release := make(chan struct{})
	stubGeoDataRunner(t, func(ctx context.Context, path string) error {
		if path != a.cfg.XKeenPath {
			return errors.New("unexpected xkeen binary")
		}
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	done := make(chan error, 1)
	go func() { done <- a.runV3GeoData(context.Background()) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("GeoData native update did not start")
	}
	if _, err := os.Stat(a.cfg.LockPath); err != nil {
		t.Fatalf("GeoData did not hold canonical mutation lock: %v", err)
	}
	online = false // Native XKeen may temporarily stop Xray while updating.
	state := &xrayRuntimeWatchState{}
	now := time.Date(2026, 10, 19, 14, 5, 0, 0, time.UTC)
	a.runXrayRuntimeWatchStep(now, state)
	a.runXrayRuntimeWatchStep(now.Add(30*time.Second), state)
	if starts != 0 || state.IncidentOpen {
		t.Fatalf("watchdog raced native GeoData updater: starts=%d incident=%v", starts, state.IncidentOpen)
	}
	online = true // XKeen restored the pre-update runtime state.
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("expected successful coordinated update: %v", err)
	}
	if _, err := os.Stat(a.cfg.LockPath); !os.IsNotExist(err) {
		t.Fatalf("GeoData left the canonical lock behind: %v", err)
	}
	status := v3ParseState(settingsV3StatePath())
	if status["GEODATA_RESULT"] != "success" {
		t.Fatalf("success not recorded after matching runtime state: %+v", status)
	}
}

func TestGeoDataBusyNeverRunsNativeUpdaterOrOverwritesLastSuccess(t *testing.T) {
	a, _ := prepareGeoDataMutexTest(t)
	if err := os.Mkdir(a.cfg.LockPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := v3WriteState(map[string]string{"GEODATA_LAST": "2026-10-13T00:00:00Z", "GEODATA_RESULT": "success"}); err != nil {
		t.Fatal(err)
	}
	ran := false
	stubGeoDataRunner(t, func(context.Context, string) error { ran = true; return nil })
	if err := a.runV3GeoData(context.Background()); err == nil || ran {
		t.Fatalf("busy update did not fail closed: err=%v ran=%v", err, ran)
	}
	if _, err := os.Stat(a.cfg.LockPath); err != nil {
		t.Fatalf("busy updater removed a lock owned by another operation: %v", err)
	}
	s := v3ParseState(settingsV3StatePath())
	if s["GEODATA_RESULT"] != "success" || s["GEODATA_LAST"] != "2026-10-13T00:00:00Z" {
		t.Fatalf("busy path forged new geodata receipt: %+v", s)
	}
}

func TestGeoDataFailureAndRuntimeMismatchReleaseLockWithoutFalseSuccess(t *testing.T) {
	for _, tc := range []struct {
		name       string
		nativeFail bool
	}{
		{"native failure", true},
		{"Xray missing after updater", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := prepareGeoDataMutexTest(t)
			online := true
			installXrayRuntimeMocks(t, &online, nil)
			stubGeoDataRunner(t, func(context.Context, string) error {
				if tc.nativeFail {
					return errors.New("fixture native XKeen failure")
				}
				online = false
				return nil
			})
			err := a.runV3GeoData(context.Background())
			if err == nil {
				t.Fatal("GeoData claimed success despite native failure or missing Xray")
			}
			if _, statErr := os.Stat(a.cfg.LockPath); !os.IsNotExist(statErr) {
				t.Fatalf("failed update left canonical lock behind: %v", statErr)
			}
			state := v3ParseState(settingsV3StatePath())
			if state["GEODATA_RESULT"] != "failed" {
				t.Fatalf("GeoData false-positive success after runtime failure: %+v", state)
			}
			if strings.Contains(err.Error(), "fixture native XKeen failure") {
				t.Fatal("upstream native error was returned unsanitized")
			}
		})
	}
}

func TestGeoDataCannotRunDuringFreeNetUpdater(t *testing.T) {
	a, _ := prepareGeoDataMutexTest(t)
	if err := os.Mkdir(a.cfg.UpdateLock, 0700); err != nil {
		t.Fatal(err)
	}
	called := false
	stubGeoDataRunner(t, func(context.Context, string) error { called = true; return nil })
	if err := a.runV3GeoData(context.Background()); err == nil || called {
		t.Fatalf("GeoData ran across update lock: err=%v called=%v", err, called)
	}
	if _, err := os.Stat(a.cfg.LockPath); !os.IsNotExist(err) {
		t.Fatalf("GeoData created canonical lock during a blocked update: %v", err)
	}
}
