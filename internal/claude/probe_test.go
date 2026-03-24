package claude

import (
	"fmt"
	"testing"
)

// --- CompositeStateReader tests ---

type stubProbe struct {
	name   string
	result *ProbeResult
	err    error
}

func (s *stubProbe) Name() string { return s.name }
func (s *stubProbe) Check(_ int, _ string) (*ProbeResult, error) {
	return s.result, s.err
}

func TestCompositeStateReader_JSONLOnly(t *testing.T) {
	csr := &CompositeStateReader{
		Probes: []Probe{
			&stubProbe{name: "jsonl", result: &ProbeResult{State: StateReady, Confidence: 0.8, Source: "jsonl"}},
		},
		PID:      123,
		FilePath: "/tmp/test.jsonl",
	}
	state, err := csr.ReadState("")
	if err != nil {
		t.Fatal(err)
	}
	if state != StateReady {
		t.Errorf("got %v, want Ready", state)
	}
}

func TestCompositeStateReader_LivenessDeadShortCircuits(t *testing.T) {
	csr := &CompositeStateReader{
		Probes: []Probe{
			&stubProbe{name: "process-liveness", result: &ProbeResult{State: StateUnknown, Confidence: 1.0, Source: "process-liveness"}},
			&stubProbe{name: "jsonl", result: &ProbeResult{State: StateReady, Confidence: 0.8, Source: "jsonl"}},
		},
		PID: 123,
	}
	state, err := csr.ReadState("")
	if err != nil {
		t.Fatal(err)
	}
	if state != StateUnknown {
		t.Errorf("got %v, want Unknown (dead process)", state)
	}
}

func TestCompositeStateReader_ChildTreeOverrides(t *testing.T) {
	csr := &CompositeStateReader{
		Probes: []Probe{
			&stubProbe{name: "process-liveness", result: &ProbeResult{State: StateBusy, Confidence: 0.1, Source: "process-liveness"}},
			&stubProbe{name: "child-tree", result: &ProbeResult{State: StateBusy, Confidence: 0.9, Source: "child-tree"}},
			&stubProbe{name: "jsonl", result: &ProbeResult{State: StateReady, Confidence: 0.8, Source: "jsonl"}},
		},
		PID: 123,
	}
	state, err := csr.ReadState("")
	if err != nil {
		t.Fatal(err)
	}
	if state != StateBusy {
		t.Errorf("got %v, want Busy (child tree override)", state)
	}
}

func TestCompositeStateReader_CPUOverridesReadyJSONL(t *testing.T) {
	csr := &CompositeStateReader{
		Probes: []Probe{
			&stubProbe{name: "process-liveness", result: &ProbeResult{State: StateBusy, Confidence: 0.1, Source: "process-liveness"}},
			&stubProbe{name: "child-tree", result: nil}, // no children
			&stubProbe{name: "jsonl", result: &ProbeResult{State: StateReady, Confidence: 0.8, Source: "jsonl"}},
			&stubProbe{name: "cpu", result: &ProbeResult{State: StateBusy, Confidence: 0.7, Source: "cpu"}},
		},
		PID: 123,
	}
	state, err := csr.ReadState("")
	if err != nil {
		t.Fatal(err)
	}
	if state != StateBusy {
		t.Errorf("got %v, want Busy (CPU override of Ready JSONL)", state)
	}
}

func TestCompositeStateReader_CPUDoesNotOverrideBusyJSONL(t *testing.T) {
	csr := &CompositeStateReader{
		Probes: []Probe{
			&stubProbe{name: "process-liveness", result: &ProbeResult{State: StateBusy, Confidence: 0.1, Source: "process-liveness"}},
			&stubProbe{name: "child-tree", result: nil},
			&stubProbe{name: "jsonl", result: &ProbeResult{State: StateBusy, Confidence: 0.8, Source: "jsonl"}},
			&stubProbe{name: "cpu", result: nil}, // low CPU
		},
		PID: 123,
	}
	state, err := csr.ReadState("")
	if err != nil {
		t.Fatal(err)
	}
	if state != StateBusy {
		t.Errorf("got %v, want Busy", state)
	}
}

func TestCompositeStateReader_MtimeFallback(t *testing.T) {
	csr := &CompositeStateReader{
		Probes: []Probe{
			&stubProbe{name: "process-liveness", result: &ProbeResult{State: StateBusy, Confidence: 0.1, Source: "process-liveness"}},
			&stubProbe{name: "child-tree", result: nil},
			&stubProbe{name: "jsonl", result: nil},                                                                   // abstain
			&stubProbe{name: "mtime", result: &ProbeResult{State: StateReady, Confidence: 0.6, Source: "mtime"}},
		},
		PID: 123,
	}
	state, err := csr.ReadState("")
	if err != nil {
		t.Fatal(err)
	}
	if state != StateReady {
		t.Errorf("got %v, want Ready (mtime fallback)", state)
	}
}

func TestCompositeStateReader_AllAbstain(t *testing.T) {
	csr := &CompositeStateReader{
		Probes: []Probe{
			&stubProbe{name: "process-liveness", result: &ProbeResult{State: StateBusy, Confidence: 0.1, Source: "process-liveness"}},
			&stubProbe{name: "child-tree", result: nil},
			&stubProbe{name: "jsonl", result: nil},
			&stubProbe{name: "cpu", result: nil},
		},
		PID: 123,
	}
	state, err := csr.ReadState("")
	if err != nil {
		t.Fatal(err)
	}
	if state != StateUnknown {
		t.Errorf("got %v, want Unknown (all abstain)", state)
	}
}

func TestCompositeStateReader_ProbeError(t *testing.T) {
	csr := &CompositeStateReader{
		Probes: []Probe{
			&stubProbe{name: "process-liveness", result: nil, err: errTest},
			&stubProbe{name: "jsonl", result: &ProbeResult{State: StateReady, Confidence: 0.8, Source: "jsonl"}},
		},
		PID: 123,
	}
	state, err := csr.ReadState("")
	if err != nil {
		t.Fatal(err)
	}
	if state != StateReady {
		t.Errorf("got %v, want Ready (error skipped)", state)
	}
}

func TestCompositeStateReader_NoProbes(t *testing.T) {
	csr := &CompositeStateReader{PID: 123}
	state, err := csr.ReadState("")
	if err != nil {
		t.Fatal(err)
	}
	if state != StateUnknown {
		t.Errorf("got %v, want Unknown", state)
	}
}

var errTest = fmt.Errorf("test error")
