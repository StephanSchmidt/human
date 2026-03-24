package dispatch

import (
	"context"
)

// TelegramSender is the subset of telegram.Client needed to send messages.
type TelegramSender interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
}

// TelegramNotifier notifies a Telegram chat about dispatch events.
type TelegramNotifier struct {
	Client TelegramSender
}

// Notify sends a text message to the given chat.
func (n *TelegramNotifier) Notify(ctx context.Context, chatID int64, text string) error {
	return n.Client.SendMessage(ctx, chatID, text)
}
