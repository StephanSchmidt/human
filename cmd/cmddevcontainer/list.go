package cmddevcontainer

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/errors"
	"github.com/StephanSchmidt/human/internal/agent"
	"github.com/StephanSchmidt/human/internal/devcontainer"
)

func buildListCmd() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List managed devcontainers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := zerolog.New(zerolog.ConsoleWriter{Out: cmd.ErrOrStderr()}).With().Timestamp().Logger()

			docker, err := devcontainer.NewDockerClient()
			if err != nil {
				return errors.WrapWithDetails(err, "connecting to Docker")
			}
			defer func() { _ = docker.Close() }()

			mgr := &devcontainer.Manager{Docker: docker, Logger: logger}
			metas, err := mgr.List(cmd.Context())
			if err != nil {
				return err
			}

			if jsonOut {
				data, _ := json.MarshalIndent(metas, "", "  ")
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(data))
				return nil
			}

			if len(metas) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No managed devcontainers")
				return nil
			}

			out := cmd.OutOrStdout()
			tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "NAME\tSTATUS\tPROJECT\tIMAGE\tAGE")
			for _, m := range metas {
				age := agent.FormatDuration(time.Since(m.CreatedAt))
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
					m.Name, m.Status, m.ProjectDir, m.ImageName, age)
			}
			return tw.Flush()
		},
	}

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")
	return cmd
}
