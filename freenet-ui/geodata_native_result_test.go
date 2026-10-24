package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeoDataNativeRejectsFalseSuccessAndOnlyReturnsFixedErrors(t *testing.T) {
	completed := "Обновление установленных баз GeoFile/GeoIPSET\n"
	success := "  GeoSite V2Fly успешно обновлён\n"
	cases := []struct {
		name       string
		output     string
		runErr     error
		truncated  bool
		kind       string
	}{
		{name: "successful file and complete", output: completed + success + "завершено\n"},
		{name: "success with warning", output: completed + "Предупреждение: mirror недоступен; direct сработал\n" + success + "завершено\n"},
		{name: "download failed after another file succeeded", output: completed + success + "  Ошибка: не удалось загрузить GeoIP Re:filter\nОбновление установленных баз GeoFile/GeoIPSET завершено\n", kind: "download_failed"},
		{name: "HTML stub still exits zero", output: completed + success + "Ошибка: получена HTML-страница вместо dat-файла\nзавершено", kind: "html_stub"},
		{name: "short or corrupt dat", output: completed + success + "Ошибка: загруженный файл слишком мал (1024 bytes) или повреждён\nзавершено", kind: "invalid_dat"},
		{name: "no installed bases", output: "Не обнаружены базы GeoFile/GeoIPSET для обновления", kind: "missing_installed"},
		{name: "exit zero without individual confirmation", output: completed + "завершено", kind: "unconfirmed"},
		{name: "one file but no final confirmation", output: completed + success, kind: "unconfirmed"},
		{name: "empty output", output: "", kind: "unconfirmed"},
		{name: "native nonzero with private output", output: "private SUBSCRIPTION=https://example.invalid/uuid\n", runErr: errors.New("private token=should-not-leak"), kind: "native_failed"},
		{name: "truncated output", output: completed + success + "завершено", truncated: true, kind: "truncated"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := classifyGeoDataNativeResult([]byte(tc.output), tc.truncated, tc.runErr)
			if tc.kind == "" {
				if err != nil { t.Fatalf("expected confirmed native output, got %v", err) }
				return
			}
			var native *geoDataNativeFailure
			if !errors.As(err, &native) || native.kind != tc.kind {
				t.Fatalf("reason=%v want %q", err, tc.kind)
			}
			if text := err.Error(); strings.Contains(text, "example.invalid") || strings.Contains(text, "should-not-leak") ||
				strings.Contains(text, "SUBSCRIPTION") || strings.Contains(text, "private") {
				t.Fatalf("secret-bearing native output was exposed: %s", text)
			}
			if text, ok := safeGeoDataNativeFailure(err); !ok || text != err.Error() {
				t.Fatalf("safe native reason mapping was not retained: %v, %t", text, ok)
			}
		})
	}
	if text, ok := safeGeoDataNativeFailure(errors.New("token=secret")); ok || text != "" {
		t.Fatalf("arbitrary error became user-visible: %q %t", text, ok)
	}
}

func TestGeoDataBoundedOutputNeverBuffersUnlimitedNativeTranscript(t *testing.T) {
	var w geoDataBoundedOutput
	payload := bytes.Repeat([]byte{'A'}, geoDataNativeOutputLimit+4096)
	count, err := w.Write(payload)
	if err != nil || count != len(payload) {
		t.Fatalf("writer must discard excess without breaking subprocess: n=%d err=%v", count, err)
	}
	data, truncated := w.snapshot()
	if !truncated || len(data) != geoDataNativeOutputLimit {
		t.Fatalf("native transcript exceeded fixed in-memory envelope: %d, truncated=%t", len(data), truncated)
	}
	if _, ok := safeGeoDataNativeFailure(classifyGeoDataNativeResult(data, truncated, nil)); !ok {
		t.Fatal("truncated native transcript must fail closed with only a fixed safe message")
	}
}

func TestVerifiedGeoDataNativeExecutorRejectsExitZeroWithPerFileFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "xkeen")
	// No network, no Xray mutation: this fixture only prints the native
	// output that XKeen 2.0 can produce after a failed per-file download.
	script := "#!/bin/sh\nprintf '%s\\n' 'GeoSite V2Fly успешно обновлён' 'Ошибка: не удалось загрузить GeoIP Re:filter' 'Обновление установленных баз GeoFile/GeoIPSET завершено'\nexit 0\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil { t.Fatal(err) }
	err := runVerifiedGeoDataXKeen(context.Background(), path)
	var native *geoDataNativeFailure
	if !errors.As(err, &native) || native.kind != "download_failed" {
		t.Fatalf("native exit zero after failed per-file fetch must not become successful GeoData update: %v", err)
	}
}
