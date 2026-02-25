package linear

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stephan-schmidt/human/internal/tracker"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// graphQLHandler routes responses by inspecting the query field in the request body.
type graphQLHandler struct {
	t        *testing.T
	handlers map[string]func(vars map[string]any) string
}

func (h *graphQLHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	assert.Equal(h.t, http.MethodPost, r.Method)
	assert.Equal(h.t, "/graphql", r.URL.Path)
	assert.Equal(h.t, "application/json", r.Header.Get("Content-Type"))

	body, err := io.ReadAll(r.Body)
	require.NoError(h.t, err)

	var req graphQLRequest
	require.NoError(h.t, json.Unmarshal(body, &req))

	for keyword, handler := range h.handlers {
		if containsQuery(req.Query, keyword) {
			_, _ = fmt.Fprint(w, handler(req.Variables))
			return
		}
	}

	h.t.Fatalf("unexpected query: %s", req.Query)
}

// containsQuery checks if the query string contains a keyword.
func containsQuery(query, keyword string) bool {
	return len(query) > 0 && len(keyword) > 0 && contains(query, keyword)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestListIssues_happy(t *testing.T) {
	srv := httptest.NewServer(&graphQLHandler{
		t: t,
		handlers: map[string]func(vars map[string]any) string{
			"issues(": func(vars map[string]any) string {
				assert.Equal(t, "ENG", vars["teamKey"])
				assert.EqualValues(t, 50, vars["first"])
				return `{"data":{"issues":{"nodes":[
					{"identifier":"ENG-1","title":"First issue","description":"desc1",
					 "state":{"name":"In Progress"},"priorityLabel":"High",
					 "assignee":{"name":"Alice"},"creator":{"name":"Bob"},
					 "labels":{"nodes":[{"name":"bug"}]}},
					{"identifier":"ENG-2","title":"Second issue","description":"desc2",
					 "state":{"name":"Todo"},"priorityLabel":"Low",
					 "assignee":null,"creator":{"name":"Charlie"},
					 "labels":{"nodes":[]}}
				]}}}`
			},
		},
	})
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	issues, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "ENG",
		MaxResults: 50,
	})

	require.NoError(t, err)
	require.Len(t, issues, 2)

	assert.Equal(t, "ENG-1", issues[0].Key)
	assert.Equal(t, "ENG", issues[0].Project)
	assert.Equal(t, "First issue", issues[0].Summary)
	assert.Equal(t, "In Progress", issues[0].Status)
	assert.Equal(t, "High", issues[0].Priority)
	assert.Equal(t, "Alice", issues[0].Assignee)
	assert.Equal(t, "Bob", issues[0].Reporter)
	assert.Equal(t, "bug", issues[0].Type)

	assert.Equal(t, "ENG-2", issues[1].Key)
	assert.Equal(t, "", issues[1].Assignee)
	assert.Equal(t, "", issues[1].Type)
}

func TestListIssues_emptyResult(t *testing.T) {
	srv := httptest.NewServer(&graphQLHandler{
		t: t,
		handlers: map[string]func(vars map[string]any) string{
			"issues(": func(_ map[string]any) string {
				return `{"data":{"issues":{"nodes":[]}}}`
			},
		},
	})
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	issues, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "ENG",
		MaxResults: 10,
	})

	require.NoError(t, err)
	assert.Empty(t, issues)
}

func TestListIssues_graphQLError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"data":null,"errors":[{"message":"Team not found"}]}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	_, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "NOPE",
		MaxResults: 10,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "graphql error")
	assert.Contains(t, err.Error(), "Team not found")
}

func TestListIssues_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	_, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "ENG",
		MaxResults: 10,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status")
}

func TestGetIssue_happy(t *testing.T) {
	srv := httptest.NewServer(&graphQLHandler{
		t: t,
		handlers: map[string]func(vars map[string]any) string{
			"issue(": func(vars map[string]any) string {
				assert.Equal(t, "ENG-42", vars["id"])
				return `{"data":{"issue":{
					"identifier":"ENG-42","title":"The answer","description":"## Desc\n\nMarkdown.",
					"state":{"name":"Done"},"priorityLabel":"Urgent",
					"assignee":{"name":"Alice"},"creator":{"name":"Bob"},
					"labels":{"nodes":[{"name":"enhancement"},{"name":"frontend"}]}
				}}}`
			},
		},
	})
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	issue, err := client.GetIssue(context.Background(), "ENG-42")

	require.NoError(t, err)
	assert.Equal(t, "ENG-42", issue.Key)
	assert.Equal(t, "ENG", issue.Project)
	assert.Equal(t, "The answer", issue.Summary)
	assert.Equal(t, "Done", issue.Status)
	assert.Equal(t, "Urgent", issue.Priority)
	assert.Equal(t, "Alice", issue.Assignee)
	assert.Equal(t, "Bob", issue.Reporter)
	assert.Equal(t, "enhancement", issue.Type)
	assert.Equal(t, "## Desc\n\nMarkdown.", issue.Description)
}

func TestGetIssue_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	_, err := client.GetIssue(context.Background(), "ENG-42")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status")
}

func TestGetIssue_graphQLError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"data":null,"errors":[{"message":"Entity not found"}]}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	_, err := client.GetIssue(context.Background(), "ENG-999")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Entity not found")
}

func TestCreateIssue_happy(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(&graphQLHandler{
		t: t,
		handlers: map[string]func(vars map[string]any) string{
			"teams(": func(vars map[string]any) string {
				callCount++
				assert.Equal(t, "ENG", vars["key"])
				return `{"data":{"teams":{"nodes":[{"id":"team-uuid-123"}]}}}`
			},
			"issueCreate(": func(vars map[string]any) string {
				callCount++
				assert.Equal(t, "team-uuid-123", vars["teamId"])
				assert.Equal(t, "New issue", vars["title"])
				assert.Equal(t, "Some description", vars["description"])
				return `{"data":{"issueCreate":{"success":true,"issue":{
					"identifier":"ENG-99","title":"New issue","description":"Some description",
					"state":{"name":""},"priorityLabel":"","assignee":null,"creator":null,
					"labels":{"nodes":[]}
				}}}}`
			},
		},
	})
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	issue, err := client.CreateIssue(context.Background(), &tracker.Issue{
		Project:     "ENG",
		Summary:     "New issue",
		Description: "Some description",
	})

	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
	assert.Equal(t, "ENG-99", issue.Key)
	assert.Equal(t, "ENG", issue.Project)
	assert.Equal(t, "New issue", issue.Summary)
	assert.Equal(t, "Some description", issue.Description)
}

func TestCreateIssue_withoutDescription(t *testing.T) {
	srv := httptest.NewServer(&graphQLHandler{
		t: t,
		handlers: map[string]func(vars map[string]any) string{
			"teams(": func(_ map[string]any) string {
				return `{"data":{"teams":{"nodes":[{"id":"team-uuid-123"}]}}}`
			},
			"issueCreate(": func(vars map[string]any) string {
				_, hasDesc := vars["description"]
				assert.False(t, hasDesc, "description should be omitted when empty")
				return `{"data":{"issueCreate":{"success":true,"issue":{
					"identifier":"ENG-100","title":"No desc","description":"",
					"state":{"name":""},"priorityLabel":"","assignee":null,"creator":null,
					"labels":{"nodes":[]}
				}}}}`
			},
		},
	})
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	issue, err := client.CreateIssue(context.Background(), &tracker.Issue{
		Project: "ENG",
		Summary: "No desc",
	})

	require.NoError(t, err)
	assert.Equal(t, "ENG-100", issue.Key)
	assert.Equal(t, "", issue.Description)
}

func TestCreateIssue_serverReturnsFailure(t *testing.T) {
	srv := httptest.NewServer(&graphQLHandler{
		t: t,
		handlers: map[string]func(vars map[string]any) string{
			"teams(": func(_ map[string]any) string {
				return `{"data":{"teams":{"nodes":[{"id":"team-uuid-123"}]}}}`
			},
			"issueCreate(": func(_ map[string]any) string {
				return `{"data":{"issueCreate":{"success":false,"issue":null}}}`
			},
		},
	})
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	_, err := client.CreateIssue(context.Background(), &tracker.Issue{
		Project: "ENG",
		Summary: "Will fail",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "creation failed")
}

func TestCreateIssue_httpError(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		if callCount == 1 {
			// Team lookup succeeds.
			_, _ = fmt.Fprint(w, `{"data":{"teams":{"nodes":[{"id":"tid"}]}}}`)
			return
		}
		// Create mutation returns HTTP error.
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	_, err := client.CreateIssue(context.Background(), &tracker.Issue{
		Project: "ENG",
		Summary: "Will fail",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status")
}

func TestCreateIssue_teamNotFound(t *testing.T) {
	srv := httptest.NewServer(&graphQLHandler{
		t: t,
		handlers: map[string]func(vars map[string]any) string{
			"teams(": func(_ map[string]any) string {
				return `{"data":{"teams":{"nodes":[]}}}`
			},
		},
	})
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	_, err := client.CreateIssue(context.Background(), &tracker.Issue{
		Project: "NOPE",
		Summary: "Will fail",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "team not found")
}

func TestCreateIssue_authHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Linear uses the API key directly, no Bearer prefix.
		assert.Equal(t, "lin_api_key_123", r.Header.Get("Authorization"))

		_, _ = fmt.Fprint(w, `{"data":{"teams":{"nodes":[{"id":"tid"}]}}}`)
	}))
	defer srv.Close()

	client := New(srv.URL, "lin_api_key_123")
	// Only need the first request (team lookup) to verify the header.
	_, _ = client.resolveTeamID(context.Background(), "ENG")
}

func Test_toTrackerIssue(t *testing.T) {
	tests := []struct {
		name    string
		input   linearIssue
		project string
		want    tracker.Issue
	}{
		{
			name: "full issue",
			input: linearIssue{
				Identifier:    "ENG-1",
				Title:         "Title",
				Description:   "Desc",
				State:         nameNode{Name: "In Progress"},
				PriorityLabel: "High",
				Assignee:      &nameNode{Name: "Alice"},
				Creator:       &nameNode{Name: "Bob"},
				Labels:        labelConnection{Nodes: []nameNode{{Name: "bug"}}},
			},
			project: "ENG",
			want: tracker.Issue{
				Key: "ENG-1", Project: "ENG", Summary: "Title",
				Status: "In Progress", Priority: "High",
				Assignee: "Alice", Reporter: "Bob", Type: "bug",
				Description: "Desc",
			},
		},
		{
			name: "nil assignee and creator",
			input: linearIssue{
				Identifier: "ENG-2",
				Title:      "No people",
				State:      nameNode{Name: "Todo"},
			},
			project: "ENG",
			want: tracker.Issue{
				Key: "ENG-2", Project: "ENG", Summary: "No people",
				Status: "Todo",
			},
		},
		{
			name: "empty labels",
			input: linearIssue{
				Identifier: "ENG-3",
				Title:      "No labels",
				State:      nameNode{Name: "Done"},
				Labels:     labelConnection{Nodes: []nameNode{}},
			},
			project: "ENG",
			want: tracker.Issue{
				Key: "ENG-3", Project: "ENG", Summary: "No labels",
				Status: "Done",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toTrackerIssue(tt.input, tt.project)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_projectFromIdentifier(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"ENG-123", "ENG"},
		{"A-B-1", "A-B"},
		{"nohyphen", ""},
		{"TEAM-1", "TEAM"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, projectFromIdentifier(tt.input))
		})
	}
}
