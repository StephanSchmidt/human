package devcontainer

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestEnsureImage_Cached(t *testing.T) {
	mock := &mockDockerClient{
		imageInspectResult: ImageInspectResponse{ID: "sha256:cached", Tags: []string{"human-dc-test:abc123abc123"}},
	}
	builder := &ImageBuilder{Docker: mock, Logger: testLogger()}

	id, name, err := builder.EnsureImage(context.Background(), &DevcontainerConfig{Image: "ubuntu"}, "/tmp/test", "abc123abc123def456", false, &strings.Builder{})
	if err != nil {
		t.Fatal(err)
	}
	if id != "sha256:cached" {
		t.Errorf("expected cached image ID, got %q", id)
	}
	if !strings.HasPrefix(name, "human-dc-") {
		t.Errorf("unexpected image name: %q", name)
	}
	// Should not have pulled.
	if len(mock.pullCalls) != 0 {
		t.Errorf("should not pull when cached, got %d pull calls", len(mock.pullCalls))
	}
}

func TestEnsureImage_PullOnMiss(t *testing.T) {
	mock := &mockDockerClient{
		imageInspectErr:    fmt.Errorf("not found"),
		imageInspectResult: ImageInspectResponse{ID: "sha256:pulled"},
	}
	// After pull, ImageInspect should succeed for the ref.
	callCount := 0
	origInspect := mock.imageInspectErr
	mock2 := &pullThenInspectMock{
		mockDockerClient: mock,
		inspectCallCount: &callCount,
		inspectErr:       origInspect,
		inspectResult:    ImageInspectResponse{ID: "sha256:pulled", Tags: []string{"ubuntu"}},
	}

	builder := &ImageBuilder{Docker: mock2, Logger: testLogger()}
	_, _, err := builder.EnsureImage(context.Background(), &DevcontainerConfig{Image: "ubuntu"}, "/tmp/test", "abc123abc123def456", false, &strings.Builder{})
	if err != nil {
		t.Fatal(err)
	}
	if len(mock.pullCalls) != 1 {
		t.Errorf("expected 1 pull call, got %d", len(mock.pullCalls))
	}
}

func TestEnsureImage_ForcedRebuild(t *testing.T) {
	mock := &mockDockerClient{
		imageInspectResult: ImageInspectResponse{ID: "sha256:cached"},
	}
	// Even with cached image, rebuild=true should pull.
	callCount := 0
	mock2 := &pullThenInspectMock{
		mockDockerClient: mock,
		inspectCallCount: &callCount,
		inspectResult:    ImageInspectResponse{ID: "sha256:fresh"},
	}

	builder := &ImageBuilder{Docker: mock2, Logger: testLogger()}
	id, _, err := builder.EnsureImage(context.Background(), &DevcontainerConfig{Image: "ubuntu"}, "/tmp/test", "abc123abc123", true, &strings.Builder{})
	if err != nil {
		t.Fatal(err)
	}
	if len(mock.pullCalls) != 1 {
		t.Errorf("expected pull on rebuild, got %d calls", len(mock.pullCalls))
	}
	if id != "sha256:fresh" {
		t.Errorf("expected fresh image ID, got %q", id)
	}
}

func TestEnsureImage_NoImageOrBuild(t *testing.T) {
	mock := &mockDockerClient{
		imageInspectErr: fmt.Errorf("not found"),
	}
	builder := &ImageBuilder{Docker: mock, Logger: testLogger()}
	_, _, err := builder.EnsureImage(context.Background(), &DevcontainerConfig{}, "/tmp/test", "hash", false, &strings.Builder{})
	if err == nil {
		t.Error("expected error when neither image nor build specified")
	}
}

// pullThenInspectMock wraps mockDockerClient to simulate: first inspect fails
// (cache miss), then succeeds after pull.
type pullThenInspectMock struct {
	*mockDockerClient
	inspectCallCount *int
	inspectErr       error
	inspectResult    ImageInspectResponse
}

func (m *pullThenInspectMock) ImageInspect(_ context.Context, _ string) (ImageInspectResponse, error) {
	*m.inspectCallCount++
	if *m.inspectCallCount == 1 && m.inspectErr != nil {
		return ImageInspectResponse{}, m.inspectErr
	}
	return m.inspectResult, nil
}
