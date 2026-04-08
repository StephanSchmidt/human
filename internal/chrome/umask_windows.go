//go:build windows

package chrome

// withRestrictiveUmask is a no-op on Windows, which has no umask
// concept — unix-socket listeners on Windows inherit ACL semantics
// instead and are not exposed to other local users by default.
func withRestrictiveUmask(fn func()) {
	fn()
}
