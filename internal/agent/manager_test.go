package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// mockRunner records commands and returns configurable results.
type mockRunner struct {
	calls   []string
	results map[string]mockResult
}

type mockResult struct {
	out []byte
	err error
}

func newMockRunner() *mockRunner {
	return &mockRunner{results: make(map[string]mockResult)}
}

func (m *mockRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	key := name + " " + strings.Join(args, " ")
	m.calls = append(m.calls, key)
	if r, ok := m.results[key]; ok {
		return r.out, r.err
	}
	// Default: match prefixes for flexibility in tests.
	for k, r := range m.results {
		if strings.HasPrefix(key, k) {
			return r.out, r.err
		}
	}
	return nil, nil
}

func (m *mockRunner) called(substr string) bool {
	for _, c := range m.calls {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

func TestIsValidName_valid(t *testing.T) {
	valids := []string{"my-agent", "agent_1", "a", "Test123", "a-b_c"}
	for _, name := range valids {
		if !isValidName(name) {
			t.Errorf("isValidName(%q) = false, want true", name)
		}
	}
}

func TestIsValidName_empty(t *testing.T) {
	if isValidName("") {
		t.Error("isValidName(\"\") = true, want false")
	}
}

func TestIsValidName_spaces(t *testing.T) {
	if isValidName("my agent") {
		t.Error("isValidName(\"my agent\") = true, want false")
	}
}

func TestIsValidName_leadingHyphen(t *testing.T) {
	if isValidName("-agent") {
		t.Error("isValidName(\"-agent\") = true, want false")
	}
}

func TestStart_happy(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	runner := newMockRunner()
	// git rev-parse returns a repo root.
	runner.results["git rev-parse --show-toplevel"] = mockResult{out: []byte("/tmp/repo\n")}
	// git branch succeeds.
	runner.results["git branch agent/test-1"] = mockResult{}
	// git worktree add succeeds.
	runner.results["git worktree add /tmp/repo/.worktrees/test-1 agent/test-1"] = mockResult{}
	// tmux new-session succeeds.
	runner.results["tmux new-session"] = mockResult{}
	// tmux send-keys succeeds.
	runner.results["tmux send-keys"] = mockResult{}

	mgr := &Manager{Runner: runner, HomeDir: tmpDir}

	meta, err := mgr.Start(context.Background(), StartOpts{
		Name:   "test-1",
		Prompt: "hello",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if meta.Status != StatusRunning {
		t.Errorf("Status = %q, want %q", meta.Status, StatusRunning)
	}
	if meta.SessionName != "human-agent-test-1" {
		t.Errorf("SessionName = %q, want %q", meta.SessionName, "human-agent-test-1")
	}
	if meta.WorktreeDir == "" {
		t.Error("WorktreeDir is empty, expected worktree to be created")
	}

	// Verify metadata was persisted.
	saved, err := ReadMeta("test-1")
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	if saved.Name != "test-1" {
		t.Errorf("saved Name = %q, want %q", saved.Name, "test-1")
	}
}

func TestStart_noWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	runner := newMockRunner()
	runner.results["tmux new-session"] = mockResult{}
	runner.results["tmux send-keys"] = mockResult{}

	mgr := &Manager{Runner: runner, HomeDir: tmpDir}

	_, err := mgr.Start(context.Background(), StartOpts{
		Name:       "no-wt",
		NoWorktree: true,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if runner.called("git worktree add") {
		t.Error("expected no git worktree add call when NoWorktree=true")
	}
}

func TestStart_duplicateRunning(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	runner := newMockRunner()
	// tmux has-session succeeds (session alive).
	runner.results["tmux has-session -t human-agent-dup"] = mockResult{}
	runner.results["tmux new-session"] = mockResult{}

	mgr := &Manager{Runner: runner, HomeDir: tmpDir}

	// Write an existing running agent.
	existing := Meta{
		Name:        "dup",
		SessionName: TmuxSessionName("dup"),
		Status:      StatusRunning,
		CreatedAt:   time.Now(),
	}
	if err := WriteMeta(existing); err != nil {
		t.Fatalf("WriteMeta: %v", err)
	}

	_, err := mgr.Start(context.Background(), StartOpts{
		Name:       "dup",
		NoWorktree: true,
	})
	if err == nil {
		t.Fatal("expected error for duplicate running agent, got nil")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("error = %q, want to contain 'already running'", err.Error())
	}
}

func TestStop_happy(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	runner := newMockRunner()
	runner.results["tmux kill-session -t human-agent-stopper"] = mockResult{}

	mgr := &Manager{Runner: runner, HomeDir: tmpDir}

	// Write a running agent.
	meta := Meta{
		Name:        "stopper",
		SessionName: TmuxSessionName("stopper"),
		Status:      StatusRunning,
		CreatedAt:   time.Now(),
	}
	if err := WriteMeta(meta); err != nil {
		t.Fatalf("WriteMeta: %v", err)
	}

	if err := mgr.Stop(context.Background(), "stopper", false); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	saved, err := ReadMeta("stopper")
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	if saved.Status != StatusStopped {
		t.Errorf("Status = %q, want %q", saved.Status, StatusStopped)
	}
}

func TestRefresh_deadSession(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	runner := newMockRunner()
	// tmux has-session fails (session dead).
	runner.results["tmux has-session -t human-agent-dead"] = mockResult{err: fmt.Errorf("no session")}

	mgr := &Manager{Runner: runner, HomeDir: tmpDir}

	meta := Meta{
		Name:        "dead",
		SessionName: TmuxSessionName("dead"),
		Status:      StatusRunning,
		CreatedAt:   time.Now(),
	}
	if err := WriteMeta(meta); err != nil {
		t.Fatalf("WriteMeta: %v", err)
	}

	if err := mgr.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	saved, err := ReadMeta("dead")
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	if saved.Status != StatusStopped {
		t.Errorf("Status = %q, want %q after refresh", saved.Status, StatusStopped)
	}
}

func TestShellQuote(t *testing.T) {
	got := shellQuote("line1\nline2")
	if strings.Contains(got, "\n") {
		t.Errorf("shellQuote should replace newlines, got %q", got)
	}
}
