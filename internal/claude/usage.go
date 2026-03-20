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
	switch {
	case strings.Contains(m, "opus"):
		return "opus"
	case strings.Contains(m, "haiku"):
		return "haiku"
	default:
		return "sonnet"
	}
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
		_, err := fmt.Fprintf(w, "  %-7s  %4.0f%%  in: %s  out: %s  cache: %s/%s\n",
			model, pct, formatTokens(mu.InputTokens), formatTokens(mu.OutputTokens),
			formatTokens(mu.CacheCreate), formatTokens(mu.CacheRead))
		if err != nil {
			return err
		}
	}
	return nil
}
