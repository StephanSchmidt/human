package claude

import (
	"bytes"
	"context"
	"io"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// NewEngineDockerClient creates a DockerClient backed by the Docker Engine API.
func NewEngineDockerClient() (DockerClient, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &engineDockerClient{cli: cli}, nil
}

type engineDockerClient struct {
	cli *client.Client
}

func (e *engineDockerClient) ListContainers(ctx context.Context) ([]ContainerInfo, error) {
	list, err := e.cli.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return nil, err
	}
	infos := make([]ContainerInfo, 0, len(list))
	for _, c := range list {
		name := ""
		if len(c.Names) > 0 {
			// Docker container names start with "/".
			name = c.Names[0]
			if len(name) > 0 && name[0] == '/' {
				name = name[1:]
			}
		}
		infos = append(infos, ContainerInfo{ID: c.ID, Name: name})
	}
	return infos, nil
}

func (e *engineDockerClient) Exec(ctx context.Context, containerID string, cmd []string) (int, io.Reader, error) {
	execCfg := container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	}
	resp, err := e.cli.ContainerExecCreate(ctx, containerID, execCfg)
	if err != nil {
		return 0, nil, err
	}

	attach, err := e.cli.ContainerExecAttach(ctx, resp.ID, container.ExecStartOptions{})
	if err != nil {
		return 0, nil, err
	}

	// Demultiplex stdout/stderr from the Docker stream.
	var stdout bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, io.Discard, attach.Reader); err != nil {
		attach.Close()
		return 0, nil, err
	}
	attach.Close()

	inspect, err := e.cli.ContainerExecInspect(ctx, resp.ID)
	if err != nil {
		return 0, nil, err
	}

	return inspect.ExitCode, &stdout, nil
}

func (e *engineDockerClient) Close() error {
	return e.cli.Close()
}

// Verify interface compliance.
var _ DockerClient = (*engineDockerClient)(nil)
