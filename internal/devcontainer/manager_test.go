package devcontainer

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/StephanSchmidt/human/internal/daemon"
)

// setupTestProject creates a temp project dir with a devcontainer.json.
func setupTestProject(t *testing.T, configJSON string) (string, *mockDockerClient, *pullThenInspectMock) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	projectDir := filepath.Join(tmp, "myproject")
	dcDir := filepath.Join(projectDir, ".devcontainer")
	if err := os.MkdirAll(dcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dcDir, "devcontainer.json"), []byte(configJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := &mockDockerClient{
		imageInspectErr:    fmt.Errorf("not found"),
		imageInspectResult: ImageInspectResponse{ID: "sha256:pulled"},
		createID:           "container-abc123",
		inspectState:       ContainerState{Running: true, Status: "running"},
	}
	callCount := 0
	docker := &pullThenInspectMock{
		mockDockerClient: mock,
		inspectCallCount: &callCount,
		inspectErr:       fmt.Errorf("not found"),
		inspectResult:    ImageInspectResponse{ID: "sha256:pulled", Tags: []string{"ubuntu:22.04"}},
	}
	return projectDir, mock, docker
}

func TestManager_Up_NewContainer(t *testing.T) {
	projectDir, mock, docker := setupTestProject(t, `{"name": "test", "image": "ubuntu:22.04", "remoteUser": "vscode"}`)

	mgr := &Manager{Docker: docker, Logger: testLogger()}
	var buf bytes.Buffer
	meta, err := mgr.Up(context.Background(), UpOptions{
		ProjectDir: projectDir,
		Out:        &buf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if meta == nil {
		t.Fatal("expected non-nil meta")
	}
	if meta.Status != StatusRunning {
		t.Errorf("status = %q, want %q", meta.Status, StatusRunning)
	}
	if meta.ContainerID != "container-abc123" {
		t.Errorf("containerID = %q", meta.ContainerID)
	}
	if meta.RemoteUser != "vscode" {
		t.Errorf("remoteUser = %q", meta.RemoteUser)
	}

	verifyContainerCreate(t, mock, projectDir)
	verifyMetaPersisted(t, meta.Name)

	if !strings.Contains(buf.String(), "Devcontainer running") {
		t.Errorf("output should contain success message: %s", buf.String())
	}
}

func verifyContainerCreate(t *testing.T, mock *mockDockerClient, projectDir string) {
	t.Helper()
	if len(mock.createCalls) != 1 {
		t.Fatalf("expected 1 create call, got %d", len(mock.createCalls))
	}
	create := mock.createCalls[0]
	if create.Name != ContainerName(projectDir) {
		t.Errorf("container name = %q", create.Name)
	}
	if create.Labels[LabelManaged] != "true" {
		t.Error("missing managed label")
	}
	if create.Labels[LabelProject] != projectDir {
		t.Errorf("project label = %q", create.Labels[LabelProject])
	}
	if len(mock.startCalls) != 1 {
		t.Errorf("expected 1 start call, got %d", len(mock.startCalls))
	}
}

func verifyMetaPersisted(t *testing.T, name string) {
	t.Helper()
	persisted, err := ReadMeta(name)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ContainerID != "container-abc123" {
		t.Errorf("persisted containerID = %q", persisted.ContainerID)
	}
}

func TestManager_Up_DaemonInjection(t *testing.T) {
	projectDir, mock, docker := setupTestProject(t, `{"image": "ubuntu"}`)

	mgr := &Manager{Docker: docker, Logger: testLogger()}
	daemonInfo := &daemon.DaemonInfo{
		Addr:  "192.168.1.5:19285",
		Token: "secret-token",
	}
	_, err := mgr.Up(context.Background(), UpOptions{
		ProjectDir: projectDir,
		DaemonInfo: daemonInfo,
		Out:        &bytes.Buffer{},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(mock.createCalls) != 1 {
		t.Fatalf("expected 1 create call, got %d", len(mock.createCalls))
	}
	env := mock.createCalls[0].Env
	found := map[string]bool{}
	for _, e := range env {
		if strings.HasPrefix(e, "HUMAN_DAEMON_TOKEN=") {
			found["token"] = true
			if !strings.Contains(e, "secret-token") {
				t.Errorf("daemon token not injected: %s", e)
			}
		}
		if strings.HasPrefix(e, "HUMAN_DAEMON_ADDR=") {
			found["addr"] = true
		}
		if strings.HasPrefix(e, "BROWSER=") {
			found["browser"] = true
		}
	}
	if !found["token"] || !found["addr"] || !found["browser"] {
		t.Errorf("missing daemon env vars: %v", found)
	}
}

func TestManager_Stop(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := WriteMeta(Meta{
		Name:        "mydc",
		ContainerID: "abc123",
		Status:      StatusRunning,
		CreatedAt:   time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	mock := &mockDockerClient{}
	mgr := &Manager{Docker: mock, Logger: testLogger()}
	if err := mgr.Stop(context.Background(), "mydc"); err != nil {
		t.Fatal(err)
	}

	if len(mock.stopCalls) != 1 || mock.stopCalls[0] != "abc123" {
		t.Errorf("stop calls = %v", mock.stopCalls)
	}

	meta, err := ReadMeta("mydc")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Status != StatusStopped {
		t.Errorf("status = %q, want stopped", meta.Status)
	}
}

func TestManager_Down(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := WriteMeta(Meta{
		Name:        "mydc",
		ContainerID: "abc123",
		Status:      StatusRunning,
		CreatedAt:   time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	mock := &mockDockerClient{}
	mgr := &Manager{Docker: mock, Logger: testLogger()}
	if err := mgr.Down(context.Background(), "mydc", false); err != nil {
		t.Fatal(err)
	}

	if len(mock.removeCalls) != 1 || mock.removeCalls[0] != "abc123" {
		t.Errorf("remove calls = %v", mock.removeCalls)
	}

	_, err := ReadMeta("mydc")
	if err == nil {
		t.Error("metadata should be deleted after down")
	}
}

func TestManager_List(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	for _, name := range []string{"dc-a", "dc-b"} {
		if err := WriteMeta(Meta{
			Name:        name,
			ContainerID: name + "-id",
			Status:      StatusRunning,
			CreatedAt:   time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	mock := &mockDockerClient{
		inspectState: ContainerState{Running: true, Status: "running"},
	}
	mgr := &Manager{Docker: mock, Logger: testLogger()}
	metas, err := mgr.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 2 {
		t.Errorf("expected 2 metas, got %d", len(metas))
	}
}

func TestManager_Exec(t *testing.T) {
	mock := &mockDockerClient{}
	mgr := &Manager{Docker: mock, Logger: testLogger()}

	var stdout, stderr bytes.Buffer
	exitCode, err := mgr.Exec(context.Background(), "container-id", []string{"echo", "hello"}, "vscode", nil, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if exitCode != 0 {
		t.Errorf("exit code = %d", exitCode)
	}
	if len(mock.execCalls) != 1 {
		t.Errorf("expected 1 exec call, got %d", len(mock.execCalls))
	}
	call := mock.execCalls[0]
	if call.ContainerID != "container-id" {
		t.Errorf("container = %q", call.ContainerID)
	}
	if call.Opts.User != "vscode" {
		t.Errorf("user = %q", call.Opts.User)
	}
}

func TestParseRunArgs(t *testing.T) {
	opts := &ContainerCreateOptions{}
	args := []string{
		"--add-host=myhost:10.0.0.1",
		"--cap-add", "SYS_PTRACE",
		"--privileged",
		"--network=host",
		"--security-opt=seccomp=unconfined",
		"--unknown-flag",
	}
	ParseRunArgs(args, opts, testLogger())

	if len(opts.ExtraHosts) != 1 || opts.ExtraHosts[0] != "myhost:10.0.0.1" {
		t.Errorf("ExtraHosts = %v", opts.ExtraHosts)
	}
	if len(opts.CapAdd) != 1 || opts.CapAdd[0] != "SYS_PTRACE" {
		t.Errorf("CapAdd = %v", opts.CapAdd)
	}
	if !opts.Privileged {
		t.Error("expected Privileged = true")
	}
	if opts.NetworkMode != "host" {
		t.Errorf("NetworkMode = %q", opts.NetworkMode)
	}
	if len(opts.SecurityOpt) != 1 || opts.SecurityOpt[0] != "seccomp=unconfined" {
		t.Errorf("SecurityOpt = %v", opts.SecurityOpt)
	}
}
