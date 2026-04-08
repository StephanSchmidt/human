//go:build !windows

package chrome

import "syscall"

// withRestrictiveUmask runs fn under a 0o077 umask so inodes it creates
// are born with 0600-compatible permissions, eliminating the TOCTOU
// window between creation and Chmod.
func withRestrictiveUmask(fn func()) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	fn()
}
