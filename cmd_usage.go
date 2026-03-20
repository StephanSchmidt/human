package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/errors"
	"github.com/StephanSchmidt/human/internal/claude"
)

func buildUsageCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "usage",
		Short: "Show Claude Code token usage for the current 5-hour window",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUsage(cmd, claude.OSDirWalker{}, time.Now())
		},
	}
}

func runUsage(cmd *cobra.Command, walker claude.DirWalker, now time.Time) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return errors.WrapWithDetails(err, "resolving home directory")
	}
	root := filepath.Join(home, ".claude", "projects")

	summary, err := claude.CalculateUsage(walker, root, now)
	if err != nil {
		return err
	}
	return claude.FormatUsage(cmd.OutOrStdout(), summary, now)
}
