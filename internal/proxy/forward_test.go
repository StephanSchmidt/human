package proxy

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForward_peekedBytesReplayedAndBidirectional(t *testing.T) {
	// client ↔ proxy ↔ upstream
	clientConn, proxyClient := net.Pipe()
	proxyUpstream, upstreamConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()
	defer func() { _ = upstreamConn.Close() }()

	peeked := []byte("PEEKED")
	logger := zerolog.Nop()

	go Forward(context.Background(), proxyClient, proxyUpstream, peeked, logger)

	// upstream should receive peeked bytes first.
	buf := make([]byte, len(peeked))
	_, err := io.ReadFull(upstreamConn, buf)
	require.NoError(t, err)
	assert.Equal(t, "PEEKED", string(buf))

	// Client sends data, upstream receives it.
	_, err = clientConn.Write([]byte("HELLO"))
	require.NoError(t, err)

	buf = make([]byte, 5)
	_, err = io.ReadFull(upstreamConn, buf)
	require.NoError(t, err)
	assert.Equal(t, "HELLO", string(buf))

	// Upstream sends data, client receives it.
	_, err = upstreamConn.Write([]byte("WORLD"))
	require.NoError(t, err)

	buf = make([]byte, 5)
	_, err = io.ReadFull(clientConn, buf)
	require.NoError(t, err)
	assert.Equal(t, "WORLD", string(buf))

	// Close upstream to end forwarding.
	_ = upstreamConn.Close()
}

func TestForward_noPeekedBytes(t *testing.T) {
	clientConn, proxyClient := net.Pipe()
	proxyUpstream, upstreamConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()
	defer func() { _ = upstreamConn.Close() }()

	logger := zerolog.Nop()

	go Forward(context.Background(), proxyClient, proxyUpstream, nil, logger)

	// Client sends directly, upstream receives it.
	_, err := clientConn.Write([]byte("DIRECT"))
	require.NoError(t, err)

	buf := make([]byte, 6)
	_, err = io.ReadFull(upstreamConn, buf)
	require.NoError(t, err)
	assert.Equal(t, "DIRECT", string(buf))

	_ = upstreamConn.Close()
}

func TestForward_contextCancellation(t *testing.T) {
	clientConn, proxyClient := net.Pipe()
	proxyUpstream, upstreamConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()
	defer func() { _ = upstreamConn.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	logger := zerolog.Nop()

	done := make(chan struct{})
	go func() {
		Forward(ctx, proxyClient, proxyUpstream, nil, logger)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Forward did not return after context cancellation")
	}
}
