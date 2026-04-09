package cmddevcontainer

import (
	"fmt"
	"path/filepath"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/errors"
	"github.com/StephanSchmidt/human/internal/devcontainer"
)

func buildStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop [project-dir]",
		Short: "Stop a running devcontainer",
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

			if err := mgr.Stop(cmd.Context(), meta.Name); err != nil {
				return err
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Devcontainer stopped: %s\n", meta.ContainerName)
			return nil
		},
	}
}
