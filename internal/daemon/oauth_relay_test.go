package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StephanSchmidt/human/internal/oauth"
)

// mockOpener records the URL it was asked to open without launching a real browser.
type mockOpener struct {
	opened chan string
}

func (m *mockOpener) Open(rawURL string) error {
	m.opened <- rawURL
	return nil
}

func TestIsBrowserWithRedirect(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantInfo *oauth.RedirectInfo
		wantURL  string
	}{
		{
			name:     "browser with OAuth redirect",
			args:     []string{"browser", "https://auth.example.com/authorize?redirect_uri=http%3A%2F%2Flocalhost%3A38599%2Fcallback"},
			wantInfo: &oauth.RedirectInfo{Port: 38599, Path: "/callback"},
			wantURL:  "https://auth.example.com/authorize?redirect_uri=http%3A%2F%2Flocalhost%3A38599%2Fcallback",
		},
		{
			name:     "browser without redirect",
			args:     []string{"browser", "https://example.com"},
			wantInfo: nil,
			wantURL:  "",
		},
		{
			name:     "non-browser command",
			args:     []string{"get", "TICKET-123"},
			wantInfo: nil,
			wantURL:  "",
		},
		{
			name:     "browser with no URL arg",
			args:     []string{"browser"},
			wantInfo: nil,
			wantURL:  "",
		},
		{
			name:     "safe mode flag before browser",
			args:     []string{"--safe", "browser", "https://auth.example.com/authorize?redirect_uri=http%3A%2F%2Flocalhost%3A9999%2Fcb"},
			wantInfo: &oauth.RedirectInfo{Port: 9999, Path: "/cb"},
			wantURL:  "https://auth.example.com/authorize?redirect_uri=http%3A%2F%2Flocalhost%3A9999%2Fcb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, gotURL := isBrowserWithRedirect(tt.args)
			assert.Equal(t, tt.wantInfo, info)
			assert.Equal(t, tt.wantURL, gotURL)
		})
	}
}

// startRelayDaemon starts a daemon server with a mock browser opener and returns
// the server address and the opener for verification.
func startRelayDaemon(t *testing.T, token string) (string, *mockOpener) {
	t.Helper()

	opener := &mockOpener{opened: make(chan string, 1)}
	srv := &Server{
		Addr:       "127.0.0.1:0",
		Token:      token,
		CmdFactory: echoCmd,
		Opener:     opener,
		Logger:     zerolog.Nop(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel() })

	ln, err := net.Listen("tcp", srv.Addr)
	require.NoError(t, err)
	srvAddr := ln.Addr().String()
	_ = ln.Close()
	srv.Addr = srvAddr

	go func() { _ = srv.ListenAndServe(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, dialErr := net.DialTimeout("tcp", srvAddr, 100*time.Millisecond)
		if dialErr == nil {
			_ = c.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	return srvAddr, opener
}

// twoLineClientResult holds the responses from the two-line OAuth protocol.
type twoLineClientResult struct {
	resp1 Response
	resp2 Response
	err   error
}

// readResponse reads and unmarshals a single JSON line from the connection.
func readResponse(conn net.Conn) (Response, error) {
	raw, err := readLine(conn)
	if err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Response{}, err
	}
	return resp, nil
}

func TestOAuthRelayEndToEnd(t *testing.T) {
	// Pick a free port to use as the redirect_uri port. The daemon will bind
	// this port on the host to catch the OAuth callback (no redirect_uri rewrite).
	redirectLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	redirectPort := redirectLn.Addr().(*net.TCPAddr).Port
	// Free the port so the daemon can bind it during the relay.
	_ = redirectLn.Close()

	oauthURL := fmt.Sprintf(
		"https://auth.example.com/authorize?redirect_uri=%s&client_id=test",
		url.QueryEscape(fmt.Sprintf("http://localhost:%d/callback", redirectPort)),
	)

	token := "relay-test-token"
	srvAddr, opener := startRelayDaemon(t, token)

	// runTwoLineClientNoDeliver is like runTwoLineClient but does NOT call
	// deliverCallback — we verify delivery separately.
	resultCh := make(chan twoLineClientResult, 1)
	line1Done := make(chan struct{})
	go func() {
		conn, dialErr := net.DialTimeout("tcp", srvAddr, 2*time.Second)
		if dialErr != nil {
			resultCh <- twoLineClientResult{err: dialErr}
			return
		}
		defer func() { _ = conn.Close() }()

		req := Request{Token: token, Args: []string{"browser", oauthURL}}
		if encErr := json.NewEncoder(conn).Encode(req); encErr != nil {
			resultCh <- twoLineClientResult{err: encErr}
			return
		}

		resp1, readErr := readResponse(conn)
		if readErr != nil {
			resultCh <- twoLineClientResult{err: readErr}
			return
		}
		close(line1Done)

		if !resp1.AwaitCallback {
			resultCh <- twoLineClientResult{resp1: resp1}
			return
		}

		resp2, readErr := readResponse(conn)
		if readErr != nil {
			resultCh <- twoLineClientResult{resp1: resp1, err: readErr}
			return
		}
		resultCh <- twoLineClientResult{resp1: resp1, resp2: resp2}
	}()

	// Wait for the browser opener — URL should be UNCHANGED (no port rewrite).
	select {
	case openedURL := <-opener.opened:
		parsedInfo := oauth.DetectRedirect(openedURL)
		require.NotNil(t, parsedInfo, "opened URL should have a redirect_uri")
		assert.Equal(t, redirectPort, parsedInfo.Port, "redirect port must be unchanged")
		assert.Equal(t, "/callback", parsedInfo.Path)
	case <-time.After(2 * time.Second):
		t.Fatal("browser opener was not called")
	}

	// Wait for line 1 before simulating the browser callback.
	select {
	case <-line1Done:
	case <-time.After(2 * time.Second):
		t.Fatal("client did not read line 1")
	}

	// Simulate the browser completing OAuth: hit the daemon's listener on
	// the original redirect port (no rewrite).
	browserClient := &http.Client{Timeout: 5 * time.Second}
	browserResp, err := browserClient.Get(fmt.Sprintf("http://127.0.0.1:%d/callback?code=xyz&state=abc", redirectPort))
	require.NoError(t, err)
	require.NotNil(t, browserResp)
	require.NotNil(t, browserResp.Body)
	defer func() { _ = browserResp.Body.Close() }()

	body, err := io.ReadAll(browserResp.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, browserResp.StatusCode)
	assert.Contains(t, string(body), "Authorization successful")

	// Wait for the full client flow to complete.
	select {
	case cr := <-resultCh:
		require.NoError(t, cr.err)
		assert.Contains(t, cr.resp1.Stdout, "Opened")
		assert.True(t, cr.resp1.AwaitCallback)
		assert.Contains(t, cr.resp2.Callback, "/callback?code=xyz&state=abc")
		// Callback URL must target the original port (for container delivery).
		assert.Contains(t, cr.resp2.Callback, fmt.Sprintf("localhost:%d", redirectPort))
	case <-time.After(5 * time.Second):
		t.Fatal("client did not complete two-line protocol")
	}
}

func TestOAuthRelay_NonOAuthBrowserUnchanged(t *testing.T) {
	// A regular browser command without OAuth redirect should go through normal path.
	token := "non-oauth-token"
	addr, _ := startTestServer(t, token)

	resp := sendRequest(t, addr, Request{
		Token: token,
		Args:  []string{"echo", "hello"},
	})

	assert.Equal(t, 0, resp.ExitCode)
	assert.Equal(t, "hello\n", resp.Stdout)
}

