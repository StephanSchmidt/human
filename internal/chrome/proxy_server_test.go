package chrome

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSpawner implements ProcessSpawner for testing.
type mockSpawner struct {
	stdinBuf  *bytes.Buffer
	stdoutBuf *bytes.Buffer
	waitErr   error
	spawnErr  error
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

type nopReadCloser struct {
	io.Reader
}

func (nopReadCloser) Close() error { return nil }

func (m *mockSpawner) Spawn(_ context.Context) (io.WriteCloser, io.ReadCloser, func() error, error) {
	if m.spawnErr != nil {
		return nil, nil, nil, m.spawnErr
	}
	return nopWriteCloser{m.stdinBuf}, nopReadCloser{m.stdoutBuf}, func() error { return m.waitErr }, nil
}

// blockingReader blocks until closed via its channel, then returns io.EOF.
type blockingReader struct {
	done chan struct{}
}

func (b *blockingReader) Read(_ []byte) (int, error) {
	<-b.done
	return 0, io.EOF
}

func TestForwardProxy_ConnToStdin(t *testing.T) {
	// Use a blocking reader for stdout so that direction doesn't finish first.
	blocker := &blockingReader{done: make(chan struct{})}
	spawner := &mockSpawner{
		stdinBuf: &bytes.Buffer{},
	}
	// Override the spawner to use the blocking reader for stdout.
	stdinBuf := &bytes.Buffer{}
	blockSpawner := &funcSpawner{
		fn: func(_ context.Context) (io.WriteCloser, io.ReadCloser, func() error, error) {
			return nopWriteCloser{stdinBuf}, nopReadCloser{blocker}, func() error {
				return spawner.waitErr
			}, nil
		},
	}

	server, client := net.Pipe()

	clientData := []byte("request from extension")
	go func() {
		_, _ = client.Write(clientData)
		_ = client.Close()
	}()

	logger := zerolog.Nop()

	done := make(chan error, 1)
	go func() {
		done <- ForwardProxy(context.Background(), server, blockSpawner, logger)
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		close(blocker.done)
		t.Fatal("ForwardProxy did not return")
	}

	close(blocker.done)
	assert.Equal(t, clientData, stdinBuf.Bytes())
}

// funcSpawner wraps a function as ProcessSpawner.
type funcSpawner struct {
	fn func(ctx context.Context) (io.WriteCloser, io.ReadCloser, func() error, error)
}

func (f *funcSpawner) Spawn(ctx context.Context) (io.WriteCloser, io.ReadCloser, func() error, error) {
	return f.fn(ctx)
}

func TestForwardProxy_StdoutToConn(t *testing.T) {
	processOutput := []byte("response from native host")
	spawner := &mockSpawner{
		stdinBuf:  &bytes.Buffer{},
		stdoutBuf: bytes.NewBuffer(processOutput),
	}

	server, client := net.Pipe()

	received := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(client)
		received <- data
	}()

	logger := zerolog.Nop()

	done := make(chan error, 1)
	go func() {
		done <- ForwardProxy(context.Background(), server, spawner, logger)
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("ForwardProxy did not return")
	}

	_ = server.Close()
	data := <-received
	_ = client.Close()
	assert.Equal(t, processOutput, data)
}

func TestForwardProxy_SpawnError(t *testing.T) {
	spawner := &mockSpawner{
		spawnErr: assert.AnError,
	}

	server, client := net.Pipe()
	defer func() {
		_ = server.Close()
		_ = client.Close()
	}()

	logger := zerolog.Nop()
	err := ForwardProxy(context.Background(), server, spawner, logger)

	require.Error(t, err)
}

func TestForwardProxy_ContextCancellation(t *testing.T) {
	blocker := &blockingReader{done: make(chan struct{})}
	blockSpawner := &funcSpawner{
		fn: func(_ context.Context) (io.WriteCloser, io.ReadCloser, func() error, error) {
			return nopWriteCloser{&bytes.Buffer{}}, nopReadCloser{blocker}, func() error { return nil }, nil
		},
	}

	server, client := net.Pipe()

	logger := zerolog.Nop()

	done := make(chan error, 1)
	go func() {
		done <- ForwardProxy(context.Background(), server, blockSpawner, logger)
	}()

	// Close client to unblock the conn→stdin direction.
	_ = client.Close()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		close(blocker.done)
		t.Fatal("ForwardProxy did not return after client close")
	}

	close(blocker.done)
}
