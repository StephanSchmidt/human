//go:build !windows

package daemon

import (
	"os"
	"syscall"
)

// IsAlive checks whether the daemon process identified by PID is still running.
func (d DaemonInfo) IsAlive() bool {
	if d.PID <= 0 {
		return false
	}
	p, err := os.FindProcess(d.PID)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
