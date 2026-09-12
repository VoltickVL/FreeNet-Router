package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
)

const canonicalFirstPaintNav = `<nav class="nav" aria-label="Навигация Control Center">
      <button class="nav-btn active" data-page="overview"><span class="nav-icon">⌂</span>Обзор</button>
      <button class="nav-btn" data-page="subscription"><span class="nav-icon">◎</span>Подписка</button>
      <button class="nav-btn" data-page="automation"><span class="nav-icon">⚙</span>Настройки</button>
      <button class="nav-btn" data-page="network"><span class="nav-icon">⇄</span>Маршрутизация</button>
      <button class="nav-btn" data-page="access"><span class="nav-icon">☷</span>Журнал</button>
    </nav>`

// The three compatibility data-page values above are deliberately internal only.
// Existing scripts still use those DOM anchors while Settings/Routing/Journal are
// mounted. Their legacy labels are never sent to the browser and pageLabels does
// not expose the old routes, so an old hash safely falls back to Overview during
// bootstrap instead of painting a retired page.
func canonicalizeControlCenterIndex(raw string) (string, error) {
	const navStart = `<nav class="nav" aria-label="Навигация Control Center">`
	const sideBottom = `<div class="side-bottom"><span id="version">FreeNet UI</span><br><a id="xkeenLink" href="#" target="_blank" rel="noopener">Открыть XKeen UI ↗</a></div>`
	const labelsStart = `const pageLabels=`
	const labelsEnd = `;
let localPending=`

	start := strings.Index(raw, navStart)
	if start < 0 {
		return "", errors.New("control center navigation marker is missing")
	}
	endRel := strings.Index(raw[start:], `</nav>`)
	if endRel < 0 {
		return "", errors.New("control center navigation end marker is missing")
	}
	end := start + endRel + len(`</nav>`)
	raw = raw[:start] + canonicalFirstPaintNav + raw[end:]

	if !strings.Contains(raw, sideBottom) {
		return "", errors.New("legacy sidebar footer marker is missing")
	}
	raw = strings.Replace(raw, sideBottom, "", 1)

	labelsAt := strings.Index(raw, labelsStart)
	if labelsAt < 0 {
		return "", errors.New("page label marker is missing")
	}
	labelsTail := strings.Index(raw[labelsAt:], labelsEnd)
	if labelsTail < 0 {
		return "", errors.New("page label end marker is missing")
	}
	labelsEndAt := labelsAt + labelsTail
	// Only pages that physically exist in the initial document are routable here.
	// The canonical bootstrap later adds Settings/Routing/Journal and their labels.
	canonicalLabels := `const pageLabels={overview:'Обзор',subscription:'Подписка'}`
	raw = raw[:labelsAt] + canonicalLabels + raw[labelsEndAt:]
	return raw, nil
}

type canonicalIndexCapture struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *canonicalIndexCapture) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *canonicalIndexCapture) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *canonicalIndexCapture) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(p)
}

// Register an exact-root route that canonicalizes the static shell before the
// first byte of HTML reaches the browser. GET / remains the fallback asset route.
// This removes the legacy first-paint without adding another client-side overlay.
func registerCanonicalIndexRoute(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		capture := &canonicalIndexCapture{}
		a.handleIndex(capture, r)
		status := capture.status
		if status == 0 {
			status = http.StatusOK
		}
		for key, values := range capture.Header() {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write(capture.body.Bytes())
			return
		}

		html, err := canonicalizeControlCenterIndex(capture.body.String())
		if err != nil {
			http.Error(w, "UI unavailable", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, html)
	})
}
