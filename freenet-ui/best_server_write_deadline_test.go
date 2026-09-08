package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBestServerResponseOutlivesDefaultWriteDeadline(t *testing.T) {
	// Real TCP responses reproduce the server-side cutoff. No router, Xray,
	// subscription or external endpoint is contacted.
	for _, extended := range []bool{false, true} {
		name := "default response is cut off"
		if extended {
			name = "scan response survives"
		}
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if extended && !prepareBestServerResponse(w) {
					return
				}
				time.Sleep(250 * time.Millisecond)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"success":true,"candidates":[]}`)
			}))
			srv.Config.WriteTimeout = 100 * time.Millisecond
			srv.Start()
			defer srv.Close()
			client := srv.Client()
			client.Timeout = 5 * time.Second
			response, err := client.Get(srv.URL)
			if !extended {
				if err == nil {
					response.Body.Close()
					t.Fatal("control response unexpectedly survived expired write deadline")
				}
				return
			}
			if err != nil {
				t.Fatalf("bounded scan response lost: %v", err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil || response.StatusCode != http.StatusOK || string(body) != `{"success":true,"candidates":[]}` {
				t.Fatalf("invalid scan response: status=%d body=%q err=%v", response.StatusCode, body, err)
			}
		})
	}
}

func TestBothFullScanHandlersPrepareBoundedResponse(t *testing.T) {
	for _, file := range []string{"best_server_quality.go", "best_server_ux.go"} {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		source := string(body)
		prepare := strings.Index(source, "if !prepareBestServerResponse(w)")
		scan := strings.Index(source, "context.WithTimeout(r.Context(), bestServerQualityScanTimeout)")
		if prepare < 0 || scan < prepare {
			t.Fatalf("%s must prepare the response before starting a full scan", file)
		}
	}
}
