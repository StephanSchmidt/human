package logparser

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- test helpers ---

func ts(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, s)
	require.NoError(t, err)
	return parsed
}

func marshalLine(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func makeUserEntry(t *testing.T, sessionID, cwd, slug, text, timestamp string) []byte {
	t.Helper()
	return marshalLine(t, map[string]interface{}{
		"type":      "user",
		"sessionId": sessionID,
		"cwd":       cwd,
		"slug":      slug,
		"timestamp": timestamp,
		"message": map[string]interface{}{
			"role": "user",
			"content": []map[string]interface{}{
				{"type": "text", "text": text},
			},
		},
	})
}

func makeAssistantEntry(t *testing.T, sessionID, timestamp string, stopReason *string, content []map[string]interface{}) []byte {
	t.Helper()
	msg := map[string]interface{}{
		"role":    "assistant",
		"content": content,
	}
	if stopReason != nil {
		msg["stop_reason"] = *stopReason
	}
	return marshalLine(t, map[string]interface{}{
		"type":      "assistant",
		"sessionId": sessionID,
		"timestamp": timestamp,
		"message":   msg,
	})
}

func makeToolResultEntry(t *testing.T, sessionID, timestamp, toolUseID string, toolUseResult interface{}) []byte {
	t.Helper()
	entry := map[string]interface{}{
		"type":      "user",
		"sessionId": sessionID,
		"timestamp": timestamp,
		"message": map[string]interface{}{
			"role": "user",
			"content": []map[string]interface{}{
				{"type": "tool_result", "tool_use_id": toolUseID, "content": "ok"},
			},
		},
	}
	if toolUseResult != nil {
		entry["toolUseResult"] = toolUseResult
	}
	return marshalLine(t, entry)
}

func makeProgressEntry(t *testing.T, sessionID, timestamp, parentToolUseID, agentID string) []byte {
	t.Helper()
	return marshalLine(t, map[string]interface{}{
		"type":             "progress",
		"sessionId":        sessionID,
		"timestamp":        timestamp,
		"parentToolUseID":  parentToolUseID,
		"data": map[string]interface{}{
			"type":    "agent_progress",
			"agentId": agentID,
		},
	})
}

func strPtr(s string) *string { return &s }

func joinLines(lines ...[]byte) []byte {
	var parts []string
	for _, l := range lines {
		parts = append(parts, string(l))
	}
	return []byte(strings.Join(parts, "\n") + "\n")
}

// --- tests ---

func TestFileParser_EmptyFile(t *testing.T) {
	p := NewFileParser()
	state, err := p.UpdateBytes([]byte{})
	require.NoError(t, err)
	assert.Equal(t, "", state.SessionID)
	assert.True(t, state.StartedAt.IsZero())
	assert.False(t, state.IsWorking)
	assert.Empty(t, state.Subagents)
	assert.Empty(t, state.Tasks)
}

func TestFileParser_BasicSession(t *testing.T) {
	p := NewFileParser()
	data := joinLines(
		makeUserEntry(t, "sess-1", "/home/user/project", "cool-slug", "hello", "2026-03-25T10:00:00.000Z"),
	)

	state, err := p.UpdateBytes(data)
	require.NoError(t, err)
	assert.Equal(t, "sess-1", state.SessionID)
	assert.Equal(t, "/home/user/project", state.Cwd)
	assert.Equal(t, "cool-slug", state.Slug)
	assert.Equal(t, ts(t, "2026-03-25T10:00:00.000Z"), state.StartedAt)
	assert.True(t, state.IsWorking) // user entry means Claude will work
}

func TestFileParser_Incremental(t *testing.T) {
	p := NewFileParser()

	line1 := makeUserEntry(t, "sess-1", "/project", "", "hi", "2026-03-25T10:00:00.000Z")
	data1 := joinLines(line1)

	state, err := p.UpdateBytes(data1)
	require.NoError(t, err)
	assert.Equal(t, "sess-1", state.SessionID)
	assert.True(t, state.IsWorking)

	// Second batch: assistant with end_turn.
	endTurn := strPtr("end_turn")
	line2 := makeAssistantEntry(t, "sess-1", "2026-03-25T10:00:05.000Z", endTurn, []map[string]interface{}{
		{"type": "text", "text": "done"},
	})
	data2 := joinLines(line2)

	state, err = p.UpdateBytes(data2)
	require.NoError(t, err)
	assert.False(t, state.IsWorking) // end_turn → idle
}

func TestFileParser_SubagentLifecycle(t *testing.T) {
	p := NewFileParser()

	// Agent tool_use.
	toolUse := strPtr("tool_use")
	agentLine := makeAssistantEntry(t, "sess-1", "2026-03-25T10:00:00.000Z", toolUse, []map[string]interface{}{
		{
			"type": "tool_use",
			"id":   "toolu_agent1",
			"name": "Agent",
			"input": map[string]interface{}{
				"description":  "Research codebase",
				"subagent_type": "Explore",
				"prompt":       "Find all Go files",
			},
		},
	})

	// Progress entry with agentId.
	progressLine := makeProgressEntry(t, "sess-1", "2026-03-25T10:00:02.000Z", "toolu_agent1", "abc123")

	// Tool result completing the agent.
	resultLine := makeToolResultEntry(t, "sess-1", "2026-03-25T10:00:10.000Z", "toolu_agent1", map[string]interface{}{
		"status":          "completed",
		"agentId":         "abc123",
		"totalDurationMs": 10000,
	})

	data := joinLines(agentLine, progressLine, resultLine)
	state, err := p.UpdateBytes(data)
	require.NoError(t, err)

	require.Len(t, state.Subagents, 1)
	sa := state.Subagents[0]
	assert.Equal(t, "toolu_agent1", sa.ToolUseID)
	assert.Equal(t, "Research codebase", sa.Description)
	assert.Equal(t, "Explore", sa.SubagentType)
	assert.Equal(t, "abc123", sa.AgentID)
	assert.NotNil(t, sa.CompletedAt)
	assert.Equal(t, int64(10000), sa.DurationMs)
}

func TestFileParser_TaskLifecycle(t *testing.T) {
	p := NewFileParser()

	// TaskCreate tool_use.
	toolUse := strPtr("tool_use")
	createLine := makeAssistantEntry(t, "sess-1", "2026-03-25T10:00:00.000Z", toolUse, []map[string]interface{}{
		{
			"type": "tool_use",
			"id":   "toolu_task1",
			"name": "TaskCreate",
			"input": map[string]interface{}{
				"subject":     "Fix the bug",
				"description": "There is a bug to fix",
			},
		},
	})

	// TaskCreate result with task ID.
	createResult := makeToolResultEntry(t, "sess-1", "2026-03-25T10:00:01.000Z", "toolu_task1", map[string]interface{}{
		"task": map[string]interface{}{
			"id":      "1",
			"subject": "Fix the bug",
		},
	})

	// TaskUpdate to in_progress.
	updateLine := makeAssistantEntry(t, "sess-1", "2026-03-25T10:00:05.000Z", toolUse, []map[string]interface{}{
		{
			"type": "tool_use",
			"id":   "toolu_update1",
			"name": "TaskUpdate",
			"input": map[string]interface{}{
				"taskId": "1",
				"status": "in_progress",
			},
		},
	})

	// TaskUpdate to completed.
	completeLine := makeAssistantEntry(t, "sess-1", "2026-03-25T10:00:20.000Z", toolUse, []map[string]interface{}{
		{
			"type": "tool_use",
			"id":   "toolu_update2",
			"name": "TaskUpdate",
			"input": map[string]interface{}{
				"taskId": "1",
				"status": "completed",
			},
		},
	})

	data := joinLines(createLine, createResult, updateLine, completeLine)
	state, err := p.UpdateBytes(data)
	require.NoError(t, err)

	require.Len(t, state.Tasks, 1)
	task := state.Tasks[0]
	assert.Equal(t, "1", task.TaskID)
	assert.Equal(t, "Fix the bug", task.Subject)
	assert.Equal(t, "completed", task.Status)
}

func TestFileParser_StatusTransitions(t *testing.T) {
	p := NewFileParser()

	// User prompt → working.
	line1 := makeUserEntry(t, "sess-1", "/project", "", "do stuff", "2026-03-25T10:00:00.000Z")
	data1 := joinLines(line1)
	state, _ := p.UpdateBytes(data1)
	assert.True(t, state.IsWorking, "user prompt should set IsWorking")

	// Assistant with tool_use stop_reason → still working.
	toolUse := strPtr("tool_use")
	line2 := makeAssistantEntry(t, "sess-1", "2026-03-25T10:00:02.000Z", toolUse, []map[string]interface{}{
		{"type": "text", "text": "let me check"},
	})
	data2 := joinLines(line2)
	state, _ = p.UpdateBytes(data2)
	assert.True(t, state.IsWorking, "tool_use stop_reason should keep IsWorking")

	// Assistant with end_turn → idle.
	endTurn := strPtr("end_turn")
	line3 := makeAssistantEntry(t, "sess-1", "2026-03-25T10:00:10.000Z", endTurn, []map[string]interface{}{
		{"type": "text", "text": "all done"},
	})
	data3 := joinLines(line3)
	state, _ = p.UpdateBytes(data3)
	assert.False(t, state.IsWorking, "end_turn should clear IsWorking")

	// New user prompt → working again.
	line4 := makeUserEntry(t, "sess-1", "/project", "", "more work", "2026-03-25T10:01:00.000Z")
	data4 := joinLines(line4)
	state, _ = p.UpdateBytes(data4)
	assert.True(t, state.IsWorking, "new user prompt should set IsWorking again")
}

func TestFileParser_MultipleSubagents(t *testing.T) {
	p := NewFileParser()

	toolUse := strPtr("tool_use")

	// Two agents launched in same assistant turn.
	agentLine := makeAssistantEntry(t, "sess-1", "2026-03-25T10:00:00.000Z", toolUse, []map[string]interface{}{
		{
			"type": "tool_use", "id": "toolu_a1", "name": "Agent",
			"input": map[string]interface{}{"description": "Agent One", "subagent_type": "Explore"},
		},
		{
			"type": "tool_use", "id": "toolu_a2", "name": "Agent",
			"input": map[string]interface{}{"description": "Agent Two", "subagent_type": "Plan"},
		},
	})

	// Only first completes.
	result1 := makeToolResultEntry(t, "sess-1", "2026-03-25T10:00:05.000Z", "toolu_a1", map[string]interface{}{
		"status":          "completed",
		"agentId":         "id1",
		"totalDurationMs": 5000,
	})

	data := joinLines(agentLine, result1)
	state, err := p.UpdateBytes(data)
	require.NoError(t, err)

	require.Len(t, state.Subagents, 2)

	// First agent completed.
	assert.Equal(t, "Agent One", state.Subagents[0].Description)
	assert.NotNil(t, state.Subagents[0].CompletedAt)

	// Second agent still running.
	assert.Equal(t, "Agent Two", state.Subagents[1].Description)
	assert.Nil(t, state.Subagents[1].CompletedAt)
}

func TestFileParser_MalformedLines(t *testing.T) {
	p := NewFileParser()

	data := joinLines(
		[]byte(`{invalid json`),
		[]byte(``),
		makeUserEntry(t, "sess-1", "/project", "", "hello", "2026-03-25T10:00:00.000Z"),
		[]byte(`{"type": "unknown_type"}`),
	)

	state, err := p.UpdateBytes(data)
	require.NoError(t, err)
	assert.Equal(t, "sess-1", state.SessionID) // valid line was processed
}

func TestFileParser_PartialLine(t *testing.T) {
	p := NewFileParser()

	completeLine := string(makeUserEntry(t, "sess-1", "/project", "", "hi", "2026-03-25T10:00:00.000Z"))
	partial := `{"type": "assistant", "sessionId": "sess-1"`

	// Data with complete line + partial line (no trailing newline).
	data := []byte(completeLine + "\n" + partial)

	consumed := p.parseBytes(data)

	// Should only consume the complete line (including its newline).
	assert.Equal(t, len(completeLine)+1, consumed)
	assert.Equal(t, "sess-1", p.state.SessionID)
}

func TestFileParser_Update_WithFileReader(t *testing.T) {
	p := NewFileParser()

	data := joinLines(
		makeUserEntry(t, "sess-1", "/project", "my-slug", "hello", "2026-03-25T10:00:00.000Z"),
	)

	reader := &memoryReader{data: data}
	state, err := p.Update(reader, "/fake/path")
	require.NoError(t, err)
	assert.Equal(t, "sess-1", state.SessionID)
	assert.Equal(t, "my-slug", state.Slug)
}

// memoryReader implements FileReader for testing.
type memoryReader struct {
	data []byte
}

func (r *memoryReader) ReadFrom(_ string, offset int64) ([]byte, int64, error) {
	if offset >= int64(len(r.data)) {
		return nil, offset, nil
	}
	d := r.data[offset:]
	return d, int64(len(r.data)), nil
}
