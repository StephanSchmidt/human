package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// PidPath returns the path to the daemon PID file (~/.human/daemon.pid).
func PidPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".human", "daemon.pid")
	}
	dir := filepath.Join(home, ".human")
	_ = os.MkdirAll(dir, 0o750)
	return filepath.Join(dir, "daemon.pid")
}

// LogPath returns the path to the daemon log file (~/.human/daemon.log).
func LogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".human", "daemon.log")
	}
	dir := filepath.Join(home, ".human")
	_ = os.MkdirAll(dir, 0o750)
	return filepath.Join(dir, "daemon.log")
}

// WritePidFile writes the given PID to the PID file.
func WritePidFile(pid int) error {
	return os.WriteFile(PidPath(), []byte(strconv.Itoa(pid)), 0o600)
}

// RemovePidFile removes the PID file (best-effort).
func RemovePidFile() {
	_ = os.Remove(PidPath())
}

// ReadAlivePid reads the PID file and checks if the process is alive.
// Returns (0, false) if no PID file exists or the process is dead.
func ReadAlivePid() (int, bool) {
	data, err := os.ReadFile(PidPath()) // #nosec G304 -- path is built by PidPath(), not user input
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	if !IsProcessAlive(pid) {
		return pid, false
	}
	return pid, true
}
