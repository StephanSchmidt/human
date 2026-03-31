package githelper

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/StephanSchmidt/human/errors"
)

// CommandRunner abstracts running external commands for testability.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// OSCommandRunner implements CommandRunner using os/exec.
type OSCommandRunner struct{}

// Run executes the command and returns its combined output.
func (OSCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output() // #nosec G204 — only called with hardcoded commands
}

// PRInfo holds metadata about a GitHub pull request.
type PRInfo struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
}

// CommitInfo holds metadata about a git commit.
type CommitInfo struct {
	SHA     string
	Subject string
}

// Helper provides git and gh CLI operations.
type Helper struct {
	Runner CommandRunner
}

// CurrentBranch returns the current git branch name.
func (h *Helper) CurrentBranch(ctx context.Context) (string, error) {
	out, err := h.Runner.Run(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", errors.WrapWithDetails(err, "getting current branch")
	}
	return strings.TrimSpace(string(out)), nil
}

// GetPRInfo fetches PR metadata using the gh CLI.
// If number is 0, it views the PR associated with the current branch.
// If repo is empty, gh uses the current repo.
func (h *Helper) GetPRInfo(ctx context.Context, number int, repo string) (*PRInfo, error) {
	args := []string{"pr", "view"}
	if number > 0 {
		args = append(args, strconv.Itoa(number))
	}
	if repo != "" {
		args = append(args, "--repo", repo)
	}
	args = append(args, "--json", "number,title,url")

	out, err := h.Runner.Run(ctx, "gh", args...)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "fetching PR info via gh")
	}

	var pr PRInfo
	if err := json.Unmarshal(out, &pr); err != nil {
		return nil, errors.WrapWithDetails(err, "parsing gh pr output")
	}
	return &pr, nil
}

// GetCommitInfo fetches commit metadata using git log.
// If sha is empty, defaults to HEAD.
func (h *Helper) GetCommitInfo(ctx context.Context, sha string) (*CommitInfo, error) {
	if sha == "" {
		sha = "HEAD"
	}
	out, err := h.Runner.Run(ctx, "git", "log", "-1", "--format=%H%n%s", sha)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "fetching commit info", "sha", sha)
	}

	lines := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)
	if len(lines) < 2 {
		return nil, errors.WithDetails("unexpected git log output", "output", string(out))
	}
	return &CommitInfo{
		SHA:     lines[0],
		Subject: lines[1],
	}, nil
}

// GetRepoSlug returns the owner/repo of the current repository via gh.
func (h *Helper) GetRepoSlug(ctx context.Context) (string, error) {
	out, err := h.Runner.Run(ctx, "gh", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner")
	if err != nil {
		return "", errors.WrapWithDetails(err, "detecting repository via gh")
	}
	slug := strings.TrimSpace(string(out))
	if slug == "" {
		return "", errors.WithDetails("gh returned empty repository slug")
	}
	return slug, nil
}

// CommitURL constructs a GitHub commit URL from repo slug and SHA.
func CommitURL(repo, sha string) string {
	return fmt.Sprintf("https://github.com/%s/commit/%s", repo, sha)
}
