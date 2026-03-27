package cmdping

import (
	"bytes"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPing_reachable(t *testing.T) {
	// Start a TCP listener to act as a fake daemon.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	cmd := BuildPingCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--addr", ln.Addr().String()})

	err = cmd.Execute()
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "reachable")
	assert.Contains(t, buf.String(), ln.Addr().String())
}

func TestPing_unreachable(t *testing.T) {
	cmd := BuildPingCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--addr", "127.0.0.1:1"})

	err := cmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, buf.String(), "not reachable")
}
