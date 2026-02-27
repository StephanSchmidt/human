package shortcut

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
			yaml: "shortcuts:\n  - name: work\n    url: https://api.app.shortcut.com\n    token: tok-abc\n",
			want: []Config{
				{Name: "work", URL: "https://api.app.shortcut.com", Token: "tok-abc"},
			},
		},
		{
			name: "multiple entries",
			yaml: "shortcuts:\n  - name: work\n    url: https://api.app.shortcut.com\n    token: tok-abc\n  - name: personal\n    url: https://api.app.shortcut.com\n    token: tok-xyz\n",
			want: []Config{
				{Name: "work", URL: "https://api.app.shortcut.com", Token: "tok-abc"},
				{Name: "personal", URL: "https://api.app.shortcut.com", Token: "tok-xyz"},
			},
		},
		{
			name: "empty list",
			yaml: "shortcuts: []\n",
			want: []Config{},
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
				"SHORTCUT_WORK_URL":   "new-url",
				"SHORTCUT_WORK_TOKEN": "new-token",
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
				"SHORTCUT_MY-ORG_TOKEN": "env-token",
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
				"SHORTCUT_WORK_TOKEN": "env-token",
			},
			want: Config{Name: "work", URL: "old-url", Token: "env-token"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, suffix := range []string{"URL", "TOKEN"} {
				if tt.cfg.Name != "" {
					unsetEnv(t, "SHORTCUT_"+tt.cfg.Name+"_"+suffix)
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
				"SHORTCUT_URL":   "global-url",
				"SHORTCUT_TOKEN": "global-token",
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
				"SHORTCUT_TOKEN": "global-token",
			},
			want: Config{Name: "work", URL: "old-url", Token: "global-token"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetEnv(t, "SHORTCUT_URL")
			unsetEnv(t, "SHORTCUT_TOKEN")
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
	writeConfig(t, dir, "shortcuts:\n  - name: work\n    url: https://api.app.shortcut.com\n    token: tok-abc\n")

	unsetEnv(t, "SHORTCUT_URL")
	unsetEnv(t, "SHORTCUT_TOKEN")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	require.Len(t, instances, 1)

	assert.Equal(t, "work", instances[0].Name)
	assert.Equal(t, "shortcut", instances[0].Kind)
	assert.Equal(t, "https://api.app.shortcut.com", instances[0].URL)
	assert.NotNil(t, instances[0].Provider)
}

func TestLoadInstances_multipleEntries(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "shortcuts:\n  - name: work\n    url: https://api.app.shortcut.com\n    token: tok-abc\n  - name: personal\n    token: tok-xyz\n")

	unsetEnv(t, "SHORTCUT_URL")
	unsetEnv(t, "SHORTCUT_TOKEN")

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

func TestLoadInstances_defaultURL(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "shortcuts:\n  - name: work\n    token: tok-abc\n")

	unsetEnv(t, "SHORTCUT_URL")
	unsetEnv(t, "SHORTCUT_TOKEN")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	require.Len(t, instances, 1)
	assert.Equal(t, "https://api.app.shortcut.com", instances[0].URL)
}

func TestLoadInstances_incompleteConfigSkipped(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "shortcuts:\n  - name: work\n    url: https://api.app.shortcut.com\n")

	unsetEnv(t, "SHORTCUT_URL")
	unsetEnv(t, "SHORTCUT_TOKEN")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	assert.Empty(t, instances)
}

func TestLoadInstances_globalEnvOverridesInstance(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "shortcuts:\n  - name: work\n    url: https://api.app.shortcut.com\n    token: file-token\n")

	unsetEnv(t, "SHORTCUT_URL")
	t.Setenv("SHORTCUT_TOKEN", "global-token")
	t.Setenv("SHORTCUT_WORK_TOKEN", "instance-token")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	require.Len(t, instances, 1)

	// Global SHORTCUT_TOKEN takes priority over instance-specific SHORTCUT_WORK_TOKEN.
	assert.Equal(t, "https://api.app.shortcut.com", instances[0].URL)
}
