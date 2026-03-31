// Package platform provides OS and environment detection helpers.
package platform

import (
	"os"
	"strings"
)

// IsWSL reports whether the process is running inside Windows Subsystem for Linux.
func IsWSL() bool {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), "microsoft")
}
