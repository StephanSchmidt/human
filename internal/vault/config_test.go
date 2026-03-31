package vault

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeConfig(t *testing.T, dir, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".humanconfig.yaml"), []byte(content), 0o644))
}

func TestReadConfig_1password(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "vault:\n  provider: 1password\n")

	cfg := ReadConfig(dir)
	require.NotNil(t, cfg)
	assert.Equal(t, "1password", cfg.Provider)
}

func TestReadConfig_noVaultSection(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "githubs:\n  - name: personal\n    token: tok\n")

	cfg := ReadConfig(dir)
	assert.Nil(t, cfg)
}

func TestReadConfig_emptyProvider(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "vault:\n  provider: \"\"\n")

	cfg := ReadConfig(dir)
	assert.Nil(t, cfg)
}

func TestReadConfig_missingFile(t *testing.T) {
	dir := t.TempDir()

	cfg := ReadConfig(dir)
	assert.Nil(t, cfg)
}

func TestNewResolverFromConfig_nil(t *testing.T) {
	r := NewResolverFromConfig(nil)
	assert.Nil(t, r)
}

func TestNewResolverFromConfig_1password(t *testing.T) {
	r := NewResolverFromConfig(&Config{Provider: "1password"})
	require.NotNil(t, r)
	assert.Len(t, r.providers, 1)
}

func TestNewResolverFromConfig_1pw(t *testing.T) {
	r := NewResolverFromConfig(&Config{Provider: "1pw"})
	require.NotNil(t, r)
	assert.Len(t, r.providers, 1)
}

func TestNewResolverFromConfig_unknownProvider(t *testing.T) {
	r := NewResolverFromConfig(&Config{Provider: "unknown"})
	assert.Nil(t, r)
}
