package chrome

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startTestChromeServer(t *testing.T, token string, spawner ProcessSpawner) (addr string, cancel context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	srv := &Server{
		Addr:    "127.0.0.1:0",
		Token:   token,
		Spawner: spawner,
		Logger:  zerolog.Nop(),
	}

	ln, err := net.Listen("tcp", srv.Addr)
	require.NoError(t, err)
	addr = ln.Addr().String()
	_ = ln.Close()

	srv.Addr = addr

	go func() {
		_ = srv.ListenAndServe(ctx)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Cleanup(func() { cancel() })
	return addr, cancel
}

func TestChromeServer_ProxySession(t *testing.T) {
	token := "chrome-test-token"
	stdinBuf := &bytes.Buffer{}
	blocker := &blockingReader{done: make(chan struct{})}

	spawner := &funcSpawner{
		fn: func(_ context.Context) (io.WriteCloser, io.ReadCloser, func() error, error) {
			return nopWriteCloser{stdinBuf}, nopReadCloser{blocker}, func() error { return nil }, nil
		},
	}
	addr, _ := startTestChromeServer(t, token, spawner)

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)

	// Send auth request.
	req := proxyRequest{Token: token}
	enc := json.NewEncoder(conn)
	require.NoError(t, enc.Encode(req))

	// Read ProxyAck.
	scanner := bufio.NewScanner(conn)
	require.True(t, scanner.Scan(), "expected proxy ack")

	var ack ProxyAck
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &ack))
	assert.True(t, ack.OK)
	assert.Empty(t, ack.Error)

	// Write data to the proxied connection.
	clientData := []byte("client-request")
	_, err = conn.Write(clientData)
	require.NoError(t, err)
	_ = conn.Close()

	time.Sleep(100 * time.Millisecond)
	close(blocker.done)

	assert.Equal(t, clientData, stdinBuf.Bytes())
}

func TestChromeServer_AuthRejection(t *testing.T) {
	token := "correct-token"
	blocker := &blockingReader{done: make(chan struct{})}
	defer close(blocker.done)

	spawner := &funcSpawner{
		fn: func(_ context.Context) (io.WriteCloser, io.ReadCloser, func() error, error) {
			return nopWriteCloser{&bytes.Buffer{}}, nopReadCloser{blocker}, func() error { return nil }, nil
		},
	}
	addr, _ := startTestChromeServer(t, token, spawner)

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	req := proxyRequest{Token: "wrong-token"}
	enc := json.NewEncoder(conn)
	require.NoError(t, enc.Encode(req))

	scanner := bufio.NewScanner(conn)
	require.True(t, scanner.Scan(), "expected proxy ack")

	var ack ProxyAck
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &ack))
	assert.False(t, ack.OK)
	assert.Contains(t, ack.Error, "authentication failed")
}

func TestChromeServer_InvalidJSON(t *testing.T) {
	token := "test-token"
	blocker := &blockingReader{done: make(chan struct{})}
	defer close(blocker.done)

	spawner := &funcSpawner{
		fn: func(_ context.Context) (io.WriteCloser, io.ReadCloser, func() error, error) {
			return nopWriteCloser{&bytes.Buffer{}}, nopReadCloser{blocker}, func() error { return nil }, nil
		},
	}
	addr, _ := startTestChromeServer(t, token, spawner)

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	_, err = conn.Write([]byte("not json\n"))
	require.NoError(t, err)

	scanner := bufio.NewScanner(conn)
	require.True(t, scanner.Scan())

	var ack ProxyAck
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &ack))
	assert.False(t, ack.OK)
	assert.Contains(t, ack.Error, "invalid request JSON")
}
