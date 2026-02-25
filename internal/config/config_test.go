package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unsetEnv registers cleanup via t.Setenv then unsets the variable for the test.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	require.NoError(t, os.Unsetenv(key))
}

// writeConfig writes a .humanconfig.yaml file in dir with the given content.
func writeConfig(t *testing.T, dir, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".humanconfig.yaml"), []byte(content), 0o644))
}

func TestLoadJiraConfigs(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    []JiraConfig
		wantErr string
	}{
		{
			name: "single entry",
			yaml: "jiras:\n  - name: work\n    url: https://work.atlassian.net\n    user: me@work.com\n    key: tok1\n",
			want: []JiraConfig{
				{Name: "work", URL: "https://work.atlassian.net", User: "me@work.com", Key: "tok1"},
			},
		},
		{
			name: "multiple entries",
			yaml: "jiras:\n  - name: work\n    url: https://work.atlassian.net\n    user: me@work.com\n    key: tok1\n  - name: personal\n    url: https://personal.atlassian.net\n    user: me@personal.com\n    key: tok2\n",
			want: []JiraConfig{
				{Name: "work", URL: "https://work.atlassian.net", User: "me@work.com", Key: "tok1"},
				{Name: "personal", URL: "https://personal.atlassian.net", User: "me@personal.com", Key: "tok2"},
			},
		},
		{
			name: "empty list",
			yaml: "jiras: []\n",
			want: []JiraConfig{},
		},
		{
			name:    "invalid YAML",
			yaml:    ":\n  :\n  invalid: [unterminated",
			wantErr: "parsing config file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeConfig(t, dir, tt.yaml)

			got, err := LoadJiraConfigs(dir)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLoadJiraConfigs_missingFile(t *testing.T) {
	dir := t.TempDir()
	got, err := LoadJiraConfigs(dir)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestLoadJiraConfigs_extensionlessFallback(t *testing.T) {
	dir := t.TempDir()
	content := "jiras:\n  - name: work\n    url: https://work.atlassian.net\n    user: me@work.com\n    key: tok1\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".humanconfig"), []byte(content), 0o644))

	got, err := LoadJiraConfigs(dir)
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, "work", got[0].Name)
}

func TestLoadConfig_defaultsToFirst(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "jiras:\n  - name: first\n    url: https://first.atlassian.net\n    user: first@example.com\n    key: tok1\n  - name: second\n    url: https://second.atlassian.net\n    user: second@example.com\n    key: tok2\n")

	unsetEnv(t, "JIRA_URL")
	unsetEnv(t, "JIRA_USER")
	unsetEnv(t, "JIRA_KEY")

	err := LoadConfig(dir, "")
	require.NoError(t, err)

	assert.Equal(t, "https://first.atlassian.net", os.Getenv("JIRA_URL"))
	assert.Equal(t, "first@example.com", os.Getenv("JIRA_USER"))
	assert.Equal(t, "tok1", os.Getenv("JIRA_KEY"))
}

func TestLoadConfig_selectsByName(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "jiras:\n  - name: first\n    url: https://first.atlassian.net\n    user: first@example.com\n    key: tok1\n  - name: second\n    url: https://second.atlassian.net\n    user: second@example.com\n    key: tok2\n")

	unsetEnv(t, "JIRA_URL")
	unsetEnv(t, "JIRA_USER")
	unsetEnv(t, "JIRA_KEY")

	err := LoadConfig(dir, "second")
	require.NoError(t, err)

	assert.Equal(t, "https://second.atlassian.net", os.Getenv("JIRA_URL"))
	assert.Equal(t, "second@example.com", os.Getenv("JIRA_USER"))
	assert.Equal(t, "tok2", os.Getenv("JIRA_KEY"))
}

func TestLoadConfig_unknownName(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "jiras:\n  - name: work\n    url: https://work.atlassian.net\n    user: me@work.com\n    key: tok1\n")

	err := LoadConfig(dir, "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown jira config")
}

func TestLoadConfig_envOverridesConfig(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "jiras:\n  - name: work\n    url: https://work.atlassian.net\n    user: me@work.com\n    key: tok1\n")

	t.Setenv("JIRA_URL", "https://from-env.atlassian.net")
	unsetEnv(t, "JIRA_USER")
	unsetEnv(t, "JIRA_KEY")

	err := LoadConfig(dir, "")
	require.NoError(t, err)

	assert.Equal(t, "https://from-env.atlassian.net", os.Getenv("JIRA_URL"))
	assert.Equal(t, "me@work.com", os.Getenv("JIRA_USER"))
	assert.Equal(t, "tok1", os.Getenv("JIRA_KEY"))
}

func TestApplyEnvOverrides(t *testing.T) {
	tests := []struct {
		name   string
		cfg    JiraConfig
		envs   map[string]string
		want   JiraConfig
	}{
		{
			name: "overrides all fields",
			cfg:  JiraConfig{Name: "work", URL: "old-url", User: "old-user", Key: "old-key"},
			envs: map[string]string{
				"JIRA_WORK_URL":  "new-url",
				"JIRA_WORK_USER": "new-user",
				"JIRA_WORK_KEY":  "new-key",
			},
			want: JiraConfig{Name: "work", URL: "new-url", User: "new-user", Key: "new-key"},
		},
		{
			name: "unset env leaves config alone",
			cfg:  JiraConfig{Name: "work", URL: "orig-url", User: "orig-user", Key: "orig-key"},
			envs: map[string]string{},
			want: JiraConfig{Name: "work", URL: "orig-url", User: "orig-user", Key: "orig-key"},
		},
		{
			name: "uppercased name",
			cfg:  JiraConfig{Name: "my-org", URL: "old-url", User: "old-user", Key: "old-key"},
			envs: map[string]string{
				"JIRA_MY-ORG_KEY": "env-key",
			},
			want: JiraConfig{Name: "my-org", URL: "old-url", User: "old-user", Key: "env-key"},
		},
		{
			name: "empty name is a no-op",
			cfg:  JiraConfig{URL: "url", User: "user", Key: "key"},
			envs: map[string]string{},
			want: JiraConfig{URL: "url", User: "user", Key: "key"},
		},
		{
			name: "partial override",
			cfg:  JiraConfig{Name: "work", URL: "old-url", User: "old-user", Key: "old-key"},
			envs: map[string]string{
				"JIRA_WORK_KEY": "env-key",
			},
			want: JiraConfig{Name: "work", URL: "old-url", User: "old-user", Key: "env-key"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Unset all possible env vars to isolate tests.
			for _, suffix := range []string{"URL", "USER", "KEY"} {
				if tt.cfg.Name != "" {
					unsetEnv(t, "JIRA_"+tt.cfg.Name+"_"+suffix)
				}
			}
			for k, v := range tt.envs {
				t.Setenv(k, v)
			}

			cfg := tt.cfg
			applyEnvOverrides(&cfg)

			assert.Equal(t, tt.want, cfg)
		})
	}
}

func TestLoadConfig_instanceEnvOverride(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "jiras:\n  - name: work\n    url: https://work.atlassian.net\n    user: me@work.com\n    key: file-key\n")

	unsetEnv(t, "JIRA_URL")
	unsetEnv(t, "JIRA_USER")
	unsetEnv(t, "JIRA_KEY")
	t.Setenv("JIRA_WORK_KEY", "env-instance-key")

	err := LoadConfig(dir, "work")
	require.NoError(t, err)

	assert.Equal(t, "https://work.atlassian.net", os.Getenv("JIRA_URL"))
	assert.Equal(t, "me@work.com", os.Getenv("JIRA_USER"))
	assert.Equal(t, "env-instance-key", os.Getenv("JIRA_KEY"))
}

func TestLoadConfig_globalEnvOverridesInstanceEnv(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "jiras:\n  - name: work\n    url: https://work.atlassian.net\n    user: me@work.com\n    key: file-key\n")

	unsetEnv(t, "JIRA_URL")
	unsetEnv(t, "JIRA_USER")
	t.Setenv("JIRA_KEY", "global-key")
	t.Setenv("JIRA_WORK_KEY", "instance-key")

	err := LoadConfig(dir, "work")
	require.NoError(t, err)

	// Global JIRA_KEY takes priority over instance-specific JIRA_WORK_KEY.
	assert.Equal(t, "global-key", os.Getenv("JIRA_KEY"))
}

func TestLoadConfig_missingFile(t *testing.T) {
	dir := t.TempDir()
	err := LoadConfig(dir, "")
	assert.NoError(t, err)
}

func TestLoadConfig_emptyList(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "jiras: []\n")

	err := LoadConfig(dir, "")
	assert.NoError(t, err)
}

// --- GitHub config tests ---

func TestLoadGitHubConfigs(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    []GitHubConfig
		wantErr string
	}{
		{
			name: "single entry",
			yaml: "githubs:\n  - name: personal\n    url: https://api.github.com\n    token: ghp_abc\n",
			want: []GitHubConfig{
				{Name: "personal", URL: "https://api.github.com", Token: "ghp_abc"},
			},
		},
		{
			name: "multiple entries",
			yaml: "githubs:\n  - name: personal\n    url: https://api.github.com\n    token: ghp_abc\n  - name: work\n    url: https://github.example.com/api/v3\n    token: ghp_xyz\n",
			want: []GitHubConfig{
				{Name: "personal", URL: "https://api.github.com", Token: "ghp_abc"},
				{Name: "work", URL: "https://github.example.com/api/v3", Token: "ghp_xyz"},
			},
		},
		{
			name: "empty list",
			yaml: "githubs: []\n",
			want: []GitHubConfig{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeConfig(t, dir, tt.yaml)

			got, err := LoadGitHubConfigs(dir)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLoadGitHubConfigs_missingFile(t *testing.T) {
	dir := t.TempDir()
	got, err := LoadGitHubConfigs(dir)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestLoadGitHubConfig_defaultsToFirst(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "githubs:\n  - name: first\n    url: https://api.github.com\n    token: tok1\n  - name: second\n    url: https://ghe.example.com/api/v3\n    token: tok2\n")

	unsetEnv(t, "GITHUB_URL")
	unsetEnv(t, "GITHUB_TOKEN")

	err := LoadGitHubConfig(dir, "")
	require.NoError(t, err)

	assert.Equal(t, "https://api.github.com", os.Getenv("GITHUB_URL"))
	assert.Equal(t, "tok1", os.Getenv("GITHUB_TOKEN"))
}

func TestLoadGitHubConfig_selectsByName(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "githubs:\n  - name: first\n    url: https://api.github.com\n    token: tok1\n  - name: second\n    url: https://ghe.example.com/api/v3\n    token: tok2\n")

	unsetEnv(t, "GITHUB_URL")
	unsetEnv(t, "GITHUB_TOKEN")

	err := LoadGitHubConfig(dir, "second")
	require.NoError(t, err)

	assert.Equal(t, "https://ghe.example.com/api/v3", os.Getenv("GITHUB_URL"))
	assert.Equal(t, "tok2", os.Getenv("GITHUB_TOKEN"))
}

func TestLoadGitHubConfig_unknownName(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "githubs:\n  - name: personal\n    url: https://api.github.com\n    token: ghp_abc\n")

	err := LoadGitHubConfig(dir, "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown github config")
}

func TestLoadGitHubConfig_defaultURL(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "githubs:\n  - name: personal\n    token: ghp_abc\n")

	unsetEnv(t, "GITHUB_URL")
	unsetEnv(t, "GITHUB_TOKEN")

	err := LoadGitHubConfig(dir, "")
	require.NoError(t, err)

	assert.Equal(t, "https://api.github.com", os.Getenv("GITHUB_URL"))
	assert.Equal(t, "ghp_abc", os.Getenv("GITHUB_TOKEN"))
}

func TestApplyGitHubEnvOverrides(t *testing.T) {
	tests := []struct {
		name string
		cfg  GitHubConfig
		envs map[string]string
		want GitHubConfig
	}{
		{
			name: "overrides all fields",
			cfg:  GitHubConfig{Name: "work", URL: "old-url", Token: "old-token"},
			envs: map[string]string{
				"GITHUB_WORK_URL":   "new-url",
				"GITHUB_WORK_TOKEN": "new-token",
			},
			want: GitHubConfig{Name: "work", URL: "new-url", Token: "new-token"},
		},
		{
			name: "unset env leaves config alone",
			cfg:  GitHubConfig{Name: "work", URL: "orig-url", Token: "orig-token"},
			envs: map[string]string{},
			want: GitHubConfig{Name: "work", URL: "orig-url", Token: "orig-token"},
		},
		{
			name: "uppercased name",
			cfg:  GitHubConfig{Name: "my-org", URL: "old-url", Token: "old-token"},
			envs: map[string]string{
				"GITHUB_MY-ORG_TOKEN": "env-token",
			},
			want: GitHubConfig{Name: "my-org", URL: "old-url", Token: "env-token"},
		},
		{
			name: "empty name is a no-op",
			cfg:  GitHubConfig{URL: "url", Token: "token"},
			envs: map[string]string{},
			want: GitHubConfig{URL: "url", Token: "token"},
		},
		{
			name: "partial override",
			cfg:  GitHubConfig{Name: "work", URL: "old-url", Token: "old-token"},
			envs: map[string]string{
				"GITHUB_WORK_TOKEN": "env-token",
			},
			want: GitHubConfig{Name: "work", URL: "old-url", Token: "env-token"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, suffix := range []string{"URL", "TOKEN"} {
				if tt.cfg.Name != "" {
					unsetEnv(t, "GITHUB_"+tt.cfg.Name+"_"+suffix)
				}
			}
			for k, v := range tt.envs {
				t.Setenv(k, v)
			}

			cfg := tt.cfg
			applyGitHubEnvOverrides(&cfg)

			assert.Equal(t, tt.want, cfg)
		})
	}
}

func TestLoadGitHubConfig_instanceEnvOverride(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "githubs:\n  - name: work\n    url: https://api.github.com\n    token: file-token\n")

	unsetEnv(t, "GITHUB_URL")
	unsetEnv(t, "GITHUB_TOKEN")
	t.Setenv("GITHUB_WORK_TOKEN", "env-instance-token")

	err := LoadGitHubConfig(dir, "work")
	require.NoError(t, err)

	assert.Equal(t, "https://api.github.com", os.Getenv("GITHUB_URL"))
	assert.Equal(t, "env-instance-token", os.Getenv("GITHUB_TOKEN"))
}

func TestLoadGitHubConfig_globalEnvOverridesInstance(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "githubs:\n  - name: work\n    url: https://api.github.com\n    token: file-token\n")

	unsetEnv(t, "GITHUB_URL")
	t.Setenv("GITHUB_TOKEN", "global-token")
	t.Setenv("GITHUB_WORK_TOKEN", "instance-token")

	err := LoadGitHubConfig(dir, "work")
	require.NoError(t, err)

	// Global GITHUB_TOKEN takes priority over instance-specific GITHUB_WORK_TOKEN.
	assert.Equal(t, "global-token", os.Getenv("GITHUB_TOKEN"))
}

func TestLoadGitHubConfig_missingFile(t *testing.T) {
	dir := t.TempDir()
	err := LoadGitHubConfig(dir, "")
	assert.NoError(t, err)
}

func TestLoadGitHubConfig_emptyList(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "githubs: []\n")

	err := LoadGitHubConfig(dir, "")
	assert.NoError(t, err)
}

// --- ResolveTracker tests ---

func TestResolveTracker_nameInJiras(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "jiras:\n  - name: work\n    url: https://work.atlassian.net\n    user: me@work.com\n    key: tok1\n")

	unsetEnv(t, "JIRA_URL")
	unsetEnv(t, "JIRA_USER")
	unsetEnv(t, "JIRA_KEY")

	kind, err := ResolveTracker(dir, "work")
	require.NoError(t, err)
	assert.Equal(t, TrackerJira, kind)
	assert.Equal(t, "https://work.atlassian.net", os.Getenv("JIRA_URL"))
}

func TestResolveTracker_nameInGithubs(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "githubs:\n  - name: personal\n    url: https://api.github.com\n    token: ghp_abc\n")

	unsetEnv(t, "GITHUB_URL")
	unsetEnv(t, "GITHUB_TOKEN")

	kind, err := ResolveTracker(dir, "personal")
	require.NoError(t, err)
	assert.Equal(t, TrackerGitHub, kind)
	assert.Equal(t, "https://api.github.com", os.Getenv("GITHUB_URL"))
}

func TestResolveTracker_unknownName(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "jiras:\n  - name: work\n    url: https://work.atlassian.net\n    user: me@work.com\n    key: tok1\n")

	_, err := ResolveTracker(dir, "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tracker name not found")
}

func TestResolveTracker_duplicateName(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "jiras:\n  - name: work\n    url: https://work.atlassian.net\n    user: me@work.com\n    key: tok1\ngithubs:\n  - name: work\n    token: ghp_abc\n")

	_, err := ResolveTracker(dir, "work")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ambiguous tracker name")
}

func TestResolveTracker_autoDetectJiraOnly(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "jiras:\n  - name: work\n    url: https://work.atlassian.net\n    user: me@work.com\n    key: tok1\n")

	unsetEnv(t, "JIRA_URL")
	unsetEnv(t, "JIRA_USER")
	unsetEnv(t, "JIRA_KEY")

	kind, err := ResolveTracker(dir, "")
	require.NoError(t, err)
	assert.Equal(t, TrackerJira, kind)
	assert.Equal(t, "https://work.atlassian.net", os.Getenv("JIRA_URL"))
}

func TestResolveTracker_autoDetectGitHubOnly(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "githubs:\n  - name: personal\n    url: https://api.github.com\n    token: ghp_abc\n")

	unsetEnv(t, "GITHUB_URL")
	unsetEnv(t, "GITHUB_TOKEN")

	kind, err := ResolveTracker(dir, "")
	require.NoError(t, err)
	assert.Equal(t, TrackerGitHub, kind)
	assert.Equal(t, "https://api.github.com", os.Getenv("GITHUB_URL"))
}

func TestResolveTracker_autoDetectBothTypes(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "jiras:\n  - name: work\n    url: https://work.atlassian.net\n    user: me@work.com\n    key: tok1\ngithubs:\n  - name: personal\n    token: ghp_abc\n")

	_, err := ResolveTracker(dir, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple tracker types configured")
}

func TestResolveTracker_noConfig(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "{}\n")

	_, err := ResolveTracker(dir, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no tracker configured")
}

func TestResolveTracker_missingFile(t *testing.T) {
	dir := t.TempDir()

	_, err := ResolveTracker(dir, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no tracker configured")
}
