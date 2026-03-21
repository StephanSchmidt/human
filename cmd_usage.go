package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/internal/claude"
)

func buildUsageCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "usage",
		Short: "Show Claude Code token usage for the current 5-hour window",
		RunE: func(cmd *cobra.Command, _ []string) error {
			finder := buildFinder()
			return runUsage(cmd, finder, time.Now())
		},
	}
}

func runUsage(cmd *cobra.Command, finder claude.InstanceFinder, now time.Time) error {
	instances, err := finder.FindInstances(cmd.Context())
	if err != nil || len(instances) == 0 {
		// Fallback: local host only.
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return homeErr
		}
		root := filepath.Join(home, ".claude", "projects")
		summary, calcErr := claude.CalculateUsage(claude.OSDirWalker{}, root, now)
		if calcErr != nil {
			return calcErr
		}
		return claude.FormatUsage(cmd.OutOrStdout(), summary, now)
	}

	var results []claude.InstanceUsage
	for _, inst := range instances {
		summary, calcErr := claude.CalculateUsage(inst.Walker, inst.Root, now)
		if calcErr != nil {
			continue
		}
		state := claude.StateUnknown
		if inst.StateReader != nil {
			if s, sErr := inst.StateReader.ReadState(inst.Root); sErr == nil {
				state = s
			}
		}
		results = append(results, claude.InstanceUsage{Instance: inst, Summary: summary, State: state})
	}

	if len(results) <= 1 {
		// Backward-compatible single-instance format.
		if len(results) == 1 {
			return claude.FormatUsage(cmd.OutOrStdout(), results[0].Summary, now)
		}
		return claude.FormatUsage(cmd.OutOrStdout(), &claude.UsageSummary{Models: map[string]*claude.ModelUsage{}}, now)
	}
	return claude.FormatMultiUsage(cmd.OutOrStdout(), results, now)
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
