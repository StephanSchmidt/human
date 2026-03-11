package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stephanschmidt/human/internal/tracker"
)

func TestAuditLogPath(t *testing.T) {
	p := auditLogPath()
	assert.Contains(t, p, ".human")
	assert.Contains(t, p, "audit.log")
	assert.True(t, filepath.IsAbs(p), "expected absolute path, got %s", p)
}

// --- help / printExamples tests ---

func TestRootHelp_includesExamples(t *testing.T) {
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	origLoader := helpInstanceLoader
	t.Cleanup(func() { helpInstanceLoader = origLoader })
	helpInstanceLoader = func() ([]tracker.Instance, error) {
		return []tracker.Instance{
			{Name: "work", Kind: "jira", URL: "https://work.atlassian.net", User: "me@work.com", Description: "Sprint planning"},
		}, nil
	}

	err := cmd.Execute()
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "Examples:")
	assert.Contains(t, out, "Connected trackers:")
	assert.Contains(t, out, "work")
	assert.Contains(t, out, "jira")
	assert.Contains(t, out, "Sprint planning")
}

func TestSubcommandHelp_noExamples(t *testing.T) {
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"jira", "--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := buf.String()
	assert.NotContains(t, out, "Examples:")
}

func TestPrintExamples(t *testing.T) {
	var buf bytes.Buffer
	printExamples(&buf)

	out := buf.String()

	// Command pattern section.
	assert.Contains(t, out, "Command pattern:")
	assert.Contains(t, out, "human <tracker> issues list")
	assert.Contains(t, out, "human <tracker> issue  get")
	assert.Contains(t, out, "human <tracker> issue  create")
	assert.Contains(t, out, "human <tracker> issue  delete")
	assert.Contains(t, out, "human <tracker> issue  comment add")
	assert.Contains(t, out, "human <tracker> issue  comment list")

	// Key format reference table — all providers present.
	assert.Contains(t, out, "jira")
	assert.Contains(t, out, "github")
	assert.Contains(t, out, "gitlab")
	assert.Contains(t, out, "linear")
	assert.Contains(t, out, "azuredevops")
	assert.Contains(t, out, "shortcut")
	assert.Contains(t, out, "KAN-1")
	assert.Contains(t, out, "octocat/hello-world#42")
	assert.Contains(t, out, "ENG-123")

	// Concrete examples.
	assert.Contains(t, out, "Examples:")
	assert.Contains(t, out, "human jira issues list --project=KAN")
	assert.Contains(t, out, "human jira issue get KAN-1")
	assert.Contains(t, out, `human jira issue create --project=KAN "Implement login page"`)
	assert.Contains(t, out, "human github issues list --project=octocat/hello-world")
	assert.Contains(t, out, "human jira issue delete KAN-1")
	assert.Contains(t, out, "human tracker list")
	assert.Contains(t, out, "human install --agent claude")
}

func TestPrintExamples_startsWithBlankLine(t *testing.T) {
	var buf bytes.Buffer
	printExamples(&buf)
	assert.True(t, strings.HasPrefix(buf.String(), "\n"), "output should start with a blank line separator")
}

func TestPrintConnectedTrackers_withInstances(t *testing.T) {
	orig := helpInstanceLoader
	t.Cleanup(func() { helpInstanceLoader = orig })

	helpInstanceLoader = func() ([]tracker.Instance, error) {
		return []tracker.Instance{
			{Name: "work", Kind: "jira", URL: "https://work.atlassian.net", User: "me@work.com", Description: "Sprint planning"},
			{Name: "personal", Kind: "github", URL: "https://api.github.com", Description: "OSS projects"},
		}, nil
	}

	var buf bytes.Buffer
	printConnectedTrackers(&buf)

	out := buf.String()
	assert.Contains(t, out, "Connected trackers:")
	assert.Contains(t, out, "work")
	assert.Contains(t, out, "jira")
	assert.Contains(t, out, "https://work.atlassian.net")
	assert.Contains(t, out, "me@work.com")
	assert.Contains(t, out, "Sprint planning")
	assert.Contains(t, out, "personal")
	assert.Contains(t, out, "github")
	assert.Contains(t, out, "OSS projects")
}

func TestPrintConnectedTrackers_empty(t *testing.T) {
	orig := helpInstanceLoader
	t.Cleanup(func() { helpInstanceLoader = orig })

	helpInstanceLoader = func() ([]tracker.Instance, error) {
		return nil, nil
	}

	var buf bytes.Buffer
	printConnectedTrackers(&buf)

	out := buf.String()
	assert.Contains(t, out, "Connected trackers: none")
	assert.Contains(t, out, ".humanconfig.yaml")
}

func TestPrintConnectedTrackers_error(t *testing.T) {
	orig := helpInstanceLoader
	t.Cleanup(func() { helpInstanceLoader = orig })

	helpInstanceLoader = func() ([]tracker.Instance, error) {
		return nil, fmt.Errorf("config error")
	}

	var buf bytes.Buffer
	printConnectedTrackers(&buf)

	assert.Empty(t, buf.String(), "errors should be silently ignored")
}

// --- mock provider ---

type mockProvider struct {
	listIssuesFn   func(ctx context.Context, opts tracker.ListOptions) ([]tracker.Issue, error)
	getIssueFn     func(ctx context.Context, key string) (*tracker.Issue, error)
	createIssueFn  func(ctx context.Context, issue *tracker.Issue) (*tracker.Issue, error)
	deleteIssueFn  func(ctx context.Context, key string) error
	listCommentsFn func(ctx context.Context, issueKey string) ([]tracker.Comment, error)
	addCommentFn   func(ctx context.Context, issueKey string, body string) (*tracker.Comment, error)
}

func (m *mockProvider) ListIssues(ctx context.Context, opts tracker.ListOptions) ([]tracker.Issue, error) {
	return m.listIssuesFn(ctx, opts)
}

func (m *mockProvider) GetIssue(ctx context.Context, key string) (*tracker.Issue, error) {
	return m.getIssueFn(ctx, key)
}

func (m *mockProvider) CreateIssue(ctx context.Context, issue *tracker.Issue) (*tracker.Issue, error) {
	return m.createIssueFn(ctx, issue)
}

func (m *mockProvider) DeleteIssue(ctx context.Context, key string) error {
	return m.deleteIssueFn(ctx, key)
}

func (m *mockProvider) ListComments(ctx context.Context, issueKey string) ([]tracker.Comment, error) {
	return m.listCommentsFn(ctx, issueKey)
}

func (m *mockProvider) AddComment(ctx context.Context, issueKey string, body string) (*tracker.Comment, error) {
	return m.addCommentFn(ctx, issueKey, body)
}

// --- print function tests ---

func TestPrintTrackerJSON(t *testing.T) {
	entries := []trackerEntry{
		{Name: "work", Type: "jira", URL: "https://example.atlassian.net", User: "alice"},
	}

	var buf bytes.Buffer
	err := printTrackerJSON(&buf, entries)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "// Configured issue trackers")
	assert.Contains(t, out, `"name": "work"`)
	assert.Contains(t, out, `"type": "jira"`)
}

func TestPrintTrackerTable_empty(t *testing.T) {
	var buf bytes.Buffer
	err := printTrackerTable(&buf, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No trackers configured")
}

func TestPrintTrackerTable_withEntries(t *testing.T) {
	entries := []trackerEntry{
		{Name: "work", Type: "jira", URL: "https://example.atlassian.net", User: "alice"},
		{Name: "oss", Type: "github", URL: "https://api.github.com"},
	}

	var buf bytes.Buffer
	err := printTrackerTable(&buf, entries)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "NAME")
	assert.Contains(t, out, "TYPE")
	assert.Contains(t, out, "work")
	assert.Contains(t, out, "jira")
	assert.Contains(t, out, "oss")
	assert.Contains(t, out, "github")
}

func TestPrintIssuesJSON(t *testing.T) {
	issues := []tracker.Issue{
		{Key: "KAN-1", Summary: "First issue", Status: "Open"},
	}

	var buf bytes.Buffer
	err := printIssuesJSON(&buf, issues)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, `"key": "KAN-1"`)
	assert.Contains(t, out, `"summary": "First issue"`)
}

func TestPrintIssuesTable(t *testing.T) {
	issues := []tracker.Issue{
		{Key: "KAN-1", Status: "Open", Summary: "First issue"},
		{Key: "KAN-2", Status: "Done", Summary: "Second issue"},
	}

	var buf bytes.Buffer
	err := printIssuesTable(&buf, issues)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "KEY")
	assert.Contains(t, out, "STATUS")
	assert.Contains(t, out, "SUMMARY")
	assert.Contains(t, out, "KAN-1")
	assert.Contains(t, out, "KAN-2")
}

func TestTrackerEntry_JSONFields(t *testing.T) {
	entry := trackerEntry{Name: "work", Type: "jira", URL: "https://example.atlassian.net", User: "alice", Description: "Sprint planning"}

	data, err := json.Marshal(entry)
	require.NoError(t, err)

	var buf bytes.Buffer
	require.NoError(t, json.Compact(&buf, data))

	var parsed map[string]string
	require.NoError(t, json.Unmarshal(buf.Bytes(), &parsed))

	assert.Equal(t, "work", parsed["name"])
	assert.Equal(t, "jira", parsed["type"])
	assert.Equal(t, "https://example.atlassian.net", parsed["url"])
	assert.Equal(t, "alice", parsed["user"])
	assert.Equal(t, "Sprint planning", parsed["description"])
}

// --- loadAllInstances tests ---

func writeConfig(t *testing.T, dir, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".humanconfig.yaml"), []byte(content), 0o644))
}

func TestLoadAllInstances_noConfig(t *testing.T) {
	dir := t.TempDir()
	instances, err := loadAllInstances(dir)
	require.NoError(t, err)
	assert.Empty(t, instances)
}

func TestLoadAllInstances_withJira(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `jiras:
  - name: work
    url: https://work.atlassian.net
    user: me@work.com
    key: tok1
`)
	instances, err := loadAllInstances(dir)
	require.NoError(t, err)
	require.Len(t, instances, 1)
	assert.Equal(t, "work", instances[0].Name)
	assert.Equal(t, "jira", instances[0].Kind)
}

func TestLoadAllInstances_multipleProviders(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `jiras:
  - name: work
    url: https://work.atlassian.net
    user: me@work.com
    key: tok1
githubs:
  - name: oss
    token: ghp_test
azuredevops:
  - name: devops
    org: myorg
    token: pat-test
`)

	unsetAzureEnvs(t)

	instances, err := loadAllInstances(dir)
	require.NoError(t, err)
	assert.Len(t, instances, 3)
}

func unsetAzureEnvs(t *testing.T) {
	t.Helper()
	for _, key := range []string{"AZURE_URL", "AZURE_ORG", "AZURE_TOKEN"} {
		t.Setenv(key, "")
		require.NoError(t, os.Unsetenv(key))
	}
}

// --- Business logic function tests ---

func TestRunListIssues_JSON(t *testing.T) {
	issues := []tracker.Issue{
		{Key: "KAN-1", Summary: "First", Status: "Open"},
	}
	p := &mockProvider{
		listIssuesFn: func(_ context.Context, opts tracker.ListOptions) ([]tracker.Issue, error) {
			assert.Equal(t, "KAN", opts.Project)
			assert.Equal(t, 50, opts.MaxResults)
			assert.False(t, opts.IncludeAll)
			return issues, nil
		},
	}

	var buf bytes.Buffer
	err := runListIssues(context.Background(), p, &buf, "KAN", false, false)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), `"key": "KAN-1"`)
}

func TestRunListIssues_All(t *testing.T) {
	issues := []tracker.Issue{
		{Key: "KAN-1", Summary: "Open", Status: "Open"},
		{Key: "KAN-2", Summary: "Done", Status: "Done"},
	}
	p := &mockProvider{
		listIssuesFn: func(_ context.Context, opts tracker.ListOptions) ([]tracker.Issue, error) {
			assert.True(t, opts.IncludeAll)
			return issues, nil
		},
	}

	var buf bytes.Buffer
	err := runListIssues(context.Background(), p, &buf, "KAN", true, false)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), `"key": "KAN-1"`)
	assert.Contains(t, buf.String(), `"key": "KAN-2"`)
}

func TestRunListIssues_Table(t *testing.T) {
	issues := []tracker.Issue{
		{Key: "KAN-1", Summary: "First", Status: "Open"},
	}
	p := &mockProvider{
		listIssuesFn: func(_ context.Context, _ tracker.ListOptions) ([]tracker.Issue, error) {
			return issues, nil
		},
	}

	var buf bytes.Buffer
	err := runListIssues(context.Background(), p, &buf, "KAN", false, true)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "KAN-1")
	assert.Contains(t, buf.String(), "KEY")
}

func TestRunListIssues_error(t *testing.T) {
	p := &mockProvider{
		listIssuesFn: func(_ context.Context, _ tracker.ListOptions) ([]tracker.Issue, error) {
			return nil, fmt.Errorf("list failed")
		},
	}

	var buf bytes.Buffer
	err := runListIssues(context.Background(), p, &buf, "KAN", false, false)
	assert.EqualError(t, err, "list failed")
}

func TestRunGetIssue(t *testing.T) {
	issue := &tracker.Issue{
		Key:         "KAN-1",
		Summary:     "Test issue",
		Status:      "In Progress",
		Priority:    "High",
		Assignee:    "alice",
		Reporter:    "bob",
		Description: "Some description",
	}
	p := &mockProvider{
		getIssueFn: func(_ context.Context, key string) (*tracker.Issue, error) {
			assert.Equal(t, "KAN-1", key)
			return issue, nil
		},
	}

	var buf bytes.Buffer
	err := runGetIssue(context.Background(), p, &buf, "KAN-1")
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "# KAN-1: Test issue")
	assert.Contains(t, out, "| Status   | In Progress |")
	assert.Contains(t, out, "| Priority | High |")
	assert.Contains(t, out, "| Assignee | alice |")
	assert.Contains(t, out, "| Reporter | bob |")
	assert.Contains(t, out, "## Description")
	assert.Contains(t, out, "Some description")
}

func TestRunGetIssue_emptyFields(t *testing.T) {
	issue := &tracker.Issue{
		Key:     "KAN-2",
		Summary: "Minimal",
		Status:  "Open",
	}
	p := &mockProvider{
		getIssueFn: func(_ context.Context, _ string) (*tracker.Issue, error) {
			return issue, nil
		},
	}

	var buf bytes.Buffer
	err := runGetIssue(context.Background(), p, &buf, "KAN-2")
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "| Priority | None |")
	assert.Contains(t, out, "| Assignee | None |")
	assert.Contains(t, out, "| Reporter | None |")
	assert.NotContains(t, out, "## Description")
}

func TestRunGetIssue_error(t *testing.T) {
	p := &mockProvider{
		getIssueFn: func(_ context.Context, _ string) (*tracker.Issue, error) {
			return nil, fmt.Errorf("get failed")
		},
	}

	var buf bytes.Buffer
	err := runGetIssue(context.Background(), p, &buf, "KAN-1")
	assert.EqualError(t, err, "get failed")
}

func TestRunCreateIssue(t *testing.T) {
	p := &mockProvider{
		createIssueFn: func(_ context.Context, issue *tracker.Issue) (*tracker.Issue, error) {
			assert.Equal(t, "KAN", issue.Project)
			assert.Equal(t, "Task", issue.Type)
			assert.Equal(t, "New issue", issue.Summary)
			return &tracker.Issue{Key: "KAN-42", Summary: "New issue"}, nil
		},
	}

	var buf bytes.Buffer
	err := runCreateIssue(context.Background(), p, &buf, "KAN", "Task", "New issue", "")
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "KAN-42")
	assert.Contains(t, buf.String(), "New issue")
}

func TestRunCreateIssue_error(t *testing.T) {
	p := &mockProvider{
		createIssueFn: func(_ context.Context, _ *tracker.Issue) (*tracker.Issue, error) {
			return nil, fmt.Errorf("create failed")
		},
	}

	var buf bytes.Buffer
	err := runCreateIssue(context.Background(), p, &buf, "KAN", "Task", "X", "")
	assert.EqualError(t, err, "create failed")
}

func TestRunDeleteIssue(t *testing.T) {
	p := &mockProvider{
		deleteIssueFn: func(_ context.Context, key string) error {
			assert.Equal(t, "KAN-1", key)
			return nil
		},
	}

	var buf bytes.Buffer
	err := runDeleteIssue(context.Background(), p, &buf, "KAN-1")
	require.NoError(t, err)
	assert.Equal(t, "Deleted KAN-1\n", buf.String())
}

func TestRunDeleteIssue_error(t *testing.T) {
	p := &mockProvider{
		deleteIssueFn: func(_ context.Context, _ string) error {
			return fmt.Errorf("delete failed")
		},
	}

	var buf bytes.Buffer
	err := runDeleteIssue(context.Background(), p, &buf, "KAN-1")
	assert.EqualError(t, err, "delete failed")
}

func TestRunAddComment(t *testing.T) {
	p := &mockProvider{
		addCommentFn: func(_ context.Context, issueKey string, body string) (*tracker.Comment, error) {
			assert.Equal(t, "KAN-1", issueKey)
			assert.Equal(t, "test comment", body)
			return &tracker.Comment{ID: "c-1", Body: "test comment"}, nil
		},
	}

	var buf bytes.Buffer
	err := runAddComment(context.Background(), p, &buf, "KAN-1", "test comment")
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "c-1")
	assert.Contains(t, buf.String(), "test comment")
}

func TestRunAddComment_error(t *testing.T) {
	p := &mockProvider{
		addCommentFn: func(_ context.Context, _ string, _ string) (*tracker.Comment, error) {
			return nil, fmt.Errorf("comment failed")
		},
	}

	var buf bytes.Buffer
	err := runAddComment(context.Background(), p, &buf, "KAN-1", "x")
	assert.EqualError(t, err, "comment failed")
}

func TestRunListComments(t *testing.T) {
	comments := []tracker.Comment{
		{ID: "c-1", Author: "alice", Body: "hello"},
		{ID: "c-2", Author: "bob", Body: "world"},
	}
	p := &mockProvider{
		listCommentsFn: func(_ context.Context, issueKey string) ([]tracker.Comment, error) {
			assert.Equal(t, "KAN-1", issueKey)
			return comments, nil
		},
	}

	var buf bytes.Buffer
	err := runListComments(context.Background(), p, &buf, "KAN-1")
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "c-1")
	assert.Contains(t, out, "alice")
	assert.Contains(t, out, "hello")
	assert.Contains(t, out, "c-2")
}

func TestRunListComments_error(t *testing.T) {
	p := &mockProvider{
		listCommentsFn: func(_ context.Context, _ string) ([]tracker.Comment, error) {
			return nil, fmt.Errorf("list comments failed")
		},
	}

	var buf bytes.Buffer
	err := runListComments(context.Background(), p, &buf, "KAN-1")
	assert.EqualError(t, err, "list comments failed")
}

func TestRunTrackerList_JSON(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `jiras:
  - name: work
    url: https://work.atlassian.net
    user: me@work.com
    key: tok1
    description: Sprint planning
`)

	var buf bytes.Buffer
	err := runTrackerList(&buf, dir, false)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "// Configured issue trackers")
	assert.Contains(t, out, `"name": "work"`)
	assert.Contains(t, out, `"type": "jira"`)
	assert.Contains(t, out, `"description": "Sprint planning"`)
}

func TestRunTrackerList_Table(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `jiras:
  - name: work
    url: https://work.atlassian.net
    user: me@work.com
    key: tok1
    description: Sprint planning
`)

	var buf bytes.Buffer
	err := runTrackerList(&buf, dir, true)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "NAME")
	assert.Contains(t, out, "DESCRIPTION")
	assert.Contains(t, out, "work")
	assert.Contains(t, out, "jira")
	assert.Contains(t, out, "Sprint planning")
}

func TestRunTrackerList_empty(t *testing.T) {
	dir := t.TempDir()

	var buf bytes.Buffer
	err := runTrackerList(&buf, dir, false)
	require.NoError(t, err)

	// Empty list => prints JSON with empty array
	out := buf.String()
	assert.Contains(t, out, "[]")
}

func TestRunTrackerList_defaultDir(t *testing.T) {
	// When Dir is empty, defaults to "." — use a clean temp dir to avoid
	// picking up a real .humanconfig from the repo root.
	dir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	var buf bytes.Buffer
	err = runTrackerList(&buf, "", false)
	require.NoError(t, err)
	// Output should contain something (either trackers or empty)
	assert.True(t, strings.Contains(buf.String(), "//") || strings.Contains(buf.String(), "[]"))
}

// --- newRootCmd tests ---

func TestRootCmd_defaultRunsTrackerList(t *testing.T) {
	// When invoked without args, root command runs "tracker list"
	dir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{})

	err = cmd.Execute()
	require.NoError(t, err)
	// Should produce tracker list output (empty list)
	assert.Contains(t, buf.String(), "[]")
}

func TestRootCmd_hasProviderSubcommands(t *testing.T) {
	cmd := newRootCmd()
	providers := []string{"jira", "github", "gitlab", "linear", "azuredevops", "shortcut"}
	for _, name := range providers {
		found := false
		for _, sub := range cmd.Commands() {
			if sub.Use == name {
				found = true
				break
			}
		}
		assert.True(t, found, "expected provider subcommand: %s", name)
	}
}

func TestRootCmd_hasStaticSubcommands(t *testing.T) {
	cmd := newRootCmd()
	for _, name := range []string{"tracker", "install"} {
		found := false
		for _, sub := range cmd.Commands() {
			if sub.Use == name {
				found = true
				break
			}
		}
		assert.True(t, found, "expected static subcommand: %s", name)
	}
}

func TestProviderCmd_hasIssueSubcommands(t *testing.T) {
	cmd := newRootCmd()
	var jiraCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Use == "jira" {
			jiraCmd = sub
			break
		}
	}
	require.NotNil(t, jiraCmd)

	subNames := make(map[string]bool)
	for _, sub := range jiraCmd.Commands() {
		subNames[sub.Use] = true
	}
	assert.True(t, subNames["issues"], "expected 'issues' subcommand")
	assert.True(t, subNames["issue"], "expected 'issue' subcommand")
}

// --- instanceFromFlags tests ---

func TestInstanceFromFlags_noFlags(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{})
	_ = cmd.Execute()
	inst := instanceFromFlags(cmd)
	assert.Nil(t, inst)
}

// --- tracker find tests ---

func TestRunTrackerFindWithInstances_JSON(t *testing.T) {
	instances := []tracker.Instance{
		{Name: "work", Kind: "jira", Provider: &mockProvider{
			getIssueFn: func(_ context.Context, _ string) (*tracker.Issue, error) {
				return &tracker.Issue{Key: "KAN-42"}, nil
			},
		}},
	}

	var buf bytes.Buffer
	err := runTrackerFindWithInstances(context.Background(), &buf, "KAN-42", instances, false)
	require.NoError(t, err)

	var result map[string]string
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result))
	assert.Equal(t, "jira", result["provider"])
	assert.Equal(t, "KAN", result["project"])
	assert.Equal(t, "KAN-42", result["key"])
}

func TestRunTrackerFindWithInstances_Table(t *testing.T) {
	instances := []tracker.Instance{
		{Name: "work", Kind: "jira", Provider: &mockProvider{
			getIssueFn: func(_ context.Context, _ string) (*tracker.Issue, error) {
				return &tracker.Issue{Key: "KAN-42"}, nil
			},
		}},
	}

	var buf bytes.Buffer
	err := runTrackerFindWithInstances(context.Background(), &buf, "KAN-42", instances, true)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "PROVIDER")
	assert.Contains(t, out, "PROJECT")
	assert.Contains(t, out, "KEY")
	assert.Contains(t, out, "jira")
	assert.Contains(t, out, "KAN")
	assert.Contains(t, out, "KAN-42")
}

func TestRunTrackerFindWithInstances_NoMatch(t *testing.T) {
	instances := []tracker.Instance{
		{Name: "work", Kind: "github", Provider: &mockProvider{}},
	}

	var buf bytes.Buffer
	err := runTrackerFindWithInstances(context.Background(), &buf, "KAN-42", instances, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no configured tracker matches key format")
}
