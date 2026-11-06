// FreeNet 0.9 starts clean: a read-only Control Center over the installed
// XKeen/Xray stack. No legacy FreeNet packages, shell, scheduler or writers.
package main

import (
    "crypto/tls"
    "embed"
    "encoding/json"
    "io/fs"
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

// buildVersion is stamped from v09/VERSION by the standalone release build.
var buildVersion = "v0.9.0-dev"

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

type dashboardData struct {
    nativeOverview
    System nativeSystem
    Topology nativeTopology
    UpdateTargets []string
}

//go:embed web/*
var webAssets embed.FS
var dashboard = template.Must(template.ParseFS(webAssets, "web/index.html"))
var staticFS, staticFSError = fs.Sub(webAssets, "web")

func writeWebAsset(w http.ResponseWriter, filename, contentType string) {
    data, err := webAssets.ReadFile("web/"+filename)
    if err != nil { http.Error(w,"asset unavailable",http.StatusInternalServerError);return }
    w.Header().Set("Content-Type",contentType)
    _,_ = w.Write(data)
}

func handlers(root string) http.Handler {
    return handlersWithProc(root,"/proc")
}

func handlersWithProc(root,procRoot string) http.Handler {
    mux := http.NewServeMux()
    // One official offline validator at a time, never a second Xray server.
    validateGate := make(chan struct{},1)
    subscriptionGate := make(chan struct{},1)
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
    mux.HandleFunc("GET /api/native/xray/topology", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json; charset=utf-8")
        result, err := nativeXrayTopology(root)
        if err != nil { w.WriteHeader(http.StatusConflict) }
        _ = json.NewEncoder(w).Encode(result)
    })
    mux.HandleFunc("GET /api/native/system", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type","application/json; charset=utf-8")
        _ = json.NewEncoder(w).Encode(nativeSystemStatus(root,procRoot))
    })
    mux.HandleFunc("GET /api/native/versions",func(w http.ResponseWriter,r *http.Request) {
        w.Header().Set("Content-Type","application/json; charset=utf-8")
        _ = json.NewEncoder(w).Encode(detectNativeVersions(r.Context(),root))
    })
    mux.HandleFunc("GET /api/native/xray/validate",func(w http.ResponseWriter,r *http.Request) {
        w.Header().Set("Content-Type","application/json; charset=utf-8")
        select {
        case validateGate <- struct{}{}:
            defer func(){<-validateGate}()
        default:
            w.WriteHeader(http.StatusConflict)
            _ = json.NewEncoder(w).Encode(nativeXrayValidation{State:"BUSY",Runtime:"NOT_VERIFIED"})
            return
        }
        _ = json.NewEncoder(w).Encode(validateNativeXray(r.Context(),root))
    })
    mux.HandleFunc("POST /api/subscription/preview",func(w http.ResponseWriter,r *http.Request) {
        handleSubscriptionPreview(w,r,subscriptionGate)
    })
    mux.HandleFunc("GET /manifest.webmanifest",func(w http.ResponseWriter,r *http.Request) {
        writeWebAsset(w,"manifest.webmanifest","application/manifest+json; charset=utf-8")
    })
    mux.HandleFunc("GET /sw.js",func(w http.ResponseWriter,r *http.Request) {
        w.Header().Set("Service-Worker-Allowed","/")
        writeWebAsset(w,"sw.js","text/javascript; charset=utf-8")
    })
    if staticFSError==nil {
        mux.Handle("GET /static/",http.StripPrefix("/static/",http.FileServer(http.FS(staticFS))))
    }
    mux.HandleFunc("GET /",func(w http.ResponseWriter,r *http.Request) {
        if r.URL.Path!="/" { http.NotFound(w,r);return }
        w.Header().Set("Content-Type","text/html; charset=utf-8")
        topology, err := nativeXrayTopology(root)
        if err!=nil {topology=stoppedTopology()}
        state:=dashboardData{
            nativeOverview:nativeStatus(root),
            System:nativeSystemStatus(root,procRoot),
            Topology:topology,
            UpdateTargets:[]string{"FreeNet","XKeen","Xray-core","GeoData / GeoIPSET"},
        }
        if err:=dashboard.Execute(w,state);err!=nil { return }
    })
    return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
        if r.URL.Path=="/" || strings.HasPrefix(r.URL.Path,"/api/") ||
            r.URL.Path=="/healthz" || r.URL.Path=="/versionz" {
            w.Header().Set("Cache-Control","no-store")
        } else {
            // The public PWA shell may be cached; no credentials/API responses.
            w.Header().Set("Cache-Control","private, max-age=300")
        }
        w.Header().Set("X-Content-Type-Options","nosniff")
        w.Header().Set("X-Frame-Options","DENY")
        w.Header().Set("Referrer-Policy","no-referrer")
        w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self'; connect-src 'self'; manifest-src 'self'; worker-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
        mux.ServeHTTP(w,r)
    })
}

func selfTest() error {
    if staticFSError != nil {return errors.New("embedded web assets unavailable")}
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
    initAdminFlag := flag.Bool("init-admin",false,"initialize one-time administrator pairing credential")
    httpsListen := flag.String("https-listen","","explicit HTTPS router IP:port, requires cert/key/admin hash")
    tlsCert := flag.String("tls-cert","","trusted HTTPS certificate chain file")
    tlsKey := flag.String("tls-key","","HTTPS private key file")
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
    if *initAdminFlag {
        if *httpsListen!="" {fmt.Fprintln(os.Stderr,"STOP: setup credential without starting server");os.Exit(2)}
        token,err:=initializeAdminCredential(*root)
        if err!=nil{fmt.Fprintln(os.Stderr,"STOP: administrator credential cannot be initialized");os.Exit(2)}
        // This is a one-time code intended for the operator's LOCAL bootstrap
        // terminal; it must never be copied to GitHub/logs/diagnostics.
        fmt.Println("FREENET09_ADMIN_CODE="+token)
        return
    }
    if *httpsListen!="" {
        if !secureListenAddress(*httpsListen) || *tlsCert=="" || *tlsKey=="" {
            fmt.Fprintln(os.Stderr,"STOP: explicit HTTPS address and certificate required")
            os.Exit(2)
        }
        if _,err:=tls.LoadX509KeyPair(*tlsCert,*tlsKey);err!=nil {
            fmt.Fprintln(os.Stderr,"STOP: HTTPS certificate invalid")
            os.Exit(2)
        }
        auth,err:=loadAdminAuth(*root)
        if err!=nil{fmt.Fprintln(os.Stderr,"STOP: administrator enrollment missing or unsafe");os.Exit(2)}
        srv:=&http.Server{
            Addr:*httpsListen,Handler:secureControlCenter(*root,"/proc",auth),
            ReadHeaderTimeout:5*time.Second,ReadTimeout:10*time.Second,
            WriteTimeout:200*time.Second,IdleTimeout:30*time.Second,
            MaxHeaderBytes:8192,TLSConfig:&tls.Config{MinVersion:tls.VersionTLS12},
        }
        fmt.Printf("FreeNet %s | authenticated HTTPS Control Center | listener ready\n",buildVersion)
        if err:=srv.ListenAndServeTLS(*tlsCert,*tlsKey);!errors.Is(err,http.ErrServerClosed) {
            fmt.Fprintln(os.Stderr,"STOP: HTTPS listener failed")
            os.Exit(1)
        }
        return
    }
    if *tlsCert!="" || *tlsKey!="" {
        fmt.Fprintln(os.Stderr,"STOP: certificate flags require -https-listen")
        os.Exit(2)
    }
    if !loopbackListen(*listen) {
        fmt.Fprintln(os.Stderr, "STOP: unauthenticated preview must bind to loopback only")
        os.Exit(2)
    }
    srv := &http.Server{
        Addr: *listen, Handler: handlers(*root),
        ReadHeaderTimeout: 5*time.Second, ReadTimeout: 10*time.Second,
        WriteTimeout: 25*time.Second, IdleTimeout: 30*time.Second,
        MaxHeaderBytes: 8192,
    }
    fmt.Printf("FreeNet %s | read-only preview | %s\n", buildVersion, *listen)
    if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}
