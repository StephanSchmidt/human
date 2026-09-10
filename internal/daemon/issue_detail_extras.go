package daemon

import (
	"strings"

	"github.com/gethuman-sh/human/internal/tracker"
)

// FixSummaryHeader marks the plain-language run summary a run posts when it
// ends — the account a person catching up reads instead of reconstructing the
// run from markers, commits and agent logs. Named for the fix pipeline that
// needed it first; the feature pipeline's implementation stage posts the same
// marker, and extraction has never been scoped to bug tickets.
//
// It is content, not a stage signal (like PlanCommentHeader), so it is
// deliberately NOT in orderedMarkerSpecs.
const FixSummaryHeader = "[human:fix-summary]"

// IssueDetailExtras are the comment-sourced fields the board detail panel shows
// beyond the issue body: what the review found, why a stage failed, and the
// fix summary. All are markdown; the daemon renders them to sanitized HTML at
// the wire boundary. Every field is optional — absence renders no section.
type IssueDetailExtras struct {
	ReviewFindings string // body of the newest [human:review-complete] comment, header line stripped
	FailureReason  string // full diagnosis body of the newest *-failed marker (markdown, header stripped), via failureBody()
	FixSummary     string // body of the newest [human:fix-summary] comment, header line stripped
	// DraftState says which of three things an empty description is: "failed"
	// (a background draft was attempted and died), "drafting" (one is running
	// now), or "" (none was ever attempted, or one finished). Without it an
	// empty pane covers all three (SC-4820).
	DraftState string
	// DraftFailureReason is the failed draft's diagnosis (markdown), empty
	// unless DraftState is "failed".
	DraftFailureReason string
}

// Draft state values. A closed set: the editor branches on exactly these.
const (
	DraftStateFailed   = "failed"
	DraftStateDrafting = "drafting"
)

// BuildIssueDetailExtras parses the comment-sourced detail sections from a
// ticket's comments. It never fails: absent sections yield empty strings, so a
// comment-fetch failure that hands nil comments degrades to a zero-value struct
// (AD-4) rather than blanking the panel.
func BuildIssueDetailExtras(comments []tracker.Comment) IssueDetailExtras {
	state, reason := draftState(comments)
	return IssueDetailExtras{
		ReviewFindings:     reviewFindings(comments),
		FailureReason:      latestFailureReason(comments),
		FixSummary:         fixSummary(comments),
		DraftState:         state,
		DraftFailureReason: reason,
	}
}

// draftState reads the newest of the drafter's three markers. Newest wins
// because they bracket runs: a started marker after a failed one is a re-run in
// flight, and a provenance record after either is a draft that landed.
func draftState(comments []tracker.Comment) (state, reason string) {
	var newest *tracker.Comment
	var newestHeader string
	for i := range comments {
		trimmed := strings.TrimSpace(comments[i].Body)
		var header string
		switch {
		case strings.HasPrefix(trimmed, IdeaDraftFailedHeader):
			header = IdeaDraftFailedHeader
		case strings.HasPrefix(trimmed, IdeaDraftStartedHeader):
			header = IdeaDraftStartedHeader
		case strings.HasPrefix(trimmed, IdeaDraftHeader):
			header = IdeaDraftHeader
		default:
			continue
		}
		if newest == nil || commentNewer(comments[i], *newest) {
			newest = &comments[i]
			newestHeader = header
		}
	}
	if newest == nil {
		return "", ""
	}
	switch newestHeader {
	case IdeaDraftFailedHeader:
		return DraftStateFailed, failureBody(newest.Body)
	case IdeaDraftStartedHeader:
		return DraftStateDrafting, ""
	default:
		return "", ""
	}
}

// reviewFindings returns the newest [human:review-complete] comment body with
// its header line stripped, so the panel shows the reviewer's inlined findings.
func reviewFindings(comments []tracker.Comment) string {
	return latestSectionComment(comments, ReviewCompleteHeader)
}

// fixSummary returns the newest [human:fix-summary] comment body with its
// header line stripped.
func fixSummary(comments []tracker.Comment) string {
	return latestSectionComment(comments, FixSummaryHeader)
}

// latestSectionComment returns the body of the newest comment whose trimmed
// body starts with header, with the header line removed. Mirrors
// latestPlanComment (board_state.go): HasPrefix on the trimmed body so a
// comment merely quoting the header mid-body is not matched, and latest-by-time
// wins so a re-post supersedes older content. Returns "" when none match.
func latestSectionComment(comments []tracker.Comment, header string) string {
	var body string
	var haveLatest bool
	var latest tracker.Comment
	for _, c := range comments {
		trimmed := strings.TrimSpace(c.Body)
		if !strings.HasPrefix(trimmed, header) {
			continue
		}
		if !haveLatest || commentNewer(c, latest) {
			latest = c
			haveLatest = true
			body = strings.TrimSpace(strings.TrimPrefix(trimmed, header))
		}
	}
	return body
}

// latestFailureReason returns the full diagnosis from the newest *-failed
// marker across all stages: header stripped, headline and markdown detail kept,
// so the detail pane shows the whole "why" while the card keeps its one-line
// failureReason. Returns "" when no failure marker is present.
func latestFailureReason(comments []tracker.Comment) string {
	var haveLatest bool
	var latest tracker.Comment
	for _, c := range comments {
		_, state, ok := ClassifyMarker(c.Body)
		if !ok || state != BoardFailed {
			continue
		}
		if !haveLatest || commentNewer(c, latest) {
			latest = c
			haveLatest = true
		}
	}
	if !haveLatest {
		return ""
	}
	// Agree with the card (DeriveBoardCard): a failure reason is shown only while
	// the *-failed marker is the ticket's newest marker. A strictly-newer marker
	// anywhere supersedes it (SC-910).
	if newest, ok := latestMarkerOverall(comments); ok && commentNewer(newest.comment, latest) {
		return ""
	}
	return failureBody(latest.Body)
}
