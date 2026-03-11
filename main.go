package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/stephanschmidt/human/errors"
	"github.com/stephanschmidt/human/internal/azuredevops"
	"github.com/stephanschmidt/human/internal/claude"
	"github.com/stephanschmidt/human/internal/github"
	"github.com/stephanschmidt/human/internal/gitlab"
	"github.com/stephanschmidt/human/internal/jira"
	"github.com/stephanschmidt/human/internal/linear"
	"github.com/stephanschmidt/human/internal/shortcut"
	"github.com/stephanschmidt/human/internal/tracker"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// --- tracker list ---

// trackerEntry is the JSON output structure for a single tracker instance.
type trackerEntry struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	URL         string `json:"url"`
	User        string `json:"user"`
	Description string `json:"description"`
}

func runTrackerList(out io.Writer, dir string, table bool) error {
	if dir == "" {
		dir = "."
	}
	instances, err := loadAllInstances(dir)
	if err != nil {
		return err
	}

	entries := make([]trackerEntry, len(instances))
	for i, inst := range instances {
		entries[i] = trackerEntry{Name: inst.Name, Type: inst.Kind, URL: inst.URL, User: inst.User, Description: inst.Description}
	}

	if table {
		return printTrackerTable(out, entries)
	}
	return printTrackerJSON(out, entries)
}

func printTrackerJSON(w io.Writer, entries []trackerEntry) error {
	_, _ = fmt.Fprintln(w, "// Configured issue trackers. Use --tracker=<name> to select one.")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(entries)
}

func printTrackerTable(out io.Writer, entries []trackerEntry) error {
	if len(entries) == 0 {
		_, _ = fmt.Fprintln(out, "No trackers configured in .humanconfig")
		return nil
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tTYPE\tURL\tUSER\tDESCRIPTION")
	for _, e := range entries {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", e.Name, e.Type, e.URL, e.User, e.Description)
	}
	return w.Flush()
}

// --- tracker find ---

// findResultEntry is the JSON output structure for tracker find.
type findResultEntry struct {
	Provider string `json:"provider"`
	Project  string `json:"project"`
	Key      string `json:"key"`
}

func runTrackerFind(ctx context.Context, out io.Writer, dir, key string, table bool) error {
	if dir == "" {
		dir = "."
	}
	instances, err := loadAllInstances(dir)
	if err != nil {
		return err
	}
	return runTrackerFindWithInstances(ctx, out, key, instances, table)
}

func runTrackerFindWithInstances(ctx context.Context, out io.Writer, key string, instances []tracker.Instance, table bool) error {
	result, err := tracker.FindTracker(ctx, key, instances)
	if err != nil {
		return err
	}

	entry := findResultEntry{
		Provider: result.Provider,
		Project:  result.Project,
		Key:      result.Key,
	}

	if table {
		return printFindTable(out, entry)
	}
	return printFindJSON(out, entry)
}

func printFindJSON(w io.Writer, entry findResultEntry) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(entry)
}

func printFindTable(out io.Writer, entry findResultEntry) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "PROVIDER\tPROJECT\tKEY")
	_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", entry.Provider, entry.Project, entry.Key)
	return w.Flush()
}

// --- help ---

// helpInstanceLoader is the function used by the root help template to load
// tracker instances.  It defaults to loadAllInstances(".") and can be
// overridden in tests.
var helpInstanceLoader = func() ([]tracker.Instance, error) {
	return loadAllInstances(".")
}

// printConnectedTrackers appends a "Connected trackers:" section to the help
// output.  Errors are silently ignored so that help always works.
func printConnectedTrackers(w io.Writer) {
	instances, err := helpInstanceLoader()
	if err != nil {
		return
	}
	if len(instances) == 0 {
		_, _ = fmt.Fprintln(w, "Connected trackers: none")
		_, _ = fmt.Fprintln(w, "  Configure trackers in .humanconfig.yaml")
		return
	}
	_, _ = fmt.Fprintln(w, "Connected trackers:")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, inst := range instances {
		line := fmt.Sprintf("  %s\t%s\t%s", inst.Name, inst.Kind, inst.URL)
		if inst.User != "" {
			line += "\t" + inst.User
		}
		if inst.Description != "" {
			line += "\t" + inst.Description
		}
		_, _ = fmt.Fprintln(tw, line)
	}
	_ = tw.Flush()
}

func printExamples(w io.Writer) {
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Command pattern:")
	_, _ = fmt.Fprintln(w, "  human <tracker> issues list --project=<PROJECT>   List issues (JSON)")
	_, _ = fmt.Fprintln(w, "  human <tracker> issue  get <KEY>                  Get issue (markdown)")
	_, _ = fmt.Fprintln(w, `  human <tracker> issue  create --project=<P> "S"   Create issue`)
	_, _ = fmt.Fprintln(w, "  human <tracker> issue  delete <KEY>               Delete/close issue")
	_, _ = fmt.Fprintln(w, "  human <tracker> issue  comment add <KEY> <BODY>   Add comment")
	_, _ = fmt.Fprintln(w, "  human <tracker> issue  comment list <KEY>         List comments")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Project key and issue key formats by tracker:")
	_, _ = fmt.Fprintln(w, "  jira        --project=KAN                  issue key: KAN-1")
	_, _ = fmt.Fprintln(w, "  github      --project=octocat/hello-world  issue key: octocat/hello-world#42")
	_, _ = fmt.Fprintln(w, "  gitlab      --project=mygroup/myproject    issue key: mygroup/myproject#42")
	_, _ = fmt.Fprintln(w, "  linear      --project=ENG                  issue key: ENG-123")
	_, _ = fmt.Fprintln(w, "  azuredevops --project=MyProject             issue key: 42")
	_, _ = fmt.Fprintln(w, "  shortcut    --project=MyProject             issue key: 123")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Examples:")
	_, _ = fmt.Fprintln(w, "  human jira issues list --project=KAN")
	_, _ = fmt.Fprintln(w, "  human jira issue get KAN-1")
	_, _ = fmt.Fprintln(w, `  human jira issue create --project=KAN "Implement login page"`)
	_, _ = fmt.Fprintln(w, "  human github issues list --project=octocat/hello-world")
	_, _ = fmt.Fprintln(w, "  human github issue get octocat/hello-world#42")
	_, _ = fmt.Fprintln(w, "  human jira issue delete KAN-1")
	_, _ = fmt.Fprintln(w, "  human jira issue comment add KAN-1 'Looks good'")
	_, _ = fmt.Fprintln(w, "  human notion search \"quarterly report\"")
	_, _ = fmt.Fprintln(w, "  human notion page get <page-id>")
	_, _ = fmt.Fprintln(w, "  human tracker list")
	_, _ = fmt.Fprintln(w, "  human install --agent claude")
}

// --- loadAllInstances / instanceFromFlags ---

// loadAllInstances collects tracker instances from all provider configs.
func loadAllInstances(dir string) ([]tracker.Instance, error) {
	var all []tracker.Instance

	ji, err := jira.LoadInstances(dir)
	if err != nil {
		return nil, err
	}
	all = append(all, ji...)

	gi, err := github.LoadInstances(dir)
	if err != nil {
		return nil, err
	}
	all = append(all, gi...)

	gli, err := gitlab.LoadInstances(dir)
	if err != nil {
		return nil, err
	}
	all = append(all, gli...)

	li, err := linear.LoadInstances(dir)
	if err != nil {
		return nil, err
	}
	all = append(all, li...)

	adi, err := azuredevops.LoadInstances(dir)
	if err != nil {
		return nil, err
	}
	all = append(all, adi...)

	sci, err := shortcut.LoadInstances(dir)
	if err != nil {
		return nil, err
	}
	return append(all, sci...), nil
}

// instanceFromFlags builds a tracker instance from root persistent flags,
// returning nil when insufficient flags are provided.
func instanceFromFlags(cmd *cobra.Command) *tracker.Instance {
	getFlag := func(name string) string {
		v, _ := cmd.Root().PersistentFlags().GetString(name)
		return v
	}

	jiraURL := getFlag("jira-url")
	jiraUser := getFlag("jira-user")
	jiraKey := getFlag("jira-key")
	if jiraURL != "" && jiraUser != "" && jiraKey != "" {
		return &tracker.Instance{
			Kind:     "jira",
			URL:      jiraURL,
			User:     jiraUser,
			Provider: jira.New(jiraURL, jiraUser, jiraKey),
		}
	}

	githubToken := getFlag("github-token")
	if githubToken != "" {
		url := getFlag("github-url")
		if url == "" {
			url = "https://api.github.com"
		}
		return &tracker.Instance{
			Kind:     "github",
			URL:      url,
			Provider: github.New(url, githubToken),
		}
	}

	gitlabToken := getFlag("gitlab-token")
	if gitlabToken != "" {
		url := getFlag("gitlab-url")
		if url == "" {
			url = "https://gitlab.com"
		}
		return &tracker.Instance{
			Kind:     "gitlab",
			URL:      url,
			Provider: gitlab.New(url, gitlabToken),
		}
	}

	linearToken := getFlag("linear-token")
	if linearToken != "" {
		url := getFlag("linear-url")
		if url == "" {
			url = "https://api.linear.app"
		}
		return &tracker.Instance{
			Kind:     "linear",
			URL:      url,
			Provider: linear.New(url, linearToken),
		}
	}

	azureToken := getFlag("azure-token")
	azureOrg := getFlag("azure-org")
	if azureToken != "" && azureOrg != "" {
		url := getFlag("azure-url")
		if url == "" {
			url = "https://dev.azure.com"
		}
		return &tracker.Instance{
			Kind:     "azuredevops",
			URL:      url,
			Provider: azuredevops.New(url, azureOrg, azureToken),
		}
	}

	shortcutToken := getFlag("shortcut-token")
	if shortcutToken != "" {
		url := getFlag("shortcut-url")
		if url == "" {
			url = "https://api.app.shortcut.com"
		}
		return &tracker.Instance{
			Kind:     "shortcut",
			URL:      url,
			Provider: shortcut.New(url, shortcutToken),
		}
	}

	return nil
}

// auditLogPath returns the path to the audit log file (~/.human/audit.log),
// creating the directory if needed.
func auditLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".human", "audit.log")
	}
	dir := filepath.Join(home, ".human")
	_ = os.MkdirAll(dir, 0o750)
	return filepath.Join(dir, "audit.log")
}

// --- newRootCmd builds the Cobra command tree ---

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "human",
		Short: "Unified CLI for issue trackers and tools",
		Long: `Unified CLI to list, read, create, delete, and comment on issues
across Jira, GitHub, GitLab, Linear, Azure DevOps, and Shortcut.
Search and read content from Notion workspaces.

Use it to:
  - fetch a ticket before planning implementation
  - check what issues exist in a project
  - create tickets for bugs or features you discover
  - add comments with status updates or findings
  - look up ticket details (status, assignee, description)
  - search Notion for meeting notes, specs, and docs

All trackers share the same command structure:
  human <tracker> issues list   — JSON array of issues
  human <tracker> issue  get    — single issue as markdown
  human <tracker> issue  create — create and return key
  human <tracker> issue  delete — delete or close
  human <tracker> issue  comment add/list — manage comments

Tools:
  human notion search QUERY     — search Notion workspace
  human notion page get ID      — page content as markdown
  human notion database query ID — query database rows
  human notion databases list   — list shared databases

Configure trackers and tools in .humanconfig.yaml or pass credentials via flags/env vars.`,
		Version: version + " (" + commit + ") " + date,
		// When no subcommand is given, run "tracker list".
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTrackerList(cmd.OutOrStdout(), ".", false)
		},
		SilenceUsage: true,
	}

	// Override help to append examples and connected trackers.
	defaultHelp := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		defaultHelp(cmd, args)
		// Only append extras for root-level help.
		if cmd != rootCmd {
			return
		}
		w := cmd.OutOrStdout()
		printExamples(w)
		printConnectedTrackers(w)
	})

	// Global persistent flags.
	pf := rootCmd.PersistentFlags()
	pf.String("tracker", "", "Named tracker instance from .humanconfig")

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
		&cobra.Group{ID: "trackers", Title: "Issue Trackers:"},
		&cobra.Group{ID: "tools", Title: "Tools:"},
		&cobra.Group{ID: "utility", Title: "Utility:"},
	)

	// Hide the auto-generated completion command.
	rootCmd.CompletionOptions.HiddenDefaultCmd = true

	// --- Provider commands (dynamic registration) ---
	providers := []string{"jira", "github", "gitlab", "linear", "azuredevops", "shortcut"}
	for _, kind := range providers {
		providerCmd := &cobra.Command{
			Use:     kind,
			Short:   kind + " issue tracker",
			GroupID: "trackers",
		}
		for _, sub := range buildProviderCommands(kind) {
			providerCmd.AddCommand(sub)
		}
		rootCmd.AddCommand(providerCmd)
	}

	// --- Notion (tools) ---
	notionCmd := buildNotionCommands()
	notionCmd.GroupID = "tools"
	rootCmd.AddCommand(notionCmd)

	// --- Static commands ---
	trackerCmd := buildTrackerCmd()
	trackerCmd.GroupID = "utility"
	rootCmd.AddCommand(trackerCmd)

	installCmd := buildInstallCmd()
	installCmd.GroupID = "utility"
	rootCmd.AddCommand(installCmd)

	return rootCmd
}

func buildTrackerCmd() *cobra.Command {
	trackerCmd := &cobra.Command{
		Use:   "tracker",
		Short: "Manage tracker connections",
	}

	var table bool
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List configured tracker instances (JSON)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTrackerList(cmd.OutOrStdout(), ".", table)
		},
	}
	listCmd.Flags().BoolVar(&table, "table", false, "Output as human-readable table instead of JSON")

	var findTable bool
	findCmd := &cobra.Command{
		Use:   "find KEY",
		Short: "Find which tracker owns a key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTrackerFind(cmd.Context(), cmd.OutOrStdout(), ".", args[0], findTable)
		},
	}
	findCmd.Flags().BoolVar(&findTable, "table", false, "Output as human-readable table instead of JSON")

	trackerCmd.AddCommand(listCmd, findCmd)
	return trackerCmd
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

// --- main ---

func main() {
	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

	rootCmd := newRootCmd()
	if err := rootCmd.Execute(); err != nil {
		errors.LogError(err).Msg("command failed")
		os.Exit(1)
	}
}
