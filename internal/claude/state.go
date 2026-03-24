package claude

import (
	"os"
	"path/filepath"
	"strings"
)

// InstanceState represents whether a Claude Code instance is busy or ready.
type InstanceState int

const (
	StateUnknown InstanceState = iota
	StateBusy
	StateReady
)

func (s InstanceState) String() string {
	switch s {
	case StateBusy:
		return "🔴"
	case StateReady:
		return "🟢"
	default:
		return "⚪"
	}
}

// findNewestJSONL finds the most recently modified .jsonl file under root.
func findNewestJSONL(root string) (string, error) {
	var newest string
	var newestMod int64

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible
		}
		if info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		mod := info.ModTime().UnixNano()
		if mod > newestMod {
			newestMod = mod
			newest = path
		}
		return nil
	})
	return newest, err
}
