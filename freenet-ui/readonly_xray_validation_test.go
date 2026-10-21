package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadOnlyConfigValidationOwnsItsChildUntilCleanup(t *testing.T) {
	for _, cancelWorker := range []bool{false, true} {
		name := "success"
		if cancelWorker {
			name = "cancel"
		}
		t.Run(name, func(t *testing.T) {
			a, records, aliases := setupReadonlyWorkerFixture(t, "test")
			configDir := t.TempDir()
			path := filepath.Join(configDir, "00_fetch.json")
			config := []byte(`{"inbounds":[{"listen":"127.0.0.1","port":12345}]}`)
			if err := os.WriteFile(path, config, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			done := make(chan error, 1)
			go func() {
				done <- runReadOnlyXrayValidation(ctx, a.routingXrayBin(), a.cfg.GeoDataDir, configDir)
				close(done)
			}()
			defer func() {
				cancel()
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Error("config validator did not drain after cancellation")
				}
			}()
			record := waitReadonlyWorkerRecord(t, records, "test")
			assertReadonlyWorkerIsolation(t, record)
			assertReadonlyWorkerLease(t)
			// The config is supplied by the caller and must survive. The worker
			// owns only its separate executable-alias directory.
			record.Dir = filepath.Dir(record.Arg0)
			cutover := readonlyWorkerCutoverAfterCleanup(t, []readonlyWorkerRecord{record})
			if cancelWorker {
				cancel()
			} else if err := os.WriteFile(filepath.Join(records, "test.json.release"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if (err != nil) != cancelWorker {
					t.Fatalf("cancel=%v validation error=%v", cancelWorker, err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("config validator did not terminate")
			}
			if err := <-cutover; err != nil {
				t.Fatal(err)
			}
			if actual, err := os.ReadFile(path); err != nil || string(actual) != string(config) {
				t.Fatal("read-only validation changed or removed the supplied config")
			}
			if entries, err := os.ReadDir(aliases); err != nil || len(entries) != 0 {
				t.Fatal("validator alias directory was not cleaned")
			}
		})
	}
}
