package cmdtui

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/cmd/cmddaemon"
	"github.com/StephanSchmidt/human/internal/claude"
	"github.com/StephanSchmidt/human/internal/claude/hookevents"
	"github.com/StephanSchmidt/human/internal/claude/logparser"
	"github.com/StephanSchmidt/human/internal/telegram"
)

// BuildTuiCmd creates the "tui" command.
func BuildTuiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Interactive dashboard for Claude Code usage",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runTUI()
		},
	}
}

func runTUI() error {
	ensureDaemon()
	finder, dc := buildFinder()
	m := newModel(finder, dc)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// ensureDaemon starts the daemon if it is not already running.
// Best-effort: if it fails, the TUI still works.
func ensureDaemon() {
	if _, alive := cmddaemon.ReadAlivePid(); alive {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	child := exec.Command(exe, "daemon", "start") // #nosec G204 -- re-exec of own binary via os.Executable()
	child.Stdout = nil
	child.Stderr = nil
	_ = child.Start()
	if child.Process != nil {
		_ = child.Process.Release()
	}
	// Poll for readiness (up to 3s).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := net.DialTimeout("tcp", "localhost:19285", 200*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func buildFinder() (claude.InstanceFinder, claude.DockerClient) {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Debug().Err(err).Msg("cannot resolve home dir for host finder")
		home = ""
	}
	finders := []claude.InstanceFinder{
		&claude.HostFinder{Runner: claude.OSCommandRunner{}, HomeDir: home},
	}
	var dc claude.DockerClient
	if client, dcErr := claude.NewEngineDockerClient(); dcErr == nil {
		dc = client
		finders = append(finders, &claude.DockerFinder{Client: dc})
	}
	return &claude.CombinedFinder{Finders: finders}, dc
}

// --- bubbletea model ---

type usageData struct {
	Instances      []claude.InstanceUsage
	Panes          []claude.TmuxPane
	DaemonPid      int
	DaemonUp       bool
	TelegramStatus string
	FetchedAt      time.Time
	Err            error
	Sessions       []logparser.SessionState
	SessionByPath  map[string]logparser.SessionState    // filePath → session for PID matching
	HookSnapshots  map[string]hookevents.SessionSnapshot // sessionID → hook state
	hookReaders    []hookevents.EventReader              // carried forward for fetchQuick
}

type model struct {
	finder       claude.InstanceFinder
	dockerClient claude.DockerClient // may be nil when Docker is unavailable
	data         *usageData
	parsers      map[string]*logparser.FileParser
	width        int
	height       int
	quitting     bool
}

func newModel(finder claude.InstanceFinder, dc claude.DockerClient) model {
	return model{
		finder:       finder,
		dockerClient: dc,
		parsers:      make(map[string]*logparser.FileParser),
	}
}

// --- messages ---

type fastTickMsg time.Time // 500ms — cheap socket/daemon/pane checks
type fullTickMsg time.Time // 2s   — full fetch including JSONL parsing

type usageMsg struct {
	data *usageData
}

// --- tea.Model implementation (v1 API) ---

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchFull(m.finder, m.parsers, m.dockerClient), fastTickCmd(), fullTickCmd())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case fastTickMsg:
		return m, tea.Batch(fetchQuick(m.finder, m.data), fastTickCmd())
	case fullTickMsg:
		return m, tea.Batch(fetchFull(m.finder, m.parsers, m.dockerClient), fullTickCmd())
	case usageMsg:
		m.data = msg.data
	}
	return m, nil
}

func (m model) View() string {
	if m.quitting {
		return ""
	}
	var b strings.Builder
	_, _ = fmt.Fprintln(&b, renderHeader())
	if m.data == nil {
		_, _ = fmt.Fprintln(&b, "  Loading...")
		return b.String()
	}
	_, _ = fmt.Fprintln(&b, renderDaemonStatus(m.data.DaemonPid, m.data.DaemonUp))
	_, _ = fmt.Fprintln(&b)
	if m.data.Err != nil {
		_, _ = fmt.Fprintln(&b, renderError(m.data.Err))
		return b.String()
	}
	renderUsage(&b, m.data)
	_, _ = fmt.Fprintln(&b)
	_, _ = fmt.Fprintln(&b, renderFooter(m.data.FetchedAt, m.data.TelegramStatus))
	return b.String()
}

// --- commands ---

func fastTickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return fastTickMsg(t)
	})
}

func fullTickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return fullTickMsg(t)
	})
}

// fetchQuick updates daemon status, pane states, and hook-based working/idle
// on the existing data. Everything else (instances, sessions, agents, tasks)
// is kept from the last full fetch to avoid flicker.
func fetchQuick(finder claude.InstanceFinder, prev *usageData) tea.Cmd {
	return func() tea.Msg {
		if prev == nil {
			return nil // nothing to update yet; wait for first full fetch
		}

		// Shallow copy so we don't mutate the previous pointer in-place.
		data := *prev
		data.FetchedAt = time.Now()

		// Cheap: daemon liveness check.
		pid, alive := cmddaemon.ReadAlivePid()
		data.DaemonPid = pid
		data.DaemonUp = alive

		// Cheap: re-read hook events for sub-second state transitions.
		ctx := context.Background()
		if len(data.hookReaders) > 0 {
			quickCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
			data.HookSnapshots = readHookSnapshots(quickCtx, data.hookReaders)
			cancel()
			overlayHookState(&data)
		}

		// Cheap: update pane states from carried-forward sessions.
		instances, err := finder.FindInstances(ctx)
		if err == nil {
			containerIDs := collectContainerIDs(instances)
			runner := claude.OSCommandRunner{}
			tmuxClient := &claude.OSTmuxClient{Runner: runner}
			procLister := &claude.OSProcessLister{Runner: runner}
			panes, _ := claude.FindClaudePanes(ctx, tmuxClient, procLister, containerIDs)

			sessionByPID := buildSessionPIDMap(data.SessionByPath, instances)
			matchPaneStates(panes, sessionByPID, data.Sessions)
			data.Panes = panes
		}

		return usageMsg{data: &data}
	}
}

// fetchFull performs the complete fetch including JSONL log parsing and hook events.
func fetchFull(finder claude.InstanceFinder, parsers map[string]*logparser.FileParser, dc claude.DockerClient) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		now := time.Now()
		data := &usageData{FetchedAt: now}

		pid, alive := cmddaemon.ReadAlivePid()
		data.DaemonPid = pid
		data.DaemonUp = alive
		data.TelegramStatus = telegramStatus()

		instances, err := finder.FindInstances(ctx)
		if err != nil {
			data.Err = err
			return usageMsg{data: data}
		}

		data.Instances = claude.CollectInstanceUsage(instances, now)

		// Tmux panes (best-effort).
		containerIDs := collectContainerIDs(instances)
		runner := claude.OSCommandRunner{}
		tmuxClient := &claude.OSTmuxClient{Runner: runner}
		procLister := &claude.OSProcessLister{Runner: runner}
		panes, _ := claude.FindClaudePanes(ctx, tmuxClient, procLister, containerIDs)

		// Session monitoring via JSONL log parsing.
		data.SessionByPath = make(map[string]logparser.SessionState)
		reader := logparser.OSFileReader{}
		for _, inst := range instances {
			if inst.FilePath == "" {
				continue
			}
			parser, ok := parsers[inst.FilePath]
			if !ok {
				parser = logparser.NewFileParser()
				parsers[inst.FilePath] = parser
			}
			state, parseErr := parser.Update(reader, inst.FilePath)
			if parseErr != nil {
				log.Debug().Err(parseErr).Str("path", inst.FilePath).Msg("session parse failed")
				continue
			}
			if state.SessionID != "" {
				data.Sessions = append(data.Sessions, state)
				data.SessionByPath[inst.FilePath] = state
			}
		}

		// Hook events: build readers and read current state.
		data.hookReaders = buildHookReaders(instances, dc)
		data.HookSnapshots = readHookSnapshots(ctx, data.hookReaders)
		overlayHookState(data)

		sessionByPID := buildSessionPIDMap(data.SessionByPath, instances)
		matchPaneStates(panes, sessionByPID, data.Sessions)
		data.Panes = panes

		return usageMsg{data: data}
	}
}

func collectContainerIDs(instances []claude.Instance) []string {
	var ids []string
	for _, inst := range instances {
		if inst.Source == "container" && inst.ContainerID != "" {
			ids = append(ids, inst.ContainerID)
		}
	}
	return ids
}

// buildHookReaders creates EventReaders for host and container event files.
func buildHookReaders(instances []claude.Instance, dc claude.DockerClient) []hookevents.EventReader {
	var readers []hookevents.EventReader

	// Host reader (always present).
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		readers = append(readers, &hookevents.FileEventReader{
			Path: filepath.Join(home, ".claude", "human-events", "events.jsonl"),
		})
	}

	// Container readers.
	if dc != nil {
		seen := make(map[string]bool)
		for _, inst := range instances {
			if inst.Source == "container" && inst.ContainerID != "" && !seen[inst.ContainerID] {
				seen[inst.ContainerID] = true
				readers = append(readers, &hookevents.DockerEventReader{
					Client:      dc,
					ContainerID: inst.ContainerID,
				})
			}
		}
	}

	return readers
}

// readHookSnapshots reads and parses hook events from all readers.
func readHookSnapshots(ctx context.Context, readers []hookevents.EventReader) map[string]hookevents.SessionSnapshot {
	all := make(map[string]hookevents.SessionSnapshot)
	for _, r := range readers {
		evtData, err := r.ReadEvents(ctx)
		if err != nil || len(evtData) == 0 {
			continue
		}
		for sid, snap := range hookevents.Parse(evtData) {
			all[sid] = snap
		}
	}
	return all
}

// overlayHookState updates Sessions' IsWorking from hook snapshots when the
// hook event is more recent than the JSONL-parsed LastActivity.
func overlayHookState(data *usageData) {
	if len(data.HookSnapshots) == 0 {
		return
	}

	updated := make([]logparser.SessionState, len(data.Sessions))
	copy(updated, data.Sessions)
	for i := range updated {
		snap, ok := data.HookSnapshots[updated[i].SessionID]
		if !ok {
			continue
		}
		if snap.LastEventAt.After(updated[i].LastActivity) {
			updated[i].IsWorking = snap.IsWorking
			updated[i].LastActivity = snap.LastEventAt
		}
	}
	data.Sessions = updated

	updatedByPath := make(map[string]logparser.SessionState, len(data.SessionByPath))
	for path, sess := range data.SessionByPath {
		if snap, ok := data.HookSnapshots[sess.SessionID]; ok {
			if snap.LastEventAt.After(sess.LastActivity) {
				sess.IsWorking = snap.IsWorking
				sess.LastActivity = snap.LastEventAt
			}
		}
		updatedByPath[path] = sess
	}
	data.SessionByPath = updatedByPath
}

func buildSessionPIDMap(sessionByPath map[string]logparser.SessionState, instances []claude.Instance) map[int]logparser.SessionState {
	byPID := make(map[int]logparser.SessionState)
	for _, inst := range instances {
		if inst.PID > 0 && inst.FilePath != "" {
			if s, ok := sessionByPath[inst.FilePath]; ok {
				byPID[inst.PID] = s
			}
		}
	}
	return byPID
}

// matchPaneStates sets each pane's State by matching it to a parsed session.
// Primary match: ClaudePID → sessionByPID. Fallback: Cwd matching.
func matchPaneStates(panes []claude.TmuxPane, sessionByPID map[int]logparser.SessionState, sessions []logparser.SessionState) {
	for i := range panes {
		if panes[i].ClaudePID > 0 {
			if s, ok := sessionByPID[panes[i].ClaudePID]; ok {
				panes[i].State = sessionToState(s)
				continue
			}
		}
		// Fallback: Cwd matching for devcontainers or unknown PIDs.
		for _, s := range sessions {
			if s.Cwd != "" && panes[i].Cwd == s.Cwd {
				panes[i].State = sessionToState(s)
				break
			}
		}
	}
}

func sessionToState(s logparser.SessionState) claude.InstanceState {
	if s.IsWorking {
		return claude.StateBusy
	}
	return claude.StateReady
}

// --- render helpers ---

var (
	headerStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	footerStyle  = lipgloss.NewStyle().Faint(true)
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	greenStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	redStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	sessionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("33"))
)

func renderHeader() string {
	title := "human tui — Claude Code Dashboard"
	if host, err := os.Hostname(); err == nil && host != "" {
		title = fmt.Sprintf("human tui — Claude Code Dashboard (%s)", host)
	}
	return headerStyle.Render(title)
}

func renderDaemonStatus(pid int, alive bool) string {
	if alive {
		return fmt.Sprintf("  Daemon: %s (PID %d)", greenStyle.Render("running"), pid)
	}
	return fmt.Sprintf("  Daemon: %s", redStyle.Render("stopped"))
}

func renderError(err error) string {
	return errorStyle.Render("  Error: " + err.Error())
}

func renderUsage(b *strings.Builder, data *usageData) {
	now := data.FetchedAt
	ws := claude.WindowStart(now)
	we := claude.WindowEnd(ws)

	_, _ = fmt.Fprintf(b, "Claude usage [%02d:00 – %02d:00 UTC]\n", ws.Hour(), we.Hour())

	if len(data.Instances) == 0 {
		_, _ = fmt.Fprintln(b, "  (no instances)")
	} else {
		renderInstances(b, data)
	}

	// Tmux panes.
	if len(data.Panes) > 0 {
		var panesBuf strings.Builder
		_ = claude.FormatTmuxPanes(&panesBuf, data.Panes)
		_, _ = fmt.Fprint(b, panesBuf.String())
	}
}

func renderInstances(b *strings.Builder, data *usageData) {
	total := &claude.UsageSummary{Models: make(map[string]*claude.ModelUsage)}
	for _, iu := range data.Instances {
		claude.MergeUsage(total, iu.Summary)
		sess, hasSess := data.SessionByPath[iu.Instance.FilePath]
		renderInstance(b, iu, sess, hasSess)
	}
	renderTotal(b, total)
}

func renderInstance(b *strings.Builder, iu claude.InstanceUsage, sess logparser.SessionState, hasSess bool) {
	icon := sessionIcon(sess, hasSess)
	header := fmt.Sprintf("\n%s %s", icon, iu.Instance.Label)
	if mem := claude.FormatMemory(iu.Instance.Memory); mem != "" {
		header += "  " + mem
	}
	if hasSess && !sess.StartedAt.IsZero() {
		header += fmt.Sprintf("  [%s]", formatElapsed(time.Since(sess.StartedAt)))
	}
	if hasSess && sess.Slug != "" {
		header += fmt.Sprintf("  %s", sessionStyle.Render(sess.Slug))
	}
	_, _ = fmt.Fprintln(b, header)

	var instanceTotal int
	for _, mu := range iu.Summary.Models {
		if mu != nil {
			instanceTotal += claude.TotalTokens(mu)
		}
	}
	_ = claude.FormatModelRows(b, iu.Summary, instanceTotal)

	if hasSess {
		renderSubagents(b, sess.Subagents, sess.IsWorking, sess.LastActivity)
		renderTaskSummary(b, sess.Tasks)
	}
}

func sessionIcon(sess logparser.SessionState, hasSess bool) string {
	if !hasSess {
		return "⚪"
	}
	if sess.IsWorking {
		return "🔴"
	}
	if !sess.LastActivity.IsZero() {
		return "🟢"
	}
	return "⚪"
}

func renderTotal(b *strings.Builder, total *claude.UsageSummary) {
	var grandTotal int
	for _, mu := range total.Models {
		if mu != nil {
			grandTotal += claude.TotalTokens(mu)
		}
	}
	_, _ = fmt.Fprintf(b, "\nTotal:\n")
	_ = claude.FormatModelRows(b, total, grandTotal)
}

func renderSubagents(b *strings.Builder, subagents []logparser.Subagent, isWorking bool, lastActivity time.Time) {
	if len(subagents) == 0 {
		return
	}

	// Hide completed agents once the session has been idle for 5s.
	if !isWorking && time.Since(lastActivity) > 5*time.Second {
		hasRunning := false
		for _, sa := range subagents {
			if sa.CompletedAt == nil {
				hasRunning = true
				break
			}
		}
		if !hasRunning {
			return
		}
	}

	// Show only recent/active subagents (last 5).
	start := 0
	if len(subagents) > 5 {
		start = len(subagents) - 5
	}
	for i := start; i < len(subagents); i++ {
		sa := subagents[i]
		status := greenStyle.Render("running")
		elapsed := formatElapsed(time.Since(sa.StartedAt))
		if sa.CompletedAt != nil {
			status = footerStyle.Render("done")
			if sa.DurationMs > 0 {
				elapsed = formatElapsed(time.Duration(sa.DurationMs) * time.Millisecond)
			} else {
				elapsed = formatElapsed(sa.CompletedAt.Sub(sa.StartedAt))
			}
		}

		desc := truncate(sa.Description, 45)
		agentType := sa.SubagentType
		if agentType == "" {
			agentType = "agent"
		}

		_, _ = fmt.Fprintf(b, "    Agent: %s (%s, %s, %s)\n", desc, agentType, status, elapsed)
	}
}

func renderTaskSummary(b *strings.Builder, tasks []logparser.Task) {
	if len(tasks) == 0 {
		return
	}

	var pending, inProgress, completed int
	for _, t := range tasks {
		switch t.Status {
		case "completed":
			completed++
		case "in_progress":
			inProgress++
		default:
			pending++
		}
	}

	parts := []string{}
	if pending > 0 {
		parts = append(parts, fmt.Sprintf("%d pending", pending))
	}
	if inProgress > 0 {
		parts = append(parts, fmt.Sprintf("%d in progress", inProgress))
	}
	if completed > 0 {
		parts = append(parts, fmt.Sprintf("%d completed", completed))
	}

	_, _ = fmt.Fprintf(b, "    Tasks: %s\n", strings.Join(parts, ", "))
}

func formatElapsed(d time.Duration) string {
	d = d.Truncate(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func renderFooter(fetchedAt time.Time, tgStatus string) string {
	return footerStyle.Render(fmt.Sprintf(
		"  Last updated: %s  |  %s  |  Refreshes every 500ms  |  Press q to quit",
		fetchedAt.Format("15:04:05"), tgStatus))
}

// telegramStatus returns a human-readable Telegram dispatch status.
func telegramStatus() string {
	configs, err := telegram.LoadConfigs(".")
	if err != nil {
		return "Telegram: config error"
	}
	if len(configs) == 0 {
		return "Telegram: not configured"
	}
	instances, err := telegram.LoadInstances(".")
	if err != nil {
		return "Telegram: config error"
	}
	if len(instances) == 0 {
		return "Telegram: missing token"
	}
	return "Telegram dispatch"
}
