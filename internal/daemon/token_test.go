package daemon

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withMemFs(t *testing.T) {
	t.Helper()
	orig := fs
	fs = afero.NewMemMapFs()
	t.Cleanup(func() { fs = orig })
}

func TestGenerateToken(t *testing.T) {
	tok, err := GenerateToken()
	require.NoError(t, err)
	assert.Len(t, tok, tokenBytes*2, "token should be hex-encoded 32 bytes")

	tok2, err := GenerateToken()
	require.NoError(t, err)
	assert.NotEqual(t, tok, tok2, "tokens should be unique")
}

func TestTokenPath(t *testing.T) {
	path := TokenPath()
	assert.Contains(t, path, "human")
	assert.Contains(t, path, "daemon-token")
}

func TestLoadOrCreateTokenAt_createsNew(t *testing.T) {
	withMemFs(t)
	path := "/tmp/test/human/daemon-token"

	tok, err := loadOrCreateTokenAt(path)
	require.NoError(t, err)
	assert.Len(t, tok, tokenBytes*2)

	// File should exist.
	exists, err := afero.Exists(fs, path)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestLoadOrCreateTokenAt_reusesExisting(t *testing.T) {
	withMemFs(t)
	path := "/tmp/test/human/daemon-token"

	tok1, err := loadOrCreateTokenAt(path)
	require.NoError(t, err)

	tok2, err := loadOrCreateTokenAt(path)
	require.NoError(t, err)

	assert.Equal(t, tok1, tok2, "should return the same token on second call")
}

func TestLoadOrCreateTokenAt_regeneratesInvalidToken(t *testing.T) {
	withMemFs(t)
	path := "/tmp/test/human/daemon-token"

	require.NoError(t, fs.MkdirAll("/tmp/test/human", 0o700))
	require.NoError(t, afero.WriteFile(fs, path, []byte("too-short"), 0o600))

	tok, err := loadOrCreateTokenAt(path)
	require.NoError(t, err)
	assert.Len(t, tok, tokenBytes*2, "should generate a valid token when existing one is invalid")
}
