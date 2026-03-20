package chrome

import (
	"bufio"
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

// mockMcpScript creates a shell script that acts as a mock claude --claude-in-chrome-mcp.
// It handles the init handshake and echoes tool calls back as success responses.
func mockMcpScript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "mock-mcp.sh")

	content := `#!/bin/sh
# Read initialize request
read -r line
# Write initialize response
echo '{"jsonrpc":"2.0","id":0,"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{},"logging":{}},"serverInfo":{"name":"mock","version":"1.0"}}}'
# Read initialized notification
read -r line
# Echo tool calls as responses (extract id with sed)
while read -r line; do
  id=$(echo "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  echo "{\"jsonrpc\":\"2.0\",\"id\":$id,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}"
done
`
	require.NoError(t, os.WriteFile(script, []byte(content), 0o755))
	return script
}

func startTestChromeServer(t *testing.T, token string, translator *McpTranslator) (addr string, cancel context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	srv := &Server{
		Addr:       "127.0.0.1:0",
		Token:      token,
		Translator: translator,
		Logger:     zerolog.Nop(),
	}

	ln, err := net.Listen("tcp", srv.Addr)
	require.NoError(t, err)
	addr = ln.Addr().String()
	_ = ln.Close()

	srv.Addr = addr

	go func() {
		_ = srv.ListenAndServe(ctx)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Cleanup(func() { cancel() })
	return addr, cancel
}

func TestChromeServer_ProxySession(t *testing.T) {
	token := "chrome-test-token"
	script := mockMcpScript(t)
	translator := &McpTranslator{ClaudePath: script, Logger: zerolog.Nop()}
	addr, _ := startTestChromeServer(t, token, translator)

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// Send auth request.
	req := proxyRequest{Token: token}
	enc := json.NewEncoder(conn)
	require.NoError(t, enc.Encode(req))

	// Read ProxyAck.
	scanner := bufio.NewScanner(conn)
	require.True(t, scanner.Scan(), "expected proxy ack")

	var ack ProxyAck
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &ack))
	assert.True(t, ack.OK)
	assert.Empty(t, ack.Error)

	// Send a tool call as a 4-byte LE framed message.
	toolCall := `{"method":"execute_tool","params":{"client_id":"claude-code","tool":"test_tool","args":{"key":"value"}}}`
	require.NoError(t, WriteMessage(conn, []byte(toolCall)))

	// Read the response (4-byte LE framed).
	resp, err := ReadMessage(conn)
	require.NoError(t, err)

	var respObj map[string]any
	require.NoError(t, json.Unmarshal(resp, &respObj))
	assert.NotNil(t, respObj["result"], "expected result in response")
}

func TestChromeServer_AuthRejection(t *testing.T) {
	token := "correct-token"
	// Translator won't be used — auth fails first. Use dummy path.
	translator := &McpTranslator{ClaudePath: "/nonexistent", Logger: zerolog.Nop()}
	addr, _ := startTestChromeServer(t, token, translator)

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	req := proxyRequest{Token: "wrong-token"}
	enc := json.NewEncoder(conn)
	require.NoError(t, enc.Encode(req))

	scanner := bufio.NewScanner(conn)
	require.True(t, scanner.Scan(), "expected proxy ack")

	var ack ProxyAck
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &ack))
	assert.False(t, ack.OK)
	assert.Contains(t, ack.Error, "authentication failed")
}

func TestChromeServer_InvalidJSON(t *testing.T) {
	token := "test-token"
	translator := &McpTranslator{ClaudePath: "/nonexistent", Logger: zerolog.Nop()}
	addr, _ := startTestChromeServer(t, token, translator)

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	_, err = conn.Write([]byte("not json\n"))
	require.NoError(t, err)

	scanner := bufio.NewScanner(conn)
	require.True(t, scanner.Scan())

	var ack ProxyAck
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &ack))
	assert.False(t, ack.OK)
	assert.Contains(t, ack.Error, "invalid request JSON")
}
