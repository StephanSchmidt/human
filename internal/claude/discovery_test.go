package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- mock CommandRunner ---

type mockRunner struct {
	output []byte
	err    error
}

func (m *mockRunner) Run(_ context.Context, _ string, _ ...string) ([]byte, error) {
	return m.output, m.err
}

// --- mock DockerClient ---

type mockStatsResult struct {
	mem *MemoryInfo
	err error
}

type mockDockerClient struct {
	containers []ContainerInfo
	listErr    error
	// execResults maps containerID+cmd-key to result
	execResults  map[string]mockExecResult
	statsResults map[string]mockStatsResult
}

type mockExecResult struct {
	exitCode int
	data     []byte
	err      error
}

func (m *mockDockerClient) ListContainers(_ context.Context) ([]ContainerInfo, error) {
	return m.containers, m.listErr
}

func (m *mockDockerClient) Exec(_ context.Context, containerID string, cmd []string) (int, io.Reader, error) {
	key := containerID + "|" + strings.Join(cmd, " ")
	for k, v := range m.execResults {
		if strings.Contains(key, k) {
			return v.exitCode, bytes.NewReader(v.data), v.err
		}
	}
	return 1, nil, errors.New("no exec result configured")
}

func (m *mockDockerClient) ContainerStats(_ context.Context, containerID string) (*MemoryInfo, error) {
	if m.statsResults == nil {
		return nil, errors.New("no stats configured")
	}
	if result, ok := m.statsResults[containerID]; ok {
		return result.mem, result.err
	}
	return nil, errors.New("no stats for container")
}

func (m *mockDockerClient) Close() error { return nil }

// --- mock ContainerChecker ---

type mockContainerChecker struct {
	containerized map[int]bool
}

func (m *mockContainerChecker) IsContainerized(pid int) bool {
	return m.containerized[pid]
}

// --- mock CwdResolver ---

type mockCwdResolver struct {
	cwds map[int]string
}

func (m *mockCwdResolver) ResolveCwd(pid int) (string, error) {
	cwd, ok := m.cwds[pid]
	if !ok {
		return "", fmt.Errorf("no cwd for PID %d", pid)
	}
	return cwd, nil
}

// alwaysClaude is a CommChecker that always returns true (for tests).
func alwaysClaude(_ int) bool { return true }

// --- mock SessionResolver ---

type mockSessionResolver struct {
	sessions map[int]string
}

func (m *mockSessionResolver) ResolveSessionID(pid int) (string, error) {
	sid, ok := m.sessions[pid]
	if !ok {
		return "", fmt.Errorf("no session for PID %d", pid)
	}
	return sid, nil
}

// --- HostFinder tests ---

func TestHostFinder_NoProcesses(t *testing.T) {
	runner := &mockRunner{output: nil, err: errors.New("exit 1")}
	finder := &HostFinder{Runner: runner, HomeDir: "/home/testuser", CommChecker: alwaysClaude}

	instances, err := finder.FindInstances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 0 {
		t.Errorf("expected 0 instances, got %d", len(instances))
	}
}

func TestHostFinder_FindsClaude(t *testing.T) {
	runner := &mockRunner{
		output: []byte("12345 /usr/bin/claude --some-flag\n67890 /usr/bin/claude\n"),
	}
	resolver := &mockCwdResolver{
		cwds: map[int]string{
			12345: "/home/testuser/projects/alpha",
			67890: "/home/testuser/projects/beta",
		},
	}
	finder := &HostFinder{Runner: runner, HomeDir: "/home/testuser", CwdResolver: resolver, CommChecker: alwaysClaude}

	instances, err := finder.FindInstances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(instances))
	}
	if instances[0].Source != "host" {
		t.Errorf("source = %q, want host", instances[0].Source)
	}
	if !strings.Contains(instances[0].Label, "projects/alpha") {
		t.Errorf("label = %q, want to contain projects/alpha", instances[0].Label)
	}
	if !strings.Contains(instances[0].Label, "PID 12345") {
		t.Errorf("label = %q, want to contain PID 12345", instances[0].Label)
	}
	if instances[0].PID != 12345 {
		t.Errorf("PID = %d, want 12345", instances[0].PID)
	}
	if !strings.HasSuffix(instances[0].Root, "-home-testuser-projects-alpha") {
		t.Errorf("root = %q, want suffix -home-testuser-projects-alpha", instances[0].Root)
	}
	if !strings.Contains(instances[1].Label, "projects/beta") {
		t.Errorf("label = %q, want to contain projects/beta", instances[1].Label)
	}
	if instances[1].PID != 67890 {
		t.Errorf("PID = %d, want 67890", instances[1].PID)
	}
}

func TestHostFinder_IgnoresNonClaude(t *testing.T) {
	runner := &mockRunner{
		output: []byte("111 /usr/bin/human-claude\n222 /usr/bin/claude-helper\n"),
	}
	finder := &HostFinder{Runner: runner, HomeDir: "/home/testuser", CommChecker: alwaysClaude}

	instances, err := finder.FindInstances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 0 {
		t.Errorf("expected 0 instances, got %d", len(instances))
	}
}

func TestHostFinder_CwdResolutionFails(t *testing.T) {
	runner := &mockRunner{
		output: []byte("12345 /usr/bin/claude\n67890 /usr/bin/claude\n"),
	}
	resolver := &mockCwdResolver{
		cwds: map[int]string{
			12345: "/home/testuser/projects/alpha",
			// 67890 not present — resolution will fail
		},
	}
	finder := &HostFinder{Runner: runner, HomeDir: "/home/testuser", CwdResolver: resolver, CommChecker: alwaysClaude}

	instances, err := finder.FindInstances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance (skip unresolvable), got %d", len(instances))
	}
	if !strings.Contains(instances[0].Label, "PID 12345") {
		t.Errorf("label = %q, want PID 12345", instances[0].Label)
	}
}

func TestHostFinder_SameProject(t *testing.T) {
	runner := &mockRunner{
		output: []byte("12345 /usr/bin/claude\n67890 /usr/bin/claude\n"),
	}
	resolver := &mockCwdResolver{
		cwds: map[int]string{
			12345: "/home/testuser/projects/alpha",
			67890: "/home/testuser/projects/alpha",
		},
	}
	finder := &HostFinder{Runner: runner, HomeDir: "/home/testuser", CwdResolver: resolver, CommChecker: alwaysClaude}

	instances, err := finder.FindInstances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Both should appear even though they share the same project dir.
	if len(instances) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(instances))
	}
	if instances[0].Root != instances[1].Root {
		t.Errorf("roots should match: %q vs %q", instances[0].Root, instances[1].Root)
	}
	if instances[0].Label == instances[1].Label {
		t.Errorf("labels should differ (different PIDs): %q", instances[0].Label)
	}
}

func TestHostFinder_SkipsContainerizedProcesses(t *testing.T) {
	runner := &mockRunner{
		output: []byte("12345 /usr/bin/claude\n67890 /usr/bin/claude\n"),
	}
	resolver := &mockCwdResolver{
		cwds: map[int]string{
			12345: "/home/testuser/projects/alpha",
			67890: "/workspaces/cli",
		},
	}
	checker := &mockContainerChecker{
		containerized: map[int]bool{
			67890: true, // this PID is inside a container
		},
	}
	finder := &HostFinder{
		Runner:           runner,
		HomeDir:          "/home/testuser",
		CwdResolver:      resolver,
		ContainerChecker: checker,
		CommChecker:      alwaysClaude,
	}

	instances, err := finder.FindInstances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance (skip containerized), got %d", len(instances))
	}
	if !strings.Contains(instances[0].Label, "PID 12345") {
		t.Errorf("label = %q, want PID 12345 (non-containerized)", instances[0].Label)
	}
}

func TestHostFinder_SessionResolvesToJSONL(t *testing.T) {
	// When session resolves and the JSONL exists, use it.
	homeDir := t.TempDir()
	projectDir := CwdToProjectDir("/home/testuser/projects/alpha")
	root := filepath.Join(homeDir, ".claude", "projects", projectDir)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	sessionID := "abc-session-123"
	sessionFile := filepath.Join(root, sessionID+".jsonl")
	if err := os.WriteFile(sessionFile, []byte("{\"type\":\"assistant\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	runner := &mockRunner{output: []byte("12345 /usr/bin/claude\n")}
	resolver := &mockCwdResolver{cwds: map[int]string{12345: "/home/testuser/projects/alpha"}}
	sessResolver := &mockSessionResolver{sessions: map[int]string{12345: sessionID}}

	finder := &HostFinder{
		Runner:          runner,
		HomeDir:         homeDir,
		CwdResolver:     resolver,
		SessionResolver: sessResolver,
		CommChecker:     alwaysClaude,
	}

	instances, err := finder.FindInstances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}
	if instances[0].FilePath != sessionFile {
		t.Errorf("FilePath = %q, want %q (session-resolved)", instances[0].FilePath, sessionFile)
	}
}

func TestHostFinder_StaleSessionFallsBackToNewest(t *testing.T) {
	// Session resolves to a missing JSONL (stale). Should fall back to newest.
	homeDir := t.TempDir()
	projectDir := CwdToProjectDir("/home/testuser/projects/alpha")
	root := filepath.Join(homeDir, ".claude", "projects", projectDir)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a JSONL for a different (newer) session.
	newestFile := filepath.Join(root, "newest-session.jsonl")
	if err := os.WriteFile(newestFile, []byte("{\"type\":\"assistant\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	runner := &mockRunner{output: []byte("12345 /usr/bin/claude\n")}
	resolver := &mockCwdResolver{cwds: map[int]string{12345: "/home/testuser/projects/alpha"}}
	// Session points to a JSONL that doesn't exist.
	sessResolver := &mockSessionResolver{sessions: map[int]string{12345: "stale-gone-session"}}

	finder := &HostFinder{
		Runner:          runner,
		HomeDir:         homeDir,
		CwdResolver:     resolver,
		SessionResolver: sessResolver,
		CommChecker:     alwaysClaude,
	}

	instances, err := finder.FindInstances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}
	if instances[0].FilePath != newestFile {
		t.Errorf("FilePath = %q, want %q (fallback to newest)", instances[0].FilePath, newestFile)
	}
}

func TestHostFinder_NoJSONL(t *testing.T) {
	// When no JSONL exists at all, instance should still be created.
	runner := &mockRunner{output: []byte("12345 /usr/bin/claude\n")}
	resolver := &mockCwdResolver{cwds: map[int]string{12345: "/home/testuser/projects/alpha"}}

	finder := &HostFinder{
		Runner:      runner,
		HomeDir:     t.TempDir(),
		CwdResolver: resolver,
		CommChecker: alwaysClaude,
	}

	instances, err := finder.FindInstances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}
	if instances[0].Source != "host" {
		t.Errorf("Source = %q, want host", instances[0].Source)
	}
}

// --- DockerFinder tests ---

func makeJSONLLine(t *testing.T, model string, ts time.Time, input, output int) []byte {
	t.Helper()
	m := map[string]interface{}{
		"type":      "assistant",
		"timestamp": ts.Format(time.RFC3339),
		"message": map[string]interface{}{
			"model": model,
			"usage": map[string]int{
				"input_tokens":                input,
				"output_tokens":               output,
				"cache_creation_input_tokens": 0,
				"cache_read_input_tokens":     0,
			},
		},
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDockerFinder_FindsContainerWithClaude(t *testing.T) {
	inWindow := time.Date(2026, 3, 20, 11, 0, 0, 0, time.UTC)
	jsonlData := makeJSONLLine(t, "claude-opus-4-6", inWindow, 500_000, 200_000)

	dc := &mockDockerClient{
		containers: []ContainerInfo{
			{ID: "abc123def456", Name: "dev-myapp"},
		},
		execResults: map[string]mockExecResult{
			"abc123def456|pgrep": {exitCode: 0, data: []byte("1\n")},
			"abc123def456|sh":    {exitCode: 0, data: []byte("1711000000 /root/.claude/projects/session.jsonl\n")},
			"abc123def456|cat":   {exitCode: 0, data: jsonlData},
		},
		statsResults: map[string]mockStatsResult{
			"abc123def456": {mem: &MemoryInfo{Usage: 512 * 1024 * 1024, Limit: 2 * 1024 * 1024 * 1024}},
		},
	}

	finder := &DockerFinder{Client: dc}
	instances, err := finder.FindInstances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}
	if instances[0].Source != "container" {
		t.Errorf("source = %q, want container", instances[0].Source)
	}
	if !strings.Contains(instances[0].Label, "dev-myapp") {
		t.Errorf("label = %q, want to contain dev-myapp", instances[0].Label)
	}
	if !strings.Contains(instances[0].Label, "abc123def456") {
		t.Errorf("label = %q, want to contain abc123def456", instances[0].Label)
	}
	if instances[0].Memory == nil {
		t.Fatal("expected memory info, got nil")
	}
	if instances[0].Memory.Usage != 512*1024*1024 {
		t.Errorf("memory usage = %d, want %d", instances[0].Memory.Usage, 512*1024*1024)
	}
	if instances[0].Memory.Limit != 2*1024*1024*1024 {
		t.Errorf("memory limit = %d, want %d", instances[0].Memory.Limit, 2*1024*1024*1024)
	}
}

func TestDockerFinder_StateUsesNewestFile(t *testing.T) {
	// Simulate two JSONL sessions: an older completed one (end_turn) and
	// a newer active one (streaming assistant, null stop_reason).
	// After sorting by mtime, the newer (active) file should be last,
	// so DetermineState should return Busy.

	endTurn := "end_turn"
	oldSession, _ := json.Marshal(map[string]interface{}{
		"type": "assistant",
		"message": map[string]interface{}{
			"stop_reason": &endTurn,
		},
	})
	// Streaming assistant — null stop_reason means actively generating.
	newSession, _ := json.Marshal(map[string]interface{}{
		"type": "assistant",
		"message": map[string]interface{}{},
	})

	// File listing: newer file (mtime 2000) and older file (mtime 1000)
	// returned in wrong order by filesystem.
	fileList := "2000 /root/.claude/projects/new-session.jsonl\n1000 /root/.claude/projects/old-session.jsonl\n"

	// Cat will be called with files sorted oldest-first: old then new.
	// So concatenated data = oldSession + "\n" + newSession + "\n"
	catData := append(oldSession, '\n')
	catData = append(catData, newSession...)
	catData = append(catData, '\n')

	dc := &mockDockerClient{
		containers: []ContainerInfo{
			{ID: "statetest12345", Name: "state-test"},
		},
		execResults: map[string]mockExecResult{
			"statetest12345|pgrep": {exitCode: 0, data: []byte("1\n")},
			"statetest12345|sh":    {exitCode: 0, data: []byte(fileList)},
			"statetest12345|cat":   {exitCode: 0, data: catData},
		},
	}

	finder := &DockerFinder{Client: dc}
	instances, err := finder.FindInstances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}

	if instances[0].Source != "container" {
		t.Errorf("Source = %q, want container", instances[0].Source)
	}
}

func TestSortFilesByMtime(t *testing.T) {
	input := []byte("1711000300 /path/c.jsonl\n1711000100 /path/a.jsonl\n1711000200 /path/b.jsonl\n")
	got := sortFilesByMtime(input)
	want := []string{"/path/a.jsonl", "/path/b.jsonl", "/path/c.jsonl"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSortFilesByMtime_Empty(t *testing.T) {
	got := sortFilesByMtime(nil)
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestDockerFinder_SkipsContainerWithoutClaude(t *testing.T) {
	dc := &mockDockerClient{
		containers: []ContainerInfo{
			{ID: "xyz789", Name: "no-claude"},
		},
		execResults: map[string]mockExecResult{
			"xyz789|pgrep": {exitCode: 1, data: nil},
		},
	}

	finder := &DockerFinder{Client: dc}
	instances, err := finder.FindInstances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 0 {
		t.Errorf("expected 0 instances, got %d", len(instances))
	}
}

func TestDockerFinder_ListError(t *testing.T) {
	dc := &mockDockerClient{
		listErr: errors.New("docker not available"),
	}

	finder := &DockerFinder{Client: dc}
	_, err := finder.FindInstances(context.Background())
	if err == nil {
		t.Fatal("expected error from ListContainers")
	}
}

// --- CombinedFinder tests ---

func TestCombinedFinder_AggregatesResults(t *testing.T) {
	f1 := &HostFinder{
		Runner:      &mockRunner{output: []byte("100 /usr/bin/claude\n")},
		HomeDir:     "/home/user1",
		CwdResolver: &mockCwdResolver{cwds: map[int]string{100: "/home/user1/project-a"}},
		CommChecker: alwaysClaude,
	}
	f2 := &HostFinder{
		Runner:      &mockRunner{output: []byte("200 /usr/bin/claude\n")},
		HomeDir:     "/home/user2",
		CwdResolver: &mockCwdResolver{cwds: map[int]string{200: "/home/user2/project-b"}},
		CommChecker: alwaysClaude,
	}

	combined := &CombinedFinder{Finders: []InstanceFinder{f1, f2}}
	instances, err := combined.FindInstances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 2 {
		t.Errorf("expected 2 instances, got %d", len(instances))
	}
}

func TestCombinedFinder_SkipsFailingFinder(t *testing.T) {
	good := &HostFinder{
		Runner:      &mockRunner{output: []byte("100 /usr/bin/claude\n")},
		HomeDir:     "/home/user1",
		CwdResolver: &mockCwdResolver{cwds: map[int]string{100: "/home/user1/project-a"}},
		CommChecker: alwaysClaude,
	}
	bad := &DockerFinder{
		Client: &mockDockerClient{listErr: errors.New("docker down")},
	}

	combined := &CombinedFinder{Finders: []InstanceFinder{bad, good}}
	instances, err := combined.FindInstances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Errorf("expected 1 instance, got %d", len(instances))
	}
}

// --- ByteWalker tests ---

func TestByteWalker_ParsesLines(t *testing.T) {
	data := []byte("line1\nline2\nline3\n")
	bw := &ByteWalker{Data: data}

	var lines []string
	err := bw.WalkJSONL("", func(line []byte) error {
		lines = append(lines, string(line))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}
}

func TestByteWalker_SkipsEmptyLines(t *testing.T) {
	data := []byte("line1\n\nline2\n")
	bw := &ByteWalker{Data: data}

	var count int
	err := bw.WalkJSONL("", func(_ []byte) error {
		count++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("expected 2 lines, got %d", count)
	}
}

func TestByteWalker_PropagatesError(t *testing.T) {
	data := []byte("line1\nline2\n")
	bw := &ByteWalker{Data: data}

	sentinel := errors.New("stop")
	err := bw.WalkJSONL("", func(_ []byte) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
}

func TestByteWalker_Empty(t *testing.T) {
	bw := &ByteWalker{Data: nil}
	var count int
	err := bw.WalkJSONL("", func(_ []byte) error {
		count++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected 0 lines, got %d", count)
	}
}

// --- helper function tests ---

func TestCwdToProjectDir(t *testing.T) {
	tests := []struct {
		cwd  string
		want string
	}{
		{"/home/user/project", "-home-user-project"},
		{"/home/user/dev/myapp", "-home-user-dev-myapp"},
		{"/", "-"},
	}
	for _, tt := range tests {
		got := CwdToProjectDir(tt.cwd)
		if got != tt.want {
			t.Errorf("CwdToProjectDir(%q) = %q, want %q", tt.cwd, got, tt.want)
		}
	}
}

func TestShortProjectName(t *testing.T) {
	tests := []struct {
		cwd  string
		want string
	}{
		{"/home/user/dev/myproject", "dev/myproject"},
		{"/home/user", "home/user"},
		{"/single", "/single"},
	}
	for _, tt := range tests {
		got := ShortProjectName(tt.cwd)
		if got != tt.want {
			t.Errorf("ShortProjectName(%q) = %q, want %q", tt.cwd, got, tt.want)
		}
	}
}
