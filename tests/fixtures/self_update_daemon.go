// self_update_daemon is a native HTTP/CLI process fixture, built only by CI.
// Its executable, argv, process lifetime and listening socket are real: updater
// tests do not replace /proc identity or the production runtime acceptance path.
package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var fixtureVersion = "fixture-unversioned"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "free-port" {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			panic(err)
		}
		fmt.Println(listener.Addr().(*net.TCPAddr).Port)
		_ = listener.Close()
		return
	}

	dir := os.Getenv("FREENET_FIXTURE_CONTROL")
	if dir == "" || !filepath.IsAbs(dir) {
		panic("fixture requires an absolute private control directory")
	}
	role := "cli"
	if len(os.Args) >= 3 && os.Args[1] == "-listen" {
		role = "daemon"
	}
	pid := strconv.Itoa(os.Getpid())
	write := func(name, value string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value+"\n"), 0600); err != nil {
			panic(err)
		}
	}
	write("process."+pid, role+" "+fixtureVersion)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	watchdog := time.NewTimer(10 * time.Minute)
	defer watchdog.Stop()

	var server *http.Server
	if role == "daemon" {
		listener, err := net.Listen("tcp4", os.Args[2])
		if err != nil {
			write("listen-failed."+pid, err.Error())
			os.Exit(2)
		}
		var mutex sync.Mutex
		counts := map[string]int{}
		mux := http.NewServeMux()
		for _, path := range []string{"/healthz", "/versionz"} {
			path := path
			mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
				key := strings.TrimPrefix(path, "/")
				mutex.Lock()
				counts[key]++
				count := counts[key]
				mutex.Unlock()
				write(key+"."+pid, strconv.Itoa(count))
				data, _ := os.ReadFile(filepath.Join(dir, fixtureVersion+"."+key))
				behavior := strings.TrimSpace(string(data))
				if behavior == "stall" {
					select {
					case <-r.Context().Done():
					case <-time.After(15 * time.Second):
					}
					return
				}
				if behavior == "bad" {
					http.Error(w, "fixture unavailable", http.StatusServiceUnavailable)
					return
				}
				if strings.HasPrefix(behavior, "wrong:") {
					until, _ := strconv.Atoi(strings.TrimPrefix(behavior, "wrong:"))
					if count <= until {
						_, _ = fmt.Fprintln(w, "fixture-wrong-version")
						return
					}
				}
				if path == "/healthz" {
					_, _ = fmt.Fprintln(w, "ok")
				} else {
					_, _ = fmt.Fprintln(w, fixtureVersion)
				}
			})
		}
		server = &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second}
		go func() { _ = server.Serve(listener) }()
		write("ready."+pid, listener.Addr().String())
	} else {
		write("ready."+pid, "cli")
	}

	select {
	case received := <-signals:
		write("signal."+pid, received.String())
	case <-watchdog.C:
		write("watchdog."+pid, "expired")
	}
	if server != nil {
		_ = server.Close()
	}
}
