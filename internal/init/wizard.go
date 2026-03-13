package init

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"text/template"

	"github.com/StephanSchmidt/human/errors"
	"github.com/StephanSchmidt/human/internal/claude"
)

// ServiceType describes a configurable service with its YAML key, defaults, and env var pattern.
type ServiceType struct {
	Label      string   // display name, e.g. "Jira"
	ConfigKey  string   // YAML top-level key, e.g. "jiras"
	DefaultURL string   // empty means user must provide it
	URLRequired bool    // if true and DefaultURL is empty, prompt for URL
	ExtraFields []string // additional fields beyond name+description, e.g. "user", "org"
	EnvVars    []string // env var suffixes, e.g. ["KEY"] → JIRA_{NAME}_KEY
	EnvPrefix  string   // e.g. "JIRA"
}

// ServiceRegistry returns all available services.
func ServiceRegistry() []ServiceType {
	return []ServiceType{
		{
			Label: "Jira", ConfigKey: "jiras",
			URLRequired: true, ExtraFields: []string{"user"},
			EnvVars: []string{"KEY"}, EnvPrefix: "JIRA",
		},
		{
			Label: "GitHub", ConfigKey: "githubs",
			DefaultURL: "https://api.github.com",
			EnvVars: []string{"TOKEN"}, EnvPrefix: "GITHUB",
		},
		{
			Label: "GitLab", ConfigKey: "gitlabs",
			DefaultURL: "https://gitlab.com",
			EnvVars: []string{"TOKEN"}, EnvPrefix: "GITLAB",
		},
		{
			Label: "Linear", ConfigKey: "linears",
			DefaultURL: "https://api.linear.app",
			EnvVars: []string{"TOKEN"}, EnvPrefix: "LINEAR",
		},
		{
			Label: "Azure DevOps", ConfigKey: "azuredevops",
			DefaultURL: "https://dev.azure.com",
			ExtraFields: []string{"org"},
			EnvVars: []string{"TOKEN"}, EnvPrefix: "AZURE",
		},
		{
			Label: "Shortcut", ConfigKey: "shortcuts",
			DefaultURL: "https://api.app.shortcut.com",
			EnvVars: []string{"TOKEN"}, EnvPrefix: "SHORTCUT",
		},
		{
			Label: "Notion", ConfigKey: "notions",
			DefaultURL: "https://api.notion.com",
			EnvVars: []string{"TOKEN"}, EnvPrefix: "NOTION",
		},
		{
			Label: "Figma", ConfigKey: "figmas",
			DefaultURL: "https://api.figma.com",
			EnvVars: []string{"TOKEN"}, EnvPrefix: "FIGMA",
		},
		{
			Label: "Amplitude", ConfigKey: "amplitudes",
			DefaultURL: "https://amplitude.com",
			URLRequired: true,
			EnvVars: []string{"KEY", "SECRET"}, EnvPrefix: "AMPLITUDE",
		},
	}
}

// Prompter abstracts TUI interactions for testability.
type Prompter interface {
	ConfirmOverwrite() (bool, error)
	SelectServices(available []ServiceType) ([]ServiceType, error)
	PromptInstance(svc ServiceType) (map[string]string, error)
	ConfirmAgentInstall() (bool, error)
}

// serviceInstance holds the values collected for one service instance.
type serviceInstance struct {
	Service ServiceType
	Values  map[string]string
}

// EnvVarName returns the env var name for a given service instance and suffix.
func EnvVarName(prefix, instanceName, suffix string) string {
	return prefix + "_" + strings.ToUpper(instanceName) + "_" + suffix
}

// configData groups instances by config key for template rendering.
type configData struct {
	Sections []configSection
}

type configSection struct {
	ConfigKey string
	Instances []configInstance
}

type configInstance struct {
	Name        string
	URL         string
	User        string
	Org         string
	Description string
	EnvComments []string
}

// GenerateConfig produces the YAML config from collected service instances.
func GenerateConfig(instances []serviceInstance) (string, error) {
	// Group by config key, preserving order.
	sectionOrder := make([]string, 0)
	sectionMap := make(map[string][]configInstance)

	for _, inst := range instances {
		key := inst.Service.ConfigKey
		if _, exists := sectionMap[key]; !exists {
			sectionOrder = append(sectionOrder, key)
		}

		ci := configInstance{
			Name:        inst.Values["name"],
			URL:         inst.Values["url"],
			User:        inst.Values["user"],
			Org:         inst.Values["org"],
			Description: inst.Values["description"],
		}

		for _, suffix := range inst.Service.EnvVars {
			envName := EnvVarName(inst.Service.EnvPrefix, ci.Name, suffix)
			ci.EnvComments = append(ci.EnvComments, fmt.Sprintf("export %s=your-%s", envName, strings.ToLower(suffix)))
		}

		sectionMap[key] = append(sectionMap[key], ci)
	}

	data := configData{}
	for _, key := range sectionOrder {
		data.Sections = append(data.Sections, configSection{
			ConfigKey: key,
			Instances: sectionMap[key],
		})
	}

	tmpl, err := template.New("config").Parse(configTemplate)
	if err != nil {
		return "", errors.WrapWithDetails(err, "parsing config template")
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", errors.WrapWithDetails(err, "executing config template")
	}

	return buf.String(), nil
}

const configTemplate = `{{- range $i, $section := .Sections }}{{ if $i }}
{{ end }}{{ $section.ConfigKey }}:
{{- range $section.Instances }}
  - name: {{ .Name }}
{{- if .URL }}
    url: {{ .URL }}
{{- end }}
{{- if .User }}
    user: {{ .User }}
{{- end }}
{{- if .Org }}
    org: {{ .Org }}
{{- end }}
{{- if .Description }}
    description: "{{ .Description }}"
{{- end }}
{{- range .EnvComments }}
    # {{ . }}
{{- end }}
{{- end }}
{{- end }}
`

// configPath is the filename written by RunInit.
const configPath = ".humanconfig.yaml"

// RunInit orchestrates the init wizard.
func RunInit(w io.Writer, prompter Prompter, fw claude.FileWriter) error {
	// Step 1: Check for existing config.
	if _, err := os.Stat(configPath); err == nil {
		overwrite, promptErr := prompter.ConfirmOverwrite()
		if promptErr != nil {
			return errors.WrapWithDetails(promptErr, "confirming overwrite")
		}
		if !overwrite {
			_, _ = fmt.Fprintln(w, "Aborted — existing .humanconfig.yaml kept.")
			return nil
		}
	}

	// Step 2: Select services.
	registry := ServiceRegistry()
	selected, err := prompter.SelectServices(registry)
	if err != nil {
		return errors.WrapWithDetails(err, "selecting services")
	}
	if len(selected) == 0 {
		_, _ = fmt.Fprintln(w, "No services selected — nothing to configure.")
		return nil
	}

	// Step 3: Prompt per-service details.
	var instances []serviceInstance
	for _, svc := range selected {
		values, promptErr := prompter.PromptInstance(svc)
		if promptErr != nil {
			return errors.WrapWithDetails(promptErr, "configuring service",
				"service", svc.Label)
		}
		instances = append(instances, serviceInstance{Service: svc, Values: values})
	}

	// Step 4: Generate and write config.
	yaml, err := GenerateConfig(instances)
	if err != nil {
		return err
	}

	if err := fw.WriteFile(configPath, []byte(yaml), 0o644); err != nil {
		return errors.WrapWithDetails(err, "writing config file",
			"path", configPath)
	}
	_, _ = fmt.Fprintf(w, "Wrote %s\n", configPath)

	// Step 5: Print env vars to set.
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Set these environment variables:")
	for _, inst := range instances {
		name := inst.Values["name"]
		for _, suffix := range inst.Service.EnvVars {
			envName := EnvVarName(inst.Service.EnvPrefix, name, suffix)
			_, _ = fmt.Fprintf(w, "  export %s=your-%s\n", envName, strings.ToLower(suffix))
		}
	}
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Tip: Use a secret manager to inject tokens instead of hardcoding them.")
	_, _ = fmt.Fprintln(w, "  1Password CLI (op) is free for personal use: https://1password.com/downloads/command-line")
	_, _ = fmt.Fprintln(w, "  Example: export JIRA_WORK_TOKEN=$(op read 'op://Vault/Jira/token')")

	// Step 6: Optionally install agents.
	installAgents, err := prompter.ConfirmAgentInstall()
	if err != nil {
		return errors.WrapWithDetails(err, "confirming agent install")
	}
	if installAgents {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "Installing Claude Code integration...")
		if err := claude.Install(w, fw, false); err != nil {
			return err
		}
	}

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Done! Run 'human tracker list --table' to verify.")
	return nil
}
