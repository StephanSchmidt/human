package claude

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInstanceStateString(t *testing.T) {
	tests := []struct {
		state InstanceState
		want  string
	}{
		{StateUnknown, "⚪"},
		{StateBusy, "🔴"},
		{StateReady, "🟢"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("InstanceState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestFindNewestJSONL(t *testing.T) {
	dir := t.TempDir()

	old := filepath.Join(dir, "old.jsonl")
	if err := os.WriteFile(old, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}

	newFile := filepath.Join(dir, "new.jsonl")
	if err := os.WriteFile(newFile, []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := findNewestJSONL(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != newFile {
		t.Errorf("findNewestJSONL = %q, want %q", got, newFile)
	}
}

func TestFindNewestJSONL_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	got, err := findNewestJSONL(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}
