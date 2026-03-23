package index

import (
	"bytes"
	"context"
	"testing"

	"github.com/StephanSchmidt/human/internal/tracker"
)

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

func (m *mockProvider) AddComment(_ context.Context, _ string, _ string) (*tracker.Comment, error) {
	return nil, nil
}

func (m *mockProvider) DeleteIssue(_ context.Context, _ string) error {
	return nil
}

func (m *mockProvider) TransitionIssue(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockProvider) AssignIssue(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockProvider) GetCurrentUser(_ context.Context) (string, error) {
	return "", nil
}

func (m *mockProvider) EditIssue(_ context.Context, _ string, _ tracker.EditOptions) (*tracker.Issue, error) {
	return nil, nil
}

func (m *mockProvider) ListStatuses(_ context.Context, _ string) ([]tracker.Status, error) {
	return nil, nil
}

func TestSync_indexesIssues(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	var buf bytes.Buffer

	provider := &mockProvider{
		listFn: func(_ context.Context, opts tracker.ListOptions) ([]tracker.Issue, error) {
			return []tracker.Issue{
				{Key: opts.Project + "-1", Title: "Issue one"},
				{Key: opts.Project + "-2", Title: "Issue two"},
			}, nil
		},
		getFn: func(_ context.Context, key string) (*tracker.Issue, error) {
			return &tracker.Issue{Key: key, Title: "Title for " + key, Description: "Desc for " + key, Status: "Open"}, nil
		},
	}

	instances := []tracker.Instance{
		{Name: "work", Kind: "jira", URL: "https://jira.example.com", Projects: []string{"KAN"}, Provider: provider},
	}

	result, err := Sync(ctx, s, instances, false, &buf)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Indexed != 2 {
		t.Errorf("expected 2 indexed, got %d", result.Indexed)
	}
	if result.Errors != 0 {
		t.Errorf("expected 0 errors, got %d", result.Errors)
	}

	keys, _ := s.AllKeys(ctx, "work")
	if len(keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(keys))
	}
}

func TestSync_prunesStaleEntries(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	var buf bytes.Buffer

	// Pre-populate a stale entry.
	_ = s.UpsertEntry(ctx, Entry{Key: "KAN-99", Source: "work", Kind: "jira"}, "old")

	provider := &mockProvider{
		listFn: func(_ context.Context, _ tracker.ListOptions) ([]tracker.Issue, error) {
			return []tracker.Issue{{Key: "KAN-1"}}, nil
		},
		getFn: func(_ context.Context, key string) (*tracker.Issue, error) {
			return &tracker.Issue{Key: key, Title: "Current"}, nil
		},
	}

	instances := []tracker.Instance{
		{Name: "work", Kind: "jira", Projects: []string{"KAN"}, Provider: provider},
	}

	result, _ := Sync(ctx, s, instances, false, &buf)
	if result.Pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", result.Pruned)
	}

	keys, _ := s.AllKeys(ctx, "work")
	if len(keys) != 1 || keys[0] != "KAN-1" {
		t.Errorf("expected [KAN-1], got %v", keys)
	}
}

func TestSync_skipsInstanceWithoutProjects(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	var buf bytes.Buffer

	provider := &mockProvider{
		listFn: func(_ context.Context, _ tracker.ListOptions) ([]tracker.Issue, error) {
			t.Fatal("ListIssues should not be called for instance without projects")
			return nil, nil
		},
	}

	instances := []tracker.Instance{
		{Name: "empty", Kind: "jira", Provider: provider},
	}

	result, _ := Sync(ctx, s, instances, false, &buf)
	if result.Indexed != 0 {
		t.Errorf("expected 0 indexed, got %d", result.Indexed)
	}
}

func TestSync_handlesListError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	var buf bytes.Buffer

	errorProvider := &mockProvider{
		listFn: func(_ context.Context, _ tracker.ListOptions) ([]tracker.Issue, error) {
			return nil, context.DeadlineExceeded
		},
	}
	okProvider := &mockProvider{
		listFn: func(_ context.Context, _ tracker.ListOptions) ([]tracker.Issue, error) {
			return []tracker.Issue{{Key: "ENG-1"}}, nil
		},
		getFn: func(_ context.Context, key string) (*tracker.Issue, error) {
			return &tracker.Issue{Key: key, Title: "OK"}, nil
		},
	}

	instances := []tracker.Instance{
		{Name: "broken", Kind: "jira", Projects: []string{"BAD"}, Provider: errorProvider},
		{Name: "working", Kind: "linear", Projects: []string{"ENG"}, Provider: okProvider},
	}

	result, _ := Sync(ctx, s, instances, false, &buf)
	if result.Errors != 1 {
		t.Errorf("expected 1 error, got %d", result.Errors)
	}
	if result.Indexed != 1 {
		t.Errorf("expected 1 indexed from working instance, got %d", result.Indexed)
	}
}

func TestSync_handlesGetError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	var buf bytes.Buffer

	provider := &mockProvider{
		listFn: func(_ context.Context, _ tracker.ListOptions) ([]tracker.Issue, error) {
			return []tracker.Issue{{Key: "KAN-1"}, {Key: "KAN-2"}}, nil
		},
		getFn: func(_ context.Context, key string) (*tracker.Issue, error) {
			if key == "KAN-1" {
				return nil, context.DeadlineExceeded
			}
			return &tracker.Issue{Key: key, Title: "OK"}, nil
		},
	}

	instances := []tracker.Instance{
		{Name: "work", Kind: "jira", Projects: []string{"KAN"}, Provider: provider},
	}

	result, _ := Sync(ctx, s, instances, false, &buf)
	if result.Errors != 1 {
		t.Errorf("expected 1 error (KAN-1 fetch), got %d", result.Errors)
	}
	if result.Indexed != 1 {
		t.Errorf("expected 1 indexed (KAN-2), got %d", result.Indexed)
	}
}

func TestSync_emptyInstances(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	var buf bytes.Buffer

	result, err := Sync(ctx, s, nil, false, &buf)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Indexed != 0 || result.Pruned != 0 || result.Errors != 0 {
		t.Errorf("expected all zeros, got %+v", result)
	}
}
