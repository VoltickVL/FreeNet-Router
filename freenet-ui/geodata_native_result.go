package main

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
)

// Only a bounded, in-memory native transcript is inspected. Never expose it
// to API responses, settings state, logs or Journal: XKeen may print local
// configuration, URLs or credentials while reporting a download failure.
const geoDataNativeOutputLimit = 256 << 10

type geoDataBoundedOutput struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	truncated bool
}

func (w *geoDataBoundedOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	count := len(p)
	remaining := geoDataNativeOutputLimit - w.buf.Len()
	if remaining > 0 {
		_, _ = w.buf.Write(p[:minInt(count, remaining)])
	}
	if count > remaining {
		w.truncated = true
	}
	return count, nil
}

func (w *geoDataBoundedOutput) snapshot() ([]byte, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buf.Bytes()...), w.truncated
}

type geoDataNativeFailure struct {
	kind string
}

func (e *geoDataNativeFailure) Error() string {
	if e == nil {
		return "GeoData: результат обновления неизвестен."
	}
	switch e.kind {
	case "download_failed":
		return "GeoData / GeoIP: не удалось загрузить одну или несколько баз."
	case "html_stub":
		return "GeoData / GeoIP: источник вернул HTML вместо базы .dat."
	case "invalid_dat":
		return "GeoData / GeoIP: скачанный файл слишком мал или повреждён."
	case "missing_installed":
		return "GeoData / GeoIP: установленные базы для обновления не обнаружены."
	case "unconfirmed":
		return "GeoData / GeoIP: XKeen не подтвердил обновление файлов; успех не засчитан."
	case "truncated":
		return "GeoData / GeoIP: вывод XKeen неполный, результат обновления неизвестен."
	case "native_failed":
		return "GeoData / GeoIP: штатный XKeen завершился с ошибкой."
	case "timeout":
		return "GeoData / GeoIP: время обновления XKeen истекло; успех не подтверждён."
	case "canceled":
		return "GeoData / GeoIP: операция XKeen прервана; успех не подтверждён."
	default:
		return "GeoData / GeoIP: результат обновления неизвестен."
	}
}

// XKeen 2.0 -ug currently prints its final "completed" message and can exit
// zero even after process_geo_file has printed a failed per-file download.
// Check known fixed phrases before the command status. At least one actual
// per-file success line and the final native completion marker are required;
// none of these checks claims cryptographic integrity or current upstream data.
func classifyGeoDataNativeResult(output []byte, truncated bool, runErr error) error {
	lower := strings.ToLower(string(output))
	switch {
	case truncated:
		return &geoDataNativeFailure{kind: "truncated"}
	case strings.Contains(lower, "html-страница вместо dat-файла") ||
		strings.Contains(lower, "html-страница вместо dat"):
		return &geoDataNativeFailure{kind: "html_stub"}
	case strings.Contains(lower, "загруженный файл слишком мал") ||
		strings.Contains(lower, "невозможно обновить. оставляем старый файл") ||
		strings.Contains(lower, "файл слишком мал") ||
		strings.Contains(lower, "повреждён"):
		return &geoDataNativeFailure{kind: "invalid_dat"}
	case strings.Contains(lower, "не удалось загрузить") ||
		strings.Contains(lower, "не удалось скачать"):
		return &geoDataNativeFailure{kind: "download_failed"}
	case strings.Contains(lower, "не обнаружены базы"):
		return &geoDataNativeFailure{kind: "missing_installed"}
	case runErr != nil:
		return &geoDataNativeFailure{kind: "native_failed"}
	case !strings.Contains(lower, "завершено") ||
		!strings.Contains(lower, "обновление установленных баз") ||
		!strings.Contains(lower, "успешно обновлён"):
		return &geoDataNativeFailure{kind: "unconfirmed"}
	default:
		return nil
	}
}

func runVerifiedGeoDataXKeen(ctx context.Context, path string) error {
	if ctx == nil {
		return &geoDataNativeFailure{kind: "native_failed"}
	}
	cmd := exec.CommandContext(ctx, path, "-ug")
	var output geoDataBoundedOutput
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return &geoDataNativeFailure{kind: "timeout"}
	}
	if ctx.Err() == context.Canceled {
		return &geoDataNativeFailure{kind: "canceled"}
	}
	out, truncated := output.snapshot()
	return classifyGeoDataNativeResult(out, truncated, err)
}

// This function is intentionally limited to safe, fixed error reasons. The
// concrete native output is never returned to the caller or made persistent.
func safeGeoDataNativeFailure(err error) (string, bool) {
	var native *geoDataNativeFailure
	if errors.As(err, &native) {
		return native.Error(), true
	}
	return "", false
}
