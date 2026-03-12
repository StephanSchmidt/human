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

// errDoer is a mock HTTPDoer that returns a fixed error.
type errDoer struct {
	err error
}

func (d *errDoer) Do(*http.Request) (*http.Response, error) {
	return nil, d.err
}

// nilDoer is a mock HTTPDoer that returns a nil response.
type nilDoer struct{}

func (*nilDoer) Do(*http.Request) (*http.Response, error) {
	return nil, nil
}

func TestDoRequest_networkError(t *testing.T) {
	client := New("https://api.github.com", "ghp_test")
	client.SetHTTPDoer(&errDoer{err: fmt.Errorf("connection refused")})

	_, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "octocat/hello-world",
		MaxResults: 10,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "requesting GitHub")
}

func TestDoRequest_nilResponse(t *testing.T) {
	client := New("https://api.github.com", "ghp_test")
	client.SetHTTPDoer(&nilDoer{})

	_, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "octocat/hello-world",
		MaxResults: 10,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil response")
}

func TestDoRequest_invalidBaseURL(t *testing.T) {
	client := New("ftp://api.github.com", "ghp_test")

	_, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "octocat/hello-world",
		MaxResults: 10,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "scheme must be http or https")
}

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
	assert.Equal(t, "Bug report", issues[0].Title)
	assert.Equal(t, "open", issues[0].Status)
	assert.Equal(t, "bug", issues[0].Type)
	assert.Equal(t, "bob", issues[0].Assignee)
	assert.Equal(t, "alice", issues[0].Reporter)

	assert.Equal(t, "octocat/hello-world#2", issues[1].Key)
	assert.Equal(t, "", issues[1].Type) // no labels
	assert.Equal(t, "", issues[1].Assignee)
}

func TestListIssues_all(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "all", r.URL.Query().Get("state"))

		_, _ = fmt.Fprint(w, `[
			{"number":1,"title":"Open issue","body":"","state":"open","user":{"login":"alice"},"labels":[]},
			{"number":2,"title":"Closed issue","body":"","state":"closed","user":{"login":"alice"},"labels":[]}
		]`)
	}))
	defer srv.Close()

	client := New(srv.URL, "ghp_test")
	issues, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "octocat/hello-world",
		MaxResults: 50,
		IncludeAll: true,
	})

	require.NoError(t, err)
	require.Len(t, issues, 2)
	assert.Equal(t, "open", issues[0].Status)
	assert.Equal(t, "closed", issues[1].Status)
}

func TestListIssues_emptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()

	client := New(srv.URL, "ghp_test")
	issues, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "octocat/hello-world",
		MaxResults: 10,
	})

	require.NoError(t, err)
	assert.Empty(t, issues)
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
	assert.Contains(t, err.Error(), "returned")
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
	assert.Equal(t, "The answer", issue.Title)
	assert.Equal(t, "open", issue.Status)
	assert.Equal(t, "enhancement", issue.Type) // first label
	assert.Equal(t, "", issue.Priority)        // GitHub has no priority
	assert.Equal(t, "bob", issue.Assignee)
	assert.Equal(t, "alice", issue.Reporter)
	assert.Equal(t, "## Description\n\nThis is markdown.", issue.Description)
}

func TestGetIssue_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := New(srv.URL, "ghp_test")
	_, err := client.GetIssue(context.Background(), "octocat/hello-world#42")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "returned")
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
		Title:     "New issue",
		Description: "Some description",
	})

	require.NoError(t, err)
	assert.Equal(t, "octocat/hello-world#99", issue.Key)
	assert.Equal(t, "octocat/hello-world", issue.Project)
	assert.Equal(t, "New issue", issue.Title)
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
		Title: "No desc issue",
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
		Title: "Will fail",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "returned")
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

func TestAddComment_happy(t *testing.T) {
	var gotBody commentRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/repos/octocat/hello-world/issues/42/comments", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &gotBody))

		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{
			"id": 101,
			"body": "Hello world",
			"user": {"login": "alice"},
			"created_at": "2025-01-15T10:30:00Z"
		}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "ghp_test")
	comment, err := client.AddComment(context.Background(), "octocat/hello-world#42", "Hello world")

	require.NoError(t, err)
	assert.Equal(t, "101", comment.ID)
	assert.Equal(t, "alice", comment.Author)
	assert.Equal(t, "Hello world", comment.Body)
	assert.False(t, comment.Created.IsZero())

	assert.Equal(t, "Hello world", gotBody.Body)
}

func TestAddComment_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	client := New(srv.URL, "ghp_test")
	_, err := client.AddComment(context.Background(), "octocat/hello-world#42", "test")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "returned")
}

func TestAddComment_invalidKey(t *testing.T) {
	client := New("http://localhost", "ghp_test")
	_, err := client.AddComment(context.Background(), "badkey", "test")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid issue key format")
}

func TestListComments_happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/repos/octocat/hello-world/issues/42/comments", r.URL.Path)

		_, _ = fmt.Fprint(w, `[
			{"id": 101, "body": "First comment", "user": {"login": "alice"}, "created_at": "2025-01-15T10:30:00Z"},
			{"id": 102, "body": "Second comment", "user": {"login": "bob"}, "created_at": "2025-01-16T11:00:00Z"}
		]`)
	}))
	defer srv.Close()

	client := New(srv.URL, "ghp_test")
	comments, err := client.ListComments(context.Background(), "octocat/hello-world#42")

	require.NoError(t, err)
	require.Len(t, comments, 2)

	assert.Equal(t, "101", comments[0].ID)
	assert.Equal(t, "alice", comments[0].Author)
	assert.Equal(t, "First comment", comments[0].Body)

	assert.Equal(t, "102", comments[1].ID)
	assert.Equal(t, "bob", comments[1].Author)
	assert.Equal(t, "Second comment", comments[1].Body)
}

func TestDoRequest_authHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer ghp_secret_token", r.Header.Get("Authorization"))

		_, _ = fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()

	client := New(srv.URL, "ghp_secret_token")
	_, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "octocat/hello-world",
		MaxResults: 10,
	})

	require.NoError(t, err)
}

func TestDeleteIssue_happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, "/repos/octocat/hello-world/issues/42", r.URL.Path)

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var payload map[string]string
		require.NoError(t, json.Unmarshal(body, &payload))
		assert.Equal(t, "closed", payload["state"])

		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"number":42,"state":"closed"}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "ghp_test")
	err := client.DeleteIssue(context.Background(), "octocat/hello-world#42")

	require.NoError(t, err)
}

func TestDeleteIssue_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := New(srv.URL, "ghp_test")
	err := client.DeleteIssue(context.Background(), "octocat/hello-world#42")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "returned")
}

func TestDeleteIssue_invalidKey(t *testing.T) {
	client := New("http://localhost", "ghp_test")
	err := client.DeleteIssue(context.Background(), "badkey")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid issue key format")
}

func TestListComments_empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()

	client := New(srv.URL, "ghp_test")
	comments, err := client.ListComments(context.Background(), "octocat/hello-world#42")

	require.NoError(t, err)
	assert.Empty(t, comments)
}

func TestTransitionIssue_noop(t *testing.T) {
	client := New("http://localhost", "ghp_test")
	err := client.TransitionIssue(context.Background(), "octocat/hello-world#1", "In Progress")

	require.NoError(t, err)
}

func TestAssignIssue_happy(t *testing.T) {
	var gotBody map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, "/repos/octocat/hello-world/issues/1", r.URL.Path)

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &gotBody))

		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"number":1}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "ghp_test")
	err := client.AssignIssue(context.Background(), "octocat/hello-world#1", "octocat")

	require.NoError(t, err)
	assert.Equal(t, map[string][]string{"assignees": {"octocat"}}, gotBody)
}

func TestAssignIssue_error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer srv.Close()

	client := New(srv.URL, "ghp_test")
	err := client.AssignIssue(context.Background(), "octocat/hello-world#1", "octocat")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "returned")
}

func TestGetCurrentUser_happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/user", r.URL.Path)

		_, _ = fmt.Fprint(w, `{"login":"octocat"}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "ghp_test")
	login, err := client.GetCurrentUser(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "octocat", login)
}

func TestGetCurrentUser_error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := New(srv.URL, "ghp_test")
	_, err := client.GetCurrentUser(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "returned")
}

func TestEditIssue_happy(t *testing.T) {
	title := "Updated Title"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, "/repos/octocat/repo/issues/1", r.URL.Path)

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var got map[string]string
		require.NoError(t, json.Unmarshal(body, &got))
		assert.Equal(t, "Updated Title", got["title"])

		_, _ = fmt.Fprint(w, `{"number":1,"title":"Updated Title","body":"desc","state":"open"}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "ghp_test")
	issue, err := client.EditIssue(context.Background(), "octocat/repo#1", tracker.EditOptions{Title: &title})

	require.NoError(t, err)
	assert.Equal(t, "octocat/repo#1", issue.Key)
	assert.Equal(t, "Updated Title", issue.Title)
}

func TestEditIssue_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	title := "X"
	client := New(srv.URL, "ghp_test")
	_, err := client.EditIssue(context.Background(), "octocat/repo#1", tracker.EditOptions{Title: &title})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "returned")
}
