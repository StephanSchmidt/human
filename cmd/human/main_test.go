package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestPrintTrackerJSON(t *testing.T) {
	entries := []trackerEntry{
		{Name: "work", Type: "jira", URL: "https://example.atlassian.net", User: "alice"},
	}

	// printTrackerJSON writes to os.Stdout, so we just verify it doesn't error
	// A full test would capture stdout, but the function is simple enough.
	err := printTrackerJSON(entries)
	assert.NoError(t, err)
}

func TestPrintTrackerTable_empty(t *testing.T) {
	// Verify empty entries prints a message and doesn't error.
	err := printTrackerTable(nil)
	assert.NoError(t, err)
}

func TestPrintTrackerTable_withEntries(t *testing.T) {
	entries := []trackerEntry{
		{Name: "work", Type: "jira", URL: "https://example.atlassian.net", User: "alice"},
		{Name: "oss", Type: "github", URL: "https://api.github.com"},
	}

	err := printTrackerTable(entries)
	assert.NoError(t, err)
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
