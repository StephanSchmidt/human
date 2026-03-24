package claude

// JSONLProbe reads state from a JSONL file using the existing DetermineState logic.
// Improvements over raw file reading:
//   - RC-10: Adaptive tail buffer (64KB → 256KB if no valid entry found)
//   - RC-2:  Retry on unparseable last line (may be partial write)
//   - RC-8:  Skips bad JSON lines (existing DetermineState already does this)
//   - RC-9:  splitLines discards trailing fragment without \n (fixed in state.go)
type JSONLProbe struct{}

func (j *JSONLProbe) Name() string { return "jsonl" }

func (j *JSONLProbe) Check(_ int, jsonlPath string) (*ProbeResult, error) {
	if jsonlPath == "" {
		return nil, nil // abstain
	}

	state, err := readStateAdaptive(jsonlPath)
	if err != nil {
		return nil, err
	}

	return &ProbeResult{
		State:      state,
		Confidence: 0.8,
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
