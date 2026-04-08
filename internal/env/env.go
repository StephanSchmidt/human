// Package env provides per-request environment variable lookup that
// avoids mutating the process environment.
//
// The daemon serves multiple concurrent client requests in the same
// process. Forwarding client env vars by mutating os.Environ would
// either require a process-wide mutex held across cmd.Execute() (which
// serialises all command execution) or risk cross-request contamination
// (one client's env value leaking into another client's command).
//
// Instead, the daemon attaches a per-request env map to the cobra
// command's context and code that previously called os.Getenv now calls
// env.Lookup(ctx, key). The lookup falls back to os.Getenv when no
// per-request map is present so direct CLI invocations behave normally.
package env

import (
	"context"
	"os"
)

// envKey is a private context key type to avoid collisions with other
// packages stashing values on the same context.
type envKey struct{}

// WithEnv returns ctx augmented with the given env map. The map is
// looked up first by Lookup before falling back to os.Getenv.
func WithEnv(ctx context.Context, env map[string]string) context.Context {
	if env == nil {
		return ctx
	}
	return context.WithValue(ctx, envKey{}, env)
}

// Lookup returns the value for key, preferring the per-request env map
// stored on ctx and falling back to the process environment.
func Lookup(ctx context.Context, key string) string {
	if ctx != nil {
		if m, ok := ctx.Value(envKey{}).(map[string]string); ok {
			if v, present := m[key]; present {
				return v
			}
		}
	}
	return os.Getenv(key)
}
