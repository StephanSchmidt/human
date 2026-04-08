package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// ConnectedTracker maintains a thread-safe set of recently-seen client PIDs.
// Each PID has a last-seen timestamp; Prune removes entries older than a TTL.
type ConnectedTracker struct {
	mu   sync.Mutex
	pids map[int]time.Time
}

// NewConnectedTracker creates an empty tracker.
func NewConnectedTracker() *ConnectedTracker {
	return &ConnectedTracker{pids: make(map[int]time.Time)}
}

// Touch records or refreshes a PID with the current time.
func (t *ConnectedTracker) Touch(pid int) {
	t.mu.Lock()
	t.pids[pid] = time.Now()
	t.mu.Unlock()
}

// Prune removes PIDs not seen within ttl.
func (t *ConnectedTracker) Prune(ttl time.Duration) {
	cutoff := time.Now().Add(-ttl)
	t.mu.Lock()
	for pid, seen := range t.pids {
		if seen.Before(cutoff) {
			delete(t.pids, pid)
		}
	}
	t.mu.Unlock()
}

// PIDs returns a sorted snapshot of currently tracked PIDs.
func (t *ConnectedTracker) PIDs() []int {
	t.mu.Lock()
	result := make([]int, 0, len(t.pids))
	for pid := range t.pids {
		result = append(result, pid)
	}
	t.mu.Unlock()
	sort.Ints(result)
	return result
}

// ConnectedPath returns the default path for the connected PIDs file.
func ConnectedPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".human", "connected.json")
	}
	return filepath.Join(home, ".human", "connected.json")
}

// WriteConnected atomically writes the connected PIDs to path. On
// rename failure the temporary file is removed so a crashed daemon
// does not leave orphan .tmp files that survive across restarts when
// the process is killed with SIGKILL.
func WriteConnected(path string, pids []int) error {
	data, err := json.Marshal(pids)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ReadConnected reads connected PIDs from path. Returns nil on any error.
func ReadConnected(path string) []int {
	data, err := os.ReadFile(path) // #nosec G304 — path built from ConnectedPath()
	if err != nil {
		return nil
	}
	var pids []int
	if json.Unmarshal(data, &pids) != nil {
		return nil
	}
	return pids
}

// RemoveConnected removes the connected PIDs file (best-effort).
func RemoveConnected(path string) {
	_ = os.Remove(path)
	_ = os.Remove(path + ".tmp")
}
