package index

import (
	"context"
	"time"
)

// Entry holds the metadata for a single indexed issue.
type Entry struct {
	Key       string    `json:"key"`
	Source    string    `json:"source"`     // instance name
	Kind      string    `json:"kind"`       // "jira", "github", etc.
	Project   string    `json:"project"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Assignee  string    `json:"assignee"`
	URL       string    `json:"url"`        // instance base URL
	IndexedAt time.Time `json:"indexed_at"`
}

// Stats summarises the current state of the index.
type Stats struct {
	TotalEntries  int            `json:"total_entries"`
	LastIndexedAt time.Time      `json:"last_indexed_at"`
	ByKind        map[string]int `json:"by_kind"`
	BySource      map[string]int `json:"by_source"`
}

// Store is the persistence interface for the search index.
type Store interface {
	UpsertEntry(ctx context.Context, entry Entry, description string) error
	DeleteEntry(ctx context.Context, key, source string) error
	Search(ctx context.Context, query string, limit int) ([]Entry, error)
	Stats(ctx context.Context) (*Stats, error)
	AllKeys(ctx context.Context, source string) ([]string, error)
	Close() error
}
