package daemon

import (
	"bufio"
	"encoding/json"
	"net"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startMockDaemon(t *testing.T, handler func(req Request) Response) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()

				scanner := bufio.NewScanner(conn)
				if !scanner.Scan() {
					return
				}
				var req Request
				if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
					return
				}

				resp := handler(req)
				enc := json.NewEncoder(conn)
				_ = enc.Encode(resp)
			}()
		}
	}()

	return ln.Addr().String()
}

func TestRunRemote_Success(t *testing.T) {
	addr := startMockDaemon(t, func(req Request) Response {
		assert.Equal(t, "test-token", req.Token)
		assert.Equal(t, []string{"echo", "hello"}, req.Args)
		return Response{
			Stdout:   "hello\n",
			ExitCode: 0,
		}
	})

	exitCode, err := RunRemote(addr, "test-token", []string{"echo", "hello"}, "dev")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
}

func TestRunRemote_NonZeroExit(t *testing.T) {
	addr := startMockDaemon(t, func(_ Request) Response {
		return Response{
			Stderr:   "error occurred\n",
			ExitCode: 1,
		}
	})

	exitCode, err := RunRemote(addr, "tok", []string{"fail"}, "dev")
	require.NoError(t, err)
	assert.Equal(t, 1, exitCode)
}

func TestGetNetworkEvents_Success(t *testing.T) {
	addr := startMockDaemon(t, func(req Request) Response {
		assert.Equal(t, []string{"network-events"}, req.Args)
		// Two-event payload mirrors the handleNetworkEvents wire format.
		data := `[{"source":"proxy","status":"forward","host":"github.com","count":3,"last_seen":"2024-01-01T00:00:00Z"},` +
			`{"source":"fail","status":"dial-fail","host":"broken.example.com","count":1,"last_seen":"2024-01-01T00:00:05Z"}]` + "\n"
		return Response{Stdout: data}
	})

	events, err := GetNetworkEvents(addr, "tok")
	require.NoError(t, err)
	require.Len(t, events, 2)
	assert.Equal(t, "github.com", events[0].Host)
	assert.Equal(t, 3, events[0].Count)
	assert.Equal(t, "broken.example.com", events[1].Host)
	assert.Equal(t, "dial-fail", events[1].Status)
}

func TestGetNetworkEvents_Empty(t *testing.T) {
	addr := startMockDaemon(t, func(_ Request) Response {
		return Response{Stdout: "[]\n"}
	})

	events, err := GetNetworkEvents(addr, "tok")
	require.NoError(t, err)
	assert.Empty(t, events)
}

func TestGetNetworkEvents_InvalidJSON(t *testing.T) {
	addr := startMockDaemon(t, func(_ Request) Response {
		return Response{Stdout: "not json\n"}
	})

	_, err := GetNetworkEvents(addr, "tok")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid network events JSON")
}

func TestRunRemote_ConnectionRefused(t *testing.T) {
	exitCode, err := RunRemote("127.0.0.1:1", "tok", []string{"echo"}, "dev")
	require.Error(t, err)
	assert.Equal(t, 1, exitCode)
	assert.Contains(t, err.Error(), "cannot reach daemon")
}

func TestRunRemote_VersionForwarded(t *testing.T) {
	addr := startMockDaemon(t, func(req Request) Response {
		assert.Equal(t, "1.2.3", req.Version)
		return Response{ExitCode: 0}
	})

	exitCode, err := RunRemote(addr, "tok", []string{}, "1.2.3")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
}

func TestRunRemote_EnvForwarded(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	addr := startMockDaemon(t, func(req Request) Response {
		assert.Equal(t, "1", req.Env["NO_COLOR"])
		return Response{ExitCode: 0}
	})

	exitCode, err := RunRemote(addr, "tok", []string{}, "dev")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
}

func TestRunRemote_ClientPIDForwarded(t *testing.T) {
	addr := startMockDaemon(t, func(req Request) Response {
		assert.Greater(t, req.ClientPID, 0, "ClientPID should be set to parent PID")
		return Response{ExitCode: 0}
	})

	exitCode, err := RunRemote(addr, "tok", []string{}, "dev")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
}

func TestRunRemote_DaemonClosesImmediately(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	exitCode, err := RunRemote(ln.Addr().String(), "tok", []string{}, "dev")
	require.Error(t, err)
	assert.Equal(t, 1, exitCode)
	// Depending on timing, the error may be a clean EOF or a connection reset.
	errMsg := err.Error()
	assert.True(t,
		strings.Contains(errMsg, "failed to read response") ||
			strings.Contains(errMsg, "failed to send request"),
		"unexpected error: %s", errMsg,
	)
}
