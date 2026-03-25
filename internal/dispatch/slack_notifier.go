package dispatch

import (
	"context"
)

// SlackSender is the subset of slack.Client needed to send messages.
type SlackSender interface {
	SendMessage(ctx context.Context, text string) error
}

// SlackNotifier notifies a Slack channel about dispatch events.
// The channel is pre-configured on the SlackSender.
type SlackNotifier struct {
	Client SlackSender
}

// Notify sends a text message to the configured Slack channel.
// The chatID parameter is ignored (it is Telegram-specific).
func (n *SlackNotifier) Notify(ctx context.Context, _ int64, text string) error {
	return n.Client.SendMessage(ctx, text)
}
