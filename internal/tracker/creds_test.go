package tracker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCredSpecForKind_known(t *testing.T) {
	kinds := []string{"jira", "github", "gitlab", "linear", "azuredevops", "shortcut"}
	for _, kind := range kinds {
		spec, ok := CredSpecForKind(kind)
		require.True(t, ok, "expected spec for kind %s", kind)
		assert.Equal(t, kind, spec.Kind)
		assert.NotEmpty(t, spec.Required)
		assert.NotEmpty(t, spec.Label)
		assert.NotEmpty(t, spec.HelpURL)
	}
}

func TestCredSpecForKind_unknown(t *testing.T) {
	_, ok := CredSpecForKind("unknown")
	assert.False(t, ok)
}

func TestCheckCredsEnv_allSet(t *testing.T) {
	spec := CredSpec{
		Kind: "github", EnvPrefix: "GITHUB", Required: []string{"TOKEN"},
	}

	result := CheckCredsEnv(spec, func(key string) string {
		if key == "GITHUB_TOKEN" {
			return "ghp_test123"
		}
		return ""
	})

	assert.True(t, result.Complete)
	assert.Equal(t, "ghp_test123", result.Available["TOKEN"])
	assert.Empty(t, result.Missing)
}

func TestCheckCredsEnv_partial(t *testing.T) {
	spec := CredSpec{
		Kind: "jira", EnvPrefix: "JIRA", Required: []string{"KEY", "USER"},
	}

	result := CheckCredsEnv(spec, func(key string) string {
		if key == "JIRA_KEY" {
			return "some-key"
		}
		return ""
	})

	assert.False(t, result.Complete)
	assert.Equal(t, "some-key", result.Available["KEY"])
	assert.Equal(t, []string{"USER"}, result.Missing)
}

func TestCheckCredsEnv_noneSet(t *testing.T) {
	spec := CredSpec{
		Kind: "github", EnvPrefix: "GITHUB", Required: []string{"TOKEN"},
	}

	result := CheckCredsEnv(spec, func(_ string) string { return "" })

	assert.False(t, result.Complete)
	assert.Empty(t, result.Available)
	assert.Equal(t, []string{"TOKEN"}, result.Missing)
}

func TestFormatMissingCreds(t *testing.T) {
	spec := CredSpec{
		Kind: "jira", EnvPrefix: "JIRA", Label: "Jira",
		Required: []string{"KEY", "USER"},
		HelpURL:  "https://example.com/tokens",
	}
	result := CredResult{
		Spec:    spec,
		Missing: []string{"KEY", "USER"},
	}
	parsed := &ParsedURL{BaseURL: "https://myco.atlassian.net"}

	msg := FormatMissingCreds(result, parsed)

	assert.Contains(t, msg, "Jira")
	assert.Contains(t, msg, "https://myco.atlassian.net")
	assert.Contains(t, msg, "JIRA_KEY")
	assert.Contains(t, msg, "JIRA_USER")
	assert.Contains(t, msg, "https://example.com/tokens")
}
