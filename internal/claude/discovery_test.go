package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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

// --- HostFinder tests ---

func TestHostFinder_NoProcesses(t *testing.T) {
	runner := &mockRunner{output: nil, err: errors.New("exit 1")}
	finder := &HostFinder{Runner: runner, HomeDir: "/home/testuser"}

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
	finder := &HostFinder{Runner: runner, HomeDir: "/home/testuser"}

	instances, err := finder.FindInstances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Should deduplicate to 1 instance (same home dir).
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}
	if instances[0].Source != "host" {
		t.Errorf("source = %q, want host", instances[0].Source)
	}
	if !strings.Contains(instances[0].Label, "PID 12345") {
		t.Errorf("label = %q, want PID 12345", instances[0].Label)
	}
	if !strings.HasSuffix(instances[0].Root, ".claude/projects") {
		t.Errorf("root = %q, want suffix .claude/projects", instances[0].Root)
	}
}

func TestHostFinder_IgnoresNonClaude(t *testing.T) {
	runner := &mockRunner{
		output: []byte("111 /usr/bin/human-claude\n222 /usr/bin/claude-helper\n"),
	}
	finder := &HostFinder{Runner: runner, HomeDir: "/home/testuser"}

	instances, err := finder.FindInstances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 0 {
		t.Errorf("expected 0 instances, got %d", len(instances))
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
			"abc123def456|sh":    {exitCode: 0, data: jsonlData},
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
		Runner:  &mockRunner{output: []byte("100 /usr/bin/claude\n")},
		HomeDir: "/home/user1",
	}
	f2 := &HostFinder{
		Runner:  &mockRunner{output: []byte("200 /usr/bin/claude\n")},
		HomeDir: "/home/user2",
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
		Runner:  &mockRunner{output: []byte("100 /usr/bin/claude\n")},
		HomeDir: "/home/user1",
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
