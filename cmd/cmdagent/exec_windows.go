//go:build windows

package cmdagent

import (
	"os"
	"os/exec"
)

// execTmuxAttach runs tmux attach-session as a child process on Windows,
// since syscall.Exec is not available.
func execTmuxAttach(tmuxPath, sessionName string) error {
	cmd := exec.Command(tmuxPath, "attach-session", "-t", sessionName) // #nosec G204 -- tmuxPath from exec.LookPath, sessionName validated
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
