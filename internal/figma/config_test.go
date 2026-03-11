package figma

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
			yaml: "figmas:\n  - name: design\n    url: https://api.figma.com\n    token: figd_abc\n    description: Product design team\n",
			want: []Config{
				{Name: "design", URL: "https://api.figma.com", Token: "figd_abc", Description: "Product design team"},
			},
		},
		{
			name: "multiple entries",
			yaml: "figmas:\n  - name: design\n    token: figd_abc\n  - name: marketing\n    token: figd_xyz\n",
			want: []Config{
				{Name: "design", Token: "figd_abc"},
				{Name: "marketing", Token: "figd_xyz"},
			},
		},
		{
			name: "empty list",
			yaml: "figmas: []\n",
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
			cfg:  Config{Name: "design", URL: "old-url", Token: "old-token"},
			envs: map[string]string{
				"FIGMA_DESIGN_URL":   "new-url",
				"FIGMA_DESIGN_TOKEN": "new-token",
			},
			want: Config{Name: "design", URL: "new-url", Token: "new-token"},
		},
		{
			name: "unset env leaves config alone",
			cfg:  Config{Name: "design", URL: "orig-url", Token: "orig-token"},
			envs: map[string]string{},
			want: Config{Name: "design", URL: "orig-url", Token: "orig-token"},
		},
		{
			name: "uppercased name",
			cfg:  Config{Name: "my-org", URL: "old-url", Token: "old-token"},
			envs: map[string]string{
				"FIGMA_MY-ORG_TOKEN": "env-token",
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
			cfg:  Config{Name: "design", URL: "old-url", Token: "old-token"},
			envs: map[string]string{
				"FIGMA_DESIGN_TOKEN": "env-token",
			},
			want: Config{Name: "design", URL: "old-url", Token: "env-token"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetEnv(t, "FIGMA_URL")
			unsetEnv(t, "FIGMA_TOKEN")
			for _, suffix := range []string{"URL", "TOKEN"} {
				if tt.cfg.Name != "" {
					unsetEnv(t, "FIGMA_"+strings.ToUpper(tt.cfg.Name)+"_"+suffix)
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
			cfg:  Config{Name: "design", URL: "old-url", Token: "old-token"},
			envs: map[string]string{
				"FIGMA_URL":   "global-url",
				"FIGMA_TOKEN": "global-token",
			},
			want: Config{Name: "design", URL: "global-url", Token: "global-token"},
		},
		{
			name: "unset env leaves config alone",
			cfg:  Config{Name: "design", URL: "orig-url", Token: "orig-token"},
			envs: map[string]string{},
			want: Config{Name: "design", URL: "orig-url", Token: "orig-token"},
		},
		{
			name: "partial override",
			cfg:  Config{Name: "design", URL: "old-url", Token: "old-token"},
			envs: map[string]string{
				"FIGMA_TOKEN": "global-token",
			},
			want: Config{Name: "design", URL: "old-url", Token: "global-token"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetEnv(t, "FIGMA_URL")
			unsetEnv(t, "FIGMA_TOKEN")
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
	writeTestConfig(t, dir, "figmas:\n  - name: design\n    url: https://api.figma.com\n    token: figd_abc\n")

	unsetEnv(t, "FIGMA_URL")
	unsetEnv(t, "FIGMA_TOKEN")
	unsetEnv(t, "FIGMA_DESIGN_URL")
	unsetEnv(t, "FIGMA_DESIGN_TOKEN")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	require.Len(t, instances, 1)

	assert.Equal(t, "design", instances[0].Name)
	assert.Equal(t, "https://api.figma.com", instances[0].URL)
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
	writeTestConfig(t, dir, "figmas:\n  - name: design\n    token: figd_abc\n")

	unsetEnv(t, "FIGMA_URL")
	unsetEnv(t, "FIGMA_TOKEN")
	unsetEnv(t, "FIGMA_DESIGN_URL")
	unsetEnv(t, "FIGMA_DESIGN_TOKEN")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	require.Len(t, instances, 1)
	assert.Equal(t, "https://api.figma.com", instances[0].URL)
}

func TestLoadInstances_missingTokenSkipped(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "figmas:\n  - name: design\n    url: https://api.figma.com\n")

	unsetEnv(t, "FIGMA_URL")
	unsetEnv(t, "FIGMA_TOKEN")
	unsetEnv(t, "FIGMA_DESIGN_URL")
	unsetEnv(t, "FIGMA_DESIGN_TOKEN")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	assert.Empty(t, instances)
}

func TestLoadInstances_globalEnvOverride(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "figmas:\n  - name: design\n    url: https://api.figma.com\n    token: file-token\n")

	unsetEnv(t, "FIGMA_URL")
	t.Setenv("FIGMA_TOKEN", "global-token")
	unsetEnv(t, "FIGMA_DESIGN_URL")
	unsetEnv(t, "FIGMA_DESIGN_TOKEN")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	require.Len(t, instances, 1)
	assert.Equal(t, "https://api.figma.com", instances[0].URL)
}
