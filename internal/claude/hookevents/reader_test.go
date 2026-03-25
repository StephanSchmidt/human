package hookevents

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileEventReader_FileNotFound(t *testing.T) {
	r := &FileEventReader{Path: "/nonexistent/events.jsonl"}
	data, err := r.ReadEvents(context.Background())
	assert.NoError(t, err)
	assert.Nil(t, data)
}

func TestFileEventReader_ReadsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	content := []byte(`{"event":"Stop","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:00Z"}`)
	require.NoError(t, os.WriteFile(path, content, 0o644))

	r := &FileEventReader{Path: path}
	data, err := r.ReadEvents(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, content, data)
}

// mockExecer implements DockerExecer for testing.
type mockExecer struct {
	exitCode int
	output   []byte
	err      error
}

func (m *mockExecer) Exec(_ context.Context, _ string, _ []string) (int, io.Reader, error) {
	if m.err != nil {
		return 0, nil, m.err
	}
	return m.exitCode, bytes.NewReader(m.output), nil
}

func TestDockerEventReader_FileNotFound(t *testing.T) {
	r := &DockerEventReader{
		Client:      &mockExecer{exitCode: 1},
		ContainerID: "abc123def456xyz",
	}
	data, err := r.ReadEvents(context.Background())
	assert.NoError(t, err)
	assert.Nil(t, data)
}

func TestDockerEventReader_ReadsEvents(t *testing.T) {
	content := []byte(`{"event":"UserPromptSubmit","session_id":"s1","cwd":"/proj","timestamp":"2026-03-25T10:00:00Z"}`)
	r := &DockerEventReader{
		Client:      &mockExecer{exitCode: 0, output: content},
		ContainerID: "abc123def456xyz",
	}
	data, err := r.ReadEvents(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, content, data)
}

func TestDockerEventReader_ExecError(t *testing.T) {
	r := &DockerEventReader{
		Client:      &mockExecer{err: assert.AnError},
		ContainerID: "abc123def456xyz",
	}
	data, err := r.ReadEvents(context.Background())
	assert.Error(t, err)
	assert.Nil(t, data)
}
