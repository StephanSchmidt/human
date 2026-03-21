package claude

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

// Instance represents a discovered Claude Code instance.
type Instance struct {
	Label       string      // e.g. "Host (PID 7046)" or `Container "dev-myapp" (abc123)`
	Source      string      // "host" or "container"
	Walker      DirWalker   // how to read its JSONL data
	StateReader StateReader // determines busy/ready state
	Root        string      // JSONL root path (or virtual path for containers)
}

// InstanceFinder discovers running Claude Code instances.
type InstanceFinder interface {
	FindInstances(ctx context.Context) ([]Instance, error)
}

// CommandRunner abstracts running external commands for testability.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// OSCommandRunner implements CommandRunner using os/exec.
type OSCommandRunner struct{}

func (OSCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output() // #nosec G204 — only called with hardcoded commands
}

// DockerClient abstracts Docker operations for testability.
type DockerClient interface {
	ListContainers(ctx context.Context) ([]ContainerInfo, error)
	Exec(ctx context.Context, containerID string, cmd []string) (int, io.Reader, error)
	Close() error
}

// ContainerInfo holds minimal container metadata.
type ContainerInfo struct {
	ID   string
	Name string
}

// HostFinder discovers Claude Code instances on the local host via pgrep.
type HostFinder struct {
	Runner  CommandRunner
	HomeDir string // override for testing; empty uses os.UserHomeDir result passed externally
}

func (h *HostFinder) FindInstances(ctx context.Context) ([]Instance, error) {
	out, err := h.Runner.Run(ctx, "pgrep", "-a", "claude")
	if err != nil {
		// pgrep exits 1 when no matches — not an error for us.
		return nil, nil
	}

	seen := make(map[string]bool)
	var instances []Instance

	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			continue
		}
		pid := parts[0]
		cmdLine := parts[1]

		// Extract the basename of the first token in the command.
		cmdParts := strings.Fields(cmdLine)
		if len(cmdParts) == 0 {
			continue
		}
		base := filepath.Base(cmdParts[0])
		if base != "claude" {
			continue
		}

		// Validate PID is numeric.
		if _, err := strconv.Atoi(pid); err != nil {
			continue
		}

		root := filepath.Join(h.HomeDir, ".claude", "projects")
		if seen[root] {
			continue
		}
		seen[root] = true

		instances = append(instances, Instance{
			Label:       fmt.Sprintf("Host (PID %s)", pid),
			Source:      "host",
			Walker:      OSDirWalker{},
			StateReader: OSStateReader{},
			Root:        root,
		})
	}
	return instances, nil
}

// DockerFinder discovers Claude Code instances inside Docker containers.
type DockerFinder struct {
	Client DockerClient
}

func (d *DockerFinder) FindInstances(ctx context.Context) ([]Instance, error) {
	containers, err := d.Client.ListContainers(ctx)
	if err != nil {
		return nil, err
	}

	var instances []Instance
	for _, ctr := range containers {
		// Check if claude is running in this container.
		exitCode, _, err := d.Client.Exec(ctx, ctr.ID, []string{"pgrep", "-x", "claude"})
		if err != nil || exitCode != 0 {
			continue
		}

		// Fetch all JSONL content from the container.
		_, reader, err := d.Client.Exec(ctx, ctr.ID, []string{
			"sh", "-c",
			"find /root/.claude/projects /home -maxdepth 6 -name '*.jsonl' -exec cat {} + 2>/dev/null",
		})
		if err != nil {
			continue
		}

		data, err := io.ReadAll(reader)
		if err != nil {
			continue
		}

		shortID := ctr.ID
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}
		name := ctr.Name
		if name == "" {
			name = shortID
		}

		instances = append(instances, Instance{
			Label:       fmt.Sprintf("Container %q (%s)", name, shortID),
			Source:      "container",
			Walker:      &ByteWalker{Data: data},
			StateReader: &ByteStateReader{Data: data},
			Root:        "/container/" + shortID,
		})
	}
	return instances, nil
}

// CombinedFinder aggregates multiple InstanceFinders, logging and skipping failures.
type CombinedFinder struct {
	Finders []InstanceFinder
}

func (c *CombinedFinder) FindInstances(ctx context.Context) ([]Instance, error) {
	var all []Instance
	for _, f := range c.Finders {
		instances, err := f.FindInstances(ctx)
		if err != nil {
			log.Debug().Err(err).Msg("instance finder failed, skipping")
			continue
		}
		all = append(all, instances...)
	}
	return all, nil
}

// ByteWalker implements DirWalker over in-memory bytes (one JSONL line per text line).
type ByteWalker struct {
	Data []byte
}

func (b *ByteWalker) WalkJSONL(_ string, fn func(line []byte) error) error {
	scanner := bufio.NewScanner(bytes.NewReader(b.Data))
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if err := fn(line); err != nil {
			return err
		}
	}
	return nil
}
