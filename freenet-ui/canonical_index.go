package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
)

const canonicalFirstPaintNav = `<nav class="nav" aria-label="Навигация Control Center">
      <button class="nav-btn active" data-page="overview"><span class="nav-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M4 10.5 12 4l8 6.5v8a1.5 1.5 0 0 1-1.5 1.5h-13A1.5 1.5 0 0 1 4 18.5v-8Z"/><path d="M9.5 20v-6h5v6"/></svg></span><span>Обзор</span></button>
      <button class="nav-btn" data-page="subscription"><span class="nav-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="8.5"/><circle cx="12" cy="12" r="4.5"/><path d="M12 3.5v2M20.5 12h-2M12 20.5v-2M3.5 12h2"/></svg></span><span>Подписка</span></button>
      <button class="nav-btn" data-page="settings"><span class="nav-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1-2.8 2.8-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6V21H10v-.1a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1L4.2 17l.1-.1a1.7 1.7 0 0 0 .3-1.9A1.7 1.7 0 0 0 3 14H3v-4h.1a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9L4.2 7 7 4.2l.1.1a1.7 1.7 0 0 0 1.9.3A1.7 1.7 0 0 0 10 3h4a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.3l.1-.1L19.8 7l-.1.1a1.7 1.7 0 0 0-.3 1.9 1.7 1.7 0 0 0 1.6 1h.1v4H21a1.7 1.7 0 0 0-1.6 1Z"/></svg></span><span>Настройки</span></button>
      <button class="nav-btn" data-page="network"><span class="nav-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M4 8h14m0 0-3-3m3 3-3 3M20 16H6m0 0 3-3m-3 3 3 3"/></svg></span><span>Маршрутизация</span></button>
      <button class="nav-btn" data-page="journal"><span class="nav-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="4" y="4" width="16" height="16" rx="2"/><path d="M8 8h8M8 12h8M8 16h6"/></svg></span><span>Журнал</span></button>
    </nav>`

const canonicalBootStyle = `<style id="freenetCanonicalBootStyle">
html.freenet-canonical-boot,html.freenet-canonical-boot body{min-height:100%;background:#07101b}
html.freenet-canonical-boot body{overflow:hidden;background:radial-gradient(circle at 68% -15%,#17345c 0,#0d1d32 42%,#081523 72%,#07101b 100%)}
html.freenet-canonical-boot body>*{visibility:hidden!important}
html.freenet-canonical-boot body::before{content:'';visibility:visible!important;position:fixed;inset:0;z-index:2147483646;background:radial-gradient(circle at 68% -15%,#17345c 0,#0d1d32 42%,#081523 72%,#07101b 100%)}
html.freenet-canonical-boot body::after{content:'FreeNet';visibility:visible!important;position:fixed;z-index:2147483647;left:50%;top:50%;transform:translate(-50%,-50%);font:900 25px/1 ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;letter-spacing:-.055em;color:#f7f9ff;text-shadow:0 0 26px rgba(105,159,255,.22)}
</style>`

const canonicalBootReleaseScript = `<script id="freenetCanonicalBootRelease">
(() => {
  const root = document.documentElement;
  let frames = 0;
  const canonicalReady = () => {
    const nav = document.querySelector('.sidebar>.nav');
    if (!nav) return false;
    const finalPages = ['overview','subscription','settings','network','journal'];
    const buttons = Array.from(nav.querySelectorAll(':scope > .nav-btn[data-page]'));
    const pages = buttons.map(button => button.dataset.page || '');
    const finalNavigation =
      buttons.length === finalPages.length &&
      finalPages.every((page, index) => pages[index] === page) &&
      buttons.every(button => !!button.querySelector('.nav-icon svg')) &&
      !nav.querySelector('[data-page="vpn"],[data-page="automation"],[data-page="system"],[data-page="access"]');
    return !!(
      finalNavigation &&
      document.querySelector('[data-page-view="settings"]') &&
      document.querySelector('[data-page-view="journal"]') &&
      document.getElementById('freenetAcceptedUXStyles') &&
      document.getElementById('freenetFinalShellPolishStyles') &&
      document.querySelector('.sidebar>.brand .fn-brand-lockup-svg')
    );
  };
  const release = () => {
    if (canonicalReady()) {
      requestAnimationFrame(() => requestAnimationFrame(() => {
        root.classList.remove('freenet-canonical-boot');
        root.dataset.freenetCanonicalReady = '1';
      }));
      return;
    }
    frames += 1;
    if (frames < 600) requestAnimationFrame(release);
    else root.dataset.freenetCanonicalBootStalled = '1';
  };
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', release, {once:true});
  else release();
})();
</script>`

// The first visible shell already uses final Settings/Journal routes. Routing keeps
// the historical internal "network" anchor because Routing v2 owns that page, but
// no retired navigation item is ever sent as part of the canonical first paint.
func canonicalizeControlCenterIndex(raw string) (string, error) {
	const htmlStart = `<html lang="ru">`
	const headStart = `<head>`
	const navStart = `<nav class="nav" aria-label="Навигация Control Center">`
	const sideBottom = `<div class="side-bottom"><span id="version">FreeNet UI</span><br><a id="xkeenLink" href="#" target="_blank" rel="noopener">Открыть XKeen UI ↗</a></div>`
	const labelsStart = `const pageLabels=`
	const labelsEnd = `;
let localPending=`

	if !strings.Contains(raw, htmlStart) || !strings.Contains(raw, headStart) {
		return "", errors.New("control center document markers are missing")
	}
	// A cold load must never paint the legacy static shell while accepted UX
	// assets are still executing. The server therefore sends the canonical
	// background gate and shared page geometry in the first HTML bytes. The gate
	// is removed only after the accepted shell styles and vector brand are mounted.
	raw = strings.Replace(raw, htmlStart, `<html lang="ru" class="freenet-canonical-boot">`, 1)
	raw = strings.Replace(raw, headStart, headStart+canonicalBootStyle+controlCenterLayoutCoherenceStyle, 1)

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
	// The canonical shell removes the legacy footer, so its old renderer must
	// not dereference those absent nodes. Otherwise loadStatus() catches a DOM
	// exception and returns nil to exact-connect verification despite valid JSON.
	raw = strings.Replace(raw, "el('version').textContent=", "if(el('version'))el('version').textContent=", 1)
	raw = strings.Replace(raw, "el('xkeenLink').href=xkeen;", "if(el('xkeenLink'))el('xkeenLink').href=xkeen;", 1)

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

	// Settings DNS, Routing v2, Config Studio parity, VPN-state reconciliation,
	// Settings profile-label hygiene and Overview quality memory are canonical
	// progressive enhancements. The production shell receives a sanitized inline
	// Routing v2 source, then the Config Studio layer upgrades only its config panel.
	if !strings.Contains(raw, `</body>`) {
		return "", errors.New("control center body end marker is missing")
	}
	routingV2 := `<script id="freenetRoutingV2">` + canonicalRoutingV2Script() + `</script>`
	configStudio := `<script id="freenetConfigStudioParity">` + string(configStudioParityAsset) + `</script>`
	progressive := `<script src="/api/settings-v3/assets/dns-ui.js"></script>` + routingV2 + configStudio
	vpnReconcile := `<script id="freenetVPNSelectorReconcile">` + string(vpnSelectorReconcileAsset) + `</script>`
	routingApply := `<script id="freenetRoutingApplyUI">` + string(routingApplyUIAsset) + `</script>`
	profileHygiene := `<script id="freenetProfileLabelHygiene">` + string(profileLabelHygieneAsset) + `</script>`
	raw = strings.Replace(raw, `</body>`, progressive+vpnReconcile+routingApply+profileHygiene+overviewCurrentQualityMemoryScript+canonicalBootReleaseScript+`</body>`, 1)
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
