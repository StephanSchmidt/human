package dispatch

import (
	"context"
	"fmt"

	"github.com/StephanSchmidt/human/internal/telegram"
)

// TelegramFetcher is the subset of telegram.Client needed to fetch messages.
type TelegramFetcher interface {
	GetUpdates(ctx context.Context, limit int) ([]telegram.Update, error)
	AckUpdate(ctx context.Context, updateID int) error
}

// TelegramSource adapts a Telegram client to the MessageSource interface.
type TelegramSource struct {
	Client       TelegramFetcher
	AllowedUsers []int64
}

// FetchMessages returns pending Telegram messages filtered by allowed users.
func (s *TelegramSource) FetchMessages(ctx context.Context) ([]QueuedMessage, error) {
	updates, err := s.Client.GetUpdates(ctx, 100)
	if err != nil {
		return nil, err
	}

	var messages []QueuedMessage
	for _, u := range updates {
		if u.Message == nil {
			continue
		}
		if !s.isAllowed(u.Message.From) {
			continue
		}
		messages = append(messages, QueuedMessage{
			UpdateID: u.UpdateID,
			ChatID:   u.Message.Chat.ID,
			From:     formatFrom(u.Message.From),
			Text:     u.Message.Text,
		})
	}
	return messages, nil
}

// AckMessage acknowledges a Telegram update.
func (s *TelegramSource) AckMessage(ctx context.Context, updateID int) error {
	return s.Client.AckUpdate(ctx, updateID)
}

func (s *TelegramSource) isAllowed(user *telegram.User) bool {
	if len(s.AllowedUsers) == 0 {
		return true
	}
	if user == nil {
		return false
	}
	for _, id := range s.AllowedUsers {
		if user.ID == id {
			return true
		}
	}
	return false
}

func formatFrom(user *telegram.User) string {
	if user == nil {
		return ""
	}
	name := user.FirstName
	if user.LastName != "" {
		name = fmt.Sprintf("%s %s", name, user.LastName)
	}
	return name
}
