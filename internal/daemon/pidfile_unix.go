//go:build !windows

package daemon

import (
	"os"
	"syscall"
)

// IsProcessAlive checks whether a process with the given PID is still running.
func IsProcessAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
