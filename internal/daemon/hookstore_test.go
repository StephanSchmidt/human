package daemon

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StephanSchmidt/human/internal/claude/hookevents"
	"github.com/StephanSchmidt/human/internal/claude/logparser"
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
	assert.Equal(t, logparser.StatusReady, snap["s1"].Status)
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
	assert.Equal(t, logparser.StatusWorking, snap["s1"].Status)
	assert.Equal(t, logparser.StatusBlocked, snap["s2"].Status)
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

func TestHookEventStore_Subscribe(t *testing.T) {
	store := NewHookEventStore()
	ch := store.Subscribe()

	store.Append(hookevents.Event{
		EventName: "UserPromptSubmit",
		SessionID: "s1",
		Timestamp: time.Now(),
	})

	select {
	case <-ch:
		// expected
	case <-time.After(time.Second):
		t.Fatal("subscriber should have been notified")
	}
}

func TestHookEventStore_SubscribeCoalesces(t *testing.T) {
	store := NewHookEventStore()
	ch := store.Subscribe()

	// Append twice before reading — only one notification should be buffered.
	store.Append(hookevents.Event{EventName: "Stop", SessionID: "s1", Timestamp: time.Now()})
	store.Append(hookevents.Event{EventName: "Stop", SessionID: "s1", Timestamp: time.Now()})

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("expected notification")
	}

	// Channel should be empty now.
	select {
	case <-ch:
		t.Fatal("expected no second notification (should coalesce)")
	default:
	}
}

func TestHookEventStore_Unsubscribe(t *testing.T) {
	store := NewHookEventStore()
	ch := store.Subscribe()
	store.Unsubscribe(ch)

	store.Append(hookevents.Event{EventName: "Stop", SessionID: "s1", Timestamp: time.Now()})

	select {
	case _, ok := <-ch:
		assert.False(t, ok, "channel should be closed after unsubscribe")
	default:
		// Channel closed and empty — also fine.
	}
}
