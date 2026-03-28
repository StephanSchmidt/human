package cmddaemon

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/internal/chrome"
	"github.com/StephanSchmidt/human/internal/claude"
	"github.com/StephanSchmidt/human/internal/daemon"
	"github.com/StephanSchmidt/human/internal/dispatch"
	"github.com/StephanSchmidt/human/internal/proxy"
	"github.com/StephanSchmidt/human/internal/slack"
	"github.com/StephanSchmidt/human/internal/telegram"
)

const daemonChildEnv = "_HUMAN_DAEMON_CHILD"

// BuildDaemonCmd creates the "daemon" command tree.
func BuildDaemonCmd(cmdFactory func() *cobra.Command, version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run human as a daemon for remote (devcontainer) access",
	}

	cmd.AddCommand(buildDaemonStartCmd(cmdFactory, version))
	cmd.AddCommand(buildDaemonTokenCmd())
	cmd.AddCommand(buildDaemonStatusCmd())
	cmd.AddCommand(buildDaemonStopCmd())
	return cmd
}

func buildDaemonStartCmd(cmdFactory func() *cobra.Command, version string) *cobra.Command {
	var addr string
	var chromeAddr string
	var proxyAddr string
	var interactive bool
	var safe bool
	var debug bool
	var foreground bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the daemon listener",
		Long:  "Start the daemon on the host. AI agents inside devcontainers connect to this daemon to execute commands with the host's credentials.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if interactive && !foreground && os.Getenv(daemonChildEnv) == "" {
				return fmt.Errorf("--interactive requires --foreground (needs stdin)")
			}

			if foreground || os.Getenv(daemonChildEnv) != "" {
				return runDaemonForeground(cmd, addr, chromeAddr, proxyAddr, interactive, safe, debug, cmdFactory, version)
			}
			return runDaemonBackground(cmd, addr, chromeAddr, proxyAddr, safe, debug)
		},
	}

	cmd.Flags().StringVar(&addr, "addr", ":19285", "Listen address (host:port)")
	cmd.Flags().StringVar(&chromeAddr, "chrome-addr", ":19286", "Chrome proxy listen address (host:port)")
	cmd.Flags().StringVar(&proxyAddr, "proxy-addr", ":19287", "HTTPS proxy listen address (host:port)")
	cmd.Flags().BoolVar(&interactive, "interactive", false, "Prompt for unknown domains instead of blocking them")
	cmd.Flags().BoolVar(&safe, "safe", os.Getenv("HUMAN_SAFE") == "1", "Block destructive operations for all daemon requests")
	cmd.Flags().BoolVar(&debug, "debug", false, "Enable debug logging")
	cmd.Flags().BoolVar(&foreground, "foreground", false, "Run in foreground (don't daemonize)")
	return cmd
}

// runDaemonForeground runs the daemon in the current process (blocking).
// It writes a PID file on start and removes it on shutdown.
func runDaemonForeground(cmd *cobra.Command, addr, chromeAddr, proxyAddr string, interactive, safe, debug bool, cmdFactory func() *cobra.Command, version string) error {
	_ = version // reserved for future use

	token, err := daemon.LoadOrCreateToken()
	if err != nil {
		return fmt.Errorf("failed to load/create token: %w", err)
	}

	if err := WritePidFile(os.Getpid()); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	defer RemovePidFile()

	out := cmd.OutOrStdout()
	hostIP := resolveHostIP()
	daemonAddr := replaceHost(addr, hostIP)
	chromeFullAddr := replaceHost(chromeAddr, hostIP)

	proxyFullAddr := replaceHost(proxyAddr, hostIP)

	info := daemon.DaemonInfo{
		Addr:       daemonAddr,
		ChromeAddr: chromeFullAddr,
		ProxyAddr:  proxyFullAddr,
		Token:      token,
		PID:        os.Getpid(),
	}
	if err := daemon.WriteInfo(info); err != nil {
		return fmt.Errorf("failed to write daemon info: %w", err)
	}
	defer daemon.RemoveInfo()

	_, _ = fmt.Fprintln(out, "Token:", token)
	_, _ = fmt.Fprintln(out, "Token file:", daemon.TokenPath())
	_, _ = fmt.Fprintln(out, "Listening on:", addr)
	_, _ = fmt.Fprintln(out, "Chrome proxy on:", chromeAddr)
	_, _ = fmt.Fprintln(out, "HTTPS proxy on:", proxyAddr)
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

	connTracker := daemon.NewConnectedTracker()

	hookStore := daemon.NewHookEventStore()

	srv := &daemon.Server{
		Addr:          addr,
		Token:         token,
		SafeMode:      safe,
		CmdFactory:    cmdFactory,
		Logger:        logger,
		ConnectedPIDs: connTracker,
		HookEvents:    hookStore,
	}

	// Start socket relay to accept Chrome native messaging
	// connections directly (harmless, may be useful later).
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

	// Chrome proxy: spawn claude --claude-in-chrome-mcp on the host
	// and translate between 4-byte LE socket framing and JSON-RPC stdio.
	claudePath, lookErr := exec.LookPath("claude")
	if lookErr != nil {
		logger.Warn().Err(lookErr).Msg("claude not found in PATH, chrome proxy will fail on connection")
	}

	chromeSrv := &chrome.Server{
		Addr:  chromeAddr,
		Token: token,
		Translator: &chrome.McpTranslator{
			ClaudePath: claudePath,
			Logger:     logger,
		},
		Logger: logger,
	}

	go func() {
		if err := chromeSrv.ListenAndServe(ctx); err != nil {
			logger.Error().Err(err).Msg("chrome proxy server failed")
		}
	}()

	proxySrv, proxyStatus, proxyErr := buildProxyServer(proxyAddr, interactive, logger)
	if proxyErr != nil {
		return proxyErr
	}
	if proxyStatus != "" {
		_, _ = fmt.Fprintln(out, proxyStatus)
	}

	go func() {
		if err := proxySrv.ListenAndServe(ctx); err != nil {
			logger.Error().Err(err).Msg("https proxy failed")
		}
	}()

	// Periodically write daemon stats (proxy connections + connected PIDs) for the TUI.
	statsPath := proxy.StatsPath()
	connectedPath := daemon.ConnectedPath()
	go writeDaemonStats(ctx, proxySrv, connTracker, statsPath, connectedPath)
	defer proxy.RemoveStats(statsPath)
	defer daemon.RemoveConnected(connectedPath)

	// Start FUSE .env filter (Linux only; no-op on other platforms)
	cwd, _ := os.Getwd()
	if unmount := fuseMount(cwd, safe, logger); unmount != nil {
		defer unmount()
	}

	// Start Slack notifier (if configured).
	slackNotifier, slackStatus := startSlackNotifier(logger)
	if slackStatus != "" {
		_, _ = fmt.Fprintln(out, "Slack notifications:", slackStatus)
	}

	// Start Telegram dispatch loop (if configured).
	telegramStatus := startTelegramDispatcher(ctx, logger, slackNotifier)
	_, _ = fmt.Fprintln(out, "Telegram dispatch:", telegramStatus)

	return srv.ListenAndServe(ctx)
}

// runDaemonBackground re-execs the current binary as a detached child process.
func runDaemonBackground(cmd *cobra.Command, addr, chromeAddr, proxyAddr string, safe, debug bool) error {
	out := cmd.OutOrStdout()

	// Check if already running.
	if pid, alive := ReadAlivePid(); alive {
		_, _ = fmt.Fprintf(out, "Daemon is already running (PID %d)\n", pid)
		return nil
	}

	logPath := DaemonLogPath()
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) // #nosec G304 -- logPath is built by DaemonLogPath(), not user input
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}

	exe, err := os.Executable()
	if err != nil {
		_ = logFile.Close()
		return fmt.Errorf("resolving executable path: %w", err)
	}

	args := []string{"daemon", "start", "--foreground",
		"--addr", addr,
		"--chrome-addr", chromeAddr,
		"--proxy-addr", proxyAddr,
	}
	if safe {
		args = append(args, "--safe")
	}
	if debug {
		args = append(args, "--debug")
	}

	child := exec.Command(exe, args...) // #nosec G204 -- re-exec of own binary via os.Executable()
	child.Env = append(os.Environ(), daemonChildEnv+"=1")
	child.Stderr = logFile
	child.Stdout = logFile
	child.SysProcAttr = detachSysProcAttr()

	if err := child.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("starting background process: %w", err)
	}
	_ = logFile.Close()

	pid := child.Process.Pid

	// Detach so we don't wait for the child.
	_ = child.Process.Release()

	// Poll for TCP readiness (up to 3s).
	const (
		pollInterval = 50 * time.Millisecond
		pollTimeout  = 3 * time.Second
	)
	deadline := time.Now().Add(pollTimeout)
	ready := false
	for time.Now().Before(deadline) {
		conn, dialErr := net.DialTimeout("tcp", "localhost"+addr, 200*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			ready = true
			break
		}
		time.Sleep(pollInterval)
	}

	hostIP := resolveHostIP()
	daemonAddr := replaceHost(addr, hostIP)

	if !ready {
		_, _ = fmt.Fprintf(out, "Daemon started (PID %d) but not yet reachable\n", pid)
		_, _ = fmt.Fprintf(out, "  Log: %s\n", logPath)
		return nil
	}

	token, _ := daemon.LoadOrCreateToken()
	chromeFullAddr := replaceHost(chromeAddr, hostIP)
	proxyFullAddr := replaceHost(proxyAddr, hostIP)

	_, _ = fmt.Fprintf(out, "Daemon started (PID %d)\n", pid)
	_, _ = fmt.Fprintln(out, "  Listening on:", daemonAddr)
	_, _ = fmt.Fprintf(out, "  Log: %s\n", logPath)
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Run in the container:")
	_, _ = fmt.Fprintf(out, "  export HUMAN_DAEMON_ADDR=%s HUMAN_DAEMON_TOKEN=%s HUMAN_CHROME_ADDR=%s HUMAN_PROXY_ADDR=%s\n",
		daemonAddr, token, chromeFullAddr, proxyFullAddr)
	return nil
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
			out := cmd.OutOrStdout()
			pid, pidAlive := ReadAlivePid()

			if !cmd.Flags().Changed("addr") {
				if info, err := daemon.ReadInfo(); err == nil {
					addr = info.Addr
				}
			}

			conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
			if err != nil {
				if pidAlive {
					_, _ = fmt.Fprintf(out, "Daemon is running (PID %d) but not reachable at %s\n", pid, addr)
				} else {
					_, _ = fmt.Fprintln(out, "Daemon is not running")
				}
				return fmt.Errorf("cannot connect to daemon: %w", err)
			}
			_ = conn.Close()

			if pidAlive {
				_, _ = fmt.Fprintf(out, "Daemon is running (PID %d) and reachable at %s\n", pid, addr)
			} else {
				_, _ = fmt.Fprintln(out, "Daemon is reachable at", addr)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&addr, "addr", "localhost:19285", "Daemon address to check")
	return cmd
}

func buildDaemonStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop a running daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			pid, alive := ReadAlivePid()
			if !alive {
				_, _ = fmt.Fprintln(out, "Daemon is not running")
				RemovePidFile()
				daemon.RemoveInfo()
				return nil
			}

			_, _ = fmt.Fprintf(out, "Stopping daemon (PID %d)...\n", pid)
			if err := stopProcess(pid); err != nil {
				return fmt.Errorf("failed to stop daemon: %w", err)
			}

			// Poll for exit (up to 5s).
			const (
				pollInterval = 100 * time.Millisecond
				pollTimeout  = 5 * time.Second
			)
			deadline := time.Now().Add(pollTimeout)
			for time.Now().Before(deadline) {
				if !isProcessAlive(pid) {
					break
				}
				time.Sleep(pollInterval)
			}

			if isProcessAlive(pid) {
				return fmt.Errorf("daemon (PID %d) did not exit within timeout", pid)
			}

			RemovePidFile()
			daemon.RemoveInfo()
			_, _ = fmt.Fprintln(out, "Daemon stopped")
			return nil
		},
	}
}

// --- PID file helpers ---

// DaemonLogPath returns the path to the daemon log file.
func DaemonLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".human", "daemon.log")
	}
	dir := filepath.Join(home, ".human")
	_ = os.MkdirAll(dir, 0o750)
	return filepath.Join(dir, "daemon.log")
}

// DaemonPidPath returns the path to the daemon PID file.
func DaemonPidPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".human", "daemon.pid")
	}
	dir := filepath.Join(home, ".human")
	_ = os.MkdirAll(dir, 0o750)
	return filepath.Join(dir, "daemon.pid")
}

// WritePidFile writes the PID to the PID file.
func WritePidFile(pid int) error {
	return os.WriteFile(DaemonPidPath(), []byte(strconv.Itoa(pid)), 0o600)
}

// RemovePidFile removes the PID file.
func RemovePidFile() {
	_ = os.Remove(DaemonPidPath())
}

// ReadAlivePid reads the PID file and checks if the process is alive.
// Returns (0, false) if no PID file exists or the process is dead.
func ReadAlivePid() (int, bool) {
	data, err := os.ReadFile(DaemonPidPath()) // #nosec G304 -- path is built by DaemonPidPath(), not user input
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	if !isProcessAlive(pid) {
		return pid, false
	}
	return pid, true
}

// resolveHostIP returns the preferred outbound IP of the host.
// Falls back to "localhost" if detection fails.
func resolveHostIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "localhost"
	}
	defer func() { _ = conn.Close() }()

	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return "localhost"
	}
	return addr.IP.String()
}

// startTelegramDispatcher starts the Telegram dispatch loop if a Telegram
// instance is configured. It runs as a background goroutine and returns
// a human-readable status string for the startup banner.
func startTelegramDispatcher(ctx context.Context, logger zerolog.Logger, extraNotifier dispatch.Notifier) string {
	configs, cfgErr := telegram.LoadConfigs(".")
	if cfgErr != nil {
		logger.Warn().Err(cfgErr).Msg("failed to load Telegram config, dispatch disabled")
		return "error loading config"
	}
	if len(configs) == 0 {
		return "not configured (add telegrams: to .humanconfig)"
	}

	instances, err := telegram.LoadInstances(".")
	if err != nil {
		logger.Warn().Err(err).Msg("failed to build Telegram instances")
		return "error loading config"
	}
	if len(instances) == 0 {
		names := make([]string, len(configs))
		for i, c := range configs {
			names[i] = c.Name
		}
		logger.Warn().Strs("instances", names).Msg("Telegram configured but token missing — set TELEGRAM_<NAME>_TOKEN")
		return fmt.Sprintf("missing token (set TELEGRAM_%s_TOKEN)", strings.ToUpper(configs[0].Name))
	}

	inst := instances[0]
	runner := claude.OSCommandRunner{}
	homeDir, _ := os.UserHomeDir()

	d := &dispatch.Dispatcher{
		Source: &dispatch.TelegramSource{
			Client:       inst.Client,
			AllowedUsers: inst.AllowedUsers,
		},
		Finder: &dispatch.TmuxAgentFinder{
			InstanceFinder: &claude.HostFinder{Runner: runner, HomeDir: homeDir},
			TmuxClient:     &claude.OSTmuxClient{Runner: runner},
			ProcessLister:  &claude.OSProcessLister{Runner: runner},
		},
		Sender:   &dispatch.TmuxSender{Runner: runner},
		Notifier: buildNotifier(&dispatch.TelegramNotifier{Client: inst.Client}, extraNotifier),
		Config:   dispatch.Config{PollInterval: dispatch.DefaultPollInterval},
		Logger:   logger,
	}

	go func() {
		if err := d.Run(ctx); err != nil {
			logger.Error().Err(err).Msg("telegram dispatcher failed")
		}
	}()

	logger.Info().Str("telegram", inst.Name).Msg("telegram dispatch enabled")
	return fmt.Sprintf("enabled (%s)", inst.Name)
}

// startSlackNotifier creates a Slack notifier if configured.
// Returns (nil, "") when Slack is not configured (no error — it is optional).
func startSlackNotifier(logger zerolog.Logger) (dispatch.Notifier, string) {
	configs, cfgErr := slack.LoadConfigs(".")
	if cfgErr != nil {
		logger.Warn().Err(cfgErr).Msg("failed to load Slack config, notifications disabled")
		return nil, "error loading config"
	}
	if len(configs) == 0 {
		return nil, ""
	}

	instances, err := slack.LoadInstances(".")
	if err != nil {
		logger.Warn().Err(err).Msg("failed to build Slack instances")
		return nil, "error loading config"
	}
	if len(instances) == 0 {
		logger.Warn().Str("instance", configs[0].Name).Msg("Slack configured but token missing")
		return nil, fmt.Sprintf("missing token (set SLACK_%s_TOKEN)", strings.ToUpper(configs[0].Name))
	}

	inst := instances[0]
	logger.Info().Str("slack", inst.Name).Msg("slack notifications enabled")
	return &dispatch.SlackNotifier{Client: inst.Client}, fmt.Sprintf("enabled (%s)", inst.Name)
}

// buildNotifier wraps a primary notifier with an optional extra notifier.
func buildNotifier(primary dispatch.Notifier, extra dispatch.Notifier) dispatch.Notifier {
	if extra == nil {
		return primary
	}
	return &dispatch.CompositeNotifier{Notifiers: []dispatch.Notifier{primary, extra}}
}

// writeDaemonStats periodically writes proxy stats and connected PIDs to disk for the TUI.
func writeDaemonStats(ctx context.Context, proxySrv *proxy.Server, tracker *daemon.ConnectedTracker, proxyPath, connectedPath string) {
	const connectedTTL = 30 * time.Second
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = proxy.WriteStats(proxyPath, proxy.Stats{ActiveConns: proxySrv.ActiveConns()})
			tracker.Prune(connectedTTL)
			_ = daemon.WriteConnected(connectedPath, tracker.PIDs())
		}
	}
}

// buildProxyServer creates the HTTPS proxy server with policy and optional
// MITM interceptor. Returns a status string for the startup banner.
func buildProxyServer(addr string, interactive bool, logger zerolog.Logger) (*proxy.Server, string, error) {
	proxyCfg, _ := proxy.LoadConfig(".")

	var policy proxy.Decider
	var err error
	if proxyCfg != nil {
		policy, err = proxy.NewPolicy(proxyCfg.Mode, proxyCfg.Domains)
		if err != nil {
			return nil, "", fmt.Errorf("invalid proxy policy: %w", err)
		}
	} else {
		policy = proxy.BlockAllPolicy()
	}

	var status string
	if interactive {
		prompt := proxy.NewTerminalPrompt(os.Stdin, os.Stderr)
		policy = proxy.NewInteractiveDecider(policy, prompt)
		status = "Interactive proxy mode: unknown domains will prompt for approval\n"
	}

	interceptor, interceptStatus := buildInterceptor(proxyCfg, logger)
	if interceptStatus != "" {
		status += interceptStatus
	}

	srv := &proxy.Server{
		Addr:        addr,
		Policy:      policy,
		Interceptor: interceptor,
		Logger:      logger,
	}

	return srv, status, nil
}

// buildInterceptor creates a MITM logging interceptor if intercept domains
// are configured. Returns (nil, "") when not configured.
func buildInterceptor(proxyCfg *proxy.Config, logger zerolog.Logger) (proxy.Interceptor, string) {
	if proxyCfg == nil || len(proxyCfg.Intercept) == 0 {
		return nil, ""
	}

	home, _ := os.UserHomeDir()
	humanDir := filepath.Join(home, ".human")

	caCert, caKey, _, err := proxy.LoadOrCreateCA(humanDir)
	if err != nil {
		logger.Error().Err(err).Msg("failed to load/create CA, intercept disabled")
		return nil, "MITM intercept: disabled (CA error)"
	}

	logDir := filepath.Join(humanDir, "llm-traffic")
	interceptor := &proxy.LoggingInterceptor{
		Domains:   proxyCfg.Intercept,
		LeafCache: &proxy.LeafCache{CACert: caCert, CAKey: caKey},
		Logger:    logger,
		LogDir:    logDir,
	}

	return interceptor, fmt.Sprintf("MITM intercept: %v\n  CA cert: %s\n  Traffic logs: %s",
		proxyCfg.Intercept, filepath.Join(humanDir, "ca.crt"), logDir)
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
