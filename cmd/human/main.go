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

	"github.com/stephanschmidt/human/errors"
	"github.com/stephanschmidt/human/internal/claude"
	"github.com/stephanschmidt/human/internal/github"
	"github.com/stephanschmidt/human/internal/gitlab"
	"github.com/stephanschmidt/human/internal/jira"
	"github.com/stephanschmidt/human/internal/linear"
	"github.com/stephanschmidt/human/internal/tracker"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// CLI is the top-level Kong struct with global flags.
type CLI struct {
	Version     kong.VersionFlag `kong:"help='Print version information'"`
	TrackerName string           `kong:"name='tracker',help='Named tracker from .humanconfig (resolves type automatically)'"`
	JiraKey     string           `kong:"env='JIRA_KEY',help='Jira API token'"`
	JiraURL     string           `kong:"env='JIRA_URL',help='Jira base URL'"`
	JiraUser    string           `kong:"env='JIRA_USER',help='Jira user email'"`
	GitHubToken string           `kong:"env='GITHUB_TOKEN',help='GitHub personal access token'"`
	GitHubURL   string           `kong:"env='GITHUB_URL',help='GitHub API base URL'"`
	GitLabToken string           `kong:"env='GITLAB_TOKEN',help='GitLab private token'"`
	GitLabURL   string           `kong:"env='GITLAB_URL',help='GitLab base URL'"`
	LinearToken string           `kong:"env='LINEAR_TOKEN',help='Linear API key'"`
	LinearURL   string           `kong:"env='LINEAR_URL',help='Linear API base URL'"`
	Issues      IssuesCmd        `kong:"cmd,help='Bulk issue operations'"`
	Issue       IssueCmd         `kong:"cmd,help='Single issue operations'"`
	Install     InstallCmd       `kong:"cmd,help='Install agent integrations'"`
	Tracker     TrackerCmd       `kong:"cmd,help='Manage tracker connections'"`
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
	instances, err := loadAllInstances(".")
	if err != nil {
		return err
	}

	entries := make([]trackerEntry, len(instances))
	for i, inst := range instances {
		entries[i] = trackerEntry{Name: inst.Name, Type: inst.Kind, URL: inst.URL, User: inst.User}
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
	Project  string           `kong:"required,help='Project key (Jira: KAN, GitHub: owner/repo, GitLab: group/project, Linear: ENG)'"`
	Table    bool             `kong:"help='Output as human-readable table instead of JSON'"`
	Provider tracker.Provider `kong:"-"`
}

func (cmd *ListCmd) Run() error {
	issues, err := cmd.Provider.ListIssues(context.TODO(), tracker.ListOptions{
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
	Get     GetCmd     `kong:"cmd,help='Get a single issue with metadata and description as markdown'"`
	Create  CreateCmd  `kong:"cmd,help='Create a new issue in a project'"`
	Comment CommentCmd `kong:"cmd,help='Comment operations on an issue'"`
}

type CommentCmd struct {
	Add  AddCommentCmd   `kong:"cmd,help='Add a comment to an issue'"`
	List ListCommentsCmd `kong:"cmd,help='List comments on an issue'"`
}

type AddCommentCmd struct {
	Key      string           `kong:"arg,required,help='Issue key'"`
	Body     string           `kong:"arg,required,help='Comment body (markdown)'"`
	Provider tracker.Provider `kong:"-"`
}

func (cmd *AddCommentCmd) Run() error {
	comment, err := cmd.Provider.AddComment(context.TODO(), cmd.Key, cmd.Body)
	if err != nil {
		return err
	}
	fmt.Printf("%s\t%s\n", comment.ID, comment.Body)
	return nil
}

type ListCommentsCmd struct {
	Key      string           `kong:"arg,required,help='Issue key'"`
	Provider tracker.Provider `kong:"-"`
}

func (cmd *ListCommentsCmd) Run() error {
	comments, err := cmd.Provider.ListComments(context.TODO(), cmd.Key)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(comments)
}

type GetCmd struct {
	Key      string           `kong:"arg,required,help='Issue key (Jira: KAN-1, GitHub: owner/repo#123, GitLab: group/project#42, Linear: ENG-123)'"`
	Provider tracker.Provider `kong:"-"`
}

func (cmd *GetCmd) Run() error {
	issue, err := cmd.Provider.GetIssue(context.TODO(), cmd.Key)
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
	Project     string           `kong:"required,help='Project key (Jira: KAN, GitHub: owner/repo, GitLab: group/project, Linear: ENG)'"`
	Type        string           `kong:"default='Task',help='Issue type (Jira only, e.g. Task, Bug, Story)'"`
	Summary     string           `kong:"arg,required,help='Issue summary'"`
	Description string           `kong:"help='Issue description (markdown)'"`
	Provider    tracker.Provider `kong:"-"`
}

func (cmd *CreateCmd) Run() error {
	issue, err := cmd.Provider.CreateIssue(context.TODO(), &tracker.Issue{
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
	_, _ = fmt.Fprintln(w, "  # List GitLab issues (JSON)")
	_, _ = fmt.Fprintln(w, "  human issues list --project=mygroup/myproject")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "  # Get a GitLab issue as markdown")
	_, _ = fmt.Fprintln(w, "  human issue get mygroup/myproject#42")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "  # Create a GitLab issue")
	_, _ = fmt.Fprintln(w, `  human issue create --project=mygroup/myproject "Fix bug"`)
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "  # List Linear issues (JSON)")
	_, _ = fmt.Fprintln(w, "  human issues list --project=ENG")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "  # Get a Linear issue as markdown")
	_, _ = fmt.Fprintln(w, "  human issue get ENG-123")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "  # Create a Linear issue")
	_, _ = fmt.Fprintln(w, `  human issue create --project=ENG "Implement feature"`)
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

// keyHint returns the first issue-key or project-key visible on the CLI so
// that Resolve can auto-detect the tracker kind from the key format.
func keyHint(cli *CLI) string {
	switch {
	case cli.Issue.Get.Key != "":
		return cli.Issue.Get.Key
	case cli.Issue.Create.Project != "":
		return cli.Issue.Create.Project
	case cli.Issues.List.Project != "":
		return cli.Issues.List.Project
	case cli.Issue.Comment.Add.Key != "":
		return cli.Issue.Comment.Add.Key
	case cli.Issue.Comment.List.Key != "":
		return cli.Issue.Comment.List.Key
	default:
		return ""
	}
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
	return append(all, li...), nil
}

// instanceFromCLI builds a tracker instance from CLI flags, returning nil
// when insufficient flags are provided.
func instanceFromCLI(cli *CLI) *tracker.Instance {
	if cli.JiraURL != "" && cli.JiraUser != "" && cli.JiraKey != "" {
		return &tracker.Instance{
			Kind:     "jira",
			URL:      cli.JiraURL,
			User:     cli.JiraUser,
			Provider: jira.New(cli.JiraURL, cli.JiraUser, cli.JiraKey),
		}
	}
	if cli.GitHubToken != "" {
		url := cli.GitHubURL
		if url == "" {
			url = "https://api.github.com"
		}
		return &tracker.Instance{
			Kind:     "github",
			URL:      url,
			Provider: github.New(url, cli.GitHubToken),
		}
	}
	if cli.GitLabToken != "" {
		url := cli.GitLabURL
		if url == "" {
			url = "https://gitlab.com"
		}
		return &tracker.Instance{
			Kind:     "gitlab",
			URL:      url,
			Provider: gitlab.New(url, cli.GitLabToken),
		}
	}
	if cli.LinearToken != "" {
		url := cli.LinearURL
		if url == "" {
			url = "https://api.linear.app"
		}
		return &tracker.Instance{
			Kind:     "linear",
			URL:      url,
			Provider: linear.New(url, cli.LinearToken),
		}
	}
	return nil
}

// setProvider sets the Provider field on all commands that need it.
func setProvider(cli *CLI, p tracker.Provider) {
	cli.Issues.List.Provider = p
	cli.Issue.Get.Provider = p
	cli.Issue.Create.Provider = p
	cli.Issue.Comment.Add.Provider = p
	cli.Issue.Comment.List.Provider = p
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
		kong.Description("AI-powered issue tracker CLI.\nReads and manages issues across Jira, GitHub, GitLab, and Linear. Output is JSON and markdown."),
		kong.Help(helpPrinter),
		kong.UsageOnError(),
		kong.Vars{"version": version + " (" + commit + ") " + date},
	)

	if needsTrackerClient(ctx.Command()) {
		instances, err := loadAllInstances(".")
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}

		if inst := instanceFromCLI(&cli); inst != nil {
			instances = append(instances, *inst)
		}

		instance, err := tracker.Resolve(cli.TrackerName, instances, keyHint(&cli))
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}

		setProvider(&cli, instance.Provider)
	}

	if err := ctx.Run(); err != nil {
		errors.LogError(err).Msg("command failed")
		os.Exit(1)
	}
}
