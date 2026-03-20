package main

import (
	"bytes"
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

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	err := runUsage(cmd, w, now)
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
	w := stubWalker{}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	err := runUsage(cmd, w, now)
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if !strings.Contains(got, "Claude usage") {
		t.Errorf("empty output should show header, got: %s", got)
	}
}

func TestBuildUsageCmd(t *testing.T) {
	cmd := buildUsageCmd()
	if cmd.Use != "usage" {
		t.Errorf("Use = %q, want %q", cmd.Use, "usage")
	}
}

// Ensure claude.DirWalker interface is satisfied by stubWalker.
var _ claude.DirWalker = stubWalker{}
