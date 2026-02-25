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
