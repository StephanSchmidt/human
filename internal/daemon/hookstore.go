package daemon

import (
	"sync"
	"time"

	"github.com/StephanSchmidt/human/internal/claude/hookevents"
)

const maxHookEvents = 100

// HookEventStore is a thread-safe ring buffer of recent hook events.
// It stores raw events and can derive per-session snapshots on demand.
// Subscribers are notified (non-blocking) whenever a new event is appended.
type HookEventStore struct {
	mu          sync.Mutex
	events      []hookevents.Event
	subscribers []chan struct{}
}

// NewHookEventStore creates an empty store.
func NewHookEventStore() *HookEventStore {
	return &HookEventStore{
		events: make([]hookevents.Event, 0, maxHookEvents),
	}
}

// Append adds a hook event. If the buffer exceeds maxHookEvents, the oldest
// event is dropped. All subscribers are notified.
func (s *HookEventStore) Append(evt hookevents.Event) {
	s.mu.Lock()
	s.events = append(s.events, evt)
	if len(s.events) > maxHookEvents {
		// Shift: drop oldest events beyond the limit.
		copy(s.events, s.events[len(s.events)-maxHookEvents:])
		s.events = s.events[:maxHookEvents]
	}
	// Copy subscribers slice under lock to iterate safely.
	subs := make([]chan struct{}, len(s.subscribers))
	copy(subs, s.subscribers)
	s.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- struct{}{}:
		default: // non-blocking — subscriber already has a pending notification
		}
	}
}

// Subscribe returns a channel that receives a signal whenever a new event is
// appended. The channel has a buffer of 1 so a single pending notification is
// coalesced. Call Unsubscribe to clean up.
func (s *HookEventStore) Subscribe() chan struct{} {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.subscribers = append(s.subscribers, ch)
	s.mu.Unlock()
	return ch
}

// Unsubscribe removes a previously registered channel from the subscriber
// list. The channel is not closed — subscribers must stop reading from it
// after calling Unsubscribe and let it be garbage collected. This avoids
// coordinating with any concurrent Append on a removed channel.
func (s *HookEventStore) Unsubscribe(ch chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sub := range s.subscribers {
		if sub == ch {
			s.subscribers = append(s.subscribers[:i], s.subscribers[i+1:]...)
			return
		}
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
// Expected args: [event, session_id, cwd, notification_type, tool_name, error_type].
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
	if len(args) > 4 {
		evt.ToolName = args[4]
	}
	if len(args) > 5 {
		evt.ErrorType = args[5]
	}
	return evt
}
