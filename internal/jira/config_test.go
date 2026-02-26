package jira

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

func TestLoadConfigs(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    []Config
		wantErr string
	}{
		{
			name: "single entry",
			yaml: "jiras:\n  - name: work\n    url: https://work.atlassian.net\n    user: me@work.com\n    key: tok1\n",
			want: []Config{
				{Name: "work", URL: "https://work.atlassian.net", User: "me@work.com", Key: "tok1"},
			},
		},
		{
			name: "multiple entries",
			yaml: "jiras:\n  - name: work\n    url: https://work.atlassian.net\n    user: me@work.com\n    key: tok1\n  - name: personal\n    url: https://personal.atlassian.net\n    user: me@personal.com\n    key: tok2\n",
			want: []Config{
				{Name: "work", URL: "https://work.atlassian.net", User: "me@work.com", Key: "tok1"},
				{Name: "personal", URL: "https://personal.atlassian.net", User: "me@personal.com", Key: "tok2"},
			},
		},
		{
			name: "empty list",
			yaml: "jiras: []\n",
			want: []Config{},
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

			got, err := LoadConfigs(dir)

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

func TestLoadConfigs_missingFile(t *testing.T) {
	dir := t.TempDir()
	got, err := LoadConfigs(dir)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestLoadConfigs_extensionlessFallback(t *testing.T) {
	dir := t.TempDir()
	content := "jiras:\n  - name: work\n    url: https://work.atlassian.net\n    user: me@work.com\n    key: tok1\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".humanconfig"), []byte(content), 0o644))

	got, err := LoadConfigs(dir)
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, "work", got[0].Name)
}

func TestApplyEnvOverrides(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		envs map[string]string
		want Config
	}{
		{
			name: "overrides all fields",
			cfg:  Config{Name: "work", URL: "old-url", User: "old-user", Key: "old-key"},
			envs: map[string]string{
				"JIRA_WORK_URL":  "new-url",
				"JIRA_WORK_USER": "new-user",
				"JIRA_WORK_KEY":  "new-key",
			},
			want: Config{Name: "work", URL: "new-url", User: "new-user", Key: "new-key"},
		},
		{
			name: "unset env leaves config alone",
			cfg:  Config{Name: "work", URL: "orig-url", User: "orig-user", Key: "orig-key"},
			envs: map[string]string{},
			want: Config{Name: "work", URL: "orig-url", User: "orig-user", Key: "orig-key"},
		},
		{
			name: "uppercased name",
			cfg:  Config{Name: "my-org", URL: "old-url", User: "old-user", Key: "old-key"},
			envs: map[string]string{
				"JIRA_MY-ORG_KEY": "env-key",
			},
			want: Config{Name: "my-org", URL: "old-url", User: "old-user", Key: "env-key"},
		},
		{
			name: "empty name is a no-op",
			cfg:  Config{URL: "url", User: "user", Key: "key"},
			envs: map[string]string{},
			want: Config{URL: "url", User: "user", Key: "key"},
		},
		{
			name: "partial override",
			cfg:  Config{Name: "work", URL: "old-url", User: "old-user", Key: "old-key"},
			envs: map[string]string{
				"JIRA_WORK_KEY": "env-key",
			},
			want: Config{Name: "work", URL: "old-url", User: "old-user", Key: "env-key"},
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

func TestApplyGlobalEnvOverrides(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		envs map[string]string
		want Config
	}{
		{
			name: "overrides all fields",
			cfg:  Config{Name: "work", URL: "old-url", User: "old-user", Key: "old-key"},
			envs: map[string]string{
				"JIRA_URL":  "global-url",
				"JIRA_USER": "global-user",
				"JIRA_KEY":  "global-key",
			},
			want: Config{Name: "work", URL: "global-url", User: "global-user", Key: "global-key"},
		},
		{
			name: "unset env leaves config alone",
			cfg:  Config{Name: "work", URL: "orig-url", User: "orig-user", Key: "orig-key"},
			envs: map[string]string{},
			want: Config{Name: "work", URL: "orig-url", User: "orig-user", Key: "orig-key"},
		},
		{
			name: "partial override",
			cfg:  Config{Name: "work", URL: "old-url", User: "old-user", Key: "old-key"},
			envs: map[string]string{
				"JIRA_KEY": "global-key",
			},
			want: Config{Name: "work", URL: "old-url", User: "old-user", Key: "global-key"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetEnv(t, "JIRA_URL")
			unsetEnv(t, "JIRA_USER")
			unsetEnv(t, "JIRA_KEY")
			for k, v := range tt.envs {
				t.Setenv(k, v)
			}

			cfg := tt.cfg
			applyGlobalEnvOverrides(&cfg)

			assert.Equal(t, tt.want, cfg)
		})
	}
}

func TestLoadInstances_happyPath(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "jiras:\n  - name: work\n    url: https://work.atlassian.net\n    user: me@work.com\n    key: tok1\n")

	unsetEnv(t, "JIRA_URL")
	unsetEnv(t, "JIRA_USER")
	unsetEnv(t, "JIRA_KEY")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	require.Len(t, instances, 1)

	assert.Equal(t, "work", instances[0].Name)
	assert.Equal(t, "jira", instances[0].Kind)
	assert.Equal(t, "https://work.atlassian.net", instances[0].URL)
	assert.Equal(t, "me@work.com", instances[0].User)
	assert.NotNil(t, instances[0].Provider)
}

func TestLoadInstances_multipleEntries(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "jiras:\n  - name: work\n    url: https://work.atlassian.net\n    user: me@work.com\n    key: tok1\n  - name: personal\n    url: https://personal.atlassian.net\n    user: me@personal.com\n    key: tok2\n")

	unsetEnv(t, "JIRA_URL")
	unsetEnv(t, "JIRA_USER")
	unsetEnv(t, "JIRA_KEY")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	assert.Len(t, instances, 2)
	assert.Equal(t, "work", instances[0].Name)
	assert.Equal(t, "personal", instances[1].Name)
}

func TestLoadInstances_missingFile(t *testing.T) {
	dir := t.TempDir()

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	assert.Empty(t, instances)
}

func TestLoadInstances_globalEnvOverridesInstanceEnv(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "jiras:\n  - name: work\n    url: https://work.atlassian.net\n    user: me@work.com\n    key: file-key\n")

	unsetEnv(t, "JIRA_URL")
	unsetEnv(t, "JIRA_USER")
	t.Setenv("JIRA_KEY", "global-key")
	t.Setenv("JIRA_WORK_KEY", "instance-key")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	require.Len(t, instances, 1)

	// Global JIRA_KEY takes priority over instance-specific JIRA_WORK_KEY.
	assert.Equal(t, "https://work.atlassian.net", instances[0].URL)
}

func TestLoadInstances_incompleteConfigSkipped(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "jiras:\n  - name: work\n    url: https://work.atlassian.net\n")

	unsetEnv(t, "JIRA_URL")
	unsetEnv(t, "JIRA_USER")
	unsetEnv(t, "JIRA_KEY")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	assert.Empty(t, instances)
}
