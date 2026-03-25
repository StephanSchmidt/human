package cmdtui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/StephanSchmidt/human/internal/claude"
	"github.com/StephanSchmidt/human/internal/claude/logparser"
	"github.com/StephanSchmidt/human/internal/claude/monitor"
)

// stubFinder returns canned instances.
type stubFinder struct {
	instances []claude.Instance
	err       error
}

func (s *stubFinder) FindInstances(_ context.Context) ([]claude.Instance, error) {
	return s.instances, s.err
}

func testModel() model {
	return newModel(monitor.New(&stubFinder{}, nil))
}

func testSnapshot(opts ...func(*monitor.Snapshot)) *monitor.Snapshot {
	snap := &monitor.Snapshot{
		FetchedAt:  time.Now(),
		TotalUsage: &claude.UsageSummary{Models: map[string]*claude.ModelUsage{}},
	}
	for _, opt := range opts {
		opt(snap)
	}
	return snap
}

func TestModelInit(t *testing.T) {
	m := testModel()
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init() returned nil Cmd")
	}
}

func TestModelUpdate_Quit(t *testing.T) {
	m := testModel()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	um := updated.(model)
	if !um.quitting {
		t.Error("expected quitting to be true")
	}
	if cmd == nil {
		t.Error("expected non-nil quit command")
	}
}

func TestModelUpdate_CtrlC(t *testing.T) {
	m := testModel()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	um := updated.(model)
	if !um.quitting {
		t.Error("expected quitting to be true")
	}
	if cmd == nil {
		t.Error("expected non-nil quit command")
	}
}

func TestModelUpdate_WindowSize(t *testing.T) {
	m := testModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	um := updated.(model)
	if um.width != 120 || um.height != 40 {
		t.Errorf("expected 120x40, got %dx%d", um.width, um.height)
	}
}

func TestModelUpdate_SnapshotMsg(t *testing.T) {
	m := testModel()
	snap := testSnapshot(func(s *monitor.Snapshot) {
		s.Daemon = monitor.DaemonState{PID: 1234, Alive: true}
	})
	updated, _ := m.Update(snapshotMsg{snap: snap})
	um := updated.(model)
	if um.snap == nil {
		t.Fatal("expected snapshot to be set")
	}
	if um.snap.Daemon.PID != 1234 {
		t.Errorf("expected PID 1234, got %d", um.snap.Daemon.PID)
	}
}

func TestModelView_Loading(t *testing.T) {
	m := testModel()
	view := m.View()
	if !strings.Contains(view, "Loading") {
		t.Errorf("expected 'Loading' in view, got:\n%s", view)
	}
}

func TestModelView_Error(t *testing.T) {
	m := testModel()
	m.snap = testSnapshot(func(s *monitor.Snapshot) {
		s.Err = context.DeadlineExceeded
	})
	view := m.View()
	if !strings.Contains(view, "Error") {
		t.Errorf("expected 'Error' in view, got:\n%s", view)
	}
}

func TestModelView_WithData(t *testing.T) {
	m := testModel()
	m.snap = testSnapshot(func(s *monitor.Snapshot) {
		s.Daemon = monitor.DaemonState{PID: 42, Alive: true}
		s.Instances = []monitor.InstanceView{
			{
				Usage: claude.InstanceUsage{
					Instance: claude.Instance{Label: "Host (PID 100)", Source: "host"},
					Summary: &claude.UsageSummary{
						Models: map[string]*claude.ModelUsage{
							"opus 4.6": {InputTokens: 1000, OutputTokens: 500},
						},
					},
					State: claude.StateUnknown,
				},
			},
		}
		s.TotalUsage = &claude.UsageSummary{
			Models: map[string]*claude.ModelUsage{
				"opus 4.6": {InputTokens: 1000, OutputTokens: 500},
			},
		}
	})
	view := m.View()
	if !strings.Contains(view, "opus") {
		t.Errorf("expected 'opus' in view, got:\n%s", view)
	}
	if !strings.Contains(view, "Claude usage") {
		t.Errorf("expected 'Claude usage' in view, got:\n%s", view)
	}
}

func TestModelView_DaemonRunning(t *testing.T) {
	m := testModel()
	m.snap = testSnapshot(func(s *monitor.Snapshot) {
		s.Daemon = monitor.DaemonState{PID: 42, Alive: true}
	})
	view := m.View()
	if !strings.Contains(view, "running") {
		t.Errorf("expected 'running' in view, got:\n%s", view)
	}
}

func TestModelView_DaemonStopped(t *testing.T) {
	m := testModel()
	m.snap = testSnapshot()
	view := m.View()
	if !strings.Contains(view, "stopped") {
		t.Errorf("expected 'stopped' in view, got:\n%s", view)
	}
}

func TestRenderDaemonStatus(t *testing.T) {
	running := renderDaemonStatus(1234, true)
	if !strings.Contains(running, "1234") || !strings.Contains(running, "running") {
		t.Errorf("expected PID and 'running', got: %s", running)
	}

	stopped := renderDaemonStatus(0, false)
	if !strings.Contains(stopped, "stopped") {
		t.Errorf("expected 'stopped', got: %s", stopped)
	}
}

func TestRenderHeader_ContainsHostname(t *testing.T) {
	header := renderHeader()
	if !strings.Contains(header, "Claude Code Dashboard") {
		t.Errorf("expected 'Claude Code Dashboard' in header, got: %s", header)
	}
	if !strings.Contains(header, "(") {
		t.Errorf("expected hostname in parentheses in header, got: %s", header)
	}
}

func TestModelView_Quitting(t *testing.T) {
	m := testModel()
	m.quitting = true
	view := m.View()
	if view != "" {
		t.Errorf("expected empty view when quitting, got: %s", view)
	}
}

// --- render helper tests ---

func TestSessionIcon(t *testing.T) {
	tests := []struct {
		name string
		sess *logparser.SessionState
		want string
	}{
		{"nil session", nil, "⚪"},
		{"working", &logparser.SessionState{IsWorking: true}, "🔴"},
		{"idle with activity", &logparser.SessionState{IsWorking: false, LastActivity: time.Now()}, "🟢"},
		{"no activity", &logparser.SessionState{IsWorking: false}, "⚪"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sessionIcon(tt.sess); got != tt.want {
				t.Errorf("sessionIcon() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatElapsed(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Second, "5s"},
		{90 * time.Second, "1m 30s"},
		{3661 * time.Second, "1h 1m"},
		{0, "0s"},
	}
	for _, tt := range tests {
		if got := formatElapsed(tt.d); got != tt.want {
			t.Errorf("formatElapsed(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		s      string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is too long", 10, "this is..."},
	}
	for _, tt := range tests {
		if got := truncate(tt.s, tt.maxLen); got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
		}
	}
}

func TestRenderTaskSummary(t *testing.T) {
	var b strings.Builder
	renderTaskSummary(&b, []logparser.Task{
		{Status: "pending"},
		{Status: "in_progress"},
		{Status: "completed"},
		{Status: "completed"},
	})
	out := b.String()
	if !strings.Contains(out, "1 pending") {
		t.Errorf("expected '1 pending', got: %s", out)
	}
	if !strings.Contains(out, "1 in progress") {
		t.Errorf("expected '1 in progress', got: %s", out)
	}
	if !strings.Contains(out, "2 completed") {
		t.Errorf("expected '2 completed', got: %s", out)
	}
}

func TestRenderTaskSummary_Empty(t *testing.T) {
	var b strings.Builder
	renderTaskSummary(&b, nil)
	if b.Len() != 0 {
		t.Errorf("expected empty output for nil tasks, got: %s", b.String())
	}
}
