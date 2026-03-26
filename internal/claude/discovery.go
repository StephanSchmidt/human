package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// MemoryInfo holds memory usage and limit for a container.
type MemoryInfo struct {
	Usage uint64 // current memory usage in bytes
	Limit uint64 // memory limit in bytes (0 = unlimited)
}

// Instance represents a discovered Claude Code instance.
type Instance struct {
	Label           string      // e.g. "Host (PID 7046)" or `Container "dev-myapp" (abc123)`
	Source          string      // "host" or "container"
	Walker          DirWalker   // how to read its JSONL data
	Root            string      // JSONL root path (or virtual path for containers)
	Memory          *MemoryInfo // memory usage (containers only)
	ContainerID     string      // full Docker container ID (containers only)
	PID             int         // host PID of the claude process (0 for containers)
	FilePath        string      // resolved JSONL path for fsnotify (host instances only)
	ProxyConfigured bool        // true if the instance is configured to use the daemon's HTTPS proxy
	DaemonConnected bool        // true if the instance has recently communicated with the daemon
}

// InstanceFinder discovers running Claude Code instances.
type InstanceFinder interface {
	FindInstances(ctx context.Context) ([]Instance, error)
}

// CommandRunner abstracts running external commands for testability.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// OSCommandRunner implements CommandRunner using os/exec.
type OSCommandRunner struct{}

func (OSCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output() // #nosec G204 — only called with hardcoded commands
}

// DockerClient abstracts Docker operations for testability.
type DockerClient interface {
	ListContainers(ctx context.Context) ([]ContainerInfo, error)
	Exec(ctx context.Context, containerID string, cmd []string) (int, io.Reader, error)
	ContainerStats(ctx context.Context, containerID string) (*MemoryInfo, error)
	Close() error
}

// ContainerInfo holds minimal container metadata.
type ContainerInfo struct {
	ID   string
	Name string
}

// ContainerChecker determines whether a process is running inside a container.
type ContainerChecker interface {
	IsContainerized(pid int) bool
}

// ProcContainerChecker reads /proc/<pid>/cgroup to detect containerized processes.
type ProcContainerChecker struct{}

func (ProcContainerChecker) IsContainerized(pid int) bool {
	// RC-11: If we ourselves are containerized, don't skip sibling processes.
	if isSelfContainerized() {
		return false
	}

	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return false
	}
	s := string(data)
	return strings.Contains(s, "docker") || strings.Contains(s, "containerd") ||
		strings.Contains(s, "/lxc/") || strings.Contains(s, "/kubepods")
}

// CwdResolver resolves the current working directory for a process.
type CwdResolver interface {
	ResolveCwd(pid int) (string, error)
}

// ProcCwdResolver reads /proc/<pid>/cwd (Linux).
type ProcCwdResolver struct{}

func (ProcCwdResolver) ResolveCwd(pid int) (string, error) {
	return os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
}

// SessionResolver resolves the session ID for a Claude process.
type SessionResolver interface {
	ResolveSessionID(pid int) (string, error)
}

// FileSessionResolver reads session info from ~/.claude/sessions/<PID>.json.
type FileSessionResolver struct {
	HomeDir string
}

func (r FileSessionResolver) ResolveSessionID(pid int) (string, error) {
	path := filepath.Join(r.HomeDir, ".claude", "sessions", fmt.Sprintf("%d.json", pid))
	data, err := os.ReadFile(path) // #nosec G304 — path constructed from trusted home dir + PID
	if err != nil {
		return "", err
	}
	var session struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(data, &session); err != nil {
		return "", err
	}
	if session.SessionID == "" {
		return "", fmt.Errorf("empty sessionId in %s", path)
	}
	return session.SessionID, nil
}

// resolveJSONLPath finds the active JSONL for a PID.
// It tries the session file first (needed when multiple Claudes share a project dir),
// then falls back to the newest JSONL by mtime when the session is stale or missing —
// Claude creates new conversations without updating the session file.
func resolveJSONLPath(sess SessionResolver, pid int, root string) string {
	if sessionID, err := sess.ResolveSessionID(pid); err == nil {
		sessionPath := filepath.Clean(filepath.Join(root, sessionID+".jsonl"))
		if _, fErr := os.Stat(sessionPath); fErr == nil { // #nosec G703 -- root is the Claude config dir, sessionID from local session file
			return sessionPath
		}
	}
	if newest, err := findNewestJSONL(root); err == nil && newest != "" {
		return newest
	}
	return ""
}

// CwdToProjectDir converts an absolute cwd to the Claude project subdir name.
// e.g. "/home/user/project" -> "-home-user-project"
func CwdToProjectDir(cwd string) string {
	return strings.ReplaceAll(cwd, string(os.PathSeparator), "-")
}

// ShortProjectName returns the last two path components for a readable label.
// e.g. "/home/user/dev/myproject" -> "dev/myproject"
func ShortProjectName(cwd string) string {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(cwd)), "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return cwd
}

// CommChecker verifies a process command name.
type CommChecker func(pid int) bool

// HostFinder discovers Claude Code instances on the local host via pgrep.
type HostFinder struct {
	Runner           CommandRunner
	HomeDir          string           // override for testing; empty uses os.UserHomeDir result passed externally
	CwdResolver      CwdResolver      // nil defaults to ProcCwdResolver
	ContainerChecker ContainerChecker // nil defaults to ProcContainerChecker
	SessionResolver  SessionResolver  // nil defaults to FileSessionResolver{HomeDir: h.HomeDir}
	CommChecker      CommChecker      // nil defaults to verifyProcComm
}

func (h *HostFinder) FindInstances(ctx context.Context) ([]Instance, error) {
	out, err := h.Runner.Run(ctx, "pgrep", "-a", "claude")
	if err != nil {
		// pgrep exits 1 when no matches — not an error for us.
		return nil, nil
	}

	resolver := h.CwdResolver
	if resolver == nil {
		resolver = ProcCwdResolver{}
	}

	ctrChecker := h.ContainerChecker
	if ctrChecker == nil {
		ctrChecker = ProcContainerChecker{}
	}

	sessResolver := h.SessionResolver
	if sessResolver == nil {
		sessResolver = FileSessionResolver{HomeDir: h.HomeDir}
	}

	var instances []Instance

	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			continue
		}
		pid := parts[0]
		cmdLine := parts[1]

		// Extract the basename of the first token in the command.
		cmdParts := strings.Fields(cmdLine)
		if len(cmdParts) == 0 {
			continue
		}
		base := filepath.Base(cmdParts[0])
		if base != "claude" {
			continue
		}

		pidNum, err := strconv.Atoi(pid)
		if err != nil {
			continue
		}

		commCheck := h.CommChecker
		if commCheck == nil {
			commCheck = verifyProcComm
		}
		if !commCheck(pidNum) {
			log.Debug().Int("pid", pidNum).Msg("proc comm mismatch, skipping")
			continue
		}

		// Skip processes running inside containers — DockerFinder handles those.
		if ctrChecker.IsContainerized(pidNum) {
			log.Trace().Int("pid", pidNum).Msg("skipping containerized process")
			continue
		}

		// Resolve the working directory for this PID.
		cwd, err := resolver.ResolveCwd(pidNum)
		if err != nil {
			log.Debug().Err(err).Int("pid", pidNum).Msg("cannot resolve cwd, skipping")
			continue
		}

		projectDir := CwdToProjectDir(cwd)
		root := filepath.Join(h.HomeDir, ".claude", "projects", projectDir)
		label := fmt.Sprintf("Host: %s (PID %s)", ShortProjectName(cwd), pid)

		filePath := resolveJSONLPath(sessResolver, pidNum, root)

		instances = append(instances, Instance{
			Label:    label,
			Source:   "host",
			Walker:   OSDirWalker{},
			Root:     root,
			PID:      pidNum,
			FilePath: filePath,
		})
	}
	return instances, nil
}

// verifyProcComm checks that /proc/<pid>/comm matches "claude".
func verifyProcComm(pid int) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid)) // #nosec G703 -- pid is an integer from os process listing
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == "claude"
}

// isSelfContainerized checks if the current process is running inside a container.
func isSelfContainerized() bool {
	data, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return false
	}
	s := string(data)
	return strings.Contains(s, "docker") || strings.Contains(s, "containerd") ||
		strings.Contains(s, "/lxc/") || strings.Contains(s, "/kubepods")
}

// dockerCacheEntry holds cached container JSONL data with a TTL.
type dockerCacheEntry struct {
	data      []byte
	fetchedAt time.Time
}

// DockerFinder discovers Claude Code instances inside Docker containers.
type DockerFinder struct {
	Client   DockerClient
	CacheTTL time.Duration // TTL for container data cache; defaults to 2s

	mu    sync.Mutex
	cache map[string]*dockerCacheEntry
}

func (d *DockerFinder) FindInstances(ctx context.Context) ([]Instance, error) {
	containers, err := d.Client.ListContainers(ctx)
	if err != nil {
		return nil, err
	}

	ttl := d.CacheTTL
	if ttl == 0 {
		ttl = 2 * time.Second
	}

	var instances []Instance
	for _, ctr := range containers {
		// Check if claude is running in this container.
		exitCode, _, err := d.Client.Exec(ctx, ctr.ID, []string{"pgrep", "-x", "claude"})
		if err != nil || exitCode != 0 {
			continue
		}

		// RC-4: Check TTL cache before executing docker exec.
		data := d.getCached(ctr.ID, ttl)
		if data == nil {
			data = d.fetchContainerData(ctx, ctr.ID)
			if data == nil {
				continue
			}
			d.putCache(ctr.ID, data)
		}

		shortID := ctr.ID
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}
		name := ctr.Name
		if name == "" {
			name = shortID
		}

		mem, _ := d.Client.ContainerStats(ctx, ctr.ID)

		// Check if the container has the HTTPS proxy configured.
		proxyExit, _, _ := d.Client.Exec(ctx, ctr.ID, []string{"printenv", "HUMAN_PROXY_ADDR"})
		proxyConfigured := proxyExit == 0

		instances = append(instances, Instance{
			Label:           fmt.Sprintf("Container %q (%s)", name, shortID),
			Source:          "container",
			Walker:          &ByteWalker{Data: data},
			Root:            "/container/" + shortID,
			Memory:          mem,
			ContainerID:     ctr.ID,
			ProxyConfigured: proxyConfigured,
		})
	}
	return instances, nil
}

func (d *DockerFinder) fetchContainerData(ctx context.Context, containerID string) []byte {
	// List JSONL files with modification times from the container.
	_, listReader, err := d.Client.Exec(ctx, containerID, []string{
		"sh", "-c",
		"find /root/.claude/projects /home -maxdepth 6 -name '*.jsonl' -exec stat -c '%Y %n' {} + 2>/dev/null",
	})
	if err != nil {
		return nil
	}

	listData, err := io.ReadAll(listReader)
	if err != nil {
		return nil
	}

	sortedFiles := sortFilesByMtime(listData)
	if len(sortedFiles) == 0 {
		return nil
	}

	catArgs := append([]string{"cat"}, sortedFiles...)
	_, catReader, err := d.Client.Exec(ctx, containerID, catArgs)
	if err != nil {
		return nil
	}

	data, err := io.ReadAll(catReader)
	if err != nil {
		return nil
	}
	return data
}

func (d *DockerFinder) getCached(containerID string, ttl time.Duration) []byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cache == nil {
		return nil
	}
	entry, ok := d.cache[containerID]
	if !ok {
		return nil
	}
	if time.Since(entry.fetchedAt) > ttl {
		delete(d.cache, containerID)
		return nil
	}
	return entry.data
}

func (d *DockerFinder) putCache(containerID string, data []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cache == nil {
		d.cache = make(map[string]*dockerCacheEntry)
	}
	d.cache[containerID] = &dockerCacheEntry{
		data:      data,
		fetchedAt: time.Now(),
	}
}

// sortFilesByMtime parses `stat -c '%Y %n'` output and returns file paths
// sorted by modification time (oldest first, newest last).
func sortFilesByMtime(data []byte) []string {
	type timedFile struct {
		mtime int64
		path  string
	}

	var files []timedFile
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		parts := bytes.SplitN(line, []byte(" "), 2)
		if len(parts) != 2 {
			continue
		}
		mtime, err := strconv.ParseInt(string(parts[0]), 10, 64)
		if err != nil {
			continue
		}
		files = append(files, timedFile{mtime: mtime, path: string(parts[1])})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].mtime < files[j].mtime
	})

	result := make([]string, len(files))
	for i, f := range files {
		result[i] = f.path
	}
	return result
}

// CombinedFinder aggregates multiple InstanceFinders, logging and skipping failures.
type CombinedFinder struct {
	Finders []InstanceFinder
}

func (c *CombinedFinder) FindInstances(ctx context.Context) ([]Instance, error) {
	var all []Instance
	for _, f := range c.Finders {
		instances, err := f.FindInstances(ctx)
		if err != nil {
			log.Debug().Err(err).Msg("instance finder failed, skipping")
			continue
		}
		all = append(all, instances...)
	}
	return all, nil
}

// DirWalker abstracts walking JSONL files for testability.
type DirWalker interface {
	WalkJSONL(root string, fn func(line []byte) error) error
}

// OSDirWalker implements DirWalker using the real filesystem.
type OSDirWalker struct{}

func (OSDirWalker) WalkJSONL(root string, fn func(line []byte) error) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible dirs
		}
		if info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		f, err := os.Open(filepath.Clean(path))
		if err != nil {
			return nil // skip unreadable files
		}
		defer func() { _ = f.Close() }()
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
		for scanner.Scan() {
			if err := fn(scanner.Bytes()); err != nil {
				return err
			}
		}
		return nil
	})
}

// findNewestJSONL finds the most recently modified .jsonl file under root.
// When two files have mtimes within 1 second, the lexicographically later path
// wins as a stable tiebreaker (session IDs sort chronologically).
func findNewestJSONL(root string) (string, error) {
	var newest string
	var newestMod int64

	const mtimeTolerance = int64(time.Second)

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error { // #nosec G703 -- root is the Claude config dir
		if err != nil {
			return nil // skip inaccessible
		}
		if info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		mod := info.ModTime().UnixNano()
		switch {
		case newestMod == 0:
			newestMod = mod
			newest = path
		case mod > newestMod+mtimeTolerance:
			// Clearly newer — pick it.
			newestMod = mod
			newest = path
		case mod >= newestMod-mtimeTolerance:
			// Within tolerance window — use lexicographic tiebreaker for stability.
			if path > newest {
				newestMod = mod
				newest = path
			}
		}
		return nil
	})
	return newest, err
}

// ByteWalker implements DirWalker over in-memory bytes (one JSONL line per text line).
type ByteWalker struct {
	Data []byte
}

func (b *ByteWalker) WalkJSONL(_ string, fn func(line []byte) error) error {
	scanner := bufio.NewScanner(bytes.NewReader(b.Data))
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if err := fn(line); err != nil {
			return err
		}
	}
	return nil
}
