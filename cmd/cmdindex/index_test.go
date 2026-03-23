package cmdindex

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/StephanSchmidt/human/internal/index"
	"github.com/StephanSchmidt/human/internal/tracker"
)

// testDeps returns IndexDeps with an in-memory store.
func testDeps(t *testing.T) (IndexDeps, *index.SQLiteStore) {
	t.Helper()
	store, err := index.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	deps := IndexDeps{
		LoadInstances: func(_ string) ([]tracker.Instance, error) {
			return nil, nil
		},
		DBPath: func() string { return ":memory:" },
		NewStore: func(_ string) (index.Store, error) {
			return store, nil
		},
	}
	return deps, store
}

func seedStore(t *testing.T, store *index.SQLiteStore) {
	t.Helper()
	ctx := context.Background()
	_ = store.UpsertEntry(ctx, index.Entry{
		Key: "KAN-42", Source: "work", Kind: "jira", Project: "KAN",
		Title: "Implement retry logic", Status: "In Progress", Assignee: "alice",
	}, "webhook delivery retry mechanism")
	_ = store.UpsertEntry(ctx, index.Entry{
		Key: "ENG-7", Source: "eng", Kind: "linear", Project: "ENG",
		Title: "Fix login page", Status: "Open", Assignee: "bob",
	}, "OAuth2 login flow broken on mobile")
}

func TestRunSearch_agentOutput(t *testing.T) {
	deps, store := testDeps(t)
	seedStore(t, store)

	var buf bytes.Buffer
	err := RunSearch(context.Background(), &buf, "retry", 10, false, false, deps)
	if err != nil {
		t.Fatalf("RunSearch: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "KAN-42: Implement retry logic") {
		t.Errorf("expected KAN-42 in output, got:\n%s", out)
	}
	if !strings.Contains(out, "human get KAN-42") {
		t.Errorf("expected 'human get KAN-42' hint, got:\n%s", out)
	}
}

func TestRunSearch_jsonOutput(t *testing.T) {
	deps, store := testDeps(t)
	seedStore(t, store)

	var buf bytes.Buffer
	err := RunSearch(context.Background(), &buf, "retry", 10, true, false, deps)
	if err != nil {
		t.Fatalf("RunSearch: %v", err)
	}

	var entries []index.Entry
	if err := json.Unmarshal(buf.Bytes(), &entries); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, buf.String())
	}
	if len(entries) != 1 || entries[0].Key != "KAN-42" {
		t.Errorf("expected [KAN-42], got %v", entries)
	}
}

func TestRunSearch_tableOutput(t *testing.T) {
	deps, store := testDeps(t)
	seedStore(t, store)

	var buf bytes.Buffer
	err := RunSearch(context.Background(), &buf, "retry", 10, false, true, deps)
	if err != nil {
		t.Fatalf("RunSearch: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "KEY") || !strings.Contains(out, "TITLE") {
		t.Errorf("expected table headers, got:\n%s", out)
	}
	if !strings.Contains(out, "KAN-42") {
		t.Errorf("expected KAN-42 in table, got:\n%s", out)
	}
}

func TestRunSearch_noResults(t *testing.T) {
	deps, store := testDeps(t)
	seedStore(t, store)

	var buf bytes.Buffer
	err := RunSearch(context.Background(), &buf, "nonexistent", 10, false, false, deps)
	if err != nil {
		t.Fatalf("RunSearch: %v", err)
	}

	if !strings.Contains(buf.String(), "No results found") {
		t.Errorf("expected 'No results found', got:\n%s", buf.String())
	}
}

func TestRunIndex_syncsAllInstances(t *testing.T) {
	deps, _ := testDeps(t)

	provider := &mockProvider{
		listFn: func(_ context.Context, opts tracker.ListOptions) ([]tracker.Issue, error) {
			return []tracker.Issue{{Key: opts.Project + "-1"}}, nil
		},
		getFn: func(_ context.Context, key string) (*tracker.Issue, error) {
			return &tracker.Issue{Key: key, Title: "Test"}, nil
		},
	}

	deps.LoadInstances = func(_ string) ([]tracker.Instance, error) {
		return []tracker.Instance{
			{Name: "work", Kind: "jira", Projects: []string{"KAN"}, Provider: provider},
		}, nil
	}

	var buf bytes.Buffer
	err := RunIndex(context.Background(), &buf, "", false, deps)
	if err != nil {
		t.Fatalf("RunIndex: %v", err)
	}

	if !strings.Contains(buf.String(), "1 indexed") {
		t.Errorf("expected '1 indexed', got:\n%s", buf.String())
	}
}

func TestRunIndex_filtersSource(t *testing.T) {
	deps, _ := testDeps(t)

	jiraProvider := &mockProvider{
		listFn: func(_ context.Context, _ tracker.ListOptions) ([]tracker.Issue, error) {
			return []tracker.Issue{{Key: "KAN-1"}}, nil
		},
		getFn: func(_ context.Context, key string) (*tracker.Issue, error) {
			return &tracker.Issue{Key: key, Title: "Jira issue"}, nil
		},
	}
	linearProvider := &mockProvider{
		listFn: func(_ context.Context, _ tracker.ListOptions) ([]tracker.Issue, error) {
			t.Fatal("linear should not be called when filtering by jira")
			return nil, nil
		},
	}

	deps.LoadInstances = func(_ string) ([]tracker.Instance, error) {
		return []tracker.Instance{
			{Name: "work", Kind: "jira", Projects: []string{"KAN"}, Provider: jiraProvider},
			{Name: "eng", Kind: "linear", Projects: []string{"ENG"}, Provider: linearProvider},
		}, nil
	}

	var buf bytes.Buffer
	err := RunIndex(context.Background(), &buf, "jira", false, deps)
	if err != nil {
		t.Fatalf("RunIndex: %v", err)
	}
}

func TestRunIndexStatus_showsStats(t *testing.T) {
	deps, store := testDeps(t)
	seedStore(t, store)

	var buf bytes.Buffer
	err := RunIndexStatus(context.Background(), &buf, deps)
	if err != nil {
		t.Fatalf("RunIndexStatus: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Total entries: 2") {
		t.Errorf("expected 'Total entries: 2', got:\n%s", out)
	}
	if !strings.Contains(out, "jira") {
		t.Errorf("expected 'jira' in stats, got:\n%s", out)
	}
}

// mockProvider implements tracker.Provider for testing.
type mockProvider struct {
	listFn func(ctx context.Context, opts tracker.ListOptions) ([]tracker.Issue, error)
	getFn  func(ctx context.Context, key string) (*tracker.Issue, error)
}

func (m *mockProvider) ListIssues(ctx context.Context, opts tracker.ListOptions) ([]tracker.Issue, error) {
	return m.listFn(ctx, opts)
}
func (m *mockProvider) GetIssue(ctx context.Context, key string) (*tracker.Issue, error) {
	return m.getFn(ctx, key)
}
func (m *mockProvider) CreateIssue(_ context.Context, _ *tracker.Issue) (*tracker.Issue, error) {
	return nil, nil
}
func (m *mockProvider) ListComments(_ context.Context, _ string) ([]tracker.Comment, error) {
	return nil, nil
}
func (m *mockProvider) AddComment(_ context.Context, _, _ string) (*tracker.Comment, error) {
	return nil, nil
}
func (m *mockProvider) DeleteIssue(_ context.Context, _ string) error { return nil }
func (m *mockProvider) TransitionIssue(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockProvider) AssignIssue(_ context.Context, _, _ string) error { return nil }
func (m *mockProvider) GetCurrentUser(_ context.Context) (string, error) {
	return "", nil
}
func (m *mockProvider) EditIssue(_ context.Context, _ string, _ tracker.EditOptions) (*tracker.Issue, error) {
	return nil, nil
}
func (m *mockProvider) ListStatuses(_ context.Context, _ string) ([]tracker.Status, error) {
	return nil, nil
}
