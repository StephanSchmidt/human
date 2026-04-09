package devcontainer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteReadMeta(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	m := Meta{
		Name:          "test-dc",
		ProjectDir:    "/home/user/project",
		ContainerID:   "abc123",
		ContainerName: "human-dc-project",
		ImageID:       "sha256:def456",
		ImageName:     "human-dc-project:abc123abc123",
		Status:        StatusRunning,
		CreatedAt:     time.Now().Truncate(time.Second),
		WorkspaceDir:  "/workspaces/project",
		RemoteUser:    "vscode",
		ConfigHash:    "abc123abc123abc123abc123",
	}

	if err := WriteMeta(m); err != nil {
		t.Fatal(err)
	}

	// File should exist with restricted permissions.
	path := MetaPath("test-dc")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions = %o, want 600", perm)
	}

	got, err := ReadMeta("test-dc")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != m.Name {
		t.Errorf("Name = %q, want %q", got.Name, m.Name)
	}
	if got.ContainerID != m.ContainerID {
		t.Errorf("ContainerID = %q, want %q", got.ContainerID, m.ContainerID)
	}
	if got.Status != StatusRunning {
		t.Errorf("Status = %q, want %q", got.Status, StatusRunning)
	}
	if !got.CreatedAt.Equal(m.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, m.CreatedAt)
	}
}

func TestReadMeta_NotFound(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	_, err := ReadMeta("nonexistent")
	if err == nil {
		t.Error("expected error for missing metadata")
	}
}

func TestListMetas(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Write two entries.
	for _, name := range []string{"alpha", "beta"} {
		if err := WriteMeta(Meta{
			Name:      name,
			Status:    StatusRunning,
			CreatedAt: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	metas, err := ListMetas()
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 2 {
		t.Errorf("got %d metas, want 2", len(metas))
	}
}

func TestListMetas_EmptyDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	metas, err := ListMetas()
	if err != nil {
		t.Fatal(err)
	}
	if metas != nil {
		t.Errorf("expected nil for missing directory, got %v", metas)
	}
}

func TestDeleteMeta(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := WriteMeta(Meta{Name: "todelete", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	if err := DeleteMeta("todelete"); err != nil {
		t.Fatal(err)
	}

	_, err := ReadMeta("todelete")
	if err == nil {
		t.Error("expected error after deletion")
	}
}

func TestFindMetaByProject(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	projDir := filepath.Join(tmp, "myproject")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := WriteMeta(Meta{
		Name:       "myproject",
		ProjectDir: projDir,
		Status:     StatusRunning,
		CreatedAt:  time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	m, found := FindMetaByProject(projDir)
	if !found {
		t.Fatal("expected to find meta by project dir")
	}
	if m.Name != "myproject" {
		t.Errorf("Name = %q, want %q", m.Name, "myproject")
	}

	_, found = FindMetaByProject("/nonexistent")
	if found {
		t.Error("should not find meta for nonexistent project")
	}
}

func TestDevcontainersDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	dir := DevcontainersDir()
	expected := filepath.Join(tmp, ".human", "devcontainers")
	if dir != expected {
		t.Errorf("DevcontainersDir() = %q, want %q", dir, expected)
	}
}
