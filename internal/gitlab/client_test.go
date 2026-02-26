package gitlab

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
	client := New("https://gitlab.com", "glpat-test")
	client.SetHTTPDoer(&errDoer{err: fmt.Errorf("connection refused")})

	_, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "mygroup/myproject",
		MaxResults: 10,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "requesting GitLab")
}

func TestDoRequest_nilResponse(t *testing.T) {
	client := New("https://gitlab.com", "glpat-test")
	client.SetHTTPDoer(&nilDoer{})

	_, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "mygroup/myproject",
		MaxResults: 10,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil response")
}

func TestDoRequest_invalidBaseURL(t *testing.T) {
	client := New("ftp://gitlab.com", "glpat-test")

	_, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "mygroup/myproject",
		MaxResults: 10,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "scheme must be http or https")
}

func TestListIssues_happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v4/projects/mygroup%2Fmyproject/issues", r.URL.RawPath)
		assert.Equal(t, "50", r.URL.Query().Get("per_page"))
		assert.Equal(t, "opened", r.URL.Query().Get("state"))
		assert.Equal(t, "glpat-test", r.Header.Get("PRIVATE-TOKEN"))

		_, _ = fmt.Fprint(w, `[
			{"iid":1,"project_id":100,"title":"Bug report","description":"desc1","state":"opened","author":{"id":1,"username":"alice"},"assignees":[{"id":2,"username":"bob"}],"labels":["bug"]},
			{"iid":2,"project_id":100,"title":"Feature request","description":"desc2","state":"opened","author":{"id":1,"username":"alice"},"assignees":[],"labels":[]}
		]`)
	}))
	defer srv.Close()

	client := New(srv.URL, "glpat-test")
	issues, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "mygroup/myproject",
		MaxResults: 50,
	})

	require.NoError(t, err)
	require.Len(t, issues, 2)

	assert.Equal(t, "mygroup/myproject#1", issues[0].Key)
	assert.Equal(t, "Bug report", issues[0].Summary)
	assert.Equal(t, "opened", issues[0].Status)
	assert.Equal(t, "bug", issues[0].Type)
	assert.Equal(t, "bob", issues[0].Assignee)
	assert.Equal(t, "alice", issues[0].Reporter)

	assert.Equal(t, "mygroup/myproject#2", issues[1].Key)
	assert.Equal(t, "", issues[1].Type)
	assert.Equal(t, "", issues[1].Assignee)
}

func TestListIssues_all(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "", r.URL.Query().Get("state"))

		_, _ = fmt.Fprint(w, `[
			{"iid":1,"project_id":100,"title":"Open issue","description":"","state":"opened","author":{"id":1,"username":"alice"},"assignees":[],"labels":[]},
			{"iid":2,"project_id":100,"title":"Closed issue","description":"","state":"closed","author":{"id":1,"username":"alice"},"assignees":[],"labels":[]}
		]`)
	}))
	defer srv.Close()

	client := New(srv.URL, "glpat-test")
	issues, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "mygroup/myproject",
		MaxResults: 50,
		IncludeAll: true,
	})

	require.NoError(t, err)
	require.Len(t, issues, 2)
	assert.Equal(t, "opened", issues[0].Status)
	assert.Equal(t, "closed", issues[1].Status)
}

func TestListIssues_emptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()

	client := New(srv.URL, "glpat-test")
	issues, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "mygroup/myproject",
		MaxResults: 10,
	})

	require.NoError(t, err)
	assert.Empty(t, issues)
}

func TestListIssues_invalidProject(t *testing.T) {
	client := New("http://localhost", "glpat-test")
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

	client := New(srv.URL, "glpat-test")
	_, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "mygroup/myproject",
		MaxResults: 10,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "returned")
}

func TestGetIssue_happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v4/projects/mygroup%2Fmyproject/issues/42", r.URL.RawPath)

		_, _ = fmt.Fprint(w, `{
			"iid": 42,
			"project_id": 100,
			"title": "The answer",
			"description": "## Description\n\nThis is markdown.",
			"state": "opened",
			"author": {"id": 1, "username": "alice"},
			"assignees": [{"id": 2, "username": "bob"}],
			"labels": ["enhancement", "help wanted"]
		}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "glpat-test")
	issue, err := client.GetIssue(context.Background(), "mygroup/myproject#42")

	require.NoError(t, err)
	assert.Equal(t, "mygroup/myproject#42", issue.Key)
	assert.Equal(t, "The answer", issue.Summary)
	assert.Equal(t, "opened", issue.Status)
	assert.Equal(t, "enhancement", issue.Type)
	assert.Equal(t, "", issue.Priority)
	assert.Equal(t, "bob", issue.Assignee)
	assert.Equal(t, "alice", issue.Reporter)
	assert.Equal(t, "## Description\n\nThis is markdown.", issue.Description)
}

func TestGetIssue_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := New(srv.URL, "glpat-test")
	_, err := client.GetIssue(context.Background(), "mygroup/myproject#42")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "returned")
}

func TestGetIssue_invalidKey(t *testing.T) {
	client := New("http://localhost", "glpat-test")
	_, err := client.GetIssue(context.Background(), "nohash")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid issue key format")
}

func TestCreateIssue_happy(t *testing.T) {
	var gotBody createRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v4/projects/mygroup%2Fmyproject/issues", r.URL.RawPath)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &gotBody))

		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{"iid":99,"title":"New issue","description":"Some description"}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "glpat-test")
	issue, err := client.CreateIssue(context.Background(), &tracker.Issue{
		Project:     "mygroup/myproject",
		Summary:     "New issue",
		Description: "Some description",
	})

	require.NoError(t, err)
	assert.Equal(t, "mygroup/myproject#99", issue.Key)
	assert.Equal(t, "mygroup/myproject", issue.Project)
	assert.Equal(t, "New issue", issue.Summary)
	assert.Equal(t, "Some description", issue.Description)

	assert.Equal(t, "New issue", gotBody.Title)
	assert.Equal(t, "Some description", gotBody.Description)
}

func TestCreateIssue_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer srv.Close()

	client := New(srv.URL, "glpat-test")
	_, err := client.CreateIssue(context.Background(), &tracker.Issue{
		Project: "mygroup/myproject",
		Summary: "Will fail",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "returned")
}

func TestAddComment_happy(t *testing.T) {
	var gotBody noteRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v4/projects/mygroup%2Fmyproject/issues/42/notes", r.URL.RawPath)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &gotBody))

		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{
			"id": 101,
			"body": "Hello world",
			"author": {"id": 1, "username": "alice"},
			"created_at": "2025-01-15T10:30:00Z",
			"system": false
		}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "glpat-test")
	comment, err := client.AddComment(context.Background(), "mygroup/myproject#42", "Hello world")

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

	client := New(srv.URL, "glpat-test")
	_, err := client.AddComment(context.Background(), "mygroup/myproject#42", "test")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "returned")
}

func TestListComments_happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v4/projects/mygroup%2Fmyproject/issues/42/notes", r.URL.RawPath)
		assert.Equal(t, "asc", r.URL.Query().Get("sort"))

		_, _ = fmt.Fprint(w, `[
			{"id": 101, "body": "First comment", "author": {"id": 1, "username": "alice"}, "created_at": "2025-01-15T10:30:00Z", "system": false},
			{"id": 102, "body": "assigned to @bob", "author": {"id": 1, "username": "alice"}, "created_at": "2025-01-15T10:31:00Z", "system": true},
			{"id": 103, "body": "Second comment", "author": {"id": 2, "username": "bob"}, "created_at": "2025-01-16T11:00:00Z", "system": false}
		]`)
	}))
	defer srv.Close()

	client := New(srv.URL, "glpat-test")
	comments, err := client.ListComments(context.Background(), "mygroup/myproject#42")

	require.NoError(t, err)
	require.Len(t, comments, 2)

	assert.Equal(t, "101", comments[0].ID)
	assert.Equal(t, "alice", comments[0].Author)
	assert.Equal(t, "First comment", comments[0].Body)

	assert.Equal(t, "103", comments[1].ID)
	assert.Equal(t, "bob", comments[1].Author)
	assert.Equal(t, "Second comment", comments[1].Body)
}

func TestDoRequest_authHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "glpat-secret-token", r.Header.Get("PRIVATE-TOKEN"))

		_, _ = fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()

	client := New(srv.URL, "glpat-secret-token")
	_, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "mygroup/myproject",
		MaxResults: 10,
	})

	require.NoError(t, err)
}

func TestDeleteIssue_happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/api/v4/projects/mygroup%2Fmyproject/issues/42", r.URL.RawPath)

		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := New(srv.URL, "glpat-test")
	err := client.DeleteIssue(context.Background(), "mygroup/myproject#42")

	require.NoError(t, err)
}

func TestDeleteIssue_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	client := New(srv.URL, "glpat-test")
	err := client.DeleteIssue(context.Background(), "mygroup/myproject#42")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "returned")
}

func TestDeleteIssue_invalidKey(t *testing.T) {
	client := New("http://localhost", "glpat-test")
	err := client.DeleteIssue(context.Background(), "nohash")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid issue key format")
}

func TestListComments_empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()

	client := New(srv.URL, "glpat-test")
	comments, err := client.ListComments(context.Background(), "mygroup/myproject#42")

	require.NoError(t, err)
	assert.Empty(t, comments)
}

func Test_splitProject(t *testing.T) {
	tests := []struct {
		name    string
		project string
		want    string
		wantErr string
	}{
		{name: "valid", project: "mygroup/myproject", want: "mygroup%2Fmyproject"},
		{name: "multi-level", project: "group/subgroup/project", want: "group%2Fsubgroup%2Fproject"},
		{name: "no slash", project: "noslash", wantErr: "invalid project format"},
		{name: "empty owner", project: "/repo", wantErr: "invalid project format"},
		{name: "empty repo", project: "owner/", wantErr: "invalid project format"},
		{name: "empty string", project: "", wantErr: "invalid project format"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := splitProject(tt.project)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_parseIssueKey(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		wantProj string
		wantIID  int
		wantErr  string
	}{
		{name: "valid", key: "mygroup/myproject#42", wantProj: "mygroup/myproject", wantIID: 42},
		{name: "multi-level", key: "group/subgroup/project#1", wantProj: "group/subgroup/project", wantIID: 1},
		{name: "large number", key: "org/repo#99999", wantProj: "org/repo", wantIID: 99999},
		{name: "no hash", key: "mygroup/myproject", wantErr: "invalid issue key format"},
		{name: "no slash", key: "noslash#42", wantErr: "invalid issue key format"},
		{name: "non-numeric", key: "mygroup/myproject#abc", wantErr: "invalid issue IID"},
		{name: "empty", key: "", wantErr: "invalid issue key format"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proj, iid, err := parseIssueKey(tt.key)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantProj, proj)
			assert.Equal(t, tt.wantIID, iid)
		})
	}
}
