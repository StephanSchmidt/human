package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"crypto/rand"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/stephanschmidt/human/errors"
	"github.com/stephanschmidt/human/internal/tracker"
)

// resolveProvider loads instances, applies CLI flag overrides, and resolves
// the provider for the given kind using the tracker name from persistent flags.
func resolveProvider(cmd *cobra.Command, kind string) (tracker.Provider, func(), error) {
	instances, err := loadAllInstances(".")
	if err != nil {
		return nil, nil, err
	}

	if inst := instanceFromFlags(cmd); inst != nil {
		instances = append(instances, *inst)
	}

	trackerName, _ := cmd.Root().PersistentFlags().GetString("tracker")

	instance, err := tracker.ResolveByKind(kind, instances, trackerName)
	if err != nil {
		return nil, nil, err
	}

	auditPath := auditLogPath()
	ap, auditErr := tracker.NewAuditProvider(instance.Provider, instance.Name, instance.Kind, auditPath)
	if auditErr != nil {
		fmt.Fprintln(os.Stderr, "warning: audit logging disabled:", auditErr)
		return instance.Provider, func() {}, nil
	}
	return ap, func() { _ = ap.Close() }, nil
}

// buildProviderCommands returns the "issues" and "issue" cobra commands
// that use the given provider kind for resolution.
func buildProviderCommands(kind string) []*cobra.Command {
	issuesCmd := &cobra.Command{
		Use:   "issues",
		Short: "Bulk issue operations",
	}
	issuesCmd.AddCommand(buildIssuesListCmd(kind))

	issueCmd := &cobra.Command{
		Use:   "issue",
		Short: "Single issue operations",
	}
	issueCmd.AddCommand(buildIssueGetCmd(kind))
	issueCmd.AddCommand(buildIssueCreateCmd(kind))
	issueCmd.AddCommand(buildIssueEditCmd(kind))
	issueCmd.AddCommand(buildIssueDeleteCmd(kind))
	issueCmd.AddCommand(buildIssueCommentCmd(kind))
	issueCmd.AddCommand(buildIssueStartCmd(kind))
	issueCmd.AddCommand(buildIssueStatusesCmd(kind))
	issueCmd.AddCommand(buildIssueStatusSetCmd(kind))

	return []*cobra.Command{issuesCmd, issueCmd}
}

func buildIssuesListCmd(kind string) *cobra.Command {
	var project string
	var all, table bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List project issues (JSON)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, cleanup, err := resolveProvider(cmd, kind)
			if err != nil {
				return err
			}
			defer cleanup()
			return runListIssues(cmd.Context(), p, cmd.OutOrStdout(), project, all, table)
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Project key (Jira: KAN, GitHub: owner/repo, GitLab: group/project, Linear: ENG)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().BoolVar(&all, "all", false, "Include all issues (default: open only)")
	cmd.Flags().BoolVar(&table, "table", false, "Output as human-readable table instead of JSON")
	return cmd
}

func buildIssueGetCmd(kind string) *cobra.Command {
	return &cobra.Command{
		Use:   "get KEY",
		Short: "Get a single issue with metadata and description as markdown",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, cleanup, err := resolveProvider(cmd, kind)
			if err != nil {
				return err
			}
			defer cleanup()
			return runGetIssue(cmd.Context(), p, cmd.OutOrStdout(), args[0])
		},
	}
}

func buildIssueCreateCmd(kind string) *cobra.Command {
	var project, typ, description string

	cmd := &cobra.Command{
		Use:     "create TITLE",
		Short:   "Create a new issue in a project",
		Example: `  human jira issue create --project=KAN "Implement login page" --description "Add OAuth2 login flow with Google provider"`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, cleanup, err := resolveProvider(cmd, kind)
			if err != nil {
				return err
			}
			defer cleanup()
			return runCreateIssue(cmd.Context(), p, cmd.OutOrStdout(), project, typ, args[0], description)
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Project key (Jira: KAN, GitHub: owner/repo, GitLab: group/project, Linear: ENG)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().StringVar(&typ, "type", "Task", "Issue type (Jira only, e.g. Task, Bug, Story)")
	cmd.Flags().StringVar(&description, "description", "", "Issue description in markdown (separate from title)")
	return cmd
}

func buildIssueEditCmd(kind string) *cobra.Command {
	var title, description string

	cmd := &cobra.Command{
		Use:     "edit KEY",
		Short:   "Edit an issue's title and/or description",
		Example: `  human jira issue edit KAN-1 --title "New title" --description "Updated description"`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("title") && !cmd.Flags().Changed("description") {
				return errors.WithDetails("at least one of --title or --description is required")
			}
			p, cleanup, err := resolveProvider(cmd, kind)
			if err != nil {
				return err
			}
			defer cleanup()

			var opts tracker.EditOptions
			if cmd.Flags().Changed("title") {
				opts.Title = &title
			}
			if cmd.Flags().Changed("description") {
				opts.Description = &description
			}

			return runEditIssue(cmd.Context(), p, cmd.OutOrStdout(), args[0], opts)
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "New issue title")
	cmd.Flags().StringVar(&description, "description", "", "New issue description (markdown)")
	return cmd
}

func buildIssueDeleteCmd(kind string) *cobra.Command {
	var confirm int

	cmd := &cobra.Command{
		Use:   "delete KEY",
		Short: "Delete (or close) an issue by key (requires --confirm)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, cleanup, err := resolveProvider(cmd, kind)
			if err != nil {
				return err
			}
			defer cleanup()
			return runDeleteIssue(cmd.Context(), p, cmd.OutOrStdout(), args[0], confirm)
		},
	}
	cmd.Flags().IntVar(&confirm, "confirm", 0, "Confirmation code from the first invocation")
	return cmd
}

func buildIssueCommentCmd(kind string) *cobra.Command {
	commentCmd := &cobra.Command{
		Use:   "comment",
		Short: "Comment operations on an issue",
	}

	addCmd := &cobra.Command{
		Use:   "add KEY BODY",
		Short: "Add a comment to an issue",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, cleanup, err := resolveProvider(cmd, kind)
			if err != nil {
				return err
			}
			defer cleanup()
			return runAddComment(cmd.Context(), p, cmd.OutOrStdout(), args[0], args[1])
		},
	}

	listCmd := &cobra.Command{
		Use:   "list KEY",
		Short: "List comments on an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, cleanup, err := resolveProvider(cmd, kind)
			if err != nil {
				return err
			}
			defer cleanup()
			return runListComments(cmd.Context(), p, cmd.OutOrStdout(), args[0])
		},
	}

	commentCmd.AddCommand(addCmd, listCmd)
	return commentCmd
}

func buildIssueStartCmd(kind string) *cobra.Command {
	return &cobra.Command{
		Use:   "start KEY",
		Short: "Start working on an issue (transition to In Progress and assign to yourself)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, cleanup, err := resolveProvider(cmd, kind)
			if err != nil {
				return err
			}
			defer cleanup()
			return runStartIssue(cmd.Context(), p, cmd.OutOrStdout(), args[0])
		},
	}
}

// --- Business logic functions ---

func runListIssues(ctx context.Context, p tracker.Provider, out io.Writer, project string, all, table bool) error {
	issues, err := p.ListIssues(ctx, tracker.ListOptions{
		Project:    project,
		MaxResults: 50,
		IncludeAll: all,
	})
	if err != nil {
		return err
	}
	if table {
		return printIssuesTable(out, issues)
	}
	return printIssuesJSON(out, issues)
}

func runGetIssue(ctx context.Context, p tracker.Provider, out io.Writer, key string) error {
	issue, err := p.GetIssue(ctx, key)
	if err != nil {
		return err
	}

	displayOrNone := func(s string) string {
		if s == "" {
			return "None"
		}
		return s
	}

	_, _ = fmt.Fprintf(out, "# %s: %s\n\n", issue.Key, issue.Title)
	_, _ = fmt.Fprintln(out, "| Field    | Value       |")
	_, _ = fmt.Fprintln(out, "|----------|-------------|")
	_, _ = fmt.Fprintf(out, "| Status   | %s |\n", issue.Status)
	_, _ = fmt.Fprintf(out, "| Priority | %s |\n", displayOrNone(issue.Priority))
	_, _ = fmt.Fprintf(out, "| Assignee | %s |\n", displayOrNone(issue.Assignee))
	_, _ = fmt.Fprintf(out, "| Reporter | %s |\n", displayOrNone(issue.Reporter))

	if issue.Description != "" {
		_, _ = fmt.Fprintf(out, "\n## Description\n\n%s", issue.Description)
	}

	return nil
}

func runCreateIssue(ctx context.Context, p tracker.Provider, out io.Writer, project, typ, title, description string) error {
	issue, err := p.CreateIssue(ctx, &tracker.Issue{
		Project:     project,
		Type:        typ,
		Title:       title,
		Description: description,
	})
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "%s\t%s\n", issue.Key, issue.Title)
	return nil
}

func runDeleteIssue(ctx context.Context, p tracker.Provider, out io.Writer, key string, confirm int) error {
	if confirm == 0 {
		code, err := generateConfirmCode(key)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "Warning: This is a destructive operation. You are about to delete %s.\n", key)
		_, _ = fmt.Fprintln(out, "Sure? From a user perspective, is this the right thing?")
		_, _ = fmt.Fprintf(out, "Use --confirm=%d to confirm deletion of %s\n", code, key)
		return nil
	}

	stored, err := readConfirmCode(key)
	if err != nil {
		return err
	}
	if confirm != stored {
		return errors.WithDetails("confirmation code does not match", "key", key)
	}

	if err := p.DeleteIssue(ctx, key); err != nil {
		return err
	}
	clearConfirmCode(key)
	_, _ = fmt.Fprintf(out, "Deleted %s\n", key)
	return nil
}

// confirmPath returns the temp file path for a confirmation code.
func confirmPath(key string) string {
	return filepath.Join(os.TempDir(), "human-confirm-"+key)
}

// generateConfirmCode creates a random 4-digit code, writes it to a temp file, returns it.
func generateConfirmCode(key string) (int, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(9000))
	if err != nil {
		return 0, errors.WithDetails("generating random confirmation code", "key", key)
	}
	code := int(n.Int64()) + 1000 // 1000–9999
	path := confirmPath(key)
	if err := os.WriteFile(path, []byte(strconv.Itoa(code)), 0o600); err != nil {
		return 0, errors.WithDetails("writing confirmation file", "path", path)
	}
	return code, nil
}

// readConfirmCode reads the stored confirmation code from the temp file.
func readConfirmCode(key string) (int, error) {
	root, err := os.OpenRoot(os.TempDir())
	if err != nil {
		return 0, errors.WithDetails("opening temp directory", "key", key)
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open("human-confirm-" + key)
	if err != nil {
		return 0, errors.WithDetails("no pending confirmation", "key", key)
	}
	defer func() { _ = f.Close() }()
	data := make([]byte, 16)
	n, _ := f.Read(data)
	return strconv.Atoi(strings.TrimSpace(string(data[:n])))
}

// clearConfirmCode removes the temp file after successful deletion.
func clearConfirmCode(key string) {
	_ = os.Remove(confirmPath(key))
}

func runEditIssue(ctx context.Context, p tracker.Provider, out io.Writer, key string, opts tracker.EditOptions) error {
	issue, err := p.EditIssue(ctx, key, opts)
	if err != nil {
		return err
	}
	if issue == nil {
		return errors.WithDetails("edit returned no issue", "key", key)
	}
	_, _ = fmt.Fprintf(out, "%s\t%s\n", issue.Key, issue.Title)
	return nil
}

func runStartIssue(ctx context.Context, p tracker.Provider, out io.Writer, key string) error {
	userID, err := p.GetCurrentUser(ctx)
	if err != nil {
		return errors.WrapWithDetails(err, "getting current user")
	}

	transitionErr := p.TransitionIssue(ctx, key, "In Progress")
	assignErr := p.AssignIssue(ctx, key, userID)

	if transitionErr != nil && assignErr != nil {
		return errors.WithDetails("failed to start issue",
			"key", key,
			"transitionError", transitionErr.Error(),
			"assignError", assignErr.Error())
	}

	if transitionErr != nil {
		_, _ = fmt.Fprintf(out, "Assigned %s to %s (transition failed: %v)\n", key, userID, transitionErr)
		return nil
	}

	if assignErr != nil {
		_, _ = fmt.Fprintf(out, "Transitioned %s to In Progress (assign failed: %v)\n", key, assignErr)
		return nil
	}

	_, _ = fmt.Fprintf(out, "Started %s\n", key)
	return nil
}

func runAddComment(ctx context.Context, p tracker.Provider, out io.Writer, key, body string) error {
	comment, err := p.AddComment(ctx, key, body)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "%s\t%s\n", comment.ID, comment.Body)
	return nil
}

func runListComments(ctx context.Context, p tracker.Provider, out io.Writer, key string) error {
	comments, err := p.ListComments(ctx, key)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(comments)
}

// --- Print helpers ---

func printIssuesJSON(w io.Writer, issues []tracker.Issue) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(issues)
}

func printIssuesTable(out io.Writer, issues []tracker.Issue) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "KEY\tSTATUS\tTITLE")
	for _, issue := range issues {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", issue.Key, issue.Status, issue.Title)
	}
	return w.Flush()
}

func buildIssueStatusesCmd(kind string) *cobra.Command {
	var table bool

	cmd := &cobra.Command{
		Use:     "statuses KEY",
		Short:   "List available statuses for an issue",
		Example: `  human jira issue statuses KAN-1`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, cleanup, err := resolveProvider(cmd, kind)
			if err != nil {
				return err
			}
			defer cleanup()
			return runListStatuses(cmd.Context(), p, cmd.OutOrStdout(), args[0], table)
		},
	}
	cmd.Flags().BoolVar(&table, "table", false, "Output as human-readable table instead of JSON")
	return cmd
}

func buildIssueStatusSetCmd(kind string) *cobra.Command {
	return &cobra.Command{
		Use:     "status KEY STATUS",
		Short:   "Set the status of an issue",
		Example: `  human jira issue status KAN-1 "In Progress"`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, cleanup, err := resolveProvider(cmd, kind)
			if err != nil {
				return err
			}
			defer cleanup()
			return runSetStatus(cmd.Context(), p, cmd.OutOrStdout(), args[0], args[1])
		},
	}
}

func runListStatuses(ctx context.Context, p tracker.Provider, out io.Writer, key string, table bool) error {
	statuses, err := p.ListStatuses(ctx, key)
	if err != nil {
		return err
	}
	if table {
		return printStatusesTable(out, statuses)
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(statuses)
}

func runSetStatus(ctx context.Context, p tracker.Provider, out io.Writer, key, status string) error {
	if err := p.TransitionIssue(ctx, key, status); err != nil {
		_, _ = fmt.Fprintf(out, "Hint: run 'human <tracker> issue statuses %s' to see available statuses\n", key)
		return err
	}
	_, _ = fmt.Fprintf(out, "Transitioned %s to %s\n", key, status)
	return nil
}

func printStatusesTable(out io.Writer, statuses []tracker.Status) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tTYPE")
	for _, s := range statuses {
		typ := s.Type
		if typ == "" {
			typ = "-"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\n", s.Name, typ)
	}
	return w.Flush()
}
