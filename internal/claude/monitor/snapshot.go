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
	Slack      string
	Instances  []InstanceView
	Panes      []claude.TmuxPane
	TotalUsage *claude.UsageSummary

	// internal — carried forward between fetches, not used by renderers.
	hookReaders   []hookevents.EventReader
	sessionByPath map[string]logparser.SessionState
	connectedPIDs map[int]bool // PIDs of Claude instances connected to the daemon
}

// DaemonState holds the daemon liveness info.
type DaemonState struct {
	PID              int
	Alive            bool
	ProxyAddr        string // proxy listen address from daemon.json
	ProxyActiveConns int64  // number of currently active proxy connections
}

// InstanceView pairs a discovered instance with its matched session (if any).
type InstanceView struct {
	Usage   claude.InstanceUsage
	Session *logparser.SessionState // nil if no session matched
}
