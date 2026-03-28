package daemon

import (
	"sync"
	"time"

	"github.com/StephanSchmidt/human/internal/claude/hookevents"
)

const maxHookEvents = 100

// HookEventStore is a thread-safe ring buffer of recent hook events.
// It stores raw events and can derive per-session snapshots on demand.
type HookEventStore struct {
	mu     sync.Mutex
	events []hookevents.Event
}

// NewHookEventStore creates an empty store.
func NewHookEventStore() *HookEventStore {
	return &HookEventStore{
		events: make([]hookevents.Event, 0, maxHookEvents),
	}
}

// Append adds a hook event. If the buffer exceeds maxHookEvents, the oldest
// event is dropped.
func (s *HookEventStore) Append(evt hookevents.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, evt)
	if len(s.events) > maxHookEvents {
		// Shift: drop oldest events beyond the limit.
		copy(s.events, s.events[len(s.events)-maxHookEvents:])
		s.events = s.events[:maxHookEvents]
	}
}

// Snapshot returns the current per-session state derived from all stored events.
func (s *HookEventStore) Snapshot() map[string]hookevents.SessionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	sessions := make(map[string]hookevents.SessionSnapshot)
	for _, evt := range s.events {
		if evt.SessionID == "" {
			continue
		}
		snap := sessions[evt.SessionID]
		snap.SessionID = evt.SessionID
		if evt.Cwd != "" {
			snap.Cwd = evt.Cwd
		}
		snap.LastEventAt = evt.Timestamp
		hookevents.ApplyEvent(&snap, &evt)
		sessions[evt.SessionID] = snap
	}
	return sessions
}

// ParseHookEventArgs converts daemon request args into a hook event.
// Expected args: [event, session_id, cwd, notification_type].
func ParseHookEventArgs(args []string) hookevents.Event {
	evt := hookevents.Event{
		Timestamp: time.Now().UTC(),
	}
	if len(args) > 0 {
		evt.EventName = args[0]
	}
	if len(args) > 1 {
		evt.SessionID = args[1]
	}
	if len(args) > 2 {
		evt.Cwd = args[2]
	}
	if len(args) > 3 {
		evt.NotificationType = args[3]
	}
	return evt
}
