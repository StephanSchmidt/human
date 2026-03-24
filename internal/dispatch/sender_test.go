package dispatch

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingRunner struct {
	calls  []string
	runErr error
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	return nil, r.runErr
}

func TestTmuxSender_SendPrompt(t *testing.T) {
	runner := &recordingRunner{}
	sender := &TmuxSender{Runner: runner}

	agent := Agent{SessionName: "claude", WindowIndex: 0, PaneIndex: 1}
	err := sender.SendPrompt(context.Background(), agent, "do the thing")
	require.NoError(t, err)

	require.Len(t, runner.calls, 2)
	assert.Equal(t, "tmux send-keys -t claude:0.1 -l do the thing", runner.calls[0])
	assert.Equal(t, "tmux send-keys -t claude:0.1 Enter", runner.calls[1])
}

func TestTmuxSender_SendPrompt_FirstCallFails(t *testing.T) {
	runner := &recordingRunner{runErr: fmt.Errorf("tmux not found")}
	sender := &TmuxSender{Runner: runner}

	err := sender.SendPrompt(context.Background(), Agent{SessionName: "s", WindowIndex: 0, PaneIndex: 0}, "test")
	require.Error(t, err)
	assert.Len(t, runner.calls, 1, "should not attempt Enter after failure")
}
