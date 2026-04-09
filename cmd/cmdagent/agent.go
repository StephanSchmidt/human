// Package cmdagent provides cobra commands for managing background Claude Code
// agents via tmux sessions.
package cmdagent

import (
	"fmt"
	"os/exec"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/cmd/cmdutil"
	"github.com/StephanSchmidt/human/errors"
	"github.com/StephanSchmidt/human/internal/agent"
	"github.com/StephanSchmidt/human/internal/claude"
	"github.com/StephanSchmidt/human/internal/config"
)

// BuildAgentCmd returns the parent "agent" command with start/stop/list/attach/resume subcommands.
func BuildAgentCmd(deps cmdutil.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage background Claude Code agents",
		Long: `Spawn, stop, list, attach, and resume background Claude Code agents.

Each agent runs in an isolated tmux session, optionally in its own git worktree,
so multiple agents can work on different tasks in parallel without interference.`,
	}

	mgr := &agent.Manager{
		Runner: claude.OSCommandRunner{},
	}

	cmd.AddCommand(buildStartCmd(mgr, deps))
	cmd.AddCommand(buildStopCmd(mgr))
	cmd.AddCommand(buildListCmd(mgr))
	cmd.AddCommand(buildAttachCmd(mgr))
	cmd.AddCommand(buildResumeCmd(mgr))
	return cmd
}

func buildStartCmd(mgr *agent.Manager, deps cmdutil.Deps) *cobra.Command {
	var prompt string
	var ticketKey string
	var model string
	var noWorktree bool
	var skipPerms bool

	cmd := &cobra.Command{
		Use:   "start NAME",
		Short: "Start a new background Claude Code agent",
		Long: `Create a tmux session with Claude Code running in interactive mode.

By default a git worktree is created in .worktrees/<name> for isolation.
Use --no-worktree to run in the current directory instead.

The agent runs with --permission-mode=auto by default. Use --skip-permissions
to run with --dangerously-skip-permissions instead.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			opts := agent.StartOpts{
				Name:       name,
				Prompt:     prompt,
				TicketKey:  ticketKey,
				Model:      model,
				NoWorktree: noWorktree,
				SkipPerms:  skipPerms,
			}

			// If ticket is provided but no prompt, fetch the ticket as the prompt.
			if ticketKey != "" && prompt == "" {
				instances, err := deps.LoadInstances(config.DirProject)
				if err != nil {
					return errors.WrapWithDetails(err, "loading tracker instances")
				}
				fetchedPrompt, err := agent.FetchTicketPrompt(cmd.Context(), ticketKey, instances)
				if err != nil {
					return err
				}
				opts.Prompt = fetchedPrompt
			}

			meta, err := mgr.Start(cmd.Context(), opts)
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Agent %q started (session: %s)\n", meta.Name, meta.SessionName)
			if meta.WorktreeDir != "" {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Worktree: %s\n", meta.WorktreeDir)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Attach:   human agent attach %s\n", meta.Name)
			return nil
		},
	}

	cmd.Flags().StringVar(&prompt, "prompt", "", "Initial prompt to send to Claude Code")
	cmd.Flags().StringVar(&ticketKey, "ticket", "", "Ticket key to fetch as the initial prompt")
	cmd.Flags().StringVar(&model, "model", "", "Claude model to use")
	cmd.Flags().BoolVar(&noWorktree, "no-worktree", false, "Run in the current directory instead of creating a worktree")
	cmd.Flags().BoolVar(&skipPerms, "skip-permissions", false, "Run with --dangerously-skip-permissions")
	return cmd
}

func buildStopCmd(mgr *agent.Manager) *cobra.Command {
	var clean bool

	cmd := &cobra.Command{
		Use:   "stop NAME",
		Short: "Stop a running agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := mgr.Stop(cmd.Context(), args[0], clean); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Agent %q stopped\n", args[0])
			return nil
		},
	}

	cmd.Flags().BoolVar(&clean, "clean", false, "Also remove the git worktree")
	return cmd
}

func buildListCmd(mgr *agent.Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all agents with their status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Refresh statuses before listing.
			_ = mgr.Refresh(cmd.Context())

			metas, err := agent.ListMetas()
			if err != nil {
				return err
			}
			if len(metas) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No agents found.")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "NAME\tSTATUS\tTICKET\tCWD\tAGE")
			for _, m := range metas {
				age := agent.FormatDuration(time.Since(m.CreatedAt))
				ticket := m.TicketKey
				if ticket == "" {
					ticket = "-"
				}
				cwd := m.Cwd
				if cwd == "" {
					cwd = "-"
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", m.Name, m.Status, ticket, cwd, age)
			}
			return w.Flush()
		},
	}
}

func buildAttachCmd(mgr *agent.Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "attach NAME",
		Short: "Attach to a running agent's tmux session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionName, err := mgr.Attach(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			tmuxPath, err := lookupTmux()
			if err != nil {
				return errors.WithDetails("tmux not found in PATH")
			}

			return execTmuxAttach(tmuxPath, sessionName)
		},
	}
}

func buildResumeCmd(mgr *agent.Manager) *cobra.Command {
	var prompt string

	cmd := &cobra.Command{
		Use:   "resume NAME",
		Short: "Resume a stopped agent or send a new prompt to a running one",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := mgr.Resume(cmd.Context(), args[0], prompt); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Agent %q resumed\n", args[0])
			return nil
		},
	}

	cmd.Flags().StringVar(&prompt, "prompt", "", "Prompt to send to the agent")
	return cmd
}

// lookupTmux finds the tmux binary in PATH.
func lookupTmux() (string, error) {
	return lookPath("tmux")
}

// lookPath is a variable for testability.
var lookPath = exec.LookPath
