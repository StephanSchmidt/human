package daemon

import (
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteAndReadInfo(t *testing.T) {
	withMemFs(t)

	info := DaemonInfo{
		Addr:       "192.168.1.5:19285",
		ChromeAddr: "192.168.1.5:19286",
		ProxyAddr:  "192.168.1.5:19287",
		Token:      "abc123",
		PID:        12345,
	}

	err := WriteInfo(info)
	require.NoError(t, err)

	got, err := ReadInfo()
	require.NoError(t, err)
	assert.Equal(t, info, got)
}

func TestWriteInfo_CreatesDirectory(t *testing.T) {
	withMemFs(t)

	info := DaemonInfo{Addr: "localhost:19285", Token: "tok", PID: 1}
	err := WriteInfo(info)
	require.NoError(t, err)

	exists, err := afero.DirExists(fs, InfoPath()[:len(InfoPath())-len("/daemon.json")])
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestWriteInfo_RestrictedPermissions(t *testing.T) {
	withMemFs(t)

	info := DaemonInfo{Addr: "localhost:19285", Token: "secret", PID: 1}
	require.NoError(t, WriteInfo(info))

	fi, err := fs.Stat(InfoPath())
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
}

func TestReadInfo_NotExists(t *testing.T) {
	withMemFs(t)

	_, err := ReadInfo()
	assert.Error(t, err)
}

func TestRemoveInfo(t *testing.T) {
	withMemFs(t)

	info := DaemonInfo{Addr: "localhost:19285", Token: "tok", PID: 1}
	require.NoError(t, WriteInfo(info))

	RemoveInfo()

	exists, err := afero.Exists(fs, InfoPath())
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestRemoveInfo_NoFile(t *testing.T) {
	withMemFs(t)
	// Should not panic or error when file doesn't exist.
	RemoveInfo()
}

func TestDaemonInfo_IsAlive_CurrentProcess(t *testing.T) {
	info := DaemonInfo{PID: os.Getpid()}
	assert.True(t, info.IsAlive())
}

func TestDaemonInfo_IsAlive_InvalidPID(t *testing.T) {
	info := DaemonInfo{PID: -1}
	assert.False(t, info.IsAlive())
}

func TestDaemonInfo_IsAlive_ZeroPID(t *testing.T) {
	info := DaemonInfo{PID: 0}
	assert.False(t, info.IsAlive())
}
