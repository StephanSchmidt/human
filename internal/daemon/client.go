package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
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

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 4*1024*1024), 4*1024*1024)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return 1, fmt.Errorf("failed to read response: %w", err)
		}
		return 1, fmt.Errorf("daemon closed connection without response")
	}

	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		return 1, fmt.Errorf("invalid response from daemon: %w", err)
	}

	if resp.Stdout != "" {
		_, _ = fmt.Fprint(os.Stdout, resp.Stdout)
	}
	if resp.Stderr != "" {
		_, _ = fmt.Fprint(os.Stderr, resp.Stderr)
	}

	return resp.ExitCode, nil
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
