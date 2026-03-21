package claude

import (
	"encoding/json"
	"io"
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

// StateReader determines the current state of a Claude Code instance.
type StateReader interface {
	ReadState(root string) (InstanceState, error)
}

// stateEntry is the minimal JSON structure needed for state detection.
type stateEntry struct {
	Type    string `json:"type"`
	Message struct {
		StopReason *string `json:"stop_reason"`
	} `json:"message"`
}

// DetermineState walks lines backward and returns the instance state
// based on the last user or assistant entry.
func DetermineState(lines [][]byte) InstanceState {
	if lines == nil {
		return StateUnknown
	}
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if len(line) == 0 {
			continue
		}

		var entry stateEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}

		switch entry.Type {
		case "user":
			return StateBusy
		case "assistant":
			if entry.Message.StopReason == nil {
				return StateBusy
			}
			if *entry.Message.StopReason == "end_turn" {
				return StateReady
			}
			// tool_use or any other stop_reason = busy
			return StateBusy
		default:
			// Skip metadata types: progress, system, file-history-snapshot, etc.
			continue
		}
	}
	return StateUnknown
}

// OSStateReader reads state from the filesystem by finding the most recent
// JSONL file and reading its tail.
type OSStateReader struct{}

func (OSStateReader) ReadState(root string) (InstanceState, error) {
	newest, err := findNewestJSONL(root)
	if err != nil || newest == "" {
		return StateUnknown, err
	}

	lines, err := readTailLines(newest, 64*1024)
	if err != nil {
		return StateUnknown, err
	}
	return DetermineState(lines), nil
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

// readTailLines reads up to tailSize bytes from the end of a file and
// splits them into lines.
func readTailLines(path string, tailSize int64) ([][]byte, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	offset := info.Size() - tailSize
	if offset < 0 {
		offset = 0
	}
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return nil, err
		}
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	// If we seeked into the middle of a line, discard the partial first line.
	if offset > 0 {
		if idx := indexByte(data, '\n'); idx >= 0 {
			data = data[idx+1:]
		}
	}

	return splitLines(data), nil
}

func indexByte(data []byte, b byte) int {
	for i, c := range data {
		if c == b {
			return i
		}
	}
	return -1
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			line := data[start:i]
			if len(line) > 0 {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	// Trailing content without newline.
	if start < len(data) {
		line := data[start:]
		if len(line) > 0 {
			lines = append(lines, line)
		}
	}
	return lines
}

// ByteStateReader reads state from in-memory data (e.g. container output).
type ByteStateReader struct {
	Data []byte
}

func (b *ByteStateReader) ReadState(_ string) (InstanceState, error) {
	if len(b.Data) == 0 {
		return StateUnknown, nil
	}
	lines := splitLines(b.Data)
	return DetermineState(lines), nil
}
