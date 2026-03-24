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
}

type model struct {
	finder   claude.InstanceFinder
	data     *usageData
	width    int
	height   int
	quitting bool
}

func newModel(finder claude.InstanceFinder) model {
	return model{finder: finder}
}

// --- messages ---

type tickMsg time.Time

type usageMsg struct {
	data *usageData
}

// --- tea.Model implementation (v1 API) ---

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchUsage(m.finder), tickCmd())
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
		return m, tea.Batch(fetchUsage(m.finder), tickCmd())
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

func fetchUsage(finder claude.InstanceFinder) tea.Cmd {
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
		data.Panes = panes

		return usageMsg{data: data}
	}
}

// --- render helpers ---

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	footerStyle = lipgloss.NewStyle().Faint(true)
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	greenStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	redStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	labelStyle  = lipgloss.NewStyle().Bold(true)
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
	_, _ = fmt.Fprintln(b, labelStyle.Render(
		fmt.Sprintf("  Usage window: %02d:00 – %02d:00 UTC", ws.Hour(), we.Hour())))
	_, _ = fmt.Fprintln(b)

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
