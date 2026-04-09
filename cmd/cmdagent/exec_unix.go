//go:build !windows

package cmdagent

import (
	"syscall"
)

// execTmuxAttach replaces the current process with tmux attach-session.
func execTmuxAttach(tmuxPath, sessionName string) error {
	return syscall.Exec(tmuxPath, []string{"tmux", "attach-session", "-t", sessionName}, nil) // #nosec G204 -- tmuxPath from exec.LookPath, sessionName validated
}
