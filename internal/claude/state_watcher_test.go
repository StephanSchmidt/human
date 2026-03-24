package claude

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestStateWatcher_DetectsWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	// Create the file first so fsnotify can watch it.
	if err := os.WriteFile(path, []byte("initial\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var called atomic.Int32
	w, err := NewStateWatcher(func(p string) {
		called.Add(1)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()

	if err := w.Watch(path); err != nil {
		t.Fatal(err)
	}

	// Write to the file.
	if err := os.WriteFile(path, []byte("updated\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Wait for callback.
	deadline := time.After(2 * time.Second)
	for called.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for fsnotify callback")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestStateWatcher_WatchIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")
	if err := os.WriteFile(path, []byte("data\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	w, err := NewStateWatcher(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()

	// Watch twice — should not error.
	if err := w.Watch(path); err != nil {
		t.Fatal(err)
	}
	if err := w.Watch(path); err != nil {
		t.Fatal(err)
	}
}

func TestStateWatcher_Unwatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")
	if err := os.WriteFile(path, []byte("data\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var called atomic.Int32
	w, err := NewStateWatcher(func(_ string) {
		called.Add(1)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()

	if err := w.Watch(path); err != nil {
		t.Fatal(err)
	}
	w.Unwatch(path)

	// Write — should NOT trigger callback.
	if err := os.WriteFile(path, []byte("updated\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	time.Sleep(200 * time.Millisecond)
	if called.Load() > 0 {
		t.Error("callback was called after Unwatch")
	}
}
