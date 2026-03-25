package hookevents

import (
	"context"
	"io"
	"os"

	"github.com/StephanSchmidt/human/errors"
)

// EventReader reads raw hook event bytes from an event source.
type EventReader interface {
	ReadEvents(ctx context.Context) ([]byte, error)
}

// DockerExecer is the subset of claude.DockerClient needed to read events.
type DockerExecer interface {
	Exec(ctx context.Context, containerID string, cmd []string) (int, io.Reader, error)
}

// FileEventReader reads events from a local file path.
type FileEventReader struct {
	Path string
}

func (r *FileEventReader) ReadEvents(_ context.Context) ([]byte, error) {
	data, err := os.ReadFile(r.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errors.WrapWithDetails(err, "reading hook events file", "path", r.Path)
	}
	return data, nil
}

// DockerEventReader reads events from a container via docker exec.
type DockerEventReader struct {
	Client      DockerExecer
	ContainerID string
}

func (r *DockerEventReader) ReadEvents(ctx context.Context) ([]byte, error) {
	// Use sh -c to resolve $HOME for /root vs /home/vscode in devcontainers.
	exitCode, reader, err := r.Client.Exec(ctx, r.ContainerID,
		[]string{"sh", "-c", `cat "$HOME/.claude/human-events/events.jsonl" 2>/dev/null`})
	if err != nil {
		return nil, errors.WrapWithDetails(err, "docker exec cat events", "container", r.ContainerID[:12])
	}
	if exitCode != 0 {
		return nil, nil // file doesn't exist yet
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "reading docker exec output", "container", r.ContainerID[:12])
	}
	return data, nil
}
