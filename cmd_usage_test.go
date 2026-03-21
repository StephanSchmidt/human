package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/internal/claude"
)

type stubWalker struct {
	lines [][]byte
}

func (s stubWalker) WalkJSONL(_ string, fn func(line []byte) error) error {
	for _, l := range s.lines {
		if err := fn(l); err != nil {
			return err
		}
	}
	return nil
}

// stubFinder returns a fixed set of instances for testing.
type stubFinder struct {
	instances []claude.Instance
	err       error
}

func (s *stubFinder) FindInstances(_ context.Context) ([]claude.Instance, error) {
	return s.instances, s.err
}

func makeTestLine(t *testing.T, model string, ts time.Time, input, output int) []byte {
	t.Helper()
	m := map[string]interface{}{
		"type":      "assistant",
		"timestamp": ts.Format(time.RFC3339),
		"message": map[string]interface{}{
			"model": model,
			"usage": map[string]int{
				"input_tokens":                input,
				"output_tokens":               output,
				"cache_creation_input_tokens": 0,
				"cache_read_input_tokens":     0,
			},
		},
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRunUsage(t *testing.T) {
	now := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	inWindow := time.Date(2026, 3, 20, 11, 0, 0, 0, time.UTC)

	w := stubWalker{
		lines: [][]byte{
			makeTestLine(t, "claude-sonnet-4-20250514", inWindow, 1_000_000, 0),
			makeTestLine(t, "claude-opus-4-20250514", inWindow, 0, 500_000),
		},
	}

	finder := &stubFinder{
		instances: []claude.Instance{
			{Label: "Host (PID 123)", Source: "host", Walker: w, Root: "/fake"},
		},
	}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	cmd.SetContext(context.Background())

	err := runUsage(cmd, finder, now)
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if !strings.Contains(got, "sonnet") {
		t.Errorf("output should contain sonnet, got: %s", got)
	}
	if !strings.Contains(got, "opus") {
		t.Errorf("output should contain opus, got: %s", got)
	}
	if !strings.Contains(got, "1.0M") {
		t.Errorf("output should contain formatted tokens, got: %s", got)
	}
}

func TestRunUsageEmpty(t *testing.T) {
	now := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)

	finder := &stubFinder{
		instances: []claude.Instance{
			{Label: "Host (PID 123)", Source: "host", Walker: stubWalker{}, Root: "/fake"},
		},
	}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	cmd.SetContext(context.Background())

	err := runUsage(cmd, finder, now)
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if !strings.Contains(got, "Claude usage") {
		t.Errorf("empty output should show header, got: %s", got)
	}
}

func TestRunUsageFallback(t *testing.T) {
	// When finder returns no instances, runUsage falls back to local filesystem.
	// We can't test the actual filesystem, but we verify it doesn't panic.
	now := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)

	finder := &stubFinder{instances: nil}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	cmd.SetContext(context.Background())

	// This will attempt to read from ~/.claude/projects — may or may not have data.
	err := runUsage(cmd, finder, now)
	if err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "Claude usage") {
		t.Errorf("fallback should show header, got: %s", got)
	}
}

func TestRunUsageMultiInstance(t *testing.T) {
	now := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	inWindow := time.Date(2026, 3, 20, 11, 0, 0, 0, time.UTC)

	hostWalker := stubWalker{
		lines: [][]byte{
			makeTestLine(t, "claude-sonnet-4-5-20250929", inWindow, 1_000_000, 500_000),
		},
	}
	containerWalker := stubWalker{
		lines: [][]byte{
			makeTestLine(t, "claude-opus-4-6", inWindow, 500_000, 200_000),
		},
	}

	finder := &stubFinder{
		instances: []claude.Instance{
			{Label: "Host (PID 12345)", Source: "host", Walker: hostWalker, StateReader: stubStateReader{state: claude.StateBusy}, Root: "/fake/host"},
			{Label: `Container "dev-myapp" (abc123)`, Source: "container", Walker: containerWalker, StateReader: stubStateReader{state: claude.StateReady}, Root: "/fake/container"},
		},
	}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	cmd.SetContext(context.Background())

	err := runUsage(cmd, finder, now)
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if !strings.Contains(got, "Host (PID 12345) 🔴") {
		t.Errorf("should contain host label with state, got: %s", got)
	}
	if !strings.Contains(got, `Container "dev-myapp" (abc123) 🟢`) {
		t.Errorf("should contain container label with state, got: %s", got)
	}
	if !strings.Contains(got, "Total:") {
		t.Errorf("should contain Total section, got: %s", got)
	}
	if !strings.Contains(got, "sonnet 4.5") {
		t.Errorf("should contain sonnet 4.5, got: %s", got)
	}
	if !strings.Contains(got, "opus 4.6") {
		t.Errorf("should contain opus 4.6, got: %s", got)
	}
}

func TestBuildUsageCmd(t *testing.T) {
	cmd := buildUsageCmd()
	if cmd.Use != "usage" {
		t.Errorf("Use = %q, want %q", cmd.Use, "usage")
	}
}

// stubStateReader returns a fixed state for testing.
type stubStateReader struct {
	state claude.InstanceState
}

func (s stubStateReader) ReadState(_ string) (claude.InstanceState, error) {
	return s.state, nil
}

// Ensure claude.DirWalker interface is satisfied by stubWalker.
var _ claude.DirWalker = stubWalker{}

// Ensure claude.InstanceFinder interface is satisfied by stubFinder.
var _ claude.InstanceFinder = &stubFinder{}

// Ensure claude.StateReader interface is satisfied by stubStateReader.
var _ claude.StateReader = stubStateReader{}
