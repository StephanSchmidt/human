package monitor

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/StephanSchmidt/human/cmd/cmddaemon"
	"github.com/StephanSchmidt/human/internal/claude"
	"github.com/StephanSchmidt/human/internal/claude/hookevents"
	"github.com/StephanSchmidt/human/internal/claude/logparser"
	"github.com/StephanSchmidt/human/internal/config"
	"github.com/StephanSchmidt/human/internal/daemon"
	"github.com/StephanSchmidt/human/internal/proxy"
	"github.com/StephanSchmidt/human/internal/slack"
	"github.com/StephanSchmidt/human/internal/telegram"
	"github.com/StephanSchmidt/human/internal/tracker"
)

// Monitor owns the data-fetching and state-reconciliation cycle for the TUI.
type Monitor struct {
	finder       claude.InstanceFinder
	dockerClient claude.DockerClient
	parsers      map[string]*logparser.FileParser
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

	pid, alive := cmddaemon.ReadAlivePid()
	snap.Daemon = DaemonState{PID: pid, Alive: alive}
	if info, err := daemon.ReadInfo(); err == nil {
		snap.Daemon.ProxyAddr = info.ProxyAddr
	}
	snap.Daemon.ProxyActiveConns = proxy.ReadStats(proxy.StatsPath()).ActiveConns
	snap.Telegram = telegramStatus()
	snap.Slack = slackStatus()
	snap.Trackers = tracker.DiagnoseTrackers(".", config.UnmarshalSection, os.Getenv)

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

	// Hook events.
	snap.hookReaders = buildHookReaders(instances, m.dockerClient)
	hookSnaps := readHookSnapshots(ctx, snap.hookReaders)
	overlayHookState(sessionByPath, hookSnaps)

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
func (m *Monitor) FetchQuick(ctx context.Context, prev *Snapshot) *Snapshot {
	if prev == nil {
		return nil
	}

	// Shallow copy carried-forward data.
	snap := *prev
	snap.FetchedAt = time.Now()

	pid, alive := cmddaemon.ReadAlivePid()
	snap.Daemon = DaemonState{PID: pid, Alive: alive}
	if info, err := daemon.ReadInfo(); err == nil {
		snap.Daemon.ProxyAddr = info.ProxyAddr
	}
	snap.Daemon.ProxyActiveConns = proxy.ReadStats(proxy.StatsPath()).ActiveConns
	snap.connectedPIDs = readConnectedPIDs()

	// Re-read hook events for sub-second state transitions.
	if len(snap.hookReaders) > 0 {
		quickCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		hookSnaps := readHookSnapshots(quickCtx, snap.hookReaders)
		cancel()

		// Deep-copy sessionByPath before mutating.
		byPath := make(map[string]logparser.SessionState, len(snap.sessionByPath))
		for k, v := range snap.sessionByPath {
			byPath[k] = v
		}
		overlayHookState(byPath, hookSnaps)
		snap.sessionByPath = byPath

		// Rebuild instance views with updated sessions.
		snap.Instances = matchInstances(extractUsages(prev.Instances), byPath)
		applyDaemonConnectedViews(snap.Instances, snap.connectedPIDs)
	}

	// Update daemon-connected status even when instances aren't rebuilt.
	if len(snap.hookReaders) == 0 {
		applyDaemonConnectedViews(snap.Instances, snap.connectedPIDs)
	}

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
	for _, inst := range instances {
		if inst.FilePath == "" {
			continue
		}
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

func buildHookReaders(instances []claude.Instance, dc claude.DockerClient) []hookevents.EventReader {
	var readers []hookevents.EventReader

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		readers = append(readers, &hookevents.FileEventReader{
			Path: filepath.Join(home, ".claude", "human-events", "events.jsonl"),
		})
	}

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

// overlayHookState updates sessions in byPath from hook snapshots when the
// hook event is more recent than the JSONL-parsed LastActivity.
func overlayHookState(byPath map[string]logparser.SessionState, hooks map[string]hookevents.SessionSnapshot) {
	if len(hooks) == 0 {
		return
	}
	for path, sess := range byPath {
		snap, ok := hooks[sess.SessionID]
		if !ok {
			continue
		}
		if snap.LastEventAt.After(sess.LastActivity) {
			sess.IsWorking = snap.IsWorking
			sess.LastActivity = snap.LastEventAt
			byPath[path] = sess
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
	if s.IsWorking {
		return claude.StateBusy
	}
	return claude.StateReady
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
