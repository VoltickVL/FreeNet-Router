package main

import (
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const connectorGrantTTL = 15 * time.Minute
const connectorGrantMaxBytes = 2048
const connectorGrantScope = "read:diagnostics"

type connectorGrant struct {
	Version int `json:"version"`
	Hash string `json:"hash"`
	Scope string `json:"scope"`
	IssuedAt time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
	LastUsedAt time.Time `json:"last_used_at,omitempty"`
}

func (a *app) connectorGrantPath() string {
	return filepath.Join(filepath.Dir(a.cfg.ConfigPath), "connector_grant.json")
}

// A corrupted/unsafe store is not a grant. Never follow symlinks or accept
// world-readable permission bits, even if the file contains only a digest.
func (a *app) readConnectorGrant() (connectorGrant, bool, error) {
	var grant connectorGrant
	info, err := os.Lstat(a.connectorGrantPath())
	if os.IsNotExist(err) { return grant, false, nil }
	if err != nil { return grant, false, err }
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() <= 0 || info.Size() > connectorGrantMaxBytes {
		return grant, false, errors.New("unsafe connector credential store")
	}
	data, err := os.ReadFile(a.connectorGrantPath())
	if err != nil { return grant, false, err }
	if err := json.Unmarshal(data, &grant); err != nil { return connectorGrant{}, false, errors.New("invalid connector credential store") }
	hash, err := hex.DecodeString(grant.Hash)
	if err != nil || len(hash) != 32 || grant.Version != 1 || grant.Scope != connectorGrantScope ||
		grant.ExpiresAt.IsZero() || !grant.ExpiresAt.After(grant.IssuedAt) ||
		grant.ExpiresAt.Sub(grant.IssuedAt) > connectorGrantTTL {
		return connectorGrant{}, false, errors.New("invalid connector grant")
	}
	return grant, true, nil
}

func (a *app) saveConnectorGrant(grant connectorGrant) error {
	data, err := json.Marshal(grant)
	if err != nil { return err }
	if err := os.MkdirAll(filepath.Dir(a.connectorGrantPath()), 0700); err != nil { return err }
	if err := atomicWrite(a.connectorGrantPath(), append(data, byte(10)), 0600); err != nil { return err }
	return os.Chmod(a.connectorGrantPath(), 0600)
}

func connectorSecureProvisioning(r *http.Request) bool {
	// Never trust user-supplied X-Forwarded-Proto for credential disclosure.
	return r.TLS != nil || (requestFromLoopback(r) && connectorLoopbackHost(r.Host))
}

func connectorLoopbackHost(raw string) bool {
	host := raw
	if parsed, _, err := net.SplitHostPort(raw); err == nil { host = parsed }
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") { return true }
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func connectorConfirm(w http.ResponseWriter, r *http.Request) bool {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"success":false,"error":"cross-origin request rejected"})
		return false
	}
	if r.Header.Get("Content-Type") != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]any{"success":false,"error":"application/json required"})
		return false
	}
	var request struct { Confirm bool `json:"confirm"` }
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || !request.Confirm {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success":false,"error":"explicit confirmation required"})
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success":false,"error":"invalid JSON body"})
		return false
	}
	return true
}

func (a *app) handleConnectorPair(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !connectorConfirm(w, r) { return }
	if !connectorSecureProvisioning(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"success":false,"error":"pairing requires direct TLS or local loopback"})
		return
	}
	a.connectorMu.Lock()
	defer a.connectorMu.Unlock()
	if _, existing, err := a.readConnectorGrant(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"success":false,"error":"credential store requires read-only reconciliation"})
		return
	} else if existing {
		// Explicit re-pair rotates the previous grant even if still active.
	}
	token, err := randomSessionToken()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"success":false,"error":"credential generation unavailable"})
		return
	}
	now := time.Now().UTC()
	grant := connectorGrant{Version:1, Hash:sessionDigest(token), Scope:connectorGrantScope, IssuedAt:now, ExpiresAt:now.Add(connectorGrantTTL)}
	if err := a.saveConnectorGrant(grant); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"success":false,"error":"credential persistence failed"})
		return
	}
	v3AppendEvent("connector", "success", "Локальный read-only доступ перевыпущен; срок действия 15 минут; сетевой туннель не включён.")
	writeJSON(w, http.StatusOK, map[string]any{
		"success":true,"token":token,"scope":connectorGrantScope,"expires_at":grant.ExpiresAt,
		"transport":"LOCAL_LOOPBACK_ONLY","mutation":"AUTHORIZATION_ONLY",
		"note":"Token shown once; external ChatGPT transport not installed",
	})
}

func (a *app) handleConnectorGrantStatus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	a.connectorMu.Lock()
	defer a.connectorMu.Unlock()
	grant, exists, err := a.readConnectorGrant()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"success":false,"active":false,"error":"connector credential state unavailable"})
		return
	}
	response := map[string]any{"success":true,"active":false,"scope":connectorGrantScope,"transport":"LOCAL_LOOPBACK_ONLY","external_connected":false}
	if exists {
		response["active"] = time.Now().Before(grant.ExpiresAt)
		response["expires_at"] = grant.ExpiresAt
		if !grant.LastUsedAt.IsZero() {response["last_used_at"] = grant.LastUsedAt}
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *app) handleConnectorRevoke(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !connectorConfirm(w, r) { return }
	a.connectorMu.Lock()
	defer a.connectorMu.Unlock()
	info, err := os.Lstat(a.connectorGrantPath())
	if err != nil && !os.IsNotExist(err) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"success":false,"error":"credential state unavailable"})
		return
	}
	if err == nil {
		// An unexpected symlink is never removed by automated credential ops.
		if !info.Mode().IsRegular() {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"success":false,"error":"unsafe credential state"})
			return
		}
		if err := os.Remove(a.connectorGrantPath()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"success":false,"error":"credential revocation failed"})
			return
		}
	}
	v3AppendEvent("connector", "success", "Локальный read-only доступ отозван.")
	writeJSON(w, http.StatusOK, map[string]any{"success":true,"active":false,"mutation":"AUTHORIZATION_ONLY"})
}

func (a *app) handleConnectorMachineDiagnostics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	// A user may reverse-proxy FreeNet accidentally; restrict machine API to
	// the actual loopback listener and Host as a second independent check.
	if !requestFromLoopback(r) || !connectorLoopbackHost(r.Host) ||
		r.Header.Get("Cookie") != "" || r.URL.Query().Has("token") {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"success":false,"error":"connector authorization required"})
		return
	}
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") || len(auth) != len("Bearer ")+64 {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"success":false,"error":"connector authorization required"})
		return
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	if _, err := hex.DecodeString(token); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"success":false,"error":"connector authorization required"})
		return
	}
	a.connectorMu.Lock()
	grant, exists, err := a.readConnectorGrant()
	if err != nil || !exists || !time.Now().Before(grant.ExpiresAt) ||
		subtle.ConstantTimeCompare([]byte(sessionDigest(token)), []byte(grant.Hash)) != 1 {
		a.connectorMu.Unlock()
		writeJSON(w, http.StatusUnauthorized, map[string]any{"success":false,"error":"connector authorization required"})
		return
	}
	now := time.Now().UTC()
	if a.connectorRateWindow.IsZero() || now.Sub(a.connectorRateWindow) >= time.Minute {
		a.connectorRateWindow, a.connectorRateCount = now, 0
	}
	if a.connectorRateCount >= 60 {
		a.connectorMu.Unlock()
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"success":false,"error":"connector rate limit exceeded"})
		return
	}
	a.connectorRateCount++
	if grant.LastUsedAt.IsZero() || now.Sub(grant.LastUsedAt) >= time.Minute {
		grant.LastUsedAt = now
		if err := a.saveConnectorGrant(grant); err != nil {
			a.connectorMu.Unlock()
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"success":false,"error":"connector audit persistence failed"})
			return
		}
	}
	a.connectorMu.Unlock()
	a.handleConnectorDiagnostics(w, r)
}
