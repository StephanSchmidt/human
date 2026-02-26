package azuredevops

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
	client := New("https://dev.azure.com", "myorg", "pat-test")
	client.SetHTTPDoer(&errDoer{err: fmt.Errorf("connection refused")})

	_, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "MyProject",
		MaxResults: 10,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "requesting Azure DevOps")
}

func TestDoRequest_nilResponse(t *testing.T) {
	client := New("https://dev.azure.com", "myorg", "pat-test")
	client.SetHTTPDoer(&nilDoer{})

	_, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "MyProject",
		MaxResults: 10,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil response")
}

func TestDoRequest_invalidBaseURL(t *testing.T) {
	client := New("ftp://dev.azure.com", "myorg", "pat-test")

	_, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "MyProject",
		MaxResults: 10,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "scheme must be http or https")
}

func TestListIssues_happy(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// WIQL query
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/myorg/Human/_apis/wit/wiql", r.URL.Path)
			assert.Equal(t, "7.1", r.URL.Query().Get("api-version"))
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			// Verify basic auth (empty username + PAT)
			user, pass, ok := r.BasicAuth()
			assert.True(t, ok)
			assert.Equal(t, "", user)
			assert.Equal(t, "pat-test", pass)

			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			assert.Contains(t, string(body), "System.TeamProject")
			assert.Contains(t, string(body), "Done")

			_, _ = fmt.Fprint(w, `{"workItems":[{"id":1,"url":"u1"},{"id":2,"url":"u2"}]}`)
			return
		}
		// Batch fetch
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/myorg/Human/_apis/wit/workitems", r.URL.Path)
		assert.Equal(t, "1,2", r.URL.Query().Get("ids"))

		_, _ = fmt.Fprint(w, `{"value":[
			{"id":1,"fields":{"System.Title":"Bug report","System.State":"New","System.WorkItemType":"Bug","System.AssignedTo":{"displayName":"Alice","uniqueName":"alice@example.com"},"System.CreatedBy":{"displayName":"Bob","uniqueName":"bob@example.com"},"Microsoft.VSTS.Common.Priority":2,"System.TeamProject":"Human"}},
			{"id":2,"fields":{"System.Title":"Feature request","System.State":"Active","System.WorkItemType":"Issue","System.CreatedBy":{"displayName":"Alice","uniqueName":"alice@example.com"},"Microsoft.VSTS.Common.Priority":0,"System.TeamProject":"Human"}}
		]}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "myorg", "pat-test")
	issues, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "Human",
		MaxResults: 50,
	})

	require.NoError(t, err)
	require.Len(t, issues, 2)

	assert.Equal(t, "Human/1", issues[0].Key)
	assert.Equal(t, "Bug report", issues[0].Summary)
	assert.Equal(t, "New", issues[0].Status)
	assert.Equal(t, "Bug", issues[0].Type)
	assert.Equal(t, "Alice", issues[0].Assignee)
	assert.Equal(t, "Bob", issues[0].Reporter)
	assert.Equal(t, "2", issues[0].Priority)

	assert.Equal(t, "Human/2", issues[1].Key)
	assert.Equal(t, "Feature request", issues[1].Summary)
	assert.Equal(t, "", issues[1].Assignee)
	assert.Equal(t, "", issues[1].Priority) // Priority 0 means not set
}

func TestListIssues_all(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			// IncludeAll should not have the Done/Removed filter
			assert.NotContains(t, string(body), "<> 'Done'")
			assert.NotContains(t, string(body), "<> 'Removed'")

			_, _ = fmt.Fprint(w, `{"workItems":[{"id":1,"url":"u1"}]}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"value":[
			{"id":1,"fields":{"System.Title":"Done item","System.State":"Done","System.WorkItemType":"Issue","Microsoft.VSTS.Common.Priority":0,"System.TeamProject":"Human"}}
		]}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "myorg", "pat-test")
	issues, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "Human",
		MaxResults: 50,
		IncludeAll: true,
	})

	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, "Done", issues[0].Status)
}

func TestListIssues_empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"workItems":[]}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "myorg", "pat-test")
	issues, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "Human",
		MaxResults: 10,
	})

	require.NoError(t, err)
	assert.Empty(t, issues)
}

func TestListIssues_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := New(srv.URL, "myorg", "pat-test")
	_, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "Human",
		MaxResults: 10,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "returned")
}

func TestListIssues_missingProject(t *testing.T) {
	client := New("http://localhost", "myorg", "pat-test")
	_, err := client.ListIssues(context.Background(), tracker.ListOptions{
		MaxResults: 10,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "project is required")
}

func TestGetIssue_happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/myorg/Human/_apis/wit/workitems/42", r.URL.Path)
		assert.Equal(t, "7.1", r.URL.Query().Get("api-version"))

		_, _ = fmt.Fprint(w, `{
			"id": 42,
			"fields": {
				"System.Title": "The answer",
				"System.Description": "## Description\n\nThis is markdown.",
				"System.State": "Active",
				"System.WorkItemType": "Issue",
				"System.AssignedTo": {"displayName": "Alice", "uniqueName": "alice@example.com"},
				"System.CreatedBy": {"displayName": "Bob", "uniqueName": "bob@example.com"},
				"Microsoft.VSTS.Common.Priority": 1,
				"System.TeamProject": "Human"
			}
		}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "myorg", "pat-test")
	issue, err := client.GetIssue(context.Background(), "Human/42")

	require.NoError(t, err)
	assert.Equal(t, "Human/42", issue.Key)
	assert.Equal(t, "The answer", issue.Summary)
	assert.Equal(t, "Active", issue.Status)
	assert.Equal(t, "Issue", issue.Type)
	assert.Equal(t, "1", issue.Priority)
	assert.Equal(t, "Alice", issue.Assignee)
	assert.Equal(t, "Bob", issue.Reporter)
	assert.Equal(t, "## Description\n\nThis is markdown.", issue.Description)
}

func TestGetIssue_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := New(srv.URL, "myorg", "pat-test")
	_, err := client.GetIssue(context.Background(), "Human/42")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "returned")
}

func TestGetIssue_invalidKey(t *testing.T) {
	client := New("http://localhost", "myorg", "pat-test")
	_, err := client.GetIssue(context.Background(), "noslash")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid issue key format")
}

func TestCreateIssue_happy(t *testing.T) {
	var gotOps []patchOp
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/myorg/Human/_apis/wit/workitems/$Issue", r.URL.Path)
		assert.Equal(t, "application/json-patch+json", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &gotOps))

		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"id":99,"fields":{"System.Title":"New issue","System.Description":"Some description","System.State":"New","System.WorkItemType":"Issue","Microsoft.VSTS.Common.Priority":0,"System.TeamProject":"Human"}}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "myorg", "pat-test")
	issue, err := client.CreateIssue(context.Background(), &tracker.Issue{
		Project:     "Human",
		Summary:     "New issue",
		Description: "Some description",
	})

	require.NoError(t, err)
	assert.Equal(t, "Human/99", issue.Key)
	assert.Equal(t, "Human", issue.Project)
	assert.Equal(t, "New issue", issue.Summary)
	assert.Equal(t, "Some description", issue.Description)

	require.Len(t, gotOps, 2)
	assert.Equal(t, "add", gotOps[0].Op)
	assert.Equal(t, "/fields/System.Title", gotOps[0].Path)
	assert.Equal(t, "New issue", gotOps[0].Value)
	assert.Equal(t, "/fields/System.Description", gotOps[1].Path)
}

func TestCreateIssue_withoutDescription(t *testing.T) {
	var gotOps []patchOp
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &gotOps))

		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"id":100,"fields":{"System.Title":"No desc","System.State":"New","System.WorkItemType":"Issue","Microsoft.VSTS.Common.Priority":0,"System.TeamProject":"Human"}}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "myorg", "pat-test")
	issue, err := client.CreateIssue(context.Background(), &tracker.Issue{
		Project: "Human",
		Summary: "No desc",
	})

	require.NoError(t, err)
	assert.Equal(t, "Human/100", issue.Key)
	// Only title op, no description
	require.Len(t, gotOps, 1)
	assert.Equal(t, "/fields/System.Title", gotOps[0].Path)
}

func TestCreateIssue_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	client := New(srv.URL, "myorg", "pat-test")
	_, err := client.CreateIssue(context.Background(), &tracker.Issue{
		Project: "Human",
		Summary: "Will fail",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "returned")
}

func TestDeleteIssue_happy(t *testing.T) {
	var gotOps []patchOp
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, "/myorg/Human/_apis/wit/workitems/42", r.URL.Path)
		assert.Equal(t, "application/json-patch+json", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &gotOps))

		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"id":42,"fields":{"System.State":"Done"}}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "myorg", "pat-test")
	err := client.DeleteIssue(context.Background(), "Human/42")

	require.NoError(t, err)
	require.Len(t, gotOps, 1)
	assert.Equal(t, "add", gotOps[0].Op)
	assert.Equal(t, "/fields/System.State", gotOps[0].Path)
	assert.Equal(t, "Done", gotOps[0].Value)
}

func TestDeleteIssue_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := New(srv.URL, "myorg", "pat-test")
	err := client.DeleteIssue(context.Background(), "Human/42")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "returned")
}

func TestDeleteIssue_invalidKey(t *testing.T) {
	client := New("http://localhost", "myorg", "pat-test")
	err := client.DeleteIssue(context.Background(), "badkey")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid issue key format")
}

func TestAddComment_happy(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/myorg/Human/_apis/wit/workItems/42/comments", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &gotBody))

		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{
			"id": 101,
			"text": "Hello world",
			"createdBy": {"displayName": "Alice", "uniqueName": "alice@example.com"},
			"createdDate": "2025-01-15T10:30:00Z"
		}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "myorg", "pat-test")
	comment, err := client.AddComment(context.Background(), "Human/42", "Hello world")

	require.NoError(t, err)
	assert.Equal(t, "101", comment.ID)
	assert.Equal(t, "Alice", comment.Author)
	assert.Equal(t, "Hello world", comment.Body)
	assert.False(t, comment.Created.IsZero())

	assert.Equal(t, "Hello world", gotBody["text"])
}

func TestAddComment_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	client := New(srv.URL, "myorg", "pat-test")
	_, err := client.AddComment(context.Background(), "Human/42", "test")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "returned")
}

func TestAddComment_invalidKey(t *testing.T) {
	client := New("http://localhost", "myorg", "pat-test")
	_, err := client.AddComment(context.Background(), "badkey", "test")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid issue key format")
}

func TestListComments_happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/myorg/Human/_apis/wit/workItems/42/comments", r.URL.Path)

		_, _ = fmt.Fprint(w, `{"comments":[
			{"id": 101, "text": "First comment", "createdBy": {"displayName": "Alice"}, "createdDate": "2025-01-15T10:30:00Z"},
			{"id": 102, "text": "Second comment", "createdBy": {"displayName": "Bob"}, "createdDate": "2025-01-16T11:00:00Z"}
		]}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "myorg", "pat-test")
	comments, err := client.ListComments(context.Background(), "Human/42")

	require.NoError(t, err)
	require.Len(t, comments, 2)

	assert.Equal(t, "101", comments[0].ID)
	assert.Equal(t, "Alice", comments[0].Author)
	assert.Equal(t, "First comment", comments[0].Body)

	assert.Equal(t, "102", comments[1].ID)
	assert.Equal(t, "Bob", comments[1].Author)
	assert.Equal(t, "Second comment", comments[1].Body)
}

func TestListComments_empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"comments":[]}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "myorg", "pat-test")
	comments, err := client.ListComments(context.Background(), "Human/42")

	require.NoError(t, err)
	assert.Empty(t, comments)
}

func TestDoRequest_authHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "", user)
		assert.Equal(t, "my-secret-pat", pass)

		_, _ = fmt.Fprint(w, `{"workItems":[]}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "myorg", "my-secret-pat")
	_, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "Human",
		MaxResults: 10,
	})

	require.NoError(t, err)
}

func Test_parseIssueKey(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		wantProj string
		wantID   int
		wantErr  string
	}{
		{name: "valid", key: "Human/2", wantProj: "Human", wantID: 2},
		{name: "project with spaces", key: "My Project/99", wantProj: "My Project", wantID: 99},
		{name: "bare number", key: "2", wantErr: "invalid issue key format"},
		{name: "empty", key: "", wantErr: "invalid issue key format"},
		{name: "empty project", key: "/2", wantErr: "invalid issue key format"},
		{name: "non-numeric id", key: "Human/abc", wantErr: "invalid work item ID"},
		{name: "trailing slash", key: "Human/", wantErr: "invalid work item ID"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proj, id, err := parseIssueKey(tt.key)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantProj, proj)
			assert.Equal(t, tt.wantID, id)
		})
	}
}
