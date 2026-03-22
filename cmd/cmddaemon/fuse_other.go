//go:build !linux

package cmddaemon

import "github.com/rs/zerolog"

// fuseMount is a no-op on non-Linux platforms.
func fuseMount(_ string, _ bool, _ zerolog.Logger) func() {
	return nil
}
