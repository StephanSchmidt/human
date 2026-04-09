package agent

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/StephanSchmidt/human/errors"
	"github.com/StephanSchmidt/human/internal/claude"
)

// validNameRe matches agent names: alphanumeric, hyphens, underscores.
var validNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// isValidName returns true when the name is non-empty and contains only
// alphanumeric characters, hyphens, and underscores.
func isValidName(name string) bool {
	return validNameRe.MatchString(name)
}

// StartOpts configures an agent start operation.
type StartOpts struct {
	Name      string
	Prompt    string
	TicketKey string
	Model     string
	NoWorktree    bool
	SkipPerms     bool
}

// Manager orchestrates agent lifecycle operations.
type Manager struct {
	Runner  claude.CommandRunner
	HomeDir string // override for testing; empty uses os.UserHomeDir
}

// Start creates a new agent: validates the name, optionally creates a git
// worktree, spawns a tmux session with Claude Code, sends the prompt, and
// persists metadata.
func (m *Manager) Start(ctx context.Context, opts StartOpts) (Meta, error) {
	if !isValidName(opts.Name) {
		return Meta{}, errors.WithDetails("invalid agent name: must be alphanumeric with hyphens/underscores", "name", opts.Name)
	}

	// Check for existing running agent with same name.
	existing, err := ReadMeta(opts.Name)
	if err == nil && existing.Status == StatusRunning {
		if m.isSessionAlive(ctx, existing.SessionName) {
			return Meta{}, errors.WithDetails("agent already running", "name", opts.Name)
		}
		// Session died but metadata says running -- update it.
		existing.Status = StatusStopped
		existing.StoppedAt = time.Now()
		_ = WriteMeta(existing)
	}

	sessionName := TmuxSessionName(opts.Name)
	var worktreeDir string
	var cwd string

	repoRoot, rootErr := m.gitRepoRoot(ctx)

	if !opts.NoWorktree {
		if rootErr != nil {
			return Meta{}, errors.WrapWithDetails(rootErr, "cannot create worktree: not inside a git repository")
		}
		branch := "agent/" + opts.Name
		wDir, wErr := m.createWorktree(ctx, repoRoot, opts.Name, branch)
		if wErr != nil {
			return Meta{}, wErr
		}
		worktreeDir = wDir
		cwd = wDir
	} else {
		// Use the current working directory (or repo root if available).
		if rootErr == nil {
			cwd = repoRoot
		} else {
			cwd = "."
		}
	}

	// Build the claude command args for the tmux session.
	claudeArgs := m.buildClaudeArgs(opts)
	claudeCmd := "claude " + strings.Join(claudeArgs, " ")

	// Create a new tmux session running Claude Code.
	_, err = m.Runner.Run(ctx, "tmux", "new-session", "-d", "-s", sessionName, "-c", cwd, claudeCmd)
	if err != nil {
		// Clean up worktree if we created one.
		if worktreeDir != "" {
			_ = m.removeWorktree(ctx, repoRoot, worktreeDir)
		}
		return Meta{}, errors.WrapWithDetails(err, "creating tmux session", "session", sessionName)
	}

	// Give Claude a moment to start, then send the prompt via send-keys
	// if a prompt was provided.
	if opts.Prompt != "" {
		// Wait briefly for the Claude interactive prompt to initialize.
		time.Sleep(2 * time.Second)
		target := sessionName + ":0.0"
		escaped := shellQuote(opts.Prompt)
		if _, err := m.Runner.Run(ctx, "tmux", "send-keys", "-t", target, "-l", escaped); err != nil {
			// Non-fatal: session is running, prompt just did not send.
			_ = err
		}
		if _, err := m.Runner.Run(ctx, "tmux", "send-keys", "-t", target, "Enter"); err != nil {
			_ = err
		}
	}

	meta := Meta{
		Name:        opts.Name,
		SessionName: sessionName,
		WorktreeDir: worktreeDir,
		Cwd:         cwd,
		Prompt:      opts.Prompt,
		TicketKey:   opts.TicketKey,
		Status:      StatusRunning,
		CreatedAt:   time.Now(),
		SkipPerms:   opts.SkipPerms,
		Model:       opts.Model,
	}

	if err := WriteMeta(meta); err != nil {
		return Meta{}, err
	}

	return meta, nil
}

// Stop kills the tmux session for the named agent and optionally cleans up
// the git worktree.
func (m *Manager) Stop(ctx context.Context, name string, cleanWorktree bool) error {
	meta, err := ReadMeta(name)
	if err != nil {
		return err
	}

	// Kill the tmux session (ignore errors if already dead).
	_, _ = m.Runner.Run(ctx, "tmux", "kill-session", "-t", meta.SessionName)

	if cleanWorktree && meta.WorktreeDir != "" {
		repoRoot, rootErr := m.gitRepoRoot(ctx)
		if rootErr == nil {
			_ = m.removeWorktree(ctx, repoRoot, meta.WorktreeDir)
		}
	}

	meta.Status = StatusStopped
	meta.StoppedAt = time.Now()
	return WriteMeta(meta)
}

// Attach returns the tmux session name for the caller to exec into.
// It validates the agent exists and the session is alive.
func (m *Manager) Attach(ctx context.Context, name string) (string, error) {
	meta, err := ReadMeta(name)
	if err != nil {
		return "", err
	}
	if !m.isSessionAlive(ctx, meta.SessionName) {
		return "", errors.WithDetails("agent session is not running", "name", name, "session", meta.SessionName)
	}
	return meta.SessionName, nil
}

// Resume sends a new prompt to an alive session, or restarts a dead session
// with --continue.
func (m *Manager) Resume(ctx context.Context, name string, prompt string) error {
	meta, err := ReadMeta(name)
	if err != nil {
		return err
	}

	if m.isSessionAlive(ctx, meta.SessionName) {
		// Session is alive -- send the prompt via send-keys.
		if prompt == "" {
			return nil // nothing to send
		}
		target := meta.SessionName + ":0.0"
		escaped := shellQuote(prompt)
		if _, err := m.Runner.Run(ctx, "tmux", "send-keys", "-t", target, "-l", escaped); err != nil {
			return errors.WrapWithDetails(err, "sending prompt to tmux session", "session", meta.SessionName)
		}
		if _, err := m.Runner.Run(ctx, "tmux", "send-keys", "-t", target, "Enter"); err != nil {
			return errors.WrapWithDetails(err, "sending Enter to tmux session", "session", meta.SessionName)
		}
		return nil
	}

	// Session is dead -- restart with --continue.
	claudeArgs := []string{"--continue"}
	if meta.SkipPerms {
		claudeArgs = append(claudeArgs, "--dangerously-skip-permissions")
	}
	if meta.Model != "" {
		claudeArgs = append(claudeArgs, "--model", meta.Model)
	}
	claudeCmd := "claude " + strings.Join(claudeArgs, " ")

	cwd := meta.Cwd
	if cwd == "" {
		cwd = "."
	}

	_, err = m.Runner.Run(ctx, "tmux", "new-session", "-d", "-s", meta.SessionName, "-c", cwd, claudeCmd)
	if err != nil {
		return errors.WrapWithDetails(err, "restarting tmux session", "session", meta.SessionName)
	}

	// Send the new prompt if provided.
	if prompt != "" {
		time.Sleep(2 * time.Second)
		target := meta.SessionName + ":0.0"
		escaped := shellQuote(prompt)
		_, _ = m.Runner.Run(ctx, "tmux", "send-keys", "-t", target, "-l", escaped)
		_, _ = m.Runner.Run(ctx, "tmux", "send-keys", "-t", target, "Enter")
	}

	meta.Status = StatusRunning
	meta.StoppedAt = time.Time{}
	return WriteMeta(meta)
}

// Refresh syncs metadata statuses with actual tmux session liveness. Agents
// whose sessions have died are marked as stopped.
func (m *Manager) Refresh(ctx context.Context) error {
	metas, err := ListMetas()
	if err != nil {
		return err
	}
	for _, meta := range metas {
		if meta.Status != StatusRunning {
			continue
		}
		if !m.isSessionAlive(ctx, meta.SessionName) {
			meta.Status = StatusStopped
			meta.StoppedAt = time.Now()
			_ = WriteMeta(meta)
		}
	}
	return nil
}

// isSessionAlive checks whether the tmux session exists.
func (m *Manager) isSessionAlive(ctx context.Context, sessionName string) bool {
	_, err := m.Runner.Run(ctx, "tmux", "has-session", "-t", sessionName)
	return err == nil
}

// gitRepoRoot returns the root of the current git repository.
func (m *Manager) gitRepoRoot(ctx context.Context) (string, error) {
	out, err := m.Runner.Run(ctx, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// createWorktree creates a git worktree for the agent in .worktrees/<name>
// relative to the repo root.
func (m *Manager) createWorktree(ctx context.Context, repoRoot, name, branch string) (string, error) {
	worktreeDir := fmt.Sprintf("%s/.worktrees/%s", repoRoot, name)

	// Create the branch if it does not exist. Use the current HEAD as the
	// starting point.
	_, _ = m.Runner.Run(ctx, "git", "branch", branch)

	_, err := m.Runner.Run(ctx, "git", "worktree", "add", worktreeDir, branch)
	if err != nil {
		return "", errors.WrapWithDetails(err, "creating git worktree", "dir", worktreeDir, "branch", branch)
	}
	return worktreeDir, nil
}

// removeWorktree removes a git worktree and prunes stale entries.
func (m *Manager) removeWorktree(ctx context.Context, repoRoot, worktreeDir string) error {
	_, err := m.Runner.Run(ctx, "git", "-C", repoRoot, "worktree", "remove", "--force", worktreeDir)
	if err != nil {
		return errors.WrapWithDetails(err, "removing git worktree", "dir", worktreeDir)
	}
	_, _ = m.Runner.Run(ctx, "git", "-C", repoRoot, "worktree", "prune")
	return nil
}

// buildClaudeArgs constructs the Claude Code command-line arguments.
func (m *Manager) buildClaudeArgs(opts StartOpts) []string {
	var args []string
	if opts.SkipPerms {
		args = append(args, "--dangerously-skip-permissions")
	} else {
		args = append(args, "--permission-mode=auto")
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	return args
}

// shellQuote wraps the string so it can be safely sent to tmux via send-keys.
// Newlines are replaced with spaces to avoid accidental early submission.
func shellQuote(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
