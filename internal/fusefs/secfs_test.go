package fusefs

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func hasFUSE() bool {
	_, err := os.Stat("/dev/fuse")
	return err == nil
}

func setupFUSETest(t *testing.T) (sourceDir, mountPoint string, handle *MountHandle) {
	t.Helper()
	if !hasFUSE() {
		t.Skip("FUSE not available (/dev/fuse missing)")
	}

	sourceDir = t.TempDir()
	mountPoint = t.TempDir()

	// Create test files in source
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "hello.txt"), []byte("hello world"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, ".env"), []byte("SECRET_KEY=sk-12345\nPORT=3000\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, ".env.local"), []byte("DB_PASS=hunter2"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(sourceDir, "subdir"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "subdir", ".env"), []byte("NESTED=secret"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "subdir", "code.go"), []byte("package main"), 0o644))

	logger := zerolog.New(zerolog.NewTestWriter(t))
	handle, err := Mount(sourceDir, mountPoint, logger)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, handle.Unmount())
	})

	return sourceDir, mountPoint, handle
}

func TestFUSE_ReadRegularFile(t *testing.T) {
	_, mountPoint, _ := setupFUSETest(t)

	content, err := os.ReadFile(filepath.Join(mountPoint, "hello.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(content))
}

func TestFUSE_WriteRegularFile(t *testing.T) {
	sourceDir, mountPoint, _ := setupFUSETest(t)

	err := os.WriteFile(filepath.Join(mountPoint, "hello.txt"), []byte("updated"), 0o644)
	require.NoError(t, err)

	// Verify write reached the real file
	content, err := os.ReadFile(filepath.Join(sourceDir, "hello.txt"))
	require.NoError(t, err)
	assert.Equal(t, "updated", string(content))
}

func TestFUSE_ReadEnvFileEmpty(t *testing.T) {
	_, mountPoint, _ := setupFUSETest(t)

	content, err := os.ReadFile(filepath.Join(mountPoint, ".env"))
	require.NoError(t, err)
	assert.Empty(t, content)
}

func TestFUSE_ReadEnvLocalFileEmpty(t *testing.T) {
	_, mountPoint, _ := setupFUSETest(t)

	content, err := os.ReadFile(filepath.Join(mountPoint, ".env.local"))
	require.NoError(t, err)
	assert.Empty(t, content)
}

func TestFUSE_ReadNestedEnvFileEmpty(t *testing.T) {
	_, mountPoint, _ := setupFUSETest(t)

	content, err := os.ReadFile(filepath.Join(mountPoint, "subdir", ".env"))
	require.NoError(t, err)
	assert.Empty(t, content)
}

func TestFUSE_WriteEnvFileBlocked(t *testing.T) {
	_, mountPoint, _ := setupFUSETest(t)

	err := os.WriteFile(filepath.Join(mountPoint, ".env"), []byte("HACK=yes"), 0o644)
	require.Error(t, err)
	assert.ErrorIs(t, err, syscall.EROFS)
}

func TestFUSE_ReadNestedRegularFile(t *testing.T) {
	_, mountPoint, _ := setupFUSETest(t)

	content, err := os.ReadFile(filepath.Join(mountPoint, "subdir", "code.go"))
	require.NoError(t, err)
	assert.Equal(t, "package main", string(content))
}

func TestFUSE_Readdir(t *testing.T) {
	_, mountPoint, _ := setupFUSETest(t)

	entries, err := os.ReadDir(mountPoint)
	require.NoError(t, err)

	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	assert.Contains(t, names, "hello.txt")
	assert.Contains(t, names, ".env")
	assert.Contains(t, names, ".env.local")
	assert.Contains(t, names, "subdir")
}

func TestFUSE_StatEnvFileZeroSize(t *testing.T) {
	_, mountPoint, _ := setupFUSETest(t)

	info, err := os.Stat(filepath.Join(mountPoint, ".env"))
	require.NoError(t, err)
	assert.Equal(t, int64(0), info.Size())
}

func TestFUSE_CreateNewFile(t *testing.T) {
	sourceDir, mountPoint, _ := setupFUSETest(t)

	err := os.WriteFile(filepath.Join(mountPoint, "new.txt"), []byte("new content"), 0o644)
	require.NoError(t, err)

	// Verify file exists on real filesystem
	content, err := os.ReadFile(filepath.Join(sourceDir, "new.txt"))
	require.NoError(t, err)
	assert.Equal(t, "new content", string(content))
}
