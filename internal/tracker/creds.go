package tracker

import (
	"fmt"
	"os"
	"strings"
)

// CredSpec describes the credentials required for a tracker kind.
type CredSpec struct {
	Kind      string   // "jira", "github", etc.
	EnvPrefix string   // "JIRA", "GITHUB", etc.
	Required  []string // env var suffixes: ["KEY", "USER"] for Jira, ["TOKEN"] for GitHub
	Label     string   // Human-readable name
	HelpURL   string   // Where to generate tokens
}

// CredResult tells the caller what credentials are available.
type CredResult struct {
	Spec      CredSpec
	Available map[string]string // suffix → value, for env vars that are set
	Missing   []string          // suffixes that are not set
	Complete  bool              // true if all required vars are set
}

// credSpecs maps tracker kinds to their credential requirements.
var credSpecs = map[string]CredSpec{
	"jira": {
		Kind: "jira", EnvPrefix: "JIRA", Label: "Jira",
		Required: []string{"KEY", "USER"},
		HelpURL:  "https://id.atlassian.com/manage-profile/security/api-tokens",
	},
	"github": {
		Kind: "github", EnvPrefix: "GITHUB", Label: "GitHub",
		Required: []string{"TOKEN"},
		HelpURL:  "https://github.com/settings/tokens",
	},
	"gitlab": {
		Kind: "gitlab", EnvPrefix: "GITLAB", Label: "GitLab",
		Required: []string{"TOKEN"},
		HelpURL:  "https://gitlab.com/-/user_settings/personal_access_tokens",
	},
	"linear": {
		Kind: "linear", EnvPrefix: "LINEAR", Label: "Linear",
		Required: []string{"TOKEN"},
		HelpURL:  "https://linear.app/settings/api",
	},
	"azuredevops": {
		Kind: "azuredevops", EnvPrefix: "AZURE", Label: "Azure DevOps",
		Required: []string{"TOKEN"},
		HelpURL:  "https://dev.azure.com/_usersSettings/tokens",
	},
	"shortcut": {
		Kind: "shortcut", EnvPrefix: "SHORTCUT", Label: "Shortcut",
		Required: []string{"TOKEN"},
		HelpURL:  "https://app.shortcut.com/settings/account/api-tokens",
	},
}

// CredSpecForKind returns the credential specification for a tracker kind.
func CredSpecForKind(kind string) (CredSpec, bool) {
	spec, ok := credSpecs[kind]
	return spec, ok
}

// CheckCreds checks which credentials are available in the environment.
// It checks global env vars (e.g. JIRA_KEY) for each required suffix.
func CheckCreds(spec CredSpec) CredResult {
	return CheckCredsEnv(spec, os.Getenv)
}

// CheckCredsEnv is like CheckCreds but accepts a custom env lookup function.
func CheckCredsEnv(spec CredSpec, getenv func(string) string) CredResult {
	result := CredResult{
		Spec:      spec,
		Available: make(map[string]string),
		Complete:  true,
	}

	for _, suffix := range spec.Required {
		envName := spec.EnvPrefix + "_" + suffix
		val := getenv(envName)
		if val != "" {
			result.Available[suffix] = val
		} else {
			result.Missing = append(result.Missing, suffix)
			result.Complete = false
		}
	}

	return result
}

// FormatMissingCreds returns a user-friendly message about which env vars to set.
func FormatMissingCreds(result CredResult, parsed *ParsedURL) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Cannot fetch ticket from %s at %s\n", result.Spec.Label, parsed.BaseURL)
	fmt.Fprintln(&b, "  Set these environment variables:")
	for _, suffix := range result.Missing {
		envName := result.Spec.EnvPrefix + "_" + suffix
		fmt.Fprintf(&b, "    export %s=your-%s\n", envName, strings.ToLower(suffix))
	}
	if result.Spec.HelpURL != "" {
		fmt.Fprintf(&b, "\n  Generate credentials at: %s\n", result.Spec.HelpURL)
	}

	return b.String()
}
