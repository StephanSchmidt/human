package daemon

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"

	"github.com/gethuman-sh/human/internal/claude/hookevents"
	"github.com/gethuman-sh/human/internal/tracker"
)

// TestRunAuxFailureWatch_FailedIdeaDraftLeavesAMarker is the ticket's third
// regression: a failed idea-draft run must leave a failure marker on the
// ticket. Before this watcher existed, a run that died on its first API call
// never got a turn to report and nothing was ever posted.
func TestRunAuxFailureWatch_FailedIdeaDraftLeavesAMarker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := NewHookEventStore()
		c := &syncCommenter{addCh: make(chan string, 4)}
		commenterFor := func() (tracker.Commenter, error) { return c, nil }
		t0 := time.Now()
		lookup := func(string) AuxRunRecord { return AuxRunRecord{Known: true, ProcessFailed: true, StartedAt: t0} }

		go RunAuxFailureWatch(t.Context(), store, AuxFailureDeps{CommenterFor: commenterFor, Lookup: lookup, Logger: zerolog.Nop()})
		time.Sleep(50 * time.Millisecond)

		store.Append(hookevents.Event{EventName: "StopFailure", AgentName: "idea-draft-SC-1", Timestamp: time.Now()})

		select {
		case body := <-c.addCh:
			assert.Contains(t, body, IdeaDraftFailedHeader)
			assert.Contains(t, body, "reason:")
		case <-time.After(2 * time.Second):
			t.Fatal("expected a failure marker to be posted")
		}
	})
}

// TestRunAuxFailureWatch_SuccessfulDraftReapedLater_PostsNothing: the drafter
// finished and posted its provenance record; a later reap must not be
// misread as a failure of THIS run.
func TestRunAuxFailureWatch_SuccessfulDraftReapedLater_PostsNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := NewHookEventStore()
		t0 := time.Now()
		c := &syncCommenter{
			comments: []tracker.Comment{cmt(IdeaDraftHeader+"\nauthor: idea-draft-SC-1", t0.Add(time.Minute))},
			addCh:    make(chan string, 4),
		}
		commenterFor := func() (tracker.Commenter, error) { return c, nil }
		lookup := func(string) AuxRunRecord { return AuxRunRecord{Known: true, ProcessFailed: true, StartedAt: t0} }

		go RunAuxFailureWatch(t.Context(), store, AuxFailureDeps{CommenterFor: commenterFor, Lookup: lookup, Logger: zerolog.Nop()})
		time.Sleep(50 * time.Millisecond)

		store.Append(hookevents.Event{EventName: "StopFailure", AgentName: "idea-draft-SC-1", Timestamp: time.Now()})

		select {
		case body := <-c.addCh:
			t.Fatalf("must not post for a run that already recorded its own success, got: %q", body)
		case <-time.After(300 * time.Millisecond):
		}
	})
}

// TestRunAuxFailureWatch_CleanExitWithNoMarkerPostsNothing: the drafter's
// legitimate `current` no-op (verdict already current, nothing to write) is a
// clean process end with no marker at all — that must never read as a failure.
func TestRunAuxFailureWatch_CleanExitWithNoMarkerPostsNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := NewHookEventStore()
		c := &syncCommenter{addCh: make(chan string, 4)}
		commenterFor := func() (tracker.Commenter, error) { return c, nil }
		lookup := func(string) AuxRunRecord { return AuxRunRecord{Known: true, ProcessFailed: false} }

		go RunAuxFailureWatch(t.Context(), store, AuxFailureDeps{CommenterFor: commenterFor, Lookup: lookup, Logger: zerolog.Nop()})
		time.Sleep(50 * time.Millisecond)

		store.Append(hookevents.Event{EventName: "Stop", AgentName: "idea-draft-SC-1", Timestamp: time.Now()})

		select {
		case body := <-c.addCh:
			t.Fatalf("a clean process end must post nothing, got: %q", body)
		case <-time.After(300 * time.Millisecond):
		}
	})
}

// TestRunAuxFailureWatch_NonZeroExitOnACleanEventStillPosts: the run's own
// record beats the event — a process that exited non-zero is a failure even
// when the hook event itself carries a clean "Stop".
func TestRunAuxFailureWatch_NonZeroExitOnACleanEventStillPosts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := NewHookEventStore()
		c := &syncCommenter{addCh: make(chan string, 4)}
		commenterFor := func() (tracker.Commenter, error) { return c, nil }
		lookup := func(string) AuxRunRecord {
			return AuxRunRecord{Known: true, ProcessFailed: true, StartedAt: time.Now()}
		}

		go RunAuxFailureWatch(t.Context(), store, AuxFailureDeps{CommenterFor: commenterFor, Lookup: lookup, Logger: zerolog.Nop()})
		time.Sleep(50 * time.Millisecond)

		store.Append(hookevents.Event{EventName: "Stop", AgentName: "idea-draft-SC-1", Timestamp: time.Now()})

		select {
		case body := <-c.addCh:
			assert.Contains(t, body, IdeaDraftFailedHeader)
		case <-time.After(2 * time.Second):
			t.Fatal("expected a failure marker even on a clean-looking event, because the run's own record says it failed")
		}
	})
}

// TestRunAuxFailureWatch_DuplicateExitPostsOnce: two exit events for the same
// run must not double-post — the second sees the first's own marker on the
// thread via syncCommenter's addCh->comments loop-back is NOT modeled, so this
// asserts via a commenter that records added bodies into its own thread.
func TestRunAuxFailureWatch_DuplicateExitPostsOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := NewHookEventStore()
		c := newLoopbackCommenter()
		commenterFor := func() (tracker.Commenter, error) { return c, nil }
		t0 := time.Now()
		lookup := func(string) AuxRunRecord { return AuxRunRecord{Known: true, ProcessFailed: true, StartedAt: t0} }

		go RunAuxFailureWatch(t.Context(), store, AuxFailureDeps{CommenterFor: commenterFor, Lookup: lookup, Logger: zerolog.Nop()})
		time.Sleep(50 * time.Millisecond)

		store.Append(hookevents.Event{EventName: "StopFailure", AgentName: "idea-draft-SC-1", Timestamp: time.Now()})
		time.Sleep(200 * time.Millisecond)
		store.Append(hookevents.Event{EventName: "StopFailure", AgentName: "idea-draft-SC-1", Timestamp: time.Now()})
		time.Sleep(200 * time.Millisecond)

		synctest.Wait()
		assert.Equal(t, 1, c.addedCount(), "exactly one failure marker for two exit events of the same run")
	})
}

// TestRunAuxFailureWatch_RelatePostsIncomplete: a relate run gets the existing
// `related incomplete` head, not a new marker type.
func TestRunAuxFailureWatch_RelatePostsIncomplete(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := NewHookEventStore()
		c := &syncCommenter{addCh: make(chan string, 4)}
		commenterFor := func() (tracker.Commenter, error) { return c, nil }
		lookup := func(string) AuxRunRecord {
			return AuxRunRecord{Known: true, ProcessFailed: true, StartedAt: time.Now()}
		}

		go RunAuxFailureWatch(t.Context(), store, AuxFailureDeps{CommenterFor: commenterFor, Lookup: lookup, Logger: zerolog.Nop()})
		time.Sleep(50 * time.Millisecond)

		store.Append(hookevents.Event{EventName: "StopFailure", AgentName: "relate-SC-1", Timestamp: time.Now()})

		select {
		case body := <-c.addCh:
			assert.Contains(t, body, RelatedHeader+" incomplete")
		case <-time.After(2 * time.Second):
			t.Fatal("expected a related-incomplete record")
		}
	})
}

// TestRunAuxFailureWatch_IgnoresBoardAgents: the aux watcher never touches a
// board stage agent's exit — that is RunBoardFailureWatch's job.
func TestRunAuxFailureWatch_IgnoresBoardAgents(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := NewHookEventStore()
		c := &syncCommenter{addCh: make(chan string, 4)}
		commenterFor := func() (tracker.Commenter, error) { return c, nil }
		lookup := func(string) AuxRunRecord { return AuxRunRecord{Known: true, ProcessFailed: true} }

		go RunAuxFailureWatch(t.Context(), store, AuxFailureDeps{CommenterFor: commenterFor, Lookup: lookup, Logger: zerolog.Nop()})
		time.Sleep(50 * time.Millisecond)

		store.Append(hookevents.Event{EventName: "StopFailure", AgentName: "board-SC-1-implementation", Timestamp: time.Now()})

		select {
		case body := <-c.addCh:
			t.Fatalf("must not post for a board stage agent, got: %q", body)
		case <-time.After(300 * time.Millisecond):
		}
	})
}

// loopbackCommenter feeds every AddComment back into its own ListComments
// thread, so a second exit event for the same run sees the first post — the
// scenario duplicate-exit dedupe must handle.
type loopbackCommenter struct {
	c *syncCommenter
}

func newLoopbackCommenter() *loopbackCommenter {
	return &loopbackCommenter{c: &syncCommenter{addCh: make(chan string, 8)}}
}

func (l *loopbackCommenter) ListComments(ctx context.Context, key string) ([]tracker.Comment, error) {
	return l.c.ListComments(ctx, key)
}

func (l *loopbackCommenter) AddComment(ctx context.Context, key, body string) (*tracker.Comment, error) {
	c, err := l.c.AddComment(ctx, key, body)
	if err != nil {
		return nil, err
	}
	l.c.mu.Lock()
	l.c.comments = append(l.c.comments, *c)
	l.c.mu.Unlock()
	return c, nil
}

func (l *loopbackCommenter) addedCount() int {
	l.c.mu.Lock()
	defer l.c.mu.Unlock()
	return len(l.c.added)
}
