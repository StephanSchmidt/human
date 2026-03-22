package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRootCmd_hasFigmaSubcommand(t *testing.T) {
	cmd := newRootCmd()
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "figma" {
			found = true
			assert.Equal(t, "tools", sub.GroupID)
			break
		}
	}
	assert.True(t, found, "expected 'figma' subcommand")
}
