//go:build windows

package daemon

import (
	"golang.org/x/sys/windows"
)

// IsProcessAlive checks whether a process with the given PID is still running.
// On Windows, os.FindProcess always succeeds, so we open a process handle and
// check the exit code instead.
func IsProcessAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer func() { _ = windows.CloseHandle(h) }()

	var exitCode uint32
	if err := windows.GetExitCodeProcess(h, &exitCode); err != nil {
		return false
	}
	// STILL_ACTIVE (259) means the process has not exited yet.
	const stillActive = 259
	return exitCode == stillActive
}
