package claude

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// --- mock TmuxClient ---

type mockTmuxClient struct {
	panes []TmuxPane
	err   error
}

func (m *mockTmuxClient) ListPanes(_ context.Context) ([]TmuxPane, error) {
	return m.panes, m.err
}

// --- mock ProcessLister ---

type mockProcessLister struct {
	procs []ProcessInfo
	err   error
}

func (m *mockProcessLister) ListProcesses(_ context.Context) ([]ProcessInfo, error) {
	return m.procs, m.err
}

// --- parseTmuxOutput tests ---

func TestParseTmuxOutput_WellFormed(t *testing.T) {
	input := "42\tdev\t0\t0\t/home/user/project\n99\tops\t1\t2\t/tmp\n"
	panes := parseTmuxOutput([]byte(input))
	if len(panes) != 2 {
		t.Fatalf("expected 2 panes, got %d", len(panes))
	}
	if panes[0].PID != 42 || panes[0].SessionName != "dev" || panes[0].WindowIndex != 0 || panes[0].PaneIndex != 0 {
		t.Errorf("pane[0] = %+v", panes[0])
	}
	if panes[0].Cwd != "/home/user/project" {
		t.Errorf("pane[0].Cwd = %q, want /home/user/project", panes[0].Cwd)
	}
	if panes[1].PID != 99 || panes[1].SessionName != "ops" || panes[1].WindowIndex != 1 || panes[1].PaneIndex != 2 {
		t.Errorf("pane[1] = %+v", panes[1])
	}
}

func TestParseTmuxOutput_BackwardCompatible(t *testing.T) {
	// Old 4-field format still works (no cwd).
	input := "42\tdev\t0\t0\n"
	panes := parseTmuxOutput([]byte(input))
	if len(panes) != 1 {
		t.Fatalf("expected 1 pane, got %d", len(panes))
	}
	if panes[0].Cwd != "" {
		t.Errorf("expected empty cwd for 4-field input, got %q", panes[0].Cwd)
	}
}

func TestParseTmuxOutput_MalformedLines(t *testing.T) {
	input := "bad line\n42\tdev\t0\t0\t/home\n\nnot\tenough\tfields\n"
	panes := parseTmuxOutput([]byte(input))
	if len(panes) != 1 {
		t.Fatalf("expected 1 pane (skipping malformed), got %d", len(panes))
	}
	if panes[0].PID != 42 {
		t.Errorf("expected PID 42, got %d", panes[0].PID)
	}
}

func TestParseTmuxOutput_NonNumericPID(t *testing.T) {
	input := "abc\tdev\t0\t0\n"
	panes := parseTmuxOutput([]byte(input))
	if len(panes) != 0 {
		t.Errorf("expected 0 panes for non-numeric PID, got %d", len(panes))
	}
}

func TestOSTmuxClient_NoTmux(t *testing.T) {
	runner := &mockRunner{output: nil, err: errors.New("exit 1")}
	client := &OSTmuxClient{Runner: runner}
	_, err := client.ListPanes(context.Background())
	if err == nil {
		t.Error("expected error when tmux is not running")
	}
}

func TestOSTmuxClient_ParsesOutput(t *testing.T) {
	runner := &mockRunner{output: []byte("100\twork\t0\t0\t/home/user\n200\thome\t1\t0\t/tmp\n")}
	client := &OSTmuxClient{Runner: runner}
	panes, err := client.ListPanes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(panes) != 2 {
		t.Fatalf("expected 2 panes, got %d", len(panes))
	}
}

// --- parseProcessList tests ---

func TestParseProcessList_WellFormed(t *testing.T) {
	input := "  PID  PPID COMMAND         COMMAND\n  100     1 zsh             /usr/bin/zsh\n  200   100 claude          /usr/bin/claude --flag\n"
	procs := parseProcessList([]byte(input))
	if len(procs) != 2 {
		t.Fatalf("expected 2 procs, got %d", len(procs))
	}
	if procs[1].PID != 200 || procs[1].PPID != 100 || procs[1].Comm != "claude" {
		t.Errorf("proc[1] = %+v", procs[1])
	}
	if !strings.Contains(procs[1].Args, "/usr/bin/claude") {
		t.Errorf("expected args to contain /usr/bin/claude, got %q", procs[1].Args)
	}
}

func TestParseProcessList_SkipsHeader(t *testing.T) {
	input := "  PID  PPID COMMAND         COMMAND\n"
	procs := parseProcessList([]byte(input))
	if len(procs) != 0 {
		t.Errorf("expected 0 procs (header only), got %d", len(procs))
	}
}

func TestParseProcessList_MalformedLines(t *testing.T) {
	input := "  PID  PPID COMMAND         COMMAND\n  bad\n  100     1 zsh             /usr/bin/zsh\n"
	procs := parseProcessList([]byte(input))
	if len(procs) != 1 {
		t.Fatalf("expected 1 proc, got %d", len(procs))
	}
}

// --- FindClaudePanes tests ---

func TestFindClaudePanes_DirectChild(t *testing.T) {
	client := &mockTmuxClient{
		panes: []TmuxPane{
			{PID: 100, SessionName: "dev", WindowIndex: 0, PaneIndex: 0},
		},
	}
	lister := &mockProcessLister{
		procs: []ProcessInfo{
			{PID: 100, PPID: 1, Comm: "zsh"},
			{PID: 200, PPID: 100, Comm: "claude"},
		},
	}

	panes, err := FindClaudePanes(context.Background(), client, lister, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(panes) != 1 {
		t.Fatalf("expected 1 pane, got %d", len(panes))
	}
	if panes[0].SessionName != "dev" {
		t.Errorf("session = %q, want dev", panes[0].SessionName)
	}
	if panes[0].ClaudePID != 200 {
		t.Errorf("ClaudePID = %d, want 200", panes[0].ClaudePID)
	}
}

func TestFindClaudePanes_DeepDescendant(t *testing.T) {
	client := &mockTmuxClient{
		panes: []TmuxPane{
			{PID: 100, SessionName: "deep", WindowIndex: 0, PaneIndex: 0},
		},
	}
	lister := &mockProcessLister{
		procs: []ProcessInfo{
			{PID: 100, PPID: 1, Comm: "zsh"},
			{PID: 200, PPID: 100, Comm: "zsh"},
			{PID: 300, PPID: 200, Comm: "bash"},
			{PID: 400, PPID: 300, Comm: "claude"},
		},
	}

	panes, err := FindClaudePanes(context.Background(), client, lister, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(panes) != 1 {
		t.Fatalf("expected 1 pane, got %d", len(panes))
	}
	if panes[0].SessionName != "deep" {
		t.Errorf("session = %q, want deep", panes[0].SessionName)
	}
}

func TestFindClaudePanes_DockerExecWithKnownContainer(t *testing.T) {
	// Pane runs devcontainer which spawns docker exec into a container running claude.
	client := &mockTmuxClient{
		panes: []TmuxPane{
			{PID: 100, SessionName: "dc", WindowIndex: 0, PaneIndex: 2},
		},
	}
	lister := &mockProcessLister{
		procs: []ProcessInfo{
			{PID: 100, PPID: 1, Comm: "zsh"},
			{PID: 200, PPID: 100, Comm: "node-MainThread", Args: "node /usr/bin/devcontainer exec --workspace-folder . bash"},
			{PID: 300, PPID: 200, Comm: "docker", Args: "docker exec -i -t abc123def456 bash"},
		},
	}

	panes, err := FindClaudePanes(context.Background(), client, lister, []string{"abc123def456789"})
	if err != nil {
		t.Fatal(err)
	}
	if len(panes) != 1 {
		t.Fatalf("expected 1 pane (docker exec matches container), got %d", len(panes))
	}
	if panes[0].SessionName != "dc" {
		t.Errorf("session = %q, want dc", panes[0].SessionName)
	}
	if !panes[0].Devcontainer {
		t.Error("expected Devcontainer=true for docker exec pane")
	}
}

func TestFindClaudePanes_DockerExecUnknownContainer(t *testing.T) {
	// docker exec into a container NOT running claude.
	client := &mockTmuxClient{
		panes: []TmuxPane{
			{PID: 100, SessionName: "dc", WindowIndex: 0, PaneIndex: 0},
		},
	}
	lister := &mockProcessLister{
		procs: []ProcessInfo{
			{PID: 100, PPID: 1, Comm: "zsh"},
			{PID: 200, PPID: 100, Comm: "docker", Args: "docker exec -i unknown123 bash"},
		},
	}

	panes, err := FindClaudePanes(context.Background(), client, lister, []string{"abc123def456"})
	if err != nil {
		t.Fatal(err)
	}
	if len(panes) != 0 {
		t.Errorf("expected 0 panes (unknown container), got %d", len(panes))
	}
}

func TestFindClaudePanes_NoMatch(t *testing.T) {
	client := &mockTmuxClient{
		panes: []TmuxPane{
			{PID: 100, SessionName: "dev", WindowIndex: 0, PaneIndex: 0},
		},
	}
	lister := &mockProcessLister{
		procs: []ProcessInfo{
			{PID: 100, PPID: 1, Comm: "zsh"},
			{PID: 200, PPID: 100, Comm: "vim"},
		},
	}

	panes, err := FindClaudePanes(context.Background(), client, lister, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(panes) != 0 {
		t.Errorf("expected 0 panes, got %d", len(panes))
	}
}

func TestFindClaudePanes_MultiplePanes(t *testing.T) {
	// 3 panes: pane 0 has claude, pane 1 has vim, pane 2 has docker exec to known container.
	client := &mockTmuxClient{
		panes: []TmuxPane{
			{PID: 100, SessionName: "s", WindowIndex: 0, PaneIndex: 0},
			{PID: 200, SessionName: "s", WindowIndex: 0, PaneIndex: 1},
			{PID: 300, SessionName: "s", WindowIndex: 0, PaneIndex: 2},
		},
	}
	lister := &mockProcessLister{
		procs: []ProcessInfo{
			{PID: 100, PPID: 1, Comm: "zsh"},
			{PID: 101, PPID: 100, Comm: "claude"},
			{PID: 200, PPID: 1, Comm: "zsh"},
			{PID: 201, PPID: 200, Comm: "vim"},
			{PID: 300, PPID: 1, Comm: "zsh"},
			{PID: 301, PPID: 300, Comm: "docker", Args: "docker exec -i abc123full bash"},
		},
	}

	panes, err := FindClaudePanes(context.Background(), client, lister, []string{"abc123full"})
	if err != nil {
		t.Fatal(err)
	}
	if len(panes) != 2 {
		t.Fatalf("expected 2 panes, got %d", len(panes))
	}
	if panes[0].Devcontainer {
		t.Error("pane 0 should not be devcontainer")
	}
	if !panes[1].Devcontainer {
		t.Error("pane 2 should be devcontainer")
	}
}

func TestFindClaudePanes_NoClaude(t *testing.T) {
	client := &mockTmuxClient{
		panes: []TmuxPane{
			{PID: 100, SessionName: "dev", WindowIndex: 0, PaneIndex: 0},
		},
	}
	lister := &mockProcessLister{
		procs: []ProcessInfo{
			{PID: 100, PPID: 1, Comm: "zsh"},
		},
	}

	panes, err := FindClaudePanes(context.Background(), client, lister, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(panes) != 0 {
		t.Errorf("expected 0 panes, got %d", len(panes))
	}
}

func TestFindClaudePanes_NoTmux(t *testing.T) {
	client := &mockTmuxClient{err: errors.New("tmux not found")}
	lister := &mockProcessLister{}

	panes, err := FindClaudePanes(context.Background(), client, lister, nil)
	if err == nil {
		t.Error("expected error when tmux is not available")
	}
	if len(panes) != 0 {
		t.Errorf("expected 0 panes, got %d", len(panes))
	}
}

func TestFindClaudePanes_NoPanes(t *testing.T) {
	client := &mockTmuxClient{panes: nil}
	lister := &mockProcessLister{}

	panes, err := FindClaudePanes(context.Background(), client, lister, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(panes) != 0 {
		t.Errorf("expected 0 panes, got %d", len(panes))
	}
}

func TestFindClaudePanes_ProcessListError(t *testing.T) {
	client := &mockTmuxClient{
		panes: []TmuxPane{
			{PID: 100, SessionName: "dev", WindowIndex: 0, PaneIndex: 0},
		},
	}
	lister := &mockProcessLister{err: errors.New("ps failed")}

	panes, err := FindClaudePanes(context.Background(), client, lister, nil)
	if err != nil {
		t.Fatal("should not return error on ps failure (best-effort)")
	}
	if len(panes) != 0 {
		t.Errorf("expected 0 panes on ps failure, got %d", len(panes))
	}
}

// --- findClaude tests ---

func TestFindClaude_DirectClaude(t *testing.T) {
	children := map[int][]int{100: {200}}
	info := map[int]ProcessInfo{200: {PID: 200, Comm: "claude"}}
	found, dc, cid, claudePID := findClaude(100, children, info, nil)
	if !found {
		t.Error("expected found for direct claude child")
	}
	if dc {
		t.Error("expected devcontainer=false for direct claude")
	}
	if cid != "" {
		t.Errorf("expected empty containerID for direct claude, got %q", cid)
	}
	if claudePID != 200 {
		t.Errorf("expected claudePID=200, got %d", claudePID)
	}
}

func TestFindClaude_DeepClaude(t *testing.T) {
	children := map[int][]int{100: {200}, 200: {300}}
	info := map[int]ProcessInfo{
		200: {PID: 200, Comm: "bash"},
		300: {PID: 300, Comm: "claude"},
	}
	found, dc, _, claudePID := findClaude(100, children, info, nil)
	if !found {
		t.Error("expected found for deep claude descendant")
	}
	if dc {
		t.Error("expected devcontainer=false for deep claude")
	}
	if claudePID != 300 {
		t.Errorf("expected claudePID=300, got %d", claudePID)
	}
}

func TestFindClaude_DockerExecMatch(t *testing.T) {
	children := map[int][]int{100: {200}}
	info := map[int]ProcessInfo{
		200: {PID: 200, Comm: "docker", Args: "docker exec -i abc123full bash"},
	}
	ids := map[string]bool{"abc123full": true}
	found, dc, cid, claudePID := findClaude(100, children, info, ids)
	if !found {
		t.Error("expected found for docker exec into known container")
	}
	if !dc {
		t.Error("expected devcontainer=true for docker exec match")
	}
	if cid != "abc123full" {
		t.Errorf("expected containerID abc123full, got %q", cid)
	}
	if claudePID != 0 {
		t.Errorf("expected claudePID=0 for docker exec, got %d", claudePID)
	}
}

func TestFindClaude_None(t *testing.T) {
	children := map[int][]int{100: {200}}
	info := map[int]ProcessInfo{200: {PID: 200, Comm: "vim"}}
	found, _, _, _ := findClaude(100, children, info, nil)
	if found {
		t.Error("expected not found when no claude descendant")
	}
}

func TestFindClaude_NoChildren(t *testing.T) {
	children := map[int][]int{}
	info := map[int]ProcessInfo{}
	found, _, _, _ := findClaude(100, children, info, nil)
	if found {
		t.Error("expected not found for PID with no children")
	}
}

// --- matchesDockerExec tests ---

func TestMatchesDockerExec_FullID(t *testing.T) {
	ids := map[string]bool{"abc123def456789": true}
	if !matchesDockerExec("docker exec -i abc123def456789 bash", ids) {
		t.Error("expected match for full container ID")
	}
}

func TestMatchesDockerExec_ShortID(t *testing.T) {
	// ps shows full ID, known set has full ID, but arg is a prefix.
	ids := map[string]bool{"abc123def456789abcdef": true}
	if !matchesDockerExec("docker exec -i abc123def456789 bash", ids) {
		t.Error("expected match for short container ID prefix")
	}
}

func TestMatchesDockerExec_NoExec(t *testing.T) {
	ids := map[string]bool{"abc123": true}
	if matchesDockerExec("docker ps abc123", ids) {
		t.Error("expected no match without exec subcommand")
	}
}

func TestMatchesDockerExec_NoMatch(t *testing.T) {
	ids := map[string]bool{"abc123": true}
	if matchesDockerExec("docker exec -i xyz789 bash", ids) {
		t.Error("expected no match for unknown container")
	}
}

func TestMatchesDockerExec_EmptyIDs(t *testing.T) {
	if matchesDockerExec("docker exec -i abc123 bash", nil) {
		t.Error("expected no match with empty container ID set")
	}
}

// --- FormatTmuxPanes tests ---

func TestFormatTmuxPanes(t *testing.T) {
	panes := []TmuxPane{
		{SessionName: "dev", WindowIndex: 0, PaneIndex: 0, State: StateReady},
		{SessionName: "ops", WindowIndex: 1, PaneIndex: 2, Devcontainer: true, State: StateBusy},
	}
	var buf bytes.Buffer
	err := FormatTmuxPanes(&buf, panes)
	if err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "Tmux panes running claude:") {
		t.Errorf("output should contain Tmux header, got: %s", got)
	}
	if !strings.Contains(got, "🟢") {
		t.Errorf("output should contain ready emoji for dev pane, got: %s", got)
	}
	if !strings.Contains(got, `"dev" (0:0)`) {
		t.Errorf("output should contain dev pane, got: %s", got)
	}
	if !strings.Contains(got, "🔴") {
		t.Errorf("output should contain busy emoji for ops pane, got: %s", got)
	}
	if !strings.Contains(got, `"ops" (1:2) (devcontainer)`) {
		t.Errorf("output should contain ops pane with devcontainer suffix, got: %s", got)
	}
}

func TestFormatTmuxPanes_WithPID(t *testing.T) {
	panes := []TmuxPane{
		{SessionName: "dev", WindowIndex: 0, PaneIndex: 1, ClaudePID: 12345, State: StateBusy},
	}
	var buf bytes.Buffer
	err := FormatTmuxPanes(&buf, panes)
	if err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "PID 12345") {
		t.Errorf("output should contain PID, got: %s", got)
	}
	if !strings.Contains(got, `"dev" (0:1) PID 12345`) {
		t.Errorf("expected pane with PID, got: %s", got)
	}
}

func TestFormatTmuxPanes_Empty(t *testing.T) {
	var buf bytes.Buffer
	err := FormatTmuxPanes(&buf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output for empty panes, got: %s", buf.String())
	}
}

// --- Ensure interfaces are satisfied ---

var _ TmuxClient = &OSTmuxClient{}
var _ TmuxClient = &mockTmuxClient{}
var _ ProcessLister = &OSProcessLister{}
var _ ProcessLister = &mockProcessLister{}
