package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/StephanSchmidt/human/internal/browser"
	"github.com/StephanSchmidt/human/internal/oauth"
)

const oauthAcceptTimeout = 5 * time.Minute

// BrowserOpener opens a URL in the browser. Extracted for testability.
type BrowserOpener interface {
	Open(url string) error
}

// isBrowserWithRedirect checks if args represent a "browser <url>" command
// where the URL contains an OAuth redirect_uri targeting localhost.
func isBrowserWithRedirect(args []string) (*oauth.RedirectInfo, string) {
	// Find "browser" subcommand followed by a URL argument.
	for i, arg := range args {
		if arg == "browser" && i+1 < len(args) {
			url := args[i+1]
			if err := browser.ValidateURL(url); err != nil {
				return nil, ""
			}
			info := oauth.DetectRedirect(url)
			if info != nil {
				return info, url
			}
			return nil, ""
		}
	}
	return nil, ""
}

// handleOAuthRelay intercepts a browser command with an OAuth redirect.
// It binds the original redirect_uri port on the host, opens the browser
// with the unmodified URL, and uses a two-line protocol to relay the
// callback URL back to the client. The client (running inside the
// container) delivers the callback to Claude Code's localhost listener.
func (s *Server) handleOAuthRelay(conn net.Conn, _ *bufio.Reader, info *oauth.RedirectInfo, originalURL string, opener BrowserOpener) {
	// Bind the ORIGINAL redirect_uri port on the host so the browser's
	// OAuth callback lands here. We must NOT rewrite the redirect_uri —
	// the OAuth provider binds the auth code to the exact redirect_uri
	// from the authorization request, and Claude Code will send that
	// same URI when exchanging the code for a token.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", info.Port))
	if err != nil {
		s.writeError(conn, fmt.Sprintf("cannot listen on port %d for OAuth callback: %s", info.Port, err), 1)
		return
	}
	defer func() { _ = ln.Close() }()

	s.Logger.Debug().Int("port", info.Port).Msg("OAuth relay: listening on original redirect port")

	// Open the browser with the ORIGINAL URL (no redirect_uri rewrite).
	if err := opener.Open(originalURL); err != nil {
		s.writeError(conn, fmt.Sprintf("failed to open browser: %s", err), 1)
		return
	}

	s.Logger.Debug().Str("url", originalURL).Msg("browser opened with original URL")

	// Line 1: tell the client to print stdout and keep waiting.
	enc := json.NewEncoder(conn)
	resp1 := Response{
		Stdout:        fmt.Sprintf("Opened %s\n", originalURL),
		AwaitCallback: true,
	}
	if err := enc.Encode(resp1); err != nil {
		s.Logger.Warn().Err(err).Msg("failed to write OAuth line 1")
		return
	}

	// Wait for the browser callback on the original redirect port.
	cbURL, ok := s.awaitCallback(ln, info)
	if !ok {
		return
	}

	// Line 2: send the callback URL to the client for local delivery.
	resp2 := Response{Callback: cbURL}
	if err := enc.Encode(resp2); err != nil {
		s.Logger.Warn().Err(err).Msg("failed to write OAuth line 2")
		return
	}

	s.Logger.Debug().Str("callback", cbURL).Msg("OAuth callback URL sent to client")
}

// awaitCallback accepts the OAuth callback on the listener and returns
// the callback URL. Returns ("", false) on timeout or error.
func (s *Server) awaitCallback(ln net.Listener, info *oauth.RedirectInfo) (string, bool) {
	callbackURL := make(chan string, 1)

	mux := http.NewServeMux()
	mux.HandleFunc(info.Path, func(w http.ResponseWriter, r *http.Request) {
		u := fmt.Sprintf("http://localhost:%d%s?%s", info.Port, r.URL.Path, r.URL.RawQuery)
		s.Logger.Debug().Str("url", u).Msg("OAuth callback received")
		callbackURL <- u

		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "<html><body><h1>Authorization successful</h1><p>You can close this tab.</p></body></html>")
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: oauthAcceptTimeout} //nolint:gosec // short-lived local server for OAuth callback
	go func() {
		if serveErr := srv.Serve(ln); serveErr != nil && serveErr != http.ErrServerClosed {
			s.Logger.Warn().Err(serveErr).Msg("OAuth callback server error")
		}
	}()
	defer func() { _ = srv.Close() }()

	select {
	case u := <-callbackURL:
		return u, true
	case <-time.After(oauthAcceptTimeout):
		s.Logger.Warn().Msg("OAuth callback timeout")
		return "", false
	}
}
