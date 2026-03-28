package hookevents

import "time"

// Event represents a single hook event line from events.jsonl.
type Event struct {
	EventName        string    `json:"event"`
	SessionID        string    `json:"session_id"`
	Cwd              string    `json:"cwd"`
	Timestamp        time.Time `json:"timestamp"`
	NotificationType string    `json:"notification_type,omitempty"`
}

// SessionSnapshot holds the derived working/idle state for one session.
type SessionSnapshot struct {
	SessionID   string
	Cwd         string
	IsWorking   bool
	IsBlocked   bool // waiting for permission approval
	HasError    bool // stopped due to API error or failure
	IsEnded     bool // session has ended
	LastEventAt time.Time
}
