package cmddevcontainer

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/errors"
	"github.com/StephanSchmidt/human/internal/devcontainer"
)

func buildStatusCmd() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "status [project-dir]",
		Short: "Show status of a devcontainer",
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
			resolved, err := mgr.ResolveContainer(absDir)
			if err != nil {
				return errors.WrapWithDetails(err, "no devcontainer found", "project", projectDir)
			}

			meta, err := mgr.Status(cmd.Context(), resolved.Name)
			if err != nil {
				return err
			}

			if jsonOut {
				data, _ := json.MarshalIndent(meta, "", "  ")
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(data))
				return nil
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "Name:       %s\n", meta.Name)
			_, _ = fmt.Fprintf(out, "Status:     %s\n", meta.Status)
			cid := meta.ContainerID
			if len(cid) > 12 {
				cid = cid[:12]
			}
			_, _ = fmt.Fprintf(out, "Container:  %s\n", cid)
			_, _ = fmt.Fprintf(out, "Image:      %s\n", meta.ImageName)
			_, _ = fmt.Fprintf(out, "Workspace:  %s\n", meta.WorkspaceDir)
			_, _ = fmt.Fprintf(out, "Project:    %s\n", meta.ProjectDir)
			if meta.DaemonAddr != "" {
				_, _ = fmt.Fprintf(out, "Daemon:     %s\n", meta.DaemonAddr)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")
	return cmd
}
