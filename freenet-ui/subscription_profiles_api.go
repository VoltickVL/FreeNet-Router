package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const defaultProfilesHelperPath = "/opt/lib/freenet/list_subscription_profiles.sh"

type subscriptionProfile struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Port    int    `json:"port"`
}

type subscriptionProfilesResponse struct {
	Success  bool                  `json:"success"`
	Count    int                   `json:"count"`
	Profiles []subscriptionProfile `json:"profiles"`
	Error    string                `json:"error,omitempty"`
}

func profilesHelperPath() string {
	if p := strings.TrimSpace(os.Getenv("FREENET_PROFILES_HELPER")); p != "" {
		return p
	}
	return defaultProfilesHelperPath
}

func (a *app) handleSubscriptionProfiles(w http.ResponseWriter, _ *http.Request) {
	if !subscriptionConfigured(a.cfg.SubPath) {
		writeJSON(w, http.StatusConflict, subscriptionProfilesResponse{Success: false, Error: "subscription is not configured"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	output, err := runProfilesHelper(ctx, profilesHelperPath(), a.cfg.SubPath)
	if ctx.Err() == context.DeadlineExceeded {
		writeJSON(w, http.StatusGatewayTimeout, subscriptionProfilesResponse{Success: false, Error: "profile discovery timed out"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, subscriptionProfilesResponse{Success: false, Error: "profile discovery failed"})
		return
	}

	profiles, err := parseSubscriptionProfiles(output)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, subscriptionProfilesResponse{Success: false, Error: "invalid sanitized profile response"})
		return
	}
	writeJSON(w, http.StatusOK, subscriptionProfilesResponse{Success: true, Count: len(profiles), Profiles: profiles})
}

func runProfilesHelper(ctx context.Context, helperPath, subPath string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, helperPath)
	cmd.Env = append(os.Environ(),
		"PATH=/opt/bin:/opt/sbin:/opt/usr/bin:/opt/usr/sbin:/bin:/sbin:/usr/bin:/usr/sbin",
		"FREENET_SUB_FILE="+subPath,
	)
	return cmd.Output()
}

func parseSubscriptionProfiles(data []byte) ([]subscriptionProfile, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 1024), 128*1024)
	profiles := make([]subscriptionProfile, 0, 16)
	seen := map[string]bool{}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var p subscriptionProfile
		dec := json.NewDecoder(strings.NewReader(line))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&p); err != nil {
			return nil, errors.New("invalid profile JSON")
		}
		if !validSubscriptionProfile(p) {
			return nil, errors.New("invalid profile fields")
		}
		if seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		profiles = append(profiles, p)
		if len(profiles) > 100 {
			return nil, errors.New("too many profiles")
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(profiles) == 0 {
		return nil, errors.New("no profiles")
	}
	return profiles, nil
}

func validSubscriptionProfile(p subscriptionProfile) bool {
	if len(p.ID) != 16 || len(p.Name) == 0 || len(p.Name) > 240 || len(p.Address) == 0 || len(p.Address) > 255 {
		return false
	}
	for _, r := range p.ID {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	if p.Port < 1 || p.Port > 65535 || strings.ContainsAny(p.Name, "\r\n\x00") || strings.ContainsAny(p.Address, "\r\n\x00 /?#@") {
		return false
	}
	if ip := net.ParseIP(p.Address); ip == nil {
		for _, label := range strings.Split(p.Address, ".") {
			if label == "" || len(label) > 63 {
				return false
			}
			for _, r := range label {
				if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-') {
					return false
				}
			}
		}
	}
	return true
}

func profileEndpoint(p subscriptionProfile) string {
	return net.JoinHostPort(p.Address, strconv.Itoa(p.Port))
}
