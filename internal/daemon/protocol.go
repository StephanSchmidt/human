package daemon

import "github.com/StephanSchmidt/human/internal/tracker"

// TrackerIssuesResult is the wire type for a single tracker/project's issues.
type TrackerIssuesResult struct {
	TrackerName string          `json:"tracker_name"`
	TrackerKind string          `json:"tracker_kind"`
	Project     string          `json:"project"`
	Issues      []tracker.Issue `json:"issues"`
	Err         string          `json:"error,omitempty"`
}

// Request is sent from the client to the daemon (one JSON line per connection).
type Request struct {
	Version   string            `json:"version"`
	Token     string            `json:"token"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env,omitempty"`
	ClientPID int               `json:"client_pid,omitempty"` // parent PID (Claude process) for connection tracking
	Cwd       string            `json:"cwd,omitempty"`        // client working directory for project routing
}

// Response is sent from the daemon back to the client (one or more JSON lines per connection).
type Response struct {
	Stdout        string `json:"stdout"`
	Stderr        string `json:"stderr"`
	ExitCode      int    `json:"exit_code"`
	AwaitCallback bool   `json:"await_callback,omitempty"`
	Callback      string `json:"callback,omitempty"`
}
