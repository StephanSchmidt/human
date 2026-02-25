package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unsetEnv registers cleanup via t.Setenv then unsets the variable for the test.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	require.NoError(t, os.Unsetenv(key))
}

func TestSetEnvFromConfig(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		mapping   map[string]string
		presetEnv map[string]string
		wantEnv   map[string]string
	}{
		{
			name: "sets env when absent",
			yaml: "jira:\n  url: https://example.atlassian.net",
			mapping: map[string]string{
				"jira.url": "TEST_JIRA_URL",
			},
			wantEnv: map[string]string{
				"TEST_JIRA_URL": "https://example.atlassian.net",
			},
		},
		{
			name: "skips when env already set",
			yaml: "jira:\n  url: https://from-config.atlassian.net",
			mapping: map[string]string{
				"jira.url": "TEST_JIRA_URL",
			},
			presetEnv: map[string]string{
				"TEST_JIRA_URL": "https://from-env.atlassian.net",
			},
			wantEnv: map[string]string{
				"TEST_JIRA_URL": "https://from-env.atlassian.net",
			},
		},
		{
			name: "skips empty values",
			yaml: "jira:\n  url: \"\"",
			mapping: map[string]string{
				"jira.url": "TEST_JIRA_URL",
			},
			wantEnv: map[string]string{
				"TEST_JIRA_URL": "",
			},
		},
		{
			name: "handles multiple keys",
			yaml: "jira:\n  url: https://example.atlassian.net\n  user: me@example.com",
			mapping: map[string]string{
				"jira.url":  "TEST_JIRA_URL",
				"jira.user": "TEST_JIRA_USER",
			},
			wantEnv: map[string]string{
				"TEST_JIRA_URL":  "https://example.atlassian.net",
				"TEST_JIRA_USER": "me@example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up env vars used in this test.
			for _, envVar := range tt.mapping {
				unsetEnv(t, envVar)
			}

			for k, v := range tt.presetEnv {
				t.Setenv(k, v)
			}

			v := viper.New()
			v.SetConfigType("yaml")
			require.NoError(t, v.ReadConfig(strings.NewReader(tt.yaml)))

			require.NoError(t, setEnvFromConfig(v, tt.mapping))

			for envVar, wantVal := range tt.wantEnv {
				got, exists := os.LookupEnv(envVar)
				if wantVal == "" && !exists {
					continue // empty value means unset is fine
				}
				assert.Equal(t, wantVal, got, "env var %s", envVar)
			}
		})
	}
}

func TestLoadConfig_missingFile(t *testing.T) {
	dir := t.TempDir()
	err := LoadConfig(dir)
	assert.NoError(t, err)
}

func TestLoadConfig_validFile(t *testing.T) {
	dir := t.TempDir()
	configContent := "jira:\n  url: https://test.atlassian.net\n  user: test@example.com\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".humanconfig.yaml"), []byte(configContent), 0o644))

	unsetEnv(t, "JIRA_URL")
	unsetEnv(t, "JIRA_USER")

	err := LoadConfig(dir)
	require.NoError(t, err)

	assert.Equal(t, "https://test.atlassian.net", os.Getenv("JIRA_URL"))
	assert.Equal(t, "test@example.com", os.Getenv("JIRA_USER"))
}

func TestLoadConfig_invalidYAML(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".humanconfig.yaml"), []byte(":\n  :\n  invalid: [unterminated"), 0o644))

	err := LoadConfig(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing config file")
}
