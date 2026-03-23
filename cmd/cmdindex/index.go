package cmdindex

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/cmd/cmdutil"
	"github.com/StephanSchmidt/human/internal/index"
	"github.com/StephanSchmidt/human/internal/tracker"
)

// IndexDeps holds injectable dependencies for index commands.
type IndexDeps struct {
	LoadInstances func(dir string) ([]tracker.Instance, error)
	DBPath        func() string
	NewStore      func(dbPath string) (index.Store, error)
}

// DefaultIndexDeps returns production dependencies.
func DefaultIndexDeps() IndexDeps {
	return IndexDeps{
		LoadInstances: cmdutil.LoadAllInstances,
		DBPath:        index.DefaultDBPath,
		NewStore: func(dbPath string) (index.Store, error) {
			return index.NewSQLiteStore(dbPath)
		},
	}
}

// BuildIndexCmd creates the "index" command.
func BuildIndexCmd(deps IndexDeps) *cobra.Command {
	var (
		status bool
		source string
		full   bool
	)

	cmd := &cobra.Command{
		Use:   "index",
		Short: "Build search index from configured trackers",
		Long:  "Sync issues from all configured tracker instances into a local SQLite index for fast full-text search.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if status {
				return RunIndexStatus(cmd.Context(), cmd.OutOrStdout(), deps)
			}
			return RunIndex(cmd.Context(), cmd.OutOrStdout(), source, full, deps)
		},
	}

	cmd.Flags().BoolVar(&status, "status", false, "Show index statistics instead of syncing")
	cmd.Flags().StringVar(&source, "source", "", "Only sync instances of this tracker kind (e.g. jira, linear)")
	cmd.Flags().BoolVar(&full, "full", false, "Include closed/done issues")

	return cmd
}

// BuildSearchCmd creates the "search" command.
func BuildSearchCmd(deps IndexDeps) *cobra.Command {
	var (
		limit    int
		jsonOut  bool
		tableOut bool
	)

	cmd := &cobra.Command{
		Use:   "search QUERY",
		Short: "Search the local issue index",
		Long:  "Full-text search across all indexed tracker issues. Run 'human index' first to build the index.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunSearch(cmd.Context(), cmd.OutOrStdout(), args[0], limit, jsonOut, tableOut, deps)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of results")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&tableOut, "table", false, "Output as table")

	return cmd
}

// RunIndex loads instances, opens the store, and syncs.
func RunIndex(ctx context.Context, out io.Writer, source string, full bool, deps IndexDeps) error {
	instances, err := deps.LoadInstances(".")
	if err != nil {
		return err
	}

	cmdutil.WarnSkippedTrackers(out, ".", instances)

	if len(instances) == 0 {
		_, _ = fmt.Fprintln(out, "No trackers connected. Run 'human init' or set credentials (see above).")
		return nil
	}

	if source != "" {
		var filtered []tracker.Instance
		for _, inst := range instances {
			if inst.Kind == source {
				filtered = append(filtered, inst)
			}
		}
		instances = filtered
	}

	store, err := deps.NewStore(deps.DBPath())
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	result, err := index.Sync(ctx, store, instances, full, out)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(out, "\nDone: %d indexed, %d pruned, %d errors\n", result.Indexed, result.Pruned, result.Errors)
	return nil
}

// RunSearch opens the store and searches.
func RunSearch(ctx context.Context, out io.Writer, query string, limit int, jsonOut, tableOut bool, deps IndexDeps) error {
	store, err := deps.NewStore(deps.DBPath())
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	entries, err := store.Search(ctx, query, limit)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		_, _ = fmt.Fprintln(out, "No results found. Run 'human index' to build or update the index.")
		return nil
	}

	if jsonOut {
		return cmdutil.PrintJSON(out, entries)
	}

	if tableOut {
		return printSearchTable(out, entries)
	}

	return printSearchDefault(out, entries)
}

// RunIndexStatus shows index statistics.
func RunIndexStatus(ctx context.Context, out io.Writer, deps IndexDeps) error {
	store, err := deps.NewStore(deps.DBPath())
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	stats, err := store.Stats(ctx)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(out, "Total entries: %d\n", stats.TotalEntries)
	if !stats.LastIndexedAt.IsZero() {
		_, _ = fmt.Fprintf(out, "Last indexed:  %s\n", stats.LastIndexedAt.Format("2006-01-02 15:04:05"))
	}

	if len(stats.ByKind) > 0 {
		_, _ = fmt.Fprintln(out, "\nBy tracker:")
		for kind, count := range stats.ByKind {
			_, _ = fmt.Fprintf(out, "  %-12s %d\n", kind, count)
		}
	}

	if len(stats.BySource) > 0 {
		_, _ = fmt.Fprintln(out, "\nBy source:")
		for source, count := range stats.BySource {
			_, _ = fmt.Fprintf(out, "  %-12s %d\n", source, count)
		}
	}

	return nil
}

// printSearchDefault prints results in the agent-friendly default format.
func printSearchDefault(out io.Writer, entries []index.Entry) error {
	for _, e := range entries {
		_, _ = fmt.Fprintf(out, "%s: %s\n", e.Key, e.Title)
		meta := fmt.Sprintf("  %s", e.Kind)
		if e.Source != "" {
			meta += " \u00b7 " + e.Source
		}
		if e.Status != "" {
			meta += " \u00b7 " + e.Status
		}
		if e.Assignee != "" {
			meta += " \u00b7 @" + e.Assignee
		}
		_, _ = fmt.Fprintln(out, meta)
		if e.Source != "" {
			_, _ = fmt.Fprintf(out, "  \u2192 human get %s --tracker=%s\n", e.Key, e.Source)
		} else {
			_, _ = fmt.Fprintf(out, "  \u2192 human get %s\n", e.Key)
		}
	}
	return nil
}

// printSearchTable prints results as a formatted table.
func printSearchTable(out io.Writer, entries []index.Entry) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "KEY\tTITLE\tKIND\tSOURCE\tSTATUS\tASSIGNEE")
	for _, e := range entries {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", e.Key, e.Title, e.Kind, e.Source, e.Status, e.Assignee)
	}
	return w.Flush()
}
