package logparser

import "time"

// Subagent represents a spawned Agent tool_use and its lifecycle.
type Subagent struct {
	ToolUseID    string     // tool_use id (e.g. "toolu_01AaZ...") for completion tracking
	Description  string     // from input.description
	SubagentType string     // "Explore", "Plan", etc.
	AgentID      string     // from toolUseResult.agentId on completion
	StartedAt    time.Time
	CompletedAt  *time.Time // nil = still running
	DurationMs   int64      // from toolUseResult.totalDurationMs (0 if still running)
}

// Task represents a TaskCreate/TaskUpdate tool_use and its status.
type Task struct {
	TaskID    string // from toolUseResult.task.id (TaskCreate result) or input.taskId (TaskUpdate)
	ToolUseID string // tool_use id of the TaskCreate — for mapping result → taskId
	Subject   string
	Status    string // "pending", "in_progress", "completed"
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SessionState holds the parsed state for a single JSONL session file.
type SessionState struct {
	SessionID    string
	Cwd          string
	Slug         string
	StartedAt    time.Time
	LastActivity time.Time
	IsWorking    bool // true = Claude actively generating, false = idle/waiting
	Subagents    []Subagent
	Tasks        []Task
}
