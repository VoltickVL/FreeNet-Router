// FreeNet 0.9 starts clean: a read-only Control Center over the installed
// XKeen/Xray stack. No legacy FreeNet packages, shell, scheduler or writers.
package main

import (
    "encoding/json"
    "errors"
    "flag"
    "fmt"
    "html/template"
    "net"
    "net/http"
    "os"
    "path/filepath"
    "strings"
    "time"
)

const buildVersion = "v0.9.0-dev"

type nativeOverview struct {
    Version        string `json:"version"`
    Mode           string `json:"mode"`
    Runtime        string `json:"runtime"`
    XKeen          string `json:"xkeen"`
    Xray           string `json:"xray"`
    XrayConfig     string `json:"xray_config"`
    LegacyFreeNet  string `json:"legacy_freenet"`
    LegacyService  string `json:"legacy_service"`
}

func observedPath(root, rel string, directory bool) string {
    if root == "" || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, "../") {
        return "UNKNOWN"
    }
    base, err := os.Lstat(root)
    if err != nil || !base.IsDir() || base.Mode()&os.ModeSymlink != 0 {
        return "UNKNOWN"
    }
    parts := strings.Split(filepath.Clean(rel), string(os.PathSeparator))
    cursor := root
    for i, part := range parts {
        if part == "" || part == "." || part == ".." {
            return "UNKNOWN"
        }
        cursor = filepath.Join(cursor, part)
        info, statErr := os.Lstat(cursor)
        if errors.Is(statErr, os.ErrNotExist) {
            return "MISSING"
        }
        if statErr != nil || info.Mode()&os.ModeSymlink != 0 {
            return "UNKNOWN"
        }
        final := i == len(parts)-1
        if !final && !info.IsDir() {
            return "UNKNOWN"
        }
        if final {
            if directory && info.IsDir() {
                return "PRESENT"
            }
            if !directory && info.Mode().IsRegular() {
                return "PRESENT"
            }
            return "UNKNOWN"
        }
    }
    return "UNKNOWN"
}

func nativeStatus(root string) nativeOverview {
    return nativeOverview{
        Version: buildVersion, Mode: "READ_ONLY", Runtime: "NOT_VERIFIED",
        XKeen: observedPath(root, "sbin/xkeen", false),
        Xray: observedPath(root, "sbin/xray", false),
        XrayConfig: observedPath(root, "etc/xray/configs", true),
        LegacyFreeNet: observedPath(root, "sbin/freenet-ui", false),
        LegacyService: observedPath(root, "etc/init.d/S99freenet-ui", false),
    }
}

// Stage and preview never bind to the LAN until separate authentication and
// cutover logic pass acceptance. An explicit wildcard bind is rejected too.
func loopbackListen(addr string) bool {
    host, port, err := net.SplitHostPort(addr)
    if err != nil || port == "" {
        return false
    }
    ip := net.ParseIP(host)
    return ip != nil && ip.IsLoopback()
}

var dashboard = template.Must(template.New("dashboard").Parse(`<!doctype html>
<html lang="ru"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>FreeNet 0.9 · Native Control Center</title>
<style>
:root{color-scheme:dark}body{margin:0;font:16px system-ui,-apple-system,sans-serif;background:#0e1523;color:#eff5ff}
main{max-width:760px;margin:8vh auto;padding:22px}.eyebrow{color:#8db8e4;letter-spacing:.15em;font-size:12px}
h1{font-size:36px;line-height:1.12;margin:10px 0}.sub{color:#a2b0c6;line-height:1.7}
.panel{margin-top:26px;border:1px solid #293850;border-radius:18px;background:#172238;padding:22px}
.row{display:flex;justify-content:space-between;gap:16px;border-bottom:1px solid #28354a;padding:13px 0}
.row:last-child{border:0}.muted{color:#9babc3}.badge{font-size:12px;border:1px solid #385a79;border-radius:100px;padding:5px 9px}
footer{font-size:13px;color:#93a4bc;margin-top:24px}
</style></head>
<body><main><div class="eyebrow">FREENET ROUTER / NATIVE FIRST</div>
<h1>Control Center 0.9</h1>
<p class="sub">Чистая версия поверх штатных XKeen и Xray. Этот предварительный режим только читает наличие компонентов: не меняет VPN, DNS, маршруты или службу FreeNet 0.8.</p>
<section class="panel"><div class="row"><strong>Состояние</strong><span class="badge">READ ONLY</span></div>
<div class="row"><span class="muted">XKeen</span><strong>{{.XKeen}}</strong></div>
<div class="row"><span class="muted">Xray</span><strong>{{.Xray}}</strong></div>
<div class="row"><span class="muted">Конфигурации Xray</span><strong>{{.XrayConfig}}</strong></div>
<div class="row"><span class="muted">FreeNet 0.8</span><strong>{{.LegacyFreeNet}}</strong></div>
<div class="row"><span class="muted">Служба 0.8</span><strong>{{.LegacyService}}</strong></div></section>
<footer>Версия {{.Version}} · Runtime: NOT_VERIFIED · Управляющие операции отключены</footer>
</main></body></html>`))

func handlers(root string) http.Handler {
    mux := http.NewServeMux()
    mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "text/plain; charset=utf-8")
        _, _ = w.Write([]byte("ok\n"))
    })
    mux.HandleFunc("GET /versionz", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "text/plain; charset=utf-8")
        _, _ = w.Write([]byte(buildVersion + "\n"))
    })
    mux.HandleFunc("GET /api/overview", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json; charset=utf-8")
        _ = json.NewEncoder(w).Encode(nativeStatus(root))
    })
    mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/" {
            http.NotFound(w, r)
            return
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        _ = dashboard.Execute(w, nativeStatus(root))
    })
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Cache-Control", "no-store")
        w.Header().Set("X-Content-Type-Options", "nosniff")
        w.Header().Set("X-Frame-Options", "DENY")
        w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
        mux.ServeHTTP(w, r)
    })
}

func selfTest() error {
    if !loopbackListen("127.0.0.1:1009") || loopbackListen("0.0.0.0:1009") {
        return errors.New("preview bind safety failed")
    }
    if buildVersion == "" {
        return errors.New("empty build version")
    }
    return nil
}

func main() {
    listen := flag.String("listen", "127.0.0.1:1009", "local-only preview listen address")
    root := flag.String("root", "/opt", "Entware root for read-only inventory")
    versionFlag := flag.Bool("version", false, "print exact candidate version")
    selftestFlag := flag.Bool("selftest", false, "safe offline candidate check")
    flag.Parse()

    if *versionFlag {
        fmt.Println(buildVersion)
        return
    }
    if *selftestFlag {
        if err := selfTest(); err != nil {
            fmt.Fprintln(os.Stderr, err)
            os.Exit(1)
        }
        fmt.Println("SELFTEST=SUCCESS")
        return
    }
    if !loopbackListen(*listen) {
        fmt.Fprintln(os.Stderr, "STOP: unauthenticated preview must bind to loopback only")
        os.Exit(2)
    }
    srv := &http.Server{
        Addr: *listen, Handler: handlers(*root),
        ReadHeaderTimeout: 5*time.Second, ReadTimeout: 10*time.Second,
        WriteTimeout: 10*time.Second, IdleTimeout: 30*time.Second,
        MaxHeaderBytes: 8192,
    }
    fmt.Printf("FreeNet %s | read-only preview | %s\n", buildVersion, *listen)
    if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}
