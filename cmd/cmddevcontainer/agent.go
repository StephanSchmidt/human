package cmddevcontainer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/errors"
	"github.com/StephanSchmidt/human/internal/devcontainer"
)

func buildAgentCmd() *cobra.Command {
	var model string
	var skipPerms bool
	var rebuild bool

	cmd := &cobra.Command{
		Use:   "agent [project-dir]",
		Short: "Start Claude Code interactively inside a devcontainer",
		Long: `Start a devcontainer and launch Claude Code inside it interactively.
This is the simplest way to get Claude working in a secure container with
all credentials forwarded from the daemon.

The container is reused if already running. Use --rebuild to force a fresh image.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectDir := "."
			if len(args) > 0 {
				projectDir = args[0]
			}
			absDir, _ := filepath.Abs(projectDir)

			out := cmd.OutOrStdout()
			logger := zerolog.New(zerolog.ConsoleWriter{Out: cmd.ErrOrStderr()}).With().Timestamp().Logger()

			// Ensure daemon is running.
			daemonInfo, _ := ensureDaemon(projectDir)

			docker, err := devcontainer.NewDockerClient()
			if err != nil {
				return errors.WrapWithDetails(err, "connecting to Docker")
			}
			defer func() { _ = docker.Close() }()

			mgr := &devcontainer.Manager{Docker: docker, Logger: logger}

			// Start or reuse the devcontainer.
			_, err = mgr.Up(cmd.Context(), devcontainer.UpOptions{
				ProjectDir: absDir,
				Rebuild:    rebuild,
				DaemonInfo: daemonInfo,
				Out:        out,
			})
			if err != nil {
				return err
			}

			// Resolve the running container.
			meta, err := mgr.ResolveContainer(absDir)
			if err != nil {
				return errors.WrapWithDetails(err, "finding devcontainer")
			}

			// Build claude command.
			claudeArgs := []string{"exec", "-it", meta.ContainerID}
			claudeArgs = append(claudeArgs, "claude")
			if skipPerms {
				claudeArgs = append(claudeArgs, "--dangerously-skip-permissions")
			} else {
				claudeArgs = append(claudeArgs, "--permission-mode=auto")
			}
			if model != "" {
				claudeArgs = append(claudeArgs, "--model", model)
			}

			_, _ = fmt.Fprintf(out, "\nStarting Claude Code in container %s...\n", meta.ContainerName)

			// Exec docker directly to get a real interactive TTY.
			dockerPath, err := exec.LookPath("docker")
			if err != nil {
				return errors.WithDetails("docker not found in PATH")
			}

			// Replace the current process with docker exec -it.
			return syscallExec(dockerPath, append([]string{"docker"}, claudeArgs...), os.Environ())
		},
	}

	cmd.Flags().StringVar(&model, "model", "", "Claude model to use")
	cmd.Flags().BoolVar(&skipPerms, "skip-permissions", false, "Run with --dangerously-skip-permissions")
	cmd.Flags().BoolVar(&rebuild, "rebuild", false, "Force image rebuild")
	return cmd
}
