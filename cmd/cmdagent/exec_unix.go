//go:build !windows

package cmdagent

import "syscall"

// syscallExec replaces the current process with the given command.
func syscallExec(path string, args []string, env []string) error {
	return syscall.Exec(path, args, env) // #nosec G204 -- intentional process replacement
}
