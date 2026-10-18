package main

// This file deliberately precedes every command-dispatch init in the Go build.
// The subprocess regression verifies that neither main nor cron dispatch runs
// when an older maintenance binary encounters a Beta transaction. Normal
// Stable boots are untouched. This is an update-only compatibility bridge,
// not an alternative VPN implementation or an arbitrary command endpoint.

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const legacyHandoffTarget = "v0.8.0-beta.5"

func legacyHandoffConfig(args []string) (config, bool, error) {
	cfg := config{}
	command := len(args) > 0 && !strings.HasPrefix(args[0], "-")
	if command {
		args = args[1:]
	}
	fs := flag.NewFlagSet("FreeNet recovery", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.Listen, "listen", defaultListen, "")
	fs.StringVar(&cfg.VPNPath, "vpn", defaultVPNPath, "")
	fs.StringVar(&cfg.FilterPath, "filter", defaultFilterPath, "")
	fs.StringVar(&cfg.OutPath, "outbound", defaultOutPath, "")
	fs.StringVar(&cfg.GeoDataDir, "geodata-dir", defaultGeoDataAssetDir, "")
	fs.StringVar(&cfg.XKeenPath, "xkeen", defaultXKeenPath, "")
	fs.StringVar(&cfg.LockPath, "updater-lock", defaultLockPath, "")
	fs.StringVar(&cfg.ConfigPath, "config", defaultConfigPath, "")
	fs.StringVar(&cfg.SubPath, "subscription", defaultSubPath, "")
	fs.StringVar(&cfg.SelfUpdatePath, "self-update", defaultSelfUpdatePath, "")
	fs.StringVar(&cfg.UpdateState, "update-state", defaultUpdateState, "")
	fs.StringVar(&cfg.UpdateLock, "update-lock", defaultUpdateLock, "")
	fs.DurationVar(&cfg.Timeout, "action-timeout", 95*time.Second, "")
	err := fs.Parse(args)
	if len(fs.Args()) != 0 && err == nil {
		err = errors.New("unexpected positional argument")
	}
	return cfg, command, err
}

// Only bounded regular state files may authorize normal Stable startup.
// A missing legacy file is normal; a symlink, unreadable or oversized one is
// not permission to start an older AUTO engine over unknown live state.
func legacyHandoffState(path string) (map[string]string, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64*1024 {
		return nil, errors.New("state is not a readable regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseKVOutput(string(data)), nil
}

func legacyHandoffReason(cfg config) string {
	update, err := legacyHandoffState(cfg.UpdateState)
	if err != nil {
		return "UPDATE_STATE_UNKNOWN"
	}
	if validBetaReleaseTag(update["FROM_VERSION"]) || validBetaReleaseTag(update["TARGET_VERSION"]) {
		return "BETA_HANDOFF"
	}
	checkpoint := strings.TrimSpace(os.Getenv("FREENET_VPN_TRANSACTION_DIR"))
	if checkpoint == "" {
		checkpoint = "/opt/var/lib/freenet/vpn-transaction.pending"
	}
	if _, err := os.Lstat(checkpoint); !os.IsNotExist(err) {
		return "VPN_CHECKPOINT_PRESERVED"
	}
	state, err := legacyHandoffState(settingsV3StatePath())
	if err != nil {
		return "VPN_STATE_UNKNOWN"
	}
	if strings.EqualFold(state["MUTATION_BLOCKED"], "yes") {
		return "VPN_ROLLBACK_UNCONFIRMED"
	}
	return ""
}

func init() {
	// Unit tests exercise the gate explicitly; subprocess tests build and start
	// the actual non-test executable to verify the pre-init safety boundary.
	name := filepath.Base(os.Args[0])
	if strings.HasSuffix(name, ".test") || strings.HasSuffix(name, ".test.exe") {
		return
	}
	cfg, command, parseErr := legacyHandoffConfig(os.Args[1:])
	if legacyHandoffReason(cfg) == "" {
		return
	}
	if command || parseErr != nil {
		fmt.Fprintln(os.Stderr, "FreeNet recovery-only: scheduled/CLI mutation is suspended; use authenticated /recovery")
		os.Exit(75)
	}
	a := &app{cfg: cfg, sem: make(chan struct{}, 1)}
	if err := serveLegacyHandoff(a); err != nil {
		fmt.Fprintln(os.Stderr, "FreeNet recovery listener failed:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func legacyHandoffUpdateBlocked(a *app) bool {
	state, err := legacyHandoffState(a.cfg.UpdateState)
	if err != nil {
		return true
	}
	return state["STATE"] == "ROLLBACK_FAILED" || state["ROLLBACK_STATE"] == "PENDING" ||
		state["ROLLBACK_STATE"] == "FAILED_UNKNOWN" || a.updateLockHeld()
}

func legacyHandoffPlan(a *app, r *http.Request) (selfUpdatePlanResponse, int) {
	req := recoveryAPIRequest(r, http.MethodGet, "/api/system/update/plan", nil)
	req.URL.RawQuery = "channel=beta&target=" + legacyHandoffTarget
	req.RequestURI = req.URL.RequestURI()
	capture := newRecoveryCaptureWriter()
	a.handleSelfUpdatePlan(capture, req)
	var plan selfUpdatePlanResponse
	_ = json.Unmarshal(capture.body.Bytes(), &plan)
	return plan, capture.code
}

func legacyHandoffPlanReady(plan selfUpdatePlanResponse, code int) bool {
	return code == http.StatusOK && plan.Success && plan.Ready && plan.ManifestVerified &&
		plan.UpdateAvailable && plan.TargetTag == legacyHandoffTarget
}

type legacyHandoffPage struct {
	Version       string
	Target        string
	Authenticated bool
	Blocked       bool
	Ready         bool
	Message       string
	State         selfUpdateStateResponse
}

func renderLegacyHandoff(a *app, w http.ResponseWriter, r *http.Request, code int, message string, ready bool) {
	data := legacyHandoffPage{Version: "v" + version, Target: legacyHandoffTarget,
		Authenticated: a.isAuthenticated(r), Message: message, Ready: ready}
	if data.Authenticated {
		data.State, _ = a.recoveryState(r)
		data.Blocked = legacyHandoffUpdateBlocked(a)
		data.Ready = data.Ready && !data.Blocked
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_ = legacyHandoffTemplate.Execute(w, data)
}

func legacyHandoffHandler(a *app) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok\n") })
	mux.HandleFunc("GET /versionz", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "v"+version+"\n") })
	mux.HandleFunc("GET /api/auth/status", a.handleAuthStatus)
	mux.HandleFunc("POST /api/auth/login", a.handleAuthLogin)
	mux.HandleFunc("POST /api/auth/logout", a.requireAuth(a.handleAuthLogout))
	mux.HandleFunc("POST /recovery/login", a.handleRecoveryLogin)
	mux.HandleFunc("GET /api/system/update/state", a.requireAuth(a.handleSelfUpdateState))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/recovery" {
			http.NotFound(w, r)
			return
		}
		ready, message := false, ""
		if r.URL.Query().Get("check") == "1" && a.isAuthenticated(r) && !legacyHandoffUpdateBlocked(a) {
			plan, code := legacyHandoffPlan(a, r)
			ready = legacyHandoffPlanReady(plan, code)
			if ready {
				message = "Точный Beta-релиз и SHA-256 подтверждены. Установка требует отдельного нажатия кнопки ниже."
			} else {
				message = "Проверка не пройдена; изменений нет. " + sanitizeOutput(plan.Error)
			}
		}
		renderLegacyHandoff(a, w, r, http.StatusOK, message, ready)
	})
	mux.HandleFunc("POST /recovery/continue", func(w http.ResponseWriter, r *http.Request) {
		if !a.isAuthenticated(r) || !sameOrigin(r) {
			http.Error(w, "authenticated same-origin request required", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		if err := r.ParseForm(); err != nil || r.FormValue("confirm") != "yes" || r.FormValue("target") != legacyHandoffTarget {
			http.Error(w, "explicit exact Beta confirmation required", http.StatusBadRequest)
			return
		}
		if legacyHandoffUpdateBlocked(a) {
			renderLegacyHandoff(a, w, r, http.StatusConflict, "Updater занят или его откат не подтверждён. Никаких изменений.", false)
			return
		}
		plan, code := legacyHandoffPlan(a, r)
		if !legacyHandoffPlanReady(plan, code) {
			renderLegacyHandoff(a, w, r, http.StatusConflict, "Точный план Beta не подтверждён. "+sanitizeOutput(plan.Error), false)
			return
		}
		payload, _ := json.Marshal(selfUpdateApplyRequest{TargetTag: legacyHandoffTarget, BetaAcknowledged: true})
		req := recoveryAPIRequest(r, http.MethodPost, "/api/system/update/apply", payload)
		capture := newRecoveryCaptureWriter()
		a.handleSelfUpdateApply(capture, req)
		var result actionResult
		_ = json.Unmarshal(capture.body.Bytes(), &result)
		message := "Установка не началась. " + sanitizeOutput(result.Error)
		if capture.code == http.StatusAccepted && result.Success {
			message = "Проверенное обновление запущено. Не выключайте роутер. Обновите страницу после перезапуска FreeNet."
		}
		renderLegacyHandoff(a, w, r, capture.code, message, false)
	})
	// Deliberately no VPN, Xray service, settings-save, upload, shell, arbitrary
	// target or latch-clear route. Existing authentication remains mandatory.
	return securityHeaders(mux)
}

func serveLegacyHandoff(a *app) error {
	listener, err := net.Listen("tcp", a.cfg.Listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	listeners := []net.Listener{listener}
	if addr := loopbackListenAddr(a.cfg.Listen); addr != "" {
		local, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		defer local.Close()
		listeners = append(listeners, local)
	}
	srv := &http.Server{Handler: legacyHandoffHandler(a), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: controlCenterWriteTimeout, IdleTimeout: 30 * time.Second}
	done := make(chan error, len(listeners))
	for _, ln := range listeners {
		go func(l net.Listener) { done <- srv.Serve(l) }(ln)
	}
	return <-done
}

var legacyHandoffTemplate = template.Must(template.New("legacy-handoff").Parse(`<!doctype html>
<html lang="ru"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>FreeNet Recovery</title><style>body{font:16px/1.55 system-ui,sans-serif;max-width:760px;margin:32px auto;padding:0 20px}button,input{font:inherit;padding:10px}section{border:1px solid #888;padding:18px;margin:18px 0}pre{white-space:pre-wrap;overflow-wrap:anywhere}</style>
<h1>FreeNet: безопасный переход с прежней Beta</h1><p>Установлено: {{.Version}}. Это промежуточный режим восстановления, не подтверждение работы VPN.</p>
<p>Автоматизация FreeNet и команды изменения VPN в этом режиме не запускаются. Конфигурации Xray, DNS, маршрутизация и незавершённая VPN-транзакция сохранены.</p>
{{if not .Authenticated}}<section><h2>Вход администратора FreeNet</h2><form method="post" action="/recovery/login"><label>Пароль <input type="password" name="password" autocomplete="current-password" required></label> <button>Войти</button></form></section>
{{else}}<section><h2>Состояние обновления</h2><pre>STATE={{.State.State}}
FROM={{.State.FromVersion}}
TARGET={{.State.TargetVersion}}
ROLLBACK={{.State.RollbackState}}
{{.State.Message}}</pre>{{if .Blocked}}<p>Обновление ещё выполняется или его откат требует проверки. Следующее применение запрещено.</p>{{end}}<p><a href="/recovery">Перечитать состояние</a></p></section>
<section><h2>Продолжить на {{.Target}}</h2><p>Это экспериментальная Beta. После обновления отдельно проверяются восстановление VPN и интернет у клиентов. Секреты и checkpoint не удаляются.</p><p>{{.Message}}</p>
{{if not .Blocked}}<p><a href="/recovery?check=1">Проверить точный Beta-релиз и SHA-256 — без установки</a></p>{{end}}
{{if .Ready}}<form method="post" action="/recovery/continue"><input type="hidden" name="target" value="{{.Target}}"><input type="hidden" name="confirm" value="yes"><button>Подтверждаю установку BETA {{.Target}}</button></form>{{end}}</section>{{end}}</html>`))
