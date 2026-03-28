package hookevents

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_Empty(t *testing.T) {
	got := Parse(nil)
	assert.Empty(t, got)

	got = Parse([]byte{})
	assert.Empty(t, got)
}

func TestParse_SinglePromptSubmit(t *testing.T) {
	data := []byte(`{"event":"UserPromptSubmit","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:00Z"}`)
	got := Parse(data)
	require.Len(t, got, 1)
	snap := got["s1"]
	assert.True(t, snap.IsWorking)
	assert.Equal(t, "/proj", snap.Cwd)
	assert.Equal(t, "s1", snap.SessionID)
}

func TestParse_PromptThenStop(t *testing.T) {
	data := []byte(
		`{"event":"UserPromptSubmit","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:00Z"}` + "\n" +
			`{"event":"Stop","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:05Z"}`,
	)
	got := Parse(data)
	require.Len(t, got, 1)
	assert.False(t, got["s1"].IsWorking)
}

func TestParse_SubagentStartAndStop(t *testing.T) {
	data := []byte(
		`{"event":"SubagentStart","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:01Z"}` + "\n" +
			`{"event":"SubagentStop","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:03Z"}`,
	)
	got := Parse(data)
	require.Len(t, got, 1)
	assert.False(t, got["s1"].IsWorking)
}

func TestParse_MultipleSessions(t *testing.T) {
	data := []byte(
		`{"event":"UserPromptSubmit","session_id":"s1","cwd":"/a","timestamp":"2026-03-25T10:00:00Z"}` + "\n" +
			`{"event":"Stop","session_id":"s2","cwd":"/b","timestamp":"2026-03-25T10:00:01Z"}`,
	)
	got := Parse(data)
	require.Len(t, got, 2)
	assert.True(t, got["s1"].IsWorking)
	assert.False(t, got["s2"].IsWorking)
}

func TestParse_MalformedLinesSkipped(t *testing.T) {
	data := []byte(
		"not json\n" +
			`{"event":"UserPromptSubmit","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:00Z"}` + "\n" +
			"{broken",
	)
	got := Parse(data)
	require.Len(t, got, 1)
	assert.True(t, got["s1"].IsWorking)
}

func TestParse_EmptySessionIDSkipped(t *testing.T) {
	data := []byte(`{"event":"UserPromptSubmit","session_id":"","cwd":"/proj","timestamp":"2026-03-25T10:00:00Z"}`)
	got := Parse(data)
	assert.Empty(t, got)
}

func TestParse_CwdPreservedFromEarlierEvent(t *testing.T) {
	data := []byte(
		`{"event":"UserPromptSubmit","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:00Z"}` + "\n" +
			`{"event":"Stop","session_id":"s1","cwd":"","timestamp":"2026-03-25T10:00:05Z"}`,
	)
	got := Parse(data)
	assert.Equal(t, "/proj", got["s1"].Cwd)
}

func TestParse_LastEventAtUpdated(t *testing.T) {
	data := []byte(
		`{"event":"UserPromptSubmit","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:00Z"}` + "\n" +
			`{"event":"Stop","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:05Z"}`,
	)
	got := Parse(data)
	expected := time.Date(2026, 3, 25, 10, 0, 5, 0, time.UTC)
	assert.Equal(t, expected, got["s1"].LastEventAt)
}

func TestParse_EmptyLines(t *testing.T) {
	data := []byte(
		"\n" +
			`{"event":"UserPromptSubmit","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:00Z"}` + "\n" +
			"\n",
	)
	got := Parse(data)
	require.Len(t, got, 1)
	assert.True(t, got["s1"].IsWorking)
}

func TestParse_PermissionRequest(t *testing.T) {
	data := []byte(
		`{"event":"UserPromptSubmit","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:00Z"}` + "\n" +
			`{"event":"PermissionRequest","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:02Z"}`,
	)
	got := Parse(data)
	require.Len(t, got, 1)
	assert.False(t, got["s1"].IsWorking)
	assert.True(t, got["s1"].IsBlocked)
}

func TestParse_StopFailure(t *testing.T) {
	data := []byte(
		`{"event":"UserPromptSubmit","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:00Z"}` + "\n" +
			`{"event":"StopFailure","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:05Z"}`,
	)
	got := Parse(data)
	require.Len(t, got, 1)
	assert.False(t, got["s1"].IsWorking)
	assert.True(t, got["s1"].HasError)
}

func TestParse_StopFailureClearedByPrompt(t *testing.T) {
	data := []byte(
		`{"event":"StopFailure","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:00Z"}` + "\n" +
			`{"event":"UserPromptSubmit","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:05Z"}`,
	)
	got := Parse(data)
	require.Len(t, got, 1)
	assert.True(t, got["s1"].IsWorking)
	assert.False(t, got["s1"].HasError)
}

func TestParse_NotificationIdlePrompt(t *testing.T) {
	data := []byte(
		`{"event":"UserPromptSubmit","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:00Z"}` + "\n" +
			`{"event":"Notification","session_id":"s1","cwd":"/proj","notification_type":"idle_prompt","timestamp":"2026-03-25T10:00:05Z"}`,
	)
	got := Parse(data)
	require.Len(t, got, 1)
	assert.False(t, got["s1"].IsWorking)
	assert.False(t, got["s1"].IsBlocked)
}

func TestParse_NotificationPermissionPrompt(t *testing.T) {
	data := []byte(
		`{"event":"UserPromptSubmit","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:00Z"}` + "\n" +
			`{"event":"Notification","session_id":"s1","cwd":"/proj","notification_type":"permission_prompt","timestamp":"2026-03-25T10:00:02Z"}`,
	)
	got := Parse(data)
	require.Len(t, got, 1)
	assert.False(t, got["s1"].IsWorking)
	assert.True(t, got["s1"].IsBlocked)
}

func TestParse_NotificationUnknownTypeIgnored(t *testing.T) {
	data := []byte(
		`{"event":"UserPromptSubmit","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:00Z"}` + "\n" +
			`{"event":"Notification","session_id":"s1","cwd":"/proj","notification_type":"some_other","timestamp":"2026-03-25T10:00:02Z"}`,
	)
	got := Parse(data)
	require.Len(t, got, 1)
	assert.True(t, got["s1"].IsWorking, "unknown notification should not change working state")
}

func TestParse_SessionStartResetsState(t *testing.T) {
	data := []byte(
		`{"event":"StopFailure","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:00Z"}` + "\n" +
			`{"event":"SessionStart","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:05Z"}`,
	)
	got := Parse(data)
	require.Len(t, got, 1)
	assert.False(t, got["s1"].HasError)
	assert.False(t, got["s1"].IsWorking)
	assert.False(t, got["s1"].IsEnded)
}

func TestParse_SessionEnd(t *testing.T) {
	data := []byte(
		`{"event":"UserPromptSubmit","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:00Z"}` + "\n" +
			`{"event":"SessionEnd","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:05Z"}`,
	)
	got := Parse(data)
	require.Len(t, got, 1)
	assert.False(t, got["s1"].IsWorking)
	assert.True(t, got["s1"].IsEnded)
}

func TestParse_StopPreservesError(t *testing.T) {
	// StopFailure followed by Stop should keep HasError true.
	data := []byte(
		`{"event":"StopFailure","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:00Z"}` + "\n" +
			`{"event":"Stop","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:01Z"}`,
	)
	got := Parse(data)
	require.Len(t, got, 1)
	assert.True(t, got["s1"].HasError, "Stop should not clear HasError")
	assert.False(t, got["s1"].IsWorking)
}
