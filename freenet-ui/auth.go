package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	authCookieName         = "freenet_session"
	authAlgorithm          = "pbkdf2-sha256"
	authIterations         = 210000
	authSaltBytes          = 32
	authKeyBytes           = 32
	authSessionTTL         = 12 * time.Hour
	authRememberSessionTTL = 30 * 24 * time.Hour
	authCredentialMode     = 0600
	authSessionStoreMode   = 0600
)

var (
	errCredentialExists = errors.New("credential already configured")
	errInvalidCredential = errors.New("invalid credential store")
	authStateByApp       sync.Map
)

type credentialRecord struct {
	Version    int    `json:"version"`
	Algorithm  string `json:"algorithm"`
	Iterations int    `json:"iterations"`
	Salt       string `json:"salt"`
	Hash       string `json:"hash"`
}

type persistentSessionRecord struct {
	Hash      string    `json:"hash"`
	ExpiresAt time.Time `json:"expires_at"`
}

type persistentSessionStore struct {
	Version  int                       `json:"version"`
	Sessions []persistentSessionRecord `json:"sessions"`
}

type authState struct {
	mu       sync.Mutex
	sessions map[string]time.Time
}

type authStatusResponse struct {
	Configured    bool `json:"configured"`
	Authenticated bool `json:"authenticated"`
}

type passwordRequest struct {
	Password string `json:"password"`
	Remember bool   `json:"remember,omitempty"`
}

func (a *app) authState() *authState {
	if state, ok := authStateByApp.Load(a); ok {
		return state.(*authState)
	}
	state := &authState{sessions: make(map[string]time.Time)}
	actual, _ := authStateByApp.LoadOrStore(a, state)
	return actual.(*authState)
}

func (a *app) authPath() string {
	return filepath.Join(filepath.Dir(a.cfg.ConfigPath), "auth.json")
}

func (a *app) authSessionPath() string {
	return filepath.Join(filepath.Dir(a.cfg.ConfigPath), "auth_sessions.json")
}

func (a *app) loadCredential() (credentialRecord, bool, error) {
	var rec credentialRecord
	b, err := os.ReadFile(a.authPath())
	if os.IsNotExist(err) {
		return rec, false, nil
	}
	if err != nil {
		return rec, false, err
	}
	if err := json.Unmarshal(b, &rec); err != nil {
		return rec, true, errInvalidCredential
	}
	if rec.Version != 1 || rec.Algorithm != authAlgorithm || rec.Iterations < 100000 {
		return rec, true, errInvalidCredential
	}
	salt, err := hex.DecodeString(rec.Salt)
	if err != nil || len(salt) != authSaltBytes {
		return rec, true, errInvalidCredential
	}
	hash, err := hex.DecodeString(rec.Hash)
	if err != nil || len(hash) != authKeyBytes {
		return rec, true, errInvalidCredential
	}
	return rec, true, nil
}

func (a *app) credentialConfigured() bool {
	_, configured, err := a.loadCredential()
	return configured && err == nil
}

func validateAdminPassword(password string) error {
	if len(password) < 12 || len(password) > 256 {
		return errors.New("password must be 12-256 bytes")
	}
	return nil
}

func pbkdf2SHA256(password, salt []byte, iterations, keyLen int) []byte {
	blocks := (keyLen + sha256.Size - 1) / sha256.Size
	out := make([]byte, 0, blocks*sha256.Size)
	for block := 1; block <= blocks; block++ {
		mac := hmac.New(sha256.New, password)
		_, _ = mac.Write(salt)
		_, _ = mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iterations; i++ {
			mac = hmac.New(sha256.New, password)
			_, _ = mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}

func (a *app) createCredential(password string) error {
	if err := validateAdminPassword(password); err != nil {
		return err
	}
	state := a.authState()
	state.mu.Lock()
	defer state.mu.Unlock()

	if _, configured, err := a.loadCredential(); err != nil {
		return err
	} else if configured {
		return errCredentialExists
	}

	salt := make([]byte, authSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	hash := pbkdf2SHA256([]byte(password), salt, authIterations, authKeyBytes)
	rec := credentialRecord{
		Version:    1,
		Algorithm:  authAlgorithm,
		Iterations: authIterations,
		Salt:       hex.EncodeToString(salt),
		Hash:       hex.EncodeToString(hash),
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.authPath()), 0700); err != nil {
		return err
	}
	if err := atomicWrite(a.authPath(), append(b, '\n'), authCredentialMode); err != nil {
		return err
	}
	return os.Chmod(a.authPath(), authCredentialMode)
}

func (a *app) verifyPassword(password string) bool {
	rec, configured, err := a.loadCredential()
	if err != nil || !configured {
		return false
	}
	salt, _ := hex.DecodeString(rec.Salt)
	expected, _ := hex.DecodeString(rec.Hash)
	actual := pbkdf2SHA256([]byte(password), salt, rec.Iterations, len(expected))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func randomSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func sessionDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func requestSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func (a *app) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, remember bool) {
	ttl := authSessionTTL
	if remember {
		ttl = authRememberSessionTTL
	}
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestSecure(r),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(ttl.Seconds()),
		Expires:  time.Now().Add(ttl),
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   requestSecure(r),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
	})
}

func (a *app) loadPersistentSessionsLocked(now time.Time) ([]persistentSessionRecord, error) {
	b, err := os.ReadFile(a.authSessionPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var store persistentSessionStore
	if err := json.Unmarshal(b, &store); err != nil || store.Version != 1 {
		return nil, errInvalidCredential
	}
	clean := make([]persistentSessionRecord, 0, len(store.Sessions))
	for _, rec := range store.Sessions {
		if len(rec.Hash) == sha256.Size*2 && rec.ExpiresAt.After(now) {
			clean = append(clean, rec)
		}
	}
	return clean, nil
}

func (a *app) savePersistentSessionsLocked(records []persistentSessionRecord) error {
	if len(records) == 0 {
		if err := os.Remove(a.authSessionPath()); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	store := persistentSessionStore{Version: 1, Sessions: records}
	b, err := json.Marshal(store)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.authSessionPath()), 0700); err != nil {
		return err
	}
	if err := atomicWrite(a.authSessionPath(), append(b, '\n'), authSessionStoreMode); err != nil {
		return err
	}
	return os.Chmod(a.authSessionPath(), authSessionStoreMode)
}

func (a *app) rememberSessionLocked(token string, expires time.Time) error {
	now := time.Now()
	records, err := a.loadPersistentSessionsLocked(now)
	if err != nil && !errors.Is(err, errInvalidCredential) {
		return err
	}
	digest := sessionDigest(token)
	out := make([]persistentSessionRecord, 0, len(records)+1)
	for _, rec := range records {
		if rec.Hash != digest {
			out = append(out, rec)
		}
	}
	out = append(out, persistentSessionRecord{Hash: digest, ExpiresAt: expires})
	return a.savePersistentSessionsLocked(out)
}

func (a *app) forgetPersistentSessionLocked(token string) {
	records, err := a.loadPersistentSessionsLocked(time.Now())
	if err != nil {
		return
	}
	digest := sessionDigest(token)
	out := records[:0]
	for _, rec := range records {
		if rec.Hash != digest {
			out = append(out, rec)
		}
	}
	_ = a.savePersistentSessionsLocked(out)
}

func (a *app) newSession(w http.ResponseWriter, r *http.Request) error {
	return a.newSessionWithRemember(w, r, false)
}

func (a *app) newSessionWithRemember(w http.ResponseWriter, r *http.Request, remember bool) error {
	token, err := randomSessionToken()
	if err != nil {
		return err
	}
	state := a.authState()
	state.mu.Lock()
	now := time.Now()
	for existing, expires := range state.sessions {
		if !expires.After(now) {
			delete(state.sessions, existing)
		}
	}
	ttl := authSessionTTL
	if remember {
		ttl = authRememberSessionTTL
	}
	expires := now.Add(ttl)
	state.sessions[token] = expires
	if remember {
		if err := a.rememberSessionLocked(token, expires); err != nil {
			delete(state.sessions, token)
			state.mu.Unlock()
			return err
		}
	}
	state.mu.Unlock()
	a.setSessionCookie(w, r, token, remember)
	return nil
}

func (a *app) sessionToken(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(authCookieName)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	state := a.authState()
	state.mu.Lock()
	defer state.mu.Unlock()
	now := time.Now()
	if expires, ok := state.sessions[cookie.Value]; ok {
		if expires.After(now) {
			return cookie.Value, true
		}
		delete(state.sessions, cookie.Value)
	}

	records, err := a.loadPersistentSessionsLocked(now)
	if err != nil {
		return "", false
	}
	digest := sessionDigest(cookie.Value)
	found := false
	var expires time.Time
	for _, rec := range records {
		if subtle.ConstantTimeCompare([]byte(rec.Hash), []byte(digest)) == 1 {
			found = true
			expires = rec.ExpiresAt
			break
		}
	}
	if !found || !expires.After(now) {
		return "", false
	}
	state.sessions[cookie.Value] = expires
	return cookie.Value, true
}

func (a *app) isAuthenticated(r *http.Request) bool {
	_, ok := a.sessionToken(r)
	return ok
}

func (a *app) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.isAuthenticated(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}
		next(w, r)
	}
}

func decodePasswordRequest(w http.ResponseWriter, r *http.Request) (passwordRequest, bool) {
	var req passwordRequest
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "request rejected"})
		return req, false
	}
	if ct := strings.ToLower(r.Header.Get("Content-Type")); !strings.HasPrefix(ct, "application/json") {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "application/json required"})
		return req, false
	}
	body := http.MaxBytesReader(w, r.Body, 4096)
	defer body.Close()
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return req, false
	}
	return req, true
}

func (a *app) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, authStatusResponse{
		Configured:    a.credentialConfigured(),
		Authenticated: a.isAuthenticated(r),
	})
}

func (a *app) handleAuthSetup(w http.ResponseWriter, r *http.Request) {
	req, ok := decodePasswordRequest(w, r)
	if !ok {
		return
	}
	if a.credentialConfigured() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "authentication already configured"})
		return
	}
	if err := a.createCredential(req.Password); err != nil {
		if errors.Is(err, errCredentialExists) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "authentication already configured"})
			return
		}
		if err.Error() == "password must be 12-256 bytes" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password does not meet policy"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot configure authentication"})
		return
	}
	if err := a.newSessionWithRemember(w, r, req.Remember); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot create session"})
		return
	}
	writeJSON(w, http.StatusCreated, authStatusResponse{Configured: true, Authenticated: true})
}

func (a *app) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	req, ok := decodePasswordRequest(w, r)
	if !ok {
		return
	}
	if !a.credentialConfigured() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "authentication setup required"})
		return
	}
	if !a.verifyPassword(req.Password) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	if err := a.newSessionWithRemember(w, r, req.Remember); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot create session"})
		return
	}
	writeJSON(w, http.StatusOK, authStatusResponse{Configured: true, Authenticated: true})
}

func (a *app) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "request rejected"})
		return
	}
	if token, ok := a.sessionToken(r); ok {
		state := a.authState()
		state.mu.Lock()
		delete(state.sessions, token)
		a.forgetPersistentSessionLocked(token)
		state.mu.Unlock()
	}
	clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, authStatusResponse{Configured: a.credentialConfigured(), Authenticated: false})
}

func (a *app) handleAuthLogoutAll(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "request rejected"})
		return
	}
	state := a.authState()
	state.mu.Lock()
	state.sessions = make(map[string]time.Time)
	_ = a.savePersistentSessionsLocked(nil)
	state.mu.Unlock()
	clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, authStatusResponse{Configured: a.credentialConfigured(), Authenticated: false})
}
