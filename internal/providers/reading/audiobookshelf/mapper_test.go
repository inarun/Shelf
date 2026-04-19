package audiobookshelf

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/inarun/Shelf/internal/domain/precedence"
	"github.com/inarun/Shelf/internal/domain/timeline"
	"github.com/inarun/Shelf/internal/index/store"
)

// newTestStore returns a Store backed by a fresh in-process SQLite
// database. store.Open applies migrations internally.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "shelf.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seed(t *testing.T, s *store.Store, row store.BookRow) {
	t.Helper()
	if row.IndexedAtUnix == 0 {
		row.IndexedAtUnix = time.Now().Unix()
	}
	if _, err := s.UpsertBook(context.Background(), row); err != nil {
		t.Fatalf("UpsertBook: %v", err)
	}
}

func TestResolver_MatchByISBN13(t *testing.T) {
	s := newTestStore(t)
	seed(t, s, store.BookRow{
		Filename:      "Hyperion by Dan Simmons.md",
		CanonicalName: true,
		Title:         "Hyperion",
		Authors:       []string{"Dan Simmons"},
		ISBN:          "9780553283686",
		Status:        "unread",
	})
	rv, err := NewResolver(context.Background(), s)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	item := LibraryItem{
		ID: "ab-1",
		Media: LibraryMedia{Metadata: LibraryMediaMetadata{
			Title:      "Hyperion",
			AuthorName: "Dan Simmons",
			ISBN:       "978-0-553-28368-6",
		}},
	}
	res, ok := rv.Match(item)
	if !ok {
		t.Fatal("expected a match")
	}
	if res.Filename != "Hyperion by Dan Simmons.md" {
		t.Errorf("filename = %q", res.Filename)
	}
	if res.Reason != "ISBN13 match" {
		t.Errorf("reason = %q", res.Reason)
	}
	if res.NeedsUserDecision {
		t.Error("ISBN match must not need user decision")
	}
}

func TestResolver_MatchByISBN10Fallback(t *testing.T) {
	s := newTestStore(t)
	seed(t, s, store.BookRow{
		Filename:      "Book by Author.md",
		CanonicalName: true,
		Title:         "Book",
		Authors:       []string{"Author"},
		ISBN:          "055328368X",
		Status:        "unread",
	})
	rv, err := NewResolver(context.Background(), s)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	item := LibraryItem{
		ID: "ab-x",
		Media: LibraryMedia{Metadata: LibraryMediaMetadata{
			Title: "Book", AuthorName: "Author", ISBN: "0-5532-8368-X",
		}},
	}
	res, ok := rv.Match(item)
	if !ok {
		t.Fatal("expected match")
	}
	if res.Reason != "ISBN10 match" {
		t.Errorf("reason = %q", res.Reason)
	}
}

func TestResolver_FuzzyExactTitleAuthor_AutoMatch(t *testing.T) {
	s := newTestStore(t)
	seed(t, s, store.BookRow{
		Filename:      "Hyperion by Dan Simmons.md",
		CanonicalName: true,
		Title:         "Hyperion",
		Authors:       []string{"Dan Simmons"},
		Status:        "unread",
	})
	rv, _ := NewResolver(context.Background(), s)
	res, ok := rv.Match(LibraryItem{
		ID: "ab-h",
		Media: LibraryMedia{Metadata: LibraryMediaMetadata{
			Title: "Hyperion", AuthorName: "Dan Simmons",
		}},
	})
	if !ok || res.NeedsUserDecision {
		t.Fatalf("expected auto-match, got ok=%v decision=%v", ok, res.NeedsUserDecision)
	}
	if res.Reason != "fuzzy title+author match (ratio=1.00)" {
		t.Errorf("reason = %q", res.Reason)
	}
}

func TestResolver_FuzzyClose_NeedsDecisionWhenAuthorsDiffer(t *testing.T) {
	s := newTestStore(t)
	seed(t, s, store.BookRow{
		Filename:      "The Expanse by James Corey.md",
		CanonicalName: true,
		Title:         "The Expanse",
		Authors:       []string{"James Corey"},
		Status:        "unread",
	})
	rv, _ := NewResolver(context.Background(), s)
	// Same title, slightly different author surname → should flag for decision
	res, ok := rv.Match(LibraryItem{
		ID: "ab-e",
		Media: LibraryMedia{Metadata: LibraryMediaMetadata{
			Title: "The Expanse", AuthorName: "James Otherly",
		}},
	})
	if !ok {
		t.Fatal("expected a candidate result")
	}
	if !res.NeedsUserDecision {
		t.Errorf("expected NeedsUserDecision on author mismatch, got %+v", res)
	}
}

func TestResolver_NoMatchBelowThreshold(t *testing.T) {
	s := newTestStore(t)
	seed(t, s, store.BookRow{
		Filename: "Hyperion by Dan Simmons.md", CanonicalName: true,
		Title: "Hyperion", Authors: []string{"Dan Simmons"}, Status: "unread",
	})
	rv, _ := NewResolver(context.Background(), s)
	_, ok := rv.Match(LibraryItem{
		ID: "ab-none",
		Media: LibraryMedia{Metadata: LibraryMediaMetadata{
			Title: "Completely Unrelated Book", AuthorName: "Some Author",
		}},
	})
	if ok {
		t.Error("expected no match when title+author are unrelated")
	}
}

func TestItemToEntries_Finished(t *testing.T) {
	item := LibraryItem{ID: "ab-f", IsFinished: true, LastUpdate: 0}
	sessions := []ListeningSession{
		{LibraryItemID: "ab-f", StartedAt: 1_700_000_000_000, UpdatedAt: 1_700_050_000_000},
		{LibraryItemID: "ab-f", StartedAt: 1_700_010_000_000, UpdatedAt: 1_700_100_000_000},
		{LibraryItemID: "other", StartedAt: 1_700_001_000_000, UpdatedAt: 1_700_002_000_000},
	}
	got := ItemToEntries(item, sessions)
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	e := got[0]
	if e.ExternalID != "ab-f" {
		t.Errorf("ExternalID = %q", e.ExternalID)
	}
	if e.Source != precedence.SourceAudiobookshelf {
		t.Errorf("Source = %v", e.Source)
	}
	if e.Kind != timeline.KindFinished {
		t.Errorf("Kind = %q", e.Kind)
	}
	// Earliest across matching sessions is session 1 (1_700_000_000_000).
	if e.Start.UnixMilli() != 1_700_000_000_000 {
		t.Errorf("Start = %v (%d)", e.Start, e.Start.UnixMilli())
	}
	// Latest across matching sessions is session 2 (1_700_100_000_000).
	if e.End.UnixMilli() != 1_700_100_000_000 {
		t.Errorf("End = %v", e.End)
	}
}

func TestItemToEntries_InProgressZeroEnd(t *testing.T) {
	item := LibraryItem{ID: "ab-p", IsFinished: false}
	sessions := []ListeningSession{
		{LibraryItemID: "ab-p", StartedAt: 1_700_000_000_000, UpdatedAt: 1_700_020_000_000},
	}
	got := ItemToEntries(item, sessions)
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	if got[0].Kind != timeline.KindProgress {
		t.Errorf("Kind = %q (want progress)", got[0].Kind)
	}
	if !got[0].End.IsZero() {
		t.Errorf("End should be zero for in-progress entry, got %v", got[0].End)
	}
}

func TestItemToEntries_NoActivityReturnsNil(t *testing.T) {
	item := LibraryItem{ID: "ab-dead", LastUpdate: 0}
	if got := ItemToEntries(item, nil); got != nil {
		t.Errorf("want nil for no-activity item, got %+v", got)
	}
}

func TestItemToEntries_FallsBackToItemLastUpdate(t *testing.T) {
	// AB sometimes carries a finished item with no sessions rows; fall back
	// to item.LastUpdate as both Start and End.
	item := LibraryItem{ID: "ab-old", IsFinished: true, LastUpdate: 1_650_000_000_000}
	got := ItemToEntries(item, nil)
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	if got[0].Start.UnixMilli() != 1_650_000_000_000 || got[0].End.UnixMilli() != 1_650_000_000_000 {
		t.Errorf("Start/End fallback wrong: %v / %v", got[0].Start, got[0].End)
	}
}

func TestVaultEntriesFromFrontmatter_Pairs(t *testing.T) {
	started := []time.Time{
		time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
	}
	finished := []time.Time{
		time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC),
	}
	got := VaultEntriesFromFrontmatter(started, finished)
	if len(got) != 2 {
		t.Fatalf("want 2 entries (paired + trailing ongoing), got %d", len(got))
	}
	if got[0].Source != precedence.SourceVaultFrontmatter {
		t.Errorf("Source[0] = %v", got[0].Source)
	}
	if got[0].End.IsZero() {
		t.Error("first paired entry should have non-zero End")
	}
	if !got[1].End.IsZero() {
		t.Error("trailing unfinished entry should have zero End")
	}
	if got[1].Kind != timeline.KindProgress {
		t.Errorf("trailing Kind = %q (want progress)", got[1].Kind)
	}
}

func TestFirstAuthor(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"Dan Simmons", "Dan Simmons"},
		{"Dan Simmons, Other Guy", "Dan Simmons"},
		{"  Trimmed  , Other", "Trimmed"},
	}
	for _, tc := range tests {
		if got := firstAuthor(tc.in); got != tc.want {
			t.Errorf("firstAuthor(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeISBN(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"978-0-553-28368-6", "9780553283686"},
		{"  0 553 28368 X ", "055328368X"},
		{"055328368x", "055328368X"},
		{"abc", ""},
	}
	for _, tc := range tests {
		if got := normalizeISBN(tc.in); got != tc.want {
			t.Errorf("normalizeISBN(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
