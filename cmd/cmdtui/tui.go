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
)

// BuildTuiCmd creates the "tui" command.
func BuildTuiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Interactive dashboard for Claude Code usage",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTUI(cmd.Context())
		},
	}
}

func runTUI(_ context.Context) error {
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
	Instances []claude.InstanceUsage
	Panes     []claude.TmuxPane
	DaemonPid int
	DaemonUp  bool
	FetchedAt time.Time
	Err       error
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
	_, _ = fmt.Fprintln(&b, renderFooter(m.data.FetchedAt))
	return b.String()
}

// --- commands ---

func tickCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
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

		// Build state maps from already-resolved instances so panes
		// show the same state as the instance list (no second disk read).
		containerState := make(map[string]claude.InstanceState)
		hostState := make(map[int]claude.InstanceState)
		for _, iu := range data.Instances {
			if iu.Instance.Source == "container" && iu.Instance.ContainerID != "" {
				containerState[iu.Instance.ContainerID] = iu.State
			}
			if iu.Instance.PID > 0 {
				hostState[iu.Instance.PID] = iu.State
			}
		}

		resolvePaneStates(panes, containerState, hostState)
		data.Panes = panes

		return usageMsg{data: data}
	}
}

// resolvePaneStates resolves busy/ready state for each tmux pane.
// It prefers states already resolved for discovered instances (hostState,
// containerState) so the pane list stays in sync with the instance list.
func resolvePaneStates(panes []claude.TmuxPane, containerState map[string]claude.InstanceState, hostState map[int]claude.InstanceState) {
	home, _ := os.UserHomeDir()
	sessResolver := claude.FileSessionResolver{HomeDir: home}
	for i := range panes {
		// Devcontainer panes: reuse container instance state.
		if panes[i].Devcontainer && panes[i].ContainerID != "" {
			if st, ok := containerState[panes[i].ContainerID]; ok {
				panes[i].State = st
				continue
			}
		}
		// Host panes: reuse host instance state when available.
		if panes[i].ClaudePID > 0 {
			if st, ok := hostState[panes[i].ClaudePID]; ok {
				panes[i].State = st
				continue
			}
		}
		// Fallback: read state from disk for panes not matched to an instance.
		if home == "" || panes[i].Cwd == "" {
			continue
		}
		projectDir := claude.CwdToProjectDir(panes[i].Cwd)
		root := filepath.Join(home, ".claude", "projects", projectDir)

		var stateReader claude.StateReader = claude.OSStateReader{}
		if panes[i].ClaudePID > 0 {
			if sessionID, sErr := sessResolver.ResolveSessionID(panes[i].ClaudePID); sErr == nil {
				sessionPath := filepath.Clean(filepath.Join(root, sessionID+".jsonl"))
				if _, fErr := os.Stat(sessionPath); fErr == nil {
					stateReader = claude.FileStateReader{Path: sessionPath}
				} else {
					stateReader = claude.ReadyStateReader{}
				}
			}
		}

		state, _ := stateReader.ReadState(root)
		panes[i].State = state
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

func renderFooter(fetchedAt time.Time) string {
	return footerStyle.Render(fmt.Sprintf(
		"  Last updated: %s  |  Refreshes every 5s  |  Press q to quit",
		fetchedAt.Format("15:04:05")))
}
