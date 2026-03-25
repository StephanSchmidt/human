package dispatch

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubSlackSender struct {
	text string
	err  error
}

func (s *stubSlackSender) SendMessage(_ context.Context, text string) error {
	s.text = text
	return s.err
}

func TestSlackNotifier_Notify(t *testing.T) {
	sender := &stubSlackSender{}
	notifier := &SlackNotifier{Client: sender}

	err := notifier.Notify(context.Background(), 42, "Dispatched to claude:0.1")
	require.NoError(t, err)
	assert.Equal(t, "Dispatched to claude:0.1", sender.text)
}

func TestSlackNotifier_Notify_IgnoresChatID(t *testing.T) {
	sender := &stubSlackSender{}
	notifier := &SlackNotifier{Client: sender}

	// Different chatIDs should not matter — channel is on the client.
	_ = notifier.Notify(context.Background(), 0, "msg1")
	assert.Equal(t, "msg1", sender.text)
	_ = notifier.Notify(context.Background(), 999, "msg2")
	assert.Equal(t, "msg2", sender.text)
}

func TestSlackNotifier_Notify_Error(t *testing.T) {
	sender := &stubSlackSender{err: fmt.Errorf("slack send failed")}
	notifier := &SlackNotifier{Client: sender}

	err := notifier.Notify(context.Background(), 42, "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "slack send failed")
}
