package cmddevcontainer

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/errors"
	"github.com/StephanSchmidt/human/internal/daemon"
	"github.com/StephanSchmidt/human/internal/devcontainer"
)

func buildUpCmd() *cobra.Command {
	var rebuild bool
	var noDaemon bool

	cmd := &cobra.Command{
		Use:   "up [project-dir]",
		Short: "Build and start a devcontainer",
		Long: `Build the devcontainer image (if needed), create and start the container,
and inject daemon connectivity automatically. Starts the daemon if not running.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectDir := "."
			if len(args) > 0 {
				projectDir = args[0]
			}

			out := cmd.OutOrStdout()
			logger := zerolog.New(zerolog.ConsoleWriter{Out: cmd.ErrOrStderr()}).With().Timestamp().Logger()

			// Ensure daemon is running (unless --no-daemon).
			var daemonInfo *daemon.DaemonInfo
			if !noDaemon {
				info, err := ensureDaemon(projectDir)
				if err != nil {
					logger.Warn().Err(err).Msg("daemon auto-start failed, continuing without daemon")
				} else {
					daemonInfo = info
				}
			}

			docker, err := devcontainer.NewDockerClient()
			if err != nil {
				return errors.WrapWithDetails(err, "connecting to Docker")
			}
			defer func() { _ = docker.Close() }()

			mgr := &devcontainer.Manager{Docker: docker, Logger: logger}
			_, err = mgr.Up(cmd.Context(), devcontainer.UpOptions{
				ProjectDir: projectDir,
				Rebuild:    rebuild,
				DaemonInfo: daemonInfo,
				Out:        out,
			})
			return err
		},
	}

	cmd.Flags().BoolVar(&rebuild, "rebuild", false, "Force image rebuild even if cached")
	cmd.Flags().BoolVar(&noDaemon, "no-daemon", false, "Skip auto-starting the daemon")
	return cmd
}

// ensureDaemon checks if the daemon is running and starts it if not.
// Returns the DaemonInfo on success.
func ensureDaemon(projectDir string) (*daemon.DaemonInfo, error) {
	info, err := daemon.ReadInfo()
	if err == nil && info.IsReachable() {
		return &info, nil
	}

	// Start daemon in background by re-execing the current binary.
	humanExe, err := os.Executable()
	if err != nil {
		return nil, errors.WrapWithDetails(err, "resolving executable path")
	}

	child := exec.Command(humanExe, "daemon", "start", "--project", projectDir) // #nosec G204 -- re-exec of own binary
	child.Stdout = os.Stderr
	child.Stderr = os.Stderr

	if err := child.Run(); err != nil {
		return nil, errors.WrapWithDetails(err, "starting daemon")
	}

	// Poll for TCP readiness.
	const (
		pollInterval = 50 * time.Millisecond
		pollTimeout  = 3 * time.Second
	)
	deadline := time.Now().Add(pollTimeout)
	addr := fmt.Sprintf("localhost:%d", daemon.DefaultPort)
	for time.Now().Before(deadline) {
		conn, dialErr := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			break
		}
		time.Sleep(pollInterval)
	}

	readInfo, err := daemon.ReadInfo()
	if err != nil {
		return nil, errors.WrapWithDetails(err, "reading daemon info after start")
	}
	return &readInfo, nil
}
