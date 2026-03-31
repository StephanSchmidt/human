package daemon

import (
	"fmt"
	"sync"
	"time"
)

const confirmTimeout = 5 * time.Minute

// PendingConfirmation represents a destructive operation that is blocked
// waiting for user confirmation via the TUI.
type PendingConfirmation struct {
	ID        string
	Operation string // "DeleteIssue", "EditIssue"
	Tracker   string // tracker kind, e.g. "jira"
	Key       string // issue key, e.g. "KAN-1"
	Prompt    string // human-readable, e.g. "Delete KAN-1?"
	ClientPID int    // PID of the Claude instance that triggered the operation
	CreatedAt time.Time
	Decision  chan bool // the blocked goroutine waits on this; true = approved
}

// PendingConfirmStore is a thread-safe store for destructive operations
// awaiting user confirmation. The daemon adds entries when it intercepts
// destructive commands; the TUI polls the snapshot and resolves them.
type PendingConfirmStore struct {
	mu  sync.Mutex
	ops map[string]*PendingConfirmation
}

// NewPendingConfirmStore creates an empty store.
func NewPendingConfirmStore() *PendingConfirmStore {
	return &PendingConfirmStore{
		ops: make(map[string]*PendingConfirmation),
	}
}

// Add stores a pending confirmation. The caller should block on pc.Decision
// after calling Add.
func (s *PendingConfirmStore) Add(pc *PendingConfirmation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ops[pc.ID] = pc
}

// Resolve sends the decision to the waiting goroutine and removes the entry.
// Returns an error if the ID is not found.
func (s *PendingConfirmStore) Resolve(id string, approved bool) error {
	s.mu.Lock()
	pc, ok := s.ops[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("no pending confirmation with id %q", id)
	}
	delete(s.ops, id)
	s.mu.Unlock()

	// Send decision outside the lock to avoid blocking.
	pc.Decision <- approved
	return nil
}

// Snapshot returns all pending confirmations as wire types for the TUI.
func (s *PendingConfirmStore) Snapshot() []PendingConfirm {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]PendingConfirm, 0, len(s.ops))
	for _, pc := range s.ops {
		out = append(out, PendingConfirm{
			ID:        pc.ID,
			Operation: pc.Operation,
			Tracker:   pc.Tracker,
			Key:       pc.Key,
			Prompt:    pc.Prompt,
			CreatedAt: pc.CreatedAt.UTC().Format(time.RFC3339),
			ClientPID: pc.ClientPID,
		})
	}
	return out
}

// Cleanup rejects and removes all pending confirmations older than maxAge.
func (s *PendingConfirmStore) Cleanup(maxAge time.Duration) {
	now := time.Now()
	s.mu.Lock()
	var expired []*PendingConfirmation
	for id, pc := range s.ops {
		if now.Sub(pc.CreatedAt) > maxAge {
			expired = append(expired, pc)
			delete(s.ops, id)
		}
	}
	s.mu.Unlock()

	// Reject outside the lock.
	for _, pc := range expired {
		select {
		case pc.Decision <- false:
		default:
		}
	}
}

// Len returns the number of pending confirmations.
func (s *PendingConfirmStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.ops)
}
