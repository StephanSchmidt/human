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
	finder := buildFinder()
	m := newModel(finder)
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

func buildFinder() claude.InstanceFinder {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Debug().Err(err).Msg("cannot resolve home dir for host finder")
		home = ""
	}
	finders := []claude.InstanceFinder{
		&claude.HostFinder{Runner: claude.OSCommandRunner{}, HomeDir: home},
	}
	if dc, dcErr := claude.NewEngineDockerClient(); dcErr == nil {
		finders = append(finders, &claude.DockerFinder{Client: dc})
	}
	return &claude.CombinedFinder{Finders: finders}
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
}

type model struct {
	finder   claude.InstanceFinder
	data     *usageData
	parsers  map[string]*logparser.FileParser
	width    int
	height   int
	quitting bool
}

func newModel(finder claude.InstanceFinder) model {
	return model{
		finder:  finder,
		parsers: make(map[string]*logparser.FileParser),
	}
}

// --- messages ---

type tickMsg time.Time

type usageMsg struct {
	data *usageData
}

// --- tea.Model implementation (v1 API) ---

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchUsage(m.finder, m.parsers), tickCmd())
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
	case tickMsg:
		return m, tea.Batch(fetchUsage(m.finder, m.parsers), tickCmd())
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

func tickCmd() tea.Cmd {
	// RC-1: Reduced from 5s to 2s as fallback for containers and pane discovery.
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func fetchUsage(finder claude.InstanceFinder, parsers map[string]*logparser.FileParser) tea.Cmd {
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
		var containerIDs []string
		for _, inst := range instances {
			if inst.Source == "container" && inst.ContainerID != "" {
				containerIDs = append(containerIDs, inst.ContainerID)
			}
		}
		runner := claude.OSCommandRunner{}
		tmuxClient := &claude.OSTmuxClient{Runner: runner}
		procLister := &claude.OSProcessLister{Runner: runner}
		panes, _ := claude.FindClaudePanes(ctx, tmuxClient, procLister, containerIDs)

		// Session monitoring via JSONL log parsing.
		// Build PID → SessionState map for tmux pane matching.
		sessionByPID := make(map[int]logparser.SessionState)
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
				if inst.PID > 0 {
					sessionByPID[inst.PID] = state
				}
			}
		}

		matchPaneStates(panes, sessionByPID, data.Sessions)
		data.Panes = panes

		return usageMsg{data: data}
	}
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
	labelStyle   = lipgloss.NewStyle().Bold(true)
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

	// Render usage via existing formatters into a string.
	var usageBuf strings.Builder
	switch {
	case len(data.Instances) == 0:
		_ = claude.FormatUsage(&usageBuf, &claude.UsageSummary{Models: map[string]*claude.ModelUsage{}}, now)
	default:
		_ = claude.FormatMultiUsage(&usageBuf, data.Instances, now)
	}
	_, _ = fmt.Fprint(b, usageBuf.String())

	// Tmux panes.
	if len(data.Panes) > 0 {
		var panesBuf strings.Builder
		_ = claude.FormatTmuxPanes(&panesBuf, data.Panes)
		_, _ = fmt.Fprint(b, panesBuf.String())
	}

	// Active sessions.
	renderSessions(b, data.Sessions)
}

func renderSessions(b *strings.Builder, sessions []logparser.SessionState) {
	if len(sessions) == 0 {
		return
	}

	_, _ = fmt.Fprintln(b)
	_, _ = fmt.Fprintln(b, labelStyle.Render("  Active Sessions:"))
	_, _ = fmt.Fprintln(b)

	for _, s := range sessions {
		icon := "⚪"
		if s.IsWorking {
			icon = "🔴"
		} else if !s.LastActivity.IsZero() {
			icon = "🟢"
		}

		elapsed := formatElapsed(time.Since(s.StartedAt))
		project := claude.ShortProjectName(s.Cwd)

		slugPart := ""
		if s.Slug != "" {
			slugPart = fmt.Sprintf("  %s", sessionStyle.Render(s.Slug))
		}

		_, _ = fmt.Fprintf(b, "  %s %s  [%s]%s\n", icon, project, elapsed, slugPart)

		// Subagents.
		renderSubagents(b, s.Subagents)

		// Tasks summary.
		renderTaskSummary(b, s.Tasks)
	}
}

func renderSubagents(b *strings.Builder, subagents []logparser.Subagent) {
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
		"  Last updated: %s  |  %s  |  Refreshes every 2s  |  Press q to quit",
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
