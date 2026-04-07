package dispatch

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/rs/zerolog"
)

// DefaultPollInterval is the default interval between Telegram polls.
const DefaultPollInterval = 10 * time.Second

// DefaultPromptTemplate is the prompt sent to idle Claude agents.
// Must be a single line — tmux send-keys treats newlines as Enter keypresses.
const DefaultPromptTemplate = `The following task was sent via Telegram. Create an implementation ticket on Linear (HUM) for it, then execute the plan: %s`

// maxMessageTextLen caps the number of bytes of Telegram message text that
// reach the Claude prompt. Telegram allows messages up to 4096 characters,
// and 2000 is a round cap well above any realistic task description while
// below the point where tmux buffer behavior or prompt bloat become
// concerns. Text longer than this is truncated with a " [truncated]"
// marker so Claude sees that something was cut.
const maxMessageTextLen = 2000

// MessageSource fetches and acknowledges Telegram messages.
type MessageSource interface {
	FetchMessages(ctx context.Context) ([]QueuedMessage, error)
	AckMessage(ctx context.Context, updateID int) error
}

// AgentFinder discovers idle Claude tmux panes.
type AgentFinder interface {
	FindIdleAgents(ctx context.Context) ([]Agent, error)
}

// AgentSender sends a prompt to a tmux pane.
type AgentSender interface {
	SendPrompt(ctx context.Context, agent Agent, prompt string) error
}

// Notifier sends a notification back to the user.
type Notifier interface {
	Notify(ctx context.Context, chatID int64, text string) error
}

// QueuedMessage holds a Telegram message waiting for dispatch.
type QueuedMessage struct {
	UpdateID int
	ChatID   int64
	From     string
	Text     string
}

// Agent represents a Claude tmux pane that can receive work.
type Agent struct {
	SessionName string
	WindowIndex int
	PaneIndex   int
	Label       string // e.g. "session:0.1"
}

// Config holds dispatcher configuration.
type Config struct {
	PollInterval time.Duration
}

// Dispatcher polls Telegram and dispatches messages to idle Claude agents.
type Dispatcher struct {
	Source   MessageSource
	Finder   AgentFinder
	Sender   AgentSender
	Notifier Notifier
	Config   Config
	Logger   zerolog.Logger

	mu    sync.Mutex
	queue []QueuedMessage
	seen  map[int]bool
}

// Run starts the dispatch loop. It blocks until ctx is cancelled.
func (d *Dispatcher) Run(ctx context.Context) error {
	interval := d.Config.PollInterval
	if interval == 0 {
		interval = DefaultPollInterval
	}
	d.seen = make(map[int]bool)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run once immediately on start.
	d.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			d.tick(ctx)
		}
	}
}

func (d *Dispatcher) tick(ctx context.Context) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.fetchMessages(ctx)
	if len(d.queue) == 0 {
		return
	}
	d.dispatchMessages(ctx)
}

func (d *Dispatcher) fetchMessages(ctx context.Context) {
	messages, err := d.Source.FetchMessages(ctx)
	if err != nil {
		d.Logger.Warn().Err(err).Msg("failed to fetch Telegram messages")
		return
	}

	for _, msg := range messages {
		if d.seen[msg.UpdateID] {
			continue
		}
		d.seen[msg.UpdateID] = true
		d.queue = append(d.queue, msg)
		d.Logger.Info().Int("updateID", msg.UpdateID).Str("from", msg.From).Msg("queued message")
	}
}

func (d *Dispatcher) dispatchMessages(ctx context.Context) {
	agents, err := d.Finder.FindIdleAgents(ctx)
	if err != nil {
		d.Logger.Warn().Err(err).Msg("failed to find idle agents")
		return
	}
	if len(agents) == 0 {
		d.Logger.Debug().Int("queued", len(d.queue)).Msg("no idle agents available")
		return
	}

	dispatched := 0
	for _, agent := range agents {
		if len(d.queue) == 0 {
			break
		}

		msg := d.queue[0]
		prompt := buildPrompt(msg.Text)

		d.Logger.Info().Str("agent", agent.Label).Int("updateID", msg.UpdateID).Int("promptLen", len(prompt)).Msg("sending prompt via tmux send-keys")

		if err := d.Sender.SendPrompt(ctx, agent, prompt); err != nil {
			d.Logger.Warn().Err(err).Str("agent", agent.Label).Int("updateID", msg.UpdateID).Msg("send-keys failed, re-queuing")
			continue
		}

		// Sent successfully — dequeue.
		d.queue = d.queue[1:]
		dispatched++

		d.Logger.Info().Str("agent", agent.Label).Int("updateID", msg.UpdateID).Str("from", msg.From).Msg("send-keys succeeded, dispatched message")

		if err := d.Source.AckMessage(ctx, msg.UpdateID); err != nil {
			d.Logger.Warn().Err(err).Int("updateID", msg.UpdateID).Msg("failed to ack message")
		}

		notifyText := fmt.Sprintf("Dispatched to %s", agent.Label)
		if err := d.Notifier.Notify(ctx, msg.ChatID, notifyText); err != nil {
			d.Logger.Warn().Err(err).Int64("chatID", msg.ChatID).Msg("failed to send dispatch notification")
		}
	}

	if dispatched > 0 {
		d.Logger.Info().Int("dispatched", dispatched).Int("remaining", len(d.queue)).Msg("dispatch cycle complete")
	}

	d.pruneSeen()
}

// maxSeenSize is the maximum number of update IDs to retain for deduplication.
// Telegram update IDs are monotonically increasing, so we evict the lowest
// IDs when the map exceeds this size.
const maxSeenSize = 1000

// pruneSeen evicts the oldest entries from the seen map when it exceeds maxSeenSize.
func (d *Dispatcher) pruneSeen() {
	if len(d.seen) <= maxSeenSize {
		return
	}
	ids := make([]int, 0, len(d.seen))
	for id := range d.seen {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	// Keep only the most recent maxSeenSize/2 entries.
	keep := maxSeenSize / 2
	for _, id := range ids[:len(ids)-keep] {
		delete(d.seen, id)
	}
}

func buildPrompt(messageText string) string {
	// Strip control characters that could interfere with tmux/terminal.
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != ' ' && r != '\t' {
			return -1
		}
		return r
	}, messageText)
	cleaned = strings.TrimSpace(cleaned)
	// Cap message length so oversized prompts cannot flood a Claude agent.
	// Byte-level truncation is sufficient here: tmux does not care about
	// rune boundaries, and the " [truncated]" marker lets Claude see that
	// the message was cut regardless of whether we sliced mid-rune.
	if len(cleaned) > maxMessageTextLen {
		cleaned = cleaned[:maxMessageTextLen] + " [truncated]"
	}
	return fmt.Sprintf(DefaultPromptTemplate, cleaned)
}
