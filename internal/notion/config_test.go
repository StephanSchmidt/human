package notion

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	require.NoError(t, os.Unsetenv(key))
}

func writeTestConfig(t *testing.T, dir, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".humanconfig.yaml"), []byte(content), 0o644))
}

func TestLoadConfigs(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want []Config
	}{
		{
			name: "single entry",
			yaml: "notions:\n  - name: work\n    url: https://api.notion.com\n    token: ntn_abc\n    description: Company workspace\n",
			want: []Config{
				{Name: "work", URL: "https://api.notion.com", Token: "ntn_abc", Description: "Company workspace"},
			},
		},
		{
			name: "multiple entries",
			yaml: "notions:\n  - name: work\n    token: ntn_abc\n  - name: personal\n    token: ntn_xyz\n",
			want: []Config{
				{Name: "work", Token: "ntn_abc"},
				{Name: "personal", Token: "ntn_xyz"},
			},
		},
		{
			name: "empty list",
			yaml: "notions: []\n",
			want: []Config{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTestConfig(t, dir, tt.yaml)

			got, err := LoadConfigs(dir)
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

func TestApplyEnvOverrides(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		envs map[string]string
		want Config
	}{
		{
			name: "overrides all fields",
			cfg:  Config{Name: "work", URL: "old-url", Token: "old-token"},
			envs: map[string]string{
				"NOTION_WORK_URL":   "new-url",
				"NOTION_WORK_TOKEN": "new-token",
			},
			want: Config{Name: "work", URL: "new-url", Token: "new-token"},
		},
		{
			name: "unset env leaves config alone",
			cfg:  Config{Name: "work", URL: "orig-url", Token: "orig-token"},
			envs: map[string]string{},
			want: Config{Name: "work", URL: "orig-url", Token: "orig-token"},
		},
		{
			name: "uppercased name",
			cfg:  Config{Name: "my-org", URL: "old-url", Token: "old-token"},
			envs: map[string]string{
				"NOTION_MY-ORG_TOKEN": "env-token",
			},
			want: Config{Name: "my-org", URL: "old-url", Token: "env-token"},
		},
		{
			name: "empty name is a no-op",
			cfg:  Config{URL: "url", Token: "token"},
			envs: map[string]string{},
			want: Config{URL: "url", Token: "token"},
		},
		{
			name: "partial override",
			cfg:  Config{Name: "work", URL: "old-url", Token: "old-token"},
			envs: map[string]string{
				"NOTION_WORK_TOKEN": "env-token",
			},
			want: Config{Name: "work", URL: "old-url", Token: "env-token"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetEnv(t, "NOTION_URL")
			unsetEnv(t, "NOTION_TOKEN")
			for _, suffix := range []string{"URL", "TOKEN"} {
				if tt.cfg.Name != "" {
					unsetEnv(t, "NOTION_"+strings.ToUpper(tt.cfg.Name)+"_"+suffix)
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
			cfg:  Config{Name: "work", URL: "old-url", Token: "old-token"},
			envs: map[string]string{
				"NOTION_URL":   "global-url",
				"NOTION_TOKEN": "global-token",
			},
			want: Config{Name: "work", URL: "global-url", Token: "global-token"},
		},
		{
			name: "unset env leaves config alone",
			cfg:  Config{Name: "work", URL: "orig-url", Token: "orig-token"},
			envs: map[string]string{},
			want: Config{Name: "work", URL: "orig-url", Token: "orig-token"},
		},
		{
			name: "partial override",
			cfg:  Config{Name: "work", URL: "old-url", Token: "old-token"},
			envs: map[string]string{
				"NOTION_TOKEN": "global-token",
			},
			want: Config{Name: "work", URL: "old-url", Token: "global-token"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetEnv(t, "NOTION_URL")
			unsetEnv(t, "NOTION_TOKEN")
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
	writeTestConfig(t, dir, "notions:\n  - name: work\n    url: https://api.notion.com\n    token: ntn_abc\n")

	unsetEnv(t, "NOTION_URL")
	unsetEnv(t, "NOTION_TOKEN")
	unsetEnv(t, "NOTION_WORK_URL")
	unsetEnv(t, "NOTION_WORK_TOKEN")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	require.Len(t, instances, 1)

	assert.Equal(t, "work", instances[0].Name)
	assert.Equal(t, "https://api.notion.com", instances[0].URL)
	assert.NotNil(t, instances[0].Client)
}

func TestLoadInstances_missingFile(t *testing.T) {
	dir := t.TempDir()
	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	assert.Empty(t, instances)
}

func TestLoadInstances_defaultURL(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "notions:\n  - name: work\n    token: ntn_abc\n")

	unsetEnv(t, "NOTION_URL")
	unsetEnv(t, "NOTION_TOKEN")
	unsetEnv(t, "NOTION_WORK_URL")
	unsetEnv(t, "NOTION_WORK_TOKEN")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	require.Len(t, instances, 1)
	assert.Equal(t, "https://api.notion.com", instances[0].URL)
}

func TestLoadInstances_missingTokenSkipped(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "notions:\n  - name: work\n    url: https://api.notion.com\n")

	unsetEnv(t, "NOTION_URL")
	unsetEnv(t, "NOTION_TOKEN")
	unsetEnv(t, "NOTION_WORK_URL")
	unsetEnv(t, "NOTION_WORK_TOKEN")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	assert.Empty(t, instances)
}

func TestLoadInstances_globalEnvOverride(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "notions:\n  - name: work\n    url: https://api.notion.com\n    token: file-token\n")

	unsetEnv(t, "NOTION_URL")
	t.Setenv("NOTION_TOKEN", "global-token")
	unsetEnv(t, "NOTION_WORK_URL")
	unsetEnv(t, "NOTION_WORK_TOKEN")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	require.Len(t, instances, 1)
	assert.Equal(t, "https://api.notion.com", instances[0].URL)
}
