package hookevents

import "time"

// Event represents a single hook event line from events.jsonl.
type Event struct {
	EventName string    `json:"event"`
	SessionID string    `json:"session_id"`
	Cwd       string    `json:"cwd"`
	Timestamp time.Time `json:"timestamp"`
}

// SessionSnapshot holds the derived working/idle state for one session.
type SessionSnapshot struct {
	SessionID   string
	Cwd         string
	IsWorking   bool
	LastEventAt time.Time
}
