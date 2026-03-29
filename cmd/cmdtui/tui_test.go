package cmdtui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"

	"github.com/StephanSchmidt/human/internal/claude"
	"github.com/StephanSchmidt/human/internal/claude/logparser"
	"github.com/StephanSchmidt/human/internal/claude/monitor"
	"github.com/StephanSchmidt/human/internal/tracker"
)

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
	updated, _ := m.Update(snapshotMsg{snap: snap, gen: m.fetchGen})
	um := updated.(model)
	if um.snap == nil {
		t.Fatal("expected snapshot to be set")
	}
	if um.snap.Daemon.PID != 1234 {
		t.Errorf("expected PID 1234, got %d", um.snap.Daemon.PID)
	}
	if um.fetching {
		t.Error("expected fetching to be false after applying snapshot")
	}
}

func TestModelUpdate_StaleSnapshot(t *testing.T) {
	m := testModel()
	m.snap = testSnapshot(func(s *monitor.Snapshot) {
		s.Daemon = monitor.DaemonState{PID: 42, Alive: true}
	})
	// Send a snapshot with a stale generation — must be discarded.
	staleSnap := testSnapshot(func(s *monitor.Snapshot) {
		s.Daemon = monitor.DaemonState{PID: 9999, Alive: false}
	})
	updated, _ := m.Update(snapshotMsg{snap: staleSnap, gen: 0})
	um := updated.(model)
	if um.snap.Daemon.PID != 42 {
		t.Errorf("stale snapshot should be discarded, PID is %d", um.snap.Daemon.PID)
	}
}

func TestModelUpdate_FastTickWhileFetching(t *testing.T) {
	m := testModel()
	// m.fetching is true from newModel — fastTick should be skipped.
	updated, cmd := m.Update(fastTickMsg(time.Now()))
	um := updated.(model)
	if !um.fetching {
		t.Error("fetching should remain true")
	}
	if um.fetchGen != 1 {
		t.Errorf("fetchGen should remain 1, got %d", um.fetchGen)
	}
	if cmd == nil {
		t.Error("expected reschedule tick command")
	}
}

func TestModelUpdate_FullTickWhileFetching(t *testing.T) {
	m := testModel()
	updated, cmd := m.Update(fullTickMsg(time.Now()))
	um := updated.(model)
	if !um.fetching {
		t.Error("fetching should remain true")
	}
	if um.fetchGen != 1 {
		t.Errorf("fetchGen should remain 1, got %d", um.fetchGen)
	}
	if cmd == nil {
		t.Error("expected reschedule tick command")
	}
}

func TestModelUpdate_FastTickDispatchesFetch(t *testing.T) {
	m := testModel()
	m.fetching = false // simulate idle
	updated, cmd := m.Update(fastTickMsg(time.Now()))
	um := updated.(model)
	if !um.fetching {
		t.Error("fetching should be true after dispatching")
	}
	if um.fetchGen != 2 {
		t.Errorf("fetchGen should be 2, got %d", um.fetchGen)
	}
	if cmd == nil {
		t.Error("expected fetch command")
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
	if !strings.Contains(view, "Host") {
		t.Errorf("expected 'Host' in view, got:\n%s", view)
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

func TestModelView_Quitting(t *testing.T) {
	m := testModel()
	m.quitting = true
	view := m.View()
	if view != "" {
		t.Errorf("expected empty view when quitting, got: %s", view)
	}
}

func TestRenderHeader_ContainsTitle(t *testing.T) {
	m := testModel()
	header := m.renderHeader(80)
	if !strings.Contains(header, "human tui") {
		t.Errorf("expected 'human tui' in header, got: %s", header)
	}
}

// --- render helper tests ---

func TestSessionIcon(t *testing.T) {
	m := testModel()
	tests := []struct {
		name     string
		sess     *logparser.SessionState
		contains string
	}{
		{"nil session", nil, "○"},
		{"idle with activity", &logparser.SessionState{Status: logparser.StatusReady, LastActivity: time.Now()}, "●"},
		{"no activity", &logparser.SessionState{Status: logparser.StatusReady}, "○"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.sessionIcon(tt.sess)
			if !strings.Contains(got, tt.contains) {
				t.Errorf("sessionIcon() = %q, want to contain %q", got, tt.contains)
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
	if !strings.Contains(out, "2 done") {
		t.Errorf("expected '2 done', got: %s", out)
	}
}

func TestRenderTaskSummary_Empty(t *testing.T) {
	var b strings.Builder
	renderTaskSummary(&b, nil)
	if b.Len() != 0 {
		t.Errorf("expected empty output for nil tasks, got: %s", b.String())
	}
}

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{500, "500"},
		{1500, "1.5K"},
		{1500000, "1.5M"},
	}
	for _, tt := range tests {
		if got := formatTokens(tt.n); got != tt.want {
			t.Errorf("formatTokens(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestRenderTrackers_onlyWorking(t *testing.T) {
	trackers := []tracker.TrackerStatus{
		{Name: "work", Kind: "linear", Label: "Linear", Working: true},
		{Name: "amazingcto", Kind: "jira", Label: "Jira", Working: false, Missing: []string{"JIRA_KEY"}},
	}
	out := renderTrackers(trackers, 80)
	if !strings.Contains(out, "Trackers") {
		t.Errorf("expected 'Trackers' label, got: %s", out)
	}
	if !strings.Contains(out, "Linear") {
		t.Errorf("expected 'Linear', got: %s", out)
	}
	if strings.Contains(out, "Jira") {
		t.Errorf("non-working tracker should be hidden, got: %s", out)
	}
}

func TestRenderTrackers_empty(t *testing.T) {
	out := renderTrackers(nil, 80)
	if out != "" {
		t.Errorf("expected empty output for nil trackers, got: %s", out)
	}
}

func TestRenderTrackers_countMultiple(t *testing.T) {
	trackers := []tracker.TrackerStatus{
		{Name: "acme", Kind: "jira", Label: "Jira", Working: true},
		{Name: "corp", Kind: "jira", Label: "Jira", Working: true},
		{Name: "work", Kind: "linear", Label: "Linear", Working: true},
	}
	out := renderTrackers(trackers, 80)
	if !strings.Contains(out, "Jira (2)") {
		t.Errorf("expected 'Jira (2)', got: %s", out)
	}
	if strings.Contains(out, "Linear (") {
		t.Errorf("single tracker should not have count, got: %s", out)
	}
}

func TestRenderTrackers_allMissing(t *testing.T) {
	trackers := []tracker.TrackerStatus{
		{Name: "broken", Kind: "github", Label: "GitHub", Working: false, Missing: []string{"GITHUB_TOKEN"}},
	}
	out := renderTrackers(trackers, 80)
	if out != "" {
		t.Errorf("expected empty when no working trackers, got: %s", out)
	}
}

func TestModelView_WithTrackers(t *testing.T) {
	m := testModel()
	m.snap = testSnapshot(func(s *monitor.Snapshot) {
		s.Trackers = []tracker.TrackerStatus{
			{Name: "work", Kind: "linear", Label: "Linear", Working: true},
		}
	})
	view := m.View()
	if !strings.Contains(view, "Linear") {
		t.Errorf("expected tracker kind label in view, got:\n%s", view)
	}
	if !strings.Contains(view, "Trackers") {
		t.Errorf("expected 'Trackers' label in view, got:\n%s", view)
	}
}

func TestRenderStatusLine(t *testing.T) {
	snap := testSnapshot(func(s *monitor.Snapshot) {
		s.Daemon = monitor.DaemonState{PID: 42, Alive: true}
		s.Telegram = "Telegram dispatch"
	})
	line := renderStatusLine(snap, 80)
	if !strings.Contains(line, "running") {
		t.Errorf("expected 'running' in status line, got: %s", line)
	}
	if !strings.Contains(line, "Telegram") {
		t.Errorf("expected 'Telegram' in status line, got: %s", line)
	}
}

func TestCycleLogMode(t *testing.T) {
	assert.Equal(t, "meta", cycleLogMode("full"))
	assert.Equal(t, "off", cycleLogMode("meta"))
	assert.Equal(t, "full", cycleLogMode("off"))
	assert.Equal(t, "full", cycleLogMode(""))      // unknown defaults to full
	assert.Equal(t, "full", cycleLogMode("bogus"))  // unknown defaults to full
}

func TestModelUpdate_LogModeKey(t *testing.T) {
	m := testModel()
	assert.Equal(t, "off", m.logMode)

	// off → full
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	um := updated.(model)
	assert.Equal(t, "full", um.logMode)
	assert.NotNil(t, cmd, "expected async command to set log mode on daemon")

	// full → meta
	updated, _ = um.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	um = updated.(model)
	assert.Equal(t, "meta", um.logMode)

	// meta → off
	updated, _ = um.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	um = updated.(model)
	assert.Equal(t, "off", um.logMode)
}

func TestModelUpdate_LogModeMsg(t *testing.T) {
	m := testModel()
	updated, _ := m.Update(logModeMsg("meta"))
	um := updated.(model)
	assert.Equal(t, "meta", um.logMode)
}

func TestRenderFooter_ShowsLogMode(t *testing.T) {
	footer := renderFooter(80, "meta")
	assert.Contains(t, footer, "log:meta")
	assert.Contains(t, footer, "l log")
	assert.Contains(t, footer, "q quit")
}
