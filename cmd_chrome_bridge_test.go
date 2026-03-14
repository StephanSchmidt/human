package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildChromeBridgeCmd_Exists(t *testing.T) {
	cmd := buildChromeBridgeCmd()
	assert.Equal(t, "chrome-bridge", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
}

func TestChromeBridge_MissingAddr(t *testing.T) {
	t.Setenv("HUMAN_CHROME_ADDR", "")
	t.Setenv("HUMAN_DAEMON_TOKEN", "some-token")

	cmd := buildChromeBridgeCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HUMAN_CHROME_ADDR")
}

func TestChromeBridge_MissingToken(t *testing.T) {
	t.Setenv("HUMAN_CHROME_ADDR", "localhost:19286")
	t.Setenv("HUMAN_DAEMON_TOKEN", "")

	cmd := buildChromeBridgeCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HUMAN_DAEMON_TOKEN")
}

func TestChromeBridge_RegisteredInRoot(t *testing.T) {
	root := newRootCmd()
	found := false
	for _, sub := range root.Commands() {
		if sub.Name() == "chrome-bridge" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected chrome-bridge command to be registered")
}
