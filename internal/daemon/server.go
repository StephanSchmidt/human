package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"sync"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/internal/browser"
	"github.com/StephanSchmidt/human/internal/proxy"
)

// defaultBrowserOpener wraps browser.DefaultOpener for production use.
type defaultBrowserOpener struct{}

func (defaultBrowserOpener) Open(url string) error {
	return browser.DefaultOpener{}.Open(url)
}

// Server listens for incoming client connections and executes CLI commands.
type Server struct {
	Addr          string
	Token         string
	SafeMode      bool
	CmdFactory    func() *cobra.Command
	Opener        BrowserOpener // used for OAuth relay; defaults to browser.DefaultOpener
	Logger        zerolog.Logger
	ConnectedPIDs *ConnectedTracker // tracks client PIDs that have pinged; nil disables tracking

	envMu sync.Mutex // protects os.Setenv/os.Unsetenv during concurrent requests
}

// ListenAndServe starts the TCP listener and blocks until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", s.Addr)
	if err != nil {
		return err
	}
	defer func() { _ = ln.Close() }()

	s.Logger.Info().Str("addr", ln.Addr().String()).Msg("daemon listening")

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			s.Logger.Warn().Err(err).Msg("accept error")
			continue
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		s.writeError(conn, "failed to read request", 1)
		return
	}

	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		s.writeError(conn, "invalid request JSON", 1)
		return
	}

	if req.Token != s.Token {
		s.writeError(conn, "authentication failed: invalid token", 1)
		return
	}

	if req.ClientPID > 0 && s.ConnectedPIDs != nil {
		s.ConnectedPIDs.Touch(req.ClientPID)
	}

	if s.SafeMode {
		req.Args = append([]string{"--safe"}, req.Args...)
	}

	s.Logger.Info().Strs("args", req.Args).Msg("handling request")

	// Intercept log-mode get/set — direct in-memory, no subprocess needed.
	if len(req.Args) >= 1 && req.Args[0] == "log-mode" {
		s.handleLogMode(conn, req.Args[1:])
		return
	}

	// Intercept browser commands with OAuth redirect_uri for relay.
	if info, url := isBrowserWithRedirect(req.Args); info != nil {
		s.Logger.Debug().Int("port", info.Port).Str("path", info.Path).Msg("OAuth redirect detected, starting relay")
		opener := s.Opener
		if opener == nil {
			opener = defaultBrowserOpener{}
		}
		s.handleOAuthRelay(conn, reader, info, url, opener)
		return
	}

	// Apply client environment variables (e.g. NO_COLOR, TERM, COLUMNS)
	// for the duration of this request, then restore the originals.
	// Mutex ensures concurrent requests don't corrupt each other's env.
	s.envMu.Lock()
	origEnv := applyEnv(req.Env)
	defer func() {
		restoreEnv(origEnv)
		s.envMu.Unlock()
	}()

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd := s.CmdFactory()
	cmd.SetArgs(req.Args)
	cmd.SetOut(&stdoutBuf)
	cmd.SetErr(&stderrBuf)

	exitCode := 0
	if err := cmd.Execute(); err != nil {
		exitCode = 1
	}

	resp := Response{
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		ExitCode: exitCode,
	}

	enc := json.NewEncoder(conn)
	if err := enc.Encode(resp); err != nil {
		s.Logger.Warn().Err(err).Msg("failed to write response")
	}
}

// handleLogMode handles get/set of the traffic log mode in-memory.
// No args → return current mode. One arg → set and return new mode.
func (s *Server) handleLogMode(conn net.Conn, args []string) {
	if len(args) == 0 {
		// Get current mode.
		mode := proxy.GetLogMode()
		resp := Response{Stdout: proxy.LogModeString(mode) + "\n"}
		enc := json.NewEncoder(conn)
		_ = enc.Encode(resp)
		return
	}

	mode, err := proxy.ParseLogMode(args[0])
	if err != nil {
		s.writeError(conn, err.Error(), 1)
		return
	}

	proxy.SetLogMode(mode)
	s.Logger.Info().Str("mode", proxy.LogModeString(mode)).Msg("traffic log mode changed")

	resp := Response{Stdout: proxy.LogModeString(mode) + "\n"}
	enc := json.NewEncoder(conn)
	_ = enc.Encode(resp)
}

func (s *Server) writeError(conn net.Conn, msg string, code int) {
	resp := Response{Stderr: msg + "\n", ExitCode: code}
	enc := json.NewEncoder(conn)
	_ = enc.Encode(resp)
}

// envEntry records an env var's previous state so it can be restored.
type envEntry struct {
	key   string
	value string
	set   bool // whether the var was set before
}

// applyEnv sets the given env vars and returns their previous values.
func applyEnv(env map[string]string) []envEntry {
	orig := make([]envEntry, 0, len(env))
	for k, v := range env {
		prev, wasSet := os.LookupEnv(k)
		orig = append(orig, envEntry{key: k, value: prev, set: wasSet})
		_ = os.Setenv(k, v)
	}
	return orig
}

// restoreEnv restores env vars to their state before applyEnv.
func restoreEnv(orig []envEntry) {
	for _, e := range orig {
		if e.set {
			_ = os.Setenv(e.key, e.value)
		} else {
			_ = os.Unsetenv(e.key)
		}
	}
}
