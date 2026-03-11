package amplitude

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
			yaml: "amplitudes:\n  - name: product\n    url: https://analytics.eu.amplitude.com\n    key: mykey\n    secret: mysecret\n    description: Product analytics\n",
			want: []Config{
				{Name: "product", URL: "https://analytics.eu.amplitude.com", Key: "mykey", Secret: "mysecret", Description: "Product analytics"},
			},
		},
		{
			name: "multiple entries",
			yaml: "amplitudes:\n  - name: product\n    key: k1\n    secret: s1\n  - name: marketing\n    key: k2\n    secret: s2\n",
			want: []Config{
				{Name: "product", Key: "k1", Secret: "s1"},
				{Name: "marketing", Key: "k2", Secret: "s2"},
			},
		},
		{
			name: "empty list",
			yaml: "amplitudes: []\n",
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
			cfg:  Config{Name: "product", URL: "old-url", Key: "old-key", Secret: "old-secret"},
			envs: map[string]string{
				"AMPLITUDE_PRODUCT_URL":    "new-url",
				"AMPLITUDE_PRODUCT_KEY":    "new-key",
				"AMPLITUDE_PRODUCT_SECRET": "new-secret",
			},
			want: Config{Name: "product", URL: "new-url", Key: "new-key", Secret: "new-secret"},
		},
		{
			name: "unset env leaves config alone",
			cfg:  Config{Name: "product", URL: "orig-url", Key: "orig-key", Secret: "orig-secret"},
			envs: map[string]string{},
			want: Config{Name: "product", URL: "orig-url", Key: "orig-key", Secret: "orig-secret"},
		},
		{
			name: "uppercased name",
			cfg:  Config{Name: "my-org", URL: "old-url", Key: "old-key", Secret: "old-secret"},
			envs: map[string]string{
				"AMPLITUDE_MY-ORG_KEY": "env-key",
			},
			want: Config{Name: "my-org", URL: "old-url", Key: "env-key", Secret: "old-secret"},
		},
		{
			name: "empty name is a no-op",
			cfg:  Config{URL: "url", Key: "key", Secret: "secret"},
			envs: map[string]string{},
			want: Config{URL: "url", Key: "key", Secret: "secret"},
		},
		{
			name: "partial override",
			cfg:  Config{Name: "product", URL: "old-url", Key: "old-key", Secret: "old-secret"},
			envs: map[string]string{
				"AMPLITUDE_PRODUCT_SECRET": "env-secret",
			},
			want: Config{Name: "product", URL: "old-url", Key: "old-key", Secret: "env-secret"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetEnv(t, "AMPLITUDE_URL")
			unsetEnv(t, "AMPLITUDE_KEY")
			unsetEnv(t, "AMPLITUDE_SECRET")
			for _, suffix := range []string{"URL", "KEY", "SECRET"} {
				if tt.cfg.Name != "" {
					unsetEnv(t, "AMPLITUDE_"+strings.ToUpper(tt.cfg.Name)+"_"+suffix)
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
			cfg:  Config{Name: "product", URL: "old-url", Key: "old-key", Secret: "old-secret"},
			envs: map[string]string{
				"AMPLITUDE_URL":    "global-url",
				"AMPLITUDE_KEY":    "global-key",
				"AMPLITUDE_SECRET": "global-secret",
			},
			want: Config{Name: "product", URL: "global-url", Key: "global-key", Secret: "global-secret"},
		},
		{
			name: "unset env leaves config alone",
			cfg:  Config{Name: "product", URL: "orig-url", Key: "orig-key", Secret: "orig-secret"},
			envs: map[string]string{},
			want: Config{Name: "product", URL: "orig-url", Key: "orig-key", Secret: "orig-secret"},
		},
		{
			name: "partial override",
			cfg:  Config{Name: "product", URL: "old-url", Key: "old-key", Secret: "old-secret"},
			envs: map[string]string{
				"AMPLITUDE_SECRET": "global-secret",
			},
			want: Config{Name: "product", URL: "old-url", Key: "old-key", Secret: "global-secret"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetEnv(t, "AMPLITUDE_URL")
			unsetEnv(t, "AMPLITUDE_KEY")
			unsetEnv(t, "AMPLITUDE_SECRET")
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
	writeTestConfig(t, dir, "amplitudes:\n  - name: product\n    url: https://analytics.eu.amplitude.com\n    key: mykey\n    secret: mysecret\n")

	unsetEnv(t, "AMPLITUDE_URL")
	unsetEnv(t, "AMPLITUDE_KEY")
	unsetEnv(t, "AMPLITUDE_SECRET")
	unsetEnv(t, "AMPLITUDE_PRODUCT_URL")
	unsetEnv(t, "AMPLITUDE_PRODUCT_KEY")
	unsetEnv(t, "AMPLITUDE_PRODUCT_SECRET")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	require.Len(t, instances, 1)

	assert.Equal(t, "product", instances[0].Name)
	assert.Equal(t, "https://analytics.eu.amplitude.com", instances[0].URL)
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
	writeTestConfig(t, dir, "amplitudes:\n  - name: product\n    key: mykey\n    secret: mysecret\n")

	unsetEnv(t, "AMPLITUDE_URL")
	unsetEnv(t, "AMPLITUDE_KEY")
	unsetEnv(t, "AMPLITUDE_SECRET")
	unsetEnv(t, "AMPLITUDE_PRODUCT_URL")
	unsetEnv(t, "AMPLITUDE_PRODUCT_KEY")
	unsetEnv(t, "AMPLITUDE_PRODUCT_SECRET")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	require.Len(t, instances, 1)
	assert.Equal(t, "https://amplitude.com", instances[0].URL)
}

func TestLoadInstances_missingKeySkipped(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "amplitudes:\n  - name: product\n    url: https://amplitude.com\n    secret: mysecret\n")

	unsetEnv(t, "AMPLITUDE_URL")
	unsetEnv(t, "AMPLITUDE_KEY")
	unsetEnv(t, "AMPLITUDE_SECRET")
	unsetEnv(t, "AMPLITUDE_PRODUCT_URL")
	unsetEnv(t, "AMPLITUDE_PRODUCT_KEY")
	unsetEnv(t, "AMPLITUDE_PRODUCT_SECRET")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	assert.Empty(t, instances)
}

func TestLoadInstances_missingSecretSkipped(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "amplitudes:\n  - name: product\n    url: https://amplitude.com\n    key: mykey\n")

	unsetEnv(t, "AMPLITUDE_URL")
	unsetEnv(t, "AMPLITUDE_KEY")
	unsetEnv(t, "AMPLITUDE_SECRET")
	unsetEnv(t, "AMPLITUDE_PRODUCT_URL")
	unsetEnv(t, "AMPLITUDE_PRODUCT_KEY")
	unsetEnv(t, "AMPLITUDE_PRODUCT_SECRET")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	assert.Empty(t, instances)
}

func TestLoadInstances_globalEnvOverride(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "amplitudes:\n  - name: product\n    url: https://amplitude.com\n    key: file-key\n    secret: file-secret\n")

	unsetEnv(t, "AMPLITUDE_URL")
	t.Setenv("AMPLITUDE_KEY", "global-key")
	t.Setenv("AMPLITUDE_SECRET", "global-secret")
	unsetEnv(t, "AMPLITUDE_PRODUCT_URL")
	unsetEnv(t, "AMPLITUDE_PRODUCT_KEY")
	unsetEnv(t, "AMPLITUDE_PRODUCT_SECRET")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	require.Len(t, instances, 1)
	assert.Equal(t, "https://amplitude.com", instances[0].URL)
}
