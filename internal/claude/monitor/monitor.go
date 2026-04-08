package monitor

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/StephanSchmidt/human/internal/claude"
	"github.com/StephanSchmidt/human/internal/claude/hookevents"
	"github.com/StephanSchmidt/human/internal/claude/logparser"
	"github.com/StephanSchmidt/human/internal/daemon"
	"github.com/StephanSchmidt/human/internal/proxy"
	"github.com/StephanSchmidt/human/internal/slack"
	"github.com/StephanSchmidt/human/internal/telegram"
)

// Monitor owns the data-fetching and state-reconciliation cycle for the TUI.
type Monitor struct {
	finder       claude.InstanceFinder
	dockerClient claude.DockerClient

	// parsersMu guards parsers. Although the TUI currently serialises
	// FetchFull/FetchQuick via its own m.fetching flag, future callers
	// (background health checks, parallel tests) must not rely on that
	// invariant — concurrent map access on parsers would otherwise crash
	// the program.
	parsersMu sync.Mutex
	parsers   map[string]*logparser.FileParser
}

// New creates a Monitor. dockerClient may be nil when Docker is unavailable.
func New(finder claude.InstanceFinder, dc claude.DockerClient) *Monitor {
	return &Monitor{
		finder:       finder,
		dockerClient: dc,
		parsers:      make(map[string]*logparser.FileParser),
	}
}

// FetchFull performs complete discovery, JSONL parsing, and hook event reading.
func (m *Monitor) FetchFull(ctx context.Context) *Snapshot {
	now := time.Now()
	snap := &Snapshot{FetchedAt: now}

	pid, alive := daemon.ReadAlivePid()
	snap.Daemon = DaemonState{PID: pid, Alive: alive}
	if info, err := daemon.ReadInfo(); err == nil {
		snap.Daemon.ProxyAddr = info.ProxyAddr
	}
	snap.Daemon.ProxyActiveConns = proxy.ReadStats(proxy.StatsPath()).ActiveConns
	snap.Telegram = telegramStatus()
	snap.Slack = slackStatus()
	if snap.Daemon.Alive {
		if info, err := daemon.ReadInfo(); err == nil {
			snap.Trackers, _ = daemon.GetTrackerDiagnose(info.Addr, info.Token)
		}
	}

	instances, err := m.finder.FindInstances(ctx)
	if err != nil {
		snap.Err = err
		return snap
	}

	// Read daemon-connected PIDs for later matching.
	snap.connectedPIDs = readConnectedPIDs()

	usages := claude.CollectInstanceUsage(instances, now)

	// Tmux panes (best-effort).
	panes := m.findPanes(ctx, instances)

	// JSONL session parsing.
	sessionByPath := m.parseSessions(instances)

	// Hook events from daemon in-memory store (authoritative for state).
	hookSnaps := fetchDaemonHookSnapshots(snap.Daemon.Alive)
	overlayHookState(sessionByPath, hookSnaps)
	fillMissingFromHooks(instances, sessionByPath, hookSnaps)

	// Match sessions to instances, then mark daemon-connected ones.
	snap.Instances = matchInstances(usages, sessionByPath)
	applyDaemonConnectedViews(snap.Instances, snap.connectedPIDs)

	// Match pane states from sessions.
	matchPaneStates(panes, sessionByPath, instances)
	snap.Panes = panes

	// Carry forward for fetchQuick.
	snap.sessionByPath = sessionByPath

	// Pre-compute total usage.
	snap.TotalUsage = aggregateUsage(usages)

	return snap
}

// FetchQuick updates daemon status, pane states, and hook-based working/idle
// on an existing snapshot. Instances, sessions, agents, and tasks are carried
// forward to avoid flicker.
func (m *Monitor) FetchQuick(_ context.Context, prev *Snapshot) *Snapshot {
	if prev == nil {
		return nil
	}

	// Shallow copy carried-forward data.
	snap := *prev
	snap.FetchedAt = time.Now()

	pid, alive := daemon.ReadAlivePid()
	snap.Daemon = DaemonState{PID: pid, Alive: alive}
	if info, err := daemon.ReadInfo(); err == nil {
		snap.Daemon.ProxyAddr = info.ProxyAddr
	}
	snap.Daemon.ProxyActiveConns = proxy.ReadStats(proxy.StatsPath()).ActiveConns
	snap.connectedPIDs = readConnectedPIDs()

	// Re-read hook events from daemon for sub-second state transitions.
	hookSnaps := fetchDaemonHookSnapshots(snap.Daemon.Alive)
	if len(hookSnaps) > 0 {
		// Deep-copy sessionByPath before mutating.
		byPath := make(map[string]logparser.SessionState, len(snap.sessionByPath))
		for k, v := range snap.sessionByPath {
			byPath[k] = v
		}
		overlayHookState(byPath, hookSnaps)
		fillMissingFromHooks(extractInstances(prev.Instances), byPath, hookSnaps)
		snap.sessionByPath = byPath

		// Rebuild instance views with updated sessions.
		snap.Instances = matchInstances(extractUsages(prev.Instances), byPath)
	}

	applyDaemonConnectedViews(snap.Instances, snap.connectedPIDs)

	// Carry forward panes from prev, updating only their states from hook data.
	if len(prev.Panes) > 0 {
		panes := make([]claude.TmuxPane, len(prev.Panes))
		copy(panes, prev.Panes)
		matchPaneStates(panes, snap.sessionByPath, extractInstances(prev.Instances))
		snap.Panes = panes
	}

	return &snap
}

// --- internal helpers ---

func (m *Monitor) findPanes(ctx context.Context, instances []claude.Instance) []claude.TmuxPane {
	containerIDs := collectContainerIDs(instances)
	runner := claude.OSCommandRunner{}
	tmuxClient := &claude.OSTmuxClient{Runner: runner}
	procLister := &claude.OSProcessLister{Runner: runner}
	panes, _ := claude.FindClaudePanes(ctx, tmuxClient, procLister, containerIDs)
	return panes
}

func (m *Monitor) parseSessions(instances []claude.Instance) map[string]logparser.SessionState {
	byPath := make(map[string]logparser.SessionState)
	reader := logparser.OSFileReader{}
	active := make(map[string]bool, len(instances))
	m.parsersMu.Lock()
	defer m.parsersMu.Unlock()
	for _, inst := range instances {
		if inst.FilePath == "" {
			continue
		}
		active[inst.FilePath] = true
		parser, ok := m.parsers[inst.FilePath]
		if !ok {
			parser = logparser.NewFileParser()
			m.parsers[inst.FilePath] = parser
		}
		state, parseErr := parser.Update(reader, inst.FilePath)
		if parseErr != nil {
			log.Debug().Err(parseErr).Str("path", inst.FilePath).Msg("session parse failed")
			continue
		}
		if state.SessionID != "" {
			byPath[inst.FilePath] = state
		}
	}
	// Prune parsers for files no longer referenced by any instance.
	// This prevents stale state from lingering when a PID's JSONL path
	// changes (e.g. after resolveJSONLPath corrects a startup race).
	for path := range m.parsers {
		if !active[path] {
			delete(m.parsers, path)
		}
	}
	return byPath
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

// fetchDaemonHookSnapshots reads hook state from the daemon's in-memory store.
// Returns nil when daemon is unavailable.
func fetchDaemonHookSnapshots(daemonAlive bool) map[string]hookevents.SessionSnapshot {
	if !daemonAlive {
		return nil
	}
	info, err := daemon.ReadInfo()
	if err != nil || info.Addr == "" {
		return nil
	}
	snap, err := daemon.GetHookSnapshot(info.Addr, info.Token)
	if err != nil {
		return nil
	}
	return snap
}

// overlayHookState updates sessions in byPath from hook snapshots.
// Hook state is authoritative only when its last event is at least as recent
// as the JSONL-derived last activity. Async hooks can arrive out of order
// (e.g. PermissionRequest after Stop), so stale hook snapshots are skipped.
func overlayHookState(byPath map[string]logparser.SessionState, hooks map[string]hookevents.SessionSnapshot) {
	if len(hooks) == 0 {
		return
	}
	for path, sess := range byPath {
		snap, ok := hooks[sess.SessionID]
		if !ok {
			continue
		}
		if snap.LastEventAt.Before(sess.LastActivity) {
			continue
		}
		sess.Status = snap.Status
		sess.CurrentTool = snap.CurrentTool
		sess.BlockedTool = snap.BlockedTool
		sess.ErrorType = snap.ErrorType
		sess.LastActivity = snap.LastEventAt
		byPath[path] = sess
	}
}

// fillMissingFromHooks creates session state from hook snapshots for instances
// that have no JSONL session yet (e.g. freshly started Claude waiting for input).
func fillMissingFromHooks(instances []claude.Instance, byPath map[string]logparser.SessionState, hooks map[string]hookevents.SessionSnapshot) {
	if len(hooks) == 0 {
		return
	}
	// Collect session IDs already matched via JSONL.
	matched := make(map[string]bool, len(byPath))
	for _, sess := range byPath {
		matched[sess.SessionID] = true
	}
	// Index unmatched hooks by cwd for instance matching. When
	// multiple snapshots share the same cwd, keep the one with the
	// most recent LastEventAt so ordering is deterministic across
	// runs (map iteration is random).
	byCwd := make(map[string]hookevents.SessionSnapshot)
	for _, snap := range hooks {
		if matched[snap.SessionID] || snap.Cwd == "" {
			continue
		}
		if existing, ok := byCwd[snap.Cwd]; ok {
			if !snap.LastEventAt.After(existing.LastEventAt) {
				continue
			}
		}
		byCwd[snap.Cwd] = snap
	}
	// Match instances without a session to hook snapshots by cwd.
	for _, inst := range instances {
		if inst.FilePath == "" || inst.Cwd == "" {
			continue
		}
		if _, hasSession := byPath[inst.FilePath]; hasSession {
			continue
		}
		snap, ok := byCwd[inst.Cwd]
		if !ok {
			continue
		}
		byPath[inst.FilePath] = logparser.SessionState{
			SessionID:    snap.SessionID,
			Cwd:          snap.Cwd,
			Status:       snap.Status,
			LastActivity: snap.LastEventAt,
			CurrentTool:  snap.CurrentTool,
			BlockedTool:  snap.BlockedTool,
			ErrorType:    snap.ErrorType,
		}
	}
}

// matchInstances pairs each InstanceUsage with its matched session.
func matchInstances(usages []claude.InstanceUsage, byPath map[string]logparser.SessionState) []InstanceView {
	views := make([]InstanceView, len(usages))
	for i, iu := range usages {
		views[i] = InstanceView{Usage: iu}
		if sess, ok := byPath[iu.Instance.FilePath]; ok {
			s := sess // copy
			views[i].Session = &s
		}
	}
	return views
}

// extractUsages returns the InstanceUsage slice from InstanceViews.
func extractUsages(views []InstanceView) []claude.InstanceUsage {
	usages := make([]claude.InstanceUsage, len(views))
	for i, v := range views {
		usages[i] = v.Usage
	}
	return usages
}

// extractInstances returns the Instance slice from InstanceViews.
func extractInstances(views []InstanceView) []claude.Instance {
	instances := make([]claude.Instance, len(views))
	for i, v := range views {
		instances[i] = v.Usage.Instance
	}
	return instances
}

// matchPaneStates sets each pane's State by matching to a parsed session.
func matchPaneStates(panes []claude.TmuxPane, byPath map[string]logparser.SessionState, instances []claude.Instance) {
	// Build PID → session map.
	byPID := make(map[int]logparser.SessionState)
	for _, inst := range instances {
		if inst.PID > 0 && inst.FilePath != "" {
			if s, ok := byPath[inst.FilePath]; ok {
				byPID[inst.PID] = s
			}
		}
	}

	sessions := make([]logparser.SessionState, 0, len(byPath))
	for _, s := range byPath {
		sessions = append(sessions, s)
	}

	for i := range panes {
		if panes[i].ClaudePID > 0 {
			if s, ok := byPID[panes[i].ClaudePID]; ok {
				panes[i].State = sessionToState(s)
				continue
			}
		}
		for _, s := range sessions {
			if s.Cwd != "" && panes[i].Cwd == s.Cwd {
				panes[i].State = sessionToState(s)
				break
			}
		}
	}
}

func sessionToState(s logparser.SessionState) claude.InstanceState {
	switch s.Status {
	case logparser.StatusWorking:
		return claude.StateBusy
	case logparser.StatusError:
		return claude.StateError
	case logparser.StatusBlocked:
		return claude.StateBlocked
	case logparser.StatusWaiting:
		return claude.StateWaiting
	default:
		return claude.StateReady
	}
}

func aggregateUsage(usages []claude.InstanceUsage) *claude.UsageSummary {
	total := &claude.UsageSummary{Models: make(map[string]*claude.ModelUsage)}
	for _, iu := range usages {
		claude.MergeUsage(total, iu.Summary)
	}
	return total
}

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

func slackStatus() string {
	configs, err := slack.LoadConfigs(".")
	if err != nil {
		return "Slack: config error"
	}
	if len(configs) == 0 {
		return ""
	}
	instances, err := slack.LoadInstances(".")
	if err != nil {
		return "Slack: config error"
	}
	if len(instances) == 0 {
		return "Slack: missing token"
	}
	return "Slack connected"
}

// readConnectedPIDs reads the set of daemon-connected PIDs from disk.
func readConnectedPIDs() map[int]bool {
	pids := daemon.ReadConnected(daemon.ConnectedPath())
	if len(pids) == 0 {
		return nil
	}
	m := make(map[int]bool, len(pids))
	for _, pid := range pids {
		m[pid] = true
	}
	return m
}

// applyDaemonConnectedViews sets DaemonConnected on instance views whose PID is in the connected set.
func applyDaemonConnectedViews(views []InstanceView, connected map[int]bool) {
	for i := range views {
		pid := views[i].Usage.Instance.PID
		views[i].Usage.Instance.DaemonConnected = pid > 0 && connected[pid]
	}
}
