package claude

import (
	"encoding/json"
	"fmt"

	"github.com/rs/zerolog/log"
)

// ProbeTraceEntry records what a single probe decided.
type ProbeTraceEntry struct {
	Probe      string  `json:"probe"`
	Result     string  `json:"result"`     // "busy", "abstain", "error", "unknown"
	Confidence float64 `json:"confidence,omitempty"`
	Detail     string  `json:"detail,omitempty"`
}

// ProbeTrace records the full reasoning of a CompositeStateReader decision.
type ProbeTrace struct {
	PID      int               `json:"pid"`
	FilePath string            `json:"file_path,omitempty"`
	Entries  []ProbeTraceEntry `json:"probes"`
	Verdict  string            `json:"verdict"`
}

// String returns the trace as a compact JSON string.
func (t ProbeTrace) String() string {
	b, _ := json.Marshal(t)
	return string(b)
}

// ProbeResult holds the outcome of a single probe check.
type ProbeResult struct {
	State      InstanceState
	Confidence float64 // 0.0–1.0
	Source     string  // probe name
}

// Probe is a pluggable signal for detecting Claude instance state.
type Probe interface {
	Name() string
	Check(pid int, jsonlPath string) (*ProbeResult, error)
}

// CompositeStateReader aggregates multiple probes and implements StateReader.
//
// Logic:
//  1. Assume Ready (idle).
//  2. Run every probe looking for evidence the instance is NOT idle.
//  3. If any probe says Busy → Busy.
//  4. If liveness probe says dead → Unknown.
//  5. Otherwise → Ready.
type CompositeStateReader struct {
	Probes   []Probe
	PID      int
	FilePath string // resolved JSONL path

	// LastTrace holds the reasoning from the most recent ReadState call.
	LastTrace *ProbeTrace
}

func (c *CompositeStateReader) ReadState(_ string) (InstanceState, error) {
	trace := ProbeTrace{PID: c.PID, FilePath: c.FilePath}

	for _, p := range c.Probes {
		result, err := p.Check(c.PID, c.FilePath)
		if err != nil {
			trace.Entries = append(trace.Entries, ProbeTraceEntry{
				Probe:  p.Name(),
				Result: "error",
				Detail: err.Error(),
			})
			log.Debug().Err(err).Str("probe", p.Name()).Int("pid", c.PID).Msg("probe error, skipping")
			continue
		}
		if result == nil {
			trace.Entries = append(trace.Entries, ProbeTraceEntry{
				Probe:  p.Name(),
				Result: "abstain",
			})
			continue
		}

		entry := ProbeTraceEntry{
			Probe:      p.Name(),
			Confidence: result.Confidence,
		}

		// Dead process → Unknown, stop immediately.
		if p.Name() == "process-liveness" && result.State == StateUnknown {
			entry.Result = "dead"
			trace.Entries = append(trace.Entries, entry)
			trace.Verdict = "unknown"
			c.LastTrace = &trace
			return StateUnknown, nil
		}

		// Any probe that says Busy → instance is busy.
		if result.State == StateBusy {
			entry.Result = "busy"
			trace.Entries = append(trace.Entries, entry)
			trace.Verdict = fmt.Sprintf("busy (decided by %s)", p.Name())
			c.LastTrace = &trace
			return StateBusy, nil
		}

		entry.Result = result.State.String()
		trace.Entries = append(trace.Entries, entry)
	}

	trace.Verdict = "ready (no probe said busy)"
	c.LastTrace = &trace
	return StateReady, nil
}
