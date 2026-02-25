package tracker

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubProvider satisfies Provider with no-op methods.
type stubProvider struct{}

func (stubProvider) ListIssues(context.Context, ListOptions) ([]Issue, error) { return nil, nil }
func (stubProvider) GetIssue(context.Context, string) (*Issue, error)         { return nil, nil }
func (stubProvider) CreateIssue(context.Context, *Issue) (*Issue, error)      { return nil, nil }

func TestResolve_byName(t *testing.T) {
	instances := []Instance{
		{Name: "work", Kind: "jira", Provider: stubProvider{}},
		{Name: "personal", Kind: "github", Provider: stubProvider{}},
	}

	inst, err := Resolve("personal", instances)
	require.NoError(t, err)
	assert.Equal(t, "personal", inst.Name)
	assert.Equal(t, "github", inst.Kind)
}

func TestResolve_unknownName(t *testing.T) {
	instances := []Instance{
		{Name: "work", Kind: "jira", Provider: stubProvider{}},
	}

	_, err := Resolve("nonexistent", instances)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tracker name not found")
}

func TestResolve_duplicateName(t *testing.T) {
	instances := []Instance{
		{Name: "work", Kind: "jira", Provider: stubProvider{}},
		{Name: "work", Kind: "github", Provider: stubProvider{}},
	}

	_, err := Resolve("work", instances)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ambiguous tracker name")
}

func TestResolve_autoDetectSingleKind(t *testing.T) {
	instances := []Instance{
		{Name: "work", Kind: "jira", Provider: stubProvider{}},
		{Name: "personal", Kind: "jira", Provider: stubProvider{}},
	}

	inst, err := Resolve("", instances)
	require.NoError(t, err)
	assert.Equal(t, "work", inst.Name)
}

func TestResolve_autoDetectMultipleKinds(t *testing.T) {
	instances := []Instance{
		{Name: "work", Kind: "jira", Provider: stubProvider{}},
		{Name: "personal", Kind: "github", Provider: stubProvider{}},
	}

	_, err := Resolve("", instances)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple tracker types configured")
}

func TestResolve_autoDetectNone(t *testing.T) {
	_, err := Resolve("", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no tracker configured")
}
