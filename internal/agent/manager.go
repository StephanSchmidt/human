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
	Name       string
	Prompt     string
	TicketKey  string
	Model      string
	NoWorktree bool
	SkipPerms  bool
	Container  bool // run inside a devcontainer instead of tmux
}

// ContainerStarter creates and starts a devcontainer for an agent.
// Injected by the command layer to avoid circular imports with internal/devcontainer.
type ContainerStarter interface {
	// StartAgentContainer builds the devcontainer image (cached), creates a
	// container named containerName mounting sourceDir, installs features,
	// runs lifecycle hooks, and returns the container ID.
	StartAgentContainer(ctx context.Context, containerName, sourceDir string) (containerID string, err error)

	// ExecClaude runs Claude Code inside the container with the given args.
	// Returns immediately after starting (non-blocking).
	ExecClaude(ctx context.Context, containerID string, claudeArgs []string, prompt string) error

	// StopContainer stops and removes a container.
	StopContainer(ctx context.Context, containerID string) error
}

// Manager orchestrates agent lifecycle operations.
type Manager struct {
	Runner           claude.CommandRunner
	HomeDir          string           // override for testing; empty uses os.UserHomeDir
	ContainerStarter ContainerStarter // nil = container mode unavailable
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

	// Container-based agent path.
	if opts.Container {
		return m.startContainer(ctx, opts)
	}

	return m.startTmux(ctx, opts)
}

// startTmux creates a tmux-based agent with optional worktree.
func (m *Manager) startTmux(ctx context.Context, opts StartOpts) (Meta, error) {
	sessionName := TmuxSessionName(opts.Name)
	worktreeDir, cwd, err := m.resolveWorkdir(ctx, opts)
	if err != nil {
		return Meta{}, err
	}

	claudeCmd := "claude " + strings.Join(m.buildClaudeArgs(opts), " ")

	_, err = m.Runner.Run(ctx, "tmux", "new-session", "-d", "-s", sessionName, "-c", cwd, claudeCmd)
	if err != nil {
		if worktreeDir != "" {
			repoRoot, _ := m.gitRepoRoot(ctx)
			_ = m.removeWorktree(ctx, repoRoot, worktreeDir)
		}
		return Meta{}, errors.WrapWithDetails(err, "creating tmux session", "session", sessionName)
	}

	m.sendPrompt(ctx, sessionName, opts.Prompt)

	meta := Meta{
		Name: opts.Name, SessionName: sessionName, WorktreeDir: worktreeDir,
		Cwd: cwd, Prompt: opts.Prompt, TicketKey: opts.TicketKey,
		Status: StatusRunning, CreatedAt: time.Now(),
		SkipPerms: opts.SkipPerms, Model: opts.Model,
	}
	if err := WriteMeta(meta); err != nil {
		return Meta{}, err
	}
	return meta, nil
}

// resolveWorkdir sets up the working directory, optionally creating a worktree.
func (m *Manager) resolveWorkdir(ctx context.Context, opts StartOpts) (worktreeDir, cwd string, err error) {
	repoRoot, rootErr := m.gitRepoRoot(ctx)
	if !opts.NoWorktree {
		if rootErr != nil {
			return "", "", errors.WrapWithDetails(rootErr, "cannot create worktree: not inside a git repository")
		}
		wDir, wErr := m.createWorktree(ctx, repoRoot, opts.Name, "agent/"+opts.Name)
		if wErr != nil {
			return "", "", wErr
		}
		return wDir, wDir, nil
	}
	if rootErr == nil {
		return "", repoRoot, nil
	}
	return "", ".", nil
}

// sendPrompt sends a prompt to a tmux session via send-keys.
func (m *Manager) sendPrompt(ctx context.Context, sessionName, prompt string) {
	if prompt == "" {
		return
	}
	time.Sleep(2 * time.Second)
	target := sessionName + ":0.0"
	escaped := shellQuote(prompt)
	_, _ = m.Runner.Run(ctx, "tmux", "send-keys", "-t", target, "-l", escaped)
	_, _ = m.Runner.Run(ctx, "tmux", "send-keys", "-t", target, "Enter")
}

// startContainer creates a devcontainer and runs Claude inside it.
func (m *Manager) startContainer(ctx context.Context, opts StartOpts) (Meta, error) {
	if m.ContainerStarter == nil {
		return Meta{}, errors.WithDetails("container mode requires Docker; ContainerStarter not configured")
	}

	repoRoot, rootErr := m.gitRepoRoot(ctx)
	if rootErr != nil {
		return Meta{}, errors.WrapWithDetails(rootErr, "cannot start container agent: not inside a git repository")
	}

	// Create worktree for source isolation (unless --no-worktree).
	var worktreeDir, sourceDir string
	if !opts.NoWorktree {
		branch := "agent/" + opts.Name
		wDir, wErr := m.createWorktree(ctx, repoRoot, opts.Name, branch)
		if wErr != nil {
			return Meta{}, wErr
		}
		worktreeDir = wDir
		sourceDir = wDir
	} else {
		sourceDir = repoRoot
	}

	containerName := SessionPrefix + opts.Name

	containerID, err := m.ContainerStarter.StartAgentContainer(ctx, containerName, sourceDir)
	if err != nil {
		if worktreeDir != "" {
			_ = m.removeWorktree(ctx, repoRoot, worktreeDir)
		}
		return Meta{}, errors.WrapWithDetails(err, "starting agent container", "name", opts.Name)
	}

	// Start Claude inside the container.
	claudeArgs := m.buildClaudeArgs(opts)
	if err := m.ContainerStarter.ExecClaude(ctx, containerID, claudeArgs, opts.Prompt); err != nil {
		// Container started but Claude failed. Leave container running for debugging.
		_ = err
	}

	meta := Meta{
		Name:          opts.Name,
		SessionName:   containerName,
		ContainerID:   containerID,
		ContainerName: containerName,
		WorktreeDir:   worktreeDir,
		Cwd:           sourceDir,
		Prompt:        opts.Prompt,
		TicketKey:     opts.TicketKey,
		Status:        StatusRunning,
		CreatedAt:     time.Now(),
		SkipPerms:     opts.SkipPerms,
		Model:         opts.Model,
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

	// Container-based agent: stop and remove the container.
	if meta.ContainerID != "" && m.ContainerStarter != nil {
		_ = m.ContainerStarter.StopContainer(ctx, meta.ContainerID)
	} else {
		// Tmux-based agent: kill the session (ignore errors if already dead).
		_, _ = m.Runner.Run(ctx, "tmux", "kill-session", "-t", meta.SessionName)
	}

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
		alive := false
		if meta.ContainerID != "" {
			// Container-based: check if container is still running.
			// We can't use Docker client here without a dependency, so
			// rely on tmux-like liveness check via the session name.
			alive = m.isSessionAlive(ctx, meta.SessionName)
		} else {
			alive = m.isSessionAlive(ctx, meta.SessionName)
		}
		if !alive {
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
