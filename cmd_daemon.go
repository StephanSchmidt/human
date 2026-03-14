package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/internal/chrome"
	"github.com/StephanSchmidt/human/internal/daemon"
)

func buildDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run human as a daemon for remote (devcontainer) access",
	}

	cmd.AddCommand(buildDaemonStartCmd())
	cmd.AddCommand(buildDaemonTokenCmd())
	cmd.AddCommand(buildDaemonStatusCmd())
	return cmd
}

func buildDaemonStartCmd() *cobra.Command {
	var addr string
	var chromeAddr string

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the daemon listener",
		Long:  "Start the daemon on the host. AI agents inside devcontainers connect to this daemon to execute commands with the host's credentials.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			token, err := daemon.LoadOrCreateToken()
			if err != nil {
				return fmt.Errorf("failed to load/create token: %w", err)
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintln(out, "Token:", token)
			_, _ = fmt.Fprintln(out, "Token file:", daemon.TokenPath())
			_, _ = fmt.Fprintln(out, "Listening on:", addr)
			_, _ = fmt.Fprintln(out, "Chrome proxy on:", chromeAddr)
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintln(out, "Set these env vars in the container:")
			_, _ = fmt.Fprintf(out, "  HUMAN_DAEMON_ADDR=%s\n", addr)
			_, _ = fmt.Fprintf(out, "  HUMAN_DAEMON_TOKEN=%s\n", token)
			_, _ = fmt.Fprintf(out, "  HUMAN_CHROME_ADDR=%s\n", chromeAddr)

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

			srv := &daemon.Server{
				Addr:       addr,
				Token:      token,
				CmdFactory: newRootCmd,
				Logger:     logger,
			}

			// Start chrome proxy server in a separate goroutine.
			// Use SocketConnector to connect to the real Chrome native
			// messaging host socket (created by Chrome's extension).
			socketDir, sdErr := chrome.SocketDir()
			if sdErr != nil {
				return fmt.Errorf("resolving socket directory: %w", sdErr)
			}

			chromeSrv := &chrome.Server{
				Addr:  chromeAddr,
				Token: token,
				Spawner: &chrome.SocketConnector{
					SocketDir: socketDir,
					Logger:    logger,
				},
				Logger: logger,
			}

			go func() {
				if err := chromeSrv.ListenAndServe(ctx); err != nil {
					logger.Error().Err(err).Msg("chrome proxy server failed")
				}
			}()

			return srv.ListenAndServe(ctx)
		},
	}

	cmd.Flags().StringVar(&addr, "addr", ":19285", "Listen address (host:port)")
	cmd.Flags().StringVar(&chromeAddr, "chrome-addr", ":19286", "Chrome proxy listen address (host:port)")
	return cmd
}

func buildDaemonTokenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "token",
		Short: "Print the current daemon token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			token, err := daemon.LoadOrCreateToken()
			if err != nil {
				return fmt.Errorf("failed to load/create token: %w", err)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), token)
			return nil
		},
	}
}

func buildDaemonStatusCmd() *cobra.Command {
	var addr string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check if a daemon is reachable",
		RunE: func(cmd *cobra.Command, _ []string) error {
			conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
			if err != nil {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Daemon is not reachable at", addr)
				return fmt.Errorf("cannot connect to daemon: %w", err)
			}
			_ = conn.Close()
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Daemon is reachable at", addr)
			return nil
		},
	}

	cmd.Flags().StringVar(&addr, "addr", "localhost:19285", "Daemon address to check")
	return cmd
}
