package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

// A manual GeoData refresh may take longer than the HTTPS reverse proxy's
// request timeout. Never bind xkeen -ug to r.Context(): disconnecting a browser
// must not kill XKeen while it is replacing an installed GeoData file.
const manualGeoDataRunTimeout = 12 * time.Minute

var manualGeoDataUpdates operationCoordinator

// An injectable executor lets tests check request cancellation and single-flight
// without touching the router, installing .dat files or starting XKeen.
var manualGeoDataExecutor = func(a *app, ctx context.Context) error {
	return a.runV3GeoData(ctx)
}

type manualGeoDataOperationResponse struct {
	Success   bool            `json:"success"`
	Active    bool            `json:"active"`
	Operation *operationState `json:"operation,omitempty"`
	Error     string          `json:"error,omitempty"`
}

func manualGeoDataSafeError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "GeoData / GeoIP: истекло время обновления XKeen; результат не подтверждён. Проверьте журнал и состояние Xray."
	}
	if errors.Is(err, context.Canceled) {
		return "GeoData / GeoIP: операция прервана; результат не подтверждён."
	}
	if text, ok := safeGeoDataNativeFailure(err); ok {
		return text
	}
	// Only known, fixed messages may cross the API boundary. Subprocess stdout
	// and arbitrary error strings can contain credentials or subscription URLs.
	switch strings.TrimSpace(err.Error()) {
	case "geodata update blocked by active/unknown router mutation",
		"router mutation state changed before geodata update":
		return "GeoData отложены: другое изменение FreeNet/VPN ещё не завершено; Xray не затронут."
	case "geodata mutation lock is already held or unavailable":
		return "GeoData отложены: другая операция с VPN/Xray уже выполняется."
	case "geodata native update failed; Xray runtime changed; STOP",
		"geodata update changed Xray runtime state; manual diagnosis required":
		return "GeoData: изменение состояния Xray не подтверждено; STOP, требуется диагностика."
	case "geodata mutation lock cleanup failed or unknown",
		"geodata mutation lock is unavailable":
		return "GeoData: состояние блокировки Xray неизвестно; STOP, требуется диагностика."
	case "xkeen updater is unavailable":
		return "GeoData: штатный XKeen недоступен."
	case "native geodata update failed":
		return "GeoData / GeoIP: штатный XKeen завершился с ошибкой; обновление не подтверждено."
	default:
		return "GeoData / GeoIP: результат обновления неизвестен; проверьте журнал FreeNet."
	}
}

func (a *app) handleGeoDataManualStart(w http.ResponseWriter, _ *http.Request) {
	op, leader, conflict := manualGeoDataUpdates.begin("geodata-update", "native-xkeen")
	if conflict != nil {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusConflict, manualGeoDataOperationResponse{
			Success: false, Active: true, Operation: conflict,
			Error: "GeoData: другая ручная операция уже выполняется.",
		})
		return
	}
	if leader {
		// The operation lives beyond the HTTP request. runV3GeoData retains
		// the existing protected VPN/Xray lock and reports terminal evidence.
		go a.runGeoDataManualOperation(op)
	}
	state, active, _ := manualGeoDataUpdates.snapshot()
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusAccepted, manualGeoDataOperationResponse{
		Success: true, Active: active, Operation: &state,
	})
}

func (a *app) runGeoDataManualOperation(op *coordinatedOperation) {
	ctx, cancel := context.WithTimeout(context.Background(), manualGeoDataRunTimeout)
	defer cancel()
	defer func() {
		if recover() != nil {
			v3Mark("geodata", "failed", "GeoData: непредвиденное завершение, результат неизвестен; STOP.")
			manualGeoDataUpdates.finish(op, http.StatusInternalServerError, nil, false,
				"GeoData не подтверждены.", "GeoData: непредвиденная ошибка; STOP.")
		}
	}()
	err := manualGeoDataExecutor(a, ctx)
	if err != nil {
		manualGeoDataUpdates.finish(op, http.StatusBadGateway, nil, false,
			"GeoData не обновлены.", manualGeoDataSafeError(err))
		return
	}
	manualGeoDataUpdates.finish(op, http.StatusOK, nil, true,
		"GeoData / GeoIP обновлены, состояние Xray проверено.", "")
}

func (a *app) handleGeoDataManualStatus(w http.ResponseWriter, _ *http.Request) {
	state, active, ok := manualGeoDataUpdates.snapshot()
	w.Header().Set("Cache-Control", "no-store")
	if !ok {
		// A restart loses the in-memory operation. Do not claim success
		// or silently start a second native updater: persisted state/locks
		// still determine whether any router mutation is safe.
		writeJSON(w, http.StatusOK, manualGeoDataOperationResponse{
			Success: true, Active: false,
		})
		return
	}
	writeJSON(w, http.StatusOK, manualGeoDataOperationResponse{
		Success: true, Active: active, Operation: &state,
	})
}
