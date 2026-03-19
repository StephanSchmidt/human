package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDaemonStartCmd_InteractiveRequiresForeground(t *testing.T) {
	t.Setenv(daemonChildEnv, "")

	cmd := buildDaemonStartCmd()
	cmd.SetArgs([]string{"--interactive"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--interactive requires --foreground")
}

func TestDaemonStartCmd_ForegroundFlag(t *testing.T) {
	cmd := buildDaemonStartCmd()
	fg := cmd.Flags().Lookup("foreground")
	require.NotNil(t, fg, "expected --foreground flag to exist")
	assert.Equal(t, "false", fg.DefValue)
}

func TestDaemonLogPath(t *testing.T) {
	p := daemonLogPath()
	assert.Contains(t, p, "daemon.log")
	assert.Contains(t, p, ".human")
}

func TestDaemonPidPath(t *testing.T) {
	p := daemonPidPath()
	assert.Contains(t, p, "daemon.pid")
	assert.Contains(t, p, ".human")
}

func TestWriteAndReadPidFile(t *testing.T) {
	// Use a temp dir to avoid polluting the real ~/.human.
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	pid := os.Getpid()
	err := writePidFile(pid)
	require.NoError(t, err)

	// Verify the file exists with correct content.
	data, err := os.ReadFile(filepath.Join(tmpDir, ".human", "daemon.pid"))
	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(pid), string(data))

	// readAlivePid should find our own process alive.
	gotPid, alive := readAlivePid()
	assert.Equal(t, pid, gotPid)
	assert.True(t, alive)

	// Clean up.
	removePidFile()
	_, err = os.Stat(filepath.Join(tmpDir, ".human", "daemon.pid"))
	assert.True(t, os.IsNotExist(err))
}

func TestReadAlivePid_NoPidFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	pid, alive := readAlivePid()
	assert.Equal(t, 0, pid)
	assert.False(t, alive)
}

func TestReadAlivePid_DeadProcess(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Write a PID that almost certainly doesn't exist.
	err := writePidFile(999999999)
	require.NoError(t, err)

	pid, alive := readAlivePid()
	assert.Equal(t, 999999999, pid)
	assert.False(t, alive)
}

func TestBuildDaemonStopCmd_Exists(t *testing.T) {
	cmd := buildDaemonStopCmd()
	assert.Equal(t, "stop", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
}

func TestBuildDaemonStopCmd_NoPidFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cmd := buildDaemonStopCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "not running")
}

func TestBuildDaemonStatusCmd_PidInfo(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// No PID file, unreachable addr → "not running".
	cmd := buildDaemonStatusCmd()
	cmd.SetArgs([]string{"--addr", "localhost:19999"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, buf.String(), "not running")
}

func TestBuildDaemonStatusCmd_WithPidNotReachable(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Write our own PID so the file exists and process is "alive".
	err := writePidFile(os.Getpid())
	require.NoError(t, err)

	cmd := buildDaemonStatusCmd()
	cmd.SetArgs([]string{"--addr", "localhost:19999"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err = cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, buf.String(), "running")
	assert.Contains(t, buf.String(), "not reachable")
}

func TestDaemonCmd_StopRegistered(t *testing.T) {
	cmd := buildDaemonCmd()
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "stop" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected stop subcommand to be registered")
}
