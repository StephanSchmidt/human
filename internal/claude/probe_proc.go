package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProcFS abstracts /proc filesystem access for testability.
type ProcFS interface {
	ReadFile(path string) ([]byte, error)
	ReadDir(path string) ([]os.DirEntry, error)
	Stat(path string) (os.FileInfo, error)
}

// OSProcFS implements ProcFS using the real filesystem.
type OSProcFS struct{}

func (OSProcFS) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path) // #nosec G304 — paths constructed from /proc/<pid>
}

func (OSProcFS) ReadDir(path string) ([]os.DirEntry, error) {
	return os.ReadDir(path)
}

func (OSProcFS) Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

// --- ProcessLivenessProbe (RC-7, RC-13) ---

// ProcessLivenessProbe checks whether a process is alive and is actually "claude".
type ProcessLivenessProbe struct {
	FS ProcFS
}

func (p *ProcessLivenessProbe) Name() string { return "process-liveness" }

func (p *ProcessLivenessProbe) Check(pid int, _ string) (*ProbeResult, error) {
	if pid <= 0 {
		return nil, nil // abstain — no PID
	}

	fs := p.fs()

	// Check /proc/<pid> exists.
	commPath := fmt.Sprintf("/proc/%d/comm", pid)
	data, err := fs.ReadFile(commPath)
	if err != nil {
		// Process doesn't exist.
		return &ProbeResult{
			State:      StateUnknown,
			Confidence: 1.0,
			Source:     "process-liveness",
		}, nil
	}

	// RC-7: Verify it's actually "claude".
	comm := strings.TrimSpace(string(data))
	if comm != "claude" {
		return &ProbeResult{
			State:      StateUnknown,
			Confidence: 1.0,
			Source:     "process-liveness",
		}, nil
	}

	// Process is alive and is claude — abstain so other probes determine state.
	return nil, nil
}

func (p *ProcessLivenessProbe) fs() ProcFS {
	if p.FS != nil {
		return p.FS
	}
	return OSProcFS{}
}

// --- ChildTreeProbe (A5) ---

// ChildTreeProbe checks if a claude process has child processes,
// indicating it's executing a tool (e.g., shell command).
type ChildTreeProbe struct {
	FS ProcFS
}

func (c *ChildTreeProbe) Name() string { return "child-tree" }

func (c *ChildTreeProbe) Check(pid int, _ string) (*ProbeResult, error) {
	if pid <= 0 {
		return nil, nil
	}

	fs := c.procFS()
	children := c.findChildren(fs, pid)
	if len(children) > 0 {
		return &ProbeResult{
			State:      StateBusy,
			Confidence: 0.9,
			Source:     "child-tree",
		}, nil
	}
	return nil, nil // abstain — no children doesn't mean ready
}

func (c *ChildTreeProbe) findChildren(fs ProcFS, ppid int) []int {
	entries, err := fs.ReadDir("/proc")
	if err != nil {
		return nil
	}

	ppidStr := strconv.Itoa(ppid)
	var children []int

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		childPID, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		statusPath := fmt.Sprintf("/proc/%d/status", childPID)
		data, err := fs.ReadFile(statusPath)
		if err != nil {
			continue
		}

		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "PPid:\t") {
				if strings.TrimPrefix(line, "PPid:\t") == ppidStr {
					children = append(children, childPID)
				}
				break
			}
		}
	}
	return children
}

func (c *ChildTreeProbe) procFS() ProcFS {
	if c.FS != nil {
		return c.FS
	}
	return OSProcFS{}
}

// --- CPUProbe (A2) ---

// CPUProbe checks whether a process is actively consuming CPU.
// If the process has significant CPU time delta, it's likely busy.
type CPUProbe struct {
	FS ProcFS

	mu       sync.Mutex
	prevStat map[int]cpuSnapshot
}

type cpuSnapshot struct {
	utime uint64
	stime uint64
	at    time.Time
}

func (cp *CPUProbe) Name() string { return "cpu" }

func (cp *CPUProbe) Check(pid int, _ string) (*ProbeResult, error) {
	if pid <= 0 {
		return nil, nil
	}

	fs := cp.procFS()

	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	data, err := fs.ReadFile(statPath)
	if err != nil {
		return nil, nil // abstain
	}

	utime, stime, err := parseProcStat(string(data))
	if err != nil {
		return nil, nil
	}

	now := time.Now()
	current := cpuSnapshot{utime: utime, stime: stime, at: now}

	cp.mu.Lock()
	if cp.prevStat == nil {
		cp.prevStat = make(map[int]cpuSnapshot)
	}
	prev, hasPrev := cp.prevStat[pid]
	cp.prevStat[pid] = current
	cp.mu.Unlock()

	if !hasPrev {
		return nil, nil // first reading, abstain
	}

	elapsed := now.Sub(prev.at).Seconds()
	if elapsed < 0.1 {
		return nil, nil // too close, abstain
	}

	// CPU ticks consumed (user + system).
	delta := float64((current.utime - prev.utime) + (current.stime - prev.stime))
	// Assume 100 ticks/sec (standard Linux HZ).
	cpuPct := (delta / 100.0) / elapsed

	if cpuPct > 0.05 { // >5% CPU usage
		return &ProbeResult{
			State:      StateBusy,
			Confidence: 0.7,
			Source:     "cpu",
		}, nil
	}

	return nil, nil // low CPU, abstain
}

// parseProcStat extracts utime and stime from /proc/<pid>/stat.
// Format: pid (comm) state ppid ... utime(14) stime(15) ...
func parseProcStat(data string) (utime, stime uint64, err error) {
	// Find the closing paren of comm field to handle spaces in command name.
	closeParen := strings.LastIndex(data, ")")
	if closeParen < 0 || closeParen+2 >= len(data) {
		return 0, 0, fmt.Errorf("malformed /proc/pid/stat")
	}

	fields := strings.Fields(data[closeParen+2:])
	// After ")" the fields are: state(0) ppid(1) ... utime(11) stime(12)
	if len(fields) < 13 {
		return 0, 0, fmt.Errorf("not enough fields in /proc/pid/stat")
	}

	utime, err = strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	stime, err = strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	return utime, stime, nil
}

func (cp *CPUProbe) procFS() ProcFS {
	if cp.FS != nil {
		return cp.FS
	}
	return OSProcFS{}
}

// --- MtimeProbe (A6) ---

// MtimeProbe uses file modification time as a cheap pre-filter.
// If the JSONL file mtime hasn't changed since last check, reuse cached state.
type MtimeProbe struct {
	FS ProcFS

	mu    sync.Mutex
	cache map[string]mtimeEntry
}

type mtimeEntry struct {
	mtime time.Time
	state InstanceState
}

func (m *MtimeProbe) Name() string { return "mtime" }

func (m *MtimeProbe) Check(_ int, jsonlPath string) (*ProbeResult, error) {
	if jsonlPath == "" {
		return nil, nil
	}

	fs := m.procFS()
	info, err := fs.Stat(jsonlPath)
	if err != nil {
		return nil, nil // file gone, abstain
	}

	mtime := info.ModTime()

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cache == nil {
		m.cache = make(map[string]mtimeEntry)
	}

	if cached, ok := m.cache[jsonlPath]; ok {
		if cached.mtime.Equal(mtime) {
			return &ProbeResult{
				State:      cached.state,
				Confidence: 0.6,
				Source:     "mtime",
			}, nil
		}
	}

	// Mtime changed — don't return a result (let JSONL probe re-read).
	return nil, nil
}

// Update stores the resolved state for a path so future mtime checks can use it.
func (m *MtimeProbe) Update(jsonlPath string, state InstanceState) {
	if jsonlPath == "" {
		return
	}

	fs := m.procFS()
	info, err := fs.Stat(jsonlPath)
	if err != nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cache == nil {
		m.cache = make(map[string]mtimeEntry)
	}
	m.cache[jsonlPath] = mtimeEntry{
		mtime: info.ModTime(),
		state: state,
	}
}

func (m *MtimeProbe) procFS() ProcFS {
	if m.FS != nil {
		return m.FS
	}
	return OSProcFS{}
}

// VerifyProcComm checks that /proc/<pid>/comm matches "claude".
// Used by HostFinder for RC-7 validation after pgrep.
func VerifyProcComm(fs ProcFS, pid int) bool {
	if fs == nil {
		fs = OSProcFS{}
	}
	commPath := filepath.Join("/proc", strconv.Itoa(pid), "comm")
	data, err := fs.ReadFile(commPath)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == "claude"
}

// IsSelfContainerized checks if the current process (PID 1) is running
// inside a container by inspecting /proc/1/cgroup. Used for RC-11 fix.
func IsSelfContainerized(fs ProcFS) bool {
	if fs == nil {
		fs = OSProcFS{}
	}
	data, err := fs.ReadFile("/proc/1/cgroup")
	if err != nil {
		return false
	}
	s := string(data)
	return strings.Contains(s, "docker") || strings.Contains(s, "containerd") ||
		strings.Contains(s, "/lxc/") || strings.Contains(s, "/kubepods")
}
