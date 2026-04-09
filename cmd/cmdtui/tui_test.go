package cmdtui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StephanSchmidt/human/internal/claude"
	"github.com/StephanSchmidt/human/internal/claude/logparser"
	"github.com/StephanSchmidt/human/internal/claude/monitor"
	"github.com/StephanSchmidt/human/internal/tracker"

	"github.com/StephanSchmidt/human/internal/daemon"
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

// Both "q" and Ctrl+C map to the same quit command. Use a table test
// instead of two near-identical functions so a future third quit key
// can be added in a single line.
func TestModelUpdate_quitKeys(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.Msg
	}{
		{"q", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}},
		{"ctrl+c", tea.KeyMsg{Type: tea.KeyCtrlC}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			m := testModel()
			updated, cmd := m.Update(tt.msg)
			um := updated.(model)
			if !um.quitting {
				t.Error("expected quitting to be true")
			}
			if cmd == nil {
				t.Error("expected non-nil quit command")
			}
		})
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
	assert.Equal(t, "full", cycleLogMode("bogus")) // unknown defaults to full
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
	footer := renderFooter(80, "meta", "", false)
	assert.Contains(t, footer, "log:meta")
	assert.Contains(t, footer, "l log")
	assert.Contains(t, footer, "q quit")
}

// --- issues panel tests ---

func TestRenderIssuesPanel_WithIssues(t *testing.T) {
	groups := []trackerIssues{
		{
			TrackerKind: "linear",
			Project:     "HUM",
			Issues: []tracker.Issue{
				{Key: "HUM-42", Status: "In Progress", StatusType: "started", Title: "Add issues to TUI"},
				{Key: "HUM-41", Status: "Todo", StatusType: "unstarted", Title: "Fix daemon reconnect"},
			},
		},
	}
	out := renderIssuesPanel(groups, time.Now(), 80, -1)
	assert.Contains(t, out, "Pipeline")
	assert.Contains(t, out, "Eng")
	assert.Contains(t, out, "HUM")
	assert.Contains(t, out, "HUM-42")
	assert.Contains(t, out, "In Dev")
	assert.Contains(t, out, "Add issues to TUI")
	assert.Contains(t, out, "HUM-41")
	assert.Contains(t, out, "Backlog")
}

func TestRenderIssuesPanel_Empty(t *testing.T) {
	out := renderIssuesPanel(nil, time.Time{}, 80, -1)
	assert.Equal(t, "", out)
}

func TestRenderIssuesPanel_TrackerError(t *testing.T) {
	groups := []trackerIssues{
		{
			TrackerKind: "jira",
			Project:     "KAN",
			Err:         fmt.Errorf("unauthorized"),
		},
	}
	out := renderIssuesPanel(groups, time.Now(), 80, -1)
	assert.Contains(t, out, "Pipeline")
	assert.Contains(t, out, "jira/KAN")
	assert.Contains(t, out, "fetch failed")
}

func TestRenderIssuesPanel_MixedSuccess(t *testing.T) {
	groups := []trackerIssues{
		{
			TrackerKind: "jira",
			Project:     "KAN",
			Err:         fmt.Errorf("timeout"),
		},
		{
			TrackerKind: "linear",
			Project:     "HUM",
			Issues: []tracker.Issue{
				{Key: "HUM-1", Status: "Todo", StatusType: "unstarted", Title: "Working issue"},
			},
		},
	}
	out := renderIssuesPanel(groups, time.Now(), 80, -1)
	assert.Contains(t, out, "Pipeline")
	assert.Contains(t, out, "fetch failed")
	assert.Contains(t, out, "HUM-1")
	assert.Contains(t, out, "Backlog")
	assert.Contains(t, out, "Working issue")
}

func TestModelUpdate_IssuesResultMsg(t *testing.T) {
	m := testModel()
	m.issuesLoading = true
	results := []trackerIssues{
		{
			TrackerKind: "linear",
			Project:     "HUM",
			Issues: []tracker.Issue{
				{Key: "HUM-1", Title: "Test"},
			},
		},
	}
	updated, _ := m.Update(issuesResultMsg{results: results})
	um := updated.(model)
	assert.False(t, um.issuesLoading)
	assert.Len(t, um.issues, 1)
	assert.Len(t, um.issues[0].Issues, 1)
	assert.False(t, um.issuesFetched.IsZero())
}

func TestModelUpdate_IssueTickWhileLoading(t *testing.T) {
	m := testModel()
	m.issuesLoading = true
	updated, cmd := m.Update(issueTickMsg(time.Now()))
	um := updated.(model)
	assert.True(t, um.issuesLoading, "should remain loading")
	assert.NotNil(t, cmd, "should reschedule tick")
}

func TestModelView_WithIssues(t *testing.T) {
	m := testModel()
	m.snap = testSnapshot()
	m.issues = []trackerIssues{
		{
			TrackerKind: "linear",
			Project:     "HUM",
			Issues: []tracker.Issue{
				{Key: "HUM-99", Status: "Todo", StatusType: "unstarted", Title: "Visible in view"},
			},
		},
	}
	m.issuesFetched = time.Now()
	view := m.View()
	assert.Contains(t, view, "Pipeline")
	assert.Contains(t, view, "HUM-99")
	assert.Contains(t, view, "Visible in view")
}

func TestPipelineStage(t *testing.T) {
	tests := []struct {
		kind, status, statusType, want string
	}{
		{"shortcut", "To Do", "unstarted", "Ready for Plan"},
		{"shortcut", "In Progress", "started", "Planning"},
		{"shortcut", "Done", "done", "Planned"},
		{"shortcut", "Custom", "", "Custom"},
		{"linear", "Backlog", "unstarted", "Backlog"},
		{"linear", "In Progress", "started", "In Dev"},
		{"linear", "Done", "done", "Done"},
		{"linear", "Canceled", "closed", "Closed"},
		{"jira", "Open", "", "Open"},
	}
	for _, tt := range tests {
		t.Run(tt.kind+"/"+tt.statusType, func(t *testing.T) {
			got := pipelineStage(tt.kind, "", tt.status, tt.statusType)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPipelineStageStyle(t *testing.T) {
	// Verify the correct style is returned for each status type.
	assert.Equal(t, subtleStyle, pipelineStageStyle("unstarted"))
	assert.Equal(t, warningStyle, pipelineStageStyle("started"))
	assert.Equal(t, specialStyle, pipelineStageStyle("done"))
	assert.Equal(t, subtleStyle, pipelineStageStyle("closed"))
	assert.Equal(t, subtleStyle, pipelineStageStyle(""))
}

func TestPipelineName(t *testing.T) {
	// Inferred roles from kind.
	assert.Contains(t, pipelineName("shortcut", ""), "PM")
	assert.Contains(t, pipelineName("linear", ""), "Eng")
	assert.Contains(t, pipelineName("jira", ""), "jira")
	// Explicit roles override kind.
	assert.Contains(t, pipelineName("jira", "pm"), "PM")
	assert.Contains(t, pipelineName("github", "engineering"), "Eng")
}

// --- flattenIssues tests ---

func TestFlattenIssues(t *testing.T) {
	groups := []trackerIssues{
		{TrackerKind: "shortcut", Project: "PM", Issues: []tracker.Issue{
			{Key: "42", Title: "A"},
			{Key: "43", Title: "B"},
		}},
		{TrackerKind: "jira", Project: "KAN", Err: fmt.Errorf("timeout")},
		{TrackerKind: "linear", Project: "HUM", Issues: []tracker.Issue{
			{Key: "HUM-1", Title: "C"},
		}},
		{TrackerKind: "linear", Project: "EMPTY"},
	}
	flat := flattenIssues(groups)
	assert.Len(t, flat, 3)
	assert.Equal(t, "shortcut", flat[0].TrackerKind)
	assert.Equal(t, "42", flat[0].Issue.Key)
	assert.Equal(t, "shortcut", flat[1].TrackerKind)
	assert.Equal(t, "43", flat[1].Issue.Key)
	assert.Equal(t, "linear", flat[2].TrackerKind)
	assert.Equal(t, "HUM-1", flat[2].Issue.Key)
}

func TestFlattenIssues_Empty(t *testing.T) {
	assert.Empty(t, flattenIssues(nil))
	assert.Empty(t, flattenIssues([]trackerIssues{}))
}

// --- clampCursor tests ---

func TestClampCursor(t *testing.T) {
	m := testModel()
	m.issues = []trackerIssues{
		{TrackerKind: "linear", Project: "HUM", Issues: []tracker.Issue{
			{Key: "HUM-1"}, {Key: "HUM-2"},
		}},
	}
	m.issueCursor = 5
	m.clampCursor()
	assert.Equal(t, 1, m.issueCursor)

	m.issues = nil
	m.issueCursor = 3
	m.clampCursor()
	assert.Equal(t, 0, m.issueCursor)
}

// --- navigation tests ---

func TestNavigationKeys_Down(t *testing.T) {
	m := testModel()
	m.issues = []trackerIssues{
		{TrackerKind: "linear", Project: "HUM", Issues: []tracker.Issue{
			{Key: "HUM-1"}, {Key: "HUM-2"}, {Key: "HUM-3"},
		}},
	}
	assert.Equal(t, 0, m.issueCursor)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	um := updated.(model)
	assert.Equal(t, 1, um.issueCursor)

	updated, _ = um.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	um = updated.(model)
	assert.Equal(t, 2, um.issueCursor)

	// Clamp at max.
	updated, _ = um.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	um = updated.(model)
	assert.Equal(t, 2, um.issueCursor)
}

func TestNavigationKeys_Up(t *testing.T) {
	m := testModel()
	m.issues = []trackerIssues{
		{TrackerKind: "linear", Project: "HUM", Issues: []tracker.Issue{
			{Key: "HUM-1"}, {Key: "HUM-2"},
		}},
	}
	m.issueCursor = 1

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	um := updated.(model)
	assert.Equal(t, 0, um.issueCursor)

	// Clamp at 0.
	updated, _ = um.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	um = updated.(model)
	assert.Equal(t, 0, um.issueCursor)
}

// --- dispatch tests ---

func TestDispatch_NoIssues(t *testing.T) {
	m := testModel()
	m.snap = testSnapshot(func(s *monitor.Snapshot) {
		s.Panes = []claude.TmuxPane{{SessionName: "s", State: claude.StateReady}}
	})

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	um := updated.(model)
	assert.Equal(t, "No issues", um.dispatchStatus)
	assert.Nil(t, cmd)
}

func TestDispatch_NoIdlePanes(t *testing.T) {
	m := testModel()
	m.snap = testSnapshot(func(s *monitor.Snapshot) {
		s.Panes = []claude.TmuxPane{{SessionName: "s", State: claude.StateBusy}}
	})
	m.issues = []trackerIssues{
		{TrackerKind: "linear", Project: "HUM", Issues: []tracker.Issue{{Key: "HUM-1"}}},
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	um := updated.(model)
	assert.Equal(t, "No idle panes", um.dispatchStatus)
	assert.Nil(t, cmd)
}

func TestDispatch_NoPanes(t *testing.T) {
	m := testModel()
	m.snap = testSnapshot() // no panes
	m.issues = []trackerIssues{
		{TrackerKind: "linear", Project: "HUM", Issues: []tracker.Issue{{Key: "HUM-1"}}},
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	um := updated.(model)
	assert.Equal(t, "No idle panes", um.dispatchStatus)
	assert.Nil(t, cmd)
}

func TestDispatch_LinearIssue(t *testing.T) {
	m := testModel()
	m.snap = testSnapshot(func(s *monitor.Snapshot) {
		s.Panes = []claude.TmuxPane{{SessionName: "work", WindowIndex: 0, PaneIndex: 1, State: claude.StateReady}}
	})
	m.issues = []trackerIssues{
		{TrackerKind: "linear", Project: "HUM", Issues: []tracker.Issue{{Key: "HUM-42"}}},
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	um := updated.(model)
	assert.True(t, um.dispatching)
	assert.NotNil(t, cmd)
}

func TestDispatch_ShortcutIssue(t *testing.T) {
	m := testModel()
	m.snap = testSnapshot(func(s *monitor.Snapshot) {
		s.Panes = []claude.TmuxPane{{SessionName: "work", State: claude.StateReady}}
	})
	m.issues = []trackerIssues{
		{TrackerKind: "shortcut", Project: "PM", Issues: []tracker.Issue{{Key: "99"}}},
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	um := updated.(model)
	assert.True(t, um.dispatching)
	assert.NotNil(t, cmd)
}

func TestDispatch_WhileDispatching(t *testing.T) {
	m := testModel()
	m.dispatching = true
	m.snap = testSnapshot(func(s *monitor.Snapshot) {
		s.Panes = []claude.TmuxPane{{SessionName: "s", State: claude.StateReady}}
	})
	m.issues = []trackerIssues{
		{TrackerKind: "linear", Project: "HUM", Issues: []tracker.Issue{{Key: "HUM-1"}}},
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	um := updated.(model)
	assert.True(t, um.dispatching)
	assert.Nil(t, cmd)
}

func TestDispatchResultMsg_Success(t *testing.T) {
	m := testModel()
	m.dispatching = true

	updated, _ := m.Update(dispatchResultMsg{issueKey: "HUM-42", paneLabel: "work:0.1"})
	um := updated.(model)
	assert.False(t, um.dispatching)
	assert.Contains(t, um.dispatchStatus, "HUM-42")
	assert.Contains(t, um.dispatchStatus, "work:0.1")
	assert.False(t, um.dispatchAt.IsZero())
}

func TestDispatchResultMsg_Error(t *testing.T) {
	m := testModel()
	m.dispatching = true

	updated, _ := m.Update(dispatchResultMsg{issueKey: "HUM-42", err: fmt.Errorf("connection refused")})
	um := updated.(model)
	assert.False(t, um.dispatching)
	assert.Contains(t, um.dispatchStatus, "Failed")
	assert.Contains(t, um.dispatchStatus, "connection refused")
}

func TestDispatchStatusAutoClear(t *testing.T) {
	m := testModel()
	m.fetching = false
	m.dispatchStatus = "Sent HUM-42"
	m.dispatchAt = time.Now().Add(-4 * time.Second) // 4s ago

	updated, _ := m.Update(fastTickMsg(time.Now()))
	um := updated.(model)
	assert.Equal(t, "", um.dispatchStatus)
}

func TestDispatchStatusNotClearedEarly(t *testing.T) {
	m := testModel()
	m.fetching = false
	m.dispatchStatus = "Sent HUM-42"
	m.dispatchAt = time.Now() // just now

	updated, _ := m.Update(fastTickMsg(time.Now()))
	um := updated.(model)
	assert.Equal(t, "Sent HUM-42", um.dispatchStatus)
}

// --- render tests for cursor ---

func TestRenderIssuesPanelCursor(t *testing.T) {
	groups := []trackerIssues{
		{TrackerKind: "linear", Project: "HUM", Issues: []tracker.Issue{
			{Key: "HUM-1", Status: "Todo", StatusType: "unstarted", Title: "First"},
			{Key: "HUM-2", Status: "Todo", StatusType: "unstarted", Title: "Second"},
		}},
	}
	out := renderIssuesPanel(groups, time.Now(), 80, 1)
	// Second issue should have the selection indicator.
	assert.Contains(t, out, "▸")
}

func TestRenderFooter_ShowsDispatchStatus(t *testing.T) {
	footer := renderFooter(120, "off", "Sent HUM-42 → work:0.1", false)
	assert.Contains(t, footer, "Sent HUM-42")
	assert.Contains(t, footer, "⏎ send")
}

func TestRenderFooter_ShowsNavKeys(t *testing.T) {
	footer := renderFooter(120, "", "", false)
	assert.Contains(t, footer, "j/k nav")
	assert.Contains(t, footer, "⏎ send")
}

func TestDispatch_SnapNil(t *testing.T) {
	m := testModel()
	// snap is nil (initial state)
	m.issues = []trackerIssues{
		{TrackerKind: "linear", Project: "HUM", Issues: []tracker.Issue{{Key: "HUM-1"}}},
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	um := updated.(model)
	assert.Equal(t, "No idle panes", um.dispatchStatus)
	assert.Nil(t, cmd)
}

// --- open in browser tests ---

func TestOpenBrowser_NoIssues(t *testing.T) {
	m := testModel()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	um := updated.(model)
	assert.Equal(t, "", um.dispatchStatus)
	assert.Nil(t, cmd)
}

func TestOpenBrowser_NoURL(t *testing.T) {
	m := testModel()
	m.issues = []trackerIssues{
		{TrackerKind: "linear", Project: "HUM", Issues: []tracker.Issue{{Key: "HUM-1"}}},
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	um := updated.(model)
	assert.Equal(t, "No URL for HUM-1", um.dispatchStatus)
	assert.Nil(t, cmd)
}

func TestOpenBrowser_HasURL(t *testing.T) {
	m := testModel()
	m.issues = []trackerIssues{
		{TrackerKind: "linear", Project: "HUM", Issues: []tracker.Issue{
			{Key: "HUM-1", URL: "https://linear.app/hum/issue/HUM-1/title"},
		}},
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	um := updated.(model)
	assert.Equal(t, "", um.dispatchStatus) // no status until async cmd completes
	assert.NotNil(t, cmd)                  // async command to open browser
}

func TestOpenBrowserMsg_Success(t *testing.T) {
	m := testModel()
	updated, _ := m.Update(openBrowserMsg{issueKey: "HUM-1"})
	um := updated.(model)
	assert.Equal(t, "Opened HUM-1", um.dispatchStatus)
	assert.False(t, um.dispatchAt.IsZero())
}

func TestOpenBrowserMsg_Error(t *testing.T) {
	m := testModel()
	updated, _ := m.Update(openBrowserMsg{issueKey: "HUM-1", err: fmt.Errorf("no browser")})
	um := updated.(model)
	assert.Contains(t, um.dispatchStatus, "Open failed")
	assert.Contains(t, um.dispatchStatus, "no browser")
}

func TestRenderFooter_ShowsOpenKey(t *testing.T) {
	footer := renderFooter(120, "", "", false)
	assert.Contains(t, footer, "o open")
}

func TestRenderFooter_ShowsTabHint(t *testing.T) {
	footer := renderFooter(120, "", "", true)
	assert.Contains(t, footer, "Tab switch")
}

func TestRenderFooter_NoTabHint(t *testing.T) {
	footer := renderFooter(120, "", "", false)
	assert.NotContains(t, footer, "Tab switch")
}

// --- project tab tests ---

func TestTabs_NoProjects(t *testing.T) {
	m := testModel()
	assert.Nil(t, m.tabs())
}

func TestTabs_SingleProject(t *testing.T) {
	m := testModel()
	m.projects = []daemon.ProjectInfo{{Name: "cli", Dir: "/home/user/cli"}}
	tabs := m.tabs()
	require.Len(t, tabs, 1, "single project should show one tab")
	assert.Equal(t, "cli", tabs[0].Name)
}

func TestTabs_MultipleProjects(t *testing.T) {
	m := testModel()
	m.projects = []daemon.ProjectInfo{
		{Name: "cli", Dir: "/home/user/cli"},
		{Name: "web", Dir: "/home/user/web"},
	}
	tabs := m.tabs()
	assert.Len(t, tabs, 2)
	assert.Equal(t, "cli", tabs[0].Name)
	assert.Equal(t, "web", tabs[1].Name)
}

func TestTabs_OtherTabShownForUnmatched(t *testing.T) {
	m := testModel()
	m.projects = []daemon.ProjectInfo{
		{Name: "cli", Dir: "/home/user/cli"},
		{Name: "web", Dir: "/home/user/web"},
	}
	m.snap = testSnapshot(func(s *monitor.Snapshot) {
		s.Instances = []monitor.InstanceView{
			{Usage: claude.InstanceUsage{Instance: claude.Instance{Cwd: "/home/user/other"}}},
		}
	})
	tabs := m.tabs()
	assert.Len(t, tabs, 3)
	assert.Equal(t, "Other", tabs[2].Name)
	assert.Equal(t, "", tabs[2].Dir)
}

func TestTabs_NoOtherTabWhenAllMatched(t *testing.T) {
	m := testModel()
	m.projects = []daemon.ProjectInfo{
		{Name: "cli", Dir: "/home/user/cli"},
		{Name: "web", Dir: "/home/user/web"},
	}
	m.snap = testSnapshot(func(s *monitor.Snapshot) {
		s.Instances = []monitor.InstanceView{
			{Usage: claude.InstanceUsage{Instance: claude.Instance{Cwd: "/home/user/cli/subdir"}}},
		}
	})
	tabs := m.tabs()
	assert.Len(t, tabs, 2)
}

func TestFilterInstances_NoTabs(t *testing.T) {
	m := testModel()
	instances := []monitor.InstanceView{
		{Usage: claude.InstanceUsage{Instance: claude.Instance{Cwd: "/a"}}},
		{Usage: claude.InstanceUsage{Instance: claude.Instance{Cwd: "/b"}}},
	}
	assert.Len(t, m.filterInstances(instances), 2, "no tabs means all instances returned")
}

func TestFilterInstances_ByProject(t *testing.T) {
	m := testModel()
	m.projects = []daemon.ProjectInfo{
		{Name: "cli", Dir: "/home/user/cli"},
		{Name: "web", Dir: "/home/user/web"},
	}
	m.snap = testSnapshot()
	m.activeTab = 0

	instances := []monitor.InstanceView{
		{Usage: claude.InstanceUsage{Instance: claude.Instance{Label: "cli-1", Cwd: "/home/user/cli"}}},
		{Usage: claude.InstanceUsage{Instance: claude.Instance{Label: "cli-2", Cwd: "/home/user/cli/subdir"}}},
		{Usage: claude.InstanceUsage{Instance: claude.Instance{Label: "web-1", Cwd: "/home/user/web"}}},
	}
	filtered := m.filterInstances(instances)
	assert.Len(t, filtered, 2)
	assert.Equal(t, "cli-1", filtered[0].Usage.Instance.Label)
	assert.Equal(t, "cli-2", filtered[1].Usage.Instance.Label)

	m.activeTab = 1
	filtered = m.filterInstances(instances)
	assert.Len(t, filtered, 1)
	assert.Equal(t, "web-1", filtered[0].Usage.Instance.Label)
}

func TestFilterInstances_OtherTab(t *testing.T) {
	m := testModel()
	m.projects = []daemon.ProjectInfo{
		{Name: "cli", Dir: "/home/user/cli"},
		{Name: "web", Dir: "/home/user/web"},
	}
	instances := []monitor.InstanceView{
		{Usage: claude.InstanceUsage{Instance: claude.Instance{Label: "cli-1", Cwd: "/home/user/cli"}}},
		{Usage: claude.InstanceUsage{Instance: claude.Instance{Label: "other-1", Cwd: "/home/user/other"}}},
	}
	m.snap = testSnapshot(func(s *monitor.Snapshot) {
		s.Instances = instances
	})
	// "Other" tab should be index 2.
	m.activeTab = 2
	filtered := m.filterInstances(instances)
	assert.Len(t, filtered, 1)
	assert.Equal(t, "other-1", filtered[0].Usage.Instance.Label)
}

func TestTabSwitching_Tab(t *testing.T) {
	m := testModel()
	m.projects = []daemon.ProjectInfo{
		{Name: "cli", Dir: "/home/user/cli"},
		{Name: "web", Dir: "/home/user/web"},
	}
	m.snap = testSnapshot()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	um := updated.(model)
	assert.Equal(t, 1, um.activeTab)

	updated, _ = um.Update(tea.KeyMsg{Type: tea.KeyTab})
	um = updated.(model)
	assert.Equal(t, 0, um.activeTab, "should wrap around")
}

func TestTabSwitching_ShiftTab(t *testing.T) {
	m := testModel()
	m.projects = []daemon.ProjectInfo{
		{Name: "cli", Dir: "/home/user/cli"},
		{Name: "web", Dir: "/home/user/web"},
	}
	m.snap = testSnapshot()
	m.activeTab = 0

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	um := updated.(model)
	assert.Equal(t, 1, um.activeTab, "should wrap to last tab")
}

func TestTabSwitching_NumberKeys(t *testing.T) {
	m := testModel()
	m.projects = []daemon.ProjectInfo{
		{Name: "cli", Dir: "/home/user/cli"},
		{Name: "web", Dir: "/home/user/web"},
		{Name: "api", Dir: "/home/user/api"},
	}
	m.snap = testSnapshot()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	um := updated.(model)
	assert.Equal(t, 1, um.activeTab)

	updated, _ = um.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	um = updated.(model)
	assert.Equal(t, 2, um.activeTab)

	// Out of range: should not change.
	updated, _ = um.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("9")})
	um = updated.(model)
	assert.Equal(t, 2, um.activeTab, "out of range number should not change tab")
}

func TestTabSwitching_NoTabsIgnored(t *testing.T) {
	m := testModel()
	// No projects, so no tabs. Tab key should be a no-op.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	um := updated.(model)
	assert.Equal(t, 0, um.activeTab)
	assert.Nil(t, cmd)
}

func TestProjectsMsg(t *testing.T) {
	m := testModel()
	projects := []daemon.ProjectInfo{
		{Name: "cli", Dir: "/home/user/cli"},
		{Name: "web", Dir: "/home/user/web"},
	}

	updated, _ := m.Update(projectsMsg(projects))
	um := updated.(model)
	assert.Len(t, um.projects, 2)
	assert.Equal(t, "cli", um.projects[0].Name)
}

func TestProjectsMsg_ClampsActiveTab(t *testing.T) {
	m := testModel()
	m.projects = []daemon.ProjectInfo{
		{Name: "cli", Dir: "/home/user/cli"},
		{Name: "web", Dir: "/home/user/web"},
		{Name: "api", Dir: "/home/user/api"},
	}
	m.activeTab = 2

	// Now reduce projects to 2 — activeTab 2 is out of bounds.
	newProjects := []daemon.ProjectInfo{
		{Name: "cli", Dir: "/home/user/cli"},
		{Name: "web", Dir: "/home/user/web"},
	}
	updated, _ := m.Update(projectsMsg(newProjects))
	um := updated.(model)
	assert.Equal(t, 0, um.activeTab, "activeTab should be clamped to 0")
}

func TestRenderTabBar_Empty(t *testing.T) {
	assert.Equal(t, "", renderTabBar(nil, 0, 80))
}

func TestRenderTabBar_TwoTabs(t *testing.T) {
	tabs := []tab{
		{Name: "cli", Dir: "/home/user/cli"},
		{Name: "web", Dir: "/home/user/web"},
	}
	out := renderTabBar(tabs, 0, 80)
	assert.Contains(t, out, "1:cli")
	assert.Contains(t, out, "2:web")
}

func TestRenderTabBar_OtherTab(t *testing.T) {
	tabs := []tab{
		{Name: "cli", Dir: "/home/user/cli"},
		{Name: "web", Dir: "/home/user/web"},
		{Name: "Other"},
	}
	out := renderTabBar(tabs, 2, 80)
	assert.Contains(t, out, "3:Other")
}

func TestMatchesAnyProject(t *testing.T) {
	projects := []daemon.ProjectInfo{
		{Name: "cli", Dir: "/home/user/cli"},
		{Name: "web", Dir: "/home/user/web"},
	}
	assert.True(t, matchesAnyProject("/home/user/cli", projects))
	assert.True(t, matchesAnyProject("/home/user/cli/subdir", projects))
	assert.True(t, matchesAnyProject("/home/user/web", projects))
	assert.False(t, matchesAnyProject("/home/user/other", projects))
	assert.False(t, matchesAnyProject("/completely/different", projects))
	assert.False(t, matchesAnyProject("", projects))
}

func TestModelView_WithTabBar(t *testing.T) {
	m := testModel()
	m.projects = []daemon.ProjectInfo{
		{Name: "cli", Dir: "/home/user/cli"},
		{Name: "web", Dir: "/home/user/web"},
	}
	m.snap = testSnapshot(func(s *monitor.Snapshot) {
		s.Instances = []monitor.InstanceView{
			{
				Usage: claude.InstanceUsage{
					Instance: claude.Instance{Label: "Host (PID 100)", Source: "host", Cwd: "/home/user/cli"},
					Summary:  &claude.UsageSummary{Models: map[string]*claude.ModelUsage{}},
					State:    claude.StateUnknown,
				},
			},
		}
		s.TotalUsage = &claude.UsageSummary{Models: map[string]*claude.ModelUsage{}}
	})
	view := m.View()
	assert.Contains(t, view, "1:cli")
	assert.Contains(t, view, "2:web")
	assert.Contains(t, view, "Tab switch")
}

func TestModelView_TabBarSingleProject(t *testing.T) {
	m := testModel()
	m.projects = []daemon.ProjectInfo{
		{Name: "cli", Dir: "/home/user/cli"},
	}
	m.snap = testSnapshot()
	view := m.View()
	assert.Contains(t, view, "1:cli")
	assert.Contains(t, view, "Tab switch")
}

func TestNextAgentName(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// No existing agents: should return agent-1.
	name := nextAgentName()
	assert.Equal(t, "agent-1", name)
}

func TestHandleSpawnAgent_NotInTmux(t *testing.T) {
	t.Setenv("TMUX", "")

	m := testModel()
	result, cmd := m.handleSpawnAgent()
	resultModel := result.(model)

	assert.Nil(t, cmd)
	assert.Contains(t, resultModel.dispatchStatus, "Not in tmux")
}

func TestHandleSpawnAgentResult_Success(t *testing.T) {
	m := testModel()
	m.handleSpawnAgentResult(spawnAgentMsg{name: "agent-1"})
	assert.Contains(t, m.dispatchStatus, "Spawned agent-1")
}

func TestHandleSpawnAgentResult_Error(t *testing.T) {
	m := testModel()
	m.handleSpawnAgentResult(spawnAgentMsg{name: "agent-1", err: fmt.Errorf("pane too small")})
	assert.Contains(t, m.dispatchStatus, "Spawn failed")
}

func TestFooterContainsAgentHint(t *testing.T) {
	footer := renderFooter(80, "", "", false)
	assert.Contains(t, footer, "a agent")
}
