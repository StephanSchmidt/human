package cmdclickup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/cmd/cmdutil"
	"github.com/StephanSchmidt/human/internal/clickup"
	"github.com/StephanSchmidt/human/internal/githelper"
	"github.com/StephanSchmidt/human/internal/tracker"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockGH struct {
	branch    string
	branchErr error
	pr        *githelper.PRInfo
	prErr     error
	commit    *githelper.CommitInfo
	commitErr error
	repoSlug  string
	repoErr   error
}

func (m *mockGH) CurrentBranch(_ context.Context) (string, error) {
	return m.branch, m.branchErr
}

func (m *mockGH) GetPRInfo(_ context.Context, _ int, _ string) (*githelper.PRInfo, error) {
	return m.pr, m.prErr
}

func (m *mockGH) GetCommitInfo(_ context.Context, _ string) (*githelper.CommitInfo, error) {
	return m.commit, m.commitErr
}

func (m *mockGH) GetRepoSlug(_ context.Context) (string, error) {
	return m.repoSlug, m.repoErr
}

// linkTestServer creates an httptest server that tracks markdown description updates.
func linkTestServer(t *testing.T, currentDesc string) (*httptest.Server, *string) {
	t.Helper()
	var mu sync.Mutex
	desc := currentDesc

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch r.Method {
		case http.MethodGet:
			// GetMarkdownDescription
			_, _ = fmt.Fprintf(w, `{
				"id": "abc1",
				"name": "Test",
				"description": "",
				"markdown_description": %s,
				"status": {"status": "open", "type": "open"},
				"assignees": [],
				"creator": {"id": 100, "username": "alice"},
				"date_created": "1700000000000",
				"date_updated": "1700100000000",
				"url": "https://app.clickup.com/t/abc1",
				"list": {"id": "901", "name": "Sprint 1"}
			}`, mustJSON(desc))

		case http.MethodPut:
			// SetMarkdownDescription
			body, _ := io.ReadAll(r.Body)
			var payload map[string]string
			_ = json.Unmarshal(body, &payload)
			desc = payload["markdown_description"]
			_, _ = fmt.Fprint(w, `{"id": "abc1"}`)

		default:
			t.Errorf("unexpected method: %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))

	return srv, &desc
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func buildLinkTestRoot(deps cmdutil.Deps, gh gitHelper) *cobra.Command {
	root := &cobra.Command{Use: "human", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("tracker", "", "")
	root.PersistentFlags().Bool("safe", false, "")

	clickupCmd := &cobra.Command{Use: "clickup"}
	clickupCmd.AddCommand(buildLinkCmd(deps, gh))
	root.AddCommand(clickupCmd)
	return root
}

func linkDeps(srv *httptest.Server) cmdutil.Deps {
	return cmdutil.Deps{
		LoadInstances: func(_ string) ([]tracker.Instance, error) {
			return []tracker.Instance{
				{
					Name:     "test",
					Kind:     "clickup",
					URL:      srv.URL,
					Provider: clickup.New(srv.URL, "tok-test", ""),
				},
			}, nil
		},
		InstanceFromFlags: func(_ *cobra.Command) *tracker.Instance { return nil },
	}
}

func TestLinkPR_explicit(t *testing.T) {
	srv, desc := linkTestServer(t, "Existing description")
	defer srv.Close()

	gh := &mockGH{
		pr: &githelper.PRInfo{
			Number: 42,
			Title:  "Fix login",
			URL:    "https://github.com/owner/repo/pull/42",
		},
	}

	root := buildLinkTestRoot(linkDeps(srv), gh)
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"clickup", "link", "pr", "--task", "abc1", "42"})

	err := root.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Linked PR #42 to task abc1")
	assert.Contains(t, *desc, linkHeader)
	assert.Contains(t, *desc, "owner/repo#42")
	assert.Contains(t, *desc, "Fix login")
	assert.Contains(t, *desc, "Existing description")
}

func TestLinkPR_autoDetectTask(t *testing.T) {
	srv, desc := linkTestServer(t, "")
	defer srv.Close()

	gh := &mockGH{
		branch: "feature/CU-abc1-fix",
		pr: &githelper.PRInfo{
			Number: 7,
			Title:  "Add feature",
			URL:    "https://github.com/o/r/pull/7",
		},
	}

	root := buildLinkTestRoot(linkDeps(srv), gh)
	root.SetArgs([]string{"clickup", "link", "pr"})

	err := root.Execute()
	require.NoError(t, err)
	assert.Contains(t, *desc, "o/r#7")
}

func TestLinkPR_noTaskDetected(t *testing.T) {
	srv, _ := linkTestServer(t, "")
	defer srv.Close()

	gh := &mockGH{
		branch: "main",
		pr:     &githelper.PRInfo{Number: 1, Title: "X", URL: "https://github.com/o/r/pull/1"},
	}

	root := buildLinkTestRoot(linkDeps(srv), gh)
	root.SetArgs([]string{"clickup", "link", "pr"})

	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot detect task ID")
}

func TestLinkCommit_explicit(t *testing.T) {
	srv, desc := linkTestServer(t, "")
	defer srv.Close()

	gh := &mockGH{
		commit: &githelper.CommitInfo{
			SHA:     "abc1234567890abcdef",
			Subject: "Fix login bug",
		},
		repoSlug: "owner/repo",
	}

	root := buildLinkTestRoot(linkDeps(srv), gh)
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"clickup", "link", "commit", "--task", "abc1", "abc1234"})

	err := root.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Linked commit abc1234 to task abc1")
	assert.Contains(t, *desc, "Commit:")
	assert.Contains(t, *desc, "abc1234")
	assert.Contains(t, *desc, "Fix login bug")
}

func TestLinkCommit_autoDetectRepo(t *testing.T) {
	srv, desc := linkTestServer(t, "")
	defer srv.Close()

	gh := &mockGH{
		branch: "CU-abc1-fix",
		commit: &githelper.CommitInfo{
			SHA:     "deadbeef12345",
			Subject: "Some commit",
		},
		repoSlug: "myorg/myrepo",
	}

	root := buildLinkTestRoot(linkDeps(srv), gh)
	root.SetArgs([]string{"clickup", "link", "commit"})

	err := root.Execute()
	require.NoError(t, err)
	assert.Contains(t, *desc, "myorg/myrepo")
}

func TestLinkCommit_withExplicitRepo(t *testing.T) {
	srv, desc := linkTestServer(t, "")
	defer srv.Close()

	gh := &mockGH{
		commit: &githelper.CommitInfo{
			SHA:     "abc123",
			Subject: "Fix",
		},
		// repoSlug should NOT be called since --repo is explicit
		repoSlug: "wrong/repo",
	}

	root := buildLinkTestRoot(linkDeps(srv), gh)
	root.SetArgs([]string{"clickup", "link", "commit", "--task", "abc1", "--repo", "correct/repo", "abc123"})

	err := root.Execute()
	require.NoError(t, err)
	assert.Contains(t, *desc, "correct/repo")
	assert.NotContains(t, *desc, "wrong/repo")
}

// --- upsertSection unit tests ---

func TestUpsertSection_empty(t *testing.T) {
	result := upsertSection("", "[o/r#1 — Title](url)")
	assert.Equal(t, "**GitHub** _(human)_\n- [o/r#1 — Title](url)", result)
}

func TestUpsertSection_existingDesc(t *testing.T) {
	result := upsertSection("Some description", "[o/r#1 — Title](url)")
	assert.Equal(t, "**GitHub** _(human)_\n- [o/r#1 — Title](url)\n\nSome description", result)
}

func TestUpsertSection_appendToExisting(t *testing.T) {
	existing := "**GitHub** _(human)_\n- [o/r#1 — First PR](url1)\n\nDescription"
	result := upsertSection(existing, "Commit: [`abc1234`](url2) Fix")
	assert.Contains(t, result, "- [o/r#1 — First PR](url1)")
	assert.Contains(t, result, "- Commit: [`abc1234`](url2) Fix")
	assert.Contains(t, result, "Description")
}

func TestUpsertSection_deduplicatePR(t *testing.T) {
	existing := "**GitHub** _(human)_\n- [o/r#1 — Old Title](url1)\n\nDescription"
	result := upsertSection(existing, "[o/r#1 — New Title](url1)")
	// Should replace, not duplicate
	assert.NotContains(t, result, "Old Title")
	assert.Contains(t, result, "New Title")
	// Count occurrences of "o/r#1"
	count := 0
	for i := 0; i+4 < len(result); i++ {
		if result[i:i+5] == "o/r#1" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

func TestUpsertSection_deduplicateCommit(t *testing.T) {
	existing := "**GitHub** _(human)_\n- Commit: [`abc1234`](url1) Old msg\n\nDescription"
	result := upsertSection(existing, "Commit: [`abc1234`](url2) New msg")
	assert.NotContains(t, result, "Old msg")
	assert.Contains(t, result, "New msg")
}

func TestRepoFromURL(t *testing.T) {
	assert.Equal(t, "owner/repo", repoFromURL("https://github.com/owner/repo/pull/42"))
	assert.Equal(t, "o/r", repoFromURL("https://github.com/o/r/commit/abc"))
	assert.Equal(t, "", repoFromURL("https://gitlab.com/o/r"))
	assert.Equal(t, "", repoFromURL(""))
}

func TestEntryMatchPrefix(t *testing.T) {
	assert.Equal(t, "o/r#42", entryMatchPrefix("[o/r#42 — Title](url)"))
	assert.Equal(t, "Commit: [`abc1234`]", entryMatchPrefix("Commit: [`abc1234`](url) Subject"))
}
