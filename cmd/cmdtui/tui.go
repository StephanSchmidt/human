package cmdtui

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/cmd/cmddaemon"
	"github.com/StephanSchmidt/human/internal/claude"
	"github.com/StephanSchmidt/human/internal/claude/logparser"
	"github.com/StephanSchmidt/human/internal/claude/monitor"
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
	mon := monitor.New(finder, dc)
	m := newModel(mon)
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

type model struct {
	mon      *monitor.Monitor
	snap     *monitor.Snapshot
	width    int
	height   int
	quitting bool
}

func newModel(mon *monitor.Monitor) model {
	return model{mon: mon}
}

// --- messages ---

type fastTickMsg time.Time // 500ms — cheap socket/daemon/pane checks
type fullTickMsg time.Time // 2s   — full fetch including JSONL parsing

type snapshotMsg struct {
	snap *monitor.Snapshot
}

// --- tea.Model implementation (v1 API) ---

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchFull(m.mon), fastTickCmd(), fullTickCmd())
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
		return m, tea.Batch(fetchQuick(m.mon, m.snap), fastTickCmd())
	case fullTickMsg:
		return m, tea.Batch(fetchFull(m.mon), fullTickCmd())
	case snapshotMsg:
		m.snap = msg.snap
	}
	return m, nil
}

func (m model) View() string {
	if m.quitting {
		return ""
	}
	var b strings.Builder
	_, _ = fmt.Fprintln(&b, renderHeader())
	if m.snap == nil {
		_, _ = fmt.Fprintln(&b, "  Loading...")
		return b.String()
	}
	_, _ = fmt.Fprintln(&b, renderDaemonStatus(m.snap.Daemon.PID, m.snap.Daemon.Alive))
	_, _ = fmt.Fprintln(&b)
	if m.snap.Err != nil {
		_, _ = fmt.Fprintln(&b, renderError(m.snap.Err))
		return b.String()
	}
	renderUsage(&b, m.snap)
	_, _ = fmt.Fprintln(&b)
	_, _ = fmt.Fprintln(&b, renderFooter(m.snap.FetchedAt, m.snap.Telegram))
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

func fetchFull(mon *monitor.Monitor) tea.Cmd {
	return func() tea.Msg {
		return snapshotMsg{snap: mon.FetchFull(context.Background())}
	}
}

func fetchQuick(mon *monitor.Monitor, prev *monitor.Snapshot) tea.Cmd {
	return func() tea.Msg {
		snap := mon.FetchQuick(context.Background(), prev)
		if snap == nil {
			return nil
		}
		return snapshotMsg{snap: snap}
	}
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
		return fmt.Sprintf("Daemon: %s (PID %d)", greenStyle.Render("running"), pid)
	}
	return fmt.Sprintf("Daemon: %s", redStyle.Render("stopped"))
}

func renderError(err error) string {
	return errorStyle.Render("  Error: " + err.Error())
}

func renderUsage(b *strings.Builder, snap *monitor.Snapshot) {
	now := snap.FetchedAt
	ws := claude.WindowStart(now)
	we := claude.WindowEnd(ws)

	_, _ = fmt.Fprintf(b, "Claude usage [%02d:00 – %02d:00 UTC]\n", ws.Hour(), we.Hour())

	if len(snap.Instances) == 0 {
		_, _ = fmt.Fprintln(b, "  (no instances)")
	} else {
		renderInstances(b, snap)
	}

	// Tmux panes.
	if len(snap.Panes) > 0 {
		var panesBuf strings.Builder
		_ = claude.FormatTmuxPanes(&panesBuf, snap.Panes)
		_, _ = fmt.Fprint(b, panesBuf.String())
	}
}

func renderInstances(b *strings.Builder, snap *monitor.Snapshot) {
	for _, iv := range snap.Instances {
		renderInstance(b, iv)
	}
	renderTotal(b, snap.TotalUsage)
}

func renderInstance(b *strings.Builder, iv monitor.InstanceView) {
	icon := sessionIcon(iv.Session)
	header := fmt.Sprintf("\n%s %s", icon, iv.Usage.Instance.Label)
	if mem := claude.FormatMemory(iv.Usage.Instance.Memory); mem != "" {
		header += "  " + mem
	}
	if iv.Session != nil && !iv.Session.StartedAt.IsZero() {
		header += fmt.Sprintf("  [%s]", formatElapsed(time.Since(iv.Session.StartedAt)))
	}
	if iv.Session != nil && iv.Session.Slug != "" {
		header += fmt.Sprintf("  %s", sessionStyle.Render(iv.Session.Slug))
	}
	_, _ = fmt.Fprintln(b, header)

	var instanceTotal int
	for _, mu := range iv.Usage.Summary.Models {
		if mu != nil {
			instanceTotal += mu.Total()
		}
	}
	_ = claude.FormatModelRows(b, iv.Usage.Summary, instanceTotal)

	if iv.Session != nil {
		renderSubagents(b, iv.Session.Subagents, iv.Session.IsWorking, iv.Session.LastActivity)
		renderTaskSummary(b, iv.Session.Tasks)
	}
}

func sessionIcon(sess *logparser.SessionState) string {
	if sess == nil {
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
			grandTotal += mu.Total()
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
