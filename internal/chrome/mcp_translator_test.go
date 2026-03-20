package chrome

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeMockScript creates a shell script in a temp dir and returns its path.
func writeMockScript(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "mock.sh")
	require.NoError(t, os.WriteFile(script, []byte(content), 0o755))
	return script
}

// echoMcpScript handles init handshake and echoes tool calls back as success responses.
func echoMcpScript(t *testing.T) string {
	t.Helper()
	return writeMockScript(t, `#!/bin/sh
# Init handshake
read -r line
echo '{"jsonrpc":"2.0","id":0,"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{},"logging":{}},"serverInfo":{"name":"mock","version":"1.0"}}}'
read -r line
# Echo tool calls as responses
while read -r line; do
  id=$(echo "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  echo "{\"jsonrpc\":\"2.0\",\"id\":$id,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}"
done
`)
}

func TestMcpTranslator_RoundTrip(t *testing.T) {
	script := echoMcpScript(t)
	translator := &McpTranslator{ClaudePath: script, Logger: zerolog.Nop()}

	server, client := net.Pipe()
	defer func() { _ = client.Close() }()

	done := make(chan error, 1)
	go func() {
		done <- translator.Serve(context.Background(), server)
	}()

	// Send a tool call via 4-byte LE frame.
	toolCall := `{"method":"execute_tool","params":{"client_id":"claude-code","tool":"navigate","args":{"url":"https://example.com"}}}`
	require.NoError(t, WriteMessage(client, []byte(toolCall)))

	// Read the response frame.
	resp, err := ReadMessage(client)
	require.NoError(t, err)

	var respObj map[string]any
	require.NoError(t, json.Unmarshal(resp, &respObj))
	assert.NotNil(t, respObj["result"], "expected result key in response")
	assert.Nil(t, respObj["jsonrpc"], "jsonrpc should be stripped")
	assert.Nil(t, respObj["id"], "id should be stripped")

	_ = client.Close()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return")
	}
}

func TestMcpTranslator_InitHandshakeFails(t *testing.T) {
	script := writeMockScript(t, `#!/bin/sh
# Return error on init
read -r line
echo '{"jsonrpc":"2.0","id":0,"error":{"code":-1,"message":"init failed"}}'
`)

	translator := &McpTranslator{ClaudePath: script, Logger: zerolog.Nop()}
	server, client := net.Pipe()
	defer func() {
		_ = server.Close()
		_ = client.Close()
	}()

	err := translator.Serve(context.Background(), server)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MCP init handshake failed")
}

func TestMcpTranslator_SubprocessExit(t *testing.T) {
	script := writeMockScript(t, `#!/bin/sh
# Init handshake then exit immediately
read -r line
echo '{"jsonrpc":"2.0","id":0,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"mock","version":"1.0"}}}'
read -r line
exit 0
`)

	translator := &McpTranslator{ClaudePath: script, Logger: zerolog.Nop()}
	server, client := net.Pipe()
	defer func() { _ = client.Close() }()

	done := make(chan error, 1)
	go func() {
		done <- translator.Serve(context.Background(), server)
	}()

	select {
	case <-done:
		// Serve returned — good
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return after subprocess exit")
	}
}

func TestMcpTranslator_ConnClose(t *testing.T) {
	script := echoMcpScript(t)
	translator := &McpTranslator{ClaudePath: script, Logger: zerolog.Nop()}

	server, client := net.Pipe()

	done := make(chan error, 1)
	go func() {
		done <- translator.Serve(context.Background(), server)
	}()

	// Give init handshake time to complete.
	time.Sleep(200 * time.Millisecond)

	// Close the client side.
	_ = client.Close()

	select {
	case <-done:
		// Serve returned — good
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return after conn close")
	}
}

func TestMcpTranslator_Notification(t *testing.T) {
	script := writeMockScript(t, `#!/bin/sh
# Init handshake
read -r line
echo '{"jsonrpc":"2.0","id":0,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"mock","version":"1.0"}}}'
read -r line
# Send a notification
echo '{"jsonrpc":"2.0","method":"notifications/tools/list_changed","params":{}}'
# Keep alive until stdin closes
cat > /dev/null
`)

	translator := &McpTranslator{ClaudePath: script, Logger: zerolog.Nop()}
	server, client := net.Pipe()
	defer func() { _ = client.Close() }()

	done := make(chan error, 1)
	go func() {
		done <- translator.Serve(context.Background(), server)
	}()

	// Read the notification frame.
	resp, err := ReadMessage(client)
	require.NoError(t, err)

	var respObj map[string]any
	require.NoError(t, json.Unmarshal(resp, &respObj))
	assert.Equal(t, "notifications/tools/list_changed", respObj["method"])
	assert.Nil(t, respObj["jsonrpc"], "jsonrpc should be stripped")

	_ = client.Close()
	<-done
}

func TestMcpTranslator_MissingClaude(t *testing.T) {
	translator := &McpTranslator{ClaudePath: "/nonexistent/claude", Logger: zerolog.Nop()}
	server, client := net.Pipe()
	defer func() {
		_ = server.Close()
		_ = client.Close()
	}()

	err := translator.Serve(context.Background(), server)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "starting MCP subprocess")
}

func TestMcpTranslator_EmptyClaudePath(t *testing.T) {
	translator := &McpTranslator{ClaudePath: "", Logger: zerolog.Nop()}
	server, client := net.Pipe()
	defer func() {
		_ = server.Close()
		_ = client.Close()
	}()

	err := translator.Serve(context.Background(), server)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "claude binary path not configured")
}
