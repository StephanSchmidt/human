package claude

import (
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
)

// StateChangeHandler is called when a watched JSONL file changes.
type StateChangeHandler func(path string)

// StateWatcher watches JSONL files via fsnotify and triggers callbacks
// on changes. This replaces polling for host instances (RC-1, A4).
type StateWatcher struct {
	watcher  *fsnotify.Watcher
	mu       sync.Mutex
	paths    map[string]bool
	onChange StateChangeHandler
	done     chan struct{}
}

// NewStateWatcher creates a watcher that calls onChange when any
// watched JSONL file is modified.
func NewStateWatcher(onChange StateChangeHandler) (*StateWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	sw := &StateWatcher{
		watcher:  w,
		paths:    make(map[string]bool),
		onChange: onChange,
		done:     make(chan struct{}),
	}
	go sw.loop()
	return sw, nil
}

// Watch adds a file path to be watched. Safe for concurrent use.
func (sw *StateWatcher) Watch(path string) error {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if sw.paths[path] {
		return nil // already watching
	}
	if err := sw.watcher.Add(path); err != nil {
		return err
	}
	sw.paths[path] = true
	return nil
}

// Unwatch removes a file path from being watched.
func (sw *StateWatcher) Unwatch(path string) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if !sw.paths[path] {
		return
	}
	_ = sw.watcher.Remove(path)
	delete(sw.paths, path)
}

// Close stops the watcher and releases resources.
func (sw *StateWatcher) Close() error {
	close(sw.done)
	return sw.watcher.Close()
}

func (sw *StateWatcher) loop() {
	for {
		select {
		case event, ok := <-sw.watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				log.Debug().Str("path", event.Name).Str("op", event.Op.String()).Msg("state file changed")
				if sw.onChange != nil {
					sw.onChange(event.Name)
				}
			}
		case err, ok := <-sw.watcher.Errors:
			if !ok {
				return
			}
			log.Debug().Err(err).Msg("fsnotify error")
		case <-sw.done:
			return
		}
	}
}
