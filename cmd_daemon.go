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
	"github.com/StephanSchmidt/human/internal/proxy"
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
	var proxyAddr string
	var interactive bool
	var safe bool
	var debug bool

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
			hostIP := resolveHostIP()
			daemonAddr := replaceHost(addr, hostIP)
			chromeFullAddr := replaceHost(chromeAddr, hostIP)

			_, _ = fmt.Fprintln(out, "Token:", token)
			_, _ = fmt.Fprintln(out, "Token file:", daemon.TokenPath())
			_, _ = fmt.Fprintln(out, "Listening on:", addr)
			_, _ = fmt.Fprintln(out, "Chrome proxy on:", chromeAddr)
			_, _ = fmt.Fprintln(out, "HTTPS proxy on:", proxyAddr)
			proxyFullAddr := replaceHost(proxyAddr, hostIP)
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintln(out, "Run in the container:")
			_, _ = fmt.Fprintf(out, "  export HUMAN_DAEMON_ADDR=%s HUMAN_DAEMON_TOKEN=%s HUMAN_CHROME_ADDR=%s HUMAN_PROXY_ADDR=%s\n",
				daemonAddr, token, chromeFullAddr, proxyFullAddr)
			_, _ = fmt.Fprintf(out, "  export BROWSER=human-browser\n")
			_, _ = fmt.Fprintln(out, "  ln -sf $(which human) /usr/local/bin/human-browser  # if not already installed")

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			level := zerolog.InfoLevel
			if debug {
				level = zerolog.DebugLevel
			}
			logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger().Level(level)

			srv := &daemon.Server{
				Addr:       addr,
				Token:      token,
				SafeMode:   safe,
				CmdFactory: newRootCmd,
				Logger:     logger,
			}

			// Start socket relay to accept Chrome native messaging
			// connections directly, then start the chrome proxy server
			// using the relay as its ProcessSpawner.
			socketDir, sdErr := chrome.SocketDir()
			if sdErr != nil {
				return fmt.Errorf("resolving socket directory: %w", sdErr)
			}

			relay := chrome.NewSocketRelay(socketDir, logger)

			go func() {
				if err := relay.ListenAndServe(ctx); err != nil {
					logger.Error().Err(err).Msg("socket relay failed")
				}
			}()

			chromeSrv := &chrome.Server{
				Addr:    chromeAddr,
				Token:   token,
				Spawner: relay,
				Logger:  logger,
			}

			go func() {
				if err := chromeSrv.ListenAndServe(ctx); err != nil {
					logger.Error().Err(err).Msg("chrome proxy server failed")
				}
			}()

			proxyCfg, _ := proxy.LoadConfig(".")
			var policy proxy.Decider
			if proxyCfg != nil {
				policy, err = proxy.NewPolicy(proxyCfg.Mode, proxyCfg.Domains)
				if err != nil {
					return fmt.Errorf("invalid proxy policy: %w", err)
				}
			} else {
				policy = proxy.BlockAllPolicy()
			}

			if interactive {
				prompt := proxy.NewTerminalPrompt(os.Stdin, os.Stderr)
				policy = proxy.NewInteractiveDecider(policy, prompt)
				_, _ = fmt.Fprintln(out, "Interactive proxy mode: unknown domains will prompt for approval")
			}

			proxySrv := &proxy.Server{
				Addr:   proxyAddr,
				Policy: policy,
				Logger: logger,
			}

			go func() {
				if err := proxySrv.ListenAndServe(ctx); err != nil {
					logger.Error().Err(err).Msg("https proxy failed")
				}
			}()

			return srv.ListenAndServe(ctx)
		},
	}

	cmd.Flags().StringVar(&addr, "addr", ":19285", "Listen address (host:port)")
	cmd.Flags().StringVar(&chromeAddr, "chrome-addr", ":19286", "Chrome proxy listen address (host:port)")
	cmd.Flags().StringVar(&proxyAddr, "proxy-addr", ":19287", "HTTPS proxy listen address (host:port)")
	cmd.Flags().BoolVar(&interactive, "interactive", false, "Prompt for unknown domains instead of blocking them")
	cmd.Flags().BoolVar(&safe, "safe", os.Getenv("HUMAN_SAFE") == "1", "Block destructive operations for all daemon requests")
	cmd.Flags().BoolVar(&debug, "debug", false, "Enable debug logging")
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

// resolveHostIP returns the preferred outbound IP of the host.
// Falls back to "localhost" if detection fails.
func resolveHostIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "localhost"
	}
	defer func() { _ = conn.Close() }()

	addr := conn.LocalAddr().(*net.UDPAddr)
	return addr.IP.String()
}

// replaceHost replaces an empty or wildcard host in addr with the given host.
// e.g. ":19285" → "192.168.1.5:19285", "0.0.0.0:19285" → "192.168.1.5:19285".
func replaceHost(addr, host string) string {
	h, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if h == "" || h == "0.0.0.0" || h == "::" {
		return net.JoinHostPort(host, port)
	}
	return addr
}
