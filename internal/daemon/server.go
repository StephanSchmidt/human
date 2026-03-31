package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/internal/browser"
	"github.com/StephanSchmidt/human/internal/config"
	"github.com/StephanSchmidt/human/internal/proxy"
	"github.com/StephanSchmidt/human/internal/tracker"
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
	Opener        BrowserOpener  // used for OAuth relay; defaults to browser.DefaultOpener
	Logger        zerolog.Logger
	ConnectedPIDs *ConnectedTracker // tracks client PIDs that have pinged; nil disables tracking
	HookEvents    *HookEventStore   // in-memory hook event buffer; nil disables hook event tracking
	IssueFetcher    func() ([]TrackerIssuesResult, error) // injected; fetches issues from configured trackers
	Projects        *ProjectRegistry                      // multi-project routing; nil means single-project mode
	PendingConfirms *PendingConfirmStore                  // pending destructive operation confirmations; nil disables

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

	// Resolve project directory for this request.
	projectDir, err := s.resolveProjectDir(req.Cwd)
	if err != nil {
		s.writeError(conn, err.Error(), 1)
		return
	}

	if s.SafeMode {
		req.Args = append([]string{"--safe"}, req.Args...)
	}

	s.Logger.Info().Strs("args", req.Args).Str("project_dir", projectDir).Msg("handling request")

	if s.routeIntercept(conn, reader, req.Args, projectDir) {
		return
	}

	// Intercept destructive operations for interactive confirmation.
	if op, ok := detectDestructive(req.Args); ok && s.PendingConfirms != nil {
		s.handleDestructiveConfirm(conn, req, op, projectDir)
		return
	}

	// Apply client environment variables (e.g. NO_COLOR, TERM, COLUMNS)
	// and set HUMAN_PROJECT_DIR so config.ResolveDir maps DirProject
	// to the correct directory for this request.
	// Mutex ensures concurrent requests don't corrupt each other's env.
	s.envMu.Lock()
	origEnv := applyEnv(req.Env)
	prevProjectDir, hadProjectDir := os.LookupEnv("HUMAN_PROJECT_DIR")
	if projectDir != "." {
		_ = os.Setenv("HUMAN_PROJECT_DIR", projectDir)
	}
	defer func() {
		if hadProjectDir {
			_ = os.Setenv("HUMAN_PROJECT_DIR", prevProjectDir)
		} else {
			_ = os.Unsetenv("HUMAN_PROJECT_DIR")
		}
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

// routeIntercept handles special commands that don't need subprocess execution.
// projectDir is the resolved project directory for this request.
// Returns true if the command was handled.
func (s *Server) routeIntercept(conn net.Conn, reader *bufio.Reader, args []string, projectDir string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "log-mode":
		s.handleLogMode(conn, args[1:])
		return true
	case "hook-event":
		s.handleHookEvent(conn, args[1:])
		return true
	case "hook-snapshot":
		s.handleHookSnapshot(conn)
		return true
	case "tracker-diagnose":
		s.handleTrackerDiagnose(conn, projectDir)
		return true
	case "tracker-issues":
		s.handleTrackerIssues(conn)
		return true
	case "pending-confirms":
		s.handlePendingConfirms(conn)
		return true
	case "confirm-op":
		s.handleConfirmOp(conn, args[1:])
		return true
	}

	// Intercept browser commands with OAuth redirect_uri for relay.
	if info, url := isBrowserWithRedirect(args); info != nil {
		s.Logger.Debug().Int("port", info.Port).Str("path", info.Path).Msg("OAuth redirect detected, starting relay")
		opener := s.Opener
		if opener == nil {
			opener = defaultBrowserOpener{}
		}
		s.handleOAuthRelay(conn, reader, info, url, opener)
		return true
	}

	return false
}

// handleHookEvent appends a Claude Code hook event to the in-memory store.
func (s *Server) handleHookEvent(conn net.Conn, args []string) {
	if s.HookEvents != nil {
		evt := ParseHookEventArgs(args)
		s.HookEvents.Append(evt)
	}
	resp := Response{Stdout: "ok\n"}
	enc := json.NewEncoder(conn)
	_ = enc.Encode(resp)
}

// handleHookSnapshot returns the current per-session hook state as JSON.
func (s *Server) handleHookSnapshot(conn net.Conn) {
	var out string
	if s.HookEvents != nil {
		snap := s.HookEvents.Snapshot()
		data, err := json.Marshal(snap)
		if err != nil {
			s.writeError(conn, err.Error(), 1)
			return
		}
		out = string(data) + "\n"
	} else {
		out = "{}\n"
	}
	resp := Response{Stdout: out}
	enc := json.NewEncoder(conn)
	_ = enc.Encode(resp)
}

// handleTrackerDiagnose returns tracker credential status from the daemon's env.
func (s *Server) handleTrackerDiagnose(conn net.Conn, projectDir string) {
	statuses := tracker.DiagnoseTrackers(projectDir, config.UnmarshalSection, os.Getenv)
	data, err := json.Marshal(statuses)
	if err != nil {
		s.writeError(conn, err.Error(), 1)
		return
	}
	resp := Response{Stdout: string(data) + "\n"}
	enc := json.NewEncoder(conn)
	_ = enc.Encode(resp)
}

// handleTrackerIssues returns open issues from all configured tracker projects.
func (s *Server) handleTrackerIssues(conn net.Conn) {
	if s.IssueFetcher == nil {
		resp := Response{Stdout: "[]\n"}
		enc := json.NewEncoder(conn)
		_ = enc.Encode(resp)
		return
	}
	results, err := s.IssueFetcher()
	if err != nil {
		s.writeError(conn, err.Error(), 1)
		return
	}
	data, err := json.Marshal(results)
	if err != nil {
		s.writeError(conn, err.Error(), 1)
		return
	}
	resp := Response{Stdout: string(data) + "\n"}
	enc := json.NewEncoder(conn)
	_ = enc.Encode(resp)
}

func (s *Server) writeError(conn net.Conn, msg string, code int) {
	resp := Response{Stderr: msg + "\n", ExitCode: code}
	enc := json.NewEncoder(conn)
	_ = enc.Encode(resp)
}

// resolveProjectDir determines the project directory for a request based on the
// client's working directory. Returns "." when no ProjectRegistry is configured.
func (s *Server) resolveProjectDir(cwd string) (string, error) {
	if s.Projects == nil {
		return ".", nil
	}
	if s.Projects.Single() {
		return s.Projects.Entries()[0].Dir, nil
	}
	entry, ok := s.Projects.Resolve(cwd)
	if !ok {
		var dirs []string
		for _, e := range s.Projects.Entries() {
			dirs = append(dirs, e.Dir+" ("+e.Name+")")
		}
		return "", fmt.Errorf("cwd does not match any registered project: %s\nRegistered projects:\n  %s",
			cwd, strings.Join(dirs, "\n  "))
	}
	return entry.Dir, nil
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

// --- destructive operation confirmation ---

// destructiveOp describes a detected destructive command.
type destructiveOp struct {
	Operation string // "DeleteIssue", "EditIssue"
	Tracker   string // tracker kind from args, e.g. "jira"
	Key       string // issue key, e.g. "KAN-1"
}

// detectDestructive inspects CLI args for destructive issue commands.
// Returns the operation details and true if the command is destructive and
// should be intercepted. The daemon always intercepts — --yes is ignored
// when the daemon is running; confirmation must come from the TUI.
func detectDestructive(args []string) (destructiveOp, bool) {
	// Strip flags (e.g. --safe, --yes) to find the tracker subcommand.
	cleaned := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--yes" || a == "--safe" {
			continue
		}
		cleaned = append(cleaned, a)
	}

	// Pattern: <tracker> issue delete <KEY>
	//          <tracker> issue edit <KEY> ...
	if len(cleaned) < 4 {
		return destructiveOp{}, false
	}

	// Find "issue" subcommand — it may not be at index 1 if --safe was prepended.
	trackerKind := ""
	issueIdx := -1
	for i, a := range cleaned {
		if a == "issue" || a == "issues" {
			issueIdx = i
			break
		}
		// Skip flags.
		if !strings.HasPrefix(a, "-") {
			trackerKind = a
		}
	}
	if issueIdx < 0 || issueIdx+2 >= len(cleaned) {
		return destructiveOp{}, false
	}

	verb := cleaned[issueIdx+1]
	key := cleaned[issueIdx+2]

	switch verb {
	case "delete":
		return destructiveOp{Operation: "DeleteIssue", Tracker: trackerKind, Key: key}, true
	case "edit":
		return destructiveOp{Operation: "EditIssue", Tracker: trackerKind, Key: key}, true
	default:
		return destructiveOp{}, false
	}
}

// handleDestructiveConfirm implements the two-line confirmation protocol for
// destructive operations. It pauses the connection, stores a pending
// confirmation for the TUI, and waits for a decision or timeout.
func (s *Server) handleDestructiveConfirm(conn net.Conn, req Request, op destructiveOp, projectDir string) {
	id := fmt.Sprintf("%s-%s-%d", op.Tracker, op.Key, time.Now().UnixNano())
	prompt := fmt.Sprintf("%s %s?", op.Operation, op.Key)

	pc := &PendingConfirmation{
		ID:        id,
		Operation: op.Operation,
		Tracker:   op.Tracker,
		Key:       op.Key,
		Prompt:    prompt,
		ClientPID: req.ClientPID,
		CreatedAt: time.Now(),
		Decision:  make(chan bool, 1),
	}

	s.PendingConfirms.Add(pc)
	s.Logger.Info().Str("id", id).Str("prompt", prompt).Msg("destructive operation awaiting confirmation")

	// Line 1: tell the client to wait.
	enc := json.NewEncoder(conn)
	resp1 := Response{
		Stderr:        "", // client prints its own "Waiting for confirmation" message
		AwaitConfirm:  true,
		ConfirmID:     id,
		ConfirmPrompt: prompt,
	}
	if err := enc.Encode(resp1); err != nil {
		s.Logger.Warn().Err(err).Msg("failed to write confirm line 1")
		return
	}

	// Wait for TUI decision or timeout.
	var approved bool
	select {
	case approved = <-pc.Decision:
	case <-time.After(confirmTimeout):
		s.Logger.Warn().Str("id", id).Msg("destructive confirmation timed out")
		// Remove from store if still present (Cleanup may have already done it).
		_ = s.PendingConfirms.Resolve(id, false)
		approved = false
	}

	if !approved {
		resp2 := Response{Stderr: "Operation aborted\n", ExitCode: 1}
		_ = enc.Encode(resp2)
		return
	}

	s.Logger.Info().Str("id", id).Msg("destructive operation approved, executing")

	// Execute the original command.
	s.envMu.Lock()
	origEnv := applyEnv(req.Env)
	prevProjectDir, hadProjectDir := os.LookupEnv("HUMAN_PROJECT_DIR")
	if projectDir != "." {
		_ = os.Setenv("HUMAN_PROJECT_DIR", projectDir)
	}
	defer func() {
		if hadProjectDir {
			_ = os.Setenv("HUMAN_PROJECT_DIR", prevProjectDir)
		} else {
			_ = os.Unsetenv("HUMAN_PROJECT_DIR")
		}
		restoreEnv(origEnv)
		s.envMu.Unlock()
	}()

	// Inject --yes so the Cobra command doesn't try to prompt again.
	execArgs := append(req.Args, "--yes")

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd := s.CmdFactory()
	cmd.SetArgs(execArgs)
	cmd.SetOut(&stdoutBuf)
	cmd.SetErr(&stderrBuf)

	exitCode := 0
	if err := cmd.Execute(); err != nil {
		exitCode = 1
	}

	resp2 := Response{
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		ExitCode: exitCode,
	}
	_ = enc.Encode(resp2)
}

// handlePendingConfirms returns the current pending confirmations as JSON.
func (s *Server) handlePendingConfirms(conn net.Conn) {
	var out string
	if s.PendingConfirms != nil {
		snap := s.PendingConfirms.Snapshot()
		data, err := json.Marshal(snap)
		if err != nil {
			s.writeError(conn, err.Error(), 1)
			return
		}
		out = string(data) + "\n"
	} else {
		out = "[]\n"
	}
	resp := Response{Stdout: out}
	enc := json.NewEncoder(conn)
	_ = enc.Encode(resp)
}

// handleConfirmOp resolves a pending confirmation with the given decision.
// Expected args: [ID, "yes"|"no"].
func (s *Server) handleConfirmOp(conn net.Conn, args []string) {
	if len(args) < 2 {
		s.writeError(conn, "usage: confirm-op ID yes|no", 1)
		return
	}
	id := args[0]
	approved := args[1] == "yes"

	if s.PendingConfirms == nil {
		s.writeError(conn, "confirmation store not available", 1)
		return
	}
	if err := s.PendingConfirms.Resolve(id, approved); err != nil {
		s.writeError(conn, err.Error(), 1)
		return
	}

	resp := Response{Stdout: "ok\n"}
	enc := json.NewEncoder(conn)
	_ = enc.Encode(resp)
}
