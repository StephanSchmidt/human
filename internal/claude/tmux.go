package claude

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// TmuxPane represents a tmux pane with an active Claude process.
type TmuxPane struct {
	PID          int
	SessionName  string
	WindowIndex  int
	PaneIndex    int
	Cwd          string        // pane working directory
	Devcontainer bool          // true when claude runs inside a devcontainer in this pane
	ContainerID  string        // matched container ID (only set when Devcontainer is true)
	ClaudePID    int           // PID of the actual claude process (0 if unknown)
	State        InstanceState // busy/ready/unknown
}

// TmuxClient abstracts listing tmux panes for testability.
type TmuxClient interface {
	ListPanes(ctx context.Context) ([]TmuxPane, error)
}

// OSTmuxClient implements TmuxClient using the real tmux command.
type OSTmuxClient struct {
	Runner CommandRunner
}

// ListPanes runs tmux list-panes and parses the output.
func (c *OSTmuxClient) ListPanes(ctx context.Context) ([]TmuxPane, error) {
	out, err := c.Runner.Run(ctx, "tmux", "list-panes", "-a",
		"-F", "#{pane_pid}\t#{session_name}\t#{window_index}\t#{pane_index}\t#{pane_current_path}")
	if err != nil {
		return nil, err
	}
	return parseTmuxOutput(out), nil
}

func parseTmuxOutput(data []byte) []TmuxPane {
	var panes []TmuxPane
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, "\t")
		if len(parts) < 4 {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			continue
		}
		winIdx, err := strconv.Atoi(strings.TrimSpace(parts[2]))
		if err != nil {
			continue
		}
		paneIdx, err := strconv.Atoi(strings.TrimSpace(parts[3]))
		if err != nil {
			continue
		}
		cwd := ""
		if len(parts) >= 5 {
			cwd = strings.TrimSpace(parts[4])
		}
		panes = append(panes, TmuxPane{
			PID:         pid,
			SessionName: strings.TrimSpace(parts[1]),
			WindowIndex: winIdx,
			PaneIndex:   paneIdx,
			Cwd:         cwd,
		})
	}
	return panes
}

// ProcessLister abstracts listing all processes with PID, PPID, and command info.
type ProcessLister interface {
	ListProcesses(ctx context.Context) ([]ProcessInfo, error)
}

// ProcessInfo holds PID, parent PID, command name, and full argument line.
type ProcessInfo struct {
	PID  int
	PPID int
	Comm string
	Args string // full command line (may be empty)
}

// OSProcessLister implements ProcessLister using ps.
type OSProcessLister struct {
	Runner CommandRunner
}

// ListProcesses returns all processes with their PID, PPID, command name, and args.
func (l *OSProcessLister) ListProcesses(ctx context.Context) ([]ProcessInfo, error) {
	out, err := l.Runner.Run(ctx, "ps", "-eo", "pid,ppid,comm,args")
	if err != nil {
		return nil, err
	}
	return parseProcessList(out), nil
}

func parseProcessList(data []byte) []ProcessInfo {
	var procs []ProcessInfo
	scanner := bufio.NewScanner(bytes.NewReader(data))
	first := true
	for scanner.Scan() {
		if first {
			first = false // skip header
			continue
		}
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		comm := fields[2]
		// args is everything after pid, ppid, comm in the original line.
		args := ""
		if len(fields) > 3 {
			// Find the start of the args portion by skipping the first 3 fields.
			idx := 0
			for skip := 0; skip < 3; skip++ {
				idx += strings.IndexFunc(line[idx:], func(r rune) bool { return r != ' ' })
				next := strings.IndexByte(line[idx:], ' ')
				if next < 0 {
					break
				}
				idx += next
			}
			if idx < len(line) {
				args = strings.TrimSpace(line[idx:])
			}
		}
		procs = append(procs, ProcessInfo{PID: pid, PPID: ppid, Comm: comm, Args: args})
	}
	return procs
}

// FindClaudePanes discovers tmux panes that have a Claude process running in them.
// It lists all tmux panes, builds a process tree from ps output, and checks
// each pane's descendant processes for a "claude" command or a "docker exec"
// into a container known to run Claude (identified by claudeContainerIDs).
func FindClaudePanes(ctx context.Context, client TmuxClient, lister ProcessLister, claudeContainerIDs []string) ([]TmuxPane, error) {
	panes, err := client.ListPanes(ctx)
	if err != nil {
		return nil, err
	}
	if len(panes) == 0 {
		return nil, nil
	}

	procs, err := lister.ListProcesses(ctx)
	if err != nil {
		return nil, nil // best-effort: no process list means no matches
	}

	// Build parent → children map and process info lookups.
	children := make(map[int][]int)
	info := make(map[int]ProcessInfo)
	for _, p := range procs {
		children[p.PPID] = append(children[p.PPID], p.PID)
		info[p.PID] = p
	}

	// Build container ID set for fast lookup.
	containerSet := make(map[string]bool, len(claudeContainerIDs))
	for _, id := range claudeContainerIDs {
		containerSet[id] = true
	}

	var matched []TmuxPane
	for _, pane := range panes {
		found, devcontainer, containerID, claudePID := findClaude(pane.PID, children, info, containerSet)
		if found {
			pane.Devcontainer = devcontainer
			pane.ContainerID = containerID
			pane.ClaudePID = claudePID
			matched = append(matched, pane)
		}
	}
	return matched, nil
}

// findClaude does a BFS over the process tree rooted at pid, returning whether
// a Claude process was found, whether it runs inside a devcontainer, the
// matched container ID (empty when running on the host), and the claude PID.
func findClaude(pid int, children map[int][]int, info map[int]ProcessInfo, containerIDs map[string]bool) (found, devcontainer bool, containerID string, claudePID int) {
	queue := children[pid]
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		p := info[cur]
		if p.Comm == "claude" {
			return true, false, "", cur
		}
		if p.Comm == "docker" {
			if cid := matchedDockerExecContainer(p.Args, containerIDs); cid != "" {
				return true, true, cid, 0
			}
		}
		queue = append(queue, children[cur]...)
	}
	return false, false, "", 0
}

// matchesDockerExec checks if args look like "docker exec ... <containerID>"
// where containerID is in the known set.
func matchesDockerExec(args string, containerIDs map[string]bool) bool {
	return matchedDockerExecContainer(args, containerIDs) != ""
}

// matchedDockerExecContainer returns the full container ID if args match
// "docker exec ... <containerID>" for a known container, or empty string.
func matchedDockerExecContainer(args string, containerIDs map[string]bool) string {
	if len(containerIDs) == 0 {
		return ""
	}
	fields := strings.Fields(args)
	// Look for "exec" as the docker subcommand, then check if any field
	// matches (or is a prefix of) a known container ID.
	foundExec := false
	for _, f := range fields {
		if f == "exec" {
			foundExec = true
			continue
		}
		if !foundExec {
			continue
		}
		for id := range containerIDs {
			if strings.HasPrefix(id, f) || strings.HasPrefix(f, id) {
				return id
			}
		}
	}
	return ""
}

// FormatTmuxPanes writes the tmux pane listing to w.
func FormatTmuxPanes(w io.Writer, panes []TmuxPane) error {
	if len(panes) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(w, "\nTmux panes running claude:\n"); err != nil {
		return err
	}
	for _, p := range panes {
		suffix := ""
		if p.Devcontainer {
			suffix = " (devcontainer)"
		}
		if _, err := fmt.Fprintf(w, "  %s %q (%d:%d)%s\n", p.State, p.SessionName, p.WindowIndex, p.PaneIndex, suffix); err != nil {
			return err
		}
	}
	return nil
}
