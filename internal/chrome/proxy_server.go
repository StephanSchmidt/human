package chrome

import (
	"context"
	"io"
	"net"

	"github.com/rs/zerolog"
)

// ProcessSpawner creates a child process for the real Chrome native host.
type ProcessSpawner interface {
	Spawn(ctx context.Context) (stdin io.WriteCloser, stdout io.ReadCloser, wait func() error, err error)
}

// ForwardProxy bridges a TCP connection to a spawned native host process.
// It performs bidirectional io.Copy: conn→stdin and stdout→conn.
func ForwardProxy(ctx context.Context, conn net.Conn, spawner ProcessSpawner, logger zerolog.Logger) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stdin, stdout, wait, err := spawner.Spawn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = stdin.Close() }()

	errCh := make(chan error, 2) //nolint:mnd // two directions

	// conn → process stdin
	go func() {
		_, cpErr := io.Copy(stdin, conn)
		_ = stdin.Close()
		errCh <- cpErr
	}()

	// process stdout → conn
	go func() {
		_, cpErr := io.Copy(conn, stdout)
		errCh <- cpErr
	}()

	// Wait for first direction to finish.
	<-errCh
	cancel()

	waitErr := wait()
	if waitErr != nil {
		logger.Debug().Err(waitErr).Msg("native host process exited")
	}

	return nil
}
