package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRootCmd_hasNotionSubcommand(t *testing.T) {
	cmd := newRootCmd()
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "notion" {
			found = true
			assert.Equal(t, "tools", sub.GroupID)
			break
		}
	}
	assert.True(t, found, "expected 'notion' subcommand")
}
