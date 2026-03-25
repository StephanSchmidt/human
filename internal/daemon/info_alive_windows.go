//go:build windows

package daemon

import (
	"golang.org/x/sys/windows"
)

// IsAlive checks whether the daemon process identified by PID is still running.
// On Windows, os.FindProcess always succeeds, so we open a process handle and
// check the exit code instead.
func (d DaemonInfo) IsAlive() bool {
	if d.PID <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(d.PID))
	if err != nil {
		return false
	}
	defer func() { _ = windows.CloseHandle(h) }()

	var exitCode uint32
	if err := windows.GetExitCodeProcess(h, &exitCode); err != nil {
		return false
	}
	const stillActive = 259
	return exitCode == stillActive
}
