package tracker

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubProvider satisfies Provider with no-op methods.
type stubProvider struct{}

func (stubProvider) ListIssues(context.Context, ListOptions) ([]Issue, error)        { return nil, nil }
func (stubProvider) GetIssue(context.Context, string) (*Issue, error)                { return nil, nil }
func (stubProvider) CreateIssue(context.Context, *Issue) (*Issue, error)             { return nil, nil }
func (stubProvider) ListComments(context.Context, string) ([]Comment, error)         { return nil, nil }
func (stubProvider) AddComment(context.Context, string, string) (*Comment, error)    { return nil, nil }

func TestResolve_byName(t *testing.T) {
	instances := []Instance{
		{Name: "work", Kind: "jira", Provider: stubProvider{}},
		{Name: "personal", Kind: "github", Provider: stubProvider{}},
	}

	inst, err := Resolve("personal", instances, "")
	require.NoError(t, err)
	assert.Equal(t, "personal", inst.Name)
	assert.Equal(t, "github", inst.Kind)
}

func TestResolve_unknownName(t *testing.T) {
	instances := []Instance{
		{Name: "work", Kind: "jira", Provider: stubProvider{}},
	}

	_, err := Resolve("nonexistent", instances, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tracker name not found")
}

func TestResolve_duplicateName(t *testing.T) {
	instances := []Instance{
		{Name: "work", Kind: "jira", Provider: stubProvider{}},
		{Name: "work", Kind: "github", Provider: stubProvider{}},
	}

	_, err := Resolve("work", instances, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ambiguous tracker name")
}

func TestResolve_autoDetectSingleKind(t *testing.T) {
	instances := []Instance{
		{Name: "work", Kind: "jira", Provider: stubProvider{}},
		{Name: "personal", Kind: "jira", Provider: stubProvider{}},
	}

	inst, err := Resolve("", instances, "")
	require.NoError(t, err)
	assert.Equal(t, "work", inst.Name)
}

func TestResolve_autoDetectMultipleKinds(t *testing.T) {
	instances := []Instance{
		{Name: "work", Kind: "jira", Provider: stubProvider{}},
		{Name: "personal", Kind: "github", Provider: stubProvider{}},
	}

	_, err := Resolve("", instances, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple tracker types configured")
}

func TestResolve_autoDetectNone(t *testing.T) {
	_, err := Resolve("", nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no tracker configured")
}

// --- DetectKind tests ---

func TestDetectKind_githubIssue(t *testing.T) {
	assert.Equal(t, "github", DetectKind("octocat/hello-world#42"))
	assert.Equal(t, "github", DetectKind("org/repo#1"))
	assert.Equal(t, "github", DetectKind("my.org/my-repo#999"))
}

func TestDetectKind_githubRepo(t *testing.T) {
	assert.Equal(t, "github", DetectKind("octocat/hello-world"))
	assert.Equal(t, "github", DetectKind("org/repo"))
}

func TestDetectKind_jiraKey(t *testing.T) {
	assert.Equal(t, "", DetectKind("KAN-1"))
	assert.Equal(t, "", DetectKind("PROJ-123"))
}

func TestDetectKind_linearKey(t *testing.T) {
	assert.Equal(t, "", DetectKind("ENG-123"))
}

func TestDetectKind_empty(t *testing.T) {
	assert.Equal(t, "", DetectKind(""))
}

// --- Key-hint resolution tests ---

func TestResolve_keyHintSelectsGitHub(t *testing.T) {
	instances := []Instance{
		{Name: "work", Kind: "jira", Provider: stubProvider{}},
		{Name: "personal", Kind: "github", Provider: stubProvider{}},
	}

	inst, err := Resolve("", instances, "octocat/repo#1")
	require.NoError(t, err)
	assert.Equal(t, "personal", inst.Name)
	assert.Equal(t, "github", inst.Kind)
}

func TestResolve_keyHintGitHubRepoKey(t *testing.T) {
	instances := []Instance{
		{Name: "work", Kind: "jira", Provider: stubProvider{}},
		{Name: "oss", Kind: "github", Provider: stubProvider{}},
	}

	inst, err := Resolve("", instances, "octocat/repo")
	require.NoError(t, err)
	assert.Equal(t, "oss", inst.Name)
	assert.Equal(t, "github", inst.Kind)
}

func TestResolve_keyHintGitHubNoInstance(t *testing.T) {
	instances := []Instance{
		{Name: "work", Kind: "jira", Provider: stubProvider{}},
	}

	_, err := Resolve("", instances, "octocat/repo#1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no tracker of detected kind configured")
}

func TestResolve_keyHintNonGitHubFallsBack(t *testing.T) {
	instances := []Instance{
		{Name: "work", Kind: "jira", Provider: stubProvider{}},
		{Name: "personal", Kind: "github", Provider: stubProvider{}},
	}

	// Jira-style key — DetectKind returns "", so falls back to multi-kind error
	_, err := Resolve("", instances, "KAN-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple tracker types configured")
}

func TestResolve_keyHintNonGitHubSingleKind(t *testing.T) {
	instances := []Instance{
		{Name: "work", Kind: "jira", Provider: stubProvider{}},
		{Name: "other", Kind: "jira", Provider: stubProvider{}},
	}

	// Jira-style key, single kind — auto-detect succeeds
	inst, err := Resolve("", instances, "KAN-1")
	require.NoError(t, err)
	assert.Equal(t, "work", inst.Name)
}
