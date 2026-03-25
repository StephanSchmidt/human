package monitor

import (
	"time"

	"github.com/StephanSchmidt/human/internal/claude"
	"github.com/StephanSchmidt/human/internal/claude/hookevents"
	"github.com/StephanSchmidt/human/internal/claude/logparser"
)

// Snapshot holds the complete TUI display state at a point in time.
type Snapshot struct {
	FetchedAt  time.Time
	Err        error
	Daemon     DaemonState
	Telegram   string
	Instances  []InstanceView
	Panes      []claude.TmuxPane
	TotalUsage *claude.UsageSummary

	// internal — carried forward between fetches, not used by renderers.
	hookReaders   []hookevents.EventReader
	sessionByPath map[string]logparser.SessionState
}

// DaemonState holds the daemon liveness info.
type DaemonState struct {
	PID   int
	Alive bool
}

// InstanceView pairs a discovered instance with its matched session (if any).
type InstanceView struct {
	Usage   claude.InstanceUsage
	Session *logparser.SessionState // nil if no session matched
}
