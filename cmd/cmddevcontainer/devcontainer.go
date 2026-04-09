// Package cmddevcontainer provides cobra commands for managing devcontainer
// lifecycle: building, starting, stopping, and executing commands.
package cmddevcontainer

import (
	"github.com/spf13/cobra"
)

// BuildDevcontainerCmd returns the parent "devcontainer" command with all subcommands.
func BuildDevcontainerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "devcontainer",
		Short: "Manage devcontainers",
		Long: `Build, start, stop, and manage devcontainers with integrated daemon support.

A single "human devcontainer up" replaces the manual process of starting the
daemon, running devcontainer CLI, and configuring environment variables.`,
	}

	cmd.AddCommand(buildUpCmd())
	cmd.AddCommand(buildAgentCmd())
	cmd.AddCommand(buildExecCmd())
	cmd.AddCommand(buildStopCmd())
	cmd.AddCommand(buildDownCmd())
	cmd.AddCommand(buildListCmd())
	cmd.AddCommand(buildStatusCmd())
	cmd.AddCommand(buildLogsCmd())
	return cmd
}
