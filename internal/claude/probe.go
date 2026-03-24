package claude

import (
	"github.com/rs/zerolog/log"
)

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
}

func (c *CompositeStateReader) ReadState(_ string) (InstanceState, error) {
	for _, p := range c.Probes {
		result, err := p.Check(c.PID, c.FilePath)
		if err != nil {
			log.Debug().Err(err).Str("probe", p.Name()).Msg("probe error, skipping")
			continue
		}
		if result == nil {
			continue // probe abstains
		}

		// Dead process → Unknown, stop immediately.
		if p.Name() == "process-liveness" && result.State == StateUnknown {
			return StateUnknown, nil
		}

		// Any probe that says Busy → instance is busy.
		if result.State == StateBusy {
			return StateBusy, nil
		}
	}

	return StateReady, nil
}
