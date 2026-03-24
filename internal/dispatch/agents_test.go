package dispatch

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StephanSchmidt/human/internal/claude"
)

// --- Mocks for agent finder ---

type stubInstanceFinder struct {
	instances []claude.Instance
	err       error
}

func (f *stubInstanceFinder) FindInstances(_ context.Context) ([]claude.Instance, error) {
	return f.instances, f.err
}

type stubTmuxClient struct {
	panes []claude.TmuxPane
	err   error
}

func (c *stubTmuxClient) ListPanes(_ context.Context) ([]claude.TmuxPane, error) {
	return c.panes, c.err
}

type stubProcessLister struct {
	procs []claude.ProcessInfo
	err   error
}

func (l *stubProcessLister) ListProcesses(_ context.Context) ([]claude.ProcessInfo, error) {
	return l.procs, l.err
}

type fixedStateReader struct {
	state claude.InstanceState
}

func (r fixedStateReader) ReadState(_ string) (claude.InstanceState, error) {
	return r.state, nil
}

func TestTmuxAgentFinder_FindIdleAgents(t *testing.T) {
	finder := &TmuxAgentFinder{
		InstanceFinder: &stubInstanceFinder{
			instances: []claude.Instance{
				{Source: "host", PID: 1000, StateReader: fixedStateReader{claude.StateReady}, Root: "/tmp"},
				{Source: "host", PID: 2000, StateReader: fixedStateReader{claude.StateBusy}, Root: "/tmp"},
			},
		},
		TmuxClient: &stubTmuxClient{
			panes: []claude.TmuxPane{
				{PID: 500, SessionName: "work", WindowIndex: 0, PaneIndex: 0},
				{PID: 600, SessionName: "work", WindowIndex: 0, PaneIndex: 1},
			},
		},
		ProcessLister: &stubProcessLister{
			procs: []claude.ProcessInfo{
				// Pane 500 shell → claude PID 1000 (idle)
				{PID: 500, PPID: 1, Comm: "zsh"},
				{PID: 1000, PPID: 500, Comm: "claude"},
				// Pane 600 shell → claude PID 2000 (busy)
				{PID: 600, PPID: 1, Comm: "zsh"},
				{PID: 2000, PPID: 600, Comm: "claude"},
			},
		},
	}

	agents, err := finder.FindIdleAgents(context.Background())
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, "work:0.0", agents[0].Label)
	assert.Equal(t, "work", agents[0].SessionName)
}

func TestTmuxAgentFinder_NoIdleInstances(t *testing.T) {
	finder := &TmuxAgentFinder{
		InstanceFinder: &stubInstanceFinder{
			instances: []claude.Instance{
				{Source: "host", PID: 1000, StateReader: fixedStateReader{claude.StateBusy}, Root: "/tmp"},
			},
		},
		TmuxClient:    &stubTmuxClient{},
		ProcessLister: &stubProcessLister{},
	}

	agents, err := finder.FindIdleAgents(context.Background())
	require.NoError(t, err)
	assert.Empty(t, agents)
}

func TestTmuxAgentFinder_InstanceFinderError(t *testing.T) {
	finder := &TmuxAgentFinder{
		InstanceFinder: &stubInstanceFinder{err: fmt.Errorf("pgrep failed")},
		TmuxClient:     &stubTmuxClient{},
		ProcessLister:  &stubProcessLister{},
	}

	_, err := finder.FindIdleAgents(context.Background())
	require.Error(t, err)
}

func TestTmuxAgentFinder_ContainerInstancesSkipped(t *testing.T) {
	finder := &TmuxAgentFinder{
		InstanceFinder: &stubInstanceFinder{
			instances: []claude.Instance{
				{Source: "container", PID: 0, StateReader: fixedStateReader{claude.StateReady}, Root: "/container/abc"},
			},
		},
		TmuxClient:    &stubTmuxClient{},
		ProcessLister: &stubProcessLister{},
	}

	agents, err := finder.FindIdleAgents(context.Background())
	require.NoError(t, err)
	assert.Empty(t, agents)
}
