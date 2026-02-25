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
	"human/internal/github"
	"human/internal/jira"
	"human/internal/tracker"
)

// CLI is the top-level Kong struct with global flags.
type CLI struct {
	TrackerName string     `kong:"name='tracker',help='Named tracker from .humanconfig (resolves type automatically)'"`
	JiraKey     string     `kong:"env='JIRA_KEY',help='Jira API token'"`
	JiraURL     string     `kong:"env='JIRA_URL',help='Jira base URL'"`
	JiraUser    string     `kong:"env='JIRA_USER',help='Jira user email'"`
	GitHubToken string     `kong:"env='GITHUB_TOKEN',help='GitHub personal access token'"`
	GitHubURL   string     `kong:"env='GITHUB_URL',help='GitHub API base URL'"`
	Issues      IssuesCmd  `kong:"cmd,help='Bulk issue operations'"`
	Issue       IssueCmd   `kong:"cmd,help='Single issue operations'"`
	Install     InstallCmd `kong:"cmd,help='Install agent integrations'"`
	Tracker     TrackerCmd `kong:"cmd,help='Manage tracker connections'"`
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
	var entries []trackerEntry

	jiraConfigs, err := config.LoadJiraConfigs(".")
	if err != nil {
		return err
	}
	for _, c := range jiraConfigs {
		entries = append(entries, trackerEntry{
			Name: c.Name,
			Type: "jira",
			URL:  c.URL,
			User: c.User,
		})
	}

	ghConfigs, err := config.LoadGitHubConfigs(".")
	if err != nil {
		return err
	}
	for _, c := range ghConfigs {
		entries = append(entries, trackerEntry{
			Name: c.Name,
			Type: "github",
			URL:  c.URL,
		})
	}

	if cmd.Table {
		return printTrackerTable(entries)
	}
	return printTrackerJSON(entries)
}

func printTrackerJSON(entries []trackerEntry) error {
	fmt.Println("// Configured issue trackers. Use --tracker=<name> to select one.")
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
	Project string `kong:"required,help='Project key (Jira: KAN, GitHub: owner/repo)'"`
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
	Key string `kong:"arg,required,help='Issue key (Jira: KAN-1, GitHub: owner/repo#123)'"`
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
	Project     string `kong:"required,help='Project key (Jira: KAN, GitHub: owner/repo)'"`
	Type        string `kong:"default='Task',help='Issue type (Jira only, e.g. Task, Bug, Story)'"`
	Summary     string `kong:"arg,required,help='Issue summary'"`
	Description string `kong:"help='Issue description (markdown)'"`
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
	_, _ = fmt.Fprintln(w, "  # List Jira issues (JSON)")
	_, _ = fmt.Fprintln(w, "  human issues list --project=KAN")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "  # List GitHub issues (JSON)")
	_, _ = fmt.Fprintln(w, "  human issues list --project=octocat/hello-world")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "  # Get a single issue as markdown")
	_, _ = fmt.Fprintln(w, "  human issue get KAN-1")
	_, _ = fmt.Fprintln(w, "  human issue get octocat/hello-world#42")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "  # Create a new issue")
	_, _ = fmt.Fprintln(w, `  human issue create --project=KAN "Implement login page"`)
	_, _ = fmt.Fprintln(w, `  human issue create --project=octocat/hello-world "Fix bug"`)
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "  # List configured trackers (JSON)")
	_, _ = fmt.Fprintln(w, "  human tracker list")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "  # Use a specific config by name")
	_, _ = fmt.Fprintln(w, "  human --tracker=work issues list --project=KAN")
	_, _ = fmt.Fprintln(w, "  human --tracker=personal issues list --project=octocat/hello-world")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "  # Install Claude Code skill and agent")
	_, _ = fmt.Fprintln(w, "  human install --agent claude")

	return nil
}

// needsTrackerClient returns true for commands that require a tracker client.
func needsTrackerClient(command string) bool {
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
		kong.Description("AI-powered issue tracker CLI.\nReads and manages issues across Jira and GitHub. Output is JSON and markdown."),
		kong.Help(helpPrinter),
		kong.UsageOnError(),
	)

	if needsTrackerClient(ctx.Command()) {
		kind, err := config.ResolveTracker(".", cli.TrackerName)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}

		backfillJiraEnv(&cli)
		backfillGitHubEnv(&cli)

		switch kind {
		case config.TrackerJira:
			if cli.JiraURL == "" || cli.JiraUser == "" || cli.JiraKey == "" {
				fmt.Fprintln(os.Stderr, "error: missing required Jira config (--jira-url, --jira-user, --jira-key or env vars)")
				os.Exit(1)
			}
			client := jira.New(cli.JiraURL, cli.JiraUser, cli.JiraKey)
			ctx.BindTo(client, (*tracker.Lister)(nil))
			ctx.BindTo(client, (*tracker.Getter)(nil))
			ctx.BindTo(client, (*tracker.Creator)(nil))

		case config.TrackerGitHub:
			if cli.GitHubToken == "" {
				fmt.Fprintln(os.Stderr, "error: missing required GitHub config (--github-token or GITHUB_TOKEN env var)")
				os.Exit(1)
			}
			if cli.GitHubURL == "" {
				cli.GitHubURL = "https://api.github.com"
			}
			client := github.New(cli.GitHubURL, cli.GitHubToken)
			ctx.BindTo(client, (*tracker.Lister)(nil))
			ctx.BindTo(client, (*tracker.Getter)(nil))
			ctx.BindTo(client, (*tracker.Creator)(nil))
		}
	}

	if err := ctx.Run(); err != nil {
		errors.LogError(err).Msg("command failed")
		os.Exit(1)
	}
}

func backfillJiraEnv(cli *CLI) {
	if cli.JiraURL == "" {
		cli.JiraURL = os.Getenv("JIRA_URL")
	}
	if cli.JiraUser == "" {
		cli.JiraUser = os.Getenv("JIRA_USER")
	}
	if cli.JiraKey == "" {
		cli.JiraKey = os.Getenv("JIRA_KEY")
	}
}

func backfillGitHubEnv(cli *CLI) {
	if cli.GitHubURL == "" {
		cli.GitHubURL = os.Getenv("GITHUB_URL")
	}
	if cli.GitHubToken == "" {
		cli.GitHubToken = os.Getenv("GITHUB_TOKEN")
	}
}
