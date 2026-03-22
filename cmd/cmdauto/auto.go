package cmdauto

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/cmd/cmdprovider"
	"github.com/StephanSchmidt/human/cmd/cmdutil"
	"github.com/StephanSchmidt/human/internal/tracker"
)

// BuildAutoGetCmd creates the top-level "get" command that auto-detects the tracker.
func BuildAutoGetCmd(deps cmdutil.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get KEY",
		Short: "Get an issue (auto-detect tracker)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			p, kind, cleanup, err := cmdutil.ResolveAutoProvider(cmd.Context(), cmd, key, true, deps)
			if err != nil {
				return err
			}
			defer cleanup()

			if err := cmdprovider.RunGetIssue(cmd.Context(), p, cmd.OutOrStdout(), key); err != nil {
				return err
			}

			project := tracker.ExtractProject(key)
			PrintAutoHints(cmd.ErrOrStderr(), kind, key, project, "get")
			return nil
		},
	}
}

// BuildAutoListCmd creates the top-level "list" command that auto-detects the tracker.
func BuildAutoListCmd(deps cmdutil.Deps) *cobra.Command {
	var project string
	var all, table bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List issues (auto-detect tracker)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, kind, cleanup, err := cmdutil.ResolveAutoProvider(cmd.Context(), cmd, project, false, deps)
			if err != nil {
				return err
			}
			defer cleanup()

			if err := cmdprovider.RunListIssues(cmd.Context(), p, cmd.OutOrStdout(), project, all, table); err != nil {
				return err
			}

			PrintAutoHints(cmd.ErrOrStderr(), kind, "", project, "list")
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Project key (Jira: KAN, GitHub: owner/repo, GitLab: group/project, Linear: ENG)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().BoolVar(&all, "all", false, "Include all issues (default: open only)")
	cmd.Flags().BoolVar(&table, "table", false, "Output as human-readable table instead of JSON")
	return cmd
}

// BuildAutoStatusesCmd creates the top-level "statuses" command that auto-detects the tracker.
func BuildAutoStatusesCmd(deps cmdutil.Deps) *cobra.Command {
	var table bool

	cmd := &cobra.Command{
		Use:   "statuses KEY",
		Short: "List available statuses for an issue (auto-detect tracker)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			p, kind, cleanup, err := cmdutil.ResolveAutoProvider(cmd.Context(), cmd, key, true, deps)
			if err != nil {
				return err
			}
			defer cleanup()

			if err := cmdprovider.RunListStatuses(cmd.Context(), p, cmd.OutOrStdout(), key, table); err != nil {
				return err
			}

			project := tracker.ExtractProject(key)
			PrintAutoHints(cmd.ErrOrStderr(), kind, key, project, "statuses")
			return nil
		},
	}
	cmd.Flags().BoolVar(&table, "table", false, "Output as human-readable table instead of JSON")
	return cmd
}

// BuildAutoStatusCmd creates the top-level "status" command that auto-detects the tracker.
func BuildAutoStatusCmd(deps cmdutil.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "status KEY STATUS",
		Short: "Set the status of an issue (auto-detect tracker)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			p, kind, cleanup, err := cmdutil.ResolveAutoProvider(cmd.Context(), cmd, key, true, deps)
			if err != nil {
				return err
			}
			defer cleanup()

			if err := cmdprovider.RunSetStatus(cmd.Context(), p, cmd.OutOrStdout(), key, args[1]); err != nil {
				return err
			}

			project := tracker.ExtractProject(key)
			PrintAutoHints(cmd.ErrOrStderr(), kind, key, project, "status")
			return nil
		},
	}
}

// PrintAutoHints prints contextual guidance to stderr after auto-detected commands.
func PrintAutoHints(w io.Writer, kind, key, project, afterCmd string) {
	_, _ = fmt.Fprintf(w, "\nDetected tracker: %s\n", kind)
	_, _ = fmt.Fprintln(w, "Related commands:")

	switch afterCmd {
	case "get":
		if project != "" {
			_, _ = fmt.Fprintf(w, "  human %s issues list --project=%s\n", kind, project)
		}
		if key != "" {
			_, _ = fmt.Fprintf(w, "  human %s issue  comment add %s 'text'\n", kind, key)
			_, _ = fmt.Fprintf(w, "  human %s issue  statuses %s\n", kind, key)
		}
	case "list":
		_, _ = fmt.Fprintf(w, "  human %s issue  get <KEY>\n", kind)
		if project != "" {
			_, _ = fmt.Fprintf(w, "  human %s issue  create --project=%s \"Title\" --description \"Description\"\n", kind, project)
		}
	case "statuses":
		if key != "" {
			_, _ = fmt.Fprintf(w, "  human %s issue  status %s \"<STATUS>\"\n", kind, key)
		}
	case "status":
		if key != "" {
			_, _ = fmt.Fprintf(w, "  human %s issue  statuses %s\n", kind, key)
		}
	}
}
