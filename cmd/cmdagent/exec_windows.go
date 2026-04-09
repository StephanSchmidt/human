//go:build windows

package cmdagent

import (
	"os"
	"os/exec"
)

// syscallExec on Windows falls back to running the command as a child process.
func syscallExec(path string, args []string, env []string) error {
	cmd := exec.Command(path, args[1:]...) // #nosec G204 -- intentional
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
