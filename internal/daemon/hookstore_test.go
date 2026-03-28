package daemon

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StephanSchmidt/human/internal/claude/hookevents"
)

func TestHookEventStore_AppendAndSnapshot(t *testing.T) {
	store := NewHookEventStore()

	store.Append(hookevents.Event{
		EventName: "UserPromptSubmit",
		SessionID: "s1",
		Cwd:       "/proj",
		Timestamp: time.Date(2026, 3, 25, 10, 0, 0, 0, time.UTC),
	})
	store.Append(hookevents.Event{
		EventName: "Stop",
		SessionID: "s1",
		Cwd:       "/proj",
		Timestamp: time.Date(2026, 3, 25, 10, 0, 5, 0, time.UTC),
	})

	snap := store.Snapshot()
	require.Len(t, snap, 1)
	assert.False(t, snap["s1"].IsWorking)
	assert.Equal(t, "/proj", snap["s1"].Cwd)
}

func TestHookEventStore_MultipleSessions(t *testing.T) {
	store := NewHookEventStore()

	store.Append(hookevents.Event{
		EventName: "UserPromptSubmit",
		SessionID: "s1",
		Timestamp: time.Now(),
	})
	store.Append(hookevents.Event{
		EventName: "PermissionRequest",
		SessionID: "s2",
		Timestamp: time.Now(),
	})

	snap := store.Snapshot()
	require.Len(t, snap, 2)
	assert.True(t, snap["s1"].IsWorking)
	assert.True(t, snap["s2"].IsBlocked)
}

func TestHookEventStore_RingBufferTrim(t *testing.T) {
	store := NewHookEventStore()

	// Fill beyond maxHookEvents.
	for i := 0; i < maxHookEvents+20; i++ {
		store.Append(hookevents.Event{
			EventName: "UserPromptSubmit",
			SessionID: "s1",
			Timestamp: time.Now(),
		})
	}

	store.mu.Lock()
	count := len(store.events)
	store.mu.Unlock()
	assert.Equal(t, maxHookEvents, count, "should trim to maxHookEvents")
}

func TestParseHookEventArgs(t *testing.T) {
	evt := ParseHookEventArgs([]string{"PermissionRequest", "s1", "/proj", ""})
	assert.Equal(t, "PermissionRequest", evt.EventName)
	assert.Equal(t, "s1", evt.SessionID)
	assert.Equal(t, "/proj", evt.Cwd)
	assert.False(t, evt.Timestamp.IsZero())
}

func TestParseHookEventArgs_WithNotificationType(t *testing.T) {
	evt := ParseHookEventArgs([]string{"Notification", "s1", "/proj", "idle_prompt"})
	assert.Equal(t, "Notification", evt.EventName)
	assert.Equal(t, "idle_prompt", evt.NotificationType)
}

func TestParseHookEventArgs_Empty(t *testing.T) {
	evt := ParseHookEventArgs(nil)
	assert.Empty(t, evt.EventName)
	assert.Empty(t, evt.SessionID)
}
