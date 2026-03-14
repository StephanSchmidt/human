//go:build windows

package main

import "syscall"

// detachSysProcAttr returns SysProcAttr for Windows (no Setsid equivalent needed).
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}
