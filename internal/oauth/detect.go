package oauth

import (
	"fmt"
	"net/url"
	"strconv"
)

// RedirectInfo contains the parsed localhost redirect target from an OAuth URL.
type RedirectInfo struct {
	Port  int    // e.g. 38599
	Path  string // e.g. "/callback"
	State string // expected state parameter; empty when the request had none
}

// DetectRedirect parses rawURL looking for a redirect_uri query parameter
// that targets localhost. Returns nil if no such redirect is found.
func DetectRedirect(rawURL string) *RedirectInfo {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}

	redirectURI := u.Query().Get("redirect_uri")
	if redirectURI == "" {
		return nil
	}

	r, err := url.Parse(redirectURI)
	if err != nil {
		return nil
	}

	if r.Hostname() != "localhost" && r.Hostname() != "127.0.0.1" {
		return nil
	}

	portStr := r.Port()
	if portStr == "" {
		return nil
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		return nil
	}

	path := r.Path
	if path == "" {
		path = "/"
	}

	return &RedirectInfo{
		Port:  port,
		Path:  path,
		State: u.Query().Get("state"),
	}
}

// RewriteRedirectPort returns a copy of rawURL with the redirect_uri's port
// replaced by newPort. If the URL cannot be parsed or has no redirect_uri,
// the original URL is returned unchanged.
func RewriteRedirectPort(rawURL string, newPort int) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	q := u.Query()
	redirectURI := q.Get("redirect_uri")
	if redirectURI == "" {
		return rawURL
	}

	r, err := url.Parse(redirectURI)
	if err != nil {
		return rawURL
	}

	r.Host = fmt.Sprintf("%s:%d", r.Hostname(), newPort)
	q.Set("redirect_uri", r.String())
	u.RawQuery = q.Encode()

	return u.String()
}
