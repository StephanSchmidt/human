package index

import (
	"os"
	"path/filepath"
)

// DefaultDBPath returns the path to the index database (~/.human/index.db),
// creating the directory if needed.
func DefaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".human", "index.db")
	}
	dir := filepath.Join(home, ".human")
	_ = os.MkdirAll(dir, 0o750)
	return filepath.Join(dir, "index.db")
}
