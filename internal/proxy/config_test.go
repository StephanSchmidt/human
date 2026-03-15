package proxy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_happyPath(t *testing.T) {
	dir := t.TempDir()
	yaml := `proxy:
  mode: allowlist
  domains:
    - "*.github.com"
    - "api.openai.com"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".humanconfig.yaml"), []byte(yaml), 0o644))

	cfg, err := LoadConfig(dir)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, ModeAllow, cfg.Mode)
	assert.Equal(t, []string{"*.github.com", "api.openai.com"}, cfg.Domains)
}

func TestLoadConfig_blocklist(t *testing.T) {
	dir := t.TempDir()
	yaml := `proxy:
  mode: blocklist
  domains:
    - "evil.com"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".humanconfig.yaml"), []byte(yaml), 0o644))

	cfg, err := LoadConfig(dir)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, ModeBlock, cfg.Mode)
	assert.Equal(t, []string{"evil.com"}, cfg.Domains)
}

func TestLoadConfig_missingProxySection(t *testing.T) {
	dir := t.TempDir()
	yaml := `jiras:
  - name: work
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".humanconfig.yaml"), []byte(yaml), 0o644))

	cfg, err := LoadConfig(dir)

	require.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestLoadConfig_missingFile(t *testing.T) {
	dir := t.TempDir()

	cfg, err := LoadConfig(dir)

	require.NoError(t, err)
	assert.Nil(t, cfg)
}
