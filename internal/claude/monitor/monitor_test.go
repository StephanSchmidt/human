package monitor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StephanSchmidt/human/internal/claude"
	"github.com/StephanSchmidt/human/internal/claude/hookevents"
	"github.com/StephanSchmidt/human/internal/claude/logparser"
)

// --- stubs ---

type stubFinder struct {
	instances []claude.Instance
	err       error
}

func (s *stubFinder) FindInstances(_ context.Context) ([]claude.Instance, error) {
	return s.instances, s.err
}

// --- overlayHookState tests ---

func TestOverlayHookState_NoHooks(t *testing.T) {
	byPath := map[string]logparser.SessionState{
		"/a.jsonl": {SessionID: "s1", IsWorking: true},
	}
	overlayHookState(byPath, nil)
	assert.True(t, byPath["/a.jsonl"].IsWorking, "should remain unchanged")
}

func TestOverlayHookState_HookNewer(t *testing.T) {
	jsonlTime := time.Date(2026, 3, 25, 10, 0, 0, 0, time.UTC)
	hookTime := time.Date(2026, 3, 25, 10, 0, 5, 0, time.UTC)

	byPath := map[string]logparser.SessionState{
		"/a.jsonl": {SessionID: "s1", IsWorking: true, LastActivity: jsonlTime},
	}
	hooks := map[string]hookevents.SessionSnapshot{
		"s1": {SessionID: "s1", IsWorking: false, LastEventAt: hookTime},
	}
	overlayHookState(byPath, hooks)

	sess := byPath["/a.jsonl"]
	assert.False(t, sess.IsWorking, "hook says idle")
	assert.Equal(t, hookTime, sess.LastActivity)
}

func TestOverlayHookState_HookOlder(t *testing.T) {
	jsonlTime := time.Date(2026, 3, 25, 10, 0, 5, 0, time.UTC)
	hookTime := time.Date(2026, 3, 25, 10, 0, 0, 0, time.UTC)

	byPath := map[string]logparser.SessionState{
		"/a.jsonl": {SessionID: "s1", IsWorking: true, LastActivity: jsonlTime},
	}
	hooks := map[string]hookevents.SessionSnapshot{
		"s1": {SessionID: "s1", IsWorking: false, LastEventAt: hookTime},
	}
	overlayHookState(byPath, hooks)
	assert.True(t, byPath["/a.jsonl"].IsWorking, "JSONL is newer, should keep")
}

// --- matchInstances tests ---

func TestMatchInstances_WithSession(t *testing.T) {
	usages := []claude.InstanceUsage{
		{Instance: claude.Instance{FilePath: "/a.jsonl"}},
	}
	byPath := map[string]logparser.SessionState{
		"/a.jsonl": {SessionID: "s1", IsWorking: true},
	}
	views := matchInstances(usages, byPath)
	require.Len(t, views, 1)
	require.NotNil(t, views[0].Session)
	assert.Equal(t, "s1", views[0].Session.SessionID)
}

func TestMatchInstances_NoSession(t *testing.T) {
	usages := []claude.InstanceUsage{
		{Instance: claude.Instance{FilePath: "/a.jsonl"}},
	}
	views := matchInstances(usages, nil)
	require.Len(t, views, 1)
	assert.Nil(t, views[0].Session)
}

// --- matchPaneStates tests ---

func TestMatchPaneStates_ByPID(t *testing.T) {
	panes := []claude.TmuxPane{{ClaudePID: 100}}
	byPath := map[string]logparser.SessionState{
		"/a.jsonl": {SessionID: "s1", IsWorking: true},
	}
	instances := []claude.Instance{{PID: 100, FilePath: "/a.jsonl"}}

	matchPaneStates(panes, byPath, instances)
	assert.Equal(t, claude.StateBusy, panes[0].State)
}

func TestMatchPaneStates_ByCwd(t *testing.T) {
	panes := []claude.TmuxPane{{Cwd: "/proj"}}
	byPath := map[string]logparser.SessionState{
		"/a.jsonl": {SessionID: "s1", Cwd: "/proj", IsWorking: false, LastActivity: time.Now()},
	}

	matchPaneStates(panes, byPath, nil)
	assert.Equal(t, claude.StateReady, panes[0].State)
}

func TestMatchPaneStates_NoMatch(t *testing.T) {
	panes := []claude.TmuxPane{{ClaudePID: 999}}
	matchPaneStates(panes, nil, nil)
	assert.Equal(t, claude.StateUnknown, panes[0].State)
}

// --- collectContainerIDs tests ---

func TestCollectContainerIDs(t *testing.T) {
	instances := []claude.Instance{
		{Source: "host", ContainerID: ""},
		{Source: "container", ContainerID: "abc123"},
		{Source: "container", ContainerID: "def456"},
	}
	ids := collectContainerIDs(instances)
	assert.Equal(t, []string{"abc123", "def456"}, ids)
}

func TestCollectContainerIDs_Empty(t *testing.T) {
	ids := collectContainerIDs(nil)
	assert.Nil(t, ids)
}

// --- extractUsages tests ---

func TestExtractUsages(t *testing.T) {
	views := []InstanceView{
		{Usage: claude.InstanceUsage{Instance: claude.Instance{Label: "a"}}},
		{Usage: claude.InstanceUsage{Instance: claude.Instance{Label: "b"}}},
	}
	usages := extractUsages(views)
	require.Len(t, usages, 2)
	assert.Equal(t, "a", usages[0].Instance.Label)
	assert.Equal(t, "b", usages[1].Instance.Label)
}

// --- aggregateUsage tests ---

func TestAggregateUsage(t *testing.T) {
	usages := []claude.InstanceUsage{
		{Summary: &claude.UsageSummary{Models: map[string]*claude.ModelUsage{
			"opus": {InputTokens: 100, OutputTokens: 50},
		}}},
		{Summary: &claude.UsageSummary{Models: map[string]*claude.ModelUsage{
			"opus": {InputTokens: 200, OutputTokens: 100},
		}}},
	}
	total := aggregateUsage(usages)
	require.NotNil(t, total.Models["opus"])
	assert.Equal(t, 300, total.Models["opus"].InputTokens)
	assert.Equal(t, 150, total.Models["opus"].OutputTokens)
}

// --- sessionToState tests ---

func TestSessionToState(t *testing.T) {
	assert.Equal(t, claude.StateBusy, sessionToState(logparser.SessionState{IsWorking: true}))
	assert.Equal(t, claude.StateReady, sessionToState(logparser.SessionState{IsWorking: false}))
}

// --- buildHookReaders tests ---

func TestBuildHookReaders_HostOnly(t *testing.T) {
	readers := buildHookReaders(nil, nil)
	// Should have at least the host reader (if home dir resolves).
	assert.NotEmpty(t, readers)
}

func TestBuildHookReaders_WithContainers(t *testing.T) {
	instances := []claude.Instance{
		{Source: "container", ContainerID: "abc123def456xyz"},
		{Source: "container", ContainerID: "abc123def456xyz"}, // duplicate
		{Source: "host"},
	}
	// dc is nil, so no container readers added.
	readers := buildHookReaders(instances, nil)
	// Only host reader.
	hostCount := 0
	for range readers {
		hostCount++
	}
	assert.GreaterOrEqual(t, hostCount, 1)
}

// --- readHookSnapshots tests ---

func TestReadHookSnapshots_NoReaders(t *testing.T) {
	snaps := readHookSnapshots(context.Background(), nil)
	assert.Empty(t, snaps)
}

// --- FetchFull integration test ---

func TestFetchFull_NoInstances(t *testing.T) {
	mon := New(&stubFinder{}, nil)
	snap := mon.FetchFull(context.Background())
	require.NotNil(t, snap)
	assert.NoError(t, snap.Err)
	assert.Empty(t, snap.Instances)
	require.NotNil(t, snap.TotalUsage)
}

func TestFetchFull_FinderError(t *testing.T) {
	mon := New(&stubFinder{err: context.DeadlineExceeded}, nil)
	snap := mon.FetchFull(context.Background())
	require.NotNil(t, snap)
	assert.ErrorIs(t, snap.Err, context.DeadlineExceeded)
}

// --- FetchQuick tests ---

func TestFetchQuick_NilPrev(t *testing.T) {
	mon := New(&stubFinder{}, nil)
	snap := mon.FetchQuick(context.Background(), nil)
	assert.Nil(t, snap)
}

func TestFetchQuick_UpdatesDaemon(t *testing.T) {
	mon := New(&stubFinder{}, nil)
	prev := &Snapshot{
		FetchedAt:  time.Now().Add(-time.Second),
		TotalUsage: &claude.UsageSummary{Models: map[string]*claude.ModelUsage{}},
	}
	snap := mon.FetchQuick(context.Background(), prev)
	require.NotNil(t, snap)
	assert.True(t, snap.FetchedAt.After(prev.FetchedAt))
}
