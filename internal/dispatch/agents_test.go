package dispatch

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTmuxAgentFinder_AlwaysReturnsNil(t *testing.T) {
	finder := &TmuxAgentFinder{}
	agents, err := finder.FindIdleAgents(context.Background())
	require.NoError(t, err)
	assert.Empty(t, agents)
}
