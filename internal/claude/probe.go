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
// Probes are evaluated in order; first decisive result wins.
//
// Priority logic:
//  1. ProcessLivenessProbe → dead → Unknown (short-circuit)
//  2. ChildTreeProbe → children exist → Busy (override, confidence 0.9)
//  3. CPUProbe → CPU active + JSONL says Ready → Busy (override)
//  4. JSONLProbe → primary signal from JSONL parsing
//  5. MtimeProbe → cached state if mtime unchanged (fallback)
//  6. All abstain → Unknown
type CompositeStateReader struct {
	Probes   []Probe
	PID      int
	FilePath string // resolved JSONL path
}

// probeOutcome indicates what the caller should do after processing a probe result.
type probeOutcome int

const (
	outcomeContinue probeOutcome = iota // keep evaluating probes
	outcomeReturn                       // return the state immediately
)

func (c *CompositeStateReader) ReadState(_ string) (InstanceState, error) {
	var jsonlState *InstanceState

	for _, p := range c.Probes {
		result, err := p.Check(c.PID, c.FilePath)
		if err != nil {
			log.Debug().Err(err).Str("probe", p.Name()).Msg("probe error, skipping")
			continue
		}
		if result == nil {
			continue // probe abstains
		}

		state, outcome := applyProbeResult(p.Name(), result, jsonlState)
		if outcome == outcomeReturn {
			return state, nil
		}
		if p.Name() == "jsonl" {
			jsonlState = &result.State
		}
	}

	if jsonlState != nil {
		return *jsonlState, nil
	}
	return StateUnknown, nil
}

// applyProbeResult interprets a single probe's result and returns a state
// plus whether the caller should return immediately or keep evaluating.
func applyProbeResult(name string, result *ProbeResult, jsonlState *InstanceState) (InstanceState, probeOutcome) {
	switch name {
	case "process-liveness":
		if result.State == StateUnknown {
			return StateUnknown, outcomeReturn
		}
	case "child-tree":
		if result.State == StateBusy {
			return StateBusy, outcomeReturn
		}
	case "cpu":
		if result.State == StateBusy && jsonlState != nil && *jsonlState == StateReady {
			return StateBusy, outcomeReturn
		}
	case "jsonl":
		// Don't return yet — let CPU probe override if needed.
	case "mtime":
		return result.State, outcomeReturn
	default:
		if result.Confidence >= 0.5 {
			return result.State, outcomeReturn
		}
	}
	return StateUnknown, outcomeContinue
}
