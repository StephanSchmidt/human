package dispatch

import (
	"context"
	"fmt"

	"github.com/StephanSchmidt/human/internal/claude"
)

// TmuxAgentFinder discovers idle Claude tmux panes by combining instance
// discovery (for state) with tmux pane discovery (for coordinates).
type TmuxAgentFinder struct {
	InstanceFinder claude.InstanceFinder
	TmuxClient     claude.TmuxClient
	ProcessLister  claude.ProcessLister
}

// FindIdleAgents returns Claude tmux panes that are in StateReady.
func (f *TmuxAgentFinder) FindIdleAgents(ctx context.Context) ([]Agent, error) {
	// Get instances with state info.
	instances, err := f.InstanceFinder.FindInstances(ctx)
	if err != nil {
		return nil, err
	}

	// RC-12: Build a set of idle host PIDs using cached state when available
	// to avoid re-reading JSONL from disk.
	idlePIDs := make(map[int]bool)
	for _, inst := range instances {
		if inst.Source != "host" || inst.PID == 0 {
			continue
		}
		var state claude.InstanceState
		if inst.CachedState != nil {
			state = *inst.CachedState
		} else {
			s, sErr := inst.StateReader.ReadState(inst.Root)
			if sErr != nil {
				continue
			}
			state = s
		}
		if state == claude.StateReady {
			idlePIDs[inst.PID] = true
		}
	}

	if len(idlePIDs) == 0 {
		return nil, nil
	}

	// Get tmux panes with Claude processes.
	panes, err := claude.FindClaudePanes(ctx, f.TmuxClient, f.ProcessLister, nil)
	if err != nil {
		return nil, err
	}

	// Match idle PIDs to panes.
	var agents []Agent
	for _, pane := range panes {
		if pane.ClaudePID > 0 && idlePIDs[pane.ClaudePID] {
			agents = append(agents, Agent{
				SessionName: pane.SessionName,
				WindowIndex: pane.WindowIndex,
				PaneIndex:   pane.PaneIndex,
				Label:       fmt.Sprintf("%s:%d.%d", pane.SessionName, pane.WindowIndex, pane.PaneIndex),
			})
		}
	}
	return agents, nil
}
