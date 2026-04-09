package cmddevcontainer

import (
	"io"
	"path/filepath"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/errors"
	"github.com/StephanSchmidt/human/internal/devcontainer"
)

func buildLogsCmd() *cobra.Command {
	var follow bool
	var tail string

	cmd := &cobra.Command{
		Use:   "logs [project-dir]",
		Short: "Show devcontainer logs",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectDir := "."
			if len(args) > 0 {
				projectDir = args[0]
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

			reader, err := docker.ContainerLogs(cmd.Context(), meta.ContainerID, devcontainer.LogsOptions{
				Follow:     follow,
				Tail:       tail,
				ShowStdout: true,
				ShowStderr: true,
			})
			if err != nil {
				return errors.WrapWithDetails(err, "fetching logs")
			}
			defer func() { _ = reader.Close() }()

			_, _ = io.Copy(cmd.OutOrStdout(), reader)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	cmd.Flags().StringVar(&tail, "tail", "100", "Number of lines from end")
	return cmd
}
