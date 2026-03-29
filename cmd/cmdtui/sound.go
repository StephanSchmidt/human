package cmdtui

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
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
		if isWSL() {
			return "powershell.exe", []string{"-NoProfile", "-NonInteractive", "-Command", "(New-Object System.Media.SoundPlayer 'C:\\Windows\\Media\\chimes.wav').PlaySync()"}
		}
		return "paplay", []string{"/usr/share/sounds/freedesktop/stereo/complete.oga"}
	default:
		return "", nil
	}
}

func isWSL() bool {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), "microsoft")
}
