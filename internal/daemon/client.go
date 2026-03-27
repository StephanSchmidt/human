package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
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
		Version:   version,
		Token:     token,
		Args:      args,
		Env:       env,
		ClientPID: findAncestorClaude(),
	}

	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		return 1, fmt.Errorf("failed to send request: %w", err)
	}

	// Single buffered reader for the connection — creating a new
	// bufio.Reader per read would lose data buffered by the first reader.
	reader := bufio.NewReader(conn)

	line, err := reader.ReadBytes('\n')
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
		line2, err := reader.ReadBytes('\n')
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

// RunRemoteCapture connects to the daemon and runs args, returning stdout
// as bytes instead of printing to os.Stdout.
func RunRemoteCapture(addr, token string, args []string) ([]byte, error) {
	conn, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("cannot reach daemon at %s: %w", addr, err)
	}
	defer func() { _ = conn.Close() }()

	req := Request{
		Token: token,
		Args:  args,
	}

	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("invalid response from daemon: %w", err)
	}

	if resp.ExitCode != 0 {
		return nil, fmt.Errorf("daemon command failed: %s", resp.Stderr)
	}

	return []byte(resp.Stdout), nil
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
	if httpResp == nil {
		return fmt.Errorf("OAuth callback delivery returned nil response")
	}
	if httpResp.Body != nil {
		defer func() { _ = httpResp.Body.Close() }()
		_, _ = io.Copy(io.Discard, httpResp.Body)
	}
	if httpResp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("OAuth callback delivery failed with status %d", httpResp.StatusCode)
	}
	return nil
}

// findAncestorClaude walks the process tree from the current process upward,
// looking for an ancestor whose /proc/<pid>/comm is "claude". Returns the
// first matching PID, or falls back to os.Getppid() if none is found.
func findAncestorClaude() int {
	pid := os.Getppid()
	for pid > 1 {
		comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
		if err != nil {
			break
		}
		if strings.TrimSpace(string(comm)) == "claude" {
			return pid
		}
		// Read the parent PID from /proc/<pid>/status.
		status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		if err != nil {
			break
		}
		ppid := 0
		for _, line := range strings.Split(string(status), "\n") {
			if strings.HasPrefix(line, "PPid:") {
				ppid, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "PPid:")))
				break
			}
		}
		if ppid <= 1 || ppid == pid {
			break
		}
		pid = ppid
	}
	return os.Getppid()
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
