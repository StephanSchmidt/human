package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func echoCmd() *cobra.Command {
	root := &cobra.Command{
		Use:          "test",
		SilenceUsage: true,
	}
	root.AddCommand(&cobra.Command{
		Use: "echo",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Join(args, " "))
			return nil
		},
	})
	root.AddCommand(&cobra.Command{
		Use: "fail",
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("intentional error")
		},
	})
	return root
}

func startTestServerWithOpts(t *testing.T, token string, safeMode bool) (addr string, cancel context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	srv := &Server{
		Addr:       "127.0.0.1:0",
		Token:      token,
		SafeMode:   safeMode,
		CmdFactory: echoCmd,
		Logger:     zerolog.Nop(),
	}

	ln, err := net.Listen("tcp", srv.Addr)
	require.NoError(t, err)
	addr = ln.Addr().String()
	_ = ln.Close()

	srv.Addr = addr

	go func() {
		_ = srv.ListenAndServe(ctx)
	}()

	// Wait for server to be ready.
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

func startTestServer(t *testing.T, token string) (addr string, cancel context.CancelFunc) {
	t.Helper()
	return startTestServerWithOpts(t, token, false)
}

func sendRequest(t *testing.T, addr string, req Request) Response {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	enc := json.NewEncoder(conn)
	require.NoError(t, enc.Encode(req))

	scanner := bufio.NewScanner(conn)
	require.True(t, scanner.Scan(), "expected response line")

	var resp Response
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &resp))
	return resp
}

func TestServer_EchoCommand(t *testing.T) {
	token := "test-token-1234"
	addr, _ := startTestServer(t, token)

	resp := sendRequest(t, addr, Request{
		Token: token,
		Args:  []string{"echo", "hello", "world"},
	})

	assert.Equal(t, 0, resp.ExitCode)
	assert.Equal(t, "hello world\n", resp.Stdout)
	assert.Empty(t, resp.Stderr)
}

func TestServer_FailCommand(t *testing.T) {
	token := "test-token-1234"
	addr, _ := startTestServer(t, token)

	resp := sendRequest(t, addr, Request{
		Token: token,
		Args:  []string{"fail"},
	})

	assert.Equal(t, 1, resp.ExitCode)
}

func TestServer_AuthRejection(t *testing.T) {
	token := "correct-token"
	addr, _ := startTestServer(t, token)

	resp := sendRequest(t, addr, Request{
		Token: "wrong-token",
		Args:  []string{"echo", "should-not-appear"},
	})

	assert.Equal(t, 1, resp.ExitCode)
	assert.Contains(t, resp.Stderr, "authentication failed")
	assert.Empty(t, resp.Stdout)
}

func TestServer_ConcurrentRequests(t *testing.T) {
	token := "test-token-concurrent"
	addr, _ := startTestServer(t, token)

	var wg sync.WaitGroup
	results := make([]Response, 10)

	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = sendRequest(t, addr, Request{
				Token: token,
				Args:  []string{"echo", fmt.Sprintf("msg-%d", idx)},
			})
		}(i)
	}

	wg.Wait()

	for i, resp := range results {
		assert.Equal(t, 0, resp.ExitCode, "request %d failed", i)
		assert.Contains(t, resp.Stdout, fmt.Sprintf("msg-%d", i))
	}
}

func TestServer_InvalidJSON(t *testing.T) {
	token := "test-token"
	addr, _ := startTestServer(t, token)

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	_, err = conn.Write([]byte("not json\n"))
	require.NoError(t, err)

	scanner := bufio.NewScanner(conn)
	require.True(t, scanner.Scan())

	var resp Response
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &resp))
	assert.Equal(t, 1, resp.ExitCode)
	assert.Contains(t, resp.Stderr, "invalid request JSON")
}

func safeCmdFactory() *cobra.Command {
	root := &cobra.Command{
		Use:          "test",
		SilenceUsage: true,
	}
	root.PersistentFlags().Bool("safe", false, "safe mode")
	root.AddCommand(&cobra.Command{
		Use: "check",
		RunE: func(cmd *cobra.Command, _ []string) error {
			safe, _ := cmd.Root().PersistentFlags().GetBool("safe")
			if safe {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "safe-mode-active")
			} else {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "safe-mode-inactive")
			}
			return nil
		},
	})
	return root
}

func TestServer_SafeModePrependsFlag(t *testing.T) {
	token := "test-token-safe"

	ctx, cancel := context.WithCancel(context.Background())
	srv := &Server{
		Addr:       "127.0.0.1:0",
		Token:      token,
		SafeMode:   true,
		CmdFactory: safeCmdFactory,
		Logger:     zerolog.Nop(),
	}

	ln, err := net.Listen("tcp", srv.Addr)
	require.NoError(t, err)
	addr := ln.Addr().String()
	_ = ln.Close()
	srv.Addr = addr

	go func() { _ = srv.ListenAndServe(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Cleanup(func() { cancel() })

	resp := sendRequest(t, addr, Request{
		Token: token,
		Args:  []string{"check"},
	})

	assert.Equal(t, 0, resp.ExitCode)
	assert.Contains(t, resp.Stdout, "safe-mode-active")
}

func TestServer_SafeModeDisabled(t *testing.T) {
	token := "test-token-nosafe"

	ctx, cancel := context.WithCancel(context.Background())
	srv := &Server{
		Addr:       "127.0.0.1:0",
		Token:      token,
		SafeMode:   false,
		CmdFactory: safeCmdFactory,
		Logger:     zerolog.Nop(),
	}

	ln, err := net.Listen("tcp", srv.Addr)
	require.NoError(t, err)
	addr := ln.Addr().String()
	_ = ln.Close()
	srv.Addr = addr

	go func() { _ = srv.ListenAndServe(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Cleanup(func() { cancel() })

	resp := sendRequest(t, addr, Request{
		Token: token,
		Args:  []string{"check"},
	})

	assert.Equal(t, 0, resp.ExitCode)
	assert.Contains(t, resp.Stdout, "safe-mode-inactive")
}
