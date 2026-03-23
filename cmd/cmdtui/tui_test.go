package cmdtui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/StephanSchmidt/human/internal/claude"
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
	return newModel(&stubFinder{})
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

func TestModelUpdate_UsageMsg(t *testing.T) {
	m := testModel()
	data := &usageData{
		DaemonUp:  true,
		DaemonPid: 1234,
		FetchedAt: time.Now(),
	}
	updated, _ := m.Update(usageMsg{data: data})
	um := updated.(model)
	if um.data == nil {
		t.Fatal("expected data to be set")
	}
	if um.data.DaemonPid != 1234 {
		t.Errorf("expected PID 1234, got %d", um.data.DaemonPid)
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
	m.data = &usageData{
		Err:       context.DeadlineExceeded,
		FetchedAt: time.Now(),
	}
	view := m.View()
	if !strings.Contains(view, "Error") {
		t.Errorf("expected 'Error' in view, got:\n%s", view)
	}
}

func TestModelView_WithData(t *testing.T) {
	m := testModel()
	m.data = &usageData{
		DaemonUp:  true,
		DaemonPid: 42,
		FetchedAt: time.Now(),
		Instances: []claude.InstanceUsage{
			{
				Instance: claude.Instance{Label: "Host (PID 100)", Source: "host"},
				Summary: &claude.UsageSummary{
					Models: map[string]*claude.ModelUsage{
						"opus 4.6": {InputTokens: 1000, OutputTokens: 500},
					},
				},
				State: claude.StateBusy,
			},
		},
	}
	view := m.View()
	if !strings.Contains(view, "opus") {
		t.Errorf("expected 'opus' in view, got:\n%s", view)
	}
	if !strings.Contains(view, "Usage window") {
		t.Errorf("expected 'Usage window' in view, got:\n%s", view)
	}
}

func TestModelView_DaemonRunning(t *testing.T) {
	m := testModel()
	m.data = &usageData{
		DaemonUp:  true,
		DaemonPid: 42,
		FetchedAt: time.Now(),
		Instances: []claude.InstanceUsage{},
	}
	view := m.View()
	if !strings.Contains(view, "running") {
		t.Errorf("expected 'running' in view, got:\n%s", view)
	}
}

func TestModelView_DaemonStopped(t *testing.T) {
	m := testModel()
	m.data = &usageData{
		DaemonUp:  false,
		FetchedAt: time.Now(),
		Instances: []claude.InstanceUsage{},
	}
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
	// os.Hostname should succeed in test environments, so we expect a parenthesized host.
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
