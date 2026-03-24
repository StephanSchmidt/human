package dispatch

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// DefaultPollInterval is the default interval between Telegram polls.
const DefaultPollInterval = 10 * time.Second

// DefaultPromptTemplate is the prompt sent to idle Claude agents.
// Must be a single line — tmux send-keys treats newlines as Enter keypresses.
const DefaultPromptTemplate = `The following task was sent via Telegram. Create an implementation ticket on Linear (HUM) for it, then execute the plan: %s`

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
	Finder  AgentFinder
	Sender  AgentSender
	Notifier Notifier
	Config  Config
	Logger  zerolog.Logger

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
}

func buildPrompt(messageText string) string {
	return fmt.Sprintf(DefaultPromptTemplate, strings.TrimSpace(messageText))
}
