package daemon

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/gethuman-sh/human/internal/tracker"
)

// SC-365 regression: the board detail panel must surface comment-sourced
// review findings, failure reason, and fix summary. These tests exercise the
// pure parsers and the extras builder that the daemon getter now threads onto
// the wire. They fail before the fix because the symbols under test do not yet
// exist (a non-compiling new test in the package is the required red state).

func TestReviewFindings_extractsFullBody(t *testing.T) {
	comments := []tracker.Comment{
		{
			Body:    ReviewCompleteHeader + "\nverdict: pass\n\n## Findings\nNil deref in foo",
			Created: time.Now(),
		},
	}
	got := reviewFindings(comments)
	assert.Contains(t, got, "## Findings")
	assert.Contains(t, got, "Nil deref in foo")
	assert.NotContains(t, got, ReviewCompleteHeader)
}

func TestReviewFindings_latestWins(t *testing.T) {
	older := time.Now().Add(-time.Hour)
	newer := time.Now()
	comments := []tracker.Comment{
		{Body: ReviewCompleteHeader + "\nold findings", Created: older},
		{Body: ReviewCompleteHeader + "\nnew findings", Created: newer},
	}
	assert.Equal(t, "new findings", reviewFindings(comments))
}

func TestReviewFindings_absent(t *testing.T) {
	comments := []tracker.Comment{{Body: "just a regular comment", Created: time.Now()}}
	assert.Equal(t, "", reviewFindings(comments))
}

func TestFixSummary_extractsBody(t *testing.T) {
	comments := []tracker.Comment{
		{Body: FixSummaryHeader + "\n## What happened\nfixed the nil deref", Created: time.Now()},
	}
	got := fixSummary(comments)
	assert.Contains(t, got, "## What happened")
	assert.Contains(t, got, "fixed the nil deref")
	assert.NotContains(t, got, FixSummaryHeader)
}

func TestFixSummary_absent(t *testing.T) {
	assert.Equal(t, "", fixSummary(nil))
}

func TestFailureReason_fromLatestFailedMarker(t *testing.T) {
	comments := []tracker.Comment{
		{Body: ReviewFailedHeader + "\npanic in board_state.go", Created: time.Now()},
	}
	assert.Equal(t, "panic in board_state.go", latestFailureReason(comments))
}

// SC-620: the detail pane surfaces the whole diagnosis — headline plus the
// markdown detail block — not just the first line.
func TestFailureReason_fullDiagnosisBodyKept(t *testing.T) {
	body := ImplementationFailedHeader + "\nclaude exited with code 1: API Error\n\nagent: board-SC-1-implementation\nexit code: 1\n\nlast output:\n~~~\nboom\n~~~"
	comments := []tracker.Comment{{Body: body, Created: time.Now()}}
	got := latestFailureReason(comments)
	assert.Contains(t, got, "claude exited with code 1: API Error")
	assert.Contains(t, got, "last output:\n~~~\nboom\n~~~")
	assert.NotContains(t, got, ImplementationFailedHeader)
}

func TestFailureReason_supersededByNewerMarker(t *testing.T) {
	t0 := time.Unix(1000, 0)
	t1 := time.Unix(2000, 0)
	comments := []tracker.Comment{
		{Body: DeployFailedHeader + "\nmerge conflict on main", Created: t0},
		{Body: ImplementationStartedHeader, Created: t1},
	}
	assert.Empty(t, latestFailureReason(comments))
}

func TestFailureReason_newestFailureKept(t *testing.T) {
	t0 := time.Unix(1000, 0)
	t1 := time.Unix(2000, 0)
	comments := []tracker.Comment{
		{Body: ImplementationStartedHeader, Created: t0},
		{Body: ImplementationFailedHeader + "\ncompile error", Created: t1},
	}
	assert.Equal(t, "compile error", latestFailureReason(comments))
}

func TestBuildIssueDetailExtras_allThree(t *testing.T) {
	comments := []tracker.Comment{
		{Body: ReviewCompleteHeader + "\n## Findings\nreview text", Created: time.Now()},
		{Body: ReviewFailedHeader + "\nboom", Created: time.Now()},
		{Body: FixSummaryHeader + "\nsummary text", Created: time.Now()},
	}
	extras := BuildIssueDetailExtras(comments)
	assert.Contains(t, extras.ReviewFindings, "review text")
	assert.Equal(t, "boom", extras.FailureReason)
	assert.Contains(t, extras.FixSummary, "summary text")
}

func TestBuildIssueDetailExtras_emptyComments(t *testing.T) {
	extras := BuildIssueDetailExtras(nil)
	assert.Equal(t, IssueDetailExtras{}, extras)
}

// TestBuildIssueDetailExtras_DraftState covers SC-4820's four draft-thread
// shapes: the newest of the drafter's three markers decides what an empty
// description means.
func TestBuildIssueDetailExtras_DraftState(t *testing.T) {
	t0 := time.Unix(1000, 0)
	t1 := time.Unix(2000, 0)

	t.Run("only started: drafting", func(t *testing.T) {
		extras := BuildIssueDetailExtras([]tracker.Comment{
			{Body: IdeaDraftStartedHeader, Created: t0},
		})
		assert.Equal(t, DraftStateDrafting, extras.DraftState)
		assert.Empty(t, extras.DraftFailureReason)
	})

	t.Run("started then failed: failed with reason", func(t *testing.T) {
		extras := BuildIssueDetailExtras([]tracker.Comment{
			{Body: IdeaDraftStartedHeader, Created: t0},
			{Body: IdeaDraftFailedHeader + "\nreason: the run stopped before finishing this stage", Created: t1},
		})
		assert.Equal(t, DraftStateFailed, extras.DraftState)
		assert.Contains(t, extras.DraftFailureReason, "the run stopped before finishing this stage")
	})

	t.Run("failed then a newer started: drafting (re-run in flight)", func(t *testing.T) {
		extras := BuildIssueDetailExtras([]tracker.Comment{
			{Body: IdeaDraftFailedHeader + "\nreason: boom", Created: t0},
			{Body: IdeaDraftStartedHeader, Created: t1},
		})
		assert.Equal(t, DraftStateDrafting, extras.DraftState)
	})

	t.Run("failed then a newer provenance record: empty (a draft landed)", func(t *testing.T) {
		extras := BuildIssueDetailExtras([]tracker.Comment{
			{Body: IdeaDraftFailedHeader + "\nreason: boom", Created: t0},
			{Body: IdeaDraftHeader + "\nauthor: idea-draft-SC-1", Created: t1},
		})
		assert.Empty(t, extras.DraftState)
	})
}

// TestBuildIssueDetailExtras_FailedDraftIsNotAStageFailure: idea-draft-failed
// is deliberately unclassified, so it must never leak into the stage-failure
// section the card's badge reads.
func TestBuildIssueDetailExtras_FailedDraftIsNotAStageFailure(t *testing.T) {
	extras := BuildIssueDetailExtras([]tracker.Comment{
		{Body: IdeaDraftFailedHeader + "\nreason: boom", Created: time.Now()},
	})
	assert.Empty(t, extras.FailureReason)
	assert.Equal(t, DraftStateFailed, extras.DraftState)
}

// Once reconcile posts [human:deployed] for a confirmed-shipped PR, the detail
// pane's failure reason clears via the same supersession guard as the card
// (SC-910, 695 class).
func TestFailureReason_clearedByDeployedMarker(t *testing.T) {
	t0 := time.Unix(1000, 0)
	t1 := time.Unix(2000, 0)
	comments := []tracker.Comment{
		{Body: DeployFailedHeader + "\nmerge conflict on main\npr: https://github.com/o/r/pull/7", Created: t0},
		{Body: DeployedHeader + "\npr: https://github.com/o/r/pull/7", Created: t1},
	}
	assert.Empty(t, latestFailureReason(comments))
}
