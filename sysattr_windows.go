//go:build windows

package main

import (
	"os"
	"syscall"
)

// detachSysProcAttr returns SysProcAttr for Windows (no Setsid equivalent needed).
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

// isProcessAlive checks whether a process with the given PID exists.
// On Windows, FindProcess always succeeds, so this is a best-effort check.
func isProcessAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = p
	return true
}

// stopProcess kills the process with the given PID.
// Windows lacks SIGTERM, so we use Kill().
func stopProcess(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
