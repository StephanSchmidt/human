package claude

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// fakeWalker replays pre-built JSONL lines.
type fakeWalker struct {
	lines [][]byte
}

func (f fakeWalker) WalkJSONL(_ string, fn func(line []byte) error) error {
	for _, l := range f.lines {
		if err := fn(l); err != nil {
			return err
		}
	}
	return nil
}

func makeLine(t *testing.T, typ, model string, ts time.Time, input, output, cacheCreate, cacheRead int) []byte {
	t.Helper()
	m := map[string]interface{}{
		"type":      typ,
		"timestamp": ts.Format(time.RFC3339),
		"message": map[string]interface{}{
			"model": model,
			"usage": map[string]int{
				"input_tokens":                input,
				"output_tokens":               output,
				"cache_creation_input_tokens": cacheCreate,
				"cache_read_input_tokens":     cacheRead,
			},
		},
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestWindowStart(t *testing.T) {
	tests := []struct {
		hour     int
		wantHour int
	}{
		{0, 0}, {3, 0}, {4, 0},
		{5, 5}, {7, 5}, {9, 5},
		{10, 10}, {14, 10},
		{15, 15}, {19, 15},
		{20, 20}, {23, 20},
	}
	for _, tt := range tests {
		now := time.Date(2026, 3, 20, tt.hour, 30, 0, 0, time.UTC)
		got := WindowStart(now)
		if got.Hour() != tt.wantHour {
			t.Errorf("WindowStart(hour=%d) = %d, want %d", tt.hour, got.Hour(), tt.wantHour)
		}
	}
}

func TestCalculateUsage(t *testing.T) {
	now := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	inWindow := time.Date(2026, 3, 20, 11, 0, 0, 0, time.UTC)
	outOfWindow := time.Date(2026, 3, 20, 9, 0, 0, 0, time.UTC)

	lines := [][]byte{
		makeLine(t, "assistant", "claude-sonnet-4-5-20250929", inWindow, 1_000_000, 0, 0, 0),
		makeLine(t, "assistant", "claude-opus-4-6", inWindow, 0, 1_000_000, 0, 0),
		// Out of window — should be ignored
		makeLine(t, "assistant", "claude-sonnet-4-5-20250929", outOfWindow, 1_000_000, 0, 0, 0),
		// Wrong type — should be ignored
		makeLine(t, "human", "claude-sonnet-4-5-20250929", inWindow, 1_000_000, 0, 0, 0),
		// Malformed line — should be skipped
		[]byte(`{invalid json`),
	}

	w := fakeWalker{lines: lines}
	summary, err := CalculateUsage(w, "/fake", now)
	if err != nil {
		t.Fatal(err)
	}

	sonnet := summary.Models["sonnet 4.5"]
	if sonnet == nil {
		t.Fatal("expected sonnet 4.5 model entry")
	}
	if sonnet.InputTokens != 1_000_000 {
		t.Errorf("sonnet input = %d, want 1000000", sonnet.InputTokens)
	}
	opus := summary.Models["opus 4.6"]
	if opus == nil {
		t.Fatal("expected opus 4.6 model entry")
	}
	if opus.OutputTokens != 1_000_000 {
		t.Errorf("opus output = %d, want 1000000", opus.OutputTokens)
	}
}

func TestCalculateUsageCacheTokens(t *testing.T) {
	now := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	inWindow := time.Date(2026, 3, 20, 11, 0, 0, 0, time.UTC)

	lines := [][]byte{
		makeLine(t, "assistant", "claude-sonnet-4-5-20250929", inWindow, 0, 0, 1_000_000, 1_000_000),
	}

	w := fakeWalker{lines: lines}
	summary, err := CalculateUsage(w, "/fake", now)
	if err != nil {
		t.Fatal(err)
	}
	sonnet := summary.Models["sonnet 4.5"]
	if sonnet == nil {
		t.Fatal("expected sonnet 4.5 model entry")
	}
	if sonnet.CacheCreate != 1_000_000 {
		t.Errorf("sonnet cache_create = %d, want 1000000", sonnet.CacheCreate)
	}
	if sonnet.CacheRead != 1_000_000 {
		t.Errorf("sonnet cache_read = %d, want 1000000", sonnet.CacheRead)
	}
}

func TestFormatUsage(t *testing.T) {
	now := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	summary := &UsageSummary{
		Models: map[string]*ModelUsage{
			"sonnet 4.5": {InputTokens: 1_000_000, OutputTokens: 500_000},
			"opus 4.6":   {OutputTokens: 1_000_000},
		},
	}
	var buf bytes.Buffer
	err := FormatUsage(&buf, summary, now)
	if err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "opus 4.6") {
		t.Errorf("should contain opus 4.6, got: %s", got)
	}
	if !strings.Contains(got, "sonnet 4.5") {
		t.Errorf("should contain sonnet 4.5, got: %s", got)
	}
	if !strings.Contains(got, "10:00") {
		t.Errorf("should contain window start, got: %s", got)
	}
	if !strings.Contains(got, "1.0M") {
		t.Errorf("should contain formatted tokens, got: %s", got)
	}
}

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{500, "500"},
		{1_500, "1.5K"},
		{1_500_000, "1.5M"},
		{0, "0"},
	}
	for _, tt := range tests {
		got := formatTokens(tt.n)
		if got != tt.want {
			t.Errorf("formatTokens(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestClassifyModel(t *testing.T) {
	tests := []struct {
		model string
		want  string
	}{
		{"claude-opus-4-6", "opus 4.6"},
		{"claude-opus-4-5-20251101", "opus 4.5"},
		{"claude-opus-4-20250514", "opus"},
		{"claude-sonnet-4-6", "sonnet 4.6"},
		{"claude-sonnet-4-5-20250929", "sonnet 4.5"},
		{"claude-sonnet-4-20250514", "sonnet"},
		{"claude-haiku-4-5-20251001", "haiku 4.5"},
		{"claude-haiku-3-5-20241022", "haiku 3.5"},
		{"sonnet", "sonnet"},
		{"haiku", "haiku"},
		{"some-unknown-model", "sonnet"},
	}
	for _, tt := range tests {
		got := classifyModel(tt.model)
		if got != tt.want {
			t.Errorf("classifyModel(%q) = %q, want %q", tt.model, got, tt.want)
		}
	}
}

func TestFormatUsageEmpty(t *testing.T) {
	now := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	summary := &UsageSummary{Models: map[string]*ModelUsage{}}
	var buf bytes.Buffer
	err := FormatUsage(&buf, summary, now)
	if err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "Claude usage") {
		t.Errorf("empty summary should show header, got: %s", got)
	}
}
