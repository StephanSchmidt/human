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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stephanschmidt/human/internal/tracker"
)

// --- mock provider ---

type mockProvider struct {
	listIssuesFn   func(ctx context.Context, opts tracker.ListOptions) ([]tracker.Issue, error)
	getIssueFn     func(ctx context.Context, key string) (*tracker.Issue, error)
	createIssueFn  func(ctx context.Context, issue *tracker.Issue) (*tracker.Issue, error)
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

func (m *mockProvider) ListComments(ctx context.Context, issueKey string) ([]tracker.Comment, error) {
	return m.listCommentsFn(ctx, issueKey)
}

func (m *mockProvider) AddComment(ctx context.Context, issueKey string, body string) (*tracker.Comment, error) {
	return m.addCommentFn(ctx, issueKey, body)
}

// --- helper tests ---

func TestKeyHint(t *testing.T) {
	tests := []struct {
		name string
		cli  CLI
		want string
	}{
		{
			name: "issue get key",
			cli:  CLI{Issue: IssueCmd{Get: GetCmd{Key: "KAN-1"}}},
			want: "KAN-1",
		},
		{
			name: "issue create project",
			cli:  CLI{Issue: IssueCmd{Create: CreateCmd{Project: "octocat/hello-world"}}},
			want: "octocat/hello-world",
		},
		{
			name: "issues list project",
			cli:  CLI{Issues: IssuesCmd{List: ListCmd{Project: "ENG"}}},
			want: "ENG",
		},
		{
			name: "comment add key",
			cli:  CLI{Issue: IssueCmd{Comment: CommentCmd{Add: AddCommentCmd{Key: "KAN-5"}}}},
			want: "KAN-5",
		},
		{
			name: "comment list key",
			cli:  CLI{Issue: IssueCmd{Comment: CommentCmd{List: ListCommentsCmd{Key: "KAN-6"}}}},
			want: "KAN-6",
		},
		{
			name: "empty CLI returns empty",
			cli:  CLI{},
			want: "",
		},
		{
			name: "priority order: issue get wins",
			cli: CLI{
				Issue:  IssueCmd{Get: GetCmd{Key: "KAN-1"}, Create: CreateCmd{Project: "KAN"}},
				Issues: IssuesCmd{List: ListCmd{Project: "ENG"}},
			},
			want: "KAN-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, keyHint(&tt.cli))
		})
	}
}

func TestNeedsTrackerClient(t *testing.T) {
	tests := []struct {
		command string
		want    bool
	}{
		{"install", false},
		{"install --agent claude", false},
		{"tracker", false},
		{"tracker list", false},
		{"issues list", true},
		{"issue get", true},
		{"issue create", true},
		{"issue comment add", true},
		{"issue comment list", true},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			assert.Equal(t, tt.want, needsTrackerClient(tt.command))
		})
	}
}

func TestInstanceFromCLI(t *testing.T) {
	tests := []struct {
		name     string
		cli      CLI
		wantNil  bool
		wantKind string
	}{
		{
			name:    "no flags returns nil",
			cli:     CLI{},
			wantNil: true,
		},
		{
			name:     "jira full flags",
			cli:      CLI{JiraURL: "https://example.atlassian.net", JiraUser: "alice@example.com", JiraKey: "token"},
			wantKind: "jira",
		},
		{
			name:    "jira partial flags returns nil",
			cli:     CLI{JiraURL: "https://example.atlassian.net"},
			wantNil: true,
		},
		{
			name:     "github token only",
			cli:      CLI{GitHubToken: "ghp_test"},
			wantKind: "github",
		},
		{
			name:     "github with custom URL",
			cli:      CLI{GitHubToken: "ghp_test", GitHubURL: "https://github.example.com/api/v3"},
			wantKind: "github",
		},
		{
			name:     "gitlab token only",
			cli:      CLI{GitLabToken: "glpat-test"},
			wantKind: "gitlab",
		},
		{
			name:     "gitlab with custom URL",
			cli:      CLI{GitLabToken: "glpat-test", GitLabURL: "https://gitlab.example.com"},
			wantKind: "gitlab",
		},
		{
			name:     "linear token only",
			cli:      CLI{LinearToken: "lin_test"},
			wantKind: "linear",
		},
		{
			name:     "linear with custom URL",
			cli:      CLI{LinearToken: "lin_test", LinearURL: "https://custom.linear.app"},
			wantKind: "linear",
		},
		{
			name:     "jira takes priority over github",
			cli:      CLI{JiraURL: "https://x.atlassian.net", JiraUser: "a@b.com", JiraKey: "key", GitHubToken: "ghp_test"},
			wantKind: "jira",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := instanceFromCLI(&tt.cli)
			if tt.wantNil {
				assert.Nil(t, inst)
				return
			}
			require.NotNil(t, inst)
			assert.Equal(t, tt.wantKind, inst.Kind)
			assert.NotNil(t, inst.Provider)
		})
	}
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
	entry := trackerEntry{Name: "work", Type: "jira", URL: "https://example.atlassian.net", User: "alice"}

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
}

// --- setProvider / setOutput tests ---

func TestSetProvider(t *testing.T) {
	p := &mockProvider{}
	var cli CLI
	setProvider(&cli, p)

	assert.Equal(t, tracker.Provider(p), cli.Issues.List.Provider)
	assert.Equal(t, tracker.Provider(p), cli.Issue.Get.Provider)
	assert.Equal(t, tracker.Provider(p), cli.Issue.Create.Provider)
	assert.Equal(t, tracker.Provider(p), cli.Issue.Comment.Add.Provider)
	assert.Equal(t, tracker.Provider(p), cli.Issue.Comment.List.Provider)
}

func TestSetOutput(t *testing.T) {
	var buf bytes.Buffer
	var cli CLI
	setOutput(&cli, &buf)

	assert.Equal(t, &buf, cli.Issues.List.Out)
	assert.Equal(t, &buf, cli.Issue.Get.Out)
	assert.Equal(t, &buf, cli.Issue.Create.Out)
	assert.Equal(t, &buf, cli.Issue.Comment.Add.Out)
	assert.Equal(t, &buf, cli.Issue.Comment.List.Out)
	assert.Equal(t, &buf, cli.Tracker.List.Out)
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
`)
	instances, err := loadAllInstances(dir)
	require.NoError(t, err)
	assert.Len(t, instances, 2)
}

// --- Run() method tests ---

func TestListCmd_Run_JSON(t *testing.T) {
	issues := []tracker.Issue{
		{Key: "KAN-1", Summary: "First", Status: "Open"},
	}
	p := &mockProvider{
		listIssuesFn: func(_ context.Context, opts tracker.ListOptions) ([]tracker.Issue, error) {
			assert.Equal(t, "KAN", opts.Project)
			assert.Equal(t, 50, opts.MaxResults)
			return issues, nil
		},
	}

	var buf bytes.Buffer
	cmd := &ListCmd{Project: "KAN", Provider: p, Out: &buf}
	err := cmd.Run()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), `"key": "KAN-1"`)
}

func TestListCmd_Run_Table(t *testing.T) {
	issues := []tracker.Issue{
		{Key: "KAN-1", Summary: "First", Status: "Open"},
	}
	p := &mockProvider{
		listIssuesFn: func(_ context.Context, _ tracker.ListOptions) ([]tracker.Issue, error) {
			return issues, nil
		},
	}

	var buf bytes.Buffer
	cmd := &ListCmd{Project: "KAN", Table: true, Provider: p, Out: &buf}
	err := cmd.Run()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "KAN-1")
	assert.Contains(t, buf.String(), "KEY")
}

func TestListCmd_Run_error(t *testing.T) {
	p := &mockProvider{
		listIssuesFn: func(_ context.Context, _ tracker.ListOptions) ([]tracker.Issue, error) {
			return nil, fmt.Errorf("list failed")
		},
	}

	var buf bytes.Buffer
	cmd := &ListCmd{Project: "KAN", Provider: p, Out: &buf}
	err := cmd.Run()
	assert.EqualError(t, err, "list failed")
}

func TestGetCmd_Run(t *testing.T) {
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
	cmd := &GetCmd{Key: "KAN-1", Provider: p, Out: &buf}
	err := cmd.Run()
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

func TestGetCmd_Run_emptyFields(t *testing.T) {
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
	cmd := &GetCmd{Key: "KAN-2", Provider: p, Out: &buf}
	err := cmd.Run()
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "| Priority | None |")
	assert.Contains(t, out, "| Assignee | None |")
	assert.Contains(t, out, "| Reporter | None |")
	assert.NotContains(t, out, "## Description")
}

func TestGetCmd_Run_error(t *testing.T) {
	p := &mockProvider{
		getIssueFn: func(_ context.Context, _ string) (*tracker.Issue, error) {
			return nil, fmt.Errorf("get failed")
		},
	}

	var buf bytes.Buffer
	cmd := &GetCmd{Key: "KAN-1", Provider: p, Out: &buf}
	err := cmd.Run()
	assert.EqualError(t, err, "get failed")
}

func TestCreateCmd_Run(t *testing.T) {
	p := &mockProvider{
		createIssueFn: func(_ context.Context, issue *tracker.Issue) (*tracker.Issue, error) {
			assert.Equal(t, "KAN", issue.Project)
			assert.Equal(t, "Task", issue.Type)
			assert.Equal(t, "New issue", issue.Summary)
			return &tracker.Issue{Key: "KAN-42", Summary: "New issue"}, nil
		},
	}

	var buf bytes.Buffer
	cmd := &CreateCmd{Project: "KAN", Type: "Task", Summary: "New issue", Provider: p, Out: &buf}
	err := cmd.Run()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "KAN-42")
	assert.Contains(t, buf.String(), "New issue")
}

func TestCreateCmd_Run_error(t *testing.T) {
	p := &mockProvider{
		createIssueFn: func(_ context.Context, _ *tracker.Issue) (*tracker.Issue, error) {
			return nil, fmt.Errorf("create failed")
		},
	}

	var buf bytes.Buffer
	cmd := &CreateCmd{Project: "KAN", Summary: "X", Provider: p, Out: &buf}
	err := cmd.Run()
	assert.EqualError(t, err, "create failed")
}

func TestAddCommentCmd_Run(t *testing.T) {
	p := &mockProvider{
		addCommentFn: func(_ context.Context, issueKey string, body string) (*tracker.Comment, error) {
			assert.Equal(t, "KAN-1", issueKey)
			assert.Equal(t, "test comment", body)
			return &tracker.Comment{ID: "c-1", Body: "test comment"}, nil
		},
	}

	var buf bytes.Buffer
	cmd := &AddCommentCmd{Key: "KAN-1", Body: "test comment", Provider: p, Out: &buf}
	err := cmd.Run()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "c-1")
	assert.Contains(t, buf.String(), "test comment")
}

func TestAddCommentCmd_Run_error(t *testing.T) {
	p := &mockProvider{
		addCommentFn: func(_ context.Context, _ string, _ string) (*tracker.Comment, error) {
			return nil, fmt.Errorf("comment failed")
		},
	}

	var buf bytes.Buffer
	cmd := &AddCommentCmd{Key: "KAN-1", Body: "x", Provider: p, Out: &buf}
	err := cmd.Run()
	assert.EqualError(t, err, "comment failed")
}

func TestListCommentsCmd_Run(t *testing.T) {
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
	cmd := &ListCommentsCmd{Key: "KAN-1", Provider: p, Out: &buf}
	err := cmd.Run()
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "c-1")
	assert.Contains(t, out, "alice")
	assert.Contains(t, out, "hello")
	assert.Contains(t, out, "c-2")
}

func TestListCommentsCmd_Run_error(t *testing.T) {
	p := &mockProvider{
		listCommentsFn: func(_ context.Context, _ string) ([]tracker.Comment, error) {
			return nil, fmt.Errorf("list comments failed")
		},
	}

	var buf bytes.Buffer
	cmd := &ListCommentsCmd{Key: "KAN-1", Provider: p, Out: &buf}
	err := cmd.Run()
	assert.EqualError(t, err, "list comments failed")
}

func TestTrackerListCmd_Run_JSON(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `jiras:
  - name: work
    url: https://work.atlassian.net
    user: me@work.com
    key: tok1
`)

	var buf bytes.Buffer
	cmd := &TrackerListCmd{Dir: dir, Out: &buf}
	err := cmd.Run()
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "// Configured issue trackers")
	assert.Contains(t, out, `"name": "work"`)
	assert.Contains(t, out, `"type": "jira"`)
}

func TestTrackerListCmd_Run_Table(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `jiras:
  - name: work
    url: https://work.atlassian.net
    user: me@work.com
    key: tok1
`)

	var buf bytes.Buffer
	cmd := &TrackerListCmd{Table: true, Dir: dir, Out: &buf}
	err := cmd.Run()
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "NAME")
	assert.Contains(t, out, "work")
	assert.Contains(t, out, "jira")
}

func TestTrackerListCmd_Run_empty(t *testing.T) {
	dir := t.TempDir()

	var buf bytes.Buffer
	cmd := &TrackerListCmd{Dir: dir, Out: &buf}
	err := cmd.Run()
	require.NoError(t, err)

	// Empty list => prints JSON with empty array
	out := buf.String()
	assert.Contains(t, out, "[]")
}

func TestTrackerListCmd_Run_defaultDir(t *testing.T) {
	// When Dir is empty, defaults to "." — use a clean temp dir to avoid
	// picking up a real .humanconfig from the repo root.
	dir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	var buf bytes.Buffer
	cmd := &TrackerListCmd{Out: &buf}
	err = cmd.Run()
	require.NoError(t, err)
	// Output should contain something (either trackers or empty)
	assert.True(t, strings.Contains(buf.String(), "//") || strings.Contains(buf.String(), "[]"))
}
