package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/StephanSchmidt/human/errors"
)

// DirWalker abstracts walking JSONL files for testability.
type DirWalker interface {
	WalkJSONL(root string, fn func(line []byte) error) error
}

// OSDirWalker implements DirWalker using the real filesystem.
type OSDirWalker struct{}

func (OSDirWalker) WalkJSONL(root string, fn func(line []byte) error) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible dirs
		}
		if info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		f, err := os.Open(filepath.Clean(path))
		if err != nil {
			return nil // skip unreadable files
		}
		defer func() { _ = f.Close() }()
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
		for scanner.Scan() {
			if err := fn(scanner.Bytes()); err != nil {
				return err
			}
		}
		return nil
	})
}

// WindowStart returns the start of the current 5-hour usage window in UTC.
func WindowStart(now time.Time) time.Time {
	utc := now.UTC()
	block := utc.Hour() / 5
	return time.Date(utc.Year(), utc.Month(), utc.Day(), block*5, 0, 0, 0, time.UTC)
}

// WindowEnd returns the end of the current 5-hour usage window in UTC.
func WindowEnd(start time.Time) time.Time {
	return start.Add(5 * time.Hour)
}

// jsonlLine is the minimal structure we need from each JSONL line.
type jsonlLine struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Message   struct {
		Model string `json:"model"`
		Usage *struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

func classifyModel(model string) string {
	m := strings.ToLower(model)

	// Determine the family name.
	var family string
	switch {
	case strings.Contains(m, "opus"):
		family = "opus"
	case strings.Contains(m, "haiku"):
		family = "haiku"
	default:
		family = "sonnet"
	}

	// Extract version from patterns like "claude-opus-4-6" or "claude-sonnet-4-5-20250929".
	// After the family name there should be "-major-minor" digits.
	idx := strings.Index(m, family)
	if idx < 0 {
		return family
	}
	rest := m[idx+len(family):]
	// rest should start with "-<major>-<minor>..." e.g. "-4-6" or "-4-5-20250929"
	parts := strings.Split(strings.TrimPrefix(rest, "-"), "-")
	if len(parts) >= 2 && isVersionDigit(parts[0]) && isVersionDigit(parts[1]) {
		return family + " " + parts[0] + "." + parts[1]
	}

	return family
}

// isVersionDigit returns true for short numeric strings (1-2 digits)
// that represent version numbers, not date stamps like "20250514".
func isVersionDigit(s string) bool {
	if len(s) == 0 || len(s) > 2 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// ModelUsage holds aggregated token counts for one model class.
type ModelUsage struct {
	InputTokens  int
	OutputTokens int
	CacheCreate  int
	CacheRead    int
}

// UsageSummary holds the full usage breakdown for the current window.
type UsageSummary struct {
	Models map[string]*ModelUsage
}

// CalculateUsage scans JSONL files under root and returns usage broken down by model.
func CalculateUsage(walker DirWalker, root string, now time.Time) (*UsageSummary, error) {
	winStart := WindowStart(now)
	winEnd := WindowEnd(winStart)
	summary := &UsageSummary{Models: make(map[string]*ModelUsage)}

	err := walker.WalkJSONL(root, func(line []byte) error {
		var entry jsonlLine
		if err := json.Unmarshal(line, &entry); err != nil {
			return nil // skip malformed lines
		}
		if entry.Type != "assistant" || entry.Message.Usage == nil {
			return nil
		}
		if entry.Timestamp.Before(winStart) || !entry.Timestamp.Before(winEnd) {
			return nil
		}

		model := classifyModel(entry.Message.Model)
		u := entry.Message.Usage

		mu := summary.Models[model]
		if mu == nil {
			mu = &ModelUsage{}
			summary.Models[model] = mu
		}

		mu.InputTokens += u.InputTokens
		mu.OutputTokens += u.OutputTokens
		mu.CacheCreate += u.CacheCreationInputTokens
		mu.CacheRead += u.CacheReadInputTokens
		return nil
	})
	if err != nil {
		return nil, errors.WrapWithDetails(err, "scanning JSONL files", "root", root)
	}
	return summary, nil
}

func formatTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func totalTokens(mu *ModelUsage) int {
	return mu.InputTokens + mu.OutputTokens + mu.CacheCreate + mu.CacheRead
}

// FormatUsage writes the usage summary to w.
func FormatUsage(w io.Writer, summary *UsageSummary, now time.Time) error {
	ws := WindowStart(now)
	we := WindowEnd(ws)

	_, err := fmt.Fprintf(w, "Claude usage [%02d:00 – %02d:00 UTC]\n", ws.Hour(), we.Hour())
	if err != nil {
		return err
	}

	// Compute grand total for percentage.
	var grandTotal int
	for _, mu := range summary.Models {
		if mu != nil {
			grandTotal += totalTokens(mu)
		}
	}

	// Sort model names for stable output.
	models := make([]string, 0, len(summary.Models))
	for m := range summary.Models {
		models = append(models, m)
	}
	sort.Strings(models)

	for _, model := range models {
		mu, ok := summary.Models[model]
		if !ok || mu == nil {
			continue
		}
		pct := 0.0
		if grandTotal > 0 {
			pct = float64(totalTokens(mu)) / float64(grandTotal) * 100
		}
		_, err := fmt.Fprintf(w, "  %-12s  %4.0f%%  in: %s  out: %s  cache: %s/%s\n",
			model, pct, formatTokens(mu.InputTokens), formatTokens(mu.OutputTokens),
			formatTokens(mu.CacheCreate), formatTokens(mu.CacheRead))
		if err != nil {
			return err
		}
	}
	return nil
}

// InstanceUsage pairs an Instance with its calculated usage.
type InstanceUsage struct {
	Instance Instance
	Summary  *UsageSummary
	State    InstanceState
}

// MergeUsage adds all model usage from src into dst.
func MergeUsage(dst, src *UsageSummary) {
	for model, srcMU := range src.Models {
		if srcMU == nil {
			continue
		}
		dstMU := dst.Models[model]
		if dstMU == nil {
			dstMU = &ModelUsage{}
			dst.Models[model] = dstMU
		}
		dstMU.InputTokens += srcMU.InputTokens
		dstMU.OutputTokens += srcMU.OutputTokens
		dstMU.CacheCreate += srcMU.CacheCreate
		dstMU.CacheRead += srcMU.CacheRead
	}
}

func formatModelRows(w io.Writer, summary *UsageSummary, grandTotal int) error {
	models := make([]string, 0, len(summary.Models))
	for m := range summary.Models {
		models = append(models, m)
	}
	sort.Strings(models)

	for _, model := range models {
		mu, ok := summary.Models[model]
		if !ok || mu == nil {
			continue
		}
		pct := 0.0
		if grandTotal > 0 {
			pct = float64(totalTokens(mu)) / float64(grandTotal) * 100
		}
		_, err := fmt.Fprintf(w, "  %-12s  %4.0f%%  in: %s  out: %s  cache: %s/%s\n",
			model, pct, formatTokens(mu.InputTokens), formatTokens(mu.OutputTokens),
			formatTokens(mu.CacheCreate), formatTokens(mu.CacheRead))
		if err != nil {
			return err
		}
	}
	return nil
}

// FormatMultiUsage writes per-instance and aggregated total usage.
func FormatMultiUsage(w io.Writer, instances []InstanceUsage, now time.Time) error {
	ws := WindowStart(now)
	we := WindowEnd(ws)

	if _, err := fmt.Fprintf(w, "Claude usage [%02d:00 – %02d:00 UTC]\n", ws.Hour(), we.Hour()); err != nil {
		return err
	}

	// Compute grand total across all instances for percentages.
	total := &UsageSummary{Models: make(map[string]*ModelUsage)}
	for _, iu := range instances {
		MergeUsage(total, iu.Summary)
	}
	var grandTotal int
	for _, mu := range total.Models {
		if mu != nil {
			grandTotal += totalTokens(mu)
		}
	}

	// Print each instance with per-instance percentages.
	for _, iu := range instances {
		if _, err := fmt.Fprintf(w, "\n%s %s\n", iu.Instance.Label, iu.State); err != nil {
			return err
		}
		var instanceTotal int
		for _, mu := range iu.Summary.Models {
			if mu != nil {
				instanceTotal += totalTokens(mu)
			}
		}
		if err := formatModelRows(w, iu.Summary, instanceTotal); err != nil {
			return err
		}
	}

	// Print aggregated total.
	if _, err := fmt.Fprintf(w, "\nTotal:\n"); err != nil {
		return err
	}
	return formatModelRows(w, total, grandTotal)
}
