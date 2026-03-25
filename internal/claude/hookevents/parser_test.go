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
