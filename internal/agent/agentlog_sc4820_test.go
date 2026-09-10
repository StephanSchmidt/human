package agent

// SC-4820 regression tests: a run whose process exits non-zero must keep its
// exit code and a failed reason regardless of which writer — the tee (at
// stream EOF) or teardown (PreserveExecutionArtifacts) — runs first. Before
// the fix, HasOutcome let whichever writer ran second discard the other's
// half of the record, so an exit-1 run reaped by teardown was filed as
// {"reason":"reaped","exit_code":0} and an exit-1 run torn down by the
// ordinary exit hook was filed as {"reason":"completed"}.

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gethuman-sh/human/internal/devcontainer"
)

// exit1TeeMock reports the exec as having exited 1 so the tee holds a real,
// non-zero exit code the teardown path must not be able to erase.
type exit1TeeMock struct{ teeMock }

func (m *exit1TeeMock) ExecInspect(_ context.Context, _ string) (devcontainer.ExecInspectResponse, error) {
	return devcontainer.ExecInspectResponse{ExitCode: 1, Running: false}, nil
}

func TestSC4820_TeeThenReap_KeepsExitCodeAndFailedReason(t *testing.T) {
	withLogRoot(t)
	exe, err := NewExecution(LaunchRecord{ID: newExecID(), Agent: "sc4820", Prompt: "p", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("NewExecution: %v", err)
	}

	attach := devcontainer.ExecAttachResponse{Reader: bytesReader(stdoutFrame("working")), Conn: nopCloser()}
	teeExecOutput(attach, exe, &exit1TeeMock{}, "exec-id")

	PreserveExecutionArtifacts(context.Background(), &exit1TeeMock{}, Meta{
		Name: "sc4820", ContainerID: "cid", ExecutionID: exe.Launch.ID,
		CreatedAt: time.Now(), Status: StatusFailed,
	})

	var oc OutcomeRecord
	if err := readJSONFile(filepath.Join(exe.Dir(), "outcome.json"), &oc); err != nil {
		t.Fatalf("outcome.json must exist: %v", err)
	}
	if oc.ExitCode != 1 {
		t.Fatalf("exit_code = %d, want 1 (tee-then-reap must not lose the exit code)", oc.ExitCode)
	}
	if oc.Reason != "failed" {
		t.Fatalf("reason = %q, want failed", oc.Reason)
	}
	if oc.Disposition != DispositionReaped {
		t.Fatalf("disposition = %q, want reaped", oc.Disposition)
	}
}

func TestSC4820_ReapThenTee_KeepsExitCodeAndFailedReason(t *testing.T) {
	withLogRoot(t)
	exe, err := NewExecution(LaunchRecord{ID: newExecID(), Agent: "sc4820", Prompt: "p", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("NewExecution: %v", err)
	}

	PreserveExecutionArtifacts(context.Background(), &exit1TeeMock{}, Meta{
		Name: "sc4820", ContainerID: "cid", ExecutionID: exe.Launch.ID,
		CreatedAt: time.Now(), Status: StatusFailed,
	})

	attach := devcontainer.ExecAttachResponse{Reader: bytesReader(stdoutFrame("working")), Conn: nopCloser()}
	teeExecOutput(attach, exe, &exit1TeeMock{}, "exec-id")

	var oc OutcomeRecord
	if err := readJSONFile(filepath.Join(exe.Dir(), "outcome.json"), &oc); err != nil {
		t.Fatalf("outcome.json must exist: %v", err)
	}
	if oc.ExitCode != 1 {
		t.Fatalf("exit_code = %d, want 1 (reap-then-tee must not lose the exit code — this is the ordering the HasOutcome guard suppresses today)", oc.ExitCode)
	}
	if oc.Reason != "failed" {
		t.Fatalf("reason = %q, want failed", oc.Reason)
	}
	if oc.Disposition != DispositionReaped {
		t.Fatalf("disposition = %q, want reaped", oc.Disposition)
	}
}

func TestSC4820_HookTeardown_DoesNotRecordAFailedRunAsCompleted(t *testing.T) {
	withLogRoot(t)
	exe, err := NewExecution(LaunchRecord{ID: newExecID(), Agent: "sc4820", Prompt: "p", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("NewExecution: %v", err)
	}

	attach := devcontainer.ExecAttachResponse{Reader: bytesReader(stdoutFrame("working")), Conn: nopCloser()}
	teeExecOutput(attach, exe, &exit1TeeMock{}, "exec-id")

	// The ordinary exit-hook teardown path: the container exited on its own
	// (StatusRunning at the choke point, not a zombie-sweep reap).
	PreserveExecutionArtifacts(context.Background(), &exit1TeeMock{}, Meta{
		Name: "sc4820", ContainerID: "cid", ExecutionID: exe.Launch.ID,
		CreatedAt: time.Now(), Status: StatusRunning,
	})

	var oc OutcomeRecord
	if err := readJSONFile(filepath.Join(exe.Dir(), "outcome.json"), &oc); err != nil {
		t.Fatalf("outcome.json must exist: %v", err)
	}
	if oc.Reason == "completed" {
		t.Fatalf("reason = %q, must not read as completed for an exit-1 run", oc.Reason)
	}
	if oc.Reason != "failed" {
		t.Fatalf("reason = %q, want failed", oc.Reason)
	}
	if oc.ExitCode != 1 {
		t.Fatalf("exit_code = %d, want 1", oc.ExitCode)
	}
	if oc.Disposition != DispositionStopped {
		t.Fatalf("disposition = %q, want stopped", oc.Disposition)
	}
}

func TestDeriveReason(t *testing.T) {
	cases := []struct {
		name string
		in   OutcomeRecord
		want string
	}{
		{"process completed wins", OutcomeRecord{Process: "completed"}, "completed"},
		{"process failed wins over disposition", OutcomeRecord{Process: "failed", Disposition: DispositionReaped}, "failed"},
		{"disposition reaped, no process", OutcomeRecord{Disposition: DispositionReaped}, "reaped"},
		{"disposition stopped, no process", OutcomeRecord{Disposition: DispositionStopped}, "completed"},
		{"nothing known", OutcomeRecord{}, "failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := deriveReason(c.in); got != c.want {
				t.Fatalf("deriveReason(%+v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestMergeOutcome_NeverWritesAZeroExitOverAKnownCode drives the two writers in
// sequence — process end, then two dispositions — and asserts the exit code and
// its known-ness, once established, are never disturbed by a later disposition
// write; and that the FIRST writer fixes EndedAt.
func TestMergeOutcome_NeverWritesAZeroExitOverAKnownCode(t *testing.T) {
	withLogRoot(t)
	exe, err := NewExecution(LaunchRecord{ID: newExecID(), Agent: "merge", Prompt: "p", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("NewExecution: %v", err)
	}
	firstEnd := time.Now()
	if err := exe.RecordProcessEnd(1, true, firstEnd, time.Second); err != nil {
		t.Fatalf("RecordProcessEnd: %v", err)
	}
	if err := exe.RecordDisposition(DispositionReaped, firstEnd.Add(time.Hour), time.Hour); err != nil {
		t.Fatalf("RecordDisposition(reaped): %v", err)
	}
	if err := exe.RecordDisposition(DispositionStopped, firstEnd.Add(2*time.Hour), 2*time.Hour); err != nil {
		t.Fatalf("RecordDisposition(stopped): %v", err)
	}

	var oc OutcomeRecord
	if err := readJSONFile(filepath.Join(exe.Dir(), "outcome.json"), &oc); err != nil {
		t.Fatalf("outcome.json must exist: %v", err)
	}
	if oc.ExitCode != 1 || !oc.ExitKnown {
		t.Fatalf("exit_code/exit_known = %d/%v, want 1/true", oc.ExitCode, oc.ExitKnown)
	}
	if !oc.EndedAt.Equal(firstEnd) {
		t.Fatalf("ended_at = %v, want the first writer's timestamp %v", oc.EndedAt, firstEnd)
	}
}
