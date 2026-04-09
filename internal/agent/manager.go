package agent

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/StephanSchmidt/human/errors"
	"github.com/StephanSchmidt/human/internal/devcontainer"
)

// validNameRe matches agent names: alphanumeric, hyphens, underscores.
var validNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

func isValidName(name string) bool {
	return validNameRe.MatchString(name)
}

// StartOpts configures an agent start operation.
type StartOpts struct {
	Name       string
	Prompt     string
	Model      string
	SkipPerms  bool
	ConfigDir  string // where .devcontainer/devcontainer.json lives (default: cwd)
	Workspace  string // directory to mount into container (default: cwd)
	NoWorktree bool
	Rebuild    bool
	Interactive bool // foreground TTY mode
}

// Manager orchestrates agent lifecycle using devcontainers.
type Manager struct {
	Docker     devcontainer.DockerClient
	GitRunner  GitRunner // for worktree operations
}

// GitRunner abstracts git commands for testability.
type GitRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// Start creates a new container-based agent.
func (m *Manager) Start(ctx context.Context, opts StartOpts) (Meta, error) {
	if !isValidName(opts.Name) {
		return Meta{}, errors.WithDetails("invalid agent name: must be alphanumeric with hyphens/underscores", "name", opts.Name)
	}

	// Check for existing running agent.
	existing, err := ReadMeta(opts.Name)
	if err == nil && existing.Status == StatusRunning {
		if m.isContainerAlive(ctx, existing.ContainerID) {
			return Meta{}, errors.WithDetails("agent already running", "name", opts.Name)
		}
		existing.Status = StatusStopped
		existing.StoppedAt = time.Now()
		_ = WriteMeta(existing)
	}

	containerName := ContainerPrefix + opts.Name
	workspace, configDir := resolveDirectories(opts)

	worktreeDir, workspace := m.maybeCreateWorktree(ctx, opts, workspace)

	dcMeta, err := m.startDevcontainer(ctx, containerName, configDir, workspace, opts.Rebuild)
	if err != nil {
		m.cleanupWorktree(ctx, worktreeDir)
		return Meta{}, errors.WrapWithDetails(err, "starting agent container", "name", opts.Name)
	}

	if !opts.Interactive && opts.Prompt != "" {
		m.execClaudeDetached(ctx, dcMeta.ContainerID, opts)
	}

	meta := Meta{
		Name: opts.Name, ContainerID: dcMeta.ContainerID, ContainerName: containerName,
		WorktreeDir: worktreeDir, Cwd: workspace, Prompt: opts.Prompt,
		Status: StatusRunning, CreatedAt: time.Now(), SkipPerms: opts.SkipPerms,
		Model: opts.Model, ConfigDir: configDir, ImageName: dcMeta.ImageName,
	}
	if err := WriteMeta(meta); err != nil {
		return Meta{}, err
	}
	return meta, nil
}

func resolveDirectories(opts StartOpts) (workspace, configDir string) {
	workspace = opts.Workspace
	if workspace == "" {
		workspace = "."
	}
	configDir = opts.ConfigDir
	if configDir == "" {
		configDir = workspace
	}
	return
}

func (m *Manager) maybeCreateWorktree(ctx context.Context, opts StartOpts, workspace string) (worktreeDir, resolvedWorkspace string) {
	if opts.NoWorktree || m.GitRunner == nil {
		return "", workspace
	}
	repoRoot, rootErr := m.gitRepoRoot(ctx)
	if rootErr != nil {
		return "", workspace
	}
	wDir, wErr := m.createWorktree(ctx, repoRoot, opts.Name, "agent/"+opts.Name)
	if wErr != nil {
		return "", workspace
	}
	return wDir, wDir
}

func (m *Manager) startDevcontainer(ctx context.Context, containerName, configDir, workspace string, rebuild bool) (*devcontainer.Meta, error) {
	dcMgr := &devcontainer.Manager{Docker: m.Docker}
	return dcMgr.Up(ctx, devcontainer.UpOptions{
		ProjectDir:    configDir,
		ContainerName: containerName,
		SourceDir:     workspace,
		Rebuild:       rebuild,
		Out:           nopWriter{},
	})
}

func (m *Manager) execClaudeDetached(ctx context.Context, containerID string, opts StartOpts) {
	claudeArgs := m.buildClaudeArgs(opts)
	claudeArgs = append(claudeArgs, "-p", opts.Prompt)
	cmd := []string{"/bin/sh", "-c", "claude " + strings.Join(claudeArgs, " ")}
	execID, execErr := m.Docker.ExecCreate(ctx, containerID, cmd, devcontainer.ExecOptions{
		User: "root", AttachStdout: true, AttachStderr: true,
	})
	if execErr == nil {
		if attach, attachErr := m.Docker.ExecAttach(ctx, execID); attachErr == nil {
			_ = attach.Close()
		}
	}
}

func (m *Manager) cleanupWorktree(ctx context.Context, worktreeDir string) {
	if worktreeDir == "" || m.GitRunner == nil {
		return
	}
	repoRoot, err := m.gitRepoRoot(ctx)
	if err == nil {
		_ = m.removeWorktree(ctx, repoRoot, worktreeDir)
	}
}

// Stop stops and removes an agent's container.
func (m *Manager) Stop(ctx context.Context, name string, cleanWorktree bool) error {
	meta, err := ReadMeta(name)
	if err != nil {
		return err
	}

	if meta.ContainerID != "" {
		timeout := 10
		_ = m.Docker.ContainerStop(ctx, meta.ContainerID, &timeout)
		_ = m.Docker.ContainerRemove(ctx, meta.ContainerID, devcontainer.ContainerRemoveOptions{Force: true})
	}

	if cleanWorktree && meta.WorktreeDir != "" && m.GitRunner != nil {
		repoRoot, rootErr := m.gitRepoRoot(ctx)
		if rootErr == nil {
			_ = m.removeWorktree(ctx, repoRoot, meta.WorktreeDir)
		}
	}

	meta.Status = StatusStopped
	meta.StoppedAt = time.Now()
	return WriteMeta(meta)
}

// Attach returns the container name for docker exec -it.
func (m *Manager) Attach(_ context.Context, name string) (string, error) {
	meta, err := ReadMeta(name)
	if err != nil {
		return "", err
	}
	if meta.ContainerName == "" {
		return "", errors.WithDetails("agent has no container", "name", name)
	}
	return meta.ContainerName, nil
}

// Refresh syncs metadata with actual container state.
func (m *Manager) Refresh(ctx context.Context) error {
	metas, err := ListMetas()
	if err != nil {
		return err
	}
	for _, meta := range metas {
		if meta.Status != StatusRunning {
			continue
		}
		if !m.isContainerAlive(ctx, meta.ContainerID) {
			meta.Status = StatusStopped
			meta.StoppedAt = time.Now()
			_ = WriteMeta(meta)
		}
	}
	return nil
}

// isContainerAlive checks if a container is running via Docker inspect.
func (m *Manager) isContainerAlive(ctx context.Context, containerID string) bool {
	if containerID == "" {
		return false
	}
	resp, err := m.Docker.ContainerInspect(ctx, containerID)
	if err != nil {
		return false
	}
	return resp.State.Running
}

// buildClaudeArgs constructs Claude Code CLI arguments.
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

// --- git worktree operations ---

func (m *Manager) gitRepoRoot(ctx context.Context) (string, error) {
	out, err := m.GitRunner.Run(ctx, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (m *Manager) createWorktree(ctx context.Context, repoRoot, name, branch string) (string, error) {
	worktreeDir := fmt.Sprintf("%s/.worktrees/%s", repoRoot, name)
	_, _ = m.GitRunner.Run(ctx, "git", "branch", branch)
	_, err := m.GitRunner.Run(ctx, "git", "worktree", "add", worktreeDir, branch)
	if err != nil {
		return "", errors.WrapWithDetails(err, "creating git worktree", "dir", worktreeDir)
	}
	return worktreeDir, nil
}

func (m *Manager) removeWorktree(ctx context.Context, repoRoot, worktreeDir string) error {
	_, err := m.GitRunner.Run(ctx, "git", "-C", repoRoot, "worktree", "remove", "--force", worktreeDir)
	if err != nil {
		return errors.WrapWithDetails(err, "removing git worktree", "dir", worktreeDir)
	}
	_, _ = m.GitRunner.Run(ctx, "git", "-C", repoRoot, "worktree", "prune")
	return nil
}

// nopWriter discards all output.
type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }
