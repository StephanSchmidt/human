package config

import "os"

const (
	// DirCwd means "the caller's working directory". Use for direct CLI
	// invocations where the user's cwd is the intended config location.
	DirCwd = "."

	// DirProject means "the project directory for this request". Inside the
	// daemon this resolves to the registered project directory via the
	// HUMAN_PROJECT_DIR env var. Outside the daemon it falls back to ".".
	DirProject = "@project"
)

// ResolveDir maps dir sentinel values to real paths.
// DirProject is resolved via HUMAN_PROJECT_DIR env var (set by the daemon
// per-request under envMu). All other values pass through unchanged.
func ResolveDir(dir string) string {
	if dir == DirProject {
		if d := os.Getenv("HUMAN_PROJECT_DIR"); d != "" {
			return d
		}
		return "."
	}
	return dir
}
