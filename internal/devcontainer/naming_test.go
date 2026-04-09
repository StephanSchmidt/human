package devcontainer

import (
	"strings"
	"testing"
)

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"my-project", "my-project"},
		{"My Project", "my-project"},
		{"my_project_v2", "my-project-v2"},
		{"UPPER", "upper"},
		{"a.b.c", "a-b-c"},
		{"---", "devcontainer"},
		{"", "devcontainer"},
		{"hello world!", "hello-world"},
	}
	for _, tt := range tests {
		got := SanitizeName(tt.input)
		// Trim trailing hyphens for comparison since SanitizeName trims them.
		if got != tt.want {
			t.Errorf("SanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestImageName(t *testing.T) {
	name := ImageName("/home/user/my-project", "abcdef1234567890abcdef1234567890")
	if !strings.HasPrefix(name, "human-dc-my-project:") {
		t.Errorf("ImageName = %q, expected prefix human-dc-my-project:", name)
	}
	// Tag should be 12 chars of the hash.
	parts := strings.SplitN(name, ":", 2)
	if len(parts) != 2 {
		t.Fatalf("expected name:tag format, got %q", name)
	}
	if len(parts[1]) != 12 {
		t.Errorf("tag length = %d, want 12", len(parts[1]))
	}
}

func TestContainerName(t *testing.T) {
	name := ContainerName("/home/user/my-project")
	if name != "human-dc-my-project" {
		t.Errorf("ContainerName = %q, want %q", name, "human-dc-my-project")
	}
}

func TestConfigHash(t *testing.T) {
	h := ConfigHash([]byte(`{"image": "ubuntu"}`))
	if len(h) != 64 {
		t.Errorf("hash length = %d, want 64", len(h))
	}
	// Deterministic.
	if h2 := ConfigHash([]byte(`{"image": "ubuntu"}`)); h2 != h {
		t.Error("hash is not deterministic")
	}
	// Different input -> different hash.
	if h3 := ConfigHash([]byte(`{"image": "alpine"}`)); h3 == h {
		t.Error("different input produced same hash")
	}
}

func TestManagedLabels(t *testing.T) {
	labels := ManagedLabels("/home/user/project", "my-dc", "abc123")
	if labels[LabelManaged] != "true" {
		t.Errorf("managed label = %q", labels[LabelManaged])
	}
	if labels[LabelProject] != "/home/user/project" {
		t.Errorf("project label = %q", labels[LabelProject])
	}
	if labels[LabelName] != "my-dc" {
		t.Errorf("name label = %q", labels[LabelName])
	}
	if labels[LabelConfigHash] != "abc123" {
		t.Errorf("config-hash label = %q", labels[LabelConfigHash])
	}
}
