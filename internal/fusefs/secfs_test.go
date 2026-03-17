//go:build linux

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

// setupFUSETest creates a FUSE mount in safe mode (empty files) for backward-compatible tests.
func setupFUSETest(t *testing.T) (sourceDir, mountPoint string, handle *MountHandle) {
	return setupFUSETestWithMode(t, true)
}

// setupFUSETestWithMode creates a FUSE mount with the given safeMode setting.
func setupFUSETestWithMode(t *testing.T, safeMode bool) (sourceDir, mountPoint string, handle *MountHandle) {
	t.Helper()
	if !hasFUSE() {
		t.Skip("FUSE not available (/dev/fuse missing)")
	}

	sourceDir = t.TempDir()
	mountPoint = t.TempDir()

	// Create test files in source
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "hello.txt"), []byte("hello world"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, ".env"), []byte("SECRET_KEY=sk-12345\nPORT=3000\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, ".env.local"), []byte("DB_PASSWORD=hunter2"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(sourceDir, "subdir"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "subdir", ".env"), []byte("NESTED=secret"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "subdir", "code.go"), []byte("package main"), 0o644))

	logger := zerolog.New(zerolog.NewTestWriter(t))
	handle, err := Mount(sourceDir, mountPoint, safeMode, logger)
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

// --- Redact mode tests (safeMode=false) ---

func TestFUSE_RedactMode_EnvFileRedacted(t *testing.T) {
	_, mountPoint, _ := setupFUSETestWithMode(t, false)

	content, err := os.ReadFile(filepath.Join(mountPoint, ".env"))
	require.NoError(t, err)
	assert.Equal(t, "SECRET_KEY=***\nPORT=3000\n", string(content))
}

func TestFUSE_RedactMode_EnvLocalRedacted(t *testing.T) {
	_, mountPoint, _ := setupFUSETestWithMode(t, false)

	content, err := os.ReadFile(filepath.Join(mountPoint, ".env.local"))
	require.NoError(t, err)
	assert.Equal(t, "DB_PASSWORD=***", string(content))
}

func TestFUSE_RedactMode_WriteBlocked(t *testing.T) {
	_, mountPoint, _ := setupFUSETestWithMode(t, false)

	err := os.WriteFile(filepath.Join(mountPoint, ".env"), []byte("HACK=yes"), 0o644)
	require.Error(t, err)
	assert.ErrorIs(t, err, syscall.EROFS)
}

func TestFUSE_RedactMode_RegularFilePassthrough(t *testing.T) {
	_, mountPoint, _ := setupFUSETestWithMode(t, false)

	content, err := os.ReadFile(filepath.Join(mountPoint, "hello.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(content))
}

func TestFUSE_RedactMode_StatReportsRedactedSize(t *testing.T) {
	_, mountPoint, _ := setupFUSETestWithMode(t, false)

	info, err := os.Stat(filepath.Join(mountPoint, ".env"))
	require.NoError(t, err)
	// "SECRET_KEY=***\nPORT=3000\n" = 25 bytes
	assert.Equal(t, int64(25), info.Size())
}

// --- Expanded sensitive file pattern tests ---

func TestFUSE_SafeMode_OpaqueFileEmpty(t *testing.T) {
	sourceDir, mountPoint, _ := setupFUSETestWithMode(t, true)

	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "server.pem"), []byte("-----BEGIN CERTIFICATE-----"), 0o644))

	content, err := os.ReadFile(filepath.Join(mountPoint, "server.pem"))
	require.NoError(t, err)
	assert.Empty(t, content)
}

func TestFUSE_RedactMode_OpaqueFileStillEmpty(t *testing.T) {
	sourceDir, mountPoint, _ := setupFUSETestWithMode(t, false)

	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "server.key"), []byte("-----BEGIN PRIVATE KEY-----"), 0o644))

	content, err := os.ReadFile(filepath.Join(mountPoint, "server.key"))
	require.NoError(t, err)
	assert.Empty(t, content, "opaque files should be empty even in redact mode")
}

func TestFUSE_RedactMode_NpmrcRedacted(t *testing.T) {
	sourceDir, mountPoint, _ := setupFUSETestWithMode(t, false)

	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, ".npmrc"), []byte("//registry.npmjs.org/:_authToken=npm_abc123\nregistry=https://registry.npmjs.org/\n"), 0o644))

	content, err := os.ReadFile(filepath.Join(mountPoint, ".npmrc"))
	require.NoError(t, err)
	// _authToken contains TOKEN keyword → redacted
	assert.Contains(t, string(content), "=***")
	assert.Contains(t, string(content), "registry=https://registry.npmjs.org/")
}

func TestFUSE_SafeMode_NpmrcEmpty(t *testing.T) {
	sourceDir, mountPoint, _ := setupFUSETestWithMode(t, true)

	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, ".npmrc"), []byte("//registry.npmjs.org/:_authToken=npm_abc123\n"), 0o644))

	content, err := os.ReadFile(filepath.Join(mountPoint, ".npmrc"))
	require.NoError(t, err)
	assert.Empty(t, content, "safe mode should return empty for .npmrc")
}
