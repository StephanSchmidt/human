package daemon

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPendingConfirmStore_AddAndSnapshot(t *testing.T) {
	store := NewPendingConfirmStore()

	pc := &PendingConfirmation{
		ID:        "op-1",
		Operation: "DeleteIssue",
		Tracker:   "jira",
		Key:       "KAN-1",
		Prompt:    "Delete KAN-1?",
		CreatedAt: time.Now(),
		Decision:  make(chan bool, 1),
	}
	store.Add(pc)

	snap := store.Snapshot()
	require.Len(t, snap, 1)
	assert.Equal(t, "op-1", snap[0].ID)
	assert.Equal(t, "DeleteIssue", snap[0].Operation)
	assert.Equal(t, "jira", snap[0].Tracker)
	assert.Equal(t, "KAN-1", snap[0].Key)
	assert.Equal(t, "Delete KAN-1?", snap[0].Prompt)
}

func TestPendingConfirmStore_ResolveApproved(t *testing.T) {
	store := NewPendingConfirmStore()

	pc := &PendingConfirmation{
		ID:       "op-2",
		Decision: make(chan bool, 1),
	}
	store.Add(pc)

	err := store.Resolve("op-2", true, 0)
	require.NoError(t, err)

	decision := <-pc.Decision
	assert.True(t, decision)
	assert.Equal(t, 0, store.Len())
}

func TestPendingConfirmStore_ResolveRejected(t *testing.T) {
	store := NewPendingConfirmStore()

	pc := &PendingConfirmation{
		ID:       "op-3",
		Decision: make(chan bool, 1),
	}
	store.Add(pc)

	err := store.Resolve("op-3", false, 0)
	require.NoError(t, err)

	decision := <-pc.Decision
	assert.False(t, decision)
	assert.Equal(t, 0, store.Len())
}

func TestPendingConfirmStore_ResolveNotFound(t *testing.T) {
	store := NewPendingConfirmStore()

	err := store.Resolve("nonexistent", true, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent")
}

func TestPendingConfirmStore_Cleanup(t *testing.T) {
	store := NewPendingConfirmStore()

	old := &PendingConfirmation{
		ID:        "old-op",
		CreatedAt: time.Now().Add(-10 * time.Minute),
		Decision:  make(chan bool, 1),
	}
	recent := &PendingConfirmation{
		ID:        "recent-op",
		CreatedAt: time.Now(),
		Decision:  make(chan bool, 1),
	}
	store.Add(old)
	store.Add(recent)

	store.Cleanup(5 * time.Minute)

	assert.Equal(t, 1, store.Len())

	snap := store.Snapshot()
	require.Len(t, snap, 1)
	assert.Equal(t, "recent-op", snap[0].ID)

	// The expired op should have been rejected.
	decision := <-old.Decision
	assert.False(t, decision)
}

func TestPendingConfirmStore_EmptySnapshot(t *testing.T) {
	store := NewPendingConfirmStore()

	snap := store.Snapshot()
	assert.Empty(t, snap)
	assert.NotNil(t, snap)
}

func TestPendingConfirmStore_Len(t *testing.T) {
	store := NewPendingConfirmStore()
	assert.Equal(t, 0, store.Len())

	store.Add(&PendingConfirmation{ID: "a", Decision: make(chan bool, 1)})
	assert.Equal(t, 1, store.Len())

	store.Add(&PendingConfirmation{ID: "b", Decision: make(chan bool, 1)})
	assert.Equal(t, 2, store.Len())

	_ = store.Resolve("a", true, 0)
	assert.Equal(t, 1, store.Len())
}

func TestPendingConfirmStore_SelfApprovalRejected(t *testing.T) {
	store := NewPendingConfirmStore()

	pc := &PendingConfirmation{
		ID:        "op-self",
		ClientPID: 12345,
		Decision:  make(chan bool, 1),
	}
	store.Add(pc)

	// Same PID as requester → rejected.
	err := store.Resolve("op-self", true, 12345)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "self-approval not allowed")
	assert.Equal(t, 1, store.Len()) // still pending

	// Different PID → allowed.
	err = store.Resolve("op-self", true, 99999)
	require.NoError(t, err)
	assert.Equal(t, 0, store.Len())
}

func TestPendingConfirmStore_ZeroPIDAlwaysAllowed(t *testing.T) {
	store := NewPendingConfirmStore()

	pc := &PendingConfirmation{
		ID:        "op-zero",
		ClientPID: 0,
		Decision:  make(chan bool, 1),
	}
	store.Add(pc)

	// PID 0 approver is always allowed (system/timeout/TUI).
	err := store.Resolve("op-zero", true, 0)
	require.NoError(t, err)
}
