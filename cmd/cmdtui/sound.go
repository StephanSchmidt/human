package cmdtui

import (
	"os/exec"
	"runtime"

	"github.com/StephanSchmidt/human/internal/platform"
)

// playNotificationSound plays a platform-appropriate notification sound
// in the background. Errors are silently ignored.
func playNotificationSound() {
	go func() {
		name, args := notificationCommand()
		if name == "" {
			return
		}
		_ = exec.Command(name, args...).Run() // #nosec G204 -- name and args are static per-platform constants
	}()
}

func notificationCommand() (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		return "afplay", []string{"/System/Library/Sounds/Glass.aiff"}
	case "linux":
		if platform.IsWSL() {
			return "powershell.exe", []string{"-NoProfile", "-NonInteractive", "-Command", "(New-Object System.Media.SoundPlayer 'C:\\Windows\\Media\\chimes.wav').PlaySync()"}
		}
		return "paplay", []string{"/usr/share/sounds/freedesktop/stereo/complete.oga"}
	default:
		return "", nil
	}
}
