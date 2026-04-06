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
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/internal/browser"
	"github.com/StephanSchmidt/human/internal/claude"
	"github.com/StephanSchmidt/human/internal/claude/logparser"
	"github.com/StephanSchmidt/human/internal/claude/monitor"
	"github.com/StephanSchmidt/human/internal/daemon"
	"github.com/StephanSchmidt/human/internal/dispatch"
	"github.com/StephanSchmidt/human/internal/tracker"
)

const defaultWidth = 80

// trackerIssues groups issues from one tracker instance and project.
type trackerIssues struct {
	TrackerName string
	TrackerKind string
	TrackerRole string // "pm", "engineering", or empty
	Project     string
	Issues      []tracker.Issue
	Err         error
}

// flatIssue is a single issue with its tracker context, used for cursor indexing.
type flatIssue struct {
	TrackerKind string
	Project     string
	Issue       tracker.Issue
}

// flattenIssues flattens grouped tracker issues into a single slice for cursor navigation.
func flattenIssues(groups []trackerIssues) []flatIssue {
	var out []flatIssue
	for _, g := range groups {
		if g.Err != nil || len(g.Issues) == 0 {
			continue
		}
		for _, issue := range g.Issues {
			out = append(out, flatIssue{TrackerKind: g.TrackerKind, Project: g.Project, Issue: issue})
		}
	}
	return out
}

// BuildTuiCmd creates the "tui" command.
func BuildTuiCmd() *cobra.Command {
	var projectDirs []string
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Interactive dashboard for Claude Code usage",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runTUI(projectDirs)
		},
	}
	cmd.Flags().StringArrayVar(&projectDirs, "project", nil, "Project directory to register (repeatable; forwarded to daemon)")
	return cmd
}

func runTUI(projectDirs []string) error {
	// Suppress log output while the TUI owns the terminal.
	prev := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.Disabled)
	defer zerolog.SetGlobalLevel(prev)

	ensureDaemon(projectDirs)
	finder, dc := buildFinder()
	mon := monitor.New(finder, dc)
	m := newModel(mon)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// ensureDaemon starts the daemon if it is not already running.
// When projectDirs is non-empty, the --project flags are forwarded to the daemon.
func ensureDaemon(projectDirs []string) {
	if _, alive := daemon.ReadAlivePid(); alive {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	args := []string{"daemon", "start"}
	for _, dir := range projectDirs {
		args = append(args, "--project", dir)
	}
	child := exec.Command(exe, args...) // #nosec G204 -- re-exec of own binary via os.Executable()
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
	logMode  string // traffic log mode: "off", "meta", "full"

	issues        []trackerIssues // issues from configured tracker projects
	issuesLoading bool            // true while issue fetch is in flight
	issuesFetched time.Time       // when issues were last successfully fetched

	issueCursor    int       // index into flattenIssues() result
	dispatching    bool      // true while dispatch cmd is in-flight
	dispatchStatus string    // feedback flash: "Sent HUM-42 → session:0.1"
	dispatchAt     time.Time // when status was set; auto-cleared after 3s

	prevStatuses map[string]logparser.SessionStatus // previous session statuses for idle detection

	projects  []daemon.ProjectInfo // registered projects from daemon info
	activeTab int                  // index into tabs(); 0 = first project or "All"

	// Destructive operation confirmation overlay.
	pendingConfirms []daemon.PendingConfirm // from daemon polling
	confirmActive   bool                    // true when confirm overlay is shown
	confirmID       string                  // which pending op we're showing
	confirmPrompt   string                  // e.g. "DeleteIssue KAN-1?"
	confirmPIDs     map[int]bool            // PIDs of Claude instances with pending confirms

	// Create ticket form overlay.
	createActive  bool      // true when the create form is shown
	createForm    *huh.Form // the huh form (implements tea.Model)
	createTracker int       // selected tracker index (value bound to form)
	createTitle   string    // bound to form
	createDesc    string    // bound to form
}

func newModel(mon *monitor.Monitor) model {
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	sp.Style = lipgloss.NewStyle().Foreground(humanRed)
	return model{mon: mon, spinner: sp, width: defaultWidth, fetchGen: 1, fetching: true, logMode: "off", prevStatuses: make(map[string]logparser.SessionStatus)}
}

// --- messages ---

type fastTickMsg time.Time
type fullTickMsg time.Time

type snapshotMsg struct {
	snap *monitor.Snapshot
	gen  uint64
}

type logModeMsg    string              // result of log-mode set/get from daemon
type projectsMsg   []daemon.ProjectInfo // projects loaded from daemon info

type issueTickMsg  time.Time
type issuesResultMsg struct {
	results []trackerIssues
}

type pendingConfirmsMsg  []daemon.PendingConfirm
type confirmDecisionMsg  struct{ err error }
type createResultMsg     struct{ key string; trackerKind string; err error }

// --- tea.Model ---

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchFull(m.mon, 1), m.spinner.Tick, fastTickCmd(), fullTickCmd(), fetchLogModeCmd(), fetchIssuesCmd(), issueTickCmd(), fetchProjectsCmd())
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// When a destructive confirmation overlay is active, only handle y/n/Esc.
	if m.confirmActive {
		return m.handleConfirmKey(msg)
	}

	// When the create form is active, delegate all keys to it.
	if m.createActive {
		return m.handleCreateKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "l":
		next := cycleLogMode(m.logMode)
		m.logMode = next
		return m, setLogModeCmd(next)
	case "j", "down":
		flat := flattenIssues(m.issues)
		if len(flat) > 0 {
			m.issueCursor = min(m.issueCursor+1, len(flat)-1)
		}
		return m, nil
	case "k", "up":
		if m.issueCursor > 0 {
			m.issueCursor--
		}
		return m, nil
	case "enter":
		return m.handleDispatch()
	case "o":
		return m.handleOpenBrowser()
	case "n":
		return m.handleCreateStart()
	default:
		return m.handleTabKey(msg)
	}
}

func (m model) handleTabKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab":
		tabs := m.tabs()
		if len(tabs) > 1 {
			m.activeTab = (m.activeTab + 1) % len(tabs)
		}
	case "shift+tab":
		tabs := m.tabs()
		if len(tabs) > 1 {
			m.activeTab = (m.activeTab - 1 + len(tabs)) % len(tabs)
		}
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx := int(msg.Runes[0]-'0') - 1 // "1" → 0
		tabs := m.tabs()
		if idx < len(tabs) {
			m.activeTab = idx
		}
	}
	return m, nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case projectsMsg:
		m.applyProjects(msg)
	case tea.WindowSizeMsg:
		m.applyWindowSize(msg)
	case fastTickMsg:
		return m.handleFastTick()
	case fullTickMsg:
		return m.handleFullTick()
	case snapshotMsg:
		return m.handleSnapshot(msg)
	case issueTickMsg:
		return m.handleIssueTick()
	case issuesResultMsg:
		m.handleIssuesResult(msg)
	case dispatchResultMsg:
		m.handleDispatchResult(msg)
	case openBrowserMsg:
		m.handleOpenBrowserResult(msg)
	case pendingConfirmsMsg:
		m.handlePendingConfirms(msg)
	case confirmDecisionMsg:
		m.handleConfirmDecision(msg)
	case createResultMsg:
		return m.handleCreateResult(msg)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	default:
		return m.handleDefault(msg)
	}
	return m, nil
}

func (m model) handleFullTick() (tea.Model, tea.Cmd) {
	if m.fetching {
		return m, fullTickCmd()
	}
	m.fetching = true
	m.fetchGen++
	return m, tea.Batch(fetchFull(m.mon, m.fetchGen), fullTickCmd(), fetchProjectsCmd(), fetchPendingConfirmsCmd())
}

func (m model) handleSnapshot(msg snapshotMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.fetchGen {
		return m, nil // stale result, discard
	}
	m.snap = msg.snap
	m.fetching = false
	m.checkIdleTransitions()
	return m, nil
}

func (m *model) handleConfirmDecision(msg confirmDecisionMsg) {
	if msg.err != nil {
		m.dispatchStatus = fmt.Sprintf("Confirm failed: %s", msg.err)
		m.dispatchAt = time.Now()
	}
}

// checkIdleTransitions plays a notification sound when any instance
// transitions from working/blocked/waiting to ready (idle).
func (m model) checkIdleTransitions() {
	if m.snap == nil {
		return
	}
	bing := false
	current := make(map[string]logparser.SessionStatus, len(m.snap.Instances))
	for _, iv := range m.snap.Instances {
		if iv.Session == nil {
			continue
		}
		sid := iv.Session.SessionID
		cur := iv.Session.Status
		current[sid] = cur
		prev, known := m.prevStatuses[sid]
		if !known {
			continue
		}
		if cur == logparser.StatusReady && (prev == logparser.StatusWorking || prev == logparser.StatusBlocked || prev == logparser.StatusWaiting) {
			bing = true
		}
	}
	for k := range m.prevStatuses {
		delete(m.prevStatuses, k)
	}
	for k, v := range current {
		m.prevStatuses[k] = v
	}
	if bing {
		playNotificationSound()
	}
}

// handleFastTick processes the fast (100ms) tick for quick snapshot refreshes.
func (m model) handleFastTick() (tea.Model, tea.Cmd) {
	m.clearExpiredDispatchStatus()
	if m.fetching {
		return m, fastTickCmd()
	}
	m.fetching = true
	m.fetchGen++
	return m, tea.Batch(fetchQuick(m.mon, m.snap, m.fetchGen), fastTickCmd())
}

// applyProjects updates the project list and clamps the active tab.
func (m *model) applyProjects(projects projectsMsg) {
	m.projects = []daemon.ProjectInfo(projects)
	if m.activeTab >= len(m.tabs()) {
		m.activeTab = 0
	}
}

// clampCursor keeps issueCursor in bounds after the issue list changes.
func (m *model) clampCursor() {
	flat := flattenIssues(m.issues)
	if m.issueCursor >= len(flat) {
		m.issueCursor = max(0, len(flat)-1)
	}
}

// clearExpiredDispatchStatus clears the dispatch status flash after 3 seconds.
func (m *model) clearExpiredDispatchStatus() {
	if !m.dispatchAt.IsZero() && time.Since(m.dispatchAt) > 3*time.Second {
		m.dispatchStatus = ""
		m.dispatchAt = time.Time{}
	}
}

// --- dispatch ---

type dispatchResultMsg struct {
	issueKey  string
	paneLabel string
	err       error
}

func (m *model) handleDispatchResult(msg dispatchResultMsg) {
	m.dispatching = false
	if msg.err != nil {
		m.dispatchStatus = fmt.Sprintf("Failed: %s", msg.err)
	} else {
		m.dispatchStatus = fmt.Sprintf("Sent %s → %s", msg.issueKey, msg.paneLabel)
	}
	m.dispatchAt = time.Now()
}

func (m model) handleDispatch() (tea.Model, tea.Cmd) {
	if m.dispatching {
		return m, nil
	}
	flat := flattenIssues(m.issues)
	if len(flat) == 0 {
		m.dispatchStatus = "No issues"
		m.dispatchAt = time.Now()
		return m, nil
	}
	if m.issueCursor >= len(flat) {
		return m, nil
	}
	sel := flat[m.issueCursor]

	// Find first idle pane scoped to the active project tab.
	var target *claude.TmuxPane
	if m.snap != nil {
		panes := m.filterPanes(m.snap.Panes)
		for i := range panes {
			if panes[i].State == claude.StateReady {
				target = &panes[i]
				break
			}
		}
	}
	if target == nil {
		m.dispatchStatus = "No idle panes"
		m.dispatchAt = time.Now()
		return m, nil
	}

	// Build slash command based on tracker kind.
	var prompt string
	switch sel.TrackerKind {
	case "shortcut":
		prompt = "/human-plan " + sel.Issue.Key
	default:
		prompt = "/human-execute " + sel.Issue.Key
	}

	m.dispatching = true
	return m, dispatchIssueCmd(*target, prompt, sel.Issue.Key)
}

func dispatchIssueCmd(pane claude.TmuxPane, prompt, issueKey string) tea.Cmd {
	return func() tea.Msg {
		sender := &dispatch.TmuxSender{Runner: claude.OSCommandRunner{}}
		agent := dispatch.Agent{
			SessionName: pane.SessionName,
			WindowIndex: pane.WindowIndex,
			PaneIndex:   pane.PaneIndex,
			Label:       fmt.Sprintf("%s:%d.%d", pane.SessionName, pane.WindowIndex, pane.PaneIndex),
		}
		err := sender.SendPrompt(context.Background(), agent, prompt)
		return dispatchResultMsg{issueKey: issueKey, paneLabel: agent.Label, err: err}
	}
}

// --- open in browser ---

type openBrowserMsg struct {
	issueKey string
	err      error
}

func (m model) handleOpenBrowser() (tea.Model, tea.Cmd) {
	flat := flattenIssues(m.issues)
	if len(flat) == 0 || m.issueCursor >= len(flat) {
		return m, nil
	}
	sel := flat[m.issueCursor]
	if sel.Issue.URL == "" {
		m.dispatchStatus = "No URL for " + sel.Issue.Key
		m.dispatchAt = time.Now()
		return m, nil
	}
	return m, openBrowserCmd(sel.Issue.URL, sel.Issue.Key)
}

func (m *model) handleOpenBrowserResult(msg openBrowserMsg) {
	if msg.err != nil {
		m.dispatchStatus = fmt.Sprintf("Open failed: %s", msg.err)
	} else {
		m.dispatchStatus = fmt.Sprintf("Opened %s", msg.issueKey)
	}
	m.dispatchAt = time.Now()
}

func openBrowserCmd(url, issueKey string) tea.Cmd {
	return func() tea.Msg {
		err := browser.DefaultOpener{}.Open(url)
		return openBrowserMsg{issueKey: issueKey, err: err}
	}
}

// --- destructive confirmation overlay ---

func (m model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		id := m.confirmID
		m.confirmActive = false
		m.confirmID = ""
		m.confirmPrompt = ""
		m.dispatchStatus = "Approved"
		m.dispatchAt = time.Now()
		return m, sendConfirmCmd(id, true)
	case "n", "esc":
		id := m.confirmID
		m.confirmActive = false
		m.confirmID = ""
		m.confirmPrompt = ""
		m.dispatchStatus = "Aborted"
		m.dispatchAt = time.Now()
		return m, sendConfirmCmd(id, false)
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil // swallow all other keys while confirming
}

func (m *model) handlePendingConfirms(confirms []daemon.PendingConfirm) {
	m.pendingConfirms = confirms

	// Build PID set for instance state rendering.
	m.confirmPIDs = make(map[int]bool, len(confirms))
	for _, c := range confirms {
		if c.ClientPID > 0 {
			m.confirmPIDs[c.ClientPID] = true
		}
	}

	if len(confirms) > 0 && !m.confirmActive {
		m.confirmActive = true
		m.confirmID = confirms[0].ID
		m.confirmPrompt = confirms[0].Prompt
	}
	if len(confirms) == 0 && m.confirmActive {
		m.confirmActive = false
		m.confirmID = ""
		m.confirmPrompt = ""
	}
}

func sendConfirmCmd(id string, approved bool) tea.Cmd {
	return func() tea.Msg {
		addr, token := daemonAddr()
		if addr == "" {
			return confirmDecisionMsg{err: fmt.Errorf("daemon not available")}
		}
		err := daemon.SendConfirmDecision(addr, token, id, approved)
		return confirmDecisionMsg{err: err}
	}
}

func fetchPendingConfirmsCmd() tea.Cmd {
	return func() tea.Msg {
		addr, token := daemonAddr()
		if addr == "" {
			return pendingConfirmsMsg(nil)
		}
		confirms, err := daemon.GetPendingConfirms(addr, token)
		if err != nil {
			return pendingConfirmsMsg(nil)
		}
		return pendingConfirmsMsg(confirms)
	}
}

// --- create ticket form ---

// trackerOption is a unique tracker/project pair for the create form selector.
type trackerOption struct {
	Kind    string
	Role    string
	Project string
}

// applyWindowSize updates the terminal dimensions.
func (m *model) applyWindowSize(msg tea.WindowSizeMsg) {
	m.width = msg.Width
	m.height = msg.Height
}

// handleDefault routes unmatched messages: logModeMsg, and create form delegation.
func (m model) handleDefault(msg tea.Msg) (tea.Model, tea.Cmd) {
	if lm, ok := msg.(logModeMsg); ok {
		m.logMode = string(lm)
		return m, nil
	}
	if m.createActive && m.createForm != nil {
		return m.updateCreateForm(msg)
	}
	return m, nil
}

// handleCreateStart builds the create form from loaded tracker/project pairs and activates it.
func (m model) handleCreateStart() (tea.Model, tea.Cmd) {
	// Extract unique (kind, role, project) tuples from loaded issues.
	seen := make(map[trackerOption]bool)
	var options []trackerOption
	for _, g := range m.issues {
		role := g.TrackerRole
		if role == "" {
			role = inferRole(g.TrackerKind)
		}
		opt := trackerOption{Kind: g.TrackerKind, Role: role, Project: g.Project}
		if !seen[opt] {
			seen[opt] = true
			options = append(options, opt)
		}
	}
	if len(options) == 0 {
		m.dispatchStatus = "No trackers"
		m.dispatchAt = time.Now()
		return m, nil
	}

	// Default to first PM tracker.
	m.createTracker = 0
	for i, opt := range options {
		if opt.Role == "pm" {
			m.createTracker = i
			break
		}
	}
	m.createTitle = ""
	m.createDesc = ""

	selectOptions := make([]huh.Option[int], len(options))
	for i, opt := range options {
		selectOptions[i] = huh.NewOption(fmt.Sprintf("%s / %s", opt.Kind, opt.Project), i)
	}
	fields := []huh.Field{
		huh.NewSelect[int]().
			Title("Tracker").
			Options(selectOptions...).
			Value(&m.createTracker).
			Inline(true),
	}

	fields = append(fields,
		huh.NewInput().
			Title("Title").
			Value(&m.createTitle).
			Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return fmt.Errorf("title is required")
				}
				return nil
			}),
		huh.NewText().
			Title("Description").
			Value(&m.createDesc).
			Lines(5),
	)

	dialogWidth := min(60, m.width-10)
	m.createForm = huh.NewForm(huh.NewGroup(fields...)).WithTheme(huh.ThemeCharm()).WithWidth(dialogWidth)
	m.createActive = true

	return m, m.createForm.Init()
}

// handleCreateKey delegates key messages to the create form.
func (m model) handleCreateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.createActive = false
		m.createForm = nil
		return m, nil
	}
	return m.updateCreateForm(msg)
}

// updateCreateForm passes a message to the create form and checks its state.
func (m model) updateCreateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	form, cmd := m.createForm.Update(msg)
	m.createForm = form.(*huh.Form)

	if m.createForm.State == huh.StateCompleted {
		m.createActive = false
		m.createForm = nil

		// Resolve the selected tracker/project.
		seen := make(map[trackerOption]bool)
		var options []trackerOption
		for _, g := range m.issues {
			role := g.TrackerRole
			if role == "" {
				role = inferRole(g.TrackerKind)
			}
			opt := trackerOption{Kind: g.TrackerKind, Role: role, Project: g.Project}
			if !seen[opt] {
				seen[opt] = true
				options = append(options, opt)
			}
		}
		if m.createTracker >= 0 && m.createTracker < len(options) {
			sel := options[m.createTracker]
			return m, createTicketCmd(sel.Kind, sel.Project, m.createTitle, m.createDesc)
		}
		m.dispatchStatus = "Create failed: invalid tracker"
		m.dispatchAt = time.Now()
		return m, nil
	}

	if m.createForm.State == huh.StateAborted {
		m.createActive = false
		m.createForm = nil
		return m, nil
	}

	return m, cmd
}

// handleIssuesResult processes an issues fetch result.
func (m *model) handleIssuesResult(msg issuesResultMsg) {
	m.issues = msg.results
	m.issuesLoading = false
	m.issuesFetched = time.Now()
	m.clampCursor()
}

// handleCreateResult processes the result of a ticket creation.
func (m model) handleCreateResult(msg createResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.dispatchStatus = fmt.Sprintf("Create failed: %s", msg.err)
		m.dispatchAt = time.Now()
		return m, nil
	}

	m.dispatchStatus = fmt.Sprintf("Created %s", msg.key)
	m.dispatchAt = time.Now()
	m.issuesLoading = true

	// Auto-dispatch /human-ready to an idle pane.
	if !m.dispatching && m.snap != nil {
		panes := m.filterPanes(m.snap.Panes)
		for i := range panes {
			if panes[i].State == claude.StateReady {
				m.dispatching = true
				prompt := "/human-ready " + msg.trackerKind + " " + msg.key
				return m, tea.Batch(fetchIssuesCmd(), dispatchIssueCmd(panes[i], prompt, msg.key))
			}
		}
	}

	return m, fetchIssuesCmd()
}

func createTicketCmd(trackerKind, project, title, description string) tea.Cmd {
	return func() tea.Msg {
		addr, token := daemonAddr()
		if addr == "" {
			return createResultMsg{err: fmt.Errorf("daemon not available")}
		}
		args := []string{trackerKind, "issue", "create",
			"--project=" + project, title}
		if description != "" {
			args = append(args, "--description", description)
		}
		out, err := daemon.RunRemoteCapture(addr, token, args)
		if err != nil {
			return createResultMsg{err: err}
		}
		key := strings.TrimSpace(string(out))
		if i := strings.IndexByte(key, '\t'); i >= 0 {
			key = key[:i]
		}
		return createResultMsg{key: key, trackerKind: trackerKind}
	}
}

// renderCreateDialog renders the create ticket form inside a centered bordered dialog.
func renderCreateDialog(formView string, width, height int) string {
	title := titleStyle.Render("New Ticket")
	hints := subtleStyle.Render("Tab next  Enter submit  Esc cancel")

	content := title + "\n\n" + formView + "\n" + hints
	dialog := dialogStyle.Render(content)

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, dialog)
}

func (m model) handleIssueTick() (tea.Model, tea.Cmd) {
	if m.issuesLoading {
		return m, issueTickCmd()
	}
	m.issuesLoading = true
	return m, tea.Batch(fetchIssuesCmd(), issueTickCmd())
}

func (m model) View() string {
	if m.quitting {
		return ""
	}
	w := m.width
	if w < 40 {
		w = defaultWidth
	}

	// Create ticket dialog — skip dashboard rendering entirely.
	if m.createActive && m.createForm != nil {
		return renderCreateDialog(m.createForm.View(), w, m.height)
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

	// Tracker status.
	if ts := renderTrackers(m.snap.Trackers, w); ts != "" {
		b.WriteString(ts)
		b.WriteByte('\n')
	}

	// Tab bar (only when 2+ projects).
	if tabBar := renderTabBar(m.tabs(), m.activeTab, w); tabBar != "" {
		b.WriteString(tabBar)
		b.WriteByte('\n')
	}

	b.WriteByte('\n')

	if m.snap.Err != nil {
		b.WriteString(errorStyle.Render("  Error: " + m.snap.Err.Error()))
		b.WriteByte('\n')
		return b.String()
	}

	// Instances (filtered by active tab).
	filtered := m.filterInstances(m.snap.Instances)
	if len(filtered) == 0 {
		b.WriteString(subtleStyle.Render("  No active instances"))
		b.WriteByte('\n')
	} else {
		for _, iv := range filtered {
			m.renderInstance(&b, iv, w)
		}
		renderTotalLine(&b, m.snap.TotalUsage, w)
	}

	// Tmux panes.
	if len(m.snap.Panes) > 0 {
		b.WriteByte('\n')
		b.WriteString(renderPanes(m.snap.Panes))
	}

	// Issues panel.
	if ip := renderIssuesPanel(m.issues, m.issuesFetched, w, m.issueCursor); ip != "" {
		b.WriteByte('\n')
		b.WriteString(ip)
	}

	// Footer.
	b.WriteByte('\n')
	if m.confirmActive {
		b.WriteString(renderConfirmFooter(w, m.confirmPrompt))
	} else {
		b.WriteString(renderFooter(w, m.logMode, m.dispatchStatus, len(m.tabs()) > 0))
	}
	b.WriteByte('\n')

	return b.String()
}

// --- commands ---

func fastTickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return fastTickMsg(t) })
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
	// Override with confirm state when this instance has a pending confirmation.
	var icon string
	var labelStyle lipgloss.Style
	if iv.Usage.Instance.PID > 0 && m.confirmPIDs[iv.Usage.Instance.PID] {
		icon = confirmStyle.Render("●")
		labelStyle = confirmStyle
	} else {
		icon = m.sessionIcon(iv.Session)
		labelStyle = sessionLabelStyle(iv.Session)
	}
	header := "  " + icon + " " + labelStyle.Render(iv.Usage.Instance.Label)
	if iv.Usage.Instance.DaemonConnected {
		if iv.Usage.Instance.ProxyConfigured {
			header += "  " + specialStyle.Render("⚡+proxy")
		} else {
			header += "  " + specialStyle.Render("⚡")
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
	if ctx := sessionContext(iv.Session); ctx != "" {
		header += "  " + ctx
	}
	b.WriteString(header)
	b.WriteByte('\n')

	// Progress bars per model.
	renderModelBars(b, iv.Usage.Summary, w)

	// Subagents + tasks.
	if iv.Session != nil {
		m.renderSubagents(b, iv.Session.Subagents)
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

func (m model) renderSubagents(b *strings.Builder, subagents []logparser.Subagent) {
	if len(subagents) == 0 {
		return
	}

	// Filter out completed agents older than 5s.
	var visible []logparser.Subagent
	for _, sa := range subagents {
		if sa.CompletedAt != nil && time.Since(*sa.CompletedAt) > 5*time.Second {
			continue
		}
		visible = append(visible, sa)
	}
	if len(visible) == 0 {
		return
	}

	start := 0
	if len(visible) > 5 {
		start = len(visible) - 5
	}
	for i := start; i < len(visible); i++ {
		sa := visible[i]
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

// --- render: trackers ---

func renderTrackers(trackers []tracker.TrackerStatus, _ int) string {
	counts := make(map[string]int)
	labels := make(map[string]string) // kind → Label
	var order []string
	for _, t := range trackers {
		if !t.Working {
			continue
		}
		if counts[t.Kind] == 0 {
			order = append(order, t.Kind)
			labels[t.Kind] = t.Label
		}
		counts[t.Kind]++
	}
	if len(order) == 0 {
		return ""
	}

	var parts []string
	for _, kind := range order {
		s := labels[kind]
		if counts[kind] > 1 {
			s += fmt.Sprintf(" (%d)", counts[kind])
		}
		parts = append(parts, s)
	}

	return "  " + subtleStyle.Render("Trackers") + "  " + strings.Join(parts, "  ")
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
		case claude.StateBlocked:
			icon = warningStyle.Render("●")
		case claude.StateWaiting:
			icon = waitingStyle.Render("●")
		case claude.StateError:
			icon = accentStyle.Render("⚠")
		case claude.StateConfirm:
			icon = confirmStyle.Render("●")
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

func renderConfirmFooter(w int, prompt string) string {
	left := confirmStyle.Render("  ⚠ " + prompt)
	right := confirmStyle.Render("y confirm  n abort")
	gap := w - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func renderFooter(w int, logMode, dispatchStatus string, showTabs bool) string {
	left := subtleStyle.Render("  ↻ live")
	if logMode != "" {
		left += "  " + subtleStyle.Render("log:"+logMode)
	}
	if dispatchStatus != "" {
		left += "  " + specialStyle.Render(dispatchStatus)
	}
	keys := "j/k nav  ⏎ send  o open  n new  l log  q quit"
	if showTabs {
		keys = "Tab switch  " + keys
	}
	right := subtleStyle.Render(keys)
	gap := w - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// --- render: icon ---

func sessionLabelStyle(sess *logparser.SessionState) lipgloss.Style {
	if sess == nil {
		return idleInstanceStyle
	}
	switch sess.Status {
	case logparser.StatusWorking:
		return busyInstanceStyle
	case logparser.StatusError:
		return errorStyle
	case logparser.StatusBlocked:
		return warningStyle
	case logparser.StatusWaiting:
		return specialStyle
	default:
		return idleInstanceStyle
	}
}

// sessionContext returns a styled string with contextual info from hook events:
// current tool being executed, blocked tool name, or error type.
func sessionContext(sess *logparser.SessionState) string {
	if sess == nil {
		return ""
	}
	switch {
	case sess.Status == logparser.StatusError && sess.ErrorType != "":
		return errorStyle.Render(sess.ErrorType)
	case sess.Status == logparser.StatusBlocked && sess.BlockedTool != "":
		return warningStyle.Render("⚠ " + sess.BlockedTool)
	case sess.CurrentTool != "":
		return subtleStyle.Render("[" + sess.CurrentTool + "]")
	default:
		return ""
	}
}

func (m model) sessionIcon(sess *logparser.SessionState) string {
	if sess == nil {
		return subtleStyle.Render("○")
	}
	switch sess.Status {
	case logparser.StatusWorking:
		return m.spinner.View()
	case logparser.StatusError:
		return accentStyle.Render("⚠")
	case logparser.StatusBlocked:
		return warningStyle.Render("●")
	case logparser.StatusWaiting:
		return waitingStyle.Render("●")
	default:
		if !sess.LastActivity.IsZero() {
			return specialStyle.Render("●")
		}
		return subtleStyle.Render("○")
	}
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

// formatTokens delegates to claude.FormatTokens for token count formatting.
func formatTokens(n int) string {
	return claude.FormatTokens(n)
}

// --- project tabs ---

// tab represents a single project tab in the TUI.
type tab struct {
	Name string // display name
	Dir  string // project directory (empty for "Other" tab)
}

// tabs builds the list of visible tabs from registered projects.
// Returns nil when no projects are registered.
func (m model) tabs() []tab {
	if len(m.projects) == 0 {
		return nil
	}
	out := make([]tab, 0, len(m.projects)+1)
	for _, p := range m.projects {
		out = append(out, tab{Name: p.Name, Dir: p.Dir})
	}
	// Append "Other" tab only if there are unmatched instances.
	if m.snap != nil && hasUnmatchedInstances(m.snap.Instances, m.projects) {
		out = append(out, tab{Name: "Other"})
	}
	return out
}

// filterInstances returns the instances that belong to the active tab.
// When there are no tabs (single project or none), all instances are returned.
func (m model) filterInstances(instances []monitor.InstanceView) []monitor.InstanceView {
	tabs := m.tabs()
	if len(tabs) == 0 {
		return instances
	}
	if m.activeTab >= len(tabs) {
		return instances
	}
	active := tabs[m.activeTab]
	if active.Dir == "" {
		// "Other" tab: instances not matching any project.
		return unmatchedInstances(instances, m.projects)
	}
	var out []monitor.InstanceView
	for _, iv := range instances {
		if strings.HasPrefix(iv.Usage.Instance.Cwd, active.Dir) {
			out = append(out, iv)
		}
	}
	return out
}

// hasUnmatchedInstances returns true if any instance does not match a registered project.
func hasUnmatchedInstances(instances []monitor.InstanceView, projects []daemon.ProjectInfo) bool {
	for _, iv := range instances {
		if !matchesAnyProject(iv.Usage.Instance.Cwd, projects) {
			return true
		}
	}
	return false
}

// unmatchedInstances returns instances whose Cwd does not match any project dir.
func unmatchedInstances(instances []monitor.InstanceView, projects []daemon.ProjectInfo) []monitor.InstanceView {
	var out []monitor.InstanceView
	for _, iv := range instances {
		if !matchesAnyProject(iv.Usage.Instance.Cwd, projects) {
			out = append(out, iv)
		}
	}
	return out
}

// filterPanes returns the panes that belong to the active tab.
// When there are no tabs (single project or none), all panes are returned.
func (m model) filterPanes(panes []claude.TmuxPane) []claude.TmuxPane {
	tabs := m.tabs()
	if len(tabs) == 0 {
		return panes
	}
	if m.activeTab >= len(tabs) {
		return panes
	}
	active := tabs[m.activeTab]
	if active.Dir == "" {
		return unmatchedPanes(panes, m.projects)
	}
	var out []claude.TmuxPane
	for _, p := range panes {
		if strings.HasPrefix(p.Cwd, active.Dir) {
			out = append(out, p)
		}
	}
	return out
}

// unmatchedPanes returns panes whose Cwd does not match any project dir.
func unmatchedPanes(panes []claude.TmuxPane, projects []daemon.ProjectInfo) []claude.TmuxPane {
	var out []claude.TmuxPane
	for _, p := range panes {
		if !matchesAnyProject(p.Cwd, projects) {
			out = append(out, p)
		}
	}
	return out
}

// matchesAnyProject returns true if cwd starts with any project's Dir.
func matchesAnyProject(cwd string, projects []daemon.ProjectInfo) bool {
	for _, p := range projects {
		if strings.HasPrefix(cwd, p.Dir) {
			return true
		}
	}
	return false
}

// renderTabBar renders a horizontal tab bar. Returns "" when tabs are not applicable.
func renderTabBar(tabs []tab, active int, w int) string {
	if len(tabs) == 0 {
		return ""
	}

	var parts []string
	for i, t := range tabs {
		label := fmt.Sprintf(" %d:%s ", i+1, t.Name)
		if i == active {
			parts = append(parts, activeTabStyle.Render(label))
		} else {
			parts = append(parts, inactiveTabStyle.Render(label))
		}
	}
	line := "  " + strings.Join(parts, " ")
	// Pad or truncate to width.
	visible := lipgloss.Width(line)
	if visible < w-2 {
		line += strings.Repeat(" ", w-2-visible)
	}
	return line
}

// fetchProjectsCmd loads project info from the daemon info file.
func fetchProjectsCmd() tea.Cmd {
	return func() tea.Msg {
		info, err := daemon.ReadInfo()
		if err != nil {
			return projectsMsg(nil)
		}
		return projectsMsg(info.Projects)
	}
}

// --- log mode ---

// cycleLogMode cycles through full → meta → off → full.
func cycleLogMode(current string) string {
	switch current {
	case "full":
		return "meta"
	case "meta":
		return "off"
	default:
		return "full"
	}
}

// daemonAddr returns the daemon address and token for direct TCP communication.
func daemonAddr() (string, string) {
	addr := os.Getenv("HUMAN_DAEMON_ADDR")
	token := os.Getenv("HUMAN_DAEMON_TOKEN")
	if addr == "" {
		if info, err := daemon.ReadInfo(); err == nil {
			addr = info.Addr
			if token == "" {
				token = info.Token
			}
		}
	}
	return addr, token
}

// fetchLogModeCmd fetches the current log mode from the daemon.
func fetchLogModeCmd() tea.Cmd {
	return func() tea.Msg {
		addr, token := daemonAddr()
		if addr == "" {
			return logModeMsg("full")
		}
		mode, err := daemon.GetLogMode(addr, token)
		if err != nil {
			return logModeMsg("full")
		}
		return logModeMsg(mode)
	}
}

// setLogModeCmd sends a log-mode change to the daemon.
func setLogModeCmd(mode string) tea.Cmd {
	return func() tea.Msg {
		addr, token := daemonAddr()
		if addr == "" {
			return logModeMsg(mode)
		}
		result, err := daemon.SetLogMode(addr, token, mode)
		if err != nil {
			return logModeMsg(mode) // optimistic
		}
		return logModeMsg(result)
	}
}

// --- issue fetching ---

func issueTickCmd() tea.Cmd {
	return tea.Tick(30*time.Second, func(t time.Time) tea.Msg { return issueTickMsg(t) })
}

func fetchIssuesCmd() tea.Cmd {
	return func() tea.Msg {
		addr, token := daemonAddr()
		if addr == "" {
			return issuesResultMsg{}
		}
		results, err := daemon.GetTrackerIssues(addr, token)
		if err != nil {
			return issuesResultMsg{}
		}
		return issuesResultMsg{results: fromDaemonResults(results)}
	}
}

func fromDaemonResults(results []daemon.TrackerIssuesResult) []trackerIssues {
	out := make([]trackerIssues, len(results))
	for i, r := range results {
		out[i] = trackerIssues{
			TrackerName: r.TrackerName,
			TrackerKind: r.TrackerKind,
			TrackerRole: r.TrackerRole,
			Project:     r.Project,
			Issues:      r.Issues,
		}
		if r.Err != "" {
			out[i].Err = fmt.Errorf("%s", r.Err)
		}
	}
	return out
}

// --- render: issues ---

// inferRole returns a role based on tracker kind when no explicit role is configured.
func inferRole(trackerKind string) string {
	switch trackerKind {
	case "shortcut":
		return "pm"
	case "linear":
		return "engineering"
	default:
		return ""
	}
}

// pipelineStage maps a tracker role and status type to a human-readable
// pipeline stage label. The pipeline model is:
//
//	PM:   Ready for Plan -> Planning -> Planned
//	Eng:  Backlog -> In Dev -> Done -> Closed
func pipelineStage(trackerKind, trackerRole, statusName, statusType string) string {
	role := trackerRole
	if role == "" {
		role = inferRole(trackerKind)
	}
	switch role {
	case "pm":
		switch statusType {
		case "unstarted":
			return "Ready for Plan"
		case "started":
			return "Planning"
		case "done":
			return "Planned"
		default:
			return statusName
		}
	case "engineering":
		switch statusType {
		case "unstarted":
			return "Backlog"
		case "started":
			return "In Dev"
		case "done":
			return "Done"
		case "closed":
			return "Closed"
		default:
			return statusName
		}
	default:
		return statusName
	}
}

// pipelineStageStyle returns a lipgloss style for the given status type,
// reflecting progress through the pipeline.
func pipelineStageStyle(statusType string) lipgloss.Style {
	switch statusType {
	case "started":
		return warningStyle // yellow -- in progress
	case "done":
		return specialStyle // teal -- complete
	case "unstarted", "closed":
		return subtleStyle
	default:
		return subtleStyle
	}
}

// pipelineName returns a display label based on the tracker's role.
func pipelineName(trackerKind, trackerRole string) string {
	role := trackerRole
	if role == "" {
		role = inferRole(trackerKind)
	}
	switch role {
	case "pm":
		return warningStyle.Render("PM")
	case "engineering":
		return specialStyle.Render("Eng")
	default:
		return subtleStyle.Render(trackerKind)
	}
}

func renderIssuesPanel(groups []trackerIssues, fetchedAt time.Time, w, cursor int) string {
	if len(groups) == 0 {
		return ""
	}

	var b strings.Builder

	header := "  " + subtleStyle.Render("Pipeline")
	if !fetchedAt.IsZero() {
		header += "  " + subtleStyle.Render(formatElapsed(time.Since(fetchedAt)) + " ago")
	}
	b.WriteString(header)
	b.WriteByte('\n')

	flatIdx := 0
	first := true
	for _, g := range groups {
		if g.Err != nil {
			if !first {
				b.WriteByte('\n')
			}
			first = false
			_, _ = fmt.Fprintf(&b, "    %s %s/%s: %s\n",
				errorStyle.Render("!"),
				g.TrackerKind, g.Project,
				subtleStyle.Render("fetch failed"))
			continue
		}
		if len(g.Issues) == 0 {
			continue
		}

		if !first {
			b.WriteByte('\n')
		}
		first = false

		pipelineLabel := pipelineName(g.TrackerKind, g.TrackerRole)
		_, _ = fmt.Fprintf(&b, "    %s %s %s\n",
			subtleStyle.Render("▸"),
			pipelineLabel,
			subtleStyle.Render(g.Project))

		for _, issue := range g.Issues {
			title := truncate(issue.Title, w-30)
			stage := pipelineStage(g.TrackerKind, g.TrackerRole, issue.Status, issue.StatusType)
			stageStyled := pipelineStageStyle(issue.StatusType).Render(truncate(stage, 14))
			keyStyle := titleStyle
			prefix := "      "
			if flatIdx == cursor {
				keyStyle = selectedStyle
				prefix = "    ▸ "
			}
			_, _ = fmt.Fprintf(&b, "%s%-12s %-14s %s\n",
				prefix,
				keyStyle.Render(issue.Key),
				stageStyled,
				title)
			flatIdx++
		}
	}

	return b.String()
}

