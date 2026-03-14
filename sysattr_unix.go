//go:build !windows

package main

import "syscall"

// detachSysProcAttr returns SysProcAttr that creates a new session,
// so the child process survives terminal close.
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
