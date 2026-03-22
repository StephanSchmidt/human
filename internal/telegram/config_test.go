package telegram

import (
	"os"
	"path/filepath"
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
			yaml: "telegrams:\n  - name: mybot\n    token: \"123456:ABC\"\n    description: My feedback bot\n",
			want: []Config{
				{Name: "mybot", Token: "123456:ABC", Description: "My feedback bot"},
			},
		},
		{
			name: "multiple entries",
			yaml: "telegrams:\n  - name: bot1\n    token: tok1\n  - name: bot2\n    token: tok2\n",
			want: []Config{
				{Name: "bot1", Token: "tok1"},
				{Name: "bot2", Token: "tok2"},
			},
		},
		{
			name: "empty list",
			yaml: "telegrams: []\n",
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

func TestLoadInstances_happyPath(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "telegrams:\n  - name: mybot\n    token: \"123456:ABC\"\n")

	unsetEnv(t, "TELEGRAM_TOKEN")
	unsetEnv(t, "TELEGRAM_MYBOT_TOKEN")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	require.Len(t, instances, 1)

	assert.Equal(t, "mybot", instances[0].Name)
	assert.NotNil(t, instances[0].Client)
}

func TestLoadInstances_missingFile(t *testing.T) {
	dir := t.TempDir()
	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	assert.Empty(t, instances)
}

func TestLoadInstances_missingTokenSkipped(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "telegrams:\n  - name: mybot\n")

	unsetEnv(t, "TELEGRAM_TOKEN")
	unsetEnv(t, "TELEGRAM_MYBOT_TOKEN")

	instances, err := LoadInstances(dir)
	require.NoError(t, err)
	assert.Empty(t, instances)
}
