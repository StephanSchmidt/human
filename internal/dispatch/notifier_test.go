package dispatch

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubSender struct {
	chatID int64
	text   string
	err    error
}

func (s *stubSender) SendMessage(_ context.Context, chatID int64, text string) error {
	s.chatID = chatID
	s.text = text
	return s.err
}

func TestTelegramNotifier_Notify(t *testing.T) {
	sender := &stubSender{}
	notifier := &TelegramNotifier{Client: sender}

	err := notifier.Notify(context.Background(), 42, "Dispatched to claude:0.1")
	require.NoError(t, err)
	assert.Equal(t, int64(42), sender.chatID)
	assert.Equal(t, "Dispatched to claude:0.1", sender.text)
}

func TestTelegramNotifier_Notify_Error(t *testing.T) {
	sender := &stubSender{err: fmt.Errorf("send failed")}
	notifier := &TelegramNotifier{Client: sender}

	err := notifier.Notify(context.Background(), 42, "test")
	require.Error(t, err)
}
