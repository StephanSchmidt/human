package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/cmd/cmdamplitude"
	"github.com/StephanSchmidt/human/cmd/cmdauto"
	"github.com/StephanSchmidt/human/cmd/cmdbrowser"
	"github.com/StephanSchmidt/human/cmd/cmddaemon"
	"github.com/StephanSchmidt/human/cmd/cmdfigma"
	"github.com/StephanSchmidt/human/cmd/cmdindex"
	"github.com/StephanSchmidt/human/cmd/cmdinit"
	"github.com/StephanSchmidt/human/cmd/cmdtui"
	"github.com/StephanSchmidt/human/cmd/cmdnotion"
	"github.com/StephanSchmidt/human/cmd/cmdprovider"
	"github.com/StephanSchmidt/human/cmd/cmdslack"
	"github.com/StephanSchmidt/human/cmd/cmdtelegram"
	"github.com/StephanSchmidt/human/cmd/cmdtracker"
	"github.com/StephanSchmidt/human/cmd/cmdusage"
	"github.com/StephanSchmidt/human/cmd/cmdutil"
	"github.com/StephanSchmidt/human/errors"
	"github.com/StephanSchmidt/human/internal/claude"
	"github.com/StephanSchmidt/human/internal/daemon"
	"github.com/StephanSchmidt/human/internal/tracker"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// helpInstanceLoader is the function used by the root help template to load
// tracker instances.  It defaults to LoadAllInstances(".") and can be
// overridden in tests.
var helpInstanceLoader = func() ([]tracker.Instance, error) {
	return cmdutil.LoadAllInstances(".")
}

// autoInstanceLoader is used by auto-detect commands to load tracker instances.
// It defaults to LoadAllInstances(".") and can be overridden in tests.
var autoInstanceLoader = func() ([]tracker.Instance, error) {
	return cmdutil.LoadAllInstances(".")
}

// --- newRootCmd builds the Cobra command tree ---

func newRootCmd() *cobra.Command {
	deps := cmdutil.DefaultDeps()

	// autoDeps uses the package-level autoInstanceLoader so tests can
	// inject mock instances without touching the real config path.
	autoDeps := deps
	autoDeps.LoadInstances = func(_ string) ([]tracker.Instance, error) {
		return autoInstanceLoader()
	}

	rootCmd := &cobra.Command{
		Use:   "human",
		Short: "Unified CLI for issue trackers and tools",
		Long: `Unified CLI to list, read, create, delete, and comment on issues
across Jira, GitHub, GitLab, Linear, Azure DevOps, and Shortcut.
Search and read content from Notion workspaces. Browse Figma designs.
Queries Amplitude product analytics. Reads Telegram bot messages.

Use it to:
  - fetch a ticket before planning implementation
  - check what issues exist in a project
  - search across all trackers with a local index
  - create tickets for bugs or features you discover
  - add comments with status updates or findings
  - look up ticket details (status, assignee, description)
  - search Notion for meeting notes, specs, and docs
  - browse Figma files, components, and comments
  - query Amplitude events, funnels, retention, and cohorts
  - read pending Telegram bot messages

All trackers share the same command structure:
  human <tracker> issues list   — JSON array of issues
  human <tracker> issue  get    — single issue as markdown
  human <tracker> issue  create — create and return key
  human <tracker> issue  edit   — update title and/or description
  human <tracker> issue  start  — transition + assign to yourself
  human <tracker> issue  delete — delete or close
  human <tracker> issue  statuses — list available statuses
  human <tracker> issue  status   — set issue status
  human <tracker> issue  comment add/list — manage comments

Tools:
  human notion search QUERY     — search Notion workspace
  human notion page get ID      — page content as markdown
  human notion database query ID — query database rows
  human notion databases list   — list shared databases
  human figma file get KEY      — file metadata and pages
  human figma file comments KEY — design feedback
  human figma file components KEY — published components
  human amplitude events list   — event types with active users
  human amplitude cohorts list  — behavioral cohorts
  human telegram list            — pending bot messages
  human telegram get UPDATE_ID   — specific message details

Configure trackers and tools in .humanconfig.yaml or pass credentials via flags/env vars.`,
		Version: version + " (" + commit + ") " + date,
		// When no subcommand is given, show help.
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		SilenceUsage: true,
	}

	// Override help to append examples and connected trackers.
	// Wrap helpInstanceLoader in a closure so tests can override it after
	// newRootCmd() returns.
	cmdutil.SetupHelp(rootCmd, func() ([]tracker.Instance, error) {
		return helpInstanceLoader()
	})

	// Global persistent flags.
	pf := rootCmd.PersistentFlags()
	pf.String("tracker", "", "Named tracker instance from .humanconfig")
	pf.Bool("safe", os.Getenv("HUMAN_SAFE") == "1", "Block destructive operations (deletes)")

	// Credential flags — functional but hidden from help (use env vars or .humanconfig).
	credFlags := []struct{ name, env, help string }{
		{"jira-key", "JIRA_KEY", "Jira API token"},
		{"jira-url", "JIRA_URL", "Jira base URL"},
		{"jira-user", "JIRA_USER", "Jira user email"},
		{"github-token", "GITHUB_TOKEN", "GitHub personal access token"},
		{"github-url", "GITHUB_URL", "GitHub API base URL"},
		{"gitlab-token", "GITLAB_TOKEN", "GitLab private token"},
		{"gitlab-url", "GITLAB_URL", "GitLab base URL"},
		{"linear-token", "LINEAR_TOKEN", "Linear API key"},
		{"linear-url", "LINEAR_URL", "Linear API base URL"},
		{"azure-token", "AZURE_TOKEN", "Azure DevOps PAT token"},
		{"azure-url", "AZURE_URL", "Azure DevOps base URL"},
		{"azure-org", "AZURE_ORG", "Azure DevOps organization"},
		{"shortcut-token", "SHORTCUT_TOKEN", "Shortcut API token"},
		{"shortcut-url", "SHORTCUT_URL", "Shortcut API base URL"},
	}
	for _, f := range credFlags {
		pf.String(f.name, os.Getenv(f.env), f.help)
		_ = pf.MarkHidden(f.name)
	}

	// --- Command groups ---
	rootCmd.AddGroup(
		&cobra.Group{ID: "shortcuts", Title: "Quick Commands:"},
		&cobra.Group{ID: "trackers", Title: "Issue Trackers:"},
		&cobra.Group{ID: "tools", Title: "Tools:"},
		&cobra.Group{ID: "utility", Title: "Utility:"},
	)

	// Hide the auto-generated completion command.
	rootCmd.CompletionOptions.HiddenDefaultCmd = true

	// --- Quick commands (auto-detect tracker) ---
	autoGetCmd := cmdauto.BuildAutoGetCmd(autoDeps)
	autoGetCmd.GroupID = "shortcuts"
	rootCmd.AddCommand(autoGetCmd)

	autoListCmd := cmdauto.BuildAutoListCmd(autoDeps)
	autoListCmd.GroupID = "shortcuts"
	rootCmd.AddCommand(autoListCmd)

	autoStatusesCmd := cmdauto.BuildAutoStatusesCmd(autoDeps)
	autoStatusesCmd.GroupID = "shortcuts"
	rootCmd.AddCommand(autoStatusesCmd)

	autoStatusCmd := cmdauto.BuildAutoStatusCmd(autoDeps)
	autoStatusCmd.GroupID = "shortcuts"
	rootCmd.AddCommand(autoStatusCmd)

	// --- Provider commands (dynamic registration) ---
	providers := []string{"jira", "github", "gitlab", "linear", "azuredevops", "shortcut"}
	for _, kind := range providers {
		providerCmd := &cobra.Command{
			Use:     kind,
			Short:   kind + " issue tracker",
			GroupID: "trackers",
		}
		for _, sub := range cmdprovider.BuildProviderCommands(kind, deps) {
			providerCmd.AddCommand(sub)
		}
		rootCmd.AddCommand(providerCmd)
	}

	// --- Notion (tools) ---
	notionCmd := cmdnotion.BuildNotionCommands()
	notionCmd.GroupID = "tools"
	rootCmd.AddCommand(notionCmd)

	// --- Figma (tools) ---
	figmaCmd := cmdfigma.BuildFigmaCommands()
	figmaCmd.GroupID = "tools"
	rootCmd.AddCommand(figmaCmd)

	// --- Amplitude (tools) ---
	amplitudeCmd := cmdamplitude.BuildAmplitudeCommands()
	amplitudeCmd.GroupID = "tools"
	rootCmd.AddCommand(amplitudeCmd)

	// --- Telegram (tools) ---
	telegramCmd := cmdtelegram.BuildTelegramCommands()
	telegramCmd.GroupID = "tools"
	rootCmd.AddCommand(telegramCmd)

	slackCmd := cmdslack.BuildSlackCommands()
	slackCmd.GroupID = "tools"
	rootCmd.AddCommand(slackCmd)

	// --- Static commands ---
	trackerCmd := cmdtracker.BuildTrackerCmd(cmdutil.LoadAllInstances)
	trackerCmd.GroupID = "utility"
	rootCmd.AddCommand(trackerCmd)

	installCmd := buildInstallCmd()
	installCmd.GroupID = "utility"
	rootCmd.AddCommand(installCmd)

	daemonCmd := cmddaemon.BuildDaemonCmd(newRootCmd, version)
	daemonCmd.GroupID = "utility"
	rootCmd.AddCommand(daemonCmd)

	browserCmd := cmdbrowser.BuildBrowserCmd()
	browserCmd.GroupID = "utility"
	rootCmd.AddCommand(browserCmd)

	initCmd := cmdinit.BuildInitCmd()
	initCmd.GroupID = "utility"
	rootCmd.AddCommand(initCmd)

	chromeBridgeCmd := cmddaemon.BuildChromeBridgeCmd(version)
	chromeBridgeCmd.GroupID = "utility"
	rootCmd.AddCommand(chromeBridgeCmd)

	usageCmd := cmdusage.BuildUsageCmd()
	usageCmd.GroupID = "utility"
	rootCmd.AddCommand(usageCmd)

	indexDeps := cmdindex.DefaultIndexDeps()
	indexCmd := cmdindex.BuildIndexCmd(indexDeps)
	indexCmd.GroupID = "utility"
	rootCmd.AddCommand(indexCmd)

	searchCmd := cmdindex.BuildSearchCmd(indexDeps)
	searchCmd.GroupID = "shortcuts"
	rootCmd.AddCommand(searchCmd)

	tuiCmd := cmdtui.BuildTuiCmd()
	tuiCmd.GroupID = "utility"
	rootCmd.AddCommand(tuiCmd)

	return rootCmd
}

func buildInstallCmd() *cobra.Command {
	var agent string
	var personal bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install agent integrations",
		RunE: func(_ *cobra.Command, _ []string) error {
			switch agent {
			case "claude":
				fmt.Println("Installing Claude Code files...")
				if err := claude.Install(os.Stdout, claude.OSFileWriter{}, personal); err != nil {
					return err
				}
				fmt.Println("Done. Skill: /human-plan <ticket-key>")
			default:
				return fmt.Errorf("unsupported agent: %s", agent)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "Agent to install (claude)")
	_ = cmd.MarkFlagRequired("agent")
	cmd.Flags().BoolVar(&personal, "personal", false, "Install to ~/.claude/ (personal) instead of .claude/ (project)")
	return cmd
}

// isLocalSubcommand returns true if args represent a command that must
// execute locally rather than being forwarded to the daemon.
func isLocalSubcommand(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		// --version should always run locally to show the client's version.
		if a == "--version" || a == "-v" {
			return true
		}
		if len(a) > 0 && a[0] == '-' {
			continue // skip other flags
		}
		return a == "daemon" || a == "chrome-bridge" || a == "install" || a == "init" || a == "usage" || a == "index" || a == "tui"
	}
	return false
}

// --- main ---

// subcmdFromBinary checks whether the binary was invoked via a symlink
// like "human-browser" and returns the implied subcommand (e.g. "browser").
// Returns "" when os.Args[0] is just "human" or unrecognised.
func subcmdFromBinary() string {
	base := filepath.Base(os.Args[0]) //nolint:nilaway // os.Args is always set in main
	// Strip common extensions (.exe on Windows).
	base = strings.TrimSuffix(base, ".exe")
	if strings.HasPrefix(base, "human-") {
		return base[len("human-"):]
	}
	return ""
}

func main() {
	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

	// Busybox-style dispatch: "human-browser URL" → "human browser URL".
	args := os.Args[1:] //nolint:nilaway // os.Args is always set in main
	if sub := subcmdFromBinary(); sub != "" {
		args = append([]string{sub}, args...)
	}

	// Client mode: forward to daemon if configured.
	// Skip forwarding for "daemon" subcommands — they must run locally.
	addr := os.Getenv("HUMAN_DAEMON_ADDR")
	token := os.Getenv("HUMAN_DAEMON_TOKEN")

	// Auto-discover from daemon info file when env vars are not set.
	if addr == "" {
		if info, err := daemon.ReadInfo(); err == nil && info.IsAlive() {
			addr = info.Addr
			if token == "" {
				token = info.Token
			}
			if os.Getenv("HUMAN_CHROME_ADDR") == "" && info.ChromeAddr != "" {
				_ = os.Setenv("HUMAN_CHROME_ADDR", info.ChromeAddr)
			}
			if os.Getenv("HUMAN_PROXY_ADDR") == "" && info.ProxyAddr != "" {
				_ = os.Setenv("HUMAN_PROXY_ADDR", info.ProxyAddr)
			}
		}
	}

	if addr != "" && !isLocalSubcommand(args) {
		exitCode, err := daemon.RunRemote(addr, token, args, version)
		if err != nil {
			errors.LogError(err).Msg("remote execution failed")
			os.Exit(1)
		}
		os.Exit(exitCode)
	}

	rootCmd := newRootCmd()
	rootCmd.SetArgs(args)
	if err := rootCmd.Execute(); err != nil {
		errors.LogError(err).Msg("command failed")
		os.Exit(1)
	}
}
