package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stephanschmidt/human/internal/tracker"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListIssues_happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/repos/octocat/hello-world/issues", r.URL.Path)
		assert.Equal(t, "50", r.URL.Query().Get("per_page"))
		assert.Equal(t, "open", r.URL.Query().Get("state"))
		assert.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))
		assert.Contains(t, r.Header.Get("Authorization"), "Bearer ")

		// Return 2 issues + 1 PR (should be filtered).
		_, _ = fmt.Fprint(w, `[
			{"number":1,"title":"Bug report","body":"desc1","state":"open","user":{"login":"alice"},"assignee":{"login":"bob"},"labels":[{"name":"bug"}]},
			{"number":2,"title":"Feature request","body":"desc2","state":"open","user":{"login":"alice"},"labels":[]},
			{"number":3,"title":"A pull request","body":"pr body","state":"open","user":{"login":"charlie"},"pull_request":{}}
		]`)
	}))
	defer srv.Close()

	client := New(srv.URL, "ghp_test")
	issues, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "octocat/hello-world",
		MaxResults: 50,
	})

	require.NoError(t, err)
	require.Len(t, issues, 2)

	assert.Equal(t, "octocat/hello-world#1", issues[0].Key)
	assert.Equal(t, "Bug report", issues[0].Summary)
	assert.Equal(t, "open", issues[0].Status)
	assert.Equal(t, "bug", issues[0].Type)
	assert.Equal(t, "bob", issues[0].Assignee)
	assert.Equal(t, "alice", issues[0].Reporter)

	assert.Equal(t, "octocat/hello-world#2", issues[1].Key)
	assert.Equal(t, "", issues[1].Type) // no labels
	assert.Equal(t, "", issues[1].Assignee)
}

func TestListIssues_invalidProject(t *testing.T) {
	client := New("http://localhost", "ghp_test")
	_, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "noslash",
		MaxResults: 10,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid project format")
}

func TestListIssues_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := New(srv.URL, "ghp_test")
	_, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "octocat/hello-world",
		MaxResults: 10,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status")
}

func TestGetIssue_happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/repos/octocat/hello-world/issues/42", r.URL.Path)

		_, _ = fmt.Fprint(w, `{
			"number": 42,
			"title": "The answer",
			"body": "## Description\n\nThis is markdown.",
			"state": "open",
			"user": {"login": "alice"},
			"assignee": {"login": "bob"},
			"labels": [{"name": "enhancement"}, {"name": "help wanted"}]
		}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "ghp_test")
	issue, err := client.GetIssue(context.Background(), "octocat/hello-world#42")

	require.NoError(t, err)
	assert.Equal(t, "octocat/hello-world#42", issue.Key)
	assert.Equal(t, "The answer", issue.Summary)
	assert.Equal(t, "open", issue.Status)
	assert.Equal(t, "enhancement", issue.Type) // first label
	assert.Equal(t, "", issue.Priority)         // GitHub has no priority
	assert.Equal(t, "bob", issue.Assignee)
	assert.Equal(t, "alice", issue.Reporter)
	assert.Equal(t, "## Description\n\nThis is markdown.", issue.Description)
}

func TestGetIssue_invalidKey(t *testing.T) {
	client := New("http://localhost", "ghp_test")
	_, err := client.GetIssue(context.Background(), "nohash")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid issue key format")
}

func TestGetIssue_invalidNumber(t *testing.T) {
	client := New("http://localhost", "ghp_test")
	_, err := client.GetIssue(context.Background(), "octocat/hello-world#abc")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid issue number")
}

func TestCreateIssue_happy(t *testing.T) {
	var gotBody createRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/repos/octocat/hello-world/issues", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &gotBody))

		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{"number":99,"title":"New issue","body":"Some description"}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "ghp_test")
	issue, err := client.CreateIssue(context.Background(), &tracker.Issue{
		Project:     "octocat/hello-world",
		Summary:     "New issue",
		Description: "Some description",
	})

	require.NoError(t, err)
	assert.Equal(t, "octocat/hello-world#99", issue.Key)
	assert.Equal(t, "octocat/hello-world", issue.Project)
	assert.Equal(t, "New issue", issue.Summary)
	assert.Equal(t, "Some description", issue.Description)

	assert.Equal(t, "New issue", gotBody.Title)
	assert.Equal(t, "Some description", gotBody.Body)
}

func TestCreateIssue_withoutDescription(t *testing.T) {
	var gotRaw map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &gotRaw))

		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{"number":100,"title":"No desc issue","body":""}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "ghp_test")
	issue, err := client.CreateIssue(context.Background(), &tracker.Issue{
		Project: "octocat/hello-world",
		Summary: "No desc issue",
	})

	require.NoError(t, err)
	assert.Equal(t, "octocat/hello-world#100", issue.Key)
	// body should be omitted from JSON when empty
	_, hasBody := gotRaw["body"]
	assert.False(t, hasBody, "body should be omitted when empty")
}

func TestCreateIssue_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer srv.Close()

	client := New(srv.URL, "ghp_test")
	_, err := client.CreateIssue(context.Background(), &tracker.Issue{
		Project: "octocat/hello-world",
		Summary: "Will fail",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status")
}

func Test_splitProject(t *testing.T) {
	tests := []struct {
		name      string
		project   string
		wantOwner string
		wantRepo  string
		wantErr   string
	}{
		{name: "valid", project: "octocat/hello-world", wantOwner: "octocat", wantRepo: "hello-world"},
		{name: "with dots", project: "org/repo.name", wantOwner: "org", wantRepo: "repo.name"},
		{name: "no slash", project: "noslash", wantErr: "invalid project format"},
		{name: "empty owner", project: "/repo", wantErr: "invalid project format"},
		{name: "empty repo", project: "owner/", wantErr: "invalid project format"},
		{name: "empty string", project: "", wantErr: "invalid project format"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := splitProject(tt.project)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantOwner, owner)
			assert.Equal(t, tt.wantRepo, repo)
		})
	}
}

func Test_parseIssueKey(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		wantOwner  string
		wantRepo   string
		wantNumber int
		wantErr    string
	}{
		{name: "valid", key: "octocat/hello-world#42", wantOwner: "octocat", wantRepo: "hello-world", wantNumber: 42},
		{name: "large number", key: "org/repo#99999", wantOwner: "org", wantRepo: "repo", wantNumber: 99999},
		{name: "no hash", key: "octocat/hello-world", wantErr: "invalid issue key format"},
		{name: "no slash", key: "noslash#42", wantErr: "invalid issue key format"},
		{name: "non-numeric", key: "octocat/hello-world#abc", wantErr: "invalid issue number"},
		{name: "empty", key: "", wantErr: "invalid issue key format"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, number, err := parseIssueKey(tt.key)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantOwner, owner)
			assert.Equal(t, tt.wantRepo, repo)
			assert.Equal(t, tt.wantNumber, number)
		})
	}
}
