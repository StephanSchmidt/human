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

var errTest = fmt.Errorf("test error")

func TestCompositeStateReader_DefaultReady(t *testing.T) {
	// No probes → default to Ready.
	csr := &CompositeStateReader{PID: 123}
	state, err := csr.ReadState("")
	if err != nil {
		t.Fatal(err)
	}
	if state != StateReady {
		t.Errorf("got %v, want Ready (default)", state)
	}
}

func TestCompositeStateReader_AllAbstain(t *testing.T) {
	csr := &CompositeStateReader{
		Probes: []Probe{
			&stubProbe{name: "child-tree", result: nil},
			&stubProbe{name: "cpu", result: nil},
		},
		PID: 123,
	}
	state, err := csr.ReadState("")
	if err != nil {
		t.Fatal(err)
	}
	if state != StateReady {
		t.Errorf("got %v, want Ready (all abstain)", state)
	}
}

func TestCompositeStateReader_JSONLBusy(t *testing.T) {
	csr := &CompositeStateReader{
		Probes: []Probe{
			&stubProbe{name: "jsonl", result: &ProbeResult{State: StateBusy, Source: "jsonl"}},
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

func TestCompositeStateReader_JSONLAbstains(t *testing.T) {
	// JSONL probe abstains (no busy evidence) → default Ready.
	csr := &CompositeStateReader{
		Probes: []Probe{
			&stubProbe{name: "jsonl", result: nil},
		},
		PID: 123,
	}
	state, err := csr.ReadState("")
	if err != nil {
		t.Fatal(err)
	}
	if state != StateReady {
		t.Errorf("got %v, want Ready (default when JSONL abstains)", state)
	}
}

func TestCompositeStateReader_AnyBusyWins(t *testing.T) {
	// JSONL abstains, but ChildTree says Busy → Busy wins.
	csr := &CompositeStateReader{
		Probes: []Probe{
			&stubProbe{name: "jsonl", result: nil},
			&stubProbe{name: "child-tree", result: &ProbeResult{State: StateBusy, Source: "child-tree"}},
		},
		PID: 123,
	}
	state, err := csr.ReadState("")
	if err != nil {
		t.Fatal(err)
	}
	if state != StateBusy {
		t.Errorf("got %v, want Busy (child-tree overrides)", state)
	}
}

func TestCompositeStateReader_LivenessDeadShortCircuits(t *testing.T) {
	csr := &CompositeStateReader{
		Probes: []Probe{
			&stubProbe{name: "process-liveness", result: &ProbeResult{State: StateUnknown, Source: "process-liveness"}},
			&stubProbe{name: "jsonl", result: nil},
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

func TestCompositeStateReader_LivenessAliveAbstains(t *testing.T) {
	// Liveness alive → nil (abstain), JSONL abstains → default Ready.
	csr := &CompositeStateReader{
		Probes: []Probe{
			&stubProbe{name: "process-liveness", result: nil}, // alive = abstain
			&stubProbe{name: "jsonl", result: nil},            // no busy evidence = abstain
		},
		PID: 123,
	}
	state, err := csr.ReadState("")
	if err != nil {
		t.Fatal(err)
	}
	if state != StateReady {
		t.Errorf("got %v, want Ready", state)
	}
}

func TestCompositeStateReader_ProbeErrorSkipped(t *testing.T) {
	csr := &CompositeStateReader{
		Probes: []Probe{
			&stubProbe{name: "process-liveness", result: nil, err: errTest},
			&stubProbe{name: "jsonl", result: nil},
		},
		PID: 123,
	}
	state, err := csr.ReadState("")
	if err != nil {
		t.Fatal(err)
	}
	if state != StateReady {
		t.Errorf("got %v, want Ready (error skipped, all abstain)", state)
	}
}

func TestCompositeStateReader_CPUBusyWins(t *testing.T) {
	csr := &CompositeStateReader{
		Probes: []Probe{
			&stubProbe{name: "jsonl", result: nil}, // abstains
			&stubProbe{name: "cpu", result: &ProbeResult{State: StateBusy, Source: "cpu"}},
		},
		PID: 123,
	}
	state, err := csr.ReadState("")
	if err != nil {
		t.Fatal(err)
	}
	if state != StateBusy {
		t.Errorf("got %v, want Busy (CPU override)", state)
	}
}
