package claude

import "time"

// busyDebounce is how long the JSONL probe stays sticky-busy after seeing
// a Busy signal. This prevents flickering during tool-use loops where the
// JSONL tail briefly shows "user" or "end_turn" between tool calls.
const busyDebounce = 5 * time.Second

// JSONLProbe reads state from a JSONL file using the existing DetermineState logic.
// Once Busy is detected, it stays Busy for busyDebounce to avoid flickering.
type JSONLProbe struct {
	busyUntil map[string]time.Time // keyed by jsonlPath
}

func (j *JSONLProbe) Name() string { return "jsonl" }

func (j *JSONLProbe) Check(_ int, jsonlPath string) (*ProbeResult, error) {
	if jsonlPath == "" {
		return nil, nil // abstain
	}

	state, err := readStateAdaptive(jsonlPath)
	if err != nil {
		return nil, err
	}

	now := time.Now()

	if state == StateBusy {
		// Refresh the debounce window.
		if j.busyUntil == nil {
			j.busyUntil = make(map[string]time.Time)
		}
		j.busyUntil[jsonlPath] = now.Add(busyDebounce)
		return &ProbeResult{
			State:      StateBusy,
			Confidence: 0.8,
			Source:     "jsonl",
		}, nil
	}

	// Not busy right now — but stay busy if within debounce window.
	if j.busyUntil != nil {
		if until, ok := j.busyUntil[jsonlPath]; ok && now.Before(until) {
			return &ProbeResult{
				State:      StateBusy,
				Confidence: 0.6,
				Source:     "jsonl",
			}, nil
		}
	}

	return nil, nil // abstain
}

// OSStateFallbackProbe wraps the OSStateReader logic as a probe.
// Used when session resolution fails and we need to scan for the newest
// JSONL file in the project root directory.
type OSStateFallbackProbe struct {
	Root string
}

func (o *OSStateFallbackProbe) Name() string { return "jsonl" }

func (o *OSStateFallbackProbe) Check(_ int, _ string) (*ProbeResult, error) {
	reader := OSStateReader{}
	state, err := reader.ReadState(o.Root)
	if err != nil {
		return nil, err
	}

	// Only report Busy — abstain on Unknown (no evidence either way).
	if state != StateBusy {
		return nil, nil
	}

	return &ProbeResult{
		State:      StateBusy,
		Confidence: 0.7,
		Source:     "jsonl",
	}, nil
}

// readStateAdaptive reads the tail of a JSONL file, trying progressively
// larger buffers if the initial read yields no valid entries (RC-10).
func readStateAdaptive(path string) (InstanceState, error) {
	// Try 64KB first.
	lines, err := readTailLines(path, 64*1024)
	if err != nil {
		return StateUnknown, err
	}

	state := DetermineState(lines)
	if state != StateUnknown {
		return state, nil
	}

	// RC-10: If 64KB wasn't enough, try 256KB.
	lines, err = readTailLines(path, 256*1024)
	if err != nil {
		return StateUnknown, err
	}

	return DetermineState(lines), nil
}
