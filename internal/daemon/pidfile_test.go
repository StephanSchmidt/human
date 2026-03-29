package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPidPath(t *testing.T) {
	p := PidPath()
	assert.Contains(t, p, "daemon.pid")
	assert.Contains(t, p, ".human")
}

func TestLogPath(t *testing.T) {
	p := LogPath()
	assert.Contains(t, p, "daemon.log")
	assert.Contains(t, p, ".human")
}

func TestWriteAndReadPidFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	pid := os.Getpid()
	err := WritePidFile(pid)
	require.NoError(t, err)

	// Verify the file exists with correct content.
	data, err := os.ReadFile(filepath.Join(tmpDir, ".human", "daemon.pid"))
	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(pid), string(data))

	// ReadAlivePid should find our own process alive.
	gotPid, alive := ReadAlivePid()
	assert.Equal(t, pid, gotPid)
	assert.True(t, alive)

	// Clean up.
	RemovePidFile()
	_, err = os.Stat(filepath.Join(tmpDir, ".human", "daemon.pid"))
	assert.True(t, os.IsNotExist(err))
}

func TestReadAlivePid_NoPidFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	pid, alive := ReadAlivePid()
	assert.Equal(t, 0, pid)
	assert.False(t, alive)
}

func TestReadAlivePid_DeadProcess(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Write a PID that almost certainly doesn't exist.
	err := WritePidFile(999999999)
	require.NoError(t, err)

	pid, alive := ReadAlivePid()
	assert.Equal(t, 999999999, pid)
	assert.False(t, alive)
}

func TestIsProcessAlive_Self(t *testing.T) {
	assert.True(t, IsProcessAlive(os.Getpid()))
}

func TestIsProcessAlive_NonExistent(t *testing.T) {
	assert.False(t, IsProcessAlive(999999999))
}
