package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

const dialTimeout = 5 * time.Second

// RunRemote connects to the daemon at addr, sends the CLI args, and returns
// the exit code. Stdout and stderr are written to os.Stdout and os.Stderr.
func RunRemote(addr, token string, args []string, version string) (int, error) {
	conn, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		return 1, fmt.Errorf("cannot reach daemon at %s: %w", addr, err)
	}
	defer func() { _ = conn.Close() }()

	env := selectedEnv()

	req := Request{
		Version: version,
		Token:   token,
		Args:    args,
		Env:     env,
	}

	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		return 1, fmt.Errorf("failed to send request: %w", err)
	}

	line, err := readLine(conn)
	if err != nil {
		return 1, fmt.Errorf("failed to read response: %w", err)
	}

	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return 1, fmt.Errorf("invalid response from daemon: %w", err)
	}

	if resp.Stdout != "" {
		_, _ = fmt.Fprint(os.Stdout, resp.Stdout)
	}
	if resp.Stderr != "" {
		_, _ = fmt.Fprint(os.Stderr, resp.Stderr)
	}

	// Two-line OAuth protocol: daemon signals us to wait for a callback URL.
	// Claude Code awaits the BROWSER process exit (10-min timeout via execa),
	// so we stay alive, read line 2 (callback URL), deliver it, then exit 0.
	if resp.AwaitCallback {
		line2, err := readLine(conn)
		if err != nil {
			return 1, fmt.Errorf("failed to read callback response: %w", err)
		}
		var resp2 Response
		if err := json.Unmarshal(line2, &resp2); err != nil {
			return 1, fmt.Errorf("invalid callback response: %w", err)
		}
		if resp2.Callback != "" {
			if err := deliverCallback(resp2.Callback); err != nil {
				return 1, fmt.Errorf("failed to deliver OAuth callback: %w", err)
			}
		}
	}

	return resp.ExitCode, nil
}

// deliverCallback performs an HTTP GET to the callback URL, delivering the
// OAuth callback to the local listener (e.g. Claude Code) from inside the
// container where localhost is reachable.
func deliverCallback(callbackURL string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	httpResp, err := client.Get(callbackURL) //nolint:gosec // URL is from trusted daemon
	if err != nil {
		return err
	}
	if httpResp != nil && httpResp.Body != nil {
		defer func() { _ = httpResp.Body.Close() }()
		_, _ = io.Copy(io.Discard, httpResp.Body)
	}
	return nil
}

// readLine reads a single newline-terminated line from a net.Conn.
func readLine(conn net.Conn) ([]byte, error) {
	var buf []byte
	tmp := make([]byte, 1)
	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[0])
			if tmp[0] == '\n' {
				return buf, nil
			}
		}
		if err != nil {
			return buf, err
		}
	}
}

// selectedEnv returns a small set of display-related env vars to forward.
func selectedEnv() map[string]string {
	keys := []string{"NO_COLOR", "TERM", "COLUMNS"}
	env := make(map[string]string)
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok {
			env[k] = v
		}
	}
	if len(env) == 0 {
		return nil
	}
	return env
}
