package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/internal/chrome"
)

func buildChromeBridgeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "chrome-bridge",
		Short: "Bridge Chrome MCP socket to daemon (for devcontainer use)",
		Long: `Creates a fake Unix socket that Claude's MCP server can discover,
and tunnels traffic over TCP to the daemon running on the host.

Requires HUMAN_CHROME_ADDR and HUMAN_DAEMON_TOKEN environment variables.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			addr := os.Getenv("HUMAN_CHROME_ADDR")
			if addr == "" {
				return chromeBridgeError("HUMAN_CHROME_ADDR environment variable is required")
			}

			token := os.Getenv("HUMAN_DAEMON_TOKEN")
			if token == "" {
				return chromeBridgeError("HUMAN_DAEMON_TOKEN environment variable is required")
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

			bridge := &chrome.Bridge{
				Dialer:  chrome.DefaultDialer{},
				Addr:    addr,
				Token:   token,
				Version: version,
				Logger:  logger,
			}

			return bridge.ListenAndServe(ctx)
		},
	}
}

// chromeBridgeError creates an error with the given message.
func chromeBridgeError(msg string) error {
	return &chromeBridgeErr{msg: msg}
}

type chromeBridgeErr struct {
	msg string
}

func (e *chromeBridgeErr) Error() string {
	return e.msg
}
