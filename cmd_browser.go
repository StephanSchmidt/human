package main

import (
	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/internal/browser"
)

func buildBrowserCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "browser URL",
		Short: "Open a URL in the default browser",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return browser.RunOpen(browser.DefaultOpener{}, cmd.OutOrStdout(), args[0])
		},
	}
}
