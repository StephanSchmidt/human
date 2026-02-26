package azuredevops

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
			yaml: "azuredevops:\n  - name: work\n    url: https://dev.azure.com\n    org: myorg\n    token: pat-abc\n",
			want: []Config{
				{Name: "work", URL: "https://dev.azure.com", Org: "myorg", Token: "pat-abc"},
			},
		},
		{
			name: "multiple entries",
			yaml: "azuredevops:\n  - name: work\n    url: https://dev.azure.com\n    org: myorg\n    token: pat-abc\n  - name: personal\n    url: https://dev.azure.com\n    org: otherorg\n    token: pat-xyz\n",
			want: []Config{
				{Name: "work", URL: "https://dev.azure.com", Org: "myorg", Token: "pat-abc"},
				{Name: "personal", URL: "https://dev.azure.com", Org: "otherorg", Token: "pat-xyz"},
			},
		},
		{
			name: "empty list",
			yaml: "azuredevops: []\n",
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
			cfg:  Config{Name: "work", URL: "old-url", Org: "old-org", Token: "old-token"},
			envs: map[string]string{
				"AZURE_WORK_URL":   "new-url",
				"AZURE_WORK_ORG":   "new-org",
				"AZURE_WORK_TOKEN": "new-token",
			},
			want: Config{Name: "work", URL: "new-url", Org: "new-org", Token: "new-token"},
		},
		{
			name: "unset env leaves config alone",
			cfg:  Config{Name: "work", URL: "orig-url", Org: "orig-org", Token: "orig-token"},
			envs: map[string]string{},
			want: Config{Name: "work", URL: "orig-url", Org: "orig-org", Token: "orig-token"},
		},
		{
			name: "uppercased name",
			cfg:  Config{Name: "my-org", URL: "old-url", Org: "old-org", Token: "old-token"},
			envs: map[string]string{
				"AZURE_MY-ORG_TOKEN": "env-token",
			},
			want: Config{Name: "my-org", URL: "old-url", Org: "old-org", Token: "env-token"},
		},
		{
			name: "empty name is a no-op",
			cfg:  Config{URL: "url", Org: "org", Token: "token"},
			envs: map[string]string{},
			want: Config{URL: "url", Org: "org", Token: "token"},
		},
		{
			name: "partial override",
			cfg:  Config{Name: "work", URL: "old-url", Org: "old-org", Token: "old-token"},
			envs: map[string]string{
				"AZURE_WORK_TOKEN": "env-token",
			},
			want: Config{Name: "work", URL: "old-url", Org: "old-org", Token: "env-token"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, suffix := range []string{"URL", "ORG", "TOKEN"} {
				if tt.cfg.Name != "" {
					unsetEnv(t, "AZURE_"+tt.cfg.Name+"_"+suffix)
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
			cfg:  Config{Name: "work", URL: "old-url", Org: "old-org", Token: "old-token"},
			envs: map[string]string{
				"AZURE_URL":   "global-url",
				"AZURE_ORG":   "global-org",
				"AZURE_TOKEN": "global-token",
			},
			want: Config{Name: "work", URL: "global-url", Org: "global-org", Token: "global-token"},
		},
		{
			name: "unset env leaves config alone",
			cfg:  Config{Name: "work", URL: "orig-url", Org: "orig-org", Token: "orig-token"},
			envs: map[string]string{},
			want: Config{Name: "work", URL: "orig-url", Org: "orig-org", Token: "orig-token"},
		},
		{
			name: "partial override",
			cfg:  Config{Name: "work", URL: "old-url", Org: "old-org", Token: "old-token"},
			envs: map[string]string{
				"AZURE_TOKEN": "global-token",
			},
			want: Config{Name: "work", URL: "old-url", Org: "old-org", Token: "global-token"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetEnv(t, "AZURE_URL")
			unsetEnv(t, "AZURE_ORG")
			unsetEnv(t, "AZURE_TOKEN")
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
	writeConfig(t, dir, "azuredevops:\n  - name: work\n    url: https://dev.azure.com\n    org: myorg\n    token: pat-abc\n")

	unsetEnv(t, "AZURE_URL")
	unsetEnv(t, "AZURE_ORG")
	unsetEnv(t, "AZURE_TOKEN")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	require.Len(t, instances, 1)

	assert.Equal(t, "work", instances[0].Name)
	assert.Equal(t, "azuredevops", instances[0].Kind)
	assert.Equal(t, "https://dev.azure.com", instances[0].URL)
	assert.NotNil(t, instances[0].Provider)
}

func TestLoadInstances_multipleEntries(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "azuredevops:\n  - name: work\n    url: https://dev.azure.com\n    org: myorg\n    token: pat-abc\n  - name: personal\n    org: otherorg\n    token: pat-xyz\n")

	unsetEnv(t, "AZURE_URL")
	unsetEnv(t, "AZURE_ORG")
	unsetEnv(t, "AZURE_TOKEN")

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
	writeConfig(t, dir, "azuredevops:\n  - name: work\n    org: myorg\n    token: pat-abc\n")

	unsetEnv(t, "AZURE_URL")
	unsetEnv(t, "AZURE_ORG")
	unsetEnv(t, "AZURE_TOKEN")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	require.Len(t, instances, 1)
	assert.Equal(t, "https://dev.azure.com", instances[0].URL)
}

func TestLoadInstances_incompleteConfigSkipped(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "azuredevops:\n  - name: work\n    url: https://dev.azure.com\n    org: myorg\n")

	unsetEnv(t, "AZURE_URL")
	unsetEnv(t, "AZURE_ORG")
	unsetEnv(t, "AZURE_TOKEN")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	assert.Empty(t, instances)
}

func TestLoadInstances_incompleteConfigSkipped_missingOrg(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "azuredevops:\n  - name: work\n    url: https://dev.azure.com\n    token: pat-abc\n")

	unsetEnv(t, "AZURE_URL")
	unsetEnv(t, "AZURE_ORG")
	unsetEnv(t, "AZURE_TOKEN")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	assert.Empty(t, instances)
}

func TestLoadInstances_globalEnvOverridesInstance(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "azuredevops:\n  - name: work\n    url: https://dev.azure.com\n    org: myorg\n    token: file-token\n")

	unsetEnv(t, "AZURE_URL")
	t.Setenv("AZURE_TOKEN", "global-token")
	t.Setenv("AZURE_WORK_TOKEN", "instance-token")
	unsetEnv(t, "AZURE_ORG")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	require.Len(t, instances, 1)

	// Global AZURE_TOKEN takes priority over instance-specific AZURE_WORK_TOKEN.
	assert.Equal(t, "https://dev.azure.com", instances[0].URL)
}
