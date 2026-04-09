//go:build windows

package cmddevcontainer

import "os/exec"

// syscallExec on Windows falls back to running the command as a child process
// since Windows does not support the exec syscall.
func syscallExec(path string, args []string, env []string) error {
	cmd := exec.Command(path, args[1:]...) // #nosec G204 -- intentional
	cmd.Env = env
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}
