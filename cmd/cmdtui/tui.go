package cmdtui

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/cmd/cmddaemon"
	"github.com/StephanSchmidt/human/internal/claude"
	"github.com/StephanSchmidt/human/internal/claude/logparser"
	"github.com/StephanSchmidt/human/internal/claude/monitor"
)

const defaultWidth = 80

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
	// Suppress log output while the TUI owns the terminal.
	prev := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.Disabled)
	defer zerolog.SetGlobalLevel(prev)

	ensureDaemon()
	finder, dc := buildFinder()
	mon := monitor.New(finder, dc)
	m := newModel(mon)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// ensureDaemon starts the daemon if it is not already running.
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
	spinner  spinner.Model
	width    int
	height   int
	quitting bool
	fetchGen uint64 // monotonic counter; assigned when dispatching a fetch
	fetching bool   // true while a fetch command is in flight
}

func newModel(mon *monitor.Monitor) model {
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	sp.Style = lipgloss.NewStyle().Foreground(humanRed)
	return model{mon: mon, spinner: sp, width: defaultWidth, fetchGen: 1, fetching: true}
}

// --- messages ---

type fastTickMsg time.Time
type fullTickMsg time.Time

type snapshotMsg struct {
	snap *monitor.Snapshot
	gen  uint64
}

// --- tea.Model ---

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchFull(m.mon, 1), m.spinner.Tick, fastTickCmd(), fullTickCmd())
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
		if m.fetching {
			return m, fastTickCmd()
		}
		m.fetching = true
		m.fetchGen++
		return m, tea.Batch(fetchQuick(m.mon, m.snap, m.fetchGen), fastTickCmd())
	case fullTickMsg:
		if m.fetching {
			return m, fullTickCmd()
		}
		m.fetching = true
		m.fetchGen++
		return m, tea.Batch(fetchFull(m.mon, m.fetchGen), fullTickCmd())
	case snapshotMsg:
		if msg.gen != m.fetchGen {
			return m, nil // stale result, discard
		}
		m.snap = msg.snap
		m.fetching = false
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) View() string {
	if m.quitting {
		return ""
	}
	w := m.width
	if w < 40 {
		w = defaultWidth
	}

	var b strings.Builder

	// Header line: title left, usage window right.
	b.WriteString(m.renderHeader(w))
	b.WriteByte('\n')

	if m.snap == nil {
		b.WriteString("  " + m.spinner.View() + " Loading...\n")
		return b.String()
	}

	// Status line: daemon left, telegram right.
	b.WriteString(renderStatusLine(m.snap, w))
	b.WriteByte('\n')
	b.WriteByte('\n')

	if m.snap.Err != nil {
		b.WriteString(errorStyle.Render("  Error: " + m.snap.Err.Error()))
		b.WriteByte('\n')
		return b.String()
	}

	// Instances.
	if len(m.snap.Instances) == 0 {
		b.WriteString(subtleStyle.Render("  No active instances"))
		b.WriteByte('\n')
	} else {
		for _, iv := range m.snap.Instances {
			m.renderInstance(&b, iv, w)
		}
		renderTotalLine(&b, m.snap.TotalUsage, w)
	}

	// Tmux panes.
	if len(m.snap.Panes) > 0 {
		b.WriteByte('\n')
		b.WriteString(renderPanes(m.snap.Panes))
	}

	// Footer.
	b.WriteByte('\n')
	b.WriteString(renderFooter(w))
	b.WriteByte('\n')

	return b.String()
}

// --- commands ---

func fastTickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return fastTickMsg(t) })
}

func fullTickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return fullTickMsg(t) })
}

func fetchFull(mon *monitor.Monitor, gen uint64) tea.Cmd {
	return func() tea.Msg {
		return snapshotMsg{snap: mon.FetchFull(context.Background()), gen: gen}
	}
}

func fetchQuick(mon *monitor.Monitor, prev *monitor.Snapshot, gen uint64) tea.Cmd {
	return func() tea.Msg {
		snap := mon.FetchQuick(context.Background(), prev)
		if snap == nil {
			snap = prev // carry forward to avoid blank flash
		}
		return snapshotMsg{snap: snap, gen: gen}
	}
}

// --- render: header + status ---

func (m model) renderHeader(w int) string {
	title := titleStyle.Render("human tui")
	if host, err := os.Hostname(); err == nil && host != "" {
		title = titleStyle.Render("human tui") + subtleStyle.Render(" — "+host)
	}

	right := ""
	if m.snap != nil {
		ws := claude.WindowStart(m.snap.FetchedAt)
		we := claude.WindowEnd(ws)
		localStart := ws.Local()
		localEnd := we.Local()
		right = subtleStyle.Render(fmt.Sprintf("%02d:00 – %02d:00", localStart.Hour(), localEnd.Hour()))
	}

	gap := w - lipgloss.Width(title) - lipgloss.Width(right) - 4
	if gap < 1 {
		gap = 1
	}
	return "  " + title + strings.Repeat(" ", gap) + right
}

func renderStatusLine(snap *monitor.Snapshot, w int) string {
	var left string
	if snap.Daemon.Alive {
		left = "  " + specialStyle.Render("●") + " Daemon running"
		if snap.Daemon.ProxyAddr != "" {
			if snap.Daemon.ProxyActiveConns > 0 {
				left += "  " + specialStyle.Render(fmt.Sprintf("Proxy: %d active", snap.Daemon.ProxyActiveConns))
			} else {
				left += "  " + subtleStyle.Render("Proxy: idle")
			}
		}
	} else {
		left = "  " + accentStyle.Render("●") + " Daemon stopped"
	}

	var rightParts []string
	if snap.Telegram != "" {
		rightParts = append(rightParts, snap.Telegram)
	}
	if snap.Slack != "" {
		rightParts = append(rightParts, snap.Slack)
	}
	right := subtleStyle.Render(strings.Join(rightParts, "  "))
	gap := w - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// --- render: instances with progress bars ---

func (m model) renderInstance(b *strings.Builder, iv monitor.InstanceView, w int) {
	b.WriteByte('\n')

	// Instance header: icon + label + elapsed + slug
	icon := m.sessionIcon(iv.Session)
	labelStyle := idleInstanceStyle
	if iv.Session != nil && iv.Session.IsWorking {
		labelStyle = busyInstanceStyle
	}
	header := "  " + icon + " " + labelStyle.Render(iv.Usage.Instance.Label)
	if iv.Usage.Instance.DaemonConnected {
		if iv.Usage.Instance.ProxyConfigured {
			header += "  " + specialStyle.Render("daemon+proxy")
		} else {
			header += "  " + specialStyle.Render("daemon")
		}
	} else if iv.Usage.Instance.ProxyConfigured {
		header += "  " + specialStyle.Render("proxy")
	}
	if mem := claude.FormatMemory(iv.Usage.Instance.Memory); mem != "" {
		header += "  " + subtleStyle.Render(mem)
	}
	if iv.Session != nil && !iv.Session.StartedAt.IsZero() {
		header += "  " + subtleStyle.Render(formatElapsed(time.Since(iv.Session.StartedAt)))
	}
	if iv.Session != nil && iv.Session.Slug != "" {
		header += "  " + slugStyle.Render(iv.Session.Slug)
	}
	b.WriteString(header)
	b.WriteByte('\n')

	// Progress bars per model.
	renderModelBars(b, iv.Usage.Summary, w)

	// Subagents + tasks.
	if iv.Session != nil {
		m.renderSubagents(b, iv.Session.Subagents, iv.Session.IsWorking, iv.Session.LastActivity)
		renderTaskSummary(b, iv.Session.Tasks)
	}
}

func renderModelBars(b *strings.Builder, summary *claude.UsageSummary, w int) {
	if summary == nil {
		return
	}

	var grandTotal int
	for _, mu := range summary.Models {
		if mu != nil {
			grandTotal += mu.Total()
		}
	}
	if grandTotal == 0 {
		return
	}

	// Sort model names for stable output.
	models := make([]string, 0, len(summary.Models))
	for name, mu := range summary.Models {
		if mu != nil && mu.Total() > 0 {
			models = append(models, name)
		}
	}
	sort.Strings(models)

	// Bar width: total width - indent(4) - label(12) - stats(~30) - padding(4)
	barWidth := w - 50
	if barWidth < 10 {
		barWidth = 10
	}
	if barWidth > 50 {
		barWidth = 50
	}

	for _, name := range models {
		mu := summary.Models[name]
		if mu == nil {
			continue
		}
		pct := float64(mu.Total()) / float64(grandTotal)

		bar := progress.New(
			progress.WithSolidFill(modelColor(name)),
			progress.WithoutPercentage(),
		)
		bar.Width = barWidth

		stats := fmt.Sprintf("  %3.0f%%  %s in  %s out",
			pct*100, formatTokens(mu.InputTokens), formatTokens(mu.OutputTokens))

		_, _ = fmt.Fprintf(b, "    %-12s %s%s\n", name, bar.ViewAs(pct), subtleStyle.Render(stats))
	}
}

// --- render: subagents ---

func (m model) renderSubagents(b *strings.Builder, subagents []logparser.Subagent, isWorking bool, lastActivity time.Time) {
	if len(subagents) == 0 {
		return
	}

	// Hide completed agents once idle for 5s.
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

	start := 0
	if len(subagents) > 5 {
		start = len(subagents) - 5
	}
	for i := start; i < len(subagents); i++ {
		sa := subagents[i]
		agentType := sa.SubagentType
		if agentType == "" {
			agentType = "agent"
		}
		desc := truncate(sa.Description, 40)

		if sa.CompletedAt != nil {
			elapsed := formatAgentDuration(sa)
			_, _ = fmt.Fprintf(b, "      %s %s %s\n",
				subtleStyle.Render("✓"),
				subtleStyle.Render(desc),
				subtleStyle.Render(fmt.Sprintf("(%s, %s)", agentType, elapsed)))
		} else {
			elapsed := formatElapsed(time.Since(sa.StartedAt))
			_, _ = fmt.Fprintf(b, "      %s %s %s\n",
				m.spinner.View(),
				desc,
				subtleStyle.Render(fmt.Sprintf("(%s, %s)", agentType, elapsed)))
		}
	}
}

func formatAgentDuration(sa logparser.Subagent) string {
	if sa.DurationMs > 0 {
		return formatElapsed(time.Duration(sa.DurationMs) * time.Millisecond)
	}
	if sa.CompletedAt != nil {
		return formatElapsed(sa.CompletedAt.Sub(sa.StartedAt))
	}
	return "0s"
}

// --- render: tasks ---

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
		parts = append(parts, fmt.Sprintf("%d done", completed))
	}

	_, _ = fmt.Fprintf(b, "      Tasks: %s\n", subtleStyle.Render(strings.Join(parts, " · ")))
}

// --- render: totals ---

func renderTotalLine(b *strings.Builder, total *claude.UsageSummary, w int) {
	b.WriteByte('\n')
	rule := ruleStyle.Render(strings.Repeat("─", w-4))
	b.WriteString("  " + rule + "\n")

	var parts []string
	for name, mu := range total.Models {
		if mu != nil && mu.Total() > 0 {
			parts = append(parts, fmt.Sprintf("%s: %s in · %s out",
				name, formatTokens(mu.InputTokens), formatTokens(mu.OutputTokens)))
		}
	}
	sort.Strings(parts)

	if len(parts) > 0 {
		b.WriteString("  " + subtleStyle.Render("Total  "+strings.Join(parts, "  ")) + "\n")
	}
}

// --- render: tmux panes ---

func renderPanes(panes []claude.TmuxPane) string {
	var parts []string
	for _, p := range panes {
		var icon string
		switch p.State {
		case claude.StateBusy:
			icon = accentStyle.Render("●")
		case claude.StateReady:
			icon = specialStyle.Render("●")
		default:
			icon = "○"
		}
		label := fmt.Sprintf("%q (%d:%d)", p.SessionName, p.WindowIndex, p.PaneIndex)
		if p.Devcontainer {
			label += " (devcontainer)"
		}
		parts = append(parts, icon+" "+label)
	}
	return "  " + subtleStyle.Render("Tmux") + "  " + strings.Join(parts, "   ")
}

// --- render: footer ---

func renderFooter(w int) string {
	left := subtleStyle.Render("  ↻ live")
	right := subtleStyle.Render("q quit")
	gap := w - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// --- render: icon ---

func (m model) sessionIcon(sess *logparser.SessionState) string {
	if sess == nil {
		return subtleStyle.Render("○")
	}
	if sess.IsWorking {
		return m.spinner.View()
	}
	if !sess.LastActivity.IsZero() {
		return specialStyle.Render("●")
	}
	return subtleStyle.Render("○")
}

// --- utilities ---

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

func formatTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	default:
		return fmt.Sprintf("%d", n)
	}
}
