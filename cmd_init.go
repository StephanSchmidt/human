package main

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/internal/claude"
	initpkg "github.com/StephanSchmidt/human/internal/init"
)

// huhPrompter implements initpkg.Prompter using charmbracelet/huh forms.
type huhPrompter struct{}

func (h huhPrompter) ConfirmOverwrite() (bool, error) {
	var overwrite bool
	err := huh.NewConfirm().
		Title(".humanconfig.yaml already exists. Overwrite?").
		Affirmative("Yes").
		Negative("No").
		Value(&overwrite).
		Run()
	return overwrite, err
}

func (h huhPrompter) SelectServices(available []initpkg.ServiceType) ([]initpkg.ServiceType, error) {
	options := make([]huh.Option[int], len(available))
	for i, svc := range available {
		options[i] = huh.NewOption(svc.Label, i)
	}

	theme := huh.ThemeCharm()
	theme.Focused.SelectedPrefix = lipgloss.NewStyle().SetString("[x] ")
	theme.Focused.UnselectedPrefix = lipgloss.NewStyle().SetString("[ ] ")
	theme.Blurred.SelectedPrefix = theme.Focused.SelectedPrefix
	theme.Blurred.UnselectedPrefix = theme.Focused.UnselectedPrefix

	var indices []int
	ms := huh.NewMultiSelect[int]().
		Title("Select services to configure").
		Description("space/x to toggle, enter to confirm").
		Options(options...).
		Filterable(false).
		Validate(func(selected []int) error {
			if len(selected) == 0 {
				return fmt.Errorf("select at least one service")
			}
			return nil
		}).
		Value(&indices)

	err := huh.NewForm(huh.NewGroup(ms)).
		WithTheme(theme).
		Run()
	if err != nil {
		return nil, err
	}

	selected := make([]initpkg.ServiceType, len(indices))
	for i, idx := range indices {
		selected[i] = available[idx]
	}
	return selected, nil
}

func (h huhPrompter) PromptInstance(svc initpkg.ServiceType) (map[string]string, error) {
	values := map[string]string{}

	var name string
	var url string
	var description string

	fields := []huh.Field{
		huh.NewInput().
			Title(fmt.Sprintf("%s — instance name", svc.Label)).
			Description("a label to identify this instance (e.g. work, personal)").
			Placeholder("work").
			Value(&name),
	}

	if svc.URLRequired || svc.DefaultURL == "" {
		fields = append(fields, huh.NewInput().
			Title("URL").
			Placeholder(svc.DefaultURL).
			Value(&url))
	}

	for _, extra := range svc.ExtraFields {
		val := new(string)
		fields = append(fields, huh.NewInput().
			Title(fmt.Sprintf("%s (required)", extra)).
			Value(val))
		// Capture closure over extra and val.
		defer func(field string, v *string) {
			if *v != "" {
				values[field] = *v
			}
		}(extra, val)
	}

	fields = append(fields, huh.NewInput().
		Title("Description (optional)").
		Value(&description))

	form := huh.NewForm(huh.NewGroup(fields...))
	if err := form.Run(); err != nil {
		return nil, err
	}

	if name == "" {
		name = "work"
	}
	values["name"] = name

	if url != "" {
		values["url"] = url
	} else if svc.DefaultURL != "" {
		values["url"] = svc.DefaultURL
	}

	if description != "" {
		values["description"] = description
	}

	return values, nil
}

func (h huhPrompter) ConfirmAgentInstall() (bool, error) {
	install := true
	err := huh.NewConfirm().
		Title("Install Claude Code agent integration?").
		Affirmative("Yes").
		Negative("No").
		Value(&install).
		Run()
	return install, err
}

func buildInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Interactive setup wizard for .humanconfig.yaml",
		Long: `Interactively configure trackers and tools, write .humanconfig.yaml,
and optionally install Claude Code agent integration.

Credentials are never stored in the config file — the wizard prints
the environment variables you need to set.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return initpkg.RunInit(cmd.OutOrStdout(), huhPrompter{}, claude.OSFileWriter{})
		},
	}
}
