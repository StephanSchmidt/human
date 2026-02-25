package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/alecthomas/kong"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"human/errors"
	"human/internal/claude"
	"human/internal/config"
	"human/internal/jira"
	"human/internal/tracker"
)

// CLI is the top-level Kong struct with global flags.
type CLI struct {
	Jira     string     `kong:"help='Named Jira config from .humanconfig (uses first entry when omitted)'"`
	JiraKey  string     `kong:"env='JIRA_KEY',help='Jira API token'"`
	JiraURL  string     `kong:"env='JIRA_URL',help='Jira base URL'"`
	JiraUser string     `kong:"env='JIRA_USER',help='Jira user email'"`
	Issues   IssuesCmd  `kong:"cmd,help='Bulk issue operations'"`
	Issue    IssueCmd   `kong:"cmd,help='Single issue operations'"`
	Install  InstallCmd  `kong:"cmd,help='Install agent integrations'"`
	Tracker  TrackerCmd  `kong:"cmd,help='Manage tracker connections'"`
}

// --- tracker list ---

// TrackerCmd groups tracker subcommands.
type TrackerCmd struct {
	List TrackerListCmd `kong:"cmd,help='List configured tracker instances (JSON)'"`
}

// trackerEntry is the JSON output structure for a single tracker instance.
type trackerEntry struct {
	Name string `json:"name"`
	Type string `json:"type"`
	URL  string `json:"url"`
	User string `json:"user"`
}

// TrackerListCmd prints all configured tracker instances.
type TrackerListCmd struct {
	Table bool `kong:"help='Output as human-readable table instead of JSON'"`
}

// Run lists configured tracker instances.
func (cmd *TrackerListCmd) Run() error {
	configs, err := config.LoadJiraConfigs(".")
	if err != nil {
		return err
	}

	entries := make([]trackerEntry, len(configs))
	for i, c := range configs {
		entries[i] = trackerEntry{
			Name: c.Name,
			Type: "jira",
			URL:  c.URL,
			User: c.User,
		}
	}

	if cmd.Table {
		return printTrackerTable(entries)
	}
	return printTrackerJSON(entries)
}

func printTrackerJSON(entries []trackerEntry) error {
	fmt.Println("// Configured issue trackers. Use --jira=<name> to select one.")
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(entries)
}

func printTrackerTable(entries []trackerEntry) error {
	if len(entries) == 0 {
		fmt.Println("No trackers configured in .humanconfig")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tTYPE\tURL\tUSER")
	for _, e := range entries {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Name, e.Type, e.URL, e.User)
	}
	return w.Flush()
}

// --- install ---

// InstallCmd installs agent integrations.
type InstallCmd struct {
	Agent    string `kong:"required,enum='claude',help='Agent to install (claude)'"`
	Personal bool   `kong:"help='Install to ~/.claude/ (personal) instead of .claude/ (project)'"`
}

// Run executes the install command.
func (cmd *InstallCmd) Run() error {
	switch cmd.Agent {
	case "claude":
		fmt.Println("Installing Claude Code files...")
		if err := claude.Install(os.Stdout, claude.OSFileWriter{}, cmd.Personal); err != nil {
			return err
		}
		fmt.Println("Done. Skill: /human-plan <ticket-key>")
	}
	return nil
}

// --- issues list ---

type IssuesCmd struct {
	List ListCmd `kong:"cmd,help='List project issues (JSON)'"`
}

type ListCmd struct {
	Project string `kong:"required,help='Jira project key (e.g. KAN)'"`
	Table   bool   `kong:"help='Output as human-readable table instead of JSON'"`
}

func (cmd *ListCmd) Run(l tracker.Lister) error {
	issues, err := l.ListIssues(context.TODO(), tracker.ListOptions{
		Project:    cmd.Project,
		MaxResults: 50,
	})
	if err != nil {
		return err
	}

	if cmd.Table {
		return printIssuesTable(issues)
	}
	return printIssuesJSON(issues)
}

func printIssuesJSON(issues []tracker.Issue) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(issues)
}

func printIssuesTable(issues []tracker.Issue) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "KEY\tSTATUS\tSUMMARY")
	for _, issue := range issues {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", issue.Key, issue.Status, issue.Summary)
	}
	return w.Flush()
}

// --- issue get ---

type IssueCmd struct {
	Get    GetCmd    `kong:"cmd,help='Get a single issue with metadata and description as markdown'"`
	Create CreateCmd `kong:"cmd,help='Create a new issue in a project'"`
}

type GetCmd struct {
	Key string `kong:"arg,required,help='Issue key (e.g. KAN-1)'"`
}

func (cmd *GetCmd) Run(g tracker.Getter) error {
	issue, err := g.GetIssue(context.TODO(), cmd.Key)
	if err != nil {
		return err
	}

	displayOrNone := func(s string) string {
		if s == "" {
			return "None"
		}
		return s
	}

	fmt.Printf("# %s: %s\n\n", issue.Key, issue.Summary)
	fmt.Println("| Field    | Value       |")
	fmt.Println("|----------|-------------|")
	fmt.Printf("| Status   | %s |\n", issue.Status)
	fmt.Printf("| Priority | %s |\n", displayOrNone(issue.Priority))
	fmt.Printf("| Assignee | %s |\n", displayOrNone(issue.Assignee))
	fmt.Printf("| Reporter | %s |\n", displayOrNone(issue.Reporter))

	if issue.Description != "" {
		fmt.Printf("\n## Description\n\n%s", issue.Description)
	}

	return nil
}

// --- issue create ---

type CreateCmd struct {
	Project     string `kong:"required,help='Project key (e.g. KAN)'"`
	Type        string `kong:"default='Task',help='Issue type (e.g. Task, Bug, Story)'"`
	Summary     string `kong:"arg,required,help='Issue summary'"`
	Description string `kong:"help='Issue description (plain text)'"`
}

func (cmd *CreateCmd) Run(c tracker.Creator) error {
	issue, err := c.CreateIssue(context.TODO(), &tracker.Issue{
		Project:     cmd.Project,
		Type:        cmd.Type,
		Summary:     cmd.Summary,
		Description: cmd.Description,
	})
	if err != nil {
		return err
	}
	fmt.Printf("%s\t%s\n", issue.Key, issue.Summary)
	return nil
}

// --- help ---

func helpPrinter(options kong.HelpOptions, ctx *kong.Context) error {
	if err := kong.DefaultHelpPrinter(options, ctx); err != nil {
		return err
	}

	// Append examples only for root-level help.
	if ctx.Command() != "" {
		return nil
	}

	w := ctx.Stdout
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Examples:")
	_, _ = fmt.Fprintln(w, "  # List all issues in a project (JSON)")
	_, _ = fmt.Fprintln(w, "  human issues list --project=KAN")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "  # Get a single issue as markdown")
	_, _ = fmt.Fprintln(w, "  human issue get KAN-1")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "  # Pipe issue details to another tool")
	_, _ = fmt.Fprintln(w, "  human issue get KAN-1 | llm 'summarize this'")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "  # Create a new issue in a project")
	_, _ = fmt.Fprintln(w, `  human issue create --project=KAN "Implement login page"`)
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "  # List configured trackers (JSON)")
	_, _ = fmt.Fprintln(w, "  human tracker list")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "  # Use a specific Jira config by name")
	_, _ = fmt.Fprintln(w, "  human --jira=work issues list --project=KAN")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "  # Install Claude Code skill and agent (no Jira credentials needed)")
	_, _ = fmt.Fprintln(w, "  human install --agent claude")

	return nil
}

// needsJiraClient returns true for commands that require Jira credentials.
func needsJiraClient(command string) bool {
	switch {
	case strings.HasPrefix(command, "install"):
		return false
	case strings.HasPrefix(command, "tracker"):
		return false
	default:
		return true
	}
}

// --- main ---

func main() {
	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

	// Show tracker list when invoked without arguments.
	if len(os.Args) < 2 {
		os.Args = append(os.Args, "tracker", "list")
	}

	var cli CLI
	ctx := kong.Parse(&cli,
		kong.Name("human"),
		kong.Description("AI-powered issue tracker CLI.\nReads and manages Jira issues. Output is plain text tables and markdown."),
		kong.Help(helpPrinter),
		kong.UsageOnError(),
	)

	// Load .humanconfig after parsing so --jira flag is available.
	// Config values fill env gaps not covered by flags or shell env.
	if err := config.LoadConfig(".", cli.Jira); err != nil {
		log.Warn().Err(err).Msg("failed to parse .humanconfig")
	}

	// Backfill CLI fields from env vars that LoadConfig may have set.
	if cli.JiraURL == "" {
		cli.JiraURL = os.Getenv("JIRA_URL")
	}
	if cli.JiraUser == "" {
		cli.JiraUser = os.Getenv("JIRA_USER")
	}
	if cli.JiraKey == "" {
		cli.JiraKey = os.Getenv("JIRA_KEY")
	}

	if needsJiraClient(ctx.Command()) {
		if cli.JiraURL == "" || cli.JiraUser == "" || cli.JiraKey == "" {
			fmt.Fprintln(os.Stderr, "error: missing required Jira config (--jira-url, --jira-user, --jira-key or env vars)")
			os.Exit(1)
		}
		client := jira.New(cli.JiraURL, cli.JiraUser, cli.JiraKey)
		ctx.BindTo(client, (*tracker.Lister)(nil))
		ctx.BindTo(client, (*tracker.Getter)(nil))
		ctx.BindTo(client, (*tracker.Creator)(nil))
	}

	if err := ctx.Run(); err != nil {
		errors.LogError(err).Msg("command failed")
		os.Exit(1)
	}
}
