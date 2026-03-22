package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRootCmd_hasAmplitudeSubcommand(t *testing.T) {
	cmd := newRootCmd()
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "amplitude" {
			found = true
			assert.Equal(t, "tools", sub.GroupID)
			break
		}
	}
	assert.True(t, found, "expected 'amplitude' subcommand")
}
