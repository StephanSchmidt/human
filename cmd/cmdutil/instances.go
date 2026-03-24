package cmdutil

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/internal/azuredevops"
	"github.com/StephanSchmidt/human/internal/github"
	"github.com/StephanSchmidt/human/internal/gitlab"
	"github.com/StephanSchmidt/human/internal/index"
	"github.com/StephanSchmidt/human/internal/jira"
	"github.com/StephanSchmidt/human/internal/linear"
	"github.com/StephanSchmidt/human/internal/notion"
	"github.com/StephanSchmidt/human/internal/shortcut"
	"github.com/StephanSchmidt/human/internal/tracker"
)

// LoadAllInstances collects tracker instances from all provider configs.
func LoadAllInstances(dir string) ([]tracker.Instance, error) {
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

// InstanceFromFlags builds a tracker instance from root persistent flags,
// returning nil when insufficient flags are provided.
func InstanceFromFlags(cmd *cobra.Command) *tracker.Instance {
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

// LoadNotionIndexInstances loads Notion instances and converts them
// to index.NotionInstance for use by the indexer.
func LoadNotionIndexInstances(dir string) ([]index.NotionInstance, error) {
	notionInsts, err := notion.LoadInstances(dir)
	if err != nil {
		return nil, err
	}
	var result []index.NotionInstance
	for _, ni := range notionInsts {
		result = append(result, index.NotionInstance{
			Name:   ni.Name,
			URL:    ni.URL,
			Client: ni.Client,
		})
	}
	return result, nil
}

// AuditLogPath returns the path to the audit log file (~/.human/audit.log),
// creating the directory if needed.
func AuditLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".human", "audit.log")
	}
	dir := filepath.Join(home, ".human")
	_ = os.MkdirAll(dir, 0o750)
	return filepath.Join(dir, "audit.log")
}
