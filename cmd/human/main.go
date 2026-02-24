package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/alecthomas/kong"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"human/errors"
	"human/internal/claude"
	"human/internal/jira"
	"human/internal/tracker"
)

// CLI is the top-level Kong struct with global flags.
type CLI struct {
	JiraKey  string     `kong:"env='JIRA_KEY',help='Jira API token'"`
	JiraURL  string     `kong:"env='JIRA_URL',help='Jira base URL'"`
	JiraUser string     `kong:"env='JIRA_USER',help='Jira user email'"`
	Issues   IssuesCmd  `kong:"cmd,help='Bulk issue operations'"`
	Issue    IssueCmd   `kong:"cmd,help='Single issue operations'"`
	Install  InstallCmd `kong:"cmd,help='Install agent integrations'"`
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
	List ListCmd `kong:"cmd,help='List project issues as a KEY/STATUS/SUMMARY table'"`
}

type ListCmd struct {
	Project string `kong:"required,help='Jira project key (e.g. KAN)'"`
}

func (cmd *ListCmd) Run(l tracker.Lister) error {
	issues, err := l.ListIssues(context.TODO(), tracker.ListOptions{
		Project:    cmd.Project,
		MaxResults: 50,
	})
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "KEY\tSTATUS\tSUMMARY")
	for _, issue := range issues {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", issue.Key, issue.Status, issue.Summary)
	}
	return w.Flush()
}

// --- issue get ---

type IssueCmd struct {
	Get GetCmd `kong:"cmd,help='Get a single issue with metadata and description as markdown'"`
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
	_, _ = fmt.Fprintln(w, "  # List all issues in a project (outputs tab-separated table)")
	_, _ = fmt.Fprintln(w, "  human --jira-url=$JIRA_URL --jira-user=$JIRA_USER --jira-key=$JIRA_KEY issues list --project=KAN")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "  # Get a single issue as markdown")
	_, _ = fmt.Fprintln(w, "  human --jira-url=$JIRA_URL --jira-user=$JIRA_USER --jira-key=$JIRA_KEY issue get KAN-1")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "  # Pipe issue details to another tool")
	_, _ = fmt.Fprintln(w, "  human --jira-url=$JIRA_URL --jira-user=$JIRA_USER --jira-key=$JIRA_KEY issue get KAN-1 | llm 'summarize this'")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "  # Install Claude Code skill and agent (no Jira credentials needed)")
	_, _ = fmt.Fprintln(w, "  human install --agent claude")

	return nil
}

// --- main ---

func main() {
	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

	// Show help when invoked without arguments.
	if len(os.Args) < 2 {
		os.Args = append(os.Args, "--help")
	}

	var cli CLI
	ctx := kong.Parse(&cli,
		kong.Name("human"),
		kong.Description("AI-powered issue tracker CLI.\nReads and manages Jira issues. Output is plain text tables and markdown."),
		kong.Help(helpPrinter),
		kong.UsageOnError(),
	)

	if !strings.HasPrefix(ctx.Command(), "install") {
		if cli.JiraURL == "" || cli.JiraUser == "" || cli.JiraKey == "" {
			fmt.Fprintln(os.Stderr, "error: missing required Jira config (--jira-url, --jira-user, --jira-key or env vars)")
			os.Exit(1)
		}
		client := jira.New(cli.JiraURL, cli.JiraUser, cli.JiraKey)
		ctx.BindTo(client, (*tracker.Lister)(nil))
		ctx.BindTo(client, (*tracker.Getter)(nil))
	}

	if err := ctx.Run(); err != nil {
		errors.LogError(err).Msg("command failed")
		os.Exit(1)
	}
}
