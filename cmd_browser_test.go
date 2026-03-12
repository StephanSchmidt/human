package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrowserCmd_noArgs(t *testing.T) {
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"browser"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, buf.String(), "accepts 1 arg(s)")
}

func TestBrowserCmd_tooManyArgs(t *testing.T) {
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"browser", "https://a.com", "https://b.com"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, buf.String(), "accepts 1 arg(s)")
}

func TestBrowserCmd_invalidURL(t *testing.T) {
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"browser", "not-a-url"})

	err := cmd.Execute()
	require.Error(t, err)
}
