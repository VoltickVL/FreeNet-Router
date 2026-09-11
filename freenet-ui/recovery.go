package main

import (
	"bytes"
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type recoveryPageData struct {
	Version       string
	Configured    bool
	Authenticated bool
	State         selfUpdateStateResponse
	Plan          selfUpdatePlanResponse
	PlanChecked   bool
	Notice        string
	Error         string
}

type recoveryCaptureWriter struct {
	header http.Header
	body   bytes.Buffer
	code   int
}

func newRecoveryCaptureWriter() *recoveryCaptureWriter {
	return &recoveryCaptureWriter{header: make(http.Header), code: http.StatusOK}
}

func (w *recoveryCaptureWriter) Header() http.Header { return w.header }

func (w *recoveryCaptureWriter) WriteHeader(code int) {
	if w.code != http.StatusOK || w.body.Len() != 0 {
		return
	}
	w.code = code
}

func (w *recoveryCaptureWriter) Write(p []byte) (int, error) {
	return w.body.Write(p)
}

func registerRecoveryRoutes(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /recovery", a.handleRecovery)
	mux.HandleFunc("POST /recovery/login", a.handleRecoveryLogin)
	mux.HandleFunc("POST /recovery/update", a.handleRecoveryUpdate)
}

func recoveryAPIRequest(r *http.Request, method, path string, body []byte) *http.Request {
	clone := r.Clone(r.Context())
	clone.Method = method
	clone.URL = &url.URL{Path: path}
	clone.RequestURI = path
	clone.Host = r.Host
	clone.ContentLength = int64(len(body))
	clone.Body = io.NopCloser(bytes.NewReader(body))
	clone.Header = r.Header.Clone()
	if len(body) > 0 {
		clone.Header.Set("Content-Type", "application/json")
	} else {
		clone.Header.Del("Content-Type")
	}
	return clone
}

func (a *app) recoveryState(r *http.Request) (selfUpdateStateResponse, int) {
	capture := newRecoveryCaptureWriter()
	req := recoveryAPIRequest(r, http.MethodGet, "/api/system/update/state", nil)
	a.requireAuth(a.handleSelfUpdateState)(capture, req)
	var state selfUpdateStateResponse
	if capture.code == http.StatusOK {
		_ = json.Unmarshal(capture.body.Bytes(), &state)
	}
	return state, capture.code
}

func (a *app) recoveryPlan(r *http.Request) (selfUpdatePlanResponse, int) {
	capture := newRecoveryCaptureWriter()
	req := recoveryAPIRequest(r, http.MethodGet, "/api/system/update/plan", nil)
	a.requireAuth(a.handleSelfUpdatePlan)(capture, req)
	var plan selfUpdatePlanResponse
	if len(capture.body.Bytes()) > 0 {
		_ = json.Unmarshal(capture.body.Bytes(), &plan)
	}
	return plan, capture.code
}

func (a *app) recoveryPage(r *http.Request) recoveryPageData {
	data := recoveryPageData{
		Version:       "v" + version,
		Configured:    a.credentialConfigured(),
		Authenticated: a.isAuthenticated(r),
	}
	if !data.Authenticated {
		return data
	}
	if state, code := a.recoveryState(r); code == http.StatusOK {
		data.State = state
	}
	if r.URL.Query().Get("check") == "1" {
		data.PlanChecked = true
		plan, code := a.recoveryPlan(r)
		data.Plan = plan
		if code != http.StatusOK && plan.Error == "" {
			data.Error = "Не удалось получить безопасный план обновления. Изменения не выполнялись."
		}
	}
	return data
}

func (a *app) handleRecovery(w http.ResponseWriter, r *http.Request) {
	a.renderRecovery(w, http.StatusOK, a.recoveryPage(r))
}

func (a *app) handleRecoveryLogin(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		a.renderRecovery(w, http.StatusForbidden, recoveryPageData{Version: "v" + version, Configured: a.credentialConfigured(), Error: "Запрос отклонён: источник страницы не совпадает."})
		return
	}
	if !a.credentialConfigured() {
		a.renderRecovery(w, http.StatusConflict, recoveryPageData{Version: "v" + version, Error: "Пароль администратора FreeNet ещё не настроен. Первичная настройка выполняется в Control Center."})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		a.renderRecovery(w, http.StatusBadRequest, recoveryPageData{Version: "v" + version, Configured: true, Error: "Некорректный запрос авторизации."})
		return
	}
	password := r.FormValue("password")
	payload, _ := json.Marshal(passwordRequest{Password: password})
	capture := newRecoveryCaptureWriter()
	loginReq := recoveryAPIRequest(r, http.MethodPost, "/api/auth/login", payload)
	a.handleAuthLogin(capture, loginReq)
	for _, cookie := range capture.Header().Values("Set-Cookie") {
		w.Header().Add("Set-Cookie", cookie)
	}
	if capture.code == http.StatusOK {
		http.Redirect(w, r, "/recovery", http.StatusSeeOther)
		return
	}
	message := "Не удалось войти в FreeNet."
	var apiErr map[string]string
	if json.Unmarshal(capture.body.Bytes(), &apiErr) == nil {
		switch apiErr["error"] {
		case "invalid credentials":
			message = "Неверный пароль администратора."
		case "authentication setup required":
			message = "Пароль администратора FreeNet ещё не настроен."
		}
	}
	a.renderRecovery(w, capture.code, recoveryPageData{Version: "v" + version, Configured: true, Error: message})
}

func (a *app) handleRecoveryUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.isAuthenticated(r) {
		a.renderRecovery(w, http.StatusUnauthorized, recoveryPageData{Version: "v" + version, Configured: a.credentialConfigured(), Error: "Для восстановления требуется вход администратора."})
		return
	}
	if !sameOrigin(r) {
		data := a.recoveryPage(r)
		data.Error = "Запрос обновления отклонён: источник страницы не совпадает."
		a.renderRecovery(w, http.StatusForbidden, data)
		return
	}

	plan, planCode := a.recoveryPlan(r)
	if planCode != http.StatusOK || !plan.Success || !plan.Ready || !plan.UpdateAvailable || !plan.ManifestVerified || !validReleaseTag(plan.TargetTag) {
		data := a.recoveryPage(r)
		data.PlanChecked = true
		data.Plan = plan
		data.Error = "Безопасный план обновления не подтверждён. FreeNet ничего не изменил."
		a.renderRecovery(w, http.StatusConflict, data)
		return
	}

	payload, _ := json.Marshal(selfUpdateApplyRequest{TargetTag: plan.TargetTag})
	capture := newRecoveryCaptureWriter()
	applyReq := recoveryAPIRequest(r, http.MethodPost, "/api/system/update/apply", payload)
	a.requireAuth(a.handleSelfUpdateApply)(capture, applyReq)
	if capture.code != http.StatusAccepted {
		message := "Не удалось запустить обновление. Изменения не повторяются автоматически."
		var apiErr map[string]any
		if json.Unmarshal(capture.body.Bytes(), &apiErr) == nil {
			if value, ok := apiErr["error"].(string); ok && value != "" {
				message += " Причина: " + value
			}
		}
		data := a.recoveryPage(r)
		data.PlanChecked = true
		data.Plan = plan
		data.Error = message
		a.renderRecovery(w, capture.code, data)
		return
	}

	data := recoveryPageData{
		Version:       "v" + version,
		Configured:    true,
		Authenticated: true,
		PlanChecked:   true,
		Plan:          plan,
		Notice:        "Обновление запущено через штатный FreeNet updater. Страница может временно стать недоступной во время перезапуска. После возврата откройте /recovery снова и проверьте итоговый STATE, PRIMARY ERROR и ROLLBACK.",
	}
	a.renderRecovery(w, http.StatusAccepted, data)
}

func (a *app) renderRecovery(w http.ResponseWriter, code int, data recoveryPageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_ = recoveryTemplate.Execute(w, data)
}

func recoveryBodyContainsMainAssets(body string) bool {
	lower := strings.ToLower(body)
	for _, forbidden := range []string{"<script", "index.html", "self-update.js", "vpn-ux-fix.js", "operation-coordinator.js", "accepted-ux.js"} {
		if strings.Contains(lower, forbidden) {
			return true
		}
	}
	return false
}

var recoveryTemplate = template.Must(template.New("recovery").Parse(`<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>FreeNet Recovery</title>
<style>
:root{color-scheme:dark;font-family:Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#06111f;color:#edf6ff}
*{box-sizing:border-box}body{margin:0;min-height:100vh;background:radial-gradient(circle at 70% 0,#102e52 0,#071626 42%,#040b14 100%);padding:28px}
main{max-width:760px;margin:0 auto}.brand{display:flex;align-items:center;gap:10px;font-size:24px;font-weight:800;margin-bottom:18px}.mark{color:#69a8ff;font-size:26px}.net{color:#79aaff}
.card{background:rgba(7,24,42,.94);border:1px solid #28547c;border-radius:18px;padding:22px;margin:14px 0;box-shadow:0 18px 60px rgba(0,0,0,.25)}
h1{font-size:27px;margin:0 0 6px}h2{font-size:18px;margin:0 0 14px}.muted{color:#9eb4ca;line-height:1.55}.grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}.fact{border:1px solid #21486c;border-radius:12px;padding:12px;background:#091c30}.fact small{display:block;color:#8da8c3;margin-bottom:4px}.fact strong{overflow-wrap:anywhere}
.ok{color:#4be3ae}.warn{color:#ffcc70}.bad{color:#ff8b8b}.notice{border:1px solid #23785f;background:#0a332c;border-radius:12px;padding:12px;line-height:1.5}.error{border:1px solid #914454;background:#351823;border-radius:12px;padding:12px;line-height:1.5}
form{margin-top:14px}label{display:block;color:#a8bdd1;font-size:13px;margin-bottom:6px}input{width:100%;padding:12px 13px;border-radius:10px;border:1px solid #315f86;background:#071522;color:#fff;font-size:15px;outline:none}input:focus{border-color:#68a8ff}
button,.button{display:inline-block;border:0;border-radius:10px;padding:11px 16px;background:#2f7df4;color:white;font-weight:700;text-decoration:none;cursor:pointer}.secondary{background:#173451;border:1px solid #315f86}.dangerButton{background:#cc4d55}button:disabled{opacity:.5;cursor:not-allowed}.actions{display:flex;gap:10px;flex-wrap:wrap;margin-top:14px}
code{color:#d7eaff}.foot{font-size:12px;color:#7892aa;margin-top:18px;line-height:1.6}@media(max-width:620px){body{padding:16px}.grid{grid-template-columns:1fr}.card{padding:17px}}
</style>
</head>
<body><main>
<div class="brand"><span class="mark">◇</span><span>Free<span class="net">Net</span></span></div>
<section class="card">
<h1>Аварийное восстановление</h1>
<p class="muted">Независимый путь FreeNet для случая, когда основной Control Center не загружается или его интерфейс повреждён. Эта страница не использует frontend Overview.</p>
<div class="grid">
<div class="fact"><small>Версия FreeNet</small><strong>{{.Version}}</strong></div>
<div class="fact"><small>Авторизация</small><strong>{{if .Authenticated}}<span class="ok">выполнена</span>{{else if .Configured}}<span class="warn">требуется вход</span>{{else}}<span class="bad">не настроена</span>{{end}}</strong></div>
</div>
</section>
{{if .Error}}<div class="error">{{.Error}}</div>{{end}}
{{if .Notice}}<div class="notice">{{.Notice}}</div>{{end}}
{{if not .Authenticated}}
<section class="card">
<h2>Вход администратора</h2>
{{if .Configured}}
<p class="muted">Используется существующий локальный пароль FreeNet. Пароль не отображается и не сохраняется этой страницей.</p>
<form method="post" action="/recovery/login" autocomplete="off">
<label for="password">Пароль администратора</label>
<input id="password" name="password" type="password" minlength="12" maxlength="256" required autocomplete="current-password">
<div class="actions"><button type="submit">Войти</button><a class="button secondary" href="/">Обычный Control Center</a></div>
</form>
{{else}}
<p class="muted">Локальная авторизация ещё не настроена. Аварийная страница не создаёт новый credential: завершите первичную настройку в обычном Control Center.</p>
<div class="actions"><a class="button secondary" href="/">Открыть Control Center</a></div>
{{end}}
</section>
{{else}}
<section class="card">
<h2>Состояние updater</h2>
<div class="grid">
<div class="fact"><small>STATE</small><strong>{{if .State.State}}{{.State.State}}{{else}}IDLE{{end}}</strong></div>
<div class="fact"><small>Update lock</small><strong>{{if .State.UpdateLockHeld}}<span class="warn">занят</span>{{else}}свободен{{end}}</strong></div>
<div class="fact"><small>From → target</small><strong>{{if .State.FromVersion}}{{.State.FromVersion}}{{else}}—{{end}} → {{if .State.TargetVersion}}{{.State.TargetVersion}}{{else}}—{{end}}</strong></div>
<div class="fact"><small>Rollback</small><strong>{{if .State.RollbackState}}{{.State.RollbackState}}{{else}}—{{end}}</strong></div>
</div>
{{if .State.PrimaryError}}<p class="error"><strong>PRIMARY ERROR:</strong> {{.State.PrimaryError}}</p>{{end}}
{{if .State.Message}}<p class="muted">{{.State.Message}}</p>{{end}}
{{if .State.UpdatedAt}}<p class="foot">Последнее изменение state: {{.State.UpdatedAt}}</p>{{end}}
</section>
<section class="card">
<h2>Проверка релиза</h2>
{{if .PlanChecked}}
<div class="grid">
<div class="fact"><small>Current</small><strong>{{if .Plan.CurrentVersion}}{{.Plan.CurrentVersion}}{{else}}{{.Version}}{{end}}</strong></div>
<div class="fact"><small>Latest / target</small><strong>{{if .Plan.TargetTag}}{{.Plan.TargetTag}}{{else if .Plan.LatestVersion}}{{.Plan.LatestVersion}}{{else}}—{{end}}</strong></div>
<div class="fact"><small>Manifest / SHA-256</small><strong>{{if .Plan.ManifestVerified}}<span class="ok">проверен</span>{{else}}<span class="bad">не подтверждён</span>{{end}}</strong></div>
<div class="fact"><small>Обновление</small><strong>{{if .Plan.UpdateAvailable}}<span class="ok">доступно</span>{{else}}не требуется{{end}}</strong></div>
</div>
{{if .Plan.Error}}<p class="error">{{.Plan.Error}}</p>{{end}}
<div class="actions"><a class="button secondary" href="/recovery?check=1">Проверить снова</a>
{{if and .Plan.Success .Plan.Ready .Plan.UpdateAvailable .Plan.ManifestVerified}}
<form method="post" action="/recovery/update" style="margin:0"><button class="dangerButton" type="submit">Обновить до {{.Plan.TargetTag}}</button></form>
{{end}}</div>
{{else}}
<p class="muted">Проверка запускается только вручную. Если GitHub или WAN недоступны, сама recovery-страница остаётся локально работоспособной.</p>
<div class="actions"><a class="button secondary" href="/recovery?check=1">Проверить обновление</a></div>
{{end}}
</section>
<section class="card">
<h2>Безопасный контракт</h2>
<p class="muted">Recovery использует тот же штатный updater FreeNet: <code>plan → backup → SHA-256 → staging → apply → post-acceptance → rollback</code>. При FAILED, ROLLBACK_FAILED или UNKNOWN автоматического повторения нет.</p>
<div class="actions"><a class="button secondary" href="/">Вернуться в Control Center</a></div>
</section>
{{end}}
<p class="foot">FreeNet Recovery — только восстановление приложения. VPN, DNS и routing не изменяются сами по себе.</p>
</main></body></html>`))
