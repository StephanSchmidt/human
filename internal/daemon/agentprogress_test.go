package daemon

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gethuman-sh/human/internal/claude/hookevents"
)

func evt(name, agent, tool string, at time.Time) hookevents.Event {
	return hookevents.Event{EventName: name, AgentName: agent, ToolName: tool, Timestamp: at}
}

// A tool call in flight is not idleness: no event can arrive until the command
// returns, so the agent gets the long budget.
func TestAgentProgress_InsideToolUsesTheLongBudget(t *testing.T) {
	progress := map[string]AgentProgress{}
	start := time.Unix(1000, 0)
	trackProgress(progress, evt("PreToolUse", "board-SC-1-implementation", "Bash", start))

	p := progress["board-SC-1-implementation"]
	require.True(t, p.InsideTool)
	require.Equal(t, WorkingIdleGrace, p.IdleBudget())

	// Ten minutes into a test suite is normal work, not a hang.
	stalled, _ := p.Stalled(start.Add(10 * time.Minute))
	require.False(t, stalled)

	stalled, idle := p.Stalled(start.Add(WorkingIdleGrace + time.Minute))
	require.True(t, stalled, "past the tool budget it is hung")
	require.Greater(t, idle, WorkingIdleGrace)
}

// Between tool calls a model acts within seconds, so silence is abnormal
// fast — but only when the daemon can actually see that nothing is open
// (SC-3853).
func TestAgentProgress_ThinkingUsesTheShortBudget(t *testing.T) {
	progress := map[string]AgentProgress{}
	start := time.Unix(1000, 0)
	trackProgress(progress, evt("PreToolUse", "a", "Bash", start))
	trackProgress(progress, evt("PostToolUse", "a", "Bash", start.Add(time.Minute)))

	p := progress["a"]
	p.ModelRequest = ModelRequestNone // the proxy could answer: nothing is open
	require.False(t, p.InsideTool, "the tool finished")
	require.Equal(t, IdleGrace, p.IdleBudget())

	stalled, _ := p.Stalled(start.Add(time.Minute + IdleGrace + time.Second))
	require.True(t, stalled)
}

// The whole point: a long stage that keeps working never looks hung, however
// long it runs. Wall-clock duration must not enter the decision.
func TestAgentProgress_LongRunningButActiveIsNeverStalled(t *testing.T) {
	progress := map[string]AgentProgress{}
	now := time.Unix(1000, 0)

	// Three hours of steady tool calls.
	for i := 0; i < 180; i++ {
		now = now.Add(time.Minute)
		trackProgress(progress, evt("PreToolUse", "a", "Bash", now))
		trackProgress(progress, evt("PostToolUse", "a", "Bash", now.Add(2*time.Second)))
	}

	p := progress["a"]
	stalled, _ := p.Stalled(now.Add(30 * time.Second))
	require.False(t, stalled, "a working agent is never stalled regardless of total runtime")
}

// SC-3074: a run waiting on the model — no hook event, between tool calls, but
// with a request sent to the model that has not come back — is WORKING, not hung.
func TestAgentProgress_OutstandingModelRequestIsNeverStalled(t *testing.T) {
	now := time.Unix(100_000, 0)
	p := AgentProgress{
		LastEventAt:  now.Add(-4 * time.Minute),
		InsideTool:   false,
		ModelRequest: ModelRequestOpen,
	}
	require.Equal(t, WorkingIdleGrace, p.IdleBudget())
	stalled, _ := p.Stalled(now)
	require.False(t, stalled, "a run waiting on the model is working, not hung")
}

// With NEITHER a hook event NOR outstanding work of any kind for longer than
// IdleGrace, the agent really has gone silent and must still be caught — the
// short budget still bounds total silence. ModelRequest is explicitly none
// here: the proxy could answer and did, so this pins genuine idleness rather
// than the unknown case below.
func TestAgentProgress_NoOutstandingWorkIsStalled(t *testing.T) {
	now := time.Unix(100_000, 0)
	p := AgentProgress{
		LastEventAt:  now.Add(-4 * time.Minute),
		ModelRequest: ModelRequestNone,
	}

	stalled, idle := p.Stalled(now)
	require.True(t, stalled, "total silence past the budget is still a hang")
	require.Greater(t, idle, IdleGrace)
}

// The zero value is UNKNOWN, not none: absent evidence about the model-request
// signal must never be read as "nothing is open", or the daemon kills live
// work on a bookkeeping failure — the one direction this must never fail in
// (SC-3853).
func TestAgentProgress_UnknownModelRequestUsesTheLongBudget(t *testing.T) {
	now := time.Unix(100_000, 0)
	p := AgentProgress{LastEventAt: now.Add(-4 * time.Minute)}

	require.Equal(t, ModelRequestUnknown, p.ModelRequest, "the zero value is unknown")
	require.Equal(t, WorkingIdleGrace, p.IdleBudget())
	stalled, _ := p.Stalled(now)
	require.False(t, stalled, "unknown gets the generous budget, not the short one")
}

// String renders all three states plus an out-of-range value, which
// //exhaustive:enforce's default case must still answer safely.
func TestAgentProgress_ModelRequestStateString(t *testing.T) {
	require.Equal(t, "unknown", ModelRequestUnknown.String())
	require.Equal(t, "none", ModelRequestNone.String())
	require.Equal(t, "open", ModelRequestOpen.String())
	require.Equal(t, "unknown", ModelRequestState(9).String())
}

// An agent waiting on a permission prompt needs an answer, not a relaunch —
// retrying it would discard the question.
func TestAgentProgress_BlockedIsNotStalled(t *testing.T) {
	progress := map[string]AgentProgress{}
	start := time.Unix(1000, 0)
	trackProgress(progress, evt("Notification", "a", "", start))

	p := progress["a"]
	require.True(t, p.Blocked)

	stalled, _ := p.Stalled(start.Add(time.Hour))
	require.False(t, stalled, "blocked on a human is not a hang")
}

func TestAgentProgress_BlockedClearsOnNextAction(t *testing.T) {
	progress := map[string]AgentProgress{}
	start := time.Unix(1000, 0)
	trackProgress(progress, evt("Notification", "a", "", start))
	trackProgress(progress, evt("PostToolUse", "a", "Bash", start.Add(time.Minute)))

	require.False(t, progress["a"].Blocked, "the human answered and the agent moved on")
}

// A finished agent must not linger as a hang candidate.
func TestAgentProgress_TerminalEventsDropTheAgent(t *testing.T) {
	for _, ending := range []string{"Stop", "SessionEnd", "StopFailure"} {
		progress := map[string]AgentProgress{}
		trackProgress(progress, evt("PreToolUse", "a", "Bash", time.Unix(1000, 0)))
		trackProgress(progress, evt(ending, "a", "", time.Unix(1001, 0)))
		require.NotContains(t, progress, "a", "%s must clear the agent", ending)
	}
}

func TestAgentProgress_IgnoresEventsWithNoAgent(t *testing.T) {
	progress := map[string]AgentProgress{}
	trackProgress(progress, evt("PreToolUse", "", "Bash", time.Unix(1000, 0)))
	require.Empty(t, progress)
}

// The store must keep progress outside the event ring: a per-session cap of 200
// evicts events, and a quiet-but-working agent whose last event aged out would
// be misread as hung — the one direction this must never fail in.
func TestHookEventStore_ProgressSurvivesRingEviction(t *testing.T) {
	s := NewHookEventStore()
	at := time.Unix(1000, 0)
	s.Append(evt("PreToolUse", "board-SC-1-implementation", "Bash", at))

	// Flood the same session well past its cap.
	for i := 0; i < maxHookEventsPerSession+50; i++ {
		s.Append(hookevents.Event{EventName: "PostToolUse", SessionID: "s1", Timestamp: at})
	}

	p, ok := s.AgentProgress("board-SC-1-implementation")
	require.True(t, ok, "progress must outlive ring eviction")
	require.True(t, p.InsideTool)
	require.Equal(t, at, p.LastEventAt)
}

func TestHookEventStore_UnknownAgentIsNotKnown(t *testing.T) {
	s := NewHookEventStore()
	_, ok := s.AgentProgress("board-SC-9-planning")
	require.False(t, ok)
}

// The SC-4900 regression, replayed from board-SC-4820-implementation run
// 97ffc408: a subagent's tool events arrive under the PARENT's agent name, so
// the subagent's last PostToolUse used to clear InsideTool and drop a run that
// was waiting on a dispatch to the 3-minute budget. It was killed at 3m4s,
// three times, always at the planning subagent's last tool call.
func TestAgentProgress_WaitingOnASubagentIsNotIdle(t *testing.T) {
	progress := map[string]AgentProgress{}
	start := time.Unix(1000, 0)
	const agent = "board-SC-4820-implementation"

	trackProgress(progress, evt("PreToolUse", agent, "Agent", start))
	trackProgress(progress, evt(hookevents.EventSubagentStart, agent, "", start.Add(time.Second)))
	// The subagent works for ten minutes under its parent's name.
	last := start.Add(10 * time.Minute)
	trackProgress(progress, evt("PreToolUse", agent, "Bash", last.Add(-time.Second)))
	trackProgress(progress, evt("PostToolUse", agent, "Bash", last))

	p := progress[agent]
	p.ModelRequest = ModelRequestNone // the proxy answered: nothing open right now
	require.False(t, p.InsideTool, "the subagent's own tool call did finish")
	require.Equal(t, 1, p.Subagents, "but the dispatch it belongs to has not")
	require.Equal(t, WorkingIdleGrace, p.IdleBudget())

	stalled, _ := p.Stalled(last.Add(3*time.Minute + 4*time.Second))
	require.False(t, stalled, "the exact idle that killed the SC-4820 runs")

	stalled, _ = p.Stalled(last.Add(WorkingIdleGrace + time.Minute))
	require.True(t, stalled, "a dispatch that never returns is still a hang, later")
}

// The dispatch brackets close: once the subagent returns, the parent is
// thinking between tool calls again and the short budget is the honest one.
func TestAgentProgress_ReturnedSubagentRestoresTheShortBudget(t *testing.T) {
	progress := map[string]AgentProgress{}
	start := time.Unix(1000, 0)
	const agent = "a"

	trackProgress(progress, evt("PreToolUse", agent, "Agent", start))
	trackProgress(progress, evt(hookevents.EventSubagentStart, agent, "", start.Add(time.Second)))
	trackProgress(progress, evt(hookevents.EventSubagentStop, agent, "", start.Add(2*time.Minute)))
	trackProgress(progress, evt("PostToolUse", agent, "Agent", start.Add(2*time.Minute+time.Second)))

	p := progress[agent]
	p.ModelRequest = ModelRequestNone
	require.Equal(t, 0, p.Subagents)
	require.Equal(t, IdleGrace, p.IdleBudget())
}

// A subagent may dispatch its own, and every bracket arrives under the same
// name — so the count is a depth, not a flag. An inner return must not report
// the outer dispatch as finished.
func TestAgentProgress_NestedSubagentsCountAsDepth(t *testing.T) {
	progress := map[string]AgentProgress{}
	start := time.Unix(1000, 0)
	const agent = "a"

	trackProgress(progress, evt(hookevents.EventSubagentStart, agent, "", start))
	trackProgress(progress, evt(hookevents.EventSubagentStart, agent, "", start.Add(time.Second)))
	trackProgress(progress, evt(hookevents.EventSubagentStop, agent, "", start.Add(2*time.Second)))

	p := progress[agent]
	p.ModelRequest = ModelRequestNone
	require.Equal(t, 1, p.Subagents, "the outer dispatch is still outstanding")
	require.Equal(t, WorkingIdleGrace, p.IdleBudget())
}

// A Stop that never arrives must not push the count below zero, where a later
// dispatch would be cancelled by a bracket that had already closed.
func TestAgentProgress_UnpairedSubagentStopFloorsAtZero(t *testing.T) {
	progress := map[string]AgentProgress{}
	start := time.Unix(1000, 0)
	const agent = "a"

	trackProgress(progress, evt(hookevents.EventSubagentStop, agent, "", start))
	require.Equal(t, 0, progress[agent].Subagents)

	trackProgress(progress, evt(hookevents.EventSubagentStart, agent, "", start.Add(time.Second)))
	p := progress[agent]
	p.ModelRequest = ModelRequestNone
	require.Equal(t, 1, p.Subagents, "the next dispatch still counts")
	require.Equal(t, WorkingIdleGrace, p.IdleBudget())
}

// A run that ends drops its entry, so a dispatch left outstanding by a kill
// cannot outlive the run and hold the generous budget open for the relaunch.
func TestAgentProgress_RunEndClearsAnOutstandingSubagent(t *testing.T) {
	progress := map[string]AgentProgress{}
	start := time.Unix(1000, 0)
	const agent = "a"

	trackProgress(progress, evt(hookevents.EventSubagentStart, agent, "", start))
	trackProgress(progress, evt(hookevents.EventStopFailure, agent, "", start.Add(time.Minute)))

	_, ok := progress[agent]
	require.False(t, ok)
}
