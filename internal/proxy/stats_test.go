package proxy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteReadStats_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy-stats.json")

	err := WriteStats(path, Stats{ActiveConns: 42})
	require.NoError(t, err)

	got := ReadStats(path)
	assert.Equal(t, int64(42), got.ActiveConns)
}

func TestReadStats_MissingFile(t *testing.T) {
	got := ReadStats(filepath.Join(t.TempDir(), "nonexistent.json"))
	assert.Equal(t, Stats{}, got)
}

func TestReadStats_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o600))

	got := ReadStats(path)
	assert.Equal(t, Stats{}, got)
}

func TestRemoveStats(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy-stats.json")
	require.NoError(t, os.WriteFile(path, []byte("{}"), 0o600))

	RemoveStats(path)
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err))
}
