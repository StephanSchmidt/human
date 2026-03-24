package claude

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// --- mockProcFS ---

type mockProcFS struct {
	files map[string][]byte
	dirs  map[string][]os.DirEntry
	stats map[string]os.FileInfo
}

func (m *mockProcFS) ReadFile(path string) ([]byte, error) {
	if data, ok := m.files[path]; ok {
		return data, nil
	}
	return nil, fmt.Errorf("file not found: %s", path)
}

func (m *mockProcFS) ReadDir(path string) ([]os.DirEntry, error) {
	if entries, ok := m.dirs[path]; ok {
		return entries, nil
	}
	return nil, fmt.Errorf("dir not found: %s", path)
}

func (m *mockProcFS) Stat(path string) (os.FileInfo, error) {
	if info, ok := m.stats[path]; ok {
		return info, nil
	}
	return nil, fmt.Errorf("stat not found: %s", path)
}

// fakeDirEntry implements os.DirEntry for testing.
type fakeDirEntry struct {
	name  string
	isDir bool
}

func (f fakeDirEntry) Name() string               { return f.name }
func (f fakeDirEntry) IsDir() bool                 { return f.isDir }
func (f fakeDirEntry) Type() os.FileMode           { return 0 }
func (f fakeDirEntry) Info() (os.FileInfo, error)   { return nil, nil }

// fakeFileInfo implements os.FileInfo for testing.
type fakeFileInfo struct {
	modTime time.Time
}

func (f fakeFileInfo) Name() string      { return "" }
func (f fakeFileInfo) Size() int64       { return 0 }
func (f fakeFileInfo) Mode() os.FileMode { return 0 }
func (f fakeFileInfo) ModTime() time.Time { return f.modTime }
func (f fakeFileInfo) IsDir() bool       { return false }
func (f fakeFileInfo) Sys() interface{}  { return nil }

// --- ProcessLivenessProbe tests ---

func TestProcessLivenessProbe_Alive(t *testing.T) {
	fs := &mockProcFS{
		files: map[string][]byte{
			"/proc/123/comm": []byte("claude\n"),
		},
	}
	probe := &ProcessLivenessProbe{FS: fs}
	result, err := probe.Check(123, "")
	if err != nil {
		t.Fatal(err)
	}
	// Alive → abstain (nil), let other probes decide.
	if result != nil {
		t.Errorf("expected nil (abstain) for alive process, got %+v", result)
	}
}

func TestProcessLivenessProbe_Dead(t *testing.T) {
	fs := &mockProcFS{files: map[string][]byte{}}
	probe := &ProcessLivenessProbe{FS: fs}
	result, err := probe.Check(123, "")
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.State != StateUnknown {
		t.Errorf("got %v, want Unknown (dead process)", result.State)
	}
}

func TestProcessLivenessProbe_WrongComm(t *testing.T) {
	fs := &mockProcFS{
		files: map[string][]byte{
			"/proc/123/comm": []byte("not-claude\n"),
		},
	}
	probe := &ProcessLivenessProbe{FS: fs}
	result, err := probe.Check(123, "")
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.State != StateUnknown {
		t.Errorf("got %v, want Unknown (wrong comm)", result.State)
	}
}

func TestProcessLivenessProbe_NoPID(t *testing.T) {
	probe := &ProcessLivenessProbe{}
	result, err := probe.Check(0, "")
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Errorf("expected nil (abstain) for PID 0")
	}
}

// --- ChildTreeProbe tests ---

func TestChildTreeProbe_HasChildren(t *testing.T) {
	fs := &mockProcFS{
		dirs: map[string][]os.DirEntry{
			"/proc": {
				fakeDirEntry{name: "123", isDir: true},
				fakeDirEntry{name: "456", isDir: true},
			},
		},
		files: map[string][]byte{
			"/proc/123/status": []byte("Name:\tclaude\nPPid:\t1\n"),
			"/proc/456/status": []byte("Name:\tbash\nPPid:\t123\n"),
		},
	}
	probe := &ChildTreeProbe{FS: fs}
	result, err := probe.Check(123, "")
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.State != StateBusy {
		t.Errorf("got %v, want Busy (has children)", result.State)
	}
}

func TestChildTreeProbe_NoChildren(t *testing.T) {
	fs := &mockProcFS{
		dirs: map[string][]os.DirEntry{
			"/proc": {
				fakeDirEntry{name: "123", isDir: true},
				fakeDirEntry{name: "456", isDir: true},
			},
		},
		files: map[string][]byte{
			"/proc/123/status": []byte("Name:\tclaude\nPPid:\t1\n"),
			"/proc/456/status": []byte("Name:\tbash\nPPid:\t999\n"),
		},
	}
	probe := &ChildTreeProbe{FS: fs}
	result, err := probe.Check(123, "")
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Errorf("expected nil (abstain) when no children")
	}
}

func TestChildTreeProbe_NoPID(t *testing.T) {
	probe := &ChildTreeProbe{}
	result, err := probe.Check(0, "")
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Errorf("expected nil for PID 0")
	}
}

// --- CPUProbe tests ---

func TestCPUProbe_FirstReadAbstains(t *testing.T) {
	fs := &mockProcFS{
		files: map[string][]byte{
			"/proc/123/stat": []byte("123 (claude) S 1 123 123 0 0 0 0 0 0 0 1000 500 0 0 20 0 1 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0"),
		},
	}
	probe := &CPUProbe{FS: fs}
	result, err := probe.Check(123, "")
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Errorf("expected nil (abstain) on first read")
	}
}

func TestCPUProbe_NoPID(t *testing.T) {
	probe := &CPUProbe{}
	result, err := probe.Check(0, "")
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Errorf("expected nil for PID 0")
	}
}

// --- MtimeProbe tests ---

func TestMtimeProbe_CachedMtime(t *testing.T) {
	now := time.Now()
	fs := &mockProcFS{
		stats: map[string]os.FileInfo{
			"/tmp/test.jsonl": fakeFileInfo{modTime: now},
		},
	}
	probe := &MtimeProbe{FS: fs}

	// Store a cached entry.
	probe.Update("/tmp/test.jsonl", StateReady)

	result, err := probe.Check(0, "/tmp/test.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected cached result")
	}
	if result.State != StateReady {
		t.Errorf("got %v, want Ready (cached)", result.State)
	}
}

func TestMtimeProbe_MtimeChanged(t *testing.T) {
	now := time.Now()
	later := now.Add(time.Second)
	fs := &mockProcFS{
		stats: map[string]os.FileInfo{
			"/tmp/test.jsonl": fakeFileInfo{modTime: later},
		},
	}
	probe := &MtimeProbe{
		FS: fs,
		cache: map[string]mtimeEntry{
			"/tmp/test.jsonl": {mtime: now, state: StateReady},
		},
	}

	result, err := probe.Check(0, "/tmp/test.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Errorf("expected nil (mtime changed, need re-read)")
	}
}

func TestMtimeProbe_NoPath(t *testing.T) {
	probe := &MtimeProbe{}
	result, err := probe.Check(0, "")
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Errorf("expected nil for empty path")
	}
}

// --- parseProcStat tests ---

func TestParseProcStat(t *testing.T) {
	// Standard format: pid (comm) state ppid pgrp session tty_nr tpgid flags
	// minflt cminflt majflt cmajflt utime stime ...
	stat := "123 (claude) S 1 123 123 0 -1 4194304 100 0 0 0 5000 3000 0 0 20 0 1 0 12345 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0"
	utime, stime, err := parseProcStat(stat)
	if err != nil {
		t.Fatal(err)
	}
	if utime != 5000 {
		t.Errorf("utime = %d, want 5000", utime)
	}
	if stime != 3000 {
		t.Errorf("stime = %d, want 3000", stime)
	}
}

func TestParseProcStat_CommWithSpaces(t *testing.T) {
	stat := "123 (Web Content) S 1 123 123 0 -1 4194304 100 0 0 0 7000 2000 0 0 20 0 1 0 12345 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0"
	utime, stime, err := parseProcStat(stat)
	if err != nil {
		t.Fatal(err)
	}
	if utime != 7000 {
		t.Errorf("utime = %d, want 7000", utime)
	}
	if stime != 2000 {
		t.Errorf("stime = %d, want 2000", stime)
	}
}

func TestParseProcStat_Malformed(t *testing.T) {
	_, _, err := parseProcStat("not a valid stat line")
	if err == nil {
		t.Error("expected error for malformed stat")
	}
}

// --- VerifyProcComm tests ---

func TestVerifyProcComm_Match(t *testing.T) {
	fs := &mockProcFS{
		files: map[string][]byte{
			"/proc/100/comm": []byte("claude\n"),
		},
	}
	if !VerifyProcComm(fs, 100) {
		t.Error("expected true for matching comm")
	}
}

func TestVerifyProcComm_NoMatch(t *testing.T) {
	fs := &mockProcFS{
		files: map[string][]byte{
			"/proc/100/comm": []byte("bash\n"),
		},
	}
	if VerifyProcComm(fs, 100) {
		t.Error("expected false for non-matching comm")
	}
}

func TestVerifyProcComm_Missing(t *testing.T) {
	fs := &mockProcFS{files: map[string][]byte{}}
	if VerifyProcComm(fs, 100) {
		t.Error("expected false for missing /proc entry")
	}
}

// --- IsSelfContainerized tests ---

func TestIsSelfContainerized_Docker(t *testing.T) {
	fs := &mockProcFS{
		files: map[string][]byte{
			"/proc/1/cgroup": []byte("0::/docker/abc123\n"),
		},
	}
	if !IsSelfContainerized(fs) {
		t.Error("expected true for docker cgroup")
	}
}

func TestIsSelfContainerized_NotContainer(t *testing.T) {
	fs := &mockProcFS{
		files: map[string][]byte{
			"/proc/1/cgroup": []byte("0::/\n"),
		},
	}
	if IsSelfContainerized(fs) {
		t.Error("expected false for root cgroup")
	}
}
