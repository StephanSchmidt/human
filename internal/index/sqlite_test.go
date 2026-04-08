package index

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore(:memory:): %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestUpsertEntry_insertsNew(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	entry := Entry{Key: "KAN-1", Source: "work", Kind: "jira", Project: "KAN", Title: "First issue", Status: "Open"}
	if err := s.UpsertEntry(ctx, entry, "detailed description here"); err != nil {
		t.Fatalf("UpsertEntry: %v", err)
	}

	keys, err := s.AllKeys(ctx, "work")
	if err != nil {
		t.Fatalf("AllKeys: %v", err)
	}
	if len(keys) != 1 || keys[0] != "KAN-1" {
		t.Errorf("expected [KAN-1], got %v", keys)
	}
}

func TestUpsertEntry_updatesExisting(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	entry := Entry{Key: "KAN-1", Source: "work", Kind: "jira", Project: "KAN", Title: "Old title", Status: "Open"}
	if err := s.UpsertEntry(ctx, entry, "old description"); err != nil {
		t.Fatalf("first UpsertEntry: %v", err)
	}

	entry.Title = "New title"
	entry.Status = "Done"
	if err := s.UpsertEntry(ctx, entry, "new description"); err != nil {
		t.Fatalf("second UpsertEntry: %v", err)
	}

	// Should still be one entry.
	keys, _ := s.AllKeys(ctx, "work")
	if len(keys) != 1 {
		t.Errorf("expected 1 key after update, got %d", len(keys))
	}

	// Search should find by new title.
	results, err := s.Search(ctx, "New title", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].Title != "New title" {
		t.Errorf("expected updated title, got %v", results)
	}
}

func TestSearch_matchesTitle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertEntry(ctx, Entry{Key: "KAN-1", Source: "work", Kind: "jira", Title: "Implement retry logic"}, "some desc"))
	require.NoError(t, s.UpsertEntry(ctx, Entry{Key: "KAN-2", Source: "work", Kind: "jira", Title: "Fix login page"}, "some desc"))

	results, err := s.Search(ctx, "retry", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].Key != "KAN-1" {
		t.Errorf("expected KAN-1, got %v", results)
	}
}

func TestSearch_matchesDescription(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertEntry(ctx, Entry{Key: "KAN-1", Source: "work", Kind: "jira", Title: "Generic title"}, "webhook delivery retry mechanism"))

	results, err := s.Search(ctx, "webhook", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].Key != "KAN-1" {
		t.Errorf("expected KAN-1 via description match, got %v", results)
	}
}

func TestSearch_matchesKey(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertEntry(ctx, Entry{Key: "KAN-42", Source: "work", Kind: "jira", Title: "Some issue"}, "desc"))

	results, err := s.Search(ctx, "KAN-42", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].Key != "KAN-42" {
		t.Errorf("expected KAN-42, got %v", results)
	}
}

func TestSearch_noResults(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertEntry(ctx, Entry{Key: "KAN-1", Source: "work", Kind: "jira", Title: "Fix bug"}, "desc"))

	results, err := s.Search(ctx, "nonexistent", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected no results, got %v", results)
	}
}

func TestSearch_limit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		key := "KAN-" + string(rune('1'+i))
		require.NoError(t, s.UpsertEntry(ctx, Entry{Key: key, Source: "work", Kind: "jira", Title: "retry issue"}, "desc"))
	}

	results, err := s.Search(ctx, "retry", 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results with limit, got %d", len(results))
	}
}

func TestSearch_ranking(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Title match should rank higher than description-only match.
	require.NoError(t, s.UpsertEntry(ctx, Entry{Key: "KAN-1", Source: "work", Kind: "jira", Title: "Unrelated title"}, "retry logic in the background"))
	require.NoError(t, s.UpsertEntry(ctx, Entry{Key: "KAN-2", Source: "work", Kind: "jira", Title: "Implement retry logic"}, "some description"))

	results, err := s.Search(ctx, "retry", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
	if results[0].Key != "KAN-2" {
		t.Errorf("expected title match KAN-2 first, got %s", results[0].Key)
	}
}

// TestSearchWithKind_filterAppliesBeforeLimit verifies the M11.3 fix:
// when a kind filter is combined with a limit, the filter is applied
// in SQL so the limit counts only matching-kind rows. Client-side
// post-filtering would silently hide all Notion hits when the top-N
// FTS matches happen to be GitHub issues.
func TestSearchWithKind_filterAppliesBeforeLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Seed 5 GitHub rows that will dominate the BM25 ranking.
	for i := 0; i < 5; i++ {
		key := "gh-" + string(rune('1'+i))
		require.NoError(t, s.UpsertEntry(ctx, Entry{
			Key: key, Source: "gh", Kind: "github", Title: "retry flow",
		}, "retry retry retry"))
	}
	// Plus one Notion row that also matches.
	require.NoError(t, s.UpsertEntry(ctx, Entry{
		Key: "note-1", Source: "notion", Kind: "notion", Title: "retry handbook",
	}, "description"))

	// With limit=3 the old client-side filter would return zero Notion
	// hits because the top-3 BM25 matches would all be GitHub.
	results, err := s.SearchWithKind(ctx, "retry", "notion", 3)
	if err != nil {
		t.Fatalf("SearchWithKind: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 Notion result, got %d", len(results))
	}
	if results[0].Key != "note-1" {
		t.Errorf("expected note-1, got %s", results[0].Key)
	}
}

func TestDeleteEntry_removes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertEntry(ctx, Entry{Key: "KAN-1", Source: "work", Kind: "jira", Title: "To delete"}, "desc"))

	if err := s.DeleteEntry(ctx, "KAN-1", "work"); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}

	keys, _ := s.AllKeys(ctx, "work")
	if len(keys) != 0 {
		t.Errorf("expected 0 keys after delete, got %d", len(keys))
	}

	// FTS should also be clean.
	results, _ := s.Search(ctx, "delete", 10)
	if len(results) != 0 {
		t.Errorf("expected 0 search results after delete, got %d", len(results))
	}
}

func TestDeleteEntry_notFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Should not error on missing entry.
	if err := s.DeleteEntry(ctx, "NOPE-1", "work"); err != nil {
		t.Fatalf("DeleteEntry on missing entry: %v", err)
	}
}

func TestStats_empty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	st, err := s.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.TotalEntries != 0 {
		t.Errorf("expected 0 entries, got %d", st.TotalEntries)
	}
}

func TestStats_populated(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertEntry(ctx, Entry{Key: "KAN-1", Source: "work", Kind: "jira"}, "d"))
	require.NoError(t, s.UpsertEntry(ctx, Entry{Key: "ENG-1", Source: "eng", Kind: "linear"}, "d"))
	require.NoError(t, s.UpsertEntry(ctx, Entry{Key: "KAN-2", Source: "work", Kind: "jira"}, "d"))

	st, err := s.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.TotalEntries != 3 {
		t.Errorf("expected 3 entries, got %d", st.TotalEntries)
	}
	if st.ByKind["jira"] != 2 {
		t.Errorf("expected 2 jira, got %d", st.ByKind["jira"])
	}
	if st.ByKind["linear"] != 1 {
		t.Errorf("expected 1 linear, got %d", st.ByKind["linear"])
	}
	if st.BySource["work"] != 2 {
		t.Errorf("expected 2 work, got %d", st.BySource["work"])
	}
	if st.BySource["eng"] != 1 {
		t.Errorf("expected 1 eng, got %d", st.BySource["eng"])
	}
}

func TestLastIndexedAt_empty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	ts, err := s.LastIndexedAt(ctx, "work")
	if err != nil {
		t.Fatalf("LastIndexedAt: %v", err)
	}
	if !ts.IsZero() {
		t.Errorf("expected zero time for empty source, got %v", ts)
	}
}

func TestLastIndexedAt_populated(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertEntry(ctx, Entry{Key: "KAN-1", Source: "work", Kind: "jira"}, "d"))
	require.NoError(t, s.UpsertEntry(ctx, Entry{Key: "ENG-1", Source: "eng", Kind: "linear"}, "d"))

	ts, err := s.LastIndexedAt(ctx, "work")
	if err != nil {
		t.Fatalf("LastIndexedAt: %v", err)
	}
	if ts.IsZero() {
		t.Error("expected non-zero time for populated source")
	}

	// Different source should have its own timestamp.
	ts2, err := s.LastIndexedAt(ctx, "eng")
	if err != nil {
		t.Fatalf("LastIndexedAt: %v", err)
	}
	if ts2.IsZero() {
		t.Error("expected non-zero time for eng source")
	}

	// Non-existent source should return zero.
	ts3, err := s.LastIndexedAt(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("LastIndexedAt: %v", err)
	}
	if !ts3.IsZero() {
		t.Errorf("expected zero time for nonexistent source, got %v", ts3)
	}
}

func TestAllKeys_bySource(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertEntry(ctx, Entry{Key: "KAN-1", Source: "work", Kind: "jira"}, "d"))
	require.NoError(t, s.UpsertEntry(ctx, Entry{Key: "ENG-1", Source: "eng", Kind: "linear"}, "d"))
	require.NoError(t, s.UpsertEntry(ctx, Entry{Key: "KAN-2", Source: "work", Kind: "jira"}, "d"))

	keys, err := s.AllKeys(ctx, "work")
	if err != nil {
		t.Fatalf("AllKeys: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 keys for work, got %d", len(keys))
	}

	keys, err = s.AllKeys(ctx, "eng")
	if err != nil {
		t.Fatalf("AllKeys: %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("expected 1 key for eng, got %d", len(keys))
	}
}

// Locks in the sanitiser's FTS5 quoting rules so a future refactor cannot
// accidentally let an operator (OR, AND, NOT, wildcards, colons, parens)
// leak through unquoted and widen a user query into an injection.
func TestSanitizeFTSQuery(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"only whitespace", "   ", ""},
		{"single word", "hello", `"hello"`},
		{"two words", "hello world", `"hello" "world"`},
		{"embedded quote", `he"llo`, `"he""llo"`},
		{"pre-quoted word not double wrapped", `"foo"`, `"foo"`},
		{"fts5 OR operator", "foo OR bar", `"foo" "OR" "bar"`},
		{"fts5 AND operator", "foo AND bar", `"foo" "AND" "bar"`},
		{"fts5 NOT operator", "foo NOT bar", `"foo" "NOT" "bar"`},
		{"fts5 wildcard", "foo*", `"foo*"`},
		{"fts5 paren", "(foo)", `"(foo)"`},
		{"fts5 colon", "col:value", `"col:value"`},
		{"fts5 caret", "foo^2", `"foo^2"`},
		{"unicode preserved", "héllo wörld", `"héllo" "wörld"`},
		{"newlines treated as whitespace", "foo\nbar", `"foo" "bar"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeFTSQuery(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// FuzzSanitizeFTSQuery verifies sanitizeFTSQuery never panics and every
// non-empty output consists of whitespace-separated tokens that each begin
// and end with a double-quote. That invariant is what keeps FTS5 from
// parsing any of the input as an operator.
func FuzzSanitizeFTSQuery(f *testing.F) {
	seeds := []string{
		"",
		"hello",
		"foo bar",
		`with "quote"`,
		"OR AND NOT",
		"(paren)",
		"col:val",
		"\x00nul\x00",
		strings.Repeat("a", 1024),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		out := sanitizeFTSQuery(input)
		if out == "" {
			return
		}
		for _, tok := range strings.Split(out, " ") {
			if !strings.HasPrefix(tok, `"`) || !strings.HasSuffix(tok, `"`) {
				t.Fatalf("token %q is not wrapped in quotes (input=%q)", tok, input)
			}
		}
	})
}
