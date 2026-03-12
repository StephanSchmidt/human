package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/stephanschmidt/human/internal/tracker"
)

// autoInstanceLoader loads tracker instances for auto-detect commands.
// It defaults to loadAllInstances(".") and can be overridden in tests.
var autoInstanceLoader = func() ([]tracker.Instance, error) {
	return loadAllInstances(".")
}

// resolveAutoProvider loads all instances, applies flag overrides, and resolves
// the provider without requiring a fixed kind. It uses tracker.Resolve for
// auto-detection and falls back to FindTracker + ResolveByKind for ambiguous
// get commands.
func resolveAutoProvider(ctx context.Context, cmd *cobra.Command, keyHint string, allowFindFallback bool) (tracker.Provider, string, func(), error) {
	instances, err := autoInstanceLoader()
	if err != nil {
		return nil, "", nil, err
	}

	if inst := instanceFromFlags(cmd); inst != nil {
		instances = append(instances, *inst)
	}

	trackerName, _ := cmd.Root().PersistentFlags().GetString("tracker")

	// Try Resolve first (name-based or auto-detect).
	instance, err := tracker.Resolve(trackerName, instances, keyHint)
	if err != nil && allowFindFallback && trackerName == "" {
		// Ambiguous — fall back to FindTracker for get commands.
		result, findErr := tracker.FindTracker(ctx, keyHint, instances)
		if findErr != nil {
			// Return the original Resolve error — it's more informative.
			return nil, "", nil, err
		}
		instance, err = tracker.ResolveByKind(result.Provider, instances, "")
		if err != nil {
			return nil, "", nil, err
		}
	} else if err != nil {
		return nil, "", nil, err
	}

	auditPath := auditLogPath()
	ap, auditErr := tracker.NewAuditProvider(instance.Provider, instance.Name, instance.Kind, auditPath)
	if auditErr != nil {
		fmt.Fprintln(os.Stderr, "warning: audit logging disabled:", auditErr)
		return instance.Provider, instance.Kind, func() {}, nil
	}
	return ap, instance.Kind, func() { _ = ap.Close() }, nil
}

// buildAutoGetCmd creates the top-level "get" command that auto-detects the tracker.
func buildAutoGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get KEY",
		Short: "Get an issue (auto-detect tracker)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			p, kind, cleanup, err := resolveAutoProvider(cmd.Context(), cmd, key, true)
			if err != nil {
				return err
			}
			defer cleanup()

			if err := runGetIssue(cmd.Context(), p, cmd.OutOrStdout(), key); err != nil {
				return err
			}

			project := tracker.ExtractProject(key)
			printAutoHints(cmd.ErrOrStderr(), kind, key, project, "get")
			return nil
		},
	}
}

// buildAutoListCmd creates the top-level "list" command that auto-detects the tracker.
func buildAutoListCmd() *cobra.Command {
	var project string
	var all, table bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List issues (auto-detect tracker)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, kind, cleanup, err := resolveAutoProvider(cmd.Context(), cmd, project, false)
			if err != nil {
				return err
			}
			defer cleanup()

			if err := runListIssues(cmd.Context(), p, cmd.OutOrStdout(), project, all, table); err != nil {
				return err
			}

			printAutoHints(cmd.ErrOrStderr(), kind, "", project, "list")
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Project key (Jira: KAN, GitHub: owner/repo, GitLab: group/project, Linear: ENG)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().BoolVar(&all, "all", false, "Include all issues (default: open only)")
	cmd.Flags().BoolVar(&table, "table", false, "Output as human-readable table instead of JSON")
	return cmd
}

// printAutoHints prints contextual guidance to stderr after auto-detected commands.
func printAutoHints(w io.Writer, kind, key, project, afterCmd string) {
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

// buildAutoStatusesCmd creates the top-level "statuses" command that auto-detects the tracker.
func buildAutoStatusesCmd() *cobra.Command {
	var table bool

	cmd := &cobra.Command{
		Use:   "statuses KEY",
		Short: "List available statuses for an issue (auto-detect tracker)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			p, kind, cleanup, err := resolveAutoProvider(cmd.Context(), cmd, key, true)
			if err != nil {
				return err
			}
			defer cleanup()

			if err := runListStatuses(cmd.Context(), p, cmd.OutOrStdout(), key, table); err != nil {
				return err
			}

			project := tracker.ExtractProject(key)
			printAutoHints(cmd.ErrOrStderr(), kind, key, project, "statuses")
			return nil
		},
	}
	cmd.Flags().BoolVar(&table, "table", false, "Output as human-readable table instead of JSON")
	return cmd
}

// buildAutoStatusCmd creates the top-level "status" command that auto-detects the tracker.
func buildAutoStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status KEY STATUS",
		Short: "Set the status of an issue (auto-detect tracker)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			p, kind, cleanup, err := resolveAutoProvider(cmd.Context(), cmd, key, true)
			if err != nil {
				return err
			}
			defer cleanup()

			if err := runSetStatus(cmd.Context(), p, cmd.OutOrStdout(), key, args[1]); err != nil {
				return err
			}

			project := tracker.ExtractProject(key)
			printAutoHints(cmd.ErrOrStderr(), kind, key, project, "status")
			return nil
		},
	}
}
