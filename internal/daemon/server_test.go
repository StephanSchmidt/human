package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
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
			if !safe {
				safe = os.Getenv("HUMAN_SAFE_MODE") == "1"
			}
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

func TestServer_SafeModeSetsEnvVar(t *testing.T) {
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

func envCmdFactory() *cobra.Command {
	root := &cobra.Command{
		Use:          "test",
		SilenceUsage: true,
	}
	root.AddCommand(&cobra.Command{
		Use: "env",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if v, ok := os.LookupEnv("NO_COLOR"); ok {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "NO_COLOR=%s\n", v)
			} else {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "NO_COLOR=<unset>")
			}
			return nil
		},
	})
	return root
}

func TestServer_EnvApplied(t *testing.T) {
	t.Setenv("NO_COLOR", "original")

	token := "test-token-env"
	ctx, cancel := context.WithCancel(context.Background())
	srv := &Server{
		Addr:       "127.0.0.1:0",
		Token:      token,
		CmdFactory: envCmdFactory,
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
		Args:  []string{"env"},
		Env:   map[string]string{"NO_COLOR": "from-client"},
	})

	assert.Equal(t, 0, resp.ExitCode)
	assert.Contains(t, resp.Stdout, "NO_COLOR=from-client")

	// Verify the original value is restored after the request.
	assert.Equal(t, "original", os.Getenv("NO_COLOR"))
}

func TestServer_TracksClientPID(t *testing.T) {
	token := "test-token"
	tracker := NewConnectedTracker()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := &Server{
		Addr:          "127.0.0.1:0",
		Token:         token,
		CmdFactory:    echoCmd,
		Logger:        zerolog.Nop(),
		ConnectedPIDs: tracker,
	}

	ln, err := net.Listen("tcp", srv.Addr)
	require.NoError(t, err)
	addr := ln.Addr().String()
	_ = ln.Close()
	srv.Addr = addr

	go func() { _ = srv.ListenAndServe(ctx) }()
	time.Sleep(50 * time.Millisecond)

	assert.Empty(t, tracker.PIDs())

	resp := sendRequest(t, addr, Request{
		Token:     token,
		Args:      []string{"echo", "hi"},
		ClientPID: 12345,
	})
	assert.Equal(t, 0, resp.ExitCode)
	assert.Equal(t, []int{12345}, tracker.PIDs())

	// Second request with different PID.
	resp = sendRequest(t, addr, Request{
		Token:     token,
		Args:      []string{"echo", "hi"},
		ClientPID: 67890,
	})
	assert.Equal(t, 0, resp.ExitCode)
	assert.Equal(t, []int{12345, 67890}, tracker.PIDs())
}

func TestServer_IgnoresZeroClientPID(t *testing.T) {
	token := "test-token"
	tracker := NewConnectedTracker()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := &Server{
		Addr:          "127.0.0.1:0",
		Token:         token,
		CmdFactory:    echoCmd,
		Logger:        zerolog.Nop(),
		ConnectedPIDs: tracker,
	}

	ln, err := net.Listen("tcp", srv.Addr)
	require.NoError(t, err)
	addr := ln.Addr().String()
	_ = ln.Close()
	srv.Addr = addr

	go func() { _ = srv.ListenAndServe(ctx) }()
	time.Sleep(50 * time.Millisecond)

	resp := sendRequest(t, addr, Request{
		Token:     token,
		Args:      []string{"echo", "hi"},
		ClientPID: 0,
	})
	assert.Equal(t, 0, resp.ExitCode)
	assert.Empty(t, tracker.PIDs())
}

// --- detectDestructive tests ---

func TestDetectDestructive_Delete(t *testing.T) {
	op, ok := detectDestructive([]string{"jira", "issue", "delete", "KAN-1"})
	assert.True(t, ok)
	assert.Equal(t, "DeleteIssue", op.Operation)
	assert.Equal(t, "jira", op.Tracker)
	assert.Equal(t, "KAN-1", op.Key)
}

func TestDetectDestructive_Edit(t *testing.T) {
	op, ok := detectDestructive([]string{"linear", "issue", "edit", "HUM-42", "--title", "New"})
	assert.True(t, ok)
	assert.Equal(t, "EditIssue", op.Operation)
	assert.Equal(t, "linear", op.Tracker)
	assert.Equal(t, "HUM-42", op.Key)
}

func TestDetectDestructive_WithSafePrefix(t *testing.T) {
	op, ok := detectDestructive([]string{"--safe", "jira", "issue", "delete", "KAN-1"})
	assert.True(t, ok)
	assert.Equal(t, "DeleteIssue", op.Operation)
	assert.Equal(t, "KAN-1", op.Key)
}

func TestDetectDestructive_YesDoesNotSkip(t *testing.T) {
	// --yes is ignored by the daemon — confirmation always required via TUI.
	op, ok := detectDestructive([]string{"jira", "issue", "delete", "KAN-1", "--yes"})
	assert.True(t, ok)
	assert.Equal(t, "DeleteIssue", op.Operation)
	assert.Equal(t, "KAN-1", op.Key)
}

func TestDetectDestructive_NonDestructive(t *testing.T) {
	_, ok := detectDestructive([]string{"jira", "issue", "list", "--project", "KAN"})
	assert.False(t, ok)
}

func TestDetectDestructive_TooShort(t *testing.T) {
	_, ok := detectDestructive([]string{"jira", "issue"})
	assert.False(t, ok)
}

func TestDetectDestructive_NoIssueSubcommand(t *testing.T) {
	_, ok := detectDestructive([]string{"echo", "hello"})
	assert.False(t, ok)
}

func TestDetectDestructive_FlagInsertionBypass(t *testing.T) {
	// Flags between "issue" and "delete" must not break detection.
	op, ok := detectDestructive([]string{"jira", "issue", "--tracker=jira", "delete", "KAN-1"})
	assert.True(t, ok)
	assert.Equal(t, "DeleteIssue", op.Operation)
	assert.Equal(t, "KAN-1", op.Key)
}

func TestDetectDestructive_ArbitraryFlagsStripped(t *testing.T) {
	op, ok := detectDestructive([]string{"--verbose", "linear", "--format=json", "issue", "edit", "HUM-1", "--title", "New"})
	assert.True(t, ok)
	assert.Equal(t, "EditIssue", op.Operation)
	assert.Equal(t, "linear", op.Tracker)
	assert.Equal(t, "HUM-1", op.Key)
}

// --- Server destructive confirmation tests ---

func startTestServerWithConfirm(t *testing.T, token string) (addr string, cancel context.CancelFunc, store *PendingConfirmStore) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	store = NewPendingConfirmStore()

	srv := &Server{
		Addr:            "127.0.0.1:0",
		Token:           token,
		CmdFactory:      echoCmd,
		Logger:          zerolog.Nop(),
		PendingConfirms: store,
	}

	ln, err := net.Listen("tcp", srv.Addr)
	require.NoError(t, err)
	addr = ln.Addr().String()
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
	return addr, cancel, store
}

func TestServer_DestructiveConfirm_Approved(t *testing.T) {
	token := "test-token"
	addr, _, store := startTestServerWithConfirm(t, token)

	// Send a destructive command in a goroutine — it will block.
	type result struct {
		resp1 Response
		resp2 Response
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			ch <- result{err: err}
			return
		}
		defer func() { _ = conn.Close() }()

		enc := json.NewEncoder(conn)
		_ = enc.Encode(Request{Token: token, ClientPID: 1111, Args: []string{"jira", "issue", "delete", "KAN-1"}})

		reader := bufio.NewReader(conn)
		line1, err := reader.ReadBytes('\n')
		if err != nil {
			ch <- result{err: err}
			return
		}
		var r1 Response
		_ = json.Unmarshal(line1, &r1)

		line2, err := reader.ReadBytes('\n')
		if err != nil {
			ch <- result{err: err}
			return
		}
		var r2 Response
		_ = json.Unmarshal(line2, &r2)

		ch <- result{resp1: r1, resp2: r2}
	}()

	// Wait for the pending confirmation to appear.
	deadline := time.Now().Add(2 * time.Second)
	for store.Len() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	require.Equal(t, 1, store.Len())

	snap := store.Snapshot()
	require.Len(t, snap, 1)
	assert.Equal(t, "DeleteIssue", snap[0].Operation)
	assert.Equal(t, "KAN-1", snap[0].Key)

	// Approve it as a distinct client (different PID from the requester).
	err := store.Resolve(snap[0].ID, true, 2222)
	require.NoError(t, err)

	r := <-ch
	require.NoError(t, r.err)
	assert.True(t, r.resp1.AwaitConfirm)
	assert.Contains(t, r.resp1.ConfirmPrompt, "KAN-1")
	// Line 2: the command executed (echo cmd handles "issue delete KAN-1 --yes" as unknown, so exit 1 is fine)
	// The important thing is we got two lines.
	assert.NotEmpty(t, r.resp1.ConfirmID)
}

func TestServer_DestructiveConfirm_Rejected(t *testing.T) {
	token := "test-token"
	addr, _, store := startTestServerWithConfirm(t, token)

	type result struct {
		resp1 Response
		resp2 Response
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			ch <- result{err: err}
			return
		}
		defer func() { _ = conn.Close() }()

		enc := json.NewEncoder(conn)
		_ = enc.Encode(Request{Token: token, ClientPID: 1111, Args: []string{"jira", "issue", "delete", "KAN-2"}})

		reader := bufio.NewReader(conn)
		line1, _ := reader.ReadBytes('\n')
		var r1 Response
		_ = json.Unmarshal(line1, &r1)

		line2, _ := reader.ReadBytes('\n')
		var r2 Response
		_ = json.Unmarshal(line2, &r2)

		ch <- result{resp1: r1, resp2: r2}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for store.Len() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	snap := store.Snapshot()
	require.Len(t, snap, 1)

	// Reject it as a distinct client.
	err := store.Resolve(snap[0].ID, false, 2222)
	require.NoError(t, err)

	r := <-ch
	require.NoError(t, r.err)
	assert.True(t, r.resp1.AwaitConfirm)
	assert.Contains(t, r.resp2.Stderr, "aborted")
	assert.Equal(t, 1, r.resp2.ExitCode)
}

func TestServer_DestructiveYes_StillRequiresConfirmation(t *testing.T) {
	token := "test-token"
	addr, _, store := startTestServerWithConfirm(t, token)

	// --yes does NOT bypass daemon confirmation; the daemon always asks.
	type result struct {
		resp1 Response
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			ch <- result{err: err}
			return
		}
		defer func() { _ = conn.Close() }()

		enc := json.NewEncoder(conn)
		_ = enc.Encode(Request{Token: token, ClientPID: 1111, Args: []string{"jira", "issue", "delete", "KAN-3", "--yes"}})

		reader := bufio.NewReader(conn)
		line1, err := reader.ReadBytes('\n')
		if err != nil {
			ch <- result{err: err}
			return
		}
		var r1 Response
		_ = json.Unmarshal(line1, &r1)
		ch <- result{resp1: r1}
	}()

	// Confirmation should still be created.
	deadline := time.Now().Add(2 * time.Second)
	for store.Len() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	assert.Equal(t, 1, store.Len())

	// Clean up: resolve it so the goroutine can finish. Use a distinct PID.
	snap := store.Snapshot()
	if len(snap) > 0 {
		_ = store.Resolve(snap[0].ID, false, 2222)
	}

	r := <-ch
	require.NoError(t, r.err)
	assert.True(t, r.resp1.AwaitConfirm)
}

func TestServer_PendingConfirmsEndpoint(t *testing.T) {
	token := "test-token"
	addr, _, store := startTestServerWithConfirm(t, token)

	// Initially empty.
	resp := sendRequest(t, addr, Request{Token: token, Args: []string{"pending-confirms"}})
	assert.Equal(t, "[]\n", resp.Stdout)

	// Add a pending confirmation manually.
	store.Add(&PendingConfirmation{
		ID:        "test-1",
		Operation: "DeleteIssue",
		Tracker:   "jira",
		Key:       "KAN-1",
		Prompt:    "Delete KAN-1?",
		CreatedAt: time.Now(),
		Decision:  make(chan bool, 1),
	})

	resp = sendRequest(t, addr, Request{Token: token, Args: []string{"pending-confirms"}})
	assert.Contains(t, resp.Stdout, "test-1")
	assert.Contains(t, resp.Stdout, "KAN-1")
}

func TestServer_ConfirmOpEndpoint(t *testing.T) {
	token := "test-token"
	addr, _, store := startTestServerWithConfirm(t, token)

	pc := &PendingConfirmation{
		ID:       "test-resolve",
		Decision: make(chan bool, 1),
	}
	store.Add(pc)

	resp := sendRequest(t, addr, Request{Token: token, Args: []string{"confirm-op", "test-resolve", "yes"}})
	assert.Equal(t, "ok\n", resp.Stdout)

	decision := <-pc.Decision
	assert.True(t, decision)
}

func TestServer_ConfirmOpEndpoint_NotFound(t *testing.T) {
	token := "test-token"
	addr, _, _ := startTestServerWithConfirm(t, token)

	resp := sendRequest(t, addr, Request{Token: token, Args: []string{"confirm-op", "nonexistent", "yes"}})
	assert.Equal(t, 1, resp.ExitCode)
	assert.Contains(t, resp.Stderr, "nonexistent")
}
