package daemon

import (
	"bufio"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureHandlerResponse calls the given handler function with one side
// of a net.Pipe and reads the JSON Response from the other side. This
// exercises the exact same write path the real daemon uses without
// standing up a TCP listener.
func captureHandlerResponse(t *testing.T, handle func(net.Conn)) Response {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = serverConn.Close() }()
		handle(serverConn)
	}()

	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(clientConn)
	line, err := reader.ReadBytes('\n')
	require.NoError(t, err)
	_ = clientConn.Close()
	<-done

	var resp Response
	require.NoError(t, json.Unmarshal(line, &resp))
	return resp
}

func TestServer_networkEventsRoute_empty(t *testing.T) {
	srv := &Server{Logger: zerolog.Nop()}

	resp := captureHandlerResponse(t, srv.handleNetworkEvents)

	assert.Equal(t, "[]\n", resp.Stdout)
	assert.Empty(t, resp.Stderr)
	assert.Equal(t, 0, resp.ExitCode)
}

func TestServer_networkEventsRoute_populated(t *testing.T) {
	store := NewNetworkEventStoreWithClock(func() time.Time {
		return time.Unix(1_700_000_000, 0).UTC()
	})
	store.Emit("proxy", "forward", "github.com")
	store.Emit("fail", "dial-fail", "broken.example.com")

	srv := &Server{Logger: zerolog.Nop(), NetworkEvents: store}

	resp := captureHandlerResponse(t, srv.handleNetworkEvents)
	require.NotEmpty(t, resp.Stdout)

	var events []NetworkEvent
	require.NoError(t, json.Unmarshal([]byte(resp.Stdout), &events))
	require.Len(t, events, 2)
	assert.Equal(t, "github.com", events[0].Host)
	assert.Equal(t, "forward", events[0].Status)
	assert.Equal(t, "broken.example.com", events[1].Host)
	assert.Equal(t, "dial-fail", events[1].Status)
}
