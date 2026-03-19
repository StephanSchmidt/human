package init

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/StephanSchmidt/human/errors"
	"github.com/StephanSchmidt/human/internal/claude"
)

// ServicesPrompter abstracts TUI interactions for the services step.
type ServicesPrompter interface {
	ConfirmOverwrite() (bool, error)
	ConfirmAddTrackers() (bool, error)
	SelectServices(available []ServiceType) ([]ServiceType, error)
	PromptInstance(svc ServiceType) (map[string]string, error)
}

type servicesStep struct {
	prompter ServicesPrompter
}

// NewServicesStep creates a WizardStep that configures services and writes .humanconfig.yaml.
func NewServicesStep(p ServicesPrompter) WizardStep {
	return &servicesStep{prompter: p}
}

func (s *servicesStep) Name() string { return "services" }

func (s *servicesStep) Run(w io.Writer, fw claude.FileWriter) ([]string, error) {
	// Check for existing config.
	if _, err := os.Stat(configPath); err == nil {
		overwrite, promptErr := s.prompter.ConfirmOverwrite()
		if promptErr != nil {
			return nil, errors.WrapWithDetails(promptErr, "confirming overwrite")
		}
		if !overwrite {
			_, _ = fmt.Fprintln(w, "Aborted — existing .humanconfig.yaml kept.")
			return nil, nil
		}
	} else {
		addTrackers, promptErr := s.prompter.ConfirmAddTrackers()
		if promptErr != nil {
			return nil, errors.WrapWithDetails(promptErr, "confirming tracker setup")
		}
		if !addTrackers {
			_, _ = fmt.Fprintln(w, "Skipping tracker configuration.")
			return nil, nil
		}
	}

	// Select services.
	registry := ServiceRegistry()
	selected, err := s.prompter.SelectServices(registry)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "selecting services")
	}
	if len(selected) == 0 {
		_, _ = fmt.Fprintln(w, "No services selected — nothing to configure.")
		return nil, nil
	}

	// Prompt per-service details.
	var instances []serviceInstance
	for _, svc := range selected {
		values, promptErr := s.prompter.PromptInstance(svc)
		if promptErr != nil {
			return nil, errors.WrapWithDetails(promptErr, "configuring service",
				"service", svc.Label)
		}
		instances = append(instances, serviceInstance{Service: svc, Values: values})
	}

	// Generate and write config.
	yaml, err := GenerateConfig(instances)
	if err != nil {
		return nil, err
	}

	if err := fw.WriteFile(configPath, []byte(yaml), 0o644); err != nil {
		return nil, errors.WrapWithDetails(err, "writing config file",
			"path", configPath)
	}
	_, _ = fmt.Fprintf(w, "Wrote %s\n", configPath)

	// Print env vars to set.
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

	return nil, nil
}
