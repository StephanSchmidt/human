package jira

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

func Test_hasDescription(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
		want bool
	}{
		{"nil raw message", nil, false},
		{"empty raw message", json.RawMessage{}, false},
		{"null string", json.RawMessage(`null`), false},
		{"valid JSON object", json.RawMessage(`{"type":"doc"}`), true},
		{"empty JSON object", json.RawMessage(`{}`), true},
		{"string value", json.RawMessage(`"hello"`), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, hasDescription(tt.raw))
		})
	}
}

func Test_nameOrEmpty(t *testing.T) {
	tests := []struct {
		name  string
		field *nameField
		want  string
	}{
		{"nil returns empty", nil, ""},
		{"display name preferred", &nameField{DisplayName: "Alice", Name: "alice"}, "Alice"},
		{"falls back to name", &nameField{Name: "bob"}, "bob"},
		{"both empty returns empty", &nameField{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, nameOrEmpty(tt.field))
		})
	}
}

func TestCreateIssue_happy(t *testing.T) {
	var gotBody createRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/rest/api/3/issue", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &gotBody))

		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{"id":"10001","key":"KAN-42"}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "user@example.com", "token")
	issue, err := client.CreateIssue(context.Background(), &tracker.Issue{
		Project:     "KAN",
		Type:        "Task",
		Summary:     "Test issue",
		Description: "Some description",
	})

	require.NoError(t, err)
	assert.Equal(t, "KAN-42", issue.Key)
	assert.Equal(t, "KAN", issue.Project)
	assert.Equal(t, "Task", issue.Type)
	assert.Equal(t, "Test issue", issue.Summary)
	assert.Equal(t, "Some description", issue.Description)

	assert.Equal(t, "KAN", gotBody.Fields.Project.Key)
	assert.Equal(t, "Task", gotBody.Fields.IssueType.Name)
	assert.Equal(t, "Test issue", gotBody.Fields.Summary)
	assert.NotNil(t, gotBody.Fields.Description)
}

func TestCreateIssue_withoutDescription(t *testing.T) {
	var gotBody createRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &gotBody))

		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{"id":"10002","key":"KAN-43"}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "user@example.com", "token")
	issue, err := client.CreateIssue(context.Background(), &tracker.Issue{
		Project: "KAN",
		Type:    "Bug",
		Summary: "No description issue",
	})

	require.NoError(t, err)
	assert.Equal(t, "KAN-43", issue.Key)
	assert.Nil(t, gotBody.Fields.Description)
}

func TestCreateIssue_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	client := New(srv.URL, "user@example.com", "token")
	_, err := client.CreateIssue(context.Background(), &tracker.Issue{
		Project: "KAN",
		Type:    "Task",
		Summary: "Will fail",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status")
}

func TestListIssues_happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/rest/api/3/search/jql", r.URL.Path)
		assert.Contains(t, r.URL.Query().Get("jql"), "project=KAN")
		assert.Equal(t, "10", r.URL.Query().Get("maxResults"))

		_, _ = fmt.Fprint(w, `{"issues":[
			{"key":"KAN-1","fields":{"summary":"First issue","status":{"name":"To Do"}}},
			{"key":"KAN-2","fields":{"summary":"Second issue","status":{"name":"In Progress"}}}
		]}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "user@example.com", "token")
	issues, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "KAN",
		MaxResults: 10,
	})

	require.NoError(t, err)
	require.Len(t, issues, 2)

	assert.Equal(t, "KAN-1", issues[0].Key)
	assert.Equal(t, "First issue", issues[0].Summary)
	assert.Equal(t, "To Do", issues[0].Status)

	assert.Equal(t, "KAN-2", issues[1].Key)
	assert.Equal(t, "Second issue", issues[1].Summary)
	assert.Equal(t, "In Progress", issues[1].Status)
}

func TestListIssues_emptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"issues":[]}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "user@example.com", "token")
	issues, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "KAN",
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

	client := New(srv.URL, "user@example.com", "token")
	_, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "KAN",
		MaxResults: 10,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status")
}

func TestGetIssue_happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/rest/api/3/issue/KAN-42", r.URL.Path)

		_, _ = fmt.Fprint(w, `{
			"key": "KAN-42",
			"fields": {
				"summary": "The answer",
				"status": {"name": "Done"},
				"priority": {"displayName": "High", "name": "High"},
				"assignee": {"displayName": "Alice", "name": "alice"},
				"reporter": {"displayName": "Bob", "name": "bob"},
				"description": {"version":1,"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Hello world"}]}]}
			}
		}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "user@example.com", "token")
	issue, err := client.GetIssue(context.Background(), "KAN-42")

	require.NoError(t, err)
	assert.Equal(t, "KAN-42", issue.Key)
	assert.Equal(t, "The answer", issue.Summary)
	assert.Equal(t, "Done", issue.Status)
	assert.Equal(t, "High", issue.Priority)
	assert.Equal(t, "Alice", issue.Assignee)
	assert.Equal(t, "Bob", issue.Reporter)
	assert.Contains(t, issue.Description, "Hello world")
}

func TestGetIssue_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := New(srv.URL, "user@example.com", "token")
	_, err := client.GetIssue(context.Background(), "KAN-42")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status")
}

func TestAddComment_happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/rest/api/3/issue/KAN-1/comment", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var got commentBody
		require.NoError(t, json.Unmarshal(body, &got))
		assert.NotNil(t, got.Body, "body should be ADF document")

		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{
			"id": "10042",
			"author": {"displayName": "Alice"},
			"body": {"version":1,"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Hello world"}]}]},
			"created": "2025-01-15T10:30:00.000+0000"
		}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "user@example.com", "token")
	comment, err := client.AddComment(context.Background(), "KAN-1", "Hello world")

	require.NoError(t, err)
	assert.Equal(t, "10042", comment.ID)
	assert.Equal(t, "Alice", comment.Author)
	assert.Contains(t, comment.Body, "Hello world")
	assert.False(t, comment.Created.IsZero())
}

func TestAddComment_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	client := New(srv.URL, "user@example.com", "token")
	_, err := client.AddComment(context.Background(), "KAN-1", "test")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status")
}

func TestListComments_happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/rest/api/3/issue/KAN-1/comment", r.URL.Path)

		_, _ = fmt.Fprint(w, `{"comments":[
			{
				"id": "10001",
				"author": {"displayName": "Alice"},
				"body": {"version":1,"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"First comment"}]}]},
				"created": "2025-01-15T10:30:00.000+0000"
			},
			{
				"id": "10002",
				"author": {"displayName": "Bob"},
				"body": {"version":1,"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Second comment"}]}]},
				"created": "2025-01-16T11:00:00.000+0000"
			}
		]}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "user@example.com", "token")
	comments, err := client.ListComments(context.Background(), "KAN-1")

	require.NoError(t, err)
	require.Len(t, comments, 2)

	assert.Equal(t, "10001", comments[0].ID)
	assert.Equal(t, "Alice", comments[0].Author)
	assert.Contains(t, comments[0].Body, "First comment")

	assert.Equal(t, "10002", comments[1].ID)
	assert.Equal(t, "Bob", comments[1].Author)
	assert.Contains(t, comments[1].Body, "Second comment")
}

func TestListComments_empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"comments":[]}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "user@example.com", "token")
	comments, err := client.ListComments(context.Background(), "KAN-1")

	require.NoError(t, err)
	assert.Empty(t, comments)
}
