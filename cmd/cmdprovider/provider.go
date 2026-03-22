package cmdprovider

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/cmd/cmdutil"
	"github.com/StephanSchmidt/human/errors"
	"github.com/StephanSchmidt/human/internal/tracker"
)

// BuildProviderCommands returns the "issues" and "issue" cobra commands
// that use the given provider kind for resolution.
func BuildProviderCommands(kind string, deps cmdutil.Deps) []*cobra.Command {
	issuesCmd := &cobra.Command{
		Use:   "issues",
		Short: "Bulk issue operations",
	}
	issuesCmd.AddCommand(buildIssuesListCmd(kind, deps))

	issueCmd := &cobra.Command{
		Use:   "issue",
		Short: "Single issue operations",
	}
	issueCmd.AddCommand(buildIssueGetCmd(kind, deps))
	issueCmd.AddCommand(buildIssueCreateCmd(kind, deps))
	issueCmd.AddCommand(buildIssueEditCmd(kind, deps))
	issueCmd.AddCommand(buildIssueDeleteCmd(kind, deps))
	issueCmd.AddCommand(buildIssueCommentCmd(kind, deps))
	issueCmd.AddCommand(buildIssueStartCmd(kind, deps))
	issueCmd.AddCommand(buildIssueStatusesCmd(kind, deps))
	issueCmd.AddCommand(buildIssueStatusSetCmd(kind, deps))

	return []*cobra.Command{issuesCmd, issueCmd}
}

func buildIssuesListCmd(kind string, deps cmdutil.Deps) *cobra.Command {
	var project string
	var all, table bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List project issues (JSON)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, cleanup, err := cmdutil.ResolveProvider(cmd, kind, deps)
			if err != nil {
				return err
			}
			defer cleanup()
			return RunListIssues(cmd.Context(), p, cmd.OutOrStdout(), project, all, table)
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Project key (Jira: KAN, GitHub: owner/repo, GitLab: group/project, Linear: ENG)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().BoolVar(&all, "all", false, "Include all issues (default: open only)")
	cmd.Flags().BoolVar(&table, "table", false, "Output as human-readable table instead of JSON")
	return cmd
}

func buildIssueGetCmd(kind string, deps cmdutil.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get KEY",
		Short: "Get a single issue with metadata and description as markdown",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, cleanup, err := cmdutil.ResolveProvider(cmd, kind, deps)
			if err != nil {
				return err
			}
			defer cleanup()
			return RunGetIssue(cmd.Context(), p, cmd.OutOrStdout(), args[0])
		},
	}
}

func buildIssueCreateCmd(kind string, deps cmdutil.Deps) *cobra.Command {
	var project, typ, description string

	cmd := &cobra.Command{
		Use:     "create TITLE",
		Short:   "Create a new issue in a project",
		Example: `  human jira issue create --project=KAN "Implement login page" --description "Add OAuth2 login flow with Google provider"`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, cleanup, err := cmdutil.ResolveProvider(cmd, kind, deps)
			if err != nil {
				return err
			}
			defer cleanup()
			return RunCreateIssue(cmd.Context(), p, cmd.OutOrStdout(), project, typ, args[0], description)
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Project key (Jira: KAN, GitHub: owner/repo, GitLab: group/project, Linear: ENG)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().StringVar(&typ, "type", "Task", "Issue type (Jira only, e.g. Task, Bug, Story)")
	cmd.Flags().StringVar(&description, "description", "", "Issue description in markdown (separate from title)")
	return cmd
}

func buildIssueEditCmd(kind string, deps cmdutil.Deps) *cobra.Command {
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
			p, cleanup, err := cmdutil.ResolveProvider(cmd, kind, deps)
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

			return RunEditIssue(cmd.Context(), p, cmd.OutOrStdout(), args[0], opts)
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "New issue title")
	cmd.Flags().StringVar(&description, "description", "", "New issue description (markdown)")
	return cmd
}

func buildIssueDeleteCmd(kind string, deps cmdutil.Deps) *cobra.Command {
	var confirm int

	cmd := &cobra.Command{
		Use:   "delete KEY",
		Short: "Delete (or close) an issue by key (requires --confirm)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, cleanup, err := cmdutil.ResolveProvider(cmd, kind, deps)
			if err != nil {
				return err
			}
			defer cleanup()
			return RunDeleteIssue(cmd.Context(), p, cmd.OutOrStdout(), args[0], confirm)
		},
	}
	cmd.Flags().IntVar(&confirm, "confirm", 0, "Confirmation code from the first invocation")
	return cmd
}

func buildIssueCommentCmd(kind string, deps cmdutil.Deps) *cobra.Command {
	commentCmd := &cobra.Command{
		Use:   "comment",
		Short: "Comment operations on an issue",
	}

	addCmd := &cobra.Command{
		Use:   "add KEY BODY",
		Short: "Add a comment to an issue",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, cleanup, err := cmdutil.ResolveProvider(cmd, kind, deps)
			if err != nil {
				return err
			}
			defer cleanup()
			return RunAddComment(cmd.Context(), p, cmd.OutOrStdout(), args[0], args[1])
		},
	}

	listCmd := &cobra.Command{
		Use:   "list KEY",
		Short: "List comments on an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, cleanup, err := cmdutil.ResolveProvider(cmd, kind, deps)
			if err != nil {
				return err
			}
			defer cleanup()
			return RunListComments(cmd.Context(), p, cmd.OutOrStdout(), args[0])
		},
	}

	commentCmd.AddCommand(addCmd, listCmd)
	return commentCmd
}

func buildIssueStartCmd(kind string, deps cmdutil.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "start KEY",
		Short: "Start working on an issue (transition to In Progress and assign to yourself)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, cleanup, err := cmdutil.ResolveProvider(cmd, kind, deps)
			if err != nil {
				return err
			}
			defer cleanup()
			return RunStartIssue(cmd.Context(), p, cmd.OutOrStdout(), args[0])
		},
	}
}

func buildIssueStatusesCmd(kind string, deps cmdutil.Deps) *cobra.Command {
	var table bool

	cmd := &cobra.Command{
		Use:     "statuses KEY",
		Short:   "List available statuses for an issue",
		Example: `  human jira issue statuses KAN-1`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, cleanup, err := cmdutil.ResolveProvider(cmd, kind, deps)
			if err != nil {
				return err
			}
			defer cleanup()
			return RunListStatuses(cmd.Context(), p, cmd.OutOrStdout(), args[0], table)
		},
	}
	cmd.Flags().BoolVar(&table, "table", false, "Output as human-readable table instead of JSON")
	return cmd
}

func buildIssueStatusSetCmd(kind string, deps cmdutil.Deps) *cobra.Command {
	return &cobra.Command{
		Use:     "status KEY STATUS",
		Short:   "Set the status of an issue",
		Example: `  human jira issue status KAN-1 "In Progress"`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, cleanup, err := cmdutil.ResolveProvider(cmd, kind, deps)
			if err != nil {
				return err
			}
			defer cleanup()
			return RunSetStatus(cmd.Context(), p, cmd.OutOrStdout(), args[0], args[1])
		},
	}
}

// --- Business logic functions (exported for use by cmdauto) ---

// RunListIssues lists issues for a project.
func RunListIssues(ctx context.Context, p tracker.Provider, out io.Writer, project string, all, table bool) error {
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

// RunGetIssue retrieves and prints a single issue.
func RunGetIssue(ctx context.Context, p tracker.Provider, out io.Writer, key string) error {
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

// RunCreateIssue creates a new issue.
func RunCreateIssue(ctx context.Context, p tracker.Provider, out io.Writer, project, typ, title, description string) error {
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

// RunDeleteIssue deletes an issue after confirmation.
func RunDeleteIssue(ctx context.Context, p tracker.Provider, out io.Writer, key string, confirm int) error {
	if confirm == 0 {
		code, err := GenerateConfirmCode(key)
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
	ClearConfirmCode(key)
	_, _ = fmt.Fprintf(out, "Deleted %s\n", key)
	return nil
}

// RunEditIssue edits an issue's title and/or description.
func RunEditIssue(ctx context.Context, p tracker.Provider, out io.Writer, key string, opts tracker.EditOptions) error {
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

// RunStartIssue transitions an issue and assigns to the current user.
func RunStartIssue(ctx context.Context, p tracker.Provider, out io.Writer, key string) error {
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

// RunAddComment adds a comment to an issue.
func RunAddComment(ctx context.Context, p tracker.Provider, out io.Writer, key, body string) error {
	comment, err := p.AddComment(ctx, key, body)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "%s\t%s\n", comment.ID, comment.Body)
	return nil
}

// RunListComments lists comments on an issue.
func RunListComments(ctx context.Context, p tracker.Provider, out io.Writer, key string) error {
	comments, err := p.ListComments(ctx, key)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(comments)
}

// RunListStatuses lists available statuses for an issue.
func RunListStatuses(ctx context.Context, p tracker.Provider, out io.Writer, key string, table bool) error {
	statuses, err := p.ListStatuses(ctx, key)
	if err != nil {
		return err
	}
	if table {
		return PrintStatusesTable(out, statuses)
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(statuses)
}

// RunSetStatus sets an issue's status.
func RunSetStatus(ctx context.Context, p tracker.Provider, out io.Writer, key, status string) error {
	if err := p.TransitionIssue(ctx, key, status); err != nil {
		_, _ = fmt.Fprintf(out, "Hint: run 'human <tracker> issue statuses %s' to see available statuses\n", key)
		return err
	}
	_, _ = fmt.Fprintf(out, "Transitioned %s to %s\n", key, status)
	return nil
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

// PrintStatusesTable prints statuses as a table.
func PrintStatusesTable(out io.Writer, statuses []tracker.Status) error {
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

// --- Confirmation code helpers ---

// confirmDir returns a per-user directory for confirmation code files,
// creating it if necessary. Uses the user cache directory instead of
// the shared /tmp to avoid TOCTOU and symlink attacks on multi-user systems.
func confirmDir() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", errors.WrapWithDetails(err, "resolving user cache directory")
	}
	dir := filepath.Join(cacheDir, "human", "confirm")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", errors.WrapWithDetails(err, "creating confirm directory", "path", dir)
	}
	return dir, nil
}

// ConfirmPath returns the file path for a confirmation code.
func ConfirmPath(key string) string {
	dir, err := confirmDir()
	if err != nil {
		// Fall back to temp dir if cache dir is unavailable.
		return filepath.Join(os.TempDir(), "human-confirm-"+key)
	}
	return filepath.Join(dir, "human-confirm-"+key)
}

// GenerateConfirmCode creates a random 6-digit code, writes it to a file, returns it.
func GenerateConfirmCode(key string) (int, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return 0, errors.WithDetails("generating random confirmation code", "key", key)
	}
	code := int(n.Int64()) + 100000 // 100000–999999
	dir, err := confirmDir()
	if err != nil {
		return 0, err
	}
	path := filepath.Join(dir, "human-confirm-"+key)
	if err := os.WriteFile(path, []byte(strconv.Itoa(code)), 0o600); err != nil {
		return 0, errors.WithDetails("writing confirmation file", "path", path)
	}
	return code, nil
}

// readConfirmCode reads the stored confirmation code from the file.
func readConfirmCode(key string) (int, error) {
	dir, err := confirmDir()
	if err != nil {
		return 0, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return 0, errors.WithDetails("opening confirm directory", "key", key)
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

// ClearConfirmCode removes the temp file after successful deletion.
func ClearConfirmCode(key string) {
	_ = os.Remove(ConfirmPath(key))
}
