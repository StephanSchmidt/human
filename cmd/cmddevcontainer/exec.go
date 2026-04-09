package cmddevcontainer

import (
	"os"
	"path/filepath"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/errors"
	"github.com/StephanSchmidt/human/internal/devcontainer"
)

func buildExecCmd() *cobra.Command {
	var user string

	cmd := &cobra.Command{
		Use:   "exec [project-dir] -- command [args...]",
		Short: "Execute a command inside a running devcontainer",
		Long: `Execute a command inside a running devcontainer. Use -- to separate
the command from devcontainer flags. Commands run non-interactively
(no TTY). For an interactive shell, use: docker exec -it <container> bash

Examples:
  human devcontainer exec -- npm test
  human devcontainer exec -- ls -la /workspaces
  human devcontainer exec --user root -- apt-get update`,
		Args:               cobra.MinimumNArgs(1),
		DisableFlagParsing: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Parse project dir and command.
			projectDir := "."
			execCmd := args

			// If first arg looks like a path (not a command), use it as project dir.
			if len(args) > 1 {
				if _, err := os.Stat(args[0]); err == nil {
					projectDir = args[0]
					execCmd = args[1:]
				}
			}

			logger := zerolog.New(zerolog.ConsoleWriter{Out: cmd.ErrOrStderr()}).With().Timestamp().Logger()

			docker, err := devcontainer.NewDockerClient()
			if err != nil {
				return errors.WrapWithDetails(err, "connecting to Docker")
			}
			defer func() { _ = docker.Close() }()

			mgr := &devcontainer.Manager{Docker: docker, Logger: logger}

			absDir, _ := filepath.Abs(projectDir)
			meta, err := mgr.ResolveContainer(absDir)
			if err != nil {
				return errors.WrapWithDetails(err, "no devcontainer found", "project", projectDir)
			}

			execUser := user
			if execUser == "" {
				execUser = meta.RemoteUser
			}

			exitCode, err := mgr.Exec(cmd.Context(), meta.ContainerID, execCmd, execUser, nil, cmd.OutOrStdout(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			if exitCode != 0 {
				os.Exit(exitCode)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&user, "user", "", "User to execute as (default: container's remoteUser)")
	return cmd
}
