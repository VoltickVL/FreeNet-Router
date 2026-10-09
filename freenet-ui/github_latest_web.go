package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GitHub explicitly documents /releases/latest as the public canonical link
// to the latest *stable* published release. Unlike api.github.com, this path
// does not consume the unauthenticated REST API's per-IP request allowance.
// Never scrape HTML, trust arbitrary redirects, or infer tags from page text.
var githubReleaseWebLatestURL = "https://github.com/VoltickVL/FreeNet-Router/releases/latest"

const githubReleaseTagPrefix = "/VoltickVL/FreeNet-Router/releases/tag/"

func githubStableTagFromLocation(location string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(location))
	if err != nil {
		return "", errors.New("invalid GitHub latest redirect")
	}
	// GitHub may use an absolute or an origin-relative redirect. A third-party
	// host, credentials, a query, a fragment, or encoded path delimiters are
	// never accepted as a FreeNet release authority.
	if u.IsAbs() {
		if u.Scheme != "https" || u.Host != "github.com" || u.User != nil {
			return "", errors.New("untrusted GitHub latest redirect")
		}
	} else if u.Host != "" || u.Scheme != "" || u.User != nil {
		return "", errors.New("untrusted GitHub latest redirect")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.EscapedPath() != u.Path ||
		!strings.HasPrefix(u.Path, githubReleaseTagPrefix) {
		return "", errors.New("invalid GitHub latest release path")
	}
	tag := strings.TrimPrefix(u.Path, githubReleaseTagPrefix)
	if !validReleaseTag(tag) {
		return "", errors.New("GitHub latest redirect does not contain a valid stable FreeNet tag")
	}
	return tag, nil
}

func githubLatestStableWebTag(ctx context.Context) (string, error) {
	var lastErr error
	for _, dnsServer := range []string{"", "77.88.8.8:53", "8.8.8.8:53"} {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, githubReleaseWebLatestURL, nil)
		if err != nil {
			return "", errors.New("invalid canonical GitHub latest URL")
		}
		req.Header.Set("User-Agent", "FreeNet-Control-Center/"+version)
		client := recoveryHTTPClient(dnsServer)
		client.Timeout = 12 * time.Second
		// Verify the Location without ever following it to a third-party URL.
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("GitHub latest link unavailable: %w", err)
			continue
		}
		resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
			http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
			// A malformed or unexpected redirect is a trust failure, not an
			// invitation to try a different DNS resolver.
			return githubStableTagFromLocation(resp.Header.Get("Location"))
		case http.StatusForbidden, http.StatusTooManyRequests:
			return "", fmt.Errorf("GitHub releases web returned HTTP %d", resp.StatusCode)
		default:
			lastErr = fmt.Errorf("GitHub releases web returned HTTP %d without a canonical latest redirect", resp.StatusCode)
		}
	}
	if lastErr == nil {
		lastErr = errors.New("GitHub latest release redirect unavailable")
	}
	return "", lastErr
}
