package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The official, SHA-256-verified archive is still never admitted without
// executing its candidate and confirming the exact selected version.
// These tests use throwaway executables; no router process is touched.
func TestXrayCoreCandidateVersionCheckReasons(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("candidate shell fixture requires POSIX")
	}
	writeFixture := func(name, script string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	good := writeFixture("good", "#!/bin/sh\necho 'Xray 26.10.31 (Xray, Penetrates Everything.)'\n")
	wrong := writeFixture("wrong", "#!/bin/sh\necho 'Xray 26.10.21 (Xray, Penetrates Everything.)'\n")
	failed := writeFixture("failed", "#!/bin/sh\nexit 42\n")
	slow := writeFixture("slow", "#!/bin/sh\nexec sleep 2\n")

	cases := []struct {
		name string
		path string
		timeout time.Duration
		want string
	}{
		{name: "exact version succeeds", path: good},
		{name: "different version fails closed", path: wrong, want: "reason=xray-candidate-version-mismatch"},
		{name: "nonzero exit distinguished from mismatch", path: failed, want: "reason=xray-candidate-version-exec"},
		{name: "missing executable distinguished from mismatch", path: filepath.Join(t.TempDir(), "not-found"), want: "reason=xray-candidate-version-exec"},
		{name: "slow version command has bounded timeout", path: slow, timeout: 20*time.Millisecond, want: "reason=xray-candidate-version-timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			cancel := func() {}
			if tc.timeout > 0 {
				ctx, cancel = context.WithTimeout(ctx, tc.timeout)
			}
			defer cancel()
			err := xrayCoreCandidateVersionCheck(ctx, tc.path, "v26.10.31")
			if tc.want == "" {
				if err != nil { t.Fatalf("valid candidate rejected: %v", err) }
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("candidate error = %v, want %s", err, tc.want)
			}
		})
	}
}

func TestXrayCoreCandidateVersionCheckDoesNotStartCanceledProbe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("candidate shell fixture requires POSIX")
	}
	marker := filepath.Join(t.TempDir(), "ran")
	script := filepath.Join(t.TempDir(), "candidate")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch '"+marker+"'\necho 'Xray 26.10.31'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := xrayCoreCandidateVersionCheck(ctx, script, "v26.10.31")
	if err == nil || !strings.Contains(err.Error(), "reason=xray-candidate-version-canceled") {
		t.Fatalf("canceled context = %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("canceled version probe must not execute staged candidate")
	}
}

func TestXrayCorePreparationGateDistinguishesExpiredDownload(t *testing.T) {
	if err := xrayCorePreparationGate(context.Background()); err != nil {
		t.Fatalf("active download context rejected: %v", err)
	}
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()
	if err := xrayCorePreparationGate(expired); err == nil || !strings.Contains(err.Error(), "reason=xray-core-preparation-timeout") {
		t.Fatalf("expired download should STOP before candidate execution, got %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := xrayCorePreparationGate(canceled); err == nil || !strings.Contains(err.Error(), "reason=xray-core-preparation-canceled") {
		t.Fatalf("canceled preparation should STOP, got %v", err)
	}
}

func TestXrayCoreCandidateVersionIndependentBudget(t *testing.T) {
	if runtime.GOOS == "windows" { t.Skip("candidate shell fixture requires POSIX") }
	path := filepath.Join(t.TempDir(), "candidate")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho 'Xray 26.10.31'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	// The old bug passed an exhausted 95-second catalog/download context
	// directly to the candidate. A fresh finite phase must accept a valid
	// executable; the preparation gate remains responsible for rejecting a
	// genuinely expired download *before* opening that new phase.
	downloadCtx, cancelDownload := context.WithCancel(context.Background())
	cancelDownload()
	if xrayCorePreparationGate(downloadCtx) == nil { t.Fatal("expired preparation admitted") }
	versionCtx, cancelVersion := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelVersion()
	if err := xrayCoreCandidateVersionCheck(versionCtx, path, "v26.10.31"); err != nil {
		t.Fatalf("separate valid version phase should pass: %v", err)
	}
}
