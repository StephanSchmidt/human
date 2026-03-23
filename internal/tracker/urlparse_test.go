package tracker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsURL(t *testing.T) {
	assert.True(t, IsURL("https://github.com/owner/repo/issues/1"))
	assert.True(t, IsURL("http://jira.example.com/browse/KAN-1"))
	assert.False(t, IsURL("KAN-42"))
	assert.False(t, IsURL("owner/repo#42"))
	assert.False(t, IsURL(""))
}

func TestParseURL_Jira_BrowseURL(t *testing.T) {
	result, ok := ParseURL("https://amazingcto.atlassian.net/browse/HUM-4")
	require.True(t, ok)
	assert.Equal(t, "jira", result.Kind)
	assert.Equal(t, "https://amazingcto.atlassian.net", result.BaseURL)
	assert.Equal(t, "HUM-4", result.Key)
}

func TestParseURL_Jira_BoardWithSelectedIssue(t *testing.T) {
	result, ok := ParseURL("https://amazingcto.atlassian.net/jira/software/projects/HUM/boards/2?selectedIssue=HUM-4")
	require.True(t, ok)
	assert.Equal(t, "jira", result.Kind)
	assert.Equal(t, "https://amazingcto.atlassian.net", result.BaseURL)
	assert.Equal(t, "HUM-4", result.Key)
}

func TestParseURL_Jira_SelfHosted(t *testing.T) {
	result, ok := ParseURL("https://jira.mycompany.com/browse/PROJ-123")
	require.True(t, ok)
	assert.Equal(t, "jira", result.Kind)
	assert.Equal(t, "https://jira.mycompany.com", result.BaseURL)
	assert.Equal(t, "PROJ-123", result.Key)
}

func TestParseURL_GitHub_Issue(t *testing.T) {
	result, ok := ParseURL("https://github.com/octocat/hello-world/issues/42")
	require.True(t, ok)
	assert.Equal(t, "github", result.Kind)
	assert.Equal(t, "https://api.github.com", result.BaseURL)
	assert.Equal(t, "octocat/hello-world#42", result.Key)
}

func TestParseURL_GitHub_PullRequest(t *testing.T) {
	result, ok := ParseURL("https://github.com/octocat/hello-world/pull/7")
	require.True(t, ok)
	assert.Equal(t, "github", result.Kind)
	assert.Equal(t, "https://api.github.com", result.BaseURL)
	assert.Equal(t, "octocat/hello-world#7", result.Key)
}

func TestParseURL_GitLab_Issue(t *testing.T) {
	result, ok := ParseURL("https://gitlab.com/mygroup/myproject/-/issues/99")
	require.True(t, ok)
	assert.Equal(t, "gitlab", result.Kind)
	assert.Equal(t, "https://gitlab.com", result.BaseURL)
	assert.Equal(t, "mygroup/myproject#99", result.Key)
}

func TestParseURL_GitLab_NestedGroup(t *testing.T) {
	result, ok := ParseURL("https://gitlab.com/org/sub/project/-/issues/5")
	require.True(t, ok)
	assert.Equal(t, "gitlab", result.Kind)
	assert.Equal(t, "https://gitlab.com", result.BaseURL)
	assert.Equal(t, "org/sub/project#5", result.Key)
}

func TestParseURL_Linear(t *testing.T) {
	result, ok := ParseURL("https://linear.app/myteam/issue/ENG-123/some-title-slug")
	require.True(t, ok)
	assert.Equal(t, "linear", result.Kind)
	assert.Equal(t, "https://api.linear.app", result.BaseURL)
	assert.Equal(t, "ENG-123", result.Key)
}

func TestParseURL_Linear_NoSlug(t *testing.T) {
	result, ok := ParseURL("https://linear.app/myteam/issue/ENG-123")
	require.True(t, ok)
	assert.Equal(t, "linear", result.Kind)
	assert.Equal(t, "ENG-123", result.Key)
}

func TestParseURL_AzureDevOps(t *testing.T) {
	result, ok := ParseURL("https://dev.azure.com/myorg/myproject/_workitems/edit/42")
	require.True(t, ok)
	assert.Equal(t, "azuredevops", result.Kind)
	assert.Equal(t, "https://dev.azure.com", result.BaseURL)
	assert.Equal(t, "myproject/42", result.Key)
	assert.Equal(t, "myorg", result.Org)
}

func TestParseURL_Shortcut(t *testing.T) {
	result, ok := ParseURL("https://app.shortcut.com/myorg/story/123/some-title")
	require.True(t, ok)
	assert.Equal(t, "shortcut", result.Kind)
	assert.Equal(t, "https://api.app.shortcut.com", result.BaseURL)
	assert.Equal(t, "123", result.Key)
}

func TestParseURL_Shortcut_NoSlug(t *testing.T) {
	result, ok := ParseURL("https://app.shortcut.com/myorg/story/456")
	require.True(t, ok)
	assert.Equal(t, "shortcut", result.Kind)
	assert.Equal(t, "456", result.Key)
}

func TestParseURL_UnknownURL(t *testing.T) {
	_, ok := ParseURL("https://example.com/something")
	assert.False(t, ok)
}

func TestParseURL_InvalidURL(t *testing.T) {
	_, ok := ParseURL("not-a-url")
	assert.False(t, ok)
}

func TestParseURL_EmptyString(t *testing.T) {
	_, ok := ParseURL("")
	assert.False(t, ok)
}

func TestParseURL_GitHub_TooFewSegments(t *testing.T) {
	_, ok := ParseURL("https://github.com/octocat")
	assert.False(t, ok)
}

func TestParseURL_Jira_TrailingSlash(t *testing.T) {
	result, ok := ParseURL("https://example.atlassian.net/browse/TEAM-55/")
	require.True(t, ok)
	assert.Equal(t, "jira", result.Kind)
	assert.Equal(t, "TEAM-55", result.Key)
}

func TestParseURL_GitHub_WithQueryParams(t *testing.T) {
	result, ok := ParseURL("https://github.com/owner/repo/issues/10?tab=comments")
	require.True(t, ok)
	assert.Equal(t, "github", result.Kind)
	assert.Equal(t, "owner/repo#10", result.Key)
}
