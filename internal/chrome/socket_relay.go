package chrome

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"

	"github.com/rs/zerolog"

	"github.com/StephanSchmidt/human/errors"
)

const pendingBuffer = 16

// SocketRelay implements ProcessSpawner by creating a Unix socket and accepting
// connections from Chrome's native messaging host. When Spawn is called (by
// ForwardProxy via chrome.Server), it dequeues a waiting Chrome connection and
// returns it as stdin/stdout, pairing it with the bridge connection.
type SocketRelay struct {
	SocketDir string
	Logger    zerolog.Logger
	pending   chan net.Conn
}

// NewSocketRelay creates a SocketRelay with a buffered pending channel.
func NewSocketRelay(socketDir string, logger zerolog.Logger) *SocketRelay {
	return &SocketRelay{
		SocketDir: socketDir,
		Logger:    logger,
		pending:   make(chan net.Conn, pendingBuffer),
	}
}

// ListenAndServe creates a Unix socket in SocketDir and accepts connections,
// queuing them in the pending channel. It blocks until ctx is cancelled.
func (r *SocketRelay) ListenAndServe(ctx context.Context) error {
	if err := os.MkdirAll(r.SocketDir, 0o700); err != nil {
		return errors.WrapWithDetails(err, "creating socket directory",
			"dir", r.SocketDir)
	}

	sockPath := filepath.Join(r.SocketDir, fmt.Sprintf("%d.sock", os.Getpid()))

	// Remove stale socket file if it exists.
	_ = os.Remove(sockPath)

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return errors.WrapWithDetails(err, "socket relay listen failed",
			"path", sockPath)
	}
	defer func() {
		_ = ln.Close()
		_ = os.Remove(sockPath)
	}()

	r.Logger.Info().Str("path", sockPath).Msg("socket relay listening")

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, aErr := ln.Accept()
		if aErr != nil {
			if ctx.Err() != nil {
				r.drainPending()
				return nil
			}
			r.Logger.Warn().Err(aErr).Msg("socket relay accept error")
			continue
		}
		if conn == nil {
			continue // satisfy nilaway; Accept never returns nil without error
		}
		r.Logger.Debug().Msg("chrome native host connected to relay")

		select {
		case <-ctx.Done():
			_ = conn.Close()
			r.drainPending()
			return nil
		case r.pending <- conn:
		}
	}
}

// Spawn implements ProcessSpawner. It blocks until a Chrome native messaging
// connection is available (or ctx is cancelled) and returns it as stdin/stdout.
func (r *SocketRelay) Spawn(ctx context.Context) (io.WriteCloser, io.ReadCloser, func() error, error) {
	select {
	case conn := <-r.pending:
		r.Logger.Info().Msg("paired chrome connection with bridge")
		wc := &connWriteCloser{conn: conn}
		rc := &connReadCloser{conn: conn}
		wait := func() error {
			return conn.Close()
		}
		return wc, rc, wait, nil
	case <-ctx.Done():
		return nil, nil, nil, errors.WrapWithDetails(ctx.Err(), "waiting for chrome connection")
	}
}

// drainPending closes all queued connections.
func (r *SocketRelay) drainPending() {
	for {
		select {
		case conn := <-r.pending:
			if conn != nil {
				_ = conn.Close()
			}
		default:
			return
		}
	}
}
