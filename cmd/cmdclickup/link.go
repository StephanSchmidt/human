package cmdclickup

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/cmd/cmdutil"
	"github.com/StephanSchmidt/human/errors"
	"github.com/StephanSchmidt/human/internal/clickup"
	"github.com/StephanSchmidt/human/internal/githelper"
)

// linkHeader is the marker for the GitHub links section in task descriptions.
const linkHeader = "**GitHub** _(human)_"

// gitHelper abstracts git/gh operations for testability.
type gitHelper interface {
	CurrentBranch(ctx context.Context) (string, error)
	GetPRInfo(ctx context.Context, number int, repo string) (*githelper.PRInfo, error)
	GetCommitInfo(ctx context.Context, sha string) (*githelper.CommitInfo, error)
	GetRepoSlug(ctx context.Context) (string, error)
}

func buildLinkCmd(deps cmdutil.Deps, gh gitHelper) *cobra.Command {
	linkCmd := &cobra.Command{
		Use:   "link",
		Short: "Link GitHub artifacts to ClickUp tasks",
	}
	linkCmd.AddCommand(buildLinkPRCmd(deps, gh))
	linkCmd.AddCommand(buildLinkCommitCmd(deps, gh))
	return linkCmd
}

func buildLinkPRCmd(deps cmdutil.Deps, gh gitHelper) *cobra.Command {
	var taskID, repo string

	cmd := &cobra.Command{
		Use:   "pr [NUMBER]",
		Short: "Link a GitHub PR to a ClickUp task",
		Long: `Links a GitHub pull request to a ClickUp task by adding an entry to the task description.

The task ID is auto-detected from the current git branch name (e.g. CU-abc123-fix
or PROJ-42-feature). Use --task to override.

The PR number defaults to the PR for the current branch. The repo defaults to the
current repository. Running the command again updates the existing entry.`,
		Example: `  human clickup link pr
  human clickup link pr 42
  human clickup link pr --task abc123 --repo owner/repo 42`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := resolveClickUpClient(cmd, deps)
			if err != nil {
				return err
			}
			var prNumber int
			if len(args) > 0 {
				prNumber, err = strconv.Atoi(args[0])
				if err != nil {
					return errors.WithDetails("PR number must be an integer", "input", args[0])
				}
			}
			return runLinkPR(cmd.Context(), client, gh, cmd.OutOrStdout(), taskID, repo, prNumber)
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "ClickUp task ID (auto-detected from branch)")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repo in owner/repo format (auto-detected)")
	return cmd
}

func buildLinkCommitCmd(deps cmdutil.Deps, gh gitHelper) *cobra.Command {
	var taskID, repo string

	cmd := &cobra.Command{
		Use:   "commit [SHA]",
		Short: "Link a git commit to a ClickUp task",
		Long: `Links a git commit to a ClickUp task by adding an entry to the task description.

The task ID is auto-detected from the current git branch name. Use --task to override.
The commit SHA defaults to HEAD. Running the command again updates the existing entry.`,
		Example: `  human clickup link commit
  human clickup link commit abc1234
  human clickup link commit --task abc123 abc1234`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := resolveClickUpClient(cmd, deps)
			if err != nil {
				return err
			}
			sha := ""
			if len(args) > 0 {
				sha = args[0]
			}
			return runLinkCommit(cmd.Context(), client, gh, cmd.OutOrStdout(), taskID, repo, sha)
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "ClickUp task ID (auto-detected from branch)")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repo in owner/repo format (auto-detected)")
	return cmd
}

// --- Business logic ---

func runLinkPR(ctx context.Context, client *clickup.Client, gh gitHelper, out io.Writer, taskID, repo string, prNumber int) error {
	taskID, err := resolveTaskID(ctx, gh, taskID)
	if err != nil {
		return err
	}

	pr, err := gh.GetPRInfo(ctx, prNumber, repo)
	if err != nil {
		return err
	}

	// Build the link entry: [owner/repo#42 — Title](url)
	entry := fmt.Sprintf("[%s#%d — %s](%s)", repoFromURL(pr.URL), pr.Number, pr.Title, pr.URL)

	if err := upsertLinkEntry(ctx, client, taskID, entry); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(out, "Linked PR #%d to task %s\n", pr.Number, taskID)
	return nil
}

func runLinkCommit(ctx context.Context, client *clickup.Client, gh gitHelper, out io.Writer, taskID, repo, sha string) error {
	taskID, err := resolveTaskID(ctx, gh, taskID)
	if err != nil {
		return err
	}

	commit, err := gh.GetCommitInfo(ctx, sha)
	if err != nil {
		return err
	}

	if repo == "" {
		repo, err = gh.GetRepoSlug(ctx)
		if err != nil {
			return err
		}
	}

	commitURL := githelper.CommitURL(repo, commit.SHA)
	shortSHA := commit.SHA
	if len(shortSHA) > 7 {
		shortSHA = shortSHA[:7]
	}

	// Build the entry: Commit: [`abc1234`](url) Subject
	entry := fmt.Sprintf("Commit: [`%s`](%s) %s", shortSHA, commitURL, commit.Subject)

	if err := upsertLinkEntry(ctx, client, taskID, entry); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(out, "Linked commit %s to task %s\n", shortSHA, taskID)
	return nil
}

// resolveTaskID returns the task ID, auto-detecting from branch if not provided.
func resolveTaskID(ctx context.Context, gh gitHelper, taskID string) (string, error) {
	if taskID != "" {
		return taskID, nil
	}
	branch, err := gh.CurrentBranch(ctx)
	if err != nil {
		return "", errors.WrapWithDetails(err, "detecting current branch")
	}
	taskID = githelper.TaskIDFromBranch(branch)
	if taskID == "" {
		return "", errors.WithDetails("cannot detect task ID from branch, use --task", "branch", branch)
	}
	return taskID, nil
}

// upsertLinkEntry fetches the task description, adds or replaces the entry in
// the GitHub links section, and writes it back.
func upsertLinkEntry(ctx context.Context, client *clickup.Client, taskID, entry string) error {
	desc, err := client.GetMarkdownDescription(ctx, taskID)
	if err != nil {
		return errors.WrapWithDetails(err, "fetching task description", "taskID", taskID)
	}

	newDesc := upsertSection(desc, entry)

	return client.SetMarkdownDescription(ctx, taskID, newDesc)
}

// upsertSection inserts or updates an entry in the GitHub links section of a
// markdown description. If the section doesn't exist, it's prepended. If an
// entry with the same prefix exists, it's replaced (deduplication).
func upsertSection(desc, entry string) string {
	lines := strings.Split(desc, "\n")
	headerIdx := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == linkHeader {
			headerIdx = i
			break
		}
	}

	if headerIdx == -1 {
		// No existing section — prepend it.
		section := linkHeader + "\n- " + entry
		if desc == "" {
			return section
		}
		return section + "\n\n" + desc
	}

	// Parse existing entries in the section (lines starting with "- " after header).
	sectionStart := headerIdx + 1
	sectionEnd := sectionStart
	for sectionEnd < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[sectionEnd]), "- ") {
		sectionEnd++
	}

	// Check for duplicate by matching the entry prefix (everything
	// before the URL). Anchor the match so e.g. "owner/repo#42" cannot
	// match "owner/repo#427". Commit entries end with "](" before the
	// URL; PR entries end with " — " before the title.
	entryPrefix := entryMatchPrefix(entry)
	var anchor string
	if strings.HasPrefix(entryPrefix, "Commit: [`") {
		anchor = entryPrefix + "("
	} else {
		anchor = entryPrefix + " — "
	}
	replaced := false
	for i := sectionStart; i < sectionEnd; i++ {
		if strings.Contains(lines[i], anchor) {
			lines[i] = "- " + entry
			replaced = true
			break
		}
	}

	if !replaced {
		// Insert new entry at end of section.
		newLine := "- " + entry
		lines = append(lines[:sectionEnd], append([]string{newLine}, lines[sectionEnd:]...)...)
	}

	return strings.Join(lines, "\n")
}

// entryMatchPrefix extracts a prefix used for deduplication.
// For PRs: "owner/repo#42" (repo + PR number)
// For commits: "Commit: [`abc1234`]" (commit prefix + short SHA)
func entryMatchPrefix(entry string) string {
	// For commit entries, match up to the backtick-quoted SHA.
	if strings.HasPrefix(entry, "Commit: [`") {
		if idx := strings.Index(entry, "`]"); idx > 0 {
			return entry[:idx+2]
		}
	}
	// For PR entries, match up to the " — " separator (repo#number).
	if idx := strings.Index(entry, " — "); idx > 0 {
		return entry[1:idx] // skip leading "["
	}
	return entry
}

// repoFromURL extracts "owner/repo" from a GitHub PR URL like
// "https://github.com/owner/repo/pull/42".
func repoFromURL(url string) string {
	const prefix = "https://github.com/"
	if !strings.HasPrefix(url, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(url, prefix)
	parts := strings.Split(rest, "/")
	if len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return ""
}
