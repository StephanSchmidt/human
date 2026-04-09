package cmdagent

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/errors"
	"github.com/StephanSchmidt/human/internal/agent"
	"github.com/StephanSchmidt/human/internal/daemon"
	"github.com/StephanSchmidt/human/internal/devcontainer"
)

// containerStarter implements agent.ContainerStarter using the devcontainer package.
type containerStarter struct {
	docker     devcontainer.DockerClient
	logger     zerolog.Logger
	daemonInfo *daemon.DaemonInfo
}

func newContainerStarter(cmd *cobra.Command) (*containerStarter, error) {
	docker, err := devcontainer.NewDockerClient()
	if err != nil {
		return nil, errors.WrapWithDetails(err, "connecting to Docker")
	}
	logger := zerolog.New(zerolog.ConsoleWriter{Out: cmd.ErrOrStderr()}).With().Timestamp().Logger()

	// Read daemon info for connectivity injection.
	var daemonInfo *daemon.DaemonInfo
	if info, infoErr := daemon.ReadInfo(); infoErr == nil && info.IsReachable() {
		daemonInfo = &info
	}

	return &containerStarter{
		docker:     docker,
		logger:     logger,
		daemonInfo: daemonInfo,
	}, nil
}

func (c *containerStarter) StartAgentContainer(ctx context.Context, containerName, sourceDir string) (string, error) {
	// Find devcontainer.json: check source dir first, then parent (for worktrees).
	projectDir := sourceDir
	if _, err := devcontainer.FindConfig(sourceDir); err != nil {
		parent := filepath.Dir(sourceDir)
		if _, parentErr := devcontainer.FindConfig(parent); parentErr == nil {
			projectDir = parent
		} else {
			return "", errors.WithDetails("no devcontainer.json found", "searched", sourceDir)
		}
	}

	mgr := &devcontainer.Manager{Docker: c.docker, Logger: c.logger}
	meta, err := mgr.Up(ctx, devcontainer.UpOptions{
		ProjectDir:    projectDir,
		ContainerName: containerName,
		SourceDir:     sourceDir,
		DaemonInfo:    c.daemonInfo,
		Out:           os.Stderr,
	})
	if err != nil {
		return "", err
	}
	return meta.ContainerID, nil
}

func (c *containerStarter) ExecClaude(ctx context.Context, containerID string, claudeArgs []string, prompt string) error {
	// Build the claude command to run inside the container.
	args := append([]string{"claude"}, claudeArgs...)
	if prompt != "" {
		args = append(args, "-p", prompt)
	}

	cmd := []string{"/bin/sh", "-c", strings.Join(args, " ")}

	execID, err := c.docker.ExecCreate(ctx, containerID, cmd, devcontainer.ExecOptions{
		User:         "root",
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return errors.WrapWithDetails(err, "creating claude exec")
	}

	// Start the exec but don't wait for it (Claude runs long).
	attach, err := c.docker.ExecAttach(ctx, execID)
	if err != nil {
		return errors.WrapWithDetails(err, "attaching to claude exec")
	}
	// Detach immediately -- Claude runs in the background inside the container.
	_ = attach.Close()

	return nil
}

func (c *containerStarter) StopContainer(ctx context.Context, containerID string) error {
	timeout := 10
	_ = c.docker.ContainerStop(ctx, containerID, &timeout)
	return c.docker.ContainerRemove(ctx, containerID, devcontainer.ContainerRemoveOptions{Force: true})
}

// Verify interface compliance.
var _ agent.ContainerStarter = (*containerStarter)(nil)
