package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoggingInterceptor_ShouldIntercept(t *testing.T) {
	li := &LoggingInterceptor{
		Domains: []string{"api.anthropic.com", "example.com"},
	}

	assert.True(t, li.ShouldIntercept("api.anthropic.com"))
	assert.True(t, li.ShouldIntercept("API.ANTHROPIC.COM"))
	assert.True(t, li.ShouldIntercept("example.com"))
	assert.False(t, li.ShouldIntercept("other.com"))
	assert.False(t, li.ShouldIntercept("sub.api.anthropic.com"))
	assert.False(t, li.ShouldIntercept(""))
}

// interceptTestEnv bundles a CA, leaf cache, upstream TLS listener, and logging interceptor
// for reuse across MITM tests.
type interceptTestEnv struct {
	CACert    *x509.Certificate
	CAPool    *x509.CertPool
	LeafCache *LeafCache
	LogDir    string
}

func newInterceptTestEnv(t *testing.T) *interceptTestEnv {
	t.Helper()
	caDir := t.TempDir()
	logDir := filepath.Join(t.TempDir(), "logs")
	caCert, caKey, _, err := LoadOrCreateCA(caDir)
	require.NoError(t, err)

	caPool := x509.NewCertPool()
	caPool.AddCert(caCert)

	return &interceptTestEnv{
		CACert:    caCert,
		CAPool:    caPool,
		LeafCache: &LeafCache{CACert: caCert, CAKey: caKey},
		LogDir:    logDir,
	}
}

// startUpstreamTLS starts a mock TLS server that handles connections with handler.
// Returns the listener address.
func startUpstreamTLS(t *testing.T, env *interceptTestEnv, hostname string, handler func(net.Conn)) net.Listener {
	t.Helper()
	cert, err := env.LeafCache.Get(hostname)
	require.NoError(t, err)

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{*cert},
		MinVersion:   tls.VersionTLS12,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go handler(conn)
		}
	}()

	return ln
}

// runInterceptViaListener sets up a TCP listener that accepts one connection,
// runs PeekClientHello + Intercept, and sends the result on a channel.
// Returns the listener address for the client to connect to.
func runInterceptViaListener(t *testing.T, ctx context.Context, li *LoggingInterceptor, hostname string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			return
		}
		// Read the real ClientHello.
		peeked, _, peekErr := PeekClientHello(conn)
		if peekErr != nil {
			_ = conn.Close()
			return
		}
		// Run interceptor (it closes conn when done).
		_ = li.Intercept(ctx, conn, hostname, peeked)
	}()

	return ln.Addr().String()
}

func TestLoggingInterceptor_Intercept_nonStreaming(t *testing.T) {
	env := newInterceptTestEnv(t)
	hostname := "upstream.test"

	upstreamLn := startUpstreamTLS(t, env, hostname, handleEchoHTTPS)

	li := &LoggingInterceptor{
		Domains:   []string{hostname},
		LeafCache: env.LeafCache,
		Logger:    zerolog.Nop(),
		LogDir:    env.LogDir,
		Dialer: func(_ context.Context, _, _ string) (net.Conn, error) {
			return tls.Dial("tcp", upstreamLn.Addr().String(), &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // test only
			})
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	proxyAddr := runInterceptViaListener(t, ctx, li, hostname)
	time.Sleep(50 * time.Millisecond)

	// Connect as a real TLS client.
	conn, err := tls.Dial("tcp", proxyAddr, &tls.Config{
		ServerName: hostname,
		RootCAs:    env.CAPool,
	})
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// Send an HTTP request.
	reqBody := `{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"hello"}]}`
	req, err := http.NewRequest(http.MethodPost, "http://"+hostname+"/v1/messages", strings.NewReader(reqBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connection", "close")
	require.NoError(t, req.Write(conn))

	// Read response.
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, reqBody, string(respBody)) // echo server returns request body

	_ = conn.Close()
	time.Sleep(200 * time.Millisecond) // wait for log flush

	// Verify traffic log.
	entries, err := os.ReadDir(env.LogDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	logData, err := os.ReadFile(filepath.Join(env.LogDir, entries[0].Name()))
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(logData)), "\n")
	require.Len(t, lines, 2, "expected request + response log entries")

	var reqLog, respLog TrafficLog
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &reqLog))
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &respLog))

	assert.Equal(t, "request", reqLog.Direction)
	assert.Equal(t, "POST", reqLog.Method)
	assert.Equal(t, "/v1/messages", reqLog.Path)
	assert.Equal(t, reqBody, reqLog.Body)

	assert.Equal(t, "response", respLog.Direction)
	assert.Equal(t, 200, respLog.Status)
	assert.Equal(t, reqBody, respLog.Body) // echo
}

func TestLoggingInterceptor_Intercept_streaming(t *testing.T) {
	env := newInterceptTestEnv(t)
	hostname := "stream.test"

	sseBody := "event: content_block_delta\ndata: {\"type\":\"text\",\"text\":\"Hello\"}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	upstreamLn := startUpstreamTLS(t, env, hostname, func(conn net.Conn) {
		handleSSEResponse(conn, sseBody)
	})

	li := &LoggingInterceptor{
		Domains:   []string{hostname},
		LeafCache: env.LeafCache,
		Logger:    zerolog.Nop(),
		LogDir:    env.LogDir,
		Dialer: func(_ context.Context, _, _ string) (net.Conn, error) {
			return tls.Dial("tcp", upstreamLn.Addr().String(), &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // test only
			})
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	proxyAddr := runInterceptViaListener(t, ctx, li, hostname)
	time.Sleep(50 * time.Millisecond)

	conn, err := tls.Dial("tcp", proxyAddr, &tls.Config{
		ServerName: hostname,
		RootCAs:    env.CAPool,
	})
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	req, err := http.NewRequest(http.MethodPost, "http://"+hostname+"/v1/messages", strings.NewReader(`{"stream":true}`))
	require.NoError(t, err)
	req.Header.Set("Connection", "close")
	require.NoError(t, req.Write(conn))

	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	assert.Contains(t, string(respBody), "content_block_delta")
	assert.Contains(t, string(respBody), "message_stop")

	_ = conn.Close()
	time.Sleep(200 * time.Millisecond)

	// Verify the SSE body was logged.
	entries, err := os.ReadDir(env.LogDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	logData, err := os.ReadFile(filepath.Join(env.LogDir, entries[0].Name()))
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(logData)), "\n")
	require.Len(t, lines, 2)

	var respLog TrafficLog
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &respLog))
	assert.Equal(t, "response", respLog.Direction)
	assert.Contains(t, respLog.Body, "content_block_delta")
}

func TestLimitWriter(t *testing.T) {
	var buf bytes.Buffer
	lw := LimitWriter(&buf, 5)

	n, err := lw.Write([]byte("hello world"))
	assert.NoError(t, err)
	assert.Equal(t, 5, n) // only 5 bytes accepted
	assert.Equal(t, "hello", buf.String())

	// Further writes are discarded.
	n, err = lw.Write([]byte("more"))
	assert.NoError(t, err)
	assert.Equal(t, 4, n) // reports full len but discards
	assert.Equal(t, "hello", buf.String())
}

func TestReplayConn_Read(t *testing.T) {
	inner := &bytes.Buffer{}
	inner.WriteString("world")

	rc := &replayConn{
		reader: io.MultiReader(strings.NewReader("hello "), inner),
	}

	buf := make([]byte, 20)
	n, err := rc.Read(buf)
	assert.NoError(t, err)
	assert.Equal(t, "hello ", string(buf[:n]))

	n, err = rc.Read(buf)
	assert.NoError(t, err)
	assert.Equal(t, "world", string(buf[:n]))
}

// --- test helpers ---

// handleEchoHTTPS reads an HTTP request over TLS and echoes the body back.
func handleEchoHTTPS(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	req, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil {
		return
	}

	body, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	if err != nil {
		return
	}

	resp := &http.Response{
		StatusCode:    http.StatusOK,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": {"application/json"}},
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Close:         true,
	}
	_ = resp.Write(conn)
}

// handleSSEResponse reads a request and writes back an SSE response.
func handleSSEResponse(conn net.Conn, sseBody string) {
	defer func() { _ = conn.Close() }()

	req, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil {
		return
	}
	_ = req.Body.Close()

	header := fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nConnection: close\r\nContent-Length: %d\r\n\r\n", len(sseBody))
	_, _ = conn.Write([]byte(header))
	_, _ = conn.Write([]byte(sseBody))
}
