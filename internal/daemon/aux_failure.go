package daemon

// A daemon-launched auxiliary per-ticket run (idea-draft, relate) is not a
// board stage: it carries no stage retry budget and must never move the card.
// Before this file existed, a run that died — most often on its very first API
// call — left nothing on the ticket at all, because the run itself never got a
// turn to report and the board's stage-failure watcher deliberately excludes
// aux agents (agentname.IsBoard). This watcher is that missing signal,
// deliberately kept separate from RunBoardFailureWatch (SC-4820).

import (
	"context"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/gethuman-sh/human/internal/agentname"
	"github.com/gethuman-sh/human/internal/claude/hookevents"
	"github.com/gethuman-sh/human/internal/marker"
	"github.com/gethuman-sh/human/internal/tracker"
)

// AuxRunRecord is what the daemon can learn about an auxiliary run from its own
// execution artifacts. It exists because the hook event alone cannot tell a
// successful draft whose container was later reaped from a run that died: both
// arrive as StopFailure. Repairing the outcome record (SC-4820, first half) is
// what makes this answerable at all.
type AuxRunRecord struct {
	StartedAt     time.Time
	ProcessFailed bool
	Known         bool // an outcome record was readable; false means fall back to the event
}

// AuxRunLookup reads an aux run's execution record by agent name. nil disables
// the check and every non-clean exit is then treated as a failure.
type AuxRunLookup func(agentName string) AuxRunRecord

// AuxFailureDeps wires the aux watcher's collaborators, following the package's
// "nil disables" convention like FailureDeps.
type AuxFailureDeps struct {
	CommenterFor CommenterFor
	Lookup       AuxRunLookup
	Diagnose     BoardFailureDiagnoser
	Logger       zerolog.Logger
}

// RunAuxFailureWatch turns a dead per-ticket AUXILIARY run into a record on its
// ticket. It is deliberately a second watcher rather than a loosened filter in
// RunBoardFailureWatch: an aux run is not a board stage, so its death must not
// reach failedTypeFor, must not spend a stage retry, and must not move the card
// (SC-4820). It mirrors that watcher's loop shape and nothing else.
func RunAuxFailureWatch(ctx context.Context, store *HookEventStore, deps AuxFailureDeps) {
	logger := deps.Logger
	if store == nil || deps.CommenterFor == nil {
		return
	}
	ch := store.Subscribe()
	defer store.Unsubscribe(ch)
	logger.Info().Msg("aux failure watcher started")

	var lastSeq uint64
	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			newEvents, seq := store.EventsSince(lastSeq)
			lastSeq = seq
			for _, evt := range newEvents {
				if _, _, ok := agentname.ParseAux(evt.AgentName); !ok {
					continue
				}
				if !hookevents.IsRunEnd(evt.EventName) {
					continue
				}
				go handleAuxAgentExit(ctx, evt, deps)
			}
		}
	}
}

// handleAuxAgentExit posts the aux run's failure record, unless the run already
// recorded an outcome of its own.
func handleAuxAgentExit(ctx context.Context, evt hookevents.Event, deps AuxFailureDeps) {
	key, prefix, ok := agentname.ParseAux(evt.AgentName)
	if !ok {
		return
	}
	if !auxRunFailed(evt, deps) {
		return
	}
	commenter, err := deps.CommenterFor()
	if err != nil {
		deps.Logger.Warn().Err(err).Str("agent", evt.AgentName).Msg("aux failure: no commenter")
		return
	}
	comments, err := commenter.ListComments(ctx, key)
	if err != nil {
		deps.Logger.Warn().Err(err).Str("agent", evt.AgentName).Msg("aux failure: cannot list comments")
		return
	}
	var since time.Time
	if deps.Lookup != nil {
		since = deps.Lookup(evt.AgentName).StartedAt
	}
	if auxRunRecorded(prefix, comments, since) {
		return
	}
	m, order := auxFailureMarker(prefix, auxDiagnosis(evt, deps))
	if m.Type == "" {
		return
	}
	if err := postMarker(ctx, commenter, key, m, order...); err != nil {
		deps.Logger.Warn().Err(err).Str("agent", evt.AgentName).Msg("aux failure: cannot post the record")
	}
}

// auxRunFailed answers from the run's OWN record when there is one, and from the
// event otherwise. The record is the better witness in both directions: a run
// that exited non-zero and fired an ordinary Stop is a failure the event calls
// clean, and a run that finished and was reaped later is a success the event
// calls a death.
func auxRunFailed(evt hookevents.Event, deps AuxFailureDeps) bool {
	if deps.Lookup != nil {
		if rec := deps.Lookup(evt.AgentName); rec.Known {
			return rec.ProcessFailed
		}
	}
	return evt.EventName == hookevents.EventStopFailure
}

// auxTerminalHeader and auxStartedHeader name the header pair for an aux
// prefix, so auxRunRecorded's scan stays closed-list rather than pattern-based.
func auxTerminalHeaders(prefix string) []string {
	switch prefix {
	case "idea-draft":
		return []string{IdeaDraftFailedHeader, IdeaDraftHeader}
	case "relate":
		return []string{RelatedHeader}
	}
	return nil
}

func auxStartedHeader(prefix string) string {
	switch prefix {
	case "idea-draft":
		return IdeaDraftStartedHeader
	case "relate":
		return RelatedStartedHeader
	}
	return ""
}

// auxRunRecorded reports whether this run already put its own outcome on the
// ticket. It is scoped to comments at or after the run's launch, so a previous
// run's record never silences this one — and with no launch time known it falls
// back to "newer than the newest start marker", which is also what makes a
// duplicate exit event for the same run a no-op.
func auxRunRecorded(prefix string, comments []tracker.Comment, since time.Time) bool {
	terminal := auxTerminalHeaders(prefix)
	started := auxStartedHeader(prefix)
	if len(terminal) == 0 {
		return false
	}

	isTerminal := func(c tracker.Comment) bool {
		trimmed := strings.TrimSpace(c.Body)
		for _, h := range terminal {
			if strings.HasPrefix(trimmed, h) {
				return true
			}
		}
		return false
	}
	isStarted := func(c tracker.Comment) bool {
		return started != "" && strings.HasPrefix(strings.TrimSpace(c.Body), started)
	}

	if !since.IsZero() {
		for _, c := range comments {
			if isTerminal(c) && !c.Created.Before(since) {
				return true
			}
		}
		return false
	}

	var newestStart, newestTerminal *tracker.Comment
	for i := range comments {
		c := comments[i]
		if isStarted(c) && (newestStart == nil || commentNewer(c, *newestStart)) {
			newestStart = &comments[i]
		}
		if isTerminal(c) && (newestTerminal == nil || commentNewer(c, *newestTerminal)) {
			newestTerminal = &comments[i]
		}
	}
	if newestTerminal == nil {
		return false
	}
	if newestStart == nil {
		return true
	}
	return commentNewer(*newestTerminal, *newestStart)
}

// auxFailureMarker composes the record for the aux kind. The drafter gets its
// own type; a relate run gets the "incomplete" head the related vocabulary
// already carries for a run that could not finish, because a second marker
// meaning the same thing would split that trail in half.
func auxFailureMarker(prefix, diagnosis string) (marker.Marker, []string) {
	switch prefix {
	case "idea-draft":
		return failureMarker(MarkerIdeaDraftFailed, diagnosis), nil
	case "relate":
		headline, detail, _ := strings.Cut(strings.TrimSpace(diagnosis), "\n")
		return marker.Marker{
			Type: MarkerRelated, Head: "incomplete",
			Body: strings.TrimSpace(headline + "\n\n" + detail),
		}, nil
	}
	return marker.Marker{}, nil
}

// auxDiagnosis distills why the run died from its artifacts, degrading to the
// generic line when no diagnoser is wired.
func auxDiagnosis(evt hookevents.Event, deps AuxFailureDeps) string {
	if deps.Diagnose == nil {
		return genericStageFailure
	}
	d := deps.Diagnose(evt.AgentName, evt.ErrorType)
	if d.Headline == "" {
		return genericStageFailure
	}
	if d.Detail == "" {
		return d.Headline
	}
	return d.Headline + "\n\n" + d.Detail
}
