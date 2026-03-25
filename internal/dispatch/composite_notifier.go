package dispatch

import (
	"context"
)

// CompositeNotifier fans out notifications to multiple notifiers.
// All notifiers are called; the first error is returned.
type CompositeNotifier struct {
	Notifiers []Notifier
}

// Notify calls all wrapped notifiers. It does not short-circuit:
// every notifier is called even if an earlier one fails.
func (c *CompositeNotifier) Notify(ctx context.Context, chatID int64, text string) error {
	var firstErr error
	for _, n := range c.Notifiers {
		if err := n.Notify(ctx, chatID, text); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
