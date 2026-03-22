package linear

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/StephanSchmidt/human/internal/apiclient"
	"github.com/StephanSchmidt/human/internal/tracker"

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

	var req apiclient.GraphQLRequest
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

func TestDoGraphQL_invalidBaseURL(t *testing.T) {
	client := New("ftp://api.linear.app", "lin_test")

	_, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "ENG",
		MaxResults: 10,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "scheme must be http or https")
}

func TestListIssues_happy(t *testing.T) {
	issuesResponse := `{"data":{"issues":{"nodes":[
		{"identifier":"ENG-1","title":"First issue","description":"desc1",
		 "state":{"name":"In Progress"},"priorityLabel":"High",
		 "assignee":{"name":"Alice"},"creator":{"name":"Bob"},
		 "labels":{"nodes":[{"name":"bug"}]}},
		{"identifier":"ENG-2","title":"Second issue","description":"desc2",
		 "state":{"name":"Todo"},"priorityLabel":"Low",
		 "assignee":null,"creator":{"name":"Charlie"},
		 "labels":{"nodes":[]}}
	]}}}`

	srv := httptest.NewServer(&graphQLHandler{
		t: t,
		handlers: map[string]func(vars map[string]any) string{
			"completed": func(vars map[string]any) string {
				assert.Equal(t, "ENG", vars["teamKey"])
				assert.EqualValues(t, 50, vars["first"])
				return issuesResponse
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
	assert.Equal(t, "First issue", issues[0].Title)
	assert.Equal(t, "In Progress", issues[0].Status)
	assert.Equal(t, "High", issues[0].Priority)
	assert.Equal(t, "Alice", issues[0].Assignee)
	assert.Equal(t, "Bob", issues[0].Reporter)
	assert.Equal(t, "bug", issues[0].Type)

	assert.Equal(t, "ENG-2", issues[1].Key)
	assert.Equal(t, "", issues[1].Assignee)
	assert.Equal(t, "", issues[1].Type)
}

func TestListIssues_all(t *testing.T) {
	srv := httptest.NewServer(&graphQLHandler{
		t: t,
		handlers: map[string]func(vars map[string]any) string{
			"issues(": func(vars map[string]any) string {
				assert.Equal(t, "ENG", vars["teamKey"])
				return `{"data":{"issues":{"nodes":[
					{"identifier":"ENG-1","title":"Open issue","description":"",
					 "state":{"name":"In Progress"},"priorityLabel":"",
					 "assignee":null,"creator":null,"labels":{"nodes":[]}},
					{"identifier":"ENG-2","title":"Done issue","description":"",
					 "state":{"name":"Done"},"priorityLabel":"",
					 "assignee":null,"creator":null,"labels":{"nodes":[]}}
				]}}}`
			},
		},
	})
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	issues, err := client.ListIssues(context.Background(), tracker.ListOptions{
		Project:    "ENG",
		MaxResults: 50,
		IncludeAll: true,
	})

	require.NoError(t, err)
	require.Len(t, issues, 2)
	assert.Equal(t, "In Progress", issues[0].Status)
	assert.Equal(t, "Done", issues[1].Status)
}

func TestListIssues_emptyResult(t *testing.T) {
	srv := httptest.NewServer(&graphQLHandler{
		t: t,
		handlers: map[string]func(vars map[string]any) string{
			"completed": func(_ map[string]any) string {
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
	assert.Contains(t, err.Error(), "returned")
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
	assert.Equal(t, "The answer", issue.Title)
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
	assert.Contains(t, err.Error(), "returned")
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
		Title:       "New issue",
		Description: "Some description",
	})

	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
	assert.Equal(t, "ENG-99", issue.Key)
	assert.Equal(t, "ENG", issue.Project)
	assert.Equal(t, "New issue", issue.Title)
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
		Title:   "No desc",
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
		Title:   "Will fail",
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
		Title:   "Will fail",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "returned")
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
		Title:   "Will fail",
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
				Key: "ENG-1", Project: "ENG", Title: "Title",
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
				Key: "ENG-2", Project: "ENG", Title: "No people",
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
				Key: "ENG-3", Project: "ENG", Title: "No labels",
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

func TestAddComment_happy(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(&graphQLHandler{
		t: t,
		handlers: map[string]func(vars map[string]any) string{
			"issue(": func(vars map[string]any) string {
				callCount++
				assert.Equal(t, "ENG-42", vars["id"])
				return `{"data":{"issue":{"id":"issue-uuid-123"}}}`
			},
			"commentCreate(": func(vars map[string]any) string {
				callCount++
				assert.Equal(t, "issue-uuid-123", vars["issueId"])
				assert.Equal(t, "Hello world", vars["body"])
				return `{"data":{"commentCreate":{"success":true,"comment":{
					"id":"comment-uuid-1","body":"Hello world",
					"createdAt":"2025-01-15T10:30:00Z",
					"user":{"name":"Alice"}
				}}}}`
			},
		},
	})
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	comment, err := client.AddComment(context.Background(), "ENG-42", "Hello world")

	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
	assert.Equal(t, "comment-uuid-1", comment.ID)
	assert.Equal(t, "Alice", comment.Author)
	assert.Equal(t, "Hello world", comment.Body)
	assert.False(t, comment.Created.IsZero())
}

func TestAddComment_httpError(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		if callCount == 1 {
			// Issue ID lookup succeeds.
			_, _ = fmt.Fprint(w, `{"data":{"issue":{"id":"issue-uuid-123"}}}`)
			return
		}
		// Comment creation returns HTTP error.
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	_, err := client.AddComment(context.Background(), "ENG-42", "test")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "returned")
}

func TestListComments_happy(t *testing.T) {
	srv := httptest.NewServer(&graphQLHandler{
		t: t,
		handlers: map[string]func(vars map[string]any) string{
			"issue(": func(vars map[string]any) string {
				assert.Equal(t, "ENG-42", vars["id"])
				return `{"data":{"issue":{"comments":{"nodes":[
					{"id":"c1","body":"First comment","createdAt":"2025-01-15T10:30:00Z","user":{"name":"Alice"}},
					{"id":"c2","body":"Second comment","createdAt":"2025-01-16T11:00:00Z","user":{"name":"Bob"}}
				]}}}}`
			},
		},
	})
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	comments, err := client.ListComments(context.Background(), "ENG-42")

	require.NoError(t, err)
	require.Len(t, comments, 2)

	assert.Equal(t, "c1", comments[0].ID)
	assert.Equal(t, "Alice", comments[0].Author)
	assert.Equal(t, "First comment", comments[0].Body)

	assert.Equal(t, "c2", comments[1].ID)
	assert.Equal(t, "Bob", comments[1].Author)
	assert.Equal(t, "Second comment", comments[1].Body)
}

func TestDeleteIssue_happy(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(&graphQLHandler{
		t: t,
		handlers: map[string]func(vars map[string]any) string{
			"issue(": func(vars map[string]any) string {
				callCount++
				assert.Equal(t, "ENG-42", vars["id"])
				return `{"data":{"issue":{"id":"issue-uuid-123"}}}`
			},
			"issueDelete(": func(vars map[string]any) string {
				callCount++
				assert.Equal(t, "issue-uuid-123", vars["id"])
				return `{"data":{"issueDelete":{"success":true}}}`
			},
		},
	})
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	err := client.DeleteIssue(context.Background(), "ENG-42")

	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestDeleteIssue_httpError(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		if callCount == 1 {
			_, _ = fmt.Fprint(w, `{"data":{"issue":{"id":"issue-uuid-123"}}}`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	err := client.DeleteIssue(context.Background(), "ENG-42")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "returned")
}

func TestDeleteIssue_serverReturnsFailure(t *testing.T) {
	srv := httptest.NewServer(&graphQLHandler{
		t: t,
		handlers: map[string]func(vars map[string]any) string{
			"issue(": func(_ map[string]any) string {
				return `{"data":{"issue":{"id":"issue-uuid-123"}}}`
			},
			"issueDelete(": func(_ map[string]any) string {
				return `{"data":{"issueDelete":{"success":false}}}`
			},
		},
	})
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	err := client.DeleteIssue(context.Background(), "ENG-42")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "deletion failed")
}

func TestTransitionIssue_happy(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(&graphQLHandler{
		t: t,
		handlers: map[string]func(vars map[string]any) string{
			"issue(id:": func(vars map[string]any) string {
				callCount++
				assert.Equal(t, "ENG-1", vars["id"])
				return `{"data":{"issue":{"id":"issue-uuid-1"}}}`
			},
			"states": func(vars map[string]any) string {
				callCount++
				assert.Equal(t, "ENG", vars["key"])
				return `{"data":{"teams":{"nodes":[{"id":"team-1","states":{"nodes":[{"id":"state-1","name":"In Progress","type":"started"}]}}]}}}`
			},
			"issueUpdate(": func(vars map[string]any) string {
				callCount++
				assert.Equal(t, "issue-uuid-1", vars["id"])
				return `{"data":{"issueUpdate":{"success":true}}}`
			},
		},
	})
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	err := client.TransitionIssue(context.Background(), "ENG-1", "In Progress")

	require.NoError(t, err)
	assert.Equal(t, 3, callCount)
}

func TestTransitionIssue_stateNotFound(t *testing.T) {
	srv := httptest.NewServer(&graphQLHandler{
		t: t,
		handlers: map[string]func(vars map[string]any) string{
			"issue(id:": func(_ map[string]any) string {
				return `{"data":{"issue":{"id":"issue-uuid-1"}}}`
			},
			"states": func(_ map[string]any) string {
				return `{"data":{"teams":{"nodes":[{"id":"team-1","states":{"nodes":[{"id":"state-1","name":"Done","type":"completed"}]}}]}}}`
			},
		},
	})
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	err := client.TransitionIssue(context.Background(), "ENG-1", "NoSuchState")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "target state not found")
}

func TestAssignIssue_happy(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(&graphQLHandler{
		t: t,
		handlers: map[string]func(vars map[string]any) string{
			"issue(id:": func(vars map[string]any) string {
				callCount++
				assert.Equal(t, "ENG-1", vars["id"])
				return `{"data":{"issue":{"id":"issue-uuid-1"}}}`
			},
			"issueUpdate(": func(vars map[string]any) string {
				callCount++
				assert.Equal(t, "issue-uuid-1", vars["id"])
				return `{"data":{"issueUpdate":{"success":true}}}`
			},
		},
	})
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	err := client.AssignIssue(context.Background(), "ENG-1", "user-uuid")

	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestAssignIssue_error(t *testing.T) {
	srv := httptest.NewServer(&graphQLHandler{
		t: t,
		handlers: map[string]func(vars map[string]any) string{
			"issue(id:": func(_ map[string]any) string {
				return `{"data":{"issue":{"id":"issue-uuid-1"}}}`
			},
			"issueUpdate(": func(_ map[string]any) string {
				return `{"data":{"issueUpdate":{"success":false}}}`
			},
		},
	})
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	err := client.AssignIssue(context.Background(), "ENG-1", "user-uuid")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "assign failed")
}

func TestGetCurrentUser_happy(t *testing.T) {
	srv := httptest.NewServer(&graphQLHandler{
		t: t,
		handlers: map[string]func(vars map[string]any) string{
			"viewer": func(_ map[string]any) string {
				return `{"data":{"viewer":{"id":"viewer-uuid"}}}`
			},
		},
	})
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	userID, err := client.GetCurrentUser(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "viewer-uuid", userID)
}

func TestGetCurrentUser_error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	_, err := client.GetCurrentUser(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "returned")
}

func TestListComments_empty(t *testing.T) {
	srv := httptest.NewServer(&graphQLHandler{
		t: t,
		handlers: map[string]func(vars map[string]any) string{
			"issue(": func(_ map[string]any) string {
				return `{"data":{"issue":{"comments":{"nodes":[]}}}}`
			},
		},
	})
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	comments, err := client.ListComments(context.Background(), "ENG-42")

	require.NoError(t, err)
	assert.Empty(t, comments)
}

func TestEditIssue_happy(t *testing.T) {
	title := "Updated Title"
	callCount := 0
	srv := httptest.NewServer(&graphQLHandler{
		t: t,
		handlers: map[string]func(vars map[string]any) string{
			"issueUpdate": func(vars map[string]any) string {
				callCount++
				input := vars["input"].(map[string]any)
				assert.Equal(t, "Updated Title", input["title"])
				return `{"data":{"issueUpdate":{"success":true}}}`
			},
			"issue(": func(_ map[string]any) string {
				callCount++
				if callCount <= 2 {
					// resolveIssueID call
					return `{"data":{"issue":{"id":"uuid-1"}}}`
				}
				// GetIssue call after edit
				return `{"data":{"issue":{
					"identifier":"ENG-42","title":"Updated Title","description":"",
					"state":{"name":"In Progress"},"priorityLabel":"High",
					"assignee":null,"creator":null,"labels":{"nodes":[]}
				}}}`
			},
		},
	})
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	issue, err := client.EditIssue(context.Background(), "ENG-42", tracker.EditOptions{Title: &title})

	require.NoError(t, err)
	assert.Equal(t, "ENG-42", issue.Key)
	assert.Equal(t, "Updated Title", issue.Title)
}

func TestEditIssue_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	title := "X"
	client := New(srv.URL, "lin_test")
	_, err := client.EditIssue(context.Background(), "ENG-42", tracker.EditOptions{Title: &title})

	require.Error(t, err)
}

func TestListStatuses_happy(t *testing.T) {
	srv := httptest.NewServer(&graphQLHandler{
		t: t,
		handlers: map[string]func(vars map[string]any) string{
			"states": func(vars map[string]any) string {
				assert.Equal(t, "ENG", vars["key"])
				return `{"data":{"teams":{"nodes":[{"id":"team-1","states":{"nodes":[
					{"id":"s1","name":"Backlog","type":"backlog"},
					{"id":"s2","name":"In Progress","type":"started"},
					{"id":"s3","name":"Done","type":"completed"},
					{"id":"s4","name":"Cancelled","type":"canceled"}
				]}}]}}}`
			},
		},
	})
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	statuses, err := client.ListStatuses(context.Background(), "ENG-1")

	require.NoError(t, err)
	require.Len(t, statuses, 4)

	assert.Equal(t, "Backlog", statuses[0].Name)
	assert.Equal(t, "unstarted", statuses[0].Type)

	assert.Equal(t, "In Progress", statuses[1].Name)
	assert.Equal(t, "started", statuses[1].Type)

	assert.Equal(t, "Done", statuses[2].Name)
	assert.Equal(t, "done", statuses[2].Type)

	assert.Equal(t, "Cancelled", statuses[3].Name)
	assert.Equal(t, "closed", statuses[3].Type)
}

func TestListStatuses_emptyStates(t *testing.T) {
	srv := httptest.NewServer(&graphQLHandler{
		t: t,
		handlers: map[string]func(vars map[string]any) string{
			"states": func(_ map[string]any) string {
				return `{"data":{"teams":{"nodes":[{"id":"team-1","states":{"nodes":[]}}]}}}`
			},
		},
	})
	defer srv.Close()

	client := New(srv.URL, "lin_test")
	statuses, err := client.ListStatuses(context.Background(), "ENG-1")

	require.NoError(t, err)
	assert.Empty(t, statuses)
}

func TestListStatuses_invalidIssueKey(t *testing.T) {
	client := New("http://localhost", "lin_test")
	_, err := client.ListStatuses(context.Background(), "nohyphen")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot determine team from issue key")
}
